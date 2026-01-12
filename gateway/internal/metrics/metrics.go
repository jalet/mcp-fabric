// Package metrics provides Prometheus metrics for the MCP Fabric gateway.
package metrics

import (
	"net/http"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

const (
	// Namespace prefix for all metrics
	namespace = "mcpfabric"

	// Subsystems
	subsystemGateway = "gateway"
	subsystemCircuit = "circuit_breaker"
	subsystemMCP     = "mcp"
)

var (
	// DurationBuckets for request durations
	DurationBuckets = []float64{0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10, 30, 60}

	// === Gateway HTTP Metrics ===

	// GatewayRequestsTotal counts total requests
	GatewayRequestsTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: namespace,
			Subsystem: subsystemGateway,
			Name:      "requests_total",
			Help:      "Total number of gateway requests",
		},
		[]string{"agent", "route", "status_code"},
	)

	// GatewayRequestDuration measures request latency
	GatewayRequestDuration = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Namespace: namespace,
			Subsystem: subsystemGateway,
			Name:      "request_duration_seconds",
			Help:      "Request latency in seconds",
			Buckets:   DurationBuckets,
		},
		[]string{"agent", "route"},
	)

	// GatewayRequestErrors counts request errors
	GatewayRequestErrors = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: namespace,
			Subsystem: subsystemGateway,
			Name:      "request_errors_total",
			Help:      "Total number of request errors",
		},
		[]string{"agent", "route", "error_type"},
	)

	// GatewayRouteMatches counts route matches
	GatewayRouteMatches = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: namespace,
			Subsystem: subsystemGateway,
			Name:      "route_matches_total",
			Help:      "Total number of route matches",
		},
		[]string{"route", "rule"},
	)

	// GatewayRouteNoMatch counts unmatched requests
	GatewayRouteNoMatch = prometheus.NewCounter(
		prometheus.CounterOpts{
			Namespace: namespace,
			Subsystem: subsystemGateway,
			Name:      "route_no_match_total",
			Help:      "Total number of requests with no route match",
		},
	)

	// GatewayBackendForwards counts forwards to agents
	GatewayBackendForwards = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: namespace,
			Subsystem: subsystemGateway,
			Name:      "backend_forwards_total",
			Help:      "Total number of forwards to backend agents",
		},
		[]string{"agent", "namespace"},
	)

	// === Circuit Breaker Metrics ===

	// CircuitBreakerActive shows active requests
	CircuitBreakerActive = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Namespace: namespace,
			Subsystem: subsystemCircuit,
			Name:      "active",
			Help:      "Number of active requests in the circuit breaker",
		},
		[]string{"route"},
	)

	// CircuitBreakerWaiting shows queued requests
	CircuitBreakerWaiting = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Namespace: namespace,
			Subsystem: subsystemCircuit,
			Name:      "waiting",
			Help:      "Number of requests waiting in the queue",
		},
		[]string{"route"},
	)

	// CircuitBreakerRejections counts rejections
	CircuitBreakerRejections = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: namespace,
			Subsystem: subsystemCircuit,
			Name:      "rejections_total",
			Help:      "Total number of circuit breaker rejections",
		},
		[]string{"route", "reason"},
	)

	// CircuitBreakerState shows circuit state (0=closed, 1=open)
	CircuitBreakerState = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Namespace: namespace,
			Subsystem: subsystemCircuit,
			Name:      "state",
			Help:      "Circuit breaker state (0=closed, 1=open)",
		},
		[]string{"route"},
	)

	// === MCP Protocol Metrics ===

	// MCPConnectionsActive shows active MCP connections
	MCPConnectionsActive = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Namespace: namespace,
			Subsystem: subsystemMCP,
			Name:      "connections_active",
			Help:      "Number of active MCP connections",
		},
		[]string{"transport"},
	)

	// MCPRequestsTotal counts MCP requests
	MCPRequestsTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: namespace,
			Subsystem: subsystemMCP,
			Name:      "requests_total",
			Help:      "Total number of MCP requests",
		},
		[]string{"method", "transport"},
	)

	// MCPRequestDuration measures MCP request latency
	MCPRequestDuration = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Namespace: namespace,
			Subsystem: subsystemMCP,
			Name:      "request_duration_seconds",
			Help:      "MCP request latency in seconds",
			Buckets:   DurationBuckets,
		},
		[]string{"method"},
	)

	// MCPToolsListTotal counts tools/list invocations
	MCPToolsListTotal = prometheus.NewCounter(
		prometheus.CounterOpts{
			Namespace: namespace,
			Subsystem: subsystemMCP,
			Name:      "tools_list_total",
			Help:      "Total number of tools/list invocations",
		},
	)

	// MCPToolsCallTotal counts tools/call invocations
	MCPToolsCallTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: namespace,
			Subsystem: subsystemMCP,
			Name:      "tools_call_total",
			Help:      "Total number of tools/call invocations",
		},
		[]string{"agent", "tool"},
	)

	// registry holds all metrics
	registry = prometheus.NewRegistry()
)

func init() {
	// Register all metrics
	registry.MustRegister(
		// Gateway metrics
		GatewayRequestsTotal,
		GatewayRequestDuration,
		GatewayRequestErrors,
		GatewayRouteMatches,
		GatewayRouteNoMatch,
		GatewayBackendForwards,
		// Circuit breaker metrics
		CircuitBreakerActive,
		CircuitBreakerWaiting,
		CircuitBreakerRejections,
		CircuitBreakerState,
		// MCP metrics
		MCPConnectionsActive,
		MCPRequestsTotal,
		MCPRequestDuration,
		MCPToolsListTotal,
		MCPToolsCallTotal,
	)

	// Also register Go runtime and process collectors
	registry.MustRegister(collectors.NewGoCollector())
	registry.MustRegister(collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}))
}

// Handler returns an HTTP handler for metrics
func Handler() http.Handler {
	return promhttp.HandlerFor(registry, promhttp.HandlerOpts{
		EnableOpenMetrics: true,
	})
}

// RecordRequest records a gateway request
func RecordRequest(agent, route, statusCode string, duration float64) {
	GatewayRequestsTotal.WithLabelValues(agent, route, statusCode).Inc()
	GatewayRequestDuration.WithLabelValues(agent, route).Observe(duration)
}

// RecordRequestError records a request error
func RecordRequestError(agent, route, errorType string) {
	GatewayRequestErrors.WithLabelValues(agent, route, errorType).Inc()
}

// RecordRouteMatch records a route match
func RecordRouteMatch(route, rule string) {
	GatewayRouteMatches.WithLabelValues(route, rule).Inc()
}

// RecordRouteNoMatch records an unmatched request
func RecordRouteNoMatch() {
	GatewayRouteNoMatch.Inc()
}

// RecordBackendForward records a forward to a backend agent
func RecordBackendForward(agent, namespace string) {
	GatewayBackendForwards.WithLabelValues(agent, namespace).Inc()
}

// SetCircuitBreakerActive sets the active count for a circuit breaker
func SetCircuitBreakerActive(route string, count int) {
	CircuitBreakerActive.WithLabelValues(route).Set(float64(count))
}

// SetCircuitBreakerWaiting sets the waiting count for a circuit breaker
func SetCircuitBreakerWaiting(route string, count int) {
	CircuitBreakerWaiting.WithLabelValues(route).Set(float64(count))
}

// RecordCircuitBreakerRejection records a circuit breaker rejection
func RecordCircuitBreakerRejection(route, reason string) {
	CircuitBreakerRejections.WithLabelValues(route, reason).Inc()
}

// SetCircuitBreakerState sets the circuit breaker state (0=closed, 1=open)
func SetCircuitBreakerState(route string, open bool) {
	val := 0.0
	if open {
		val = 1.0
	}
	CircuitBreakerState.WithLabelValues(route).Set(val)
}

// SetMCPConnectionsActive sets the active MCP connection count
func SetMCPConnectionsActive(transport string, count int) {
	MCPConnectionsActive.WithLabelValues(transport).Set(float64(count))
}

// RecordMCPRequest records an MCP request
func RecordMCPRequest(method, transport string, duration float64) {
	MCPRequestsTotal.WithLabelValues(method, transport).Inc()
	MCPRequestDuration.WithLabelValues(method).Observe(duration)
}

// RecordMCPToolsList records a tools/list invocation
func RecordMCPToolsList() {
	MCPToolsListTotal.Inc()
}

// RecordMCPToolsCall records a tools/call invocation
func RecordMCPToolsCall(agent, tool string) {
	MCPToolsCallTotal.WithLabelValues(agent, tool).Inc()
}
