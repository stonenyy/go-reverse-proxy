package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"go-reverse-proxy/internal/config"
	"go-reverse-proxy/internal/healthcheck"
	"go-reverse-proxy/internal/logger"
	"go-reverse-proxy/internal/metrics"
	"go-reverse-proxy/internal/middleware"
	"go-reverse-proxy/internal/proxy"
)

func main() {
	// 命令行参数
	configPath := flag.String("config", "configs/config.yaml", "配置文件路径")
	flag.Parse()

	// 初始化日志
	if err := logger.Init("logs"); err != nil {
		log.Fatalf("初始化日志失败: %v", err)
	}
	defer func() {
		logger.Close()
	}()

	// 加载配置
	cfg, err := config.Load(*configPath)
	if err != nil {
		log.Fatalf("加载配置失败: %v", err)
	}

	log.Printf("✅ 加载配置成功: %s", *configPath)

	// 初始化路由
	router, err := proxy.NewRouter(cfg.Routes)
	if err != nil {
		log.Fatalf("初始化路由失败: %v", err)
	}

	// 构建请求处理器
	metricsStore := metrics.New()
	handler := buildHandler(cfg, router, metricsStore)

	// 启动健康检查和管理接口
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var checker *healthcheck.Checker
	mux := newMux(handler, router, nil, metricsStore)
	if cfg.HealthCheck.Enabled {
		checker = healthcheck.New(router.GetAllBackends(), cfg.HealthCheck)
		go checker.Start(ctx)
		mux = newMux(handler, router, checker, metricsStore)
		log.Printf("✅ 健康检查已启用: 间隔 %ds", cfg.HealthCheck.Interval)
	}

	addr := fmt.Sprintf(":%d", cfg.Server.Port)
	// 启动 HTTP Server
	srv := newServer(cfg, mux)

	// 优雅退出
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		log.Printf("🚀 反向代理启动成功，监听 %s", addr)
		log.Printf("   管理接口: http://localhost%s/__proxy/status", addr)
		log.Printf("   健康接口: http://localhost%s/__proxy/health", addr)
		log.Printf("   节点统计: http://localhost%s/__proxy/stats", addr)

		var listenErr error
		if cfg.TLS != nil {
			log.Printf("🔒 TLS 已启用")
			listenErr = srv.ListenAndServeTLS(cfg.TLS.CertFile, cfg.TLS.KeyFile)
		} else {
			listenErr = srv.ListenAndServe()
		}

		if listenErr != nil && listenErr != http.ErrServerClosed {
			log.Fatalf("服务器启动失败: %v", listenErr)
		}
	}()

	<-quit
	log.Println("⏳ 收到退出信号，开始优雅关闭...")
	cancel()

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer shutdownCancel()

	if err := srv.Shutdown(shutdownCtx); err != nil {
		log.Fatalf("优雅关闭失败: %v", err)
	}
	log.Println("👋 服务器已关闭")
}

func buildHandler(cfg *config.Config, router *proxy.Router, metrics *metrics.Metrics) http.Handler {
	middlewares := []func(http.Handler) http.Handler{
		middleware.Recovery,
		middleware.Logger,
		middleware.CORS,
		middleware.Metrics(metrics),
	}
	if cfg.RateLimit.Enabled {
		middlewares = append(middlewares,
			middleware.RateLimiter(cfg.RateLimit.RequestsPerSecond, cfg.RateLimit.Burst, metrics),
		)
		log.Printf("✅ 限流已启用: %.0f req/s, burst %d", cfg.RateLimit.RequestsPerSecond, cfg.RateLimit.Burst)
	}
	return middleware.Chain(router, middlewares...)
}

func newMux(handler http.Handler, router *proxy.Router, checker *healthcheck.Checker, metricsStore *metrics.Metrics) *http.ServeMux {
	mux := http.NewServeMux()
	mux.Handle("/", handler)
	mux.HandleFunc("/__proxy/status", statusHandler(router))
	mux.HandleFunc("/__proxy/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, "OK")
	})
	if checker != nil {
		mux.HandleFunc("/__proxy/stats", statsHandler(checker))
	}
	mux.HandleFunc("/__proxy/metrics", metricsHandler(metricsStore))
	return mux
}

func newServer(cfg *config.Config, handler http.Handler) *http.Server {
	return &http.Server{
		Addr:         fmt.Sprintf(":%d", cfg.Server.Port),
		Handler:      handler,
		ReadTimeout:  cfg.Server.ReadTimeoutDuration(),
		WriteTimeout: cfg.Server.WriteTimeoutDuration(),
		IdleTimeout:  cfg.Server.IdleTimeoutDuration(),
	}
}

// statusHandler 返回路由状态
func statusHandler(router *proxy.Router) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		backends := router.GetAllBackends()
		type backendInfo struct {
			URL   string `json:"url"`
			Alive bool   `json:"alive"`
		}
		var list []backendInfo
		for _, b := range backends {
			list = append(list, backendInfo{
				URL:   b.URL.String(),
				Alive: b.Alive.Load(),
			})
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"status":   "running",
			"backends": list,
			"time":     time.Now().Format(time.RFC3339),
		})
	}
}

// statsHandler 返回健康检查统计
func statsHandler(checker *healthcheck.Checker) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		stats := checker.GetStats()
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(stats)
	}
}

func metricsHandler(metricsStore *metrics.Metrics) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if metricsStore == nil {
			http.Error(w, "Metrics unavailable", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(metricsStore.Snapshot())
	}
}
