package api

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/jarsater/mcp-fabric/gateway/internal/circuit"
	"github.com/jarsater/mcp-fabric/gateway/internal/metrics"
	"github.com/jarsater/mcp-fabric/gateway/internal/routes"
)

// InvokeRequest is the request body for POST /v1/invoke.
type InvokeRequest struct {
	Agent         string                 `json:"agent,omitempty"`
	Intent        string                 `json:"intent,omitempty"`
	Query         string                 `json:"query"`
	TenantID      string                 `json:"tenantId,omitempty"`
	CorrelationID string                 `json:"correlationId,omitempty"`
	Input         map[string]interface{} `json:"input,omitempty"`
	Metadata      map[string]interface{} `json:"metadata,omitempty"`
}

// InvokeResponse is the response from POST /v1/invoke.
type InvokeResponse struct {
	Success       bool                   `json:"success"`
	Result        interface{}            `json:"result,omitempty"`
	Error         string                 `json:"error,omitempty"`
	Agent         string                 `json:"agent,omitempty"`
	CorrelationID string                 `json:"correlationId,omitempty"`
	LatencyMs     int64                  `json:"latencyMs,omitempty"`
	Metadata      map[string]interface{} `json:"metadata,omitempty"`
}

// Handler handles HTTP requests for the agent gateway.
type Handler struct {
	table      *routes.Table
	selector   *routes.Selector
	breakers   *circuit.BreakerManager
	httpClient *http.Client
	reqTimeout time.Duration
}

// NewHandler creates a new API handler.
func NewHandler(table *routes.Table, reqTimeout time.Duration) *Handler {
	if reqTimeout <= 0 {
		reqTimeout = 5 * time.Minute
	}

	return &Handler{
		table:    table,
		selector: routes.NewSelector(),
		breakers: circuit.NewManager(circuit.DefaultConfig()),
		httpClient: &http.Client{
			Timeout: reqTimeout,
		},
		reqTimeout: reqTimeout,
	}
}

// UpdateDefaults updates circuit breaker defaults from route config.
func (h *Handler) UpdateDefaults() {
	defaults := h.table.GetDefaults()
	if defaults == nil {
		return
	}

	h.breakers.UpdateConfig(circuit.Config{
		MaxConcurrent: defaults.MaxConcurrent,
		MaxQueueSize:  defaults.MaxQueueSize,
		QueueTimeout:  time.Duration(defaults.QueueTimeoutMs) * time.Millisecond,
	})

	if defaults.RequestTimeoutMs > 0 {
		h.reqTimeout = time.Duration(defaults.RequestTimeoutMs) * time.Millisecond
		h.httpClient.Timeout = h.reqTimeout
	}
}

// ServeHTTP implements http.Handler.
func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	switch {
	case r.Method == http.MethodPost && r.URL.Path == "/v1/invoke":
		h.handleInvoke(w, r)
	case r.Method == http.MethodGet && r.URL.Path == "/v1/agents":
		h.handleListAgents(w, r)
	case r.Method == http.MethodGet && r.URL.Path == "/v1/routes":
		h.handleListRoutes(w, r)
	case r.Method == http.MethodGet && r.URL.Path == "/healthz":
		h.handleHealthz(w, r)
	default:
		http.NotFound(w, r)
	}
}

func (h *Handler) handleInvoke(w http.ResponseWriter, r *http.Request) {
	start := time.Now()
	var agentName, routeName string
	var statusCode = http.StatusOK

	// Ensure metrics are recorded on exit
	defer func() {
		duration := time.Since(start).Seconds()
		metrics.RecordRequest(agentName, routeName, strconv.Itoa(statusCode), duration)
	}()

	// Parse request
	var req InvokeRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		statusCode = http.StatusBadRequest
		metrics.RecordRequestError(agentName, routeName, "invalid_request")
		h.writeError(w, statusCode, "invalid request body: "+err.Error())
		return
	}

	// Match route
	matchResult := h.table.Match(routes.MatchRequest{
		Agent:    req.Agent,
		Intent:   req.Intent,
		TenantID: req.TenantID,
		Headers:  extractHeaders(r),
	})

	if matchResult == nil || len(matchResult.Backends) == 0 {
		metrics.RecordRouteNoMatch()
		defaults := h.table.GetDefaults()
		if defaults != nil && defaults.RejectUnmatched {
			statusCode = http.StatusBadRequest
			metrics.RecordRequestError(agentName, routeName, "no_route_match")
			h.writeError(w, statusCode, "no matching route found")
			return
		}
		statusCode = http.StatusNotFound
		metrics.RecordRequestError(agentName, routeName, "no_agent")
		h.writeError(w, statusCode, "no available agent for this request")
		return
	}

	routeName = matchResult.RuleName
	metrics.RecordRouteMatch(routeName, matchResult.RuleName)

	// Select backend
	var backend *routes.CompiledRouteBackend
	if req.TenantID != "" || req.CorrelationID != "" {
		// Use consistent hashing for sticky sessions
		hashKey := req.TenantID + ":" + req.CorrelationID
		backend = h.selector.Select(matchResult.Backends, routes.StrategyConsistentHash, hashKey)
	} else {
		backend = h.selector.Select(matchResult.Backends, routes.StrategyWeightedRandom, "")
	}

	if backend == nil {
		statusCode = http.StatusServiceUnavailable
		metrics.RecordRequestError(agentName, routeName, "no_backend")
		h.writeError(w, statusCode, "no backend available")
		return
	}

	agentName = backend.AgentName

	// Acquire circuit breaker slot
	breaker := h.breakers.Get(matchResult.RuleName)
	if err := breaker.Acquire(r.Context()); err != nil {
		statusCode = http.StatusServiceUnavailable
		var errorType string
		switch err {
		case circuit.ErrQueueFull:
			errorType = "queue_full"
			metrics.RecordCircuitBreakerRejection(routeName, "queue_full")
		case circuit.ErrQueueTimeout:
			errorType = "queue_timeout"
			metrics.RecordCircuitBreakerRejection(routeName, "timeout")
		default:
			errorType = "circuit_breaker"
		}
		metrics.RecordRequestError(agentName, routeName, errorType)
		h.writeError(w, statusCode, err.Error())
		return
	}
	defer breaker.Release()

	// Record backend forward
	metrics.RecordBackendForward(agentName, backend.Namespace)

	// Forward request to agent
	result, err := h.forwardToAgent(r.Context(), backend, &req)
	if err != nil {
		statusCode = http.StatusBadGateway
		metrics.RecordRequestError(agentName, routeName, "agent_error")
		h.writeError(w, statusCode, "agent error: "+err.Error())
		return
	}

	// Build response
	resp := InvokeResponse{
		Success:       true,
		Result:        result,
		Agent:         backend.AgentName,
		CorrelationID: req.CorrelationID,
		LatencyMs:     time.Since(start).Milliseconds(),
	}

	h.writeJSON(w, statusCode, resp)
}

func (h *Handler) forwardToAgent(ctx context.Context, backend *routes.CompiledRouteBackend, req *InvokeRequest) (interface{}, error) {
	// Build request to agent
	agentReq := map[string]interface{}{
		"query":         req.Query,
		"input":         req.Input,
		"metadata":      req.Metadata,
		"correlationId": req.CorrelationID,
		"tenantId":      req.TenantID,
	}

	body, err := json.Marshal(agentReq)
	if err != nil {
		return nil, err
	}

	// Ensure endpoint uses FQDN format (trailing dot) to avoid search domain issues
	endpoint := backend.Endpoint
	if strings.Contains(endpoint, ".svc.cluster.local") && !strings.HasSuffix(strings.Split(endpoint, ":")[0], ".") {
		parts := strings.SplitN(endpoint, ":", 2)
		if len(parts) == 2 {
			endpoint = parts[0] + ".:" + parts[1]
		}
	}

	// Create HTTP request
	url := fmt.Sprintf("http://%s/invoke", endpoint)
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Content-Type", "application/json")

	// Execute
	resp, err := h.httpClient.Do(httpReq)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()

	// Read response
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("agent returned %d: %s", resp.StatusCode, string(respBody))
	}

	// Parse response
	var result interface{}
	if err := json.Unmarshal(respBody, &result); err != nil {
		// Return raw response if not JSON
		return string(respBody), nil
	}

	return result, nil
}

func (h *Handler) handleListAgents(w http.ResponseWriter, r *http.Request) {
	config := h.table.GetConfig()
	if config == nil {
		h.writeJSON(w, http.StatusOK, map[string]interface{}{"agents": []string{}})
		return
	}

	// Collect unique agents
	agents := make(map[string]bool)
	for _, rule := range config.Rules {
		for _, backend := range rule.Backends {
			if backend.Ready {
				agents[backend.Namespace+"/"+backend.AgentName] = true
			}
		}
	}

	var agentList []string
	for a := range agents {
		agentList = append(agentList, a)
	}

	h.writeJSON(w, http.StatusOK, map[string]interface{}{"agents": agentList})
}

func (h *Handler) handleListRoutes(w http.ResponseWriter, r *http.Request) {
	config := h.table.GetConfig()
	if config == nil {
		h.writeJSON(w, http.StatusOK, map[string]interface{}{"routes": []string{}})
		return
	}

	var routeNames []string
	for _, rule := range config.Rules {
		routeNames = append(routeNames, rule.Name)
	}

	h.writeJSON(w, http.StatusOK, map[string]interface{}{"routes": routeNames, "count": len(routeNames)})
}

func (h *Handler) handleHealthz(w http.ResponseWriter, r *http.Request) {
	h.writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (h *Handler) writeJSON(w http.ResponseWriter, status int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(data)
}

func (h *Handler) writeError(w http.ResponseWriter, status int, message string) {
	h.writeJSON(w, status, InvokeResponse{
		Success: false,
		Error:   message,
	})
}

func extractHeaders(r *http.Request) map[string]string {
	headers := make(map[string]string)
	for k, v := range r.Header {
		if len(v) > 0 {
			headers[k] = v[0]
		}
	}
	return headers
}
