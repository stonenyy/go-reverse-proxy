package proxy

import (
	"fmt"
	"log"
	"net/http"
	"net/http/httputil"
	"net/url"
	"sort"
	"strings"
	"time"

	"go-reverse-proxy/internal/config"
	"go-reverse-proxy/internal/middleware"
)

// Route 路由规则
type Route struct {
	Path           string
	Backends       []*Backend
	Balancer       LoadBalancer
	ReverseProxies map[string]*httputil.ReverseProxy
	Handler        http.Handler // 含 Header 校验中间件的最终 handler
}

// Router 路由器
type Router struct {
	routes    []*Route
	transport *http.Transport
}

func NewRouter(routes []config.RouteConfig) (*Router, error) {
	transport := &http.Transport{
		MaxIdleConns:          100,
		MaxIdleConnsPerHost:   20,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   10 * time.Second,
		ResponseHeaderTimeout: 30 * time.Second,
	}

	router := &Router{transport: transport}

	for _, rc := range routes {
		route := &Route{
			Path:           rc.Path,
			Balancer:       NewLoadBalancer(rc.LoadBalance),
			ReverseProxies: make(map[string]*httputil.ReverseProxy),
		}

		for _, bc := range rc.Backends {
			target, err := url.Parse(bc.URL)
			if err != nil {
				return nil, fmt.Errorf("解析后端地址失败 %s: %w", bc.URL, err)
			}
			weight := bc.Weight
			if weight <= 0 {
				weight = 1
			}
			backend := &Backend{URL: target, Weight: weight}
			backend.Alive.Store(true)
			route.Backends = append(route.Backends, backend)

			rp := newReverseProxy(target, transport)
			route.ReverseProxies[bc.URL] = rp
		}

		// 构建该路由的核心转发 handler
		var h http.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			backend := route.Balancer.Next(route.Backends)
			if backend == nil {
				log.Printf("[ERROR] 没有可用的后端节点: %s", r.URL.Path)
				http.Error(w, "Service Unavailable", http.StatusServiceUnavailable)
				return
			}
			rp, ok := route.ReverseProxies[backend.URL.String()]
			if !ok {
				http.Error(w, "Internal Server Error", http.StatusInternalServerError)
				return
			}
			log.Printf("[INFO] %s %s -> %s", r.Method, r.URL.Path, backend.URL.String())
			rp.ServeHTTP(w, r)
		})

		// 如果配置了 required_headers，则在最外层挂载 HeaderAuth 中间件
		if len(rc.RequiredHeaders) > 0 {
			h = middleware.HeaderAuth(rc.RequiredHeaders)(h)
			log.Printf("[路由] %s 已启用 Header 校验，规则数: %d", rc.Path, len(rc.RequiredHeaders))
		}

		route.Handler = h
		router.routes = append(router.routes, route)
	}

	// 按路径长度降序排列（精确路径优先匹配）
	sort.Slice(router.routes, func(i, j int) bool {
		return len(router.routes[i].Path) > len(router.routes[j].Path)
	})

	return router, nil
}

func newReverseProxy(target *url.URL, transport *http.Transport) *httputil.ReverseProxy {
	rp := httputil.NewSingleHostReverseProxy(target)
	rp.Transport = transport

	originalDirector := rp.Director
	rp.Director = func(req *http.Request) {
		originalDirector(req)
		if clientIP := req.Header.Get("X-Forwarded-For"); clientIP == "" {
			req.Header.Set("X-Forwarded-For", req.RemoteAddr)
		}
		req.Header.Set("X-Real-IP", req.RemoteAddr)
		req.Header.Set("X-Forwarded-Host", req.Host)
		req.Header.Set("X-Forwarded-Proto", "http")
	}

	rp.ErrorHandler = func(w http.ResponseWriter, r *http.Request, err error) {
		log.Printf("[ERROR] 代理请求失败 %s -> %s: %v", r.URL.Path, target.String(), err)
		http.Error(w, "Bad Gateway", http.StatusBadGateway)
	}

	rp.ModifyResponse = func(resp *http.Response) error {
		resp.Header.Set("Via", "go-reverse-proxy/1.0")
		return nil
	}

	return rp
}

// Match 匹配路由
func (r *Router) Match(path string) *Route {
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
