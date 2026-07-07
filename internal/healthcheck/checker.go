package healthcheck

import (
	"context"
	"log"
	"net/http"
	"net/url"
	"sync"
	"time"

	"go-reverse-proxy/internal/config"
	"go-reverse-proxy/internal/proxy"
)

// Checker 健康检查器
type Checker struct {
	backends []*proxy.Backend
	cfg      config.HealthCheckConfig
	client   *http.Client
	mu       sync.RWMutex
	stats    map[string]*Stats
}

// Stats 节点统计信息
type Stats struct {
	Total     int
	Success   int
	Fail      int
	LastCheck time.Time
	Alive     bool
}

func New(backends []*proxy.Backend, cfg config.HealthCheckConfig) *Checker {
	return &Checker{
		backends: backends,
		cfg:      cfg,
		client: &http.Client{
			Timeout: time.Duration(cfg.Timeout) * time.Second,
		},
		stats: make(map[string]*Stats),
	}
}

// Start 启动健康检查（后台 goroutine）
func (c *Checker) Start(ctx context.Context) {
	interval := time.Duration(c.cfg.Interval) * time.Second
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	log.Printf("[健康检查] 启动，间隔 %v", interval)
	c.checkAll() // 启动时立即检查一次

	for {
		select {
		case <-ctx.Done():
			log.Println("[健康检查] 停止")
			return
		case <-ticker.C:
			c.checkAll()
		}
	}
}

func (c *Checker) checkAll() {
	var wg sync.WaitGroup
	for _, b := range c.backends {
		wg.Add(1)
		go func(backend *proxy.Backend) {
			defer wg.Done()
			c.check(backend)
		}(b)
	}
	wg.Wait()
}

func (c *Checker) check(b *proxy.Backend) {
	checkURL := b.URL.ResolveReference(&url.URL{Path: c.cfg.Path}).String()

	resp, err := c.client.Get(checkURL)

	c.mu.Lock()
	if _, ok := c.stats[b.URL.String()]; !ok {
		c.stats[b.URL.String()] = &Stats{}
	}
	stat := c.stats[b.URL.String()]
	stat.Total++
	stat.LastCheck = time.Now()
	c.mu.Unlock()

	if err != nil {
		if b.Alive.Load() {
			log.Printf("[健康检查] ❌ 节点下线: %s (err: %v)", b.URL.String(), err)
		}
		b.Alive.Store(false)
		c.mu.Lock()
		stat.Fail++
		stat.Alive = false
		c.mu.Unlock()
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 500 {
		if b.Alive.Load() {
			log.Printf("[健康检查] ❌ 节点下线: %s (status: %d)", b.URL.String(), resp.StatusCode)
		}
		b.Alive.Store(false)
		c.mu.Lock()
		stat.Fail++
		stat.Alive = false
		c.mu.Unlock()
		return
	}

	if !b.Alive.Load() {
		log.Printf("[健康检查] ✅ 节点恢复: %s", b.URL.String())
	}
	b.Alive.Store(true)
	c.mu.Lock()
	stat.Success++
	stat.Alive = true
	c.mu.Unlock()
}

// GetStats 获取所有节点统计（用于 /status 接口）
func (c *Checker) GetStats() map[string]*Stats {
	c.mu.RLock()
	defer c.mu.RUnlock()

	result := make(map[string]*Stats)
	for k, v := range c.stats {
		cp := *v
		result[k] = &cp
	}
	return result
}
