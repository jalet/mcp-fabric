# CRD Reference

MCP Fabric defines four Custom Resource Definitions: Agent, Tool, Route, and
Task.

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
| `toolPackages` | [\[\]ToolRef](#toolref) | No | - | References to Tool resources |
| `mcpSelector` | [MCPServerSelector](#mcpserverselector) | No | - | Selector for MCPServer resources |
| `policy` | [AgentPolicy](#agentpolicy) | No | - | Runtime constraints |
| `network` | [NetworkSpec](#networkspec) | No | - | Egress rules |
| `replicas` | int32 | No | `1` | Number of agent pods |
| `standalone` | *bool | No | `true` | Run as a long-running Deployment + Service. Set `false` for agents used only as Task workers — the Task controller co-locates them as a sidecar, so no standalone Deployment/Service is created (a ServiceAccount + ConfigMap are still reconciled). |
| `resources` | ResourceRequirements | No | - | Compute resource requirements |
| `image` | string | No | - | Override default strands-agent-runner image |
| `serviceAccountName` | string | No | - | Service account for agent pods |
| `nodeSelector` | map[string]string | No | - | Pod scheduling node selector |
| `tolerations` | []Toleration | No | - | Pod scheduling tolerations |
| `env` | []EnvVar | No | - | Environment variables |
| `envFrom` | []EnvFromSource | No | - | Environment from Secrets/ConfigMaps |
| `tools` | [\[\]AgentTool](#agenttool) | No | - | MCP tools this agent exposes |

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
| `tools` | [\[\]ToolDefinition](#tooldefinition) | No | - | Declared tools (or discovered at runtime) |
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
  image: ghcr.io/jalet/string-tools:latest
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
| `rules` | [\[\]RouteRule](#routerule) | No | - | Routing rules |
| `defaults` | [RouteDefaults](#routedefaults) | No | - | Fallback behavior |
| `gatewaySelector` | map[string]string | No | - | Gateway label selector |

### RouteRule

| Field | Type | Required | Default | Description |
|-------|------|----------|---------|-------------|
| `name` | string | Yes | - | Unique rule identifier |
| `priority` | int32 | No | `0` | Evaluation order (higher = first) |
| `match` | [RouteMatch](#routematch) | Yes | - | Matching conditions |
| `backends` | [\[\]RouteBackend](#routebackend) | Yes | - | Target agents |

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

## Task

Defines an autonomous, multi-step execution loop. The operator runs an
orchestrator Job that repeatedly dispatches PRD items to a worker agent
(co-located as a sidecar), runs quality gates, and optionally commits and opens
a pull request when all items pass.

```yaml
apiVersion: fabric.jarsater.ai/v1alpha1
kind: Task
```

**Short name:** `tk`

### TaskSpec

| Field | Type | Required | Default | Description |
|-------|------|----------|---------|-------------|
| `workerRef` | [AgentReference](#agentreference) | Yes | - | Agent that implements individual tasks. Co-located as a sidecar in the orchestrator Job. |
| `orchestratorRef` | [AgentReference](#agentreference) | No | `task-orchestrator` | Agent that runs the orchestration loop. |
| `taskSource` | [TaskSource](#tasksource) | Yes | - | Where to read the PRD (task list) from. |
| `limits` | [TaskLimits](#tasklimits) | No | - | Execution constraints. |
| `qualityGates` | [\[\]QualityGate](#qualitygate) | No | - | Commands run after each task. |
| `git` | [GitConfig](#gitconfig) | No | - | Git repository settings (clone, commit, push, PR). |
| `paused` | bool | No | `false` | Pause the loop (e.g. for manual review). |
| `context` | string | No | - | Extra context passed to the orchestrator. |

### AgentReference

| Field | Type | Required | Default | Description |
|-------|------|----------|---------|-------------|
| `name` | string | Yes | - | Agent name. |
| `namespace` | string | No | Task namespace | Agent namespace. |

### TaskSource

| Field | Type | Required | Default | Description |
|-------|------|----------|---------|-------------|
| `type` | string | Yes | `configmap` | One of `configmap`, `secret`, `inline`. |
| `configMapRef` | ConfigMapKeySelector | No | - | ConfigMap holding the PRD (key defaults to `prd.json`). Required for `configmap`. |
| `secretRef` | SecretKeySelector | No | - | Secret holding the PRD. Required for `secret`. |
| `inline` | string | No | - | PRD JSON inline in the spec. Required for `inline`. |

The PRD is JSON with a `stories` array (alias: `tasks`); each item has `id`,
`title`, `priority`, `acceptanceCriteria`, and a `passes` flag the orchestrator
flips as items complete.

### TaskLimits

| Field | Type | Required | Default | Description |
|-------|------|----------|---------|-------------|
| `maxIterations` | *int32 | No | `100` | Maximum loop iterations. |
| `iterationTimeout` | Duration | No | `30m` | Per-task dispatch timeout. |
| `totalTimeout` | Duration | No | `24h` | Maximum total task duration (enforced as the Job's `activeDeadlineSeconds`). |
| `maxConsecutiveFailures` | *int32 | No | `3` | Consecutive failures before the task fails. |
| `maxJobRecreations` | *int32 | No | `3` | Times a lost orchestrator Job is recreated before failing. |

### QualityGate

| Field | Type | Required | Default | Description |
|-------|------|----------|---------|-------------|
| `name` | string | Yes | - | Gate identifier. |
| `command` | []string | Yes | - | Command to execute in the workspace. |
| `failurePolicy` | string | No | `Fail` | `Fail` (a failing gate marks the task not-passed) or `Ignore` (recorded only). |
| `timeout` | Duration | No | `5m` | Gate command timeout. |

### GitConfig

Only cloning existing repositories is supported. Automatic PR creation is
implemented for **GitHub only**; for `gitlab`/`bitbucket` the branch is pushed
but no PR is opened.

| Field | Type | Required | Default | Description |
|-------|------|----------|---------|-------------|
| `url` | string | Yes | - | Repository URL to clone. |
| `provider` | string | No | `github` | `github`, `gitlab`, or `bitbucket` (PR creation: GitHub only). |
| `image` | string | No | `alpine/git:2.43` | Image for the git-clone init container. |
| `branch` | string | No | `main` | Branch to work on. |
| `baseBranch` | string | No | - | If set, create `branch` from this base. |
| `depth` | *int32 | No | `1` | Shallow-clone depth (`0` = full clone). |
| `credentialsSecret` | LocalObjectReference | Yes | - | Secret with a `token` key (GitHub PAT or equivalent). |
| `commitAuthor` | string | No | `MCP Fabric Task` | Commit author name. |
| `commitEmail` | string | No | `task@mcp-fabric.local` | Commit author email. |
| `autoPush` | *bool | No | `true` | Push on completion. |
| `createPR` | *bool | No | `true` | Open a PR on completion (GitHub). |
| `draftPR` | *bool | No | `true` | Open the PR as a draft. |
| `prTitle` | string | No | - | PR title (default `Task: {task-name}`). |
| `prBody` | string | No | - | PR body template. Placeholders: `{task}`, `{completed}`, `{total}`. |

### TaskStatus

| Field | Type | Description |
|-------|------|-------------|
| `phase` | string | `Pending`, `Running`, `Completed`, `Failed`, or `Paused`. |
| `currentIteration` | int32 | Current/last iteration number. |
| `completedTasks` / `totalTasks` | int32 | Progress counters. |
| `consecutiveFailures` | int32 | Consecutive-failure counter. |
| `startedAt` / `completedAt` | Time | Execution start / completion timestamps. |
| `recentIterations` | []IterationResult | Up to 10 recent iteration results. |
| `repositoryUrl` / `lastCommitSha` / `pullRequestUrl` | string | Git outputs from the run. |
| `message` | string | Human-readable status detail. |
| `conditions` | []Condition | Standard `Ready` condition. |

> Progress fields are populated from the orchestrator's final result when the
> Job completes, not streamed live during the run.

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
