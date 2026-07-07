package proxy

import (
	"math"
	"math/rand"
	"net/url"
	"sync"
	"sync/atomic"
)

// Backend 后端服务节点
type Backend struct {
	URL    *url.URL
	Weight int
	Alive  atomic.Bool
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
	mu             sync.Mutex
	currentWeights map[string]int
}

func (w *WeightedBalancer) Next(backends []*Backend) *Backend {
	alive := aliveBackends(backends)
	if len(alive) == 0 {
		return nil
	}

	w.mu.Lock()
	defer w.mu.Unlock()

	if w.currentWeights == nil {
		w.currentWeights = make(map[string]int)
	}

	totalWeight := 0
	for _, b := range alive {
		totalWeight += b.Weight
		if _, ok := w.currentWeights[b.URL.String()]; !ok {
			w.currentWeights[b.URL.String()] = 0
		}
	}

	var best *Backend
	bestWeight := math.MinInt
	for _, b := range alive {
		key := b.URL.String()
		w.currentWeights[key] += b.Weight
		if w.currentWeights[key] > bestWeight {
			bestWeight = w.currentWeights[key]
			best = b
		}
	}

	if best == nil {
		return alive[0]
	}

	w.currentWeights[best.URL.String()] -= totalWeight
	return best
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
