package routes

import (
	"hash/fnv"
	"math/rand"
	"sync"
	"time"
)

// Selector chooses a backend from a list based on different strategies.
type Selector struct {
	rng *rand.Rand
	mu  sync.Mutex
}

// NewSelector creates a new backend selector.
func NewSelector() *Selector {
	return &Selector{
		rng: rand.New(rand.NewSource(time.Now().UnixNano())),
	}
}

// SelectWeighted picks a backend using weighted random selection.
func (s *Selector) SelectWeighted(backends []CompiledRouteBackend) *CompiledRouteBackend {
	if len(backends) == 0 {
		return nil
	}
	if len(backends) == 1 {
		return &backends[0]
	}

	// Calculate total weight
	var totalWeight int32
	for _, b := range backends {
		totalWeight += b.Weight
	}

	if totalWeight <= 0 {
		// Equal weight fallback
		s.mu.Lock()
		idx := s.rng.Intn(len(backends))
		s.mu.Unlock()
		return &backends[idx]
	}

	// Pick random value in range
	s.mu.Lock()
	r := s.rng.Int31n(totalWeight)
	s.mu.Unlock()

	// Find corresponding backend
	var cumulative int32
	for i := range backends {
		cumulative += backends[i].Weight
		if r < cumulative {
			return &backends[i]
		}
	}

	return &backends[len(backends)-1]
}

// SelectConsistentHash picks a backend using consistent hashing.
// This ensures the same key always routes to the same backend (when available).
func (s *Selector) SelectConsistentHash(backends []CompiledRouteBackend, key string) *CompiledRouteBackend {
	if len(backends) == 0 {
		return nil
	}
	if len(backends) == 1 {
		return &backends[0]
	}

	h := fnv.New32a()
	h.Write([]byte(key))
	hash := h.Sum32()

	idx := int(hash) % len(backends)
	if idx < 0 {
		idx = -idx
	}

	return &backends[idx]
}

// SelectionStrategy defines how backends are selected.
type SelectionStrategy int

const (
	// StrategyWeightedRandom uses weighted random selection.
	StrategyWeightedRandom SelectionStrategy = iota
	// StrategyConsistentHash uses consistent hashing by key.
	StrategyConsistentHash
)

// Select picks a backend using the specified strategy.
func (s *Selector) Select(backends []CompiledRouteBackend, strategy SelectionStrategy, hashKey string) *CompiledRouteBackend {
	switch strategy {
	case StrategyConsistentHash:
		return s.SelectConsistentHash(backends, hashKey)
	default:
		return s.SelectWeighted(backends)
	}
}
