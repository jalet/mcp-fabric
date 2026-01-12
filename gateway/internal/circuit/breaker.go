package circuit

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/jarsater/mcp-fabric/gateway/internal/metrics"
)

var (
	// ErrCircuitOpen is returned when the circuit breaker is open.
	ErrCircuitOpen = errors.New("circuit breaker open: too many concurrent requests")
	// ErrQueueFull is returned when the queue is full.
	ErrQueueFull = errors.New("queue full: cannot accept more requests")
	// ErrQueueTimeout is returned when waiting in queue times out.
	ErrQueueTimeout = errors.New("queue timeout: waited too long for capacity")
)

// Breaker implements a simple concurrency-limiting circuit breaker.
type Breaker struct {
	route         string
	maxConcurrent int32
	maxQueue      int32
	queueTimeout  time.Duration

	mu       sync.Mutex
	active   int32
	waiting  int32
	waitChan chan struct{}
}

// Config holds circuit breaker configuration.
type Config struct {
	MaxConcurrent int32
	MaxQueueSize  int32
	QueueTimeout  time.Duration
}

// DefaultConfig returns sensible default configuration.
func DefaultConfig() Config {
	return Config{
		MaxConcurrent: 100,
		MaxQueueSize:  50,
		QueueTimeout:  30 * time.Second,
	}
}

// New creates a new circuit breaker.
func New(route string, cfg Config) *Breaker {
	if cfg.MaxConcurrent <= 0 {
		cfg.MaxConcurrent = 100
	}
	if cfg.MaxQueueSize < 0 {
		cfg.MaxQueueSize = 0
	}
	if cfg.QueueTimeout <= 0 {
		cfg.QueueTimeout = 30 * time.Second
	}

	return &Breaker{
		route:         route,
		maxConcurrent: cfg.MaxConcurrent,
		maxQueue:      cfg.MaxQueueSize,
		queueTimeout:  cfg.QueueTimeout,
		waitChan:      make(chan struct{}, cfg.MaxConcurrent+cfg.MaxQueueSize),
	}
}

// Acquire tries to acquire a slot for processing a request.
// It blocks if at capacity (up to queue size), returns error if queue is full.
func (b *Breaker) Acquire(ctx context.Context) error {
	b.mu.Lock()

	// Check if we have capacity
	if b.active < b.maxConcurrent {
		b.active++
		b.updateMetrics()
		b.mu.Unlock()
		return nil
	}

	// Check if we can queue
	if b.waiting >= b.maxQueue {
		b.mu.Unlock()
		metrics.RecordCircuitBreakerRejection(b.route, "queue_full")
		return ErrQueueFull
	}

	// Queue this request
	b.waiting++
	b.updateMetrics()
	b.mu.Unlock()

	// Wait for capacity
	timer := time.NewTimer(b.queueTimeout)
	defer timer.Stop()

	select {
	case <-ctx.Done():
		b.mu.Lock()
		b.waiting--
		b.updateMetrics()
		b.mu.Unlock()
		return ctx.Err()
	case <-timer.C:
		b.mu.Lock()
		b.waiting--
		b.updateMetrics()
		b.mu.Unlock()
		metrics.RecordCircuitBreakerRejection(b.route, "timeout")
		return ErrQueueTimeout
	case <-b.waitChan:
		b.mu.Lock()
		b.waiting--
		b.active++
		b.updateMetrics()
		b.mu.Unlock()
		return nil
	}
}

// updateMetrics updates the Prometheus metrics for this breaker.
// Must be called while holding the lock.
func (b *Breaker) updateMetrics() {
	metrics.SetCircuitBreakerActive(b.route, int(b.active))
	metrics.SetCircuitBreakerWaiting(b.route, int(b.waiting))
}

// Release releases a slot back to the pool.
func (b *Breaker) Release() {
	b.mu.Lock()
	b.active--
	b.updateMetrics()

	// Signal a waiter if any
	if b.waiting > 0 {
		select {
		case b.waitChan <- struct{}{}:
		default:
		}
	}
	b.mu.Unlock()
}

// Stats returns current breaker statistics.
type Stats struct {
	Active      int32
	Waiting     int32
	MaxCapacity int32
	MaxQueue    int32
}

// Stats returns current statistics.
func (b *Breaker) Stats() Stats {
	b.mu.Lock()
	defer b.mu.Unlock()

	return Stats{
		Active:      b.active,
		Waiting:     b.waiting,
		MaxCapacity: b.maxConcurrent,
		MaxQueue:    b.maxQueue,
	}
}

// BreakerManager manages multiple circuit breakers (e.g., per-route).
type BreakerManager struct {
	mu       sync.RWMutex
	breakers map[string]*Breaker
	defaults Config
}

// NewManager creates a new breaker manager.
func NewManager(defaults Config) *BreakerManager {
	return &BreakerManager{
		breakers: make(map[string]*Breaker),
		defaults: defaults,
	}
}

// Get returns the breaker for a route, creating one if needed.
func (m *BreakerManager) Get(route string) *Breaker {
	m.mu.RLock()
	b, ok := m.breakers[route]
	m.mu.RUnlock()

	if ok {
		return b
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	// Double-check after acquiring write lock
	if b, ok := m.breakers[route]; ok {
		return b
	}

	b = New(route, m.defaults)
	m.breakers[route] = b
	return b
}

// UpdateConfig updates the default config for new breakers.
func (m *BreakerManager) UpdateConfig(cfg Config) {
	m.mu.Lock()
	m.defaults = cfg
	m.mu.Unlock()
}
