package routes

import (
	"encoding/json"
	"os"
	"regexp"
	"sync"
)

// RouteConfig is the compiled routing configuration.
type RouteConfig struct {
	Rules    []CompiledRouteRule `json:"rules"`
	Defaults *RouteDefaultConfig `json:"defaults,omitempty"`
}

// CompiledRouteRule is a pre-compiled route rule.
type CompiledRouteRule struct {
	Name     string                 `json:"name"`
	Priority int32                  `json:"priority"`
	Match    CompiledRouteMatch     `json:"match"`
	Backends []CompiledRouteBackend `json:"backends"`
}

// CompiledRouteMatch is the match criteria for a rule.
type CompiledRouteMatch struct {
	Agent       string            `json:"agent,omitempty"`
	IntentRegex string            `json:"intentRegex,omitempty"`
	TenantID    string            `json:"tenantId,omitempty"`
	Headers     map[string]string `json:"headers,omitempty"`
}

// CompiledRouteBackend is a resolved backend.
type CompiledRouteBackend struct {
	AgentName string `json:"agentName"`
	Namespace string `json:"namespace"`
	Endpoint  string `json:"endpoint"`
	Weight    int32  `json:"weight"`
	Ready     bool   `json:"ready"`
}

// RouteDefaultConfig contains default routing configuration.
type RouteDefaultConfig struct {
	Backend          *CompiledRouteBackend `json:"backend,omitempty"`
	MaxConcurrent    int32                 `json:"maxConcurrent"`
	MaxQueueSize     int32                 `json:"maxQueueSize"`
	QueueTimeoutMs   int64                 `json:"queueTimeoutMs"`
	RequestTimeoutMs int64                 `json:"requestTimeoutMs"`
	RejectUnmatched  bool                  `json:"rejectUnmatched"`
}

// Table holds the in-memory route table with compiled regexes.
type Table struct {
	mu       sync.RWMutex
	config   *RouteConfig
	compiled []compiledRule
}

type compiledRule struct {
	rule        CompiledRouteRule
	intentRegex *regexp.Regexp
}

// NewTable creates a new route table.
func NewTable() *Table {
	return &Table{}
}

// LoadFromFile loads routing configuration from a JSON file.
func (t *Table) LoadFromFile(path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	return t.LoadFromJSON(data)
}

// LoadFromJSON loads routing configuration from JSON bytes.
func (t *Table) LoadFromJSON(data []byte) error {
	var config RouteConfig
	if err := json.Unmarshal(data, &config); err != nil {
		return err
	}

	// Pre-compile regexes
	compiled := make([]compiledRule, 0, len(config.Rules))
	for _, rule := range config.Rules {
		cr := compiledRule{rule: rule}
		if rule.Match.IntentRegex != "" {
			re, err := regexp.Compile(rule.Match.IntentRegex)
			if err != nil {
				return err
			}
			cr.intentRegex = re
		}
		compiled = append(compiled, cr)
	}

	t.mu.Lock()
	t.config = &config
	t.compiled = compiled
	t.mu.Unlock()

	return nil
}

// MatchRequest finds backends matching the given request parameters.
type MatchRequest struct {
	Agent    string
	Intent   string
	TenantID string
	Headers  map[string]string
}

// MatchResult contains the matched backends.
type MatchResult struct {
	RuleName string
	Backends []CompiledRouteBackend
}

// Match finds the first matching rule and returns its ready backends.
func (t *Table) Match(req MatchRequest) *MatchResult {
	t.mu.RLock()
	defer t.mu.RUnlock()

	if t.config == nil {
		return nil
	}

	// Try explicit agent match first
	if req.Agent != "" {
		for _, cr := range t.compiled {
			if cr.rule.Match.Agent == req.Agent {
				readyBackends := filterReadyBackends(cr.rule.Backends)
				if len(readyBackends) > 0 {
					return &MatchResult{
						RuleName: cr.rule.Name,
						Backends: readyBackends,
					}
				}
			}
		}
	}

	// Try other rules (by priority, already sorted)
	for _, cr := range t.compiled {
		if t.ruleMatches(cr, req) {
			readyBackends := filterReadyBackends(cr.rule.Backends)
			if len(readyBackends) > 0 {
				return &MatchResult{
					RuleName: cr.rule.Name,
					Backends: readyBackends,
				}
			}
		}
	}

	// Fall back to default backend
	if t.config.Defaults != nil && t.config.Defaults.Backend != nil {
		if t.config.Defaults.Backend.Ready {
			return &MatchResult{
				RuleName: "_default",
				Backends: []CompiledRouteBackend{*t.config.Defaults.Backend},
			}
		}
	}

	return nil
}

func (t *Table) ruleMatches(cr compiledRule, req MatchRequest) bool {
	match := cr.rule.Match

	// Check agent name
	if match.Agent != "" && match.Agent != req.Agent {
		return false
	}

	// Check intent regex
	if cr.intentRegex != nil {
		if !cr.intentRegex.MatchString(req.Intent) {
			return false
		}
	}

	// Check tenant ID
	if match.TenantID != "" && match.TenantID != req.TenantID {
		return false
	}

	// Check headers
	for k, v := range match.Headers {
		if req.Headers[k] != v {
			return false
		}
	}

	return true
}

func filterReadyBackends(backends []CompiledRouteBackend) []CompiledRouteBackend {
	var ready []CompiledRouteBackend
	for _, b := range backends {
		if b.Ready {
			ready = append(ready, b)
		}
	}
	return ready
}

// GetDefaults returns the default configuration.
func (t *Table) GetDefaults() *RouteDefaultConfig {
	t.mu.RLock()
	defer t.mu.RUnlock()

	if t.config == nil {
		return nil
	}
	return t.config.Defaults
}

// GetConfig returns a copy of the current config (for debugging/discovery).
func (t *Table) GetConfig() *RouteConfig {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return t.config
}
