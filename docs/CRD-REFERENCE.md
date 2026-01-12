# CRD Reference

MCP Fabric defines three Custom Resource Definitions for managing AI agents.

## Agent

Declares a Strands AI agent with tools and MCP connections.

```yaml
apiVersion: fabric.jarsater.ai/v1alpha1
kind: Agent
```

**Short name:** `ag`

### AgentSpec

| Field | Type | Required | Default | Description |
|-------|------|----------|---------|-------------|
| `prompt` | string | Yes | - | System instruction/persona for the agent |
| `model` | [ModelConfig](#modelconfig) | Yes | - | LLM backend configuration |
| `toolPackages` | [][ToolRef](#toolref) | No | - | References to Tool resources |
| `mcpSelector` | [MCPServerSelector](#mcpserverselector) | No | - | Selector for MCPServer resources |
| `policy` | [AgentPolicy](#agentpolicy) | No | - | Runtime constraints |
| `network` | [NetworkSpec](#networkspec) | No | - | Egress rules |
| `replicas` | int32 | No | `1` | Number of agent pods |
| `resources` | ResourceRequirements | No | - | Compute resource requirements |
| `image` | string | No | - | Override default strands-agent-runner image |
| `serviceAccountName` | string | No | - | Service account for agent pods |
| `nodeSelector` | map[string]string | No | - | Pod scheduling node selector |
| `tolerations` | []Toleration | No | - | Pod scheduling tolerations |
| `env` | []EnvVar | No | - | Environment variables |
| `envFrom` | []EnvFromSource | No | - | Environment from Secrets/ConfigMaps |
| `tools` | [][AgentTool](#agenttool) | No | - | MCP tools this agent exposes |

### ModelConfig

| Field | Type | Required | Default | Description |
|-------|------|----------|---------|-------------|
| `provider` | string | Yes | - | Model provider: `anthropic`, `openai`, `bedrock` |
| `modelId` | string | Yes | - | Model identifier (e.g., `claude-sonnet-4-20250514`) |
| `temperature` | float64 | No | - | Randomness control (0.0-1.0) |
| `maxTokens` | int32 | No | - | Maximum response tokens |
| `endpoint` | string | No | - | Override default provider endpoint |

### ToolRef

| Field | Type | Required | Default | Description |
|-------|------|----------|---------|-------------|
| `name` | string | Yes | - | Name of the Tool resource |
| `namespace` | string | No | agent namespace | Namespace of the Tool |
| `enabledTools` | []string | No | all | Specific tools to enable |
| `disabledTools` | []string | No | none | Specific tools to disable |

### MCPServerSelector

| Field | Type | Required | Default | Description |
|-------|------|----------|---------|-------------|
| `labelSelector` | LabelSelector | No | - | Label selector for MCPServers |
| `namespaces` | []string | No | agent namespace | Namespaces to search |

### AgentPolicy

| Field | Type | Required | Default | Description |
|-------|------|----------|---------|-------------|
| `maxToolCalls` | int32 | No | `50` | Max tool invocations per request |
| `requestTimeout` | Duration | No | `5m` | Max duration per request |
| `toolTimeout` | Duration | No | `30s` | Max duration per tool call |
| `maxConcurrentRequests` | int32 | No | `10` | Max parallel requests |

### NetworkSpec

| Field | Type | Required | Default | Description |
|-------|------|----------|---------|-------------|
| `allowedFqdns` | []string | No | - | FQDNs agent can connect to |
| `allowedCidrs` | []string | No | - | CIDR blocks agent can connect to |
| `allowModelProvider` | bool | No | `true` | Auto-allow model provider egress |
| `allowObjectStore` | bool | No | `false` | Auto-allow object store egress |

### AgentTool

| Field | Type | Required | Default | Description |
|-------|------|----------|---------|-------------|
| `name` | string | Yes | - | Tool identifier |
| `description` | string | Yes | - | Tool description |
| `inputSchema` | JSON | No | - | JSON Schema for parameters |

### AgentStatus

| Field | Type | Description |
|-------|------|-------------|
| `ready` | bool | Agent deployment is ready |
| `observedGeneration` | int64 | Last observed generation |
| `endpoint` | string | Service endpoint |
| `availableReplicas` | int32 | Number of ready pods |
| `resolvedMcpEndpoints` | []ResolvedMCPEndpoint | Discovered MCP servers |
| `configHash` | string | Configuration hash for rolling updates |
| `availableTools` | []AgentTool | Ready MCP tools |
| `conditions` | []Condition | Status conditions |

### Example

```yaml
apiVersion: fabric.jarsater.ai/v1alpha1
kind: Agent
metadata:
  name: finops-assistant
  namespace: mcp-fabric-agents
spec:
  prompt: |
    You are a FinOps assistant specialized in AWS cloud cost management.
    Help users analyze costs, identify optimization opportunities, and
    provide actionable recommendations.

  model:
    provider: bedrock
    modelId: amazon.nova-lite-v1:0
    temperature: 0.3
    maxTokens: 4096

  toolPackages:
    - name: string-tools

  policy:
    maxToolCalls: 20
    requestTimeout: 2m
    toolTimeout: 15s

  network:
    allowedFqdns:
      - "ce.us-east-1.amazonaws.com"
      - "organizations.us-east-1.amazonaws.com"

  replicas: 2

  resources:
    requests:
      memory: "256Mi"
      cpu: "100m"
    limits:
      memory: "512Mi"
      cpu: "500m"

  env:
    - name: AWS_DEFAULT_REGION
      value: us-east-1

  envFrom:
    - secretRef:
        name: aws-bedrock-credentials

  tools:
    - name: analyze_costs
      description: Analyze AWS costs for a given time period
      inputSchema:
        type: object
        properties:
          start_date:
            type: string
          end_date:
            type: string
        required: ["start_date", "end_date"]
```

---

## Tool

Declares a Strands tool bundle containing `@tool` decorated Python functions.

```yaml
apiVersion: fabric.jarsater.ai/v1alpha1
kind: Tool
```

**Short name:** `tl`

### ToolSpec

| Field | Type | Required | Default | Description |
|-------|------|----------|---------|-------------|
| `image` | string | Yes | - | OCI image with tool package code |
| `imagePullPolicy` | string | No | `IfNotPresent` | `Always`, `IfNotPresent`, `Never` |
| `imagePullSecrets` | []LocalObjectReference | No | - | Secrets for pulling image |
| `tools` | [][ToolDefinition](#tooldefinition) | No | - | Declared tools (or discovered at runtime) |
| `entryModule` | string | No | - | Python module path (e.g., `mypackage.tools`) |

### ToolDefinition

| Field | Type | Required | Default | Description |
|-------|------|----------|---------|-------------|
| `name` | string | Yes | - | Tool function name |
| `description` | string | No | - | Tool description |
| `inputSchema` | [JSONSchemaProps](#jsonschemaprops) | No | - | Input parameter schema |
| `outputSchema` | [JSONSchemaProps](#jsonschemaprops) | No | - | Output schema |

### JSONSchemaProps

| Field | Type | Required | Default | Description |
|-------|------|----------|---------|-------------|
| `type` | string | No | - | `object`, `string`, `number`, `integer`, `boolean`, `array` |
| `properties` | JSON | No | - | Object properties (raw JSON) |
| `required` | []string | No | - | Required property names |
| `items` | JSON | No | - | Array item schema |
| `description` | string | No | - | Schema element description |

### ToolStatus

| Field | Type | Description |
|-------|------|-------------|
| `ready` | bool | Tool is validated and available |
| `observedGeneration` | int64 | Last observed generation |
| `availableTools` | []ToolDefinition | Discovered or declared tools |
| `conditions` | []Condition | Status conditions |

### Example

```yaml
apiVersion: fabric.jarsater.ai/v1alpha1
kind: Tool
metadata:
  name: string-tools
  namespace: mcp-fabric-agents
spec:
  image: ghcr.io/jarsater/string-tools:latest
  imagePullPolicy: IfNotPresent
  entryModule: string_tools.tools

  tools:
    - name: reverse_string
      description: Reverse the characters in a string
      inputSchema:
        type: object
        properties:
          text:
            type: string
            description: The string to reverse
        required: ["text"]

    - name: count_words
      description: Count the number of words in a string
      inputSchema:
        type: object
        properties:
          text:
            type: string
            description: The text to count words in
        required: ["text"]

    - name: change_case
      description: Change the case of a string
      inputSchema:
        type: object
        properties:
          text:
            type: string
          case:
            type: string
            description: "Target case: upper, lower, title"
        required: ["text", "case"]
```

---

## Route

Declares routing rules from the gateway to agents.

```yaml
apiVersion: fabric.jarsater.ai/v1alpha1
kind: Route
```

**Short name:** `rt`

### RouteSpec

| Field | Type | Required | Default | Description |
|-------|------|----------|---------|-------------|
| `rules` | [][RouteRule](#routerule) | No | - | Routing rules |
| `defaults` | [RouteDefaults](#routedefaults) | No | - | Fallback behavior |
| `gatewaySelector` | map[string]string | No | - | Gateway label selector |

### RouteRule

| Field | Type | Required | Default | Description |
|-------|------|----------|---------|-------------|
| `name` | string | Yes | - | Unique rule identifier |
| `priority` | int32 | No | `0` | Evaluation order (higher = first) |
| `match` | [RouteMatch](#routematch) | Yes | - | Matching conditions |
| `backends` | [][RouteBackend](#routebackend) | Yes | - | Target agents |

### RouteMatch

| Field | Type | Required | Default | Description |
|-------|------|----------|---------|-------------|
| `agent` | string | No | - | Match explicit agent name |
| `intentRegex` | string | No | - | Regex for request intent (RE2 syntax) |
| `tenantId` | string | No | - | Match specific tenant |
| `headers` | map[string]string | No | - | Match request metadata |

### RouteBackend

| Field | Type | Required | Default | Description |
|-------|------|----------|---------|-------------|
| `agentRef` | [AgentRef](#agentref) | Yes | - | Agent reference |
| `weight` | int32 | No | `100` | Selection probability (0-100) |

### AgentRef

| Field | Type | Required | Default | Description |
|-------|------|----------|---------|-------------|
| `name` | string | Yes | - | Agent name |
| `namespace` | string | No | route namespace | Agent namespace |

### RouteDefaults

| Field | Type | Required | Default | Description |
|-------|------|----------|---------|-------------|
| `backend` | RouteBackend | No | - | Fallback agent |
| `circuitBreaker` | [CircuitBreakerConfig](#circuitbreakerconfig) | No | - | Request limiting |
| `rejectUnmatched` | bool | No | `false` | Error on unmatched requests |

### CircuitBreakerConfig

| Field | Type | Required | Default | Description |
|-------|------|----------|---------|-------------|
| `maxConcurrent` | int32 | No | `100` | Max concurrent backend requests |
| `maxQueueSize` | int32 | No | `50` | Max queued requests |
| `queueTimeout` | Duration | No | `30s` | Max queue wait time |
| `requestTimeout` | Duration | No | `5m` | Max backend request duration |

### RouteStatus

| Field | Type | Description |
|-------|------|-------------|
| `ready` | bool | All referenced agents available |
| `observedGeneration` | int64 | Last observed generation |
| `activeRules` | int32 | Count of compiled rules |
| `backends` | []BackendStatus | Backend agent health |
| `compiledConfigMap` | string | Generated routes ConfigMap name |
| `conditions` | []Condition | Status conditions |

### Example

```yaml
apiVersion: fabric.jarsater.ai/v1alpha1
kind: Route
metadata:
  name: default-routes
  namespace: mcp-fabric-agents
spec:
  rules:
    # Explicit agent routing (highest priority)
    - name: explicit-text-assistant
      priority: 100
      match:
        agent: text-assistant
      backends:
        - agentRef:
            name: text-assistant
            namespace: mcp-fabric-agents

    # Intent-based routing
    - name: cost-intent
      priority: 50
      match:
        intentRegex: "(?i)(cost|spend|budget|finops)"
      backends:
        - agentRef:
            name: aws-finops
            namespace: mcp-fabric-agents

    - name: docs-intent
      priority: 50
      match:
        intentRegex: "(?i)(document|docs|search.*doc)"
      backends:
        - agentRef:
            name: aws-docs
            namespace: mcp-fabric-agents

    # Weighted routing (A/B testing)
    - name: api-routing
      priority: 40
      match:
        intentRegex: "(?i)(aws|ec2|s3|lambda)"
      backends:
        - agentRef:
            name: aws-api-v1
          weight: 80
        - agentRef:
            name: aws-api-v2
          weight: 20

    # Tenant-specific routing
    - name: enterprise-tenant
      priority: 60
      match:
        tenantId: "enterprise-corp"
      backends:
        - agentRef:
            name: enterprise-agent

  defaults:
    backend:
      agentRef:
        name: general-assistant
        namespace: mcp-fabric-agents
    circuitBreaker:
      maxConcurrent: 50
      maxQueueSize: 25
      queueTimeout: 15s
      requestTimeout: 3m
    rejectUnmatched: false
```

---

## Routing Logic

The gateway evaluates routes in this order:

1. **Explicit agent match** - If `request.agent` matches a rule's `match.agent`
2. **Intent regex match** - If `request.intent` matches `match.intentRegex`
3. **Tenant match** - If `request.tenantId` matches `match.tenantId`
4. **Header match** - If request metadata matches `match.headers`

Within matching rules:
- Higher `priority` rules are evaluated first
- First matching rule wins
- If multiple backends, weighted random selection applies

If no rules match:
- Use `defaults.backend` if configured
- If `defaults.rejectUnmatched: true`, return 400 error
- Otherwise return 404

## Conditions

All CRDs use standard Kubernetes conditions:

| Condition Type | Description |
|----------------|-------------|
| `Ready` | Resource is fully operational |
| `Progressing` | Resource is being created/updated |
| `Degraded` | Resource is partially available |

Example condition:
```yaml
conditions:
  - type: Ready
    status: "True"
    reason: AgentReady
    message: "Agent deployment has 2 available replicas"
    lastTransitionTime: "2024-01-15T10:30:00Z"
```
