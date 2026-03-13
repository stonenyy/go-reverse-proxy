package middleware

import (
	"bufio"
	"fmt"
	"log"
	"net"
	"net/http"
	"runtime/debug"
	"time"

	"go-reverse-proxy/internal/config"

	"golang.org/x/time/rate"
)

// responseWriter 包装，用于捕获状态码
type responseWriter struct {
	http.ResponseWriter
	statusCode int
	written    int64
}

func (rw *responseWriter) WriteHeader(code int) {
	rw.statusCode = code
	rw.ResponseWriter.WriteHeader(code)
}

func (rw *responseWriter) Write(b []byte) (int, error) {
	n, err := rw.ResponseWriter.Write(b)
	rw.written += int64(n)
	return n, err
}

func (rw *responseWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	hijacker, ok := rw.ResponseWriter.(http.Hijacker)
	if !ok {
		return nil, nil, fmt.Errorf("底层 ResponseWriter 不支持 Hijack")
	}
	return hijacker.Hijack()
}

// Logger 请求日志中间件
func Logger(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rw := &responseWriter{ResponseWriter: w, statusCode: http.StatusOK}
		next.ServeHTTP(rw, r)
		duration := time.Since(start)
		log.Printf("[%s] %d %s %s %s %v %db",
			r.RemoteAddr, rw.statusCode, r.Method, r.Host, r.URL.Path, duration, rw.written)
	})
}

// Recovery panic 恢复中间件
func Recovery(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if err := recover(); err != nil {
				log.Printf("[PANIC] %v\n%s", err, debug.Stack())
				http.Error(w, "Internal Server Error", http.StatusInternalServerError)
			}
		}()
		next.ServeHTTP(w, r)
	})
}

// RateLimiter 限流中间件（令牌桶算法）
func RateLimiter(rps float64, burst int) func(http.Handler) http.Handler {
	limiter := rate.NewLimiter(rate.Limit(rps), burst)
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !limiter.Allow() {
				log.Printf("[RATE LIMIT] 请求被限流: %s %s", r.Method, r.URL.Path)
				w.Header().Set("Retry-After", "1")
				http.Error(w, "Too Many Requests", http.StatusTooManyRequests)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// HeaderAuth Header 访问限制中间件
// 支持两种校验：
//   - 只检查 Header 是否存在（rule.Value 为空）
//   - 检查 Header 存在且值精确匹配（rule.Value 非空）
//
// 任意规则不通过 → 401 Unauthorized
func HeaderAuth(rules []config.HeaderRule) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			for _, rule := range rules {
				val := r.Header.Get(rule.Name)

				// Header 不存在
				if val == "" {
					log.Printf("[AUTH] 缺少 Header: %s, path: %s, ip: %s",
						rule.Name, r.URL.Path, r.RemoteAddr)
					w.Header().Set("WWW-Authenticate", `Bearer realm="proxy"`)
					http.Error(w, "Unauthorized: missing header "+rule.Name, http.StatusUnauthorized)
					return
				}

				// Header 存在但值不匹配
				if rule.Value != "" && val != rule.Value {
					log.Printf("[AUTH] Header 值不匹配: %s, path: %s, ip: %s",
						rule.Name, r.URL.Path, r.RemoteAddr)
					w.Header().Set("WWW-Authenticate", `Bearer realm="proxy"`)
					http.Error(w, "Unauthorized: invalid header "+rule.Name, http.StatusUnauthorized)
					return
				}
			}
			next.ServeHTTP(w, r)
		})
	}
}

// CORS 跨域中间件
func CORS(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS, PATCH")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization, X-Requested-With")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// Chain 中间件链式组合
func Chain(handler http.Handler, middlewares ...func(http.Handler) http.Handler) http.Handler {
	for i := len(middlewares) - 1; i >= 0; i-- {
		handler = middlewares[i](handler)
	}
	return handler
}
