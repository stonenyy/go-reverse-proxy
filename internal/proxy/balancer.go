package proxy

import (
	"math/rand"
	"net/url"
	"sync"
	"sync/atomic"
)

// Backend 后端服务节点
type Backend struct {
	URL     *url.URL
	Weight  int
	Alive   atomic.Bool
}

// LoadBalancer 负载均衡器接口
type LoadBalancer interface {
	Next(backends []*Backend) *Backend
}

// ----------------------------------------
// 轮询负载均衡
// ----------------------------------------

type RoundRobinBalancer struct {
	counter uint64
}

func (r *RoundRobinBalancer) Next(backends []*Backend) *Backend {
	alive := aliveBackends(backends)
	if len(alive) == 0 {
		return nil
	}
	idx := atomic.AddUint64(&r.counter, 1) % uint64(len(alive))
	return alive[idx]
}

// ----------------------------------------
// 加权轮询负载均衡
// ----------------------------------------

type WeightedBalancer struct {
	mu      sync.Mutex
	current int
	cw      int // 当前权重
}

func (w *WeightedBalancer) Next(backends []*Backend) *Backend {
	alive := aliveBackends(backends)
	if len(alive) == 0 {
		return nil
	}

	w.mu.Lock()
	defer w.mu.Unlock()

	// 平滑加权轮询算法（Nginx 同款）
	totalWeight := 0
	for _, b := range alive {
		totalWeight += b.Weight
	}

	type item struct {
		backend       *Backend
		currentWeight int
	}

	items := make([]item, len(alive))
	for i, b := range alive {
		items[i] = item{backend: b, currentWeight: b.Weight}
	}

	// 找最大权重节点
	best := -1
	maxWeight := -1 << 62

	for i := range items {
		if items[i].currentWeight > maxWeight {
			maxWeight = items[i].currentWeight
			best = i
		}
	}

	if best == -1 {
		return alive[0]
	}

	items[best].currentWeight -= totalWeight
	for i := range items {
		items[i].currentWeight += items[i].backend.Weight
	}

	return items[best].backend
}

// ----------------------------------------
// 随机负载均衡
// ----------------------------------------

type RandomBalancer struct{}

func (r *RandomBalancer) Next(backends []*Backend) *Backend {
	alive := aliveBackends(backends)
	if len(alive) == 0 {
		return nil
	}
	return alive[rand.Intn(len(alive))]
}

// ----------------------------------------
// 工具函数
// ----------------------------------------

func aliveBackends(backends []*Backend) []*Backend {
	var alive []*Backend
	for _, b := range backends {
		if b.Alive.Load() {
			alive = append(alive, b)
		}
	}
	return alive
}

func NewLoadBalancer(strategy string) LoadBalancer {
	switch strategy {
	case "weighted":
		return &WeightedBalancer{}
	case "random":
		return &RandomBalancer{}
	default:
		return &RoundRobinBalancer{}
	}
}
