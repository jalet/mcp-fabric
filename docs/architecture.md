# Architecture

MCP Fabric is a Kubernetes-native platform for deploying AI agents declaratively.

## System Overview

```
┌─────────────────────────────────────────────────────────────┐
│                   mcp-fabric-gateway namespace              │
│  ┌─────────────────┐    ┌─────────────────────────────────┐ │
│  │  Agent Gateway  │◄───│  agent-gateway-routes ConfigMap │ │
│  │   (Go HTTP)     │    │  (compiled from Route CRs)      │ │
│  └────────┬────────┘    └─────────────────────────────────┘ │
└───────────│─────────────────────────────────────────────────┘
            │
            ▼
┌─────────────────────────────────────────────────────────────┐
│                   mcp-fabric-agents namespace               │
│  ┌──────────────────┐  ┌──────────────────┐                 │
│  │   Agent Pod 1    │  │   Agent Pod 2    │                 │
│  │   + Service      │  │   + Service      │                 │
│  │   + ConfigMap    │  │   + ConfigMap    │                 │
│  │   + NetworkPolicy│  │   + NetworkPolicy│                 │
│  └──────────────────┘  └──────────────────┘                 │
└─────────────────────────────────────────────────────────────┘
            │
            ▼
┌─────────────────────────────────────────────────────────────┐
│                External Services                             │
│  ┌──────────────┐  ┌──────────────┐  ┌──────────────┐       │
│  │ LLM Provider │  │  MCP Server  │  │   AWS APIs   │       │
│  │  (Bedrock)   │  │   (stdio)    │  │              │       │
│  └──────────────┘  └──────────────┘  └──────────────┘       │
└─────────────────────────────────────────────────────────────┘
```

## Components

### Operator

The operator is a Kubernetes controller that watches for Agent, Route, and Tool CRs and reconciles the desired state.

**Responsibilities:**
- Watch Agent CRs and create/update Deployments, Services, ConfigMaps, NetworkPolicies
- Watch Route CRs and compile routing rules into a ConfigMap for the gateway
- Watch Tool CRs and validate tool package references

**Resources Created per Agent:**

| Resource | Name | Purpose |
|----------|------|---------|
| ServiceAccount | `{agent-name}` | Minimal SA (no permissions) |
| ConfigMap | `{agent-name}-config` | Agent runtime configuration |
| NetworkPolicy | `{agent-name}` | Network isolation rules |
| Deployment | `{agent-name}` | Agent pods |
| Service | `{agent-name}` | ClusterIP service |

### Gateway

The gateway is an HTTP server that routes requests to appropriate agent pods.

**Responsibilities:**
- Accept HTTP requests at `/v1/invoke`
- Match requests to agents via routing rules
- Load balance across agent replicas
- Apply circuit breaker and rate limiting

**Routing Logic:**

1. If `request.agent` is specified → route directly to that agent
2. Else match `request.intent` against regex rules (by priority)
3. Filter to ready backends only
4. Select backend using weighted random or consistent hashing
5. Forward to agent's `/invoke` endpoint

### Agent Runtime

The agent runtime executes agent logic. The default runtime uses Python with the Strands AI framework.

**Contract:**
- `GET /healthz` - Health check endpoint
- `POST /invoke` - Agent invocation endpoint

## Custom Resource Definitions

### Agent

Defines an AI agent with its persona, model, and tools.

```yaml
apiVersion: fabric.jarsater.ai/v1alpha1
kind: Agent
metadata:
  name: my-agent
spec:
  prompt: "System prompt for the agent"
  model:
    provider: bedrock
    modelId: eu.anthropic.claude-3-7-sonnet-20250219-v1:0
    maxTokens: 4096
  toolPackages:
    - name: my-tools
  envFrom:
    - secretRef:
        name: credentials
```

### Route

Defines routing rules from gateway to agents.

```yaml
apiVersion: fabric.jarsater.ai/v1alpha1
kind: Route
metadata:
  name: routes
spec:
  rules:
    - name: explicit-route
      priority: 100
      match:
        agent: my-agent
      backends:
        - agentRef:
            name: my-agent
```

### Tool

Declares tool packages available to agents.

```yaml
apiVersion: fabric.jarsater.ai/v1alpha1
kind: Tool
metadata:
  name: my-tools
spec:
  image: ghcr.io/example/my-tools:v1.0.0
  entryModule: my_tools.tools
```

## Security Model

### Pod Security

All agent pods run with hardened security contexts:

- `runAsNonRoot: true`
- `runAsUser: 65534` (nobody)
- `readOnlyRootFilesystem: true`
- `allowPrivilegeEscalation: false`
- `capabilities: drop: ["ALL"]`
- `seccompProfile: RuntimeDefault`

### Network Isolation

NetworkPolicies enforce:

- Default deny ingress/egress
- Ingress only from gateway namespace
- Egress only to:
  - DNS (UDP/TCP 53)
  - Model provider endpoints
  - Explicitly allowed FQDNs

### Credential Management

Secrets are never stored in CRs. Use `envFrom` to inject credentials:

```yaml
envFrom:
  - secretRef:
      name: aws-bedrock-credentials
```

## Data Flow

### Request Flow

```
Client
  │
  ▼
Gateway (/v1/invoke)
  │
  ├─► Route Matching
  │     - Check explicit agent name
  │     - Match intent regex patterns
  │     - Select by priority
  │
  ├─► Backend Selection
  │     - Filter ready backends
  │     - Apply weighted selection
  │     - Consistent hashing for affinity
  │
  ├─► Circuit Breaker
  │     - Check concurrency limits
  │     - Queue or reject if overloaded
  │
  ▼
Agent Pod (/invoke)
  │
  ├─► Load Config from ConfigMap
  ├─► Initialize LLM Client
  ├─► Load Tools from ToolPackages
  ├─► Execute Agent Logic
  │
  ▼
Response → Gateway → Client
```

### Configuration Flow

```
Agent CR
  │
  ▼
Operator
  │
  ├─► Generate ConfigMap (agent.json)
  ├─► Hash config content
  ├─► Create/Update Deployment
  │     (with config hash annotation)
  │
  ▼
Kubernetes
  │
  ├─► Detect annotation change
  ├─► Trigger rolling update
  │
  ▼
New Agent Pods (with updated config)
```

## Patterns

### Config Hash for Rolling Updates

The operator hashes ConfigMap content and stores it in a Deployment annotation. When config changes, the hash changes, triggering a rolling update.

### Owner References

All resources created by the operator have owner references to the parent CR. When the CR is deleted, Kubernetes garbage collects all owned resources.

### Circuit Breaker

The gateway implements circuit breaker pattern with:
- Maximum concurrent requests per agent
- Request queue with timeout
- Fail-fast when overloaded

## Model Providers

Supported model providers and their egress endpoints:

| Provider | FQDNs |
|----------|-------|
| anthropic | `api.anthropic.com` |
| openai | `api.openai.com` |
| bedrock | `bedrock-runtime.*.amazonaws.com` |
| azure | `*.openai.azure.com` |
| google | `generativelanguage.googleapis.com` |
