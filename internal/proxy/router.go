package proxy

import (
	"crypto/tls"
	"fmt"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"sort"
	"strings"
	"sync"
	"time"

	"go-reverse-proxy/internal/config"
	"go-reverse-proxy/internal/logger"
	"go-reverse-proxy/internal/middleware"

	"go.uber.org/zap"
)

// Route 路由规则
type Route struct {
	Path           string
	Backends       []*Backend
	Balancer       LoadBalancer
	ReverseProxies map[string]*httputil.ReverseProxy
	Handler        http.Handler
}

// Router 路由器
type Router struct {
	mu        sync.RWMutex
	routes    []*Route
	transport *http.Transport
}

func NewRouter(routes []config.RouteConfig) (*Router, error) {
	transport := &http.Transport{
		MaxIdleConns:          1000,
		MaxIdleConnsPerHost:   100,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   10 * time.Second,
		ResponseHeaderTimeout: 30 * time.Second,
	}

	router := &Router{transport: transport}
	if err := router.UpdateRoutes(routes); err != nil {
		return nil, err
	}

	return router, nil
}

// UpdateRoutes 动态更新路由规则
func (r *Router) UpdateRoutes(routes []config.RouteConfig) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	var newRoutes []*Route
	for _, rc := range routes {
		route := &Route{
			Path:           rc.Path,
			Balancer:       NewLoadBalancer(rc.LoadBalance),
			ReverseProxies: make(map[string]*httputil.ReverseProxy),
		}

		for _, bc := range rc.Backends {
			target, err := url.Parse(bc.URL)
			if err != nil {
				return fmt.Errorf("解析后端地址失败 %s: %w", bc.URL, err)
			}
			weight := bc.Weight
			if weight <= 0 {
				weight = 1
			}
			backend := &Backend{URL: target, Weight: weight}
			backend.Alive.Store(true)
			route.Backends = append(route.Backends, backend)

			rp := newReverseProxy(target, r.transport)
			route.ReverseProxies[bc.URL] = rp
		}

		// 构建核心转发 handler
		// WebSocket 请求完全绕过 ReverseProxy，直接 TCP 隧道转发，避免 Header 重复问题
		var h http.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			backend := route.Balancer.Next(route.Backends)
			if backend == nil {
				logger.Log.Error("No available backend nodes", zap.String("path", r.URL.Path))
				http.Error(w, "Service Unavailable", http.StatusServiceUnavailable)
				return
			}

			// ✅ WebSocket 请求：直接 TCP 隧道，不经过 ReverseProxy
			if isWebSocketRequest(r) {
				proxyWebSocket(w, r, backend.URL)
				return
			}

			// 普通 HTTP 请求：走 ReverseProxy
			rp, ok := route.ReverseProxies[backend.URL.String()]
			if !ok {
				logger.Log.Error("Internal Server Error", zap.String("path", r.URL.Path))
				http.Error(w, "Internal Server Error", http.StatusInternalServerError)
				return
			}
			logger.Log.Info("Proxying request",
				zap.String("method", r.Method),
				zap.String("path", r.URL.Path),
				zap.String("backend", backend.URL.String()),
			)
			rp.ServeHTTP(w, r)
		})

		if len(rc.RequiredHeaders) > 0 {
			h = middleware.HeaderAuth(rc.RequiredHeaders)(h)
			logger.Log.Info("Header authentication enabled for route",
				zap.String("path", rc.Path),
				zap.Int("rules_count", len(rc.RequiredHeaders)),
			)
		}

		route.Handler = h
		newRoutes = append(newRoutes, route)
	}

	sort.Slice(newRoutes, func(i, j int) bool {
		return len(newRoutes[i].Path) > len(newRoutes[j].Path)
	})

	r.routes = newRoutes
	logger.Log.Info("Router configuration updated successfully", zap.Int("routes_count", len(newRoutes)))
	return nil
}

func newReverseProxy(target *url.URL, transport *http.Transport) *httputil.ReverseProxy {
	rp := httputil.NewSingleHostReverseProxy(target)
	rp.Transport = transport

	originalDirector := rp.Director
	rp.Director = func(req *http.Request) {
		originalDirector(req)
		clientIP, _, err := net.SplitHostPort(req.RemoteAddr)
		if err != nil {
			clientIP = req.RemoteAddr
		}
		if prior := req.Header.Get("X-Forwarded-For"); prior != "" {
			req.Header.Set("X-Forwarded-For", prior+", "+clientIP)
		} else {
			req.Header.Set("X-Forwarded-For", clientIP)
		}
		req.Header.Set("X-Real-IP", clientIP)
		req.Header.Set("X-Forwarded-Host", req.Host)
		if req.TLS != nil {
			req.Header.Set("X-Forwarded-Proto", "https")
		} else {
			req.Header.Set("X-Forwarded-Proto", "http")
		}
	}

	rp.ErrorHandler = func(w http.ResponseWriter, r *http.Request, err error) {
		logger.Log.Error("Proxy request failed",
			zap.String("path", r.URL.Path),
			zap.String("target", target.String()),
			zap.Error(err),
		)
		http.Error(w, "Bad Gateway", http.StatusBadGateway)
	}

	rp.ModifyResponse = func(resp *http.Response) error {
		resp.Header.Set("Via", "go-reverse-proxy/1.1")
		return nil
	}

	return rp
}

// proxyWebSocket 使用原始 TCP 隧道转发 WebSocket，完全不经过 ReverseProxy
// 原理：HTTP CONNECT 隧道 —— 劫持客户端连接后直接与后端 TCP 双向 copy
// proxyWebSocket 使用原始 TCP 隧道转发 WebSocket，完全不经过 ReverseProxy
// 原理：HTTP CONNECT 隧道 —— 劫持客户端连接后直接与后端 TCP 双向 copy
func isWebSocketRequest(r *http.Request) bool {
	return strings.EqualFold(r.Header.Get("Upgrade"), "websocket") &&
		strings.Contains(strings.ToLower(r.Header.Get("Connection")), "upgrade")
}

func proxyWebSocket(w http.ResponseWriter, r *http.Request, target *url.URL) {
	// 连接后端
	backendAddr := target.Host
	if target.Port() == "" {
		if target.Scheme == "https" || target.Scheme == "wss" {
			backendAddr += ":443"
		} else {
			backendAddr += ":80"
		}
	}

	var backendConn net.Conn
	var err error
	if target.Scheme == "https" || target.Scheme == "wss" {
		dialer := &net.Dialer{
			Timeout:   10 * time.Second,
			KeepAlive: 30 * time.Second,
		}
		backendConn, err = tls.DialWithDialer(dialer, "tcp", backendAddr, &tls.Config{})
	} else {
		dialer := &net.Dialer{
			Timeout:   10 * time.Second,
			KeepAlive: 30 * time.Second,
		}
		backendConn, err = dialer.Dial("tcp", backendAddr)
	}
	if err != nil {
		logger.Log.Error("Connection failed", zap.String("addr", backendAddr), zap.Error(err))
		http.Error(w, "Bad Gateway", http.StatusBadGateway)
		return
	}
	defer backendConn.Close()

	// 劫持客户端连接
	hijacker, ok := w.(http.Hijacker)
	if !ok {
		logger.Log.Error("Hijack not supported", zap.String("path", r.URL.Path))
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}
	clientConn, bufrw, err := hijacker.Hijack()
	if err != nil {
		logger.Log.Error("Hijack failed", zap.String("path", r.URL.Path), zap.Error(err))
		return
	}
	defer clientConn.Close()

	// 设置 TCP KeepAlive 避免死连接
	if tc, ok := clientConn.(*net.TCPConn); ok {
		tc.SetKeepAlive(true)
		tc.SetKeepAlivePeriod(30 * time.Second)
	}

	// 把原始 HTTP 升级请求转发给后端
	if err := r.Write(backendConn); err != nil {
		logger.Log.Error("WebSocket handshake forward failed", zap.Error(err))
		return
	}

	logger.Log.Info("WebSocket connection established",
		zap.String("path", r.URL.Path),
		zap.String("backend", backendAddr),
	)

	// 双向 TCP copy
	done := make(chan struct{}, 2)
	const idleTimeout = 60 * time.Second

	copyFunc := func(dst, src net.Conn) {
		defer func() { done <- struct{}{} }()

		// 循环读取，以便在每次成功传输后刷新 Deadline
		buf := make([]byte, 32*1024)
		for {
			// 设置读超时，防止死连接
			src.SetReadDeadline(time.Now().Add(idleTimeout))
			n, err := src.Read(buf)
			if n > 0 {
				if _, werr := dst.Write(buf[:n]); werr != nil {
					return
				}
			}
			if err != nil {
				return
			}
		}
	}

	go func() {
		// 后端 → 客户端
		if bufrw.Reader.Buffered() > 0 {
			buf := make([]byte, bufrw.Reader.Buffered())
			bufrw.Read(buf)
			backendConn.Write(buf)
		}
		copyFunc(clientConn, backendConn)
	}()

	go func() {
		// 客户端 → 后端
		copyFunc(backendConn, clientConn)
	}()

	<-done
	logger.Log.Info("WebSocket connection closed", zap.String("path", r.URL.Path))
}

func (r *Router) Match(path string) *Route {
	r.mu.RLock()
	defer r.mu.RUnlock()
	for _, route := range r.routes {
		if strings.HasPrefix(path, route.Path) {
			return route
		}
	}
	return nil
}

// ServeHTTP 实现 http.Handler
func (r *Router) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	route := r.Match(req.URL.Path)
	if route == nil {
		http.Error(w, "No route found", http.StatusNotFound)
		return
	}
	route.Handler.ServeHTTP(w, req)
}

// GetAllBackends 获取所有后端节点（用于健康检查）
func (r *Router) GetAllBackends() []*Backend {
	seen := make(map[string]bool)
	var all []*Backend
	for _, route := range r.routes {
		for _, b := range route.Backends {
			key := b.URL.String()
			if !seen[key] {
				seen[key] = true
				all = append(all, b)
			}
		}
	}
	return all
}
