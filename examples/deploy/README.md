# Example Deployments

Sample Kubernetes manifests for deploying example agents, tools, and routes.

## Structure

```
deploy/
├── agents/           # Agent CR examples
├── tools/            # Tool CR examples
├── routes/           # Route CR examples
├── tasks/            # Task CR examples
└── monitoring/       # Prometheus stack
```

## Deploying Examples

### Prerequisites

1. MCP Fabric operator and gateway deployed
2. CRDs installed
3. AWS credentials secret created:
   ```bash
   kubectl -n mcp-fabric-agents create secret generic aws-bedrock-credentials \
     --from-literal=AWS_REGION=eu-north-1 \
     --from-literal=AWS_ACCESS_KEY_ID=your-key \
     --from-literal=AWS_SECRET_ACCESS_KEY=your-secret
   ```

### Deploy All Examples

```bash
kubectl apply -f examples/deploy/tools/
kubectl apply -f examples/deploy/agents/
kubectl apply -f examples/deploy/routes/
kubectl apply -f examples/deploy/tasks/
```

### Deploy Individual Agents

```bash
# Engineering artist agent
kubectl apply -f examples/deploy/agents/agent-engineering-artist.yaml

# Text assistant agent (with string tools)
kubectl apply -f examples/deploy/tools/tool-string-tools.yaml
kubectl apply -f examples/deploy/agents/agent-text-assistant.yaml
```

## Agent Examples

| File | Agent | Description |
|------|-------|-------------|
| `agent-engineering-artist.yaml` | engineering-artist | Diagram generation |
| `agent-text-assistant.yaml` | text-assistant | Text processing with string tools |

## Tool Examples

| File | Tool | Description |
|------|------|-------------|
| `tool-string-tools.yaml` | string-tools | String manipulation utilities |

## Route Examples

| File | Routes |
|------|--------|
| `route-default.yaml` | Default routing rules for all agents |

## Task Examples

Tasks enable autonomous multi-step AI workflows with Git integration.

| File | Description |
|------|-------------|
| `example-task.yaml` | Complete Task CR example with Git, quality gates, and limits |
| `example-prd-configmap.yaml` | Example PRD (Product Requirements Document) stored in ConfigMap |

### Deploy a Task

```bash
# Create the PRD ConfigMap first
kubectl apply -f examples/deploy/tasks/example-prd-configmap.yaml

# Create the git credentials secret (required for git operations)
kubectl -n mcp-fabric-agents create secret generic github-credentials \
  --from-literal=token=your-github-token

# Deploy the Task
kubectl apply -f examples/deploy/tasks/example-task.yaml
```

### Task Features

- **Task Sources**: Load PRD from ConfigMap, Secret, or inline in the spec
- **Git Integration**: Automatic clone, commit, push, and PR creation
- **Quality Gates**: Run lint, test, or other commands after each task
- **Execution Limits**: Control iterations, timeouts, and consecutive failures
- **Progress Tracking**: Persist progress via ConfigMap or PVC

See the [Task CRD Reference](../../docs/CRD-REFERENCE.md) for full specification details.

## Monitoring

The `monitoring/` directory contains a Prometheus stack for observability:

```bash
# Install Prometheus stack
kubectl apply -k examples/deploy/monitoring/prometheus/
```

See [monitoring/prometheus/README.md](monitoring/prometheus/README.md) for details.

## Configuration

The `configmap.example.yaml` shows agent configuration structure:

```yaml
apiVersion: v1
kind: ConfigMap
metadata:
  name: agent-config-example
data:
  agent.json: |
    {
      "prompt": "System prompt here",
      "model": {
        "modelId": "eu.anthropic.claude-3-7-sonnet-20250219-v1:0",
        "maxTokens": 4096
      }
    }
```

## Customizing

To customize an example:

1. Copy the YAML file
2. Modify the spec as needed
3. Apply to your cluster

Key fields to customize:
- `spec.prompt` - System prompt for the agent
- `spec.model.modelId` - LLM model to use
- `spec.replicas` - Number of agent pods
- `spec.resources` - CPU/memory requests
