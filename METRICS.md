# Metrics Reference

MCP Fabric exposes Prometheus metrics from both the operator and gateway components, with pre-built Grafana dashboards for visualization.

## Overview

| Component | Metrics Port | Metrics Path |
|-----------|--------------|--------------|
| Operator | `:8080` | `/metrics` |
| Gateway | `:9090` | `/metrics` |
| Agents | `:9090` | `/metrics` |

## Quick Start

### Deploy Prometheus Stack

```bash
# Deploy kube-prometheus-stack with Kustomize
kubectl apply -k examples/deploy/monitoring/prometheus/

# If CRDs aren't ready yet, run again
kubectl apply -k examples/deploy/monitoring/prometheus/
```

See [examples/deploy/monitoring/prometheus/README.md](examples/deploy/monitoring/prometheus/README.md) for detailed installation instructions.

### Access Grafana

```bash
# Port forward to Grafana
kubectl port-forward -n monitoring svc/prometheus-grafana 3000:80

# Open http://localhost:3000
# Default credentials: admin / mcp-admin
```

### Access Prometheus

```bash
kubectl port-forward -n monitoring svc/prometheus-kube-prometheus-prometheus 9090:9090
```

## Metrics Reference

### Operator Metrics

The operator exposes metrics about CRD reconciliation and resource status.

#### Reconciliation Metrics

| Metric | Type | Labels | Description |
|--------|------|--------|-------------|
| `mcpfabric_reconcile_total` | Counter | `controller`, `result` | Total reconciliations per controller |
| `mcpfabric_reconcile_duration_seconds` | Histogram | `controller`, `result` | Reconciliation duration |
| `mcpfabric_reconcile_errors_total` | Counter | `controller`, `error_type` | Reconciliation errors by type |

**Controllers:** `agent`, `tool`, `route`, `task`

**Results:** `success`, `error`, `requeue`

#### Agent Metrics

| Metric | Type | Labels | Description |
|--------|------|--------|-------------|
| `mcpfabric_agent_info` | Gauge | `name`, `namespace`, `model_id`, `image` | Agent metadata (always 1) |
| `mcpfabric_agent_ready` | Gauge | `name`, `namespace` | Agent ready state (0/1) |
| `mcpfabric_agent_replicas` | Gauge | `name`, `namespace` | Desired replica count |
| `mcpfabric_agent_replicas_available` | Gauge | `name`, `namespace` | Available replica count |
| `mcpfabric_agent_tools_count` | Gauge | `name`, `namespace` | Tools available to agent |

#### Tool Metrics

| Metric | Type | Labels | Description |
|--------|------|--------|-------------|
| `mcpfabric_tool_ready` | Gauge | `name`, `namespace` | Tool ready state (0/1) |
| `mcpfabric_tool_definitions_count` | Gauge | `name`, `namespace` | Number of tool definitions |

#### Route Metrics

| Metric | Type | Labels | Description |
|--------|------|--------|-------------|
| `mcpfabric_route_rules_count` | Gauge | `name`, `namespace` | Number of routing rules |
| `mcpfabric_route_backends_ready` | Gauge | `name`, `namespace` | Ready backend count |

### Gateway Metrics

The gateway exposes HTTP and MCP protocol metrics.

#### HTTP Gateway Metrics

| Metric | Type | Labels | Description |
|--------|------|--------|-------------|
| `mcpfabric_gateway_requests_total` | Counter | `agent`, `route`, `status_code` | Total HTTP requests |
| `mcpfabric_gateway_request_duration_seconds` | Histogram | `agent`, `route` | Request latency |
| `mcpfabric_gateway_request_errors_total` | Counter | `agent`, `route`, `error_type` | Request errors |
| `mcpfabric_gateway_route_matches_total` | Counter | `route`, `rule` | Route match counts |
| `mcpfabric_gateway_route_no_match_total` | Counter | - | Unmatched requests |
| `mcpfabric_gateway_backend_forwards_total` | Counter | `agent`, `namespace` | Forwards to backends |

#### Circuit Breaker Metrics

| Metric | Type | Labels | Description |
|--------|------|--------|-------------|
| `mcpfabric_circuit_breaker_active` | Gauge | `route` | Active requests |
| `mcpfabric_circuit_breaker_waiting` | Gauge | `route` | Queued requests |
| `mcpfabric_circuit_breaker_rejections_total` | Counter | `route`, `reason` | Rejection count |
| `mcpfabric_circuit_breaker_state` | Gauge | `route` | State (0=closed, 1=open) |

#### MCP Protocol Metrics

| Metric | Type | Labels | Description |
|--------|------|--------|-------------|
| `mcpfabric_mcp_connections_active` | Gauge | `transport` | Active MCP connections |
| `mcpfabric_mcp_requests_total` | Counter | `method`, `transport` | MCP requests by method |
| `mcpfabric_mcp_request_duration_seconds` | Histogram | `method` | MCP request latency |
| `mcpfabric_mcp_tools_list_total` | Counter | - | tools/list invocations |
| `mcpfabric_mcp_tools_call_total` | Counter | `agent`, `tool` | tools/call invocations |

### Agent Pod Metrics

Agent pods expose their own metrics that are scraped via PodMonitor.

| Metric | Type | Labels | Description |
|--------|------|--------|-------------|
| `mcpfabric_agent_requests_total` | Counter | - | Agent invocation requests |
| `mcpfabric_agent_request_duration_seconds` | Histogram | - | Request duration |
| `mcpfabric_genai_requests_total` | Counter | - | LLM invocations |
| `mcpfabric_genai_request_duration_seconds` | Histogram | - | LLM latency |
| `mcpfabric_genai_errors_total` | Counter | - | LLM errors |

#### Agent Metric Labels (via PodMonitor relabeling)

Agent metrics are automatically enriched with labels from pod metadata:

| Label | Source | Description |
|-------|--------|-------------|
| `agent` | `fabric.jarsater.ai/agent` | Agent name |
| `provider` | `fabric.jarsater.ai/provider` | Model provider (e.g., `bedrock`, `openai`) |
| `model_id` | `fabric.jarsater.ai/model-id` | Model identifier (sanitized for label compatibility) |
| `prompt_hash` | `fabric.jarsater.ai/prompt-hash` | SHA256 hash of agent prompt (first 16 chars) |

## Grafana Dashboards

Three pre-built dashboards are included:

### MCP Fabric Overview

**UID:** `mcp-fabric-overview`

Overview dashboard with key metrics:
- Agents ready count
- MCP request rate
- Tool calls rate
- Route misses
- Tool packages ready
- Operator reconciliation panels
- Agent replica status

### MCP Fabric Gateway & Protocol

**UID:** `mcp-fabric-gateway`

Detailed gateway and MCP protocol metrics:
- Gateway request rate and error rate
- p95 latency
- Route misses rate
- MCP requests by method and transport
- MCP request duration percentiles
- Tool calls by agent and tool
- Request duration distribution histogram

### MCP Fabric Operator

**UID:** `mcp-fabric-operator`

Operator-focused metrics:
- CRD ready states (Agents, Routes, Tools)
- Agent replica tracking
- Reconciliation rate by controller and result
- Reconciliation duration percentiles
- Work queue depth

## Breaking Changes

### Reconciliation metric label changes

`mcpfabric_reconcile_total` and `mcpfabric_reconcile_duration_seconds` now include a `result` label (`success`, `error`, `requeue`) in addition to the existing `controller` label.

**Previous labels:** `["controller"]`
**Current labels:** `["controller", "result"]`

This is a breaking change for PromQL queries and alert rules that use exact label matchers against these metrics. Queries using `by (controller)` aggregation will continue to work, but queries that assumed a single-label series (e.g. bare metric selectors without aggregation) may return unexpected multiple series per controller.

**Migration examples:**

```promql
# BEFORE: total reconciliation rate per controller
sum by (controller) (rate(mcpfabric_reconcile_total[5m]))
# AFTER: same query still works — the additional label is aggregated away

# BEFORE: error rate (not previously possible without result label)
# AFTER: error rate per controller
sum by (controller) (rate(mcpfabric_reconcile_total{result="error"}[5m]))

# BEFORE: reconcile duration p95
histogram_quantile(0.95, sum by (controller, le) (rate(mcpfabric_reconcile_duration_seconds_bucket[5m])))
# AFTER: include result label in aggregation, or aggregate it away
histogram_quantile(0.95, sum by (controller, le) (rate(mcpfabric_reconcile_duration_seconds_bucket[5m])))
# Or filter by result:
histogram_quantile(0.95, sum by (controller, le) (rate(mcpfabric_reconcile_duration_seconds_bucket{result="success"}[5m])))
```

**Action required:** Review any existing Grafana dashboards, alert rules, or recording rules that reference `mcpfabric_reconcile_total` or `mcpfabric_reconcile_duration_seconds` and ensure they handle the additional `result` label dimension.

## PromQL Query Examples

### Request Rate

```promql
# Total gateway request rate
sum(rate(mcpfabric_gateway_requests_total[5m]))

# Request rate by agent
sum by (agent) (rate(mcpfabric_gateway_requests_total[5m]))
```

### Error Rate

```promql
# Gateway error rate percentage
sum(rate(mcpfabric_gateway_request_errors_total[5m]))
/ sum(rate(mcpfabric_gateway_requests_total[5m])) * 100

# Reconciliation error rate
sum(rate(mcpfabric_reconcile_total{result="error"}[5m]))
```

### Latency

```promql
# Gateway p50 latency
histogram_quantile(0.50, sum(rate(mcpfabric_gateway_request_duration_seconds_bucket[5m])) by (le))

# Gateway p95 latency
histogram_quantile(0.95, sum(rate(mcpfabric_gateway_request_duration_seconds_bucket[5m])) by (le))

# MCP request p95 latency by method
histogram_quantile(0.95, sum by (method, le) (rate(mcpfabric_mcp_request_duration_seconds_bucket[5m])))
```

### Tool Usage

```promql
# Tool calls rate by agent and tool
sum by (agent, tool) (rate(mcpfabric_mcp_tools_call_total[5m]))

# Total tool calls in last hour
sum(increase(mcpfabric_mcp_tools_call_total[1h]))
```

### Agent Health

```promql
# Count of ready agents
sum(mcpfabric_agent_ready)

# Agents with unavailable replicas
mcpfabric_agent_replicas - mcpfabric_agent_replicas_available > 0
```

### Model Comparison

```promql
# Request rate by model provider
sum by (provider) (rate(mcpfabric_agent_requests_total[5m]))

# P95 latency comparison across models
histogram_quantile(0.95,
  sum by (model_id) (
    rate(mcpfabric_agent_request_duration_seconds_bucket[5m])
  )
)

# Compare same prompt across different models
histogram_quantile(0.95,
  sum by (model_id, prompt_hash) (
    rate(mcpfabric_agent_request_duration_seconds_bucket[5m])
  )
)

# Error rate by model
sum by (model_id) (rate(mcpfabric_genai_errors_total[5m]))
  /
sum by (model_id) (rate(mcpfabric_genai_requests_total[5m]))
```

## Alert Rules

Example Prometheus alert rules for MCP Fabric:

```yaml
apiVersion: monitoring.coreos.com/v1
kind: PrometheusRule
metadata:
  name: mcp-fabric-alerts
  namespace: mcp-fabric-system
  labels:
    release: prometheus
spec:
  groups:
    - name: mcp-fabric.rules
      rules:
        # High error rate
        - alert: MCPFabricHighErrorRate
          expr: |
            sum(rate(mcpfabric_gateway_request_errors_total[5m]))
            / sum(rate(mcpfabric_gateway_requests_total[5m])) > 0.05
          for: 5m
          labels:
            severity: warning
          annotations:
            summary: "MCP Fabric gateway error rate > 5%"
            description: "Gateway error rate is {{ $value | humanizePercentage }}"

        # High latency
        - alert: MCPFabricHighLatency
          expr: |
            histogram_quantile(0.95,
              sum(rate(mcpfabric_gateway_request_duration_seconds_bucket[5m])) by (le)
            ) > 5
          for: 5m
          labels:
            severity: warning
          annotations:
            summary: "MCP Fabric p95 latency > 5s"
            description: "Gateway p95 latency is {{ $value | humanizeDuration }}"

        # Agent not ready
        - alert: MCPFabricAgentNotReady
          expr: mcpfabric_agent_ready == 0
          for: 5m
          labels:
            severity: critical
          annotations:
            summary: "Agent {{ $labels.name }} not ready"
            description: "Agent {{ $labels.name }} in {{ $labels.namespace }} has been not ready for 5 minutes"

        # Agent replicas unavailable
        - alert: MCPFabricAgentReplicasUnavailable
          expr: |
            mcpfabric_agent_replicas - mcpfabric_agent_replicas_available > 0
          for: 10m
          labels:
            severity: warning
          annotations:
            summary: "Agent {{ $labels.name }} has unavailable replicas"
            description: "Agent {{ $labels.name }} has {{ $value }} unavailable replicas"

        # Reconciliation errors
        - alert: MCPFabricReconcileErrors
          expr: |
            sum by (controller) (rate(mcpfabric_reconcile_total{result="error"}[5m])) > 0.1
          for: 5m
          labels:
            severity: warning
          annotations:
            summary: "{{ $labels.controller }} controller reconcile errors"
            description: "Controller {{ $labels.controller }} has {{ $value | humanize }} errors/s"

        # Circuit breaker open
        - alert: MCPFabricCircuitBreakerOpen
          expr: mcpfabric_circuit_breaker_state == 1
          for: 1m
          labels:
            severity: critical
          annotations:
            summary: "Circuit breaker open for route {{ $labels.route }}"
            description: "Circuit breaker for route {{ $labels.route }} is open"

        # High queue depth
        - alert: MCPFabricHighQueueDepth
          expr: mcpfabric_circuit_breaker_waiting > 25
          for: 5m
          labels:
            severity: warning
          annotations:
            summary: "High queue depth for route {{ $labels.route }}"
            description: "Route {{ $labels.route }} has {{ $value }} requests waiting"
```

## ServiceMonitor Configuration

The gateway and operator expose metrics endpoints. ServiceMonitors are included in the Kustomize deployment:

### Gateway ServiceMonitor

```yaml
apiVersion: monitoring.coreos.com/v1
kind: ServiceMonitor
metadata:
  name: mcp-fabric-gateway
  namespace: mcp-fabric-gateway
  labels:
    release: prometheus
spec:
  selector:
    matchLabels:
      app: mcp-fabric-gateway
  namespaceSelector:
    matchNames:
      - mcp-fabric-gateway
  endpoints:
    - port: metrics
      interval: 15s
      path: /metrics
```

### Operator ServiceMonitor

```yaml
apiVersion: monitoring.coreos.com/v1
kind: ServiceMonitor
metadata:
  name: mcp-fabric-operator
  namespace: mcp-fabric-system
  labels:
    release: prometheus
spec:
  selector:
    matchLabels:
      control-plane: controller-manager
  namespaceSelector:
    matchNames:
      - mcp-fabric-system
  endpoints:
    - port: metrics
      interval: 15s
      path: /metrics
```

## Troubleshooting

### Metrics Not Appearing

1. Check ServiceMonitor is created:
   ```bash
   kubectl get servicemonitors -A
   ```

2. Verify Prometheus is scraping the target:
   ```bash
   # Port forward to Prometheus
   kubectl port-forward -n monitoring svc/prometheus-kube-prometheus-prometheus 9090:9090
   # Check targets at http://localhost:9090/targets
   ```

3. Check service labels match ServiceMonitor selector:
   ```bash
   kubectl get svc -n mcp-fabric-gateway --show-labels
   ```

### Dashboards Not Loading

1. Check dashboard ConfigMap has correct labels:
   ```bash
   kubectl get configmaps -n monitoring -l grafana_dashboard=1
   ```

2. Check Grafana sidecar logs:
   ```bash
   kubectl logs -n monitoring -l app.kubernetes.io/name=grafana -c grafana-sc-dashboard
   ```

3. Verify datasource is configured:
   - Go to Grafana > Connections > Data sources
   - Ensure Prometheus datasource points to correct URL

### High Cardinality

If metrics cardinality is too high:

1. Limit label values in application code
2. Use recording rules to pre-aggregate:
   ```yaml
   groups:
     - name: mcp-fabric.aggregations
       rules:
         - record: mcpfabric:gateway_requests:rate5m
           expr: sum(rate(mcpfabric_gateway_requests_total[5m]))
   ```
