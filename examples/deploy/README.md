# Example Deployments

Sample Kubernetes manifests for deploying example agents, tools, and routes.

## Structure

```
deploy/
├── agents/           # Agent CR examples
├── tools/            # Tool CR examples
├── routes/           # Route CR examples
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
