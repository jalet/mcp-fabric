// Package metrics provides Prometheus metrics for the MCP Fabric operator.
package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
	"sigs.k8s.io/controller-runtime/pkg/metrics"
)

const (
	// Namespace prefix for all metrics
	namespace = "mcpfabric"

	// Controller names
	ControllerAgent = "agent"
	ControllerTool  = "tool"
	ControllerRoute = "route"

	// Result labels
	ResultSuccess = "success"
	ResultError   = "error"
	ResultRequeue = "requeue"
)

var (
	// DurationBuckets for request/reconciliation durations
	DurationBuckets = []float64{0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10}

	// ReconcileTotal counts total reconciliations per controller
	ReconcileTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: namespace,
			Name:      "reconcile_total",
			Help:      "Total number of reconciliations per controller and result",
		},
		[]string{"controller", "result"},
	)

	// ReconcileDuration measures reconciliation duration
	ReconcileDuration = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Namespace: namespace,
			Name:      "reconcile_duration_seconds",
			Help:      "Duration of reconciliation in seconds",
			Buckets:   DurationBuckets,
		},
		[]string{"controller"},
	)

	// ReconcileErrors counts errors by type
	ReconcileErrors = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: namespace,
			Name:      "reconcile_errors_total",
			Help:      "Total reconciliation errors by controller and error type",
		},
		[]string{"controller", "error_type"},
	)

	// AgentInfo provides agent metadata (always 1)
	AgentInfo = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Namespace: namespace,
			Name:      "agent_info",
			Help:      "Agent metadata information (value is always 1)",
		},
		[]string{"name", "namespace", "model_id", "image"},
	)

	// AgentReady indicates if agent is ready (0 or 1)
	AgentReady = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Namespace: namespace,
			Name:      "agent_ready",
			Help:      "Whether the agent is ready (1) or not (0)",
		},
		[]string{"name", "namespace"},
	)

	// AgentReplicas shows desired replicas
	AgentReplicas = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Namespace: namespace,
			Name:      "agent_replicas",
			Help:      "Desired number of agent replicas",
		},
		[]string{"name", "namespace"},
	)

	// AgentReplicasAvailable shows available replicas
	AgentReplicasAvailable = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Namespace: namespace,
			Name:      "agent_replicas_available",
			Help:      "Number of available agent replicas",
		},
		[]string{"name", "namespace"},
	)

	// AgentToolsCount shows number of tools available
	AgentToolsCount = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Namespace: namespace,
			Name:      "agent_tools_count",
			Help:      "Number of tools available to the agent",
		},
		[]string{"name", "namespace"},
	)

	// ToolReady indicates if Tool is ready
	ToolReady = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Namespace: namespace,
			Name:      "tool_ready",
			Help:      "Whether the Tool is ready (1) or not (0)",
		},
		[]string{"name", "namespace"},
	)

	// ToolDefinitionsCount shows tools in package
	ToolDefinitionsCount = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Namespace: namespace,
			Name:      "tool_definitions_count",
			Help:      "Number of tool definitions in the Tool",
		},
		[]string{"name", "namespace"},
	)

	// RouteRulesCount shows number of routing rules
	RouteRulesCount = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Namespace: namespace,
			Name:      "route_rules_count",
			Help:      "Number of routing rules in the Route",
		},
		[]string{"name", "namespace"},
	)

	// RouteBackendsReady shows ready backends
	RouteBackendsReady = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Namespace: namespace,
			Name:      "route_backends_ready",
			Help:      "Number of ready backends in the Route",
		},
		[]string{"name", "namespace"},
	)
)

func init() {
	// Register all metrics with controller-runtime's global registry
	metrics.Registry.MustRegister(
		ReconcileTotal,
		ReconcileDuration,
		ReconcileErrors,
		AgentInfo,
		AgentReady,
		AgentReplicas,
		AgentReplicasAvailable,
		AgentToolsCount,
		ToolReady,
		ToolDefinitionsCount,
		RouteRulesCount,
		RouteBackendsReady,
	)
}

// RecordReconcile records a reconciliation attempt
func RecordReconcile(controller, result string, duration float64) {
	ReconcileTotal.WithLabelValues(controller, result).Inc()
	ReconcileDuration.WithLabelValues(controller).Observe(duration)
}

// RecordReconcileError records a reconciliation error
func RecordReconcileError(controller, errorType string) {
	ReconcileErrors.WithLabelValues(controller, errorType).Inc()
}

// SetAgentMetrics updates all agent-related metrics
func SetAgentMetrics(name, namespace, modelID, image string, ready bool, replicas, availableReplicas, toolsCount int) {
	// Set info metric
	AgentInfo.WithLabelValues(name, namespace, modelID, image).Set(1)

	// Set ready state
	readyVal := float64(0)
	if ready {
		readyVal = 1
	}
	AgentReady.WithLabelValues(name, namespace).Set(readyVal)

	// Set replica counts
	AgentReplicas.WithLabelValues(name, namespace).Set(float64(replicas))
	AgentReplicasAvailable.WithLabelValues(name, namespace).Set(float64(availableReplicas))

	// Set tools count
	AgentToolsCount.WithLabelValues(name, namespace).Set(float64(toolsCount))
}

// DeleteAgentMetrics removes metrics for a deleted agent
func DeleteAgentMetrics(name, namespace string) {
	AgentReady.DeleteLabelValues(name, namespace)
	AgentReplicas.DeleteLabelValues(name, namespace)
	AgentReplicasAvailable.DeleteLabelValues(name, namespace)
	AgentToolsCount.DeleteLabelValues(name, namespace)
	// Note: AgentInfo has more labels, so we can't easily delete it
	// It will be overwritten on next reconcile or stale after deletion
}

// SetToolMetrics updates Tool metrics
func SetToolMetrics(name, namespace string, ready bool, toolsCount int) {
	readyVal := float64(0)
	if ready {
		readyVal = 1
	}
	ToolReady.WithLabelValues(name, namespace).Set(readyVal)
	ToolDefinitionsCount.WithLabelValues(name, namespace).Set(float64(toolsCount))
}

// DeleteToolMetrics removes metrics for a deleted Tool
func DeleteToolMetrics(name, namespace string) {
	ToolReady.DeleteLabelValues(name, namespace)
	ToolDefinitionsCount.DeleteLabelValues(name, namespace)
}

// SetRouteMetrics updates Route metrics
func SetRouteMetrics(name, namespace string, rulesCount, backendsReady int) {
	RouteRulesCount.WithLabelValues(name, namespace).Set(float64(rulesCount))
	RouteBackendsReady.WithLabelValues(name, namespace).Set(float64(backendsReady))
}

// DeleteRouteMetrics removes metrics for a deleted Route
func DeleteRouteMetrics(name, namespace string) {
	RouteRulesCount.DeleteLabelValues(name, namespace)
	RouteBackendsReady.DeleteLabelValues(name, namespace)
}
