package mcp

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"go.uber.org/zap"

	"github.com/jarsater/mcp-fabric/gateway/internal/k8s"
	"github.com/jarsater/mcp-fabric/gateway/internal/metrics"
)

const (
	protocolVersion = "2024-11-05"
	serverName      = "mcp-fabric-gateway"
	serverVersion   = "1.0.0"
)

// Handler handles MCP protocol requests.
type Handler struct {
	logger         *zap.SugaredLogger
	watcher        *k8s.AgentWatcher
	httpClient     *http.Client
	sessions       sync.Map // sessionID -> *session
	sessionID      atomic.Uint64
	sseConnections atomic.Int32 // track active SSE connections for metrics
}

type session struct {
	id          uint64
	initialized bool
	writer      http.ResponseWriter
	flusher     http.Flusher
	done        chan struct{}
	mu          sync.Mutex
}

// NewHandler creates a new MCP handler.
func NewHandler(logger *zap.SugaredLogger, watcher *k8s.AgentWatcher) *Handler {
	return &Handler{
		logger:  logger,
		watcher: watcher,
		httpClient: &http.Client{
			Timeout: 5 * time.Minute,
		},
	}
}

// HandleSSE handles the SSE connection endpoint (GET /mcp/sse).
func (h *Handler) HandleSSE(w http.ResponseWriter, r *http.Request) {
	// Check for SSE support
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "SSE not supported", http.StatusInternalServerError)
		return
	}

	// Create session
	sessionID := h.sessionID.Add(1)
	sess := &session{
		id:      sessionID,
		writer:  w,
		flusher: flusher,
		done:    make(chan struct{}),
	}
	h.sessions.Store(sessionID, sess)

	// Track active SSE connections
	activeCount := h.sseConnections.Add(1)
	metrics.SetMCPConnectionsActive("sse", int(activeCount))
	defer func() {
		activeCount := h.sseConnections.Add(-1)
		metrics.SetMCPConnectionsActive("sse", int(activeCount))
	}()

	h.logger.Infof("MCP SSE session started: %d", sessionID)

	// Set SSE headers
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")

	// Send endpoint event with session ID for message posting
	endpointURL := fmt.Sprintf("/mcp/message?sessionId=%d", sessionID)
	h.sendSSEEvent(sess, "endpoint", endpointURL)

	// Keep connection alive
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	select {
	case <-r.Context().Done():
	case <-sess.done:
	case <-ticker.C:
		// Send ping to keep connection alive
		h.sendSSEEvent(sess, "ping", "")
	}

	h.sessions.Delete(sessionID)
	h.logger.Infof("MCP SSE session ended: %d", sessionID)
}

// HandleMessage handles incoming MCP messages (POST /mcp/message) for SSE transport.
func (h *Handler) HandleMessage(w http.ResponseWriter, r *http.Request) {
	start := time.Now()

	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Get session ID
	sessionIDStr := r.URL.Query().Get("sessionId")
	if sessionIDStr == "" {
		http.Error(w, "Missing sessionId", http.StatusBadRequest)
		return
	}

	var sessionID uint64
	if _, err := fmt.Sscanf(sessionIDStr, "%d", &sessionID); err != nil {
		http.Error(w, "Invalid sessionId", http.StatusBadRequest)
		return
	}

	sessVal, ok := h.sessions.Load(sessionID)
	if !ok {
		http.Error(w, "Session not found", http.StatusNotFound)
		return
	}
	sess := sessVal.(*session)

	// Parse request
	var req Request
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.sendError(sess, nil, ErrCodeParse, "Parse error", err.Error())
		w.WriteHeader(http.StatusAccepted)
		return
	}

	h.logger.Debugf("MCP request: method=%s id=%v", req.Method, req.ID)

	// Record MCP request metrics
	defer func() {
		metrics.RecordMCPRequest(req.Method, "sse", time.Since(start).Seconds())
	}()

	// Handle request
	switch req.Method {
	case "initialize":
		h.handleInitialize(sess, &req)
	case "initialized":
		// Notification, no response needed
		sess.initialized = true
	case "tools/list":
		metrics.RecordMCPToolsList()
		h.handleListTools(sess, &req)
	case "tools/call":
		h.handleCallTool(r.Context(), sess, &req)
	case "ping":
		h.sendResult(sess, req.ID, map[string]interface{}{})
	default:
		h.sendError(sess, req.ID, ErrCodeMethodNotFound, "Method not found", req.Method)
	}

	w.WriteHeader(http.StatusAccepted)
}

// HandleHTTP handles MCP requests via HTTP transport (POST /mcp).
// This is the recommended transport - each request gets a direct JSON-RPC response.
func (h *Handler) HandleHTTP(w http.ResponseWriter, r *http.Request) {
	start := time.Now()

	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Parse request
	var req Request
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.writeHTTPError(w, nil, ErrCodeParse, "Parse error", err.Error())
		return
	}

	h.logger.Debugf("MCP HTTP request: method=%s id=%v", req.Method, req.ID)

	// Record MCP request metrics
	defer func() {
		metrics.RecordMCPRequest(req.Method, "http", time.Since(start).Seconds())
	}()

	// Handle request and write response directly
	var resp Response
	resp.JSONRPC = "2.0"
	resp.ID = req.ID

	switch req.Method {
	case "initialize":
		resp.Result = InitializeResult{
			ProtocolVersion: protocolVersion,
			Capabilities: Capabilities{
				Tools: &ToolsCapability{
					ListChanged: true,
				},
			},
			ServerInfo: Implementation{
				Name:    serverName,
				Version: serverVersion,
			},
		}
	case "initialized":
		// Notification, just acknowledge
		resp.Result = map[string]interface{}{}
	case "tools/list":
		metrics.RecordMCPToolsList()
		resp.Result = h.buildToolsList()
	case "tools/call":
		result, err := h.handleCallToolHTTP(r.Context(), &req)
		if err != nil {
			resp.Error = &Error{Code: ErrCodeInternal, Message: err.Error()}
		} else {
			resp.Result = result
		}
	case "ping":
		resp.Result = map[string]interface{}{}
	default:
		resp.Error = &Error{Code: ErrCodeMethodNotFound, Message: "Method not found", Data: req.Method}
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(resp)
}

func (h *Handler) buildToolsList() ListToolsResult {
	agents := h.watcher.ListReady()

	var tools []Tool
	for _, agent := range agents {
		agentTools := agent.Status.AvailableTools
		if len(agentTools) == 0 {
			agentTools = agent.Spec.Tools
		}

		if len(agentTools) > 0 {
			for _, t := range agentTools {
				inputSchema := t.InputSchema
				if inputSchema == nil {
					inputSchema = defaultInputSchema()
				}
				tools = append(tools, Tool{
					Name:        fmt.Sprintf("%s_%s", agent.Name, t.Name),
					Description: t.Description,
					InputSchema: inputSchema,
				})
			}
		} else {
			tools = append(tools, Tool{
				Name:        agent.Name,
				Description: extractDescription(agent.Spec.Prompt),
				InputSchema: defaultInputSchema(),
			})
		}
	}

	return ListToolsResult{Tools: tools}
}

func (h *Handler) handleCallToolHTTP(ctx context.Context, req *Request) (*CallToolResult, error) {
	paramsJSON, err := json.Marshal(req.Params)
	if err != nil {
		return nil, fmt.Errorf("invalid params: %w", err)
	}

	var params CallToolParams
	if err := json.Unmarshal(paramsJSON, &params); err != nil {
		return nil, fmt.Errorf("invalid params: %w", err)
	}

	h.logger.Debugf("[MCP] Tool call: %s with args: %v", params.Name, params.Arguments)

	// Extract agent name from tool name
	agentName := params.Name
	toolName := ""
	if idx := strings.Index(params.Name, "_"); idx > 0 {
		agentName = params.Name[:idx]
		toolName = params.Name[idx+1:]
	}

	// Record tool call metric
	metrics.RecordMCPToolsCall(agentName, toolName)

	h.logger.Debugf("[MCP] Resolved agent=%s tool=%s", agentName, toolName)

	agent, found := h.watcher.GetByName(agentName)
	if !found {
		h.logger.Warnf("[MCP] Agent not found: %s", agentName)
		return nil, fmt.Errorf("agent not found: %s", agentName)
	}

	if !agent.Status.Ready {
		h.logger.Warnf("[MCP] Agent not ready: %s", agentName)
		return nil, fmt.Errorf("agent not ready: %s", agentName)
	}

	h.logger.Debugf("[MCP] Agent %s is ready, endpoint=%s", agentName, agent.Status.Endpoint)

	// Build query from arguments
	query := ""
	for _, key := range []string{"query", "question", "request", "description"} {
		if q, ok := params.Arguments[key].(string); ok && q != "" {
			query = q
			break
		}
	}

	if query == "" {
		parts := make([]string, 0)
		for k, v := range params.Arguments {
			if s, ok := v.(string); ok && s != "" {
				parts = append(parts, fmt.Sprintf("%s: %s", k, s))
			}
		}
		query = strings.Join(parts, "\n")
	}

	h.logger.Debugf("[MCP] Forwarding to agent %s: query=%q", agentName, truncate(query, 100))

	result, err := h.forwardToAgent(ctx, agent, query, params.Arguments)
	if err != nil {
		h.logger.Errorf("[MCP] Error from agent %s: %v", agentName, err)
		return &CallToolResult{
			Content: []Content{{Type: "text", Text: fmt.Sprintf("Error: %v", err)}},
			IsError: true,
		}, nil
	}

	h.logger.Debugf("[MCP] Success from agent %s: response=%q", agentName, truncate(result, 200))

	return &CallToolResult{
		Content: []Content{{Type: "text", Text: result}},
	}, nil
}

func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}

func (h *Handler) writeHTTPError(w http.ResponseWriter, id interface{}, code int, message, data string) {
	resp := Response{
		JSONRPC: "2.0",
		ID:      id,
		Error: &Error{
			Code:    code,
			Message: message,
			Data:    data,
		},
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(resp)
}

func (h *Handler) handleInitialize(sess *session, req *Request) {
	result := InitializeResult{
		ProtocolVersion: protocolVersion,
		Capabilities: Capabilities{
			Tools: &ToolsCapability{
				ListChanged: true,
			},
		},
		ServerInfo: Implementation{
			Name:    serverName,
			Version: serverVersion,
		},
	}
	h.sendResult(sess, req.ID, result)
}

func (h *Handler) handleListTools(sess *session, req *Request) {
	agents := h.watcher.ListReady()

	var tools []Tool
	for _, agent := range agents {
		// Use available tools from status if present, otherwise generate from spec
		agentTools := agent.Status.AvailableTools
		if len(agentTools) == 0 {
			agentTools = agent.Spec.Tools
		}

		if len(agentTools) > 0 {
			// Agent has explicit tools defined
			for _, t := range agentTools {
				inputSchema := t.InputSchema
				if inputSchema == nil {
					inputSchema = defaultInputSchema()
				}
				tools = append(tools, Tool{
					Name:        fmt.Sprintf("%s_%s", agent.Name, t.Name),
					Description: t.Description,
					InputSchema: inputSchema,
				})
			}
		} else {
			// Generate default tool from agent name + prompt
			tools = append(tools, Tool{
				Name:        agent.Name,
				Description: extractDescription(agent.Spec.Prompt),
				InputSchema: defaultInputSchema(),
			})
		}
	}

	h.sendResult(sess, req.ID, ListToolsResult{Tools: tools})
}

func (h *Handler) handleCallTool(ctx context.Context, sess *session, req *Request) {
	// Parse params
	paramsJSON, err := json.Marshal(req.Params)
	if err != nil {
		h.sendError(sess, req.ID, ErrCodeInvalidParams, "Invalid params", err.Error())
		return
	}

	var params CallToolParams
	if err := json.Unmarshal(paramsJSON, &params); err != nil {
		h.sendError(sess, req.ID, ErrCodeInvalidParams, "Invalid params", err.Error())
		return
	}

	// Extract agent name from tool name (format: agentname_toolname or just agentname)
	agentName := params.Name
	toolName := ""
	if idx := strings.Index(params.Name, "_"); idx > 0 {
		agentName = params.Name[:idx]
		toolName = params.Name[idx+1:]
	}

	// Record tool call metric
	metrics.RecordMCPToolsCall(agentName, toolName)

	// Find agent
	agent, found := h.watcher.GetByName(agentName)
	if !found {
		h.sendError(sess, req.ID, ErrCodeInvalidParams, "Agent not found", agentName)
		return
	}

	if !agent.Status.Ready {
		h.sendError(sess, req.ID, ErrCodeInternal, "Agent not ready", agentName)
		return
	}

	// Build query from arguments
	query := ""
	if q, ok := params.Arguments["query"].(string); ok {
		query = q
	} else if q, ok := params.Arguments["question"].(string); ok {
		query = q
	} else if q, ok := params.Arguments["request"].(string); ok {
		query = q
	} else if q, ok := params.Arguments["description"].(string); ok {
		query = q
	}

	if query == "" {
		// Try to construct query from all arguments
		parts := make([]string, 0)
		for k, v := range params.Arguments {
			if s, ok := v.(string); ok && s != "" {
				parts = append(parts, fmt.Sprintf("%s: %s", k, s))
			}
		}
		query = strings.Join(parts, "\n")
	}

	// Forward to agent
	result, err := h.forwardToAgent(ctx, agent, query, params.Arguments)
	if err != nil {
		h.sendResult(sess, req.ID, CallToolResult{
			Content: []Content{{Type: "text", Text: fmt.Sprintf("Error: %v", err)}},
			IsError: true,
		})
		return
	}

	h.sendResult(sess, req.ID, CallToolResult{
		Content: []Content{{Type: "text", Text: result}},
	})
}

func (h *Handler) forwardToAgent(ctx context.Context, agent *k8s.Agent, query string, args map[string]interface{}) (string, error) {
	// Build request to agent
	agentReq := map[string]interface{}{
		"query":    query,
		"input":    args,
		"metadata": map[string]interface{}{"source": "mcp"},
	}

	body, err := json.Marshal(agentReq)
	if err != nil {
		return "", err
	}

	// Create HTTP request - ensure FQDN format to avoid DNS search domain issues
	endpoint := agent.Status.Endpoint
	if strings.Contains(endpoint, ".svc.cluster.local") && !strings.HasSuffix(strings.Split(endpoint, ":")[0], ".") {
		parts := strings.SplitN(endpoint, ":", 2)
		if len(parts) == 2 {
			endpoint = parts[0] + ".:" + parts[1]
		}
	}
	url := fmt.Sprintf("http://%s/invoke", endpoint)
	h.logger.Debugf("[AGENT] >> POST %s", url)
	h.logger.Debugf("[AGENT] >> Body: %s", truncate(string(body), 500))

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	httpReq.Header.Set("Content-Type", "application/json")

	// Execute
	startTime := time.Now()
	resp, err := h.httpClient.Do(httpReq)
	if err != nil {
		h.logger.Errorf("[AGENT] << Error after %v: %v", time.Since(startTime), err)
		return "", err
	}
	defer func() { _ = resp.Body.Close() }()

	// Read response
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}

	h.logger.Debugf("[AGENT] << %d after %v", resp.StatusCode, time.Since(startTime))
	h.logger.Debugf("[AGENT] << Body: %s", truncate(string(respBody), 500))

	if resp.StatusCode >= 400 {
		return "", fmt.Errorf("agent returned %d: %s", resp.StatusCode, string(respBody))
	}

	// Try to extract result from JSON response
	var result map[string]interface{}
	if err := json.Unmarshal(respBody, &result); err == nil {
		// Check for common result field names
		if r, ok := result["result"]; ok {
			if s, ok := r.(string); ok {
				return s, nil
			}
			// Marshal back to JSON
			resultJSON, _ := json.MarshalIndent(r, "", "  ")
			return string(resultJSON), nil
		}
		if r, ok := result["response"]; ok {
			if s, ok := r.(string); ok {
				return s, nil
			}
		}
		if r, ok := result["output"]; ok {
			if s, ok := r.(string); ok {
				return s, nil
			}
		}
		// Return entire response as JSON
		return string(respBody), nil
	}

	return string(respBody), nil
}

func (h *Handler) sendResult(sess *session, id interface{}, result interface{}) {
	resp := Response{
		JSONRPC: "2.0",
		ID:      id,
		Result:  result,
	}
	h.sendSSEMessage(sess, resp)
}

func (h *Handler) sendError(sess *session, id interface{}, code int, message, data string) {
	resp := Response{
		JSONRPC: "2.0",
		ID:      id,
		Error: &Error{
			Code:    code,
			Message: message,
			Data:    data,
		},
	}
	h.sendSSEMessage(sess, resp)
}

func (h *Handler) sendSSEMessage(sess *session, data interface{}) {
	jsonData, err := json.Marshal(data)
	if err != nil {
		h.logger.Errorf("Failed to marshal SSE message: %v", err)
		return
	}
	h.sendSSEEvent(sess, "message", string(jsonData))
}

func (h *Handler) sendSSEEvent(sess *session, event, data string) {
	sess.mu.Lock()
	defer sess.mu.Unlock()

	// Write event type
	_, _ = fmt.Fprintf(sess.writer, "event: %s\n", event)

	// Write data (handle multi-line)
	if data != "" {
		scanner := bufio.NewScanner(strings.NewReader(data))
		for scanner.Scan() {
			_, _ = fmt.Fprintf(sess.writer, "data: %s\n", scanner.Text())
		}
	} else {
		_, _ = fmt.Fprint(sess.writer, "data: \n")
	}

	// End event
	_, _ = fmt.Fprint(sess.writer, "\n")
	sess.flusher.Flush()
}

// NotifyToolsListChanged sends a notification that the tools list has changed.
func (h *Handler) NotifyToolsListChanged() {
	h.sessions.Range(func(key, value interface{}) bool {
		sess := value.(*session)
		if sess.initialized {
			notification := Notification{
				JSONRPC: "2.0",
				Method:  "notifications/tools/list_changed",
			}
			h.sendSSEMessage(sess, notification)
		}
		return true
	})
}

func extractDescription(prompt string) string {
	// Extract first sentence or first 200 chars
	prompt = strings.TrimSpace(prompt)
	if len(prompt) == 0 {
		return "AI agent"
	}

	// Find first sentence
	if idx := strings.Index(prompt, "."); idx > 0 && idx < 200 {
		return prompt[:idx+1]
	}

	// Truncate to 200 chars
	if len(prompt) > 200 {
		return prompt[:197] + "..."
	}
	return prompt
}

func defaultInputSchema() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"query": map[string]interface{}{
				"type":        "string",
				"description": "The query or task for the agent",
			},
		},
		"required": []string{"query"},
	}
}
