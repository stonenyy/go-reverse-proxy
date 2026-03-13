// 模拟后端服务，用于测试反向代理
// 启动后会在 9001、9002、9003 端口各起一个 HTTP 服务
package main

import (
	"fmt"
	"log"
	"net/http"
	"sync"
	"time"
)

func startServer(port int) {
	mux := http.NewServeMux()

	// 普通接口
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, `{"server": "backend-%d", "path": "%s", "time": "%s"}`,
			port, r.URL.Path, time.Now().Format(time.RFC3339))
	})

	// 健康检查接口
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		fmt.Fprintf(w, `{"status": "ok", "port": %d}`, port)
	})

	// API 接口
	mux.HandleFunc("/api/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{"server": "backend-%d", "api": true, "path": "%s"}`, port, r.URL.Path)
	})

	// 静态资源接口
	mux.HandleFunc("/static/", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, "static file from backend-%d: %s", port, r.URL.Path)
	})

	addr := fmt.Sprintf(":%d", port)
	log.Printf("✅ 模拟后端 backend-%d 启动: http://localhost%s", port, addr)

	if err := http.ListenAndServe(addr, mux); err != nil {
		log.Fatalf("后端 %d 启动失败: %v", port, err)
	}
}

func main() {
	ports := []int{9001, 9002, 9003}

	var wg sync.WaitGroup
	for _, port := range ports {
		wg.Add(1)
		p := port
		go func() {
			defer wg.Done()
			startServer(p)
		}()
	}

	fmt.Println("🚀 模拟后端已启动:")
	for _, p := range ports {
		fmt.Printf("   http://localhost:%d\n", p)
	}
	fmt.Println("\n按 Ctrl+C 退出")

	wg.Wait()
}
