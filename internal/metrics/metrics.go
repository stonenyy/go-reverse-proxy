package metrics

import "sync/atomic"

type Metrics struct {
	TotalRequests atomic.Uint64
	TotalErrors   atomic.Uint64
	RateLimited   atomic.Uint64
}

type Snapshot struct {
	TotalRequests uint64 `json:"total_requests"`
	TotalErrors   uint64 `json:"total_errors"`
	RateLimited   uint64 `json:"rate_limited"`
}

func New() *Metrics {
	return &Metrics{}
}

func (m *Metrics) IncRequest() {
	m.TotalRequests.Add(1)
}

func (m *Metrics) IncError() {
	m.TotalErrors.Add(1)
}

func (m *Metrics) IncRateLimited() {
	m.RateLimited.Add(1)
}

func (m *Metrics) Snapshot() Snapshot {
	return Snapshot{
		TotalRequests: m.TotalRequests.Load(),
		TotalErrors:   m.TotalErrors.Load(),
		RateLimited:   m.RateLimited.Load(),
	}
}
