# Getting Started

This guide walks you through deploying MCP Fabric and creating your first agent.

## Prerequisites

- Kubernetes cluster (1.28+) or Kind for local development
- kubectl configured to connect to your cluster
- Docker for building images
- AWS credentials with Bedrock access (or alternative LLM provider)

## Installation

### 1. Install CRDs

```bash
kubectl apply -f operator/config/crd/bases/
```

This installs three Custom Resource Definitions:
- `Agent` - Defines AI agents
- `Route` - Defines request routing rules
- `Tool` - Defines tool packages

### 2. Create Namespaces

```bash
kubectl create namespace mcp-fabric-system
kubectl create namespace mcp-fabric-gateway
kubectl create namespace mcp-fabric-agents
```

### 3. Create AWS Credentials Secret

```bash
kubectl -n mcp-fabric-agents create secret generic aws-bedrock-credentials \
  --from-literal=AWS_REGION=eu-north-1 \
  --from-literal=AWS_ACCESS_KEY_ID=your-access-key \
  --from-literal=AWS_SECRET_ACCESS_KEY=your-secret-key
```

### 4. Deploy the Operator

```bash
kubectl apply -k deploy/kustomize/base/operator/
```

Verify the operator is running:

```bash
kubectl -n mcp-fabric-system get pods
```

### 5. Deploy the Gateway

```bash
kubectl apply -k deploy/kustomize/base/gateway/
```

Verify the gateway is running:

```bash
kubectl -n mcp-fabric-gateway get pods
```

## Creating Your First Agent

### 1. Create an Agent Resource

Create a file `my-agent.yaml`:

```yaml
apiVersion: fabric.jarsater.ai/v1alpha1
kind: Agent
metadata:
  name: my-assistant
  namespace: mcp-fabric-agents
spec:
  prompt: |
    You are a helpful assistant that answers questions clearly and concisely.
  model:
    provider: bedrock
    modelId: eu.anthropic.claude-3-7-sonnet-20250219-v1:0
    maxTokens: 4096
  envFrom:
    - secretRef:
        name: aws-bedrock-credentials
  replicas: 1
  resources:
    requests:
      memory: "256Mi"
      cpu: "100m"
```

Apply it:

```bash
kubectl apply -f my-agent.yaml
```

### 2. Verify Agent Deployment

```bash
kubectl -n mcp-fabric-agents get agents
kubectl -n mcp-fabric-agents get pods
```

### 3. Create a Route

Create `my-route.yaml`:

```yaml
apiVersion: fabric.jarsater.ai/v1alpha1
kind: Route
metadata:
  name: default-route
  namespace: mcp-fabric-agents
spec:
  rules:
    - name: my-assistant-route
      priority: 100
      match:
        agent: my-assistant
      backends:
        - agentRef:
            name: my-assistant
            namespace: mcp-fabric-agents
```

Apply it:

```bash
kubectl apply -f my-route.yaml
```

## Invoking the Agent

### Port Forward the Gateway

```bash
kubectl -n mcp-fabric-gateway port-forward svc/agent-gateway 8080:8080
```

### Send a Request

```bash
curl -X POST http://localhost:8080/v1/invoke \
  -H "Content-Type: application/json" \
  -d '{
    "agent": "my-assistant",
    "query": "What is Kubernetes?"
  }'
```

## Local Development with Kind

For local development, use Kind:

```bash
# Create cluster
kind create cluster --name mcp-fabric --config examples/kind-config.yaml

# Build and load images
make docker-build
kind load docker-image ghcr.io/jarsater/mcp-fabric-operator:latest --name mcp-fabric
kind load docker-image ghcr.io/jarsater/mcp-fabric-gateway:latest --name mcp-fabric

# Deploy
kubectl apply -f operator/config/crd/bases/
kubectl apply -k deploy/kustomize/base/operator/
kubectl apply -k deploy/kustomize/base/gateway/
```

## Next Steps

- [Architecture](architecture.md) - Understand how MCP Fabric works
- [Writing Agents](writing-agents.md) - Build custom agent implementations
- [Writing Tools](writing-tools.md) - Create tool packages for agents
- [Examples](../examples/README.md) - Explore reference implementations
