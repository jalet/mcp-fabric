# Development Guide

This guide covers setting up a local development environment for MCP Fabric.

## Prerequisites

- Go 1.21+
- Docker
- Kind (Kubernetes in Docker)
- kubectl
- kustomize
- golangci-lint (for linting)
- controller-gen (for CRD generation)

### Installing Prerequisites

```bash
# Install Kind
go install sigs.k8s.io/kind@latest

# Install controller-gen
go install sigs.k8s.io/controller-tools/cmd/controller-gen@latest

# Install golangci-lint
curl -sSfL https://raw.githubusercontent.com/golangci/golangci-lint/master/install.sh | sh -s -- -b $(go env GOPATH)/bin

# Install kustomize
curl -s "https://raw.githubusercontent.com/kubernetes-sigs/kustomize/master/hack/install_kustomize.sh" | bash
```

## Project Structure

```text
mcp-fabric/
├── operator/           # Kubernetes operator (Go)
│   ├── api/            # CRD types (v1alpha1)
│   ├── cmd/manager/    # Operator entrypoint
│   ├── config/         # CRD manifests, RBAC
│   └── internal/       # Controllers, renderers
├── gateway/            # HTTP/MCP gateway (Go)
│   ├── cmd/gateway/    # Gateway entrypoint
│   └── internal/       # API, routing, MCP handlers
├── pkg/                # Shared Go packages
├── deploy/
│   └── kustomize/      # Kustomize bases for operator/gateway
├── examples/           # Example implementations
│   ├── agents/         # Example agent container images
│   ├── tools/          # Example tool packages
│   ├── libs/           # Shared libraries for agents
│   └── deploy/         # Sample CRs and manifests
└── docs/               # Documentation
```

## Local Development with Kind

### 1. Create the Kind Cluster

```bash
# Create cluster with port mappings
kind create cluster --config examples/kind-config.yaml

# Wait for CoreDNS
kubectl -n kube-system wait --for=condition=ready pod -l k8s-app=kube-dns --timeout=60s

# Patch CoreDNS to use 9.9.9.9 as upstream DNS (fixes some network issues)
kubectl -n kube-system patch configmap coredns --type merge -p '
{
  "data": {
    "Corefile": ".:53 {\n    errors\n    health {\n       lameduck 5s\n    }\n    ready\n    kubernetes cluster.local in-addr.arpa ip6.arpa {\n       pods insecure\n       fallthrough in-addr.arpa ip6.arpa\n       ttl 30\n    }\n    prometheus :9153\n    forward . 9.9.9.9\n    cache 30\n    loop\n    reload\n    loadbalance\n}\n"
  }
}'

# Restart CoreDNS
kubectl -n kube-system rollout restart deployment coredns
```

### 2. Build and Load Images

```bash
# Build operator and gateway
mise run docker:build

# Build example agents, tools, and libs
mise run images:examples

# Load images into Kind
kind load docker-image ghcr.io/jalet/mcp-fabric-operator:latest --name mcp-fabric
kind load docker-image ghcr.io/jalet/mcp-fabric-gateway:latest --name mcp-fabric
kind load docker-image ghcr.io/jalet/strands-agent-runner:latest --name mcp-fabric
kind load docker-image ghcr.io/jalet/string-tools:latest --name mcp-fabric
# Load other example agent images as needed
```

### 3. Deploy MCP Fabric

```bash
# Install CRDs
kubectl apply -f operator/config/crd/bases/

# Create namespaces
kubectl create namespace mcp-fabric-system
kubectl create namespace mcp-fabric-gateway
kubectl create namespace mcp-fabric-agents

# Deploy operator
kubectl apply -k deploy/kustomize/base/operator

# Deploy gateway
kubectl apply -k deploy/kustomize/base/gateway

# Deploy example agents (optional)
kubectl apply -f examples/deploy/tools/
kubectl apply -f examples/deploy/agents/
kubectl apply -f examples/deploy/routes/
```

### 4. Verify Deployment

```bash
# Check operator
kubectl -n mcp-fabric-system get pods
kubectl -n mcp-fabric-system logs -l control-plane=controller-manager

# Check gateway
kubectl -n mcp-fabric-gateway get pods
kubectl -n mcp-fabric-gateway logs -l app=mcp-fabric-gateway

# Check agents
kubectl -n mcp-fabric-agents get agents,tools,routes
kubectl -n mcp-fabric-agents get pods
```

## Tasks

Tooling and tasks are managed by [mise](https://mise.jdx.dev). Run
`mise tasks` to list everything; the common ones:

```bash
mise tasks                # List all tasks

# Code generation
mise run generate         # Generate DeepCopy methods (controller-gen object)
mise run manifests        # Generate CRD + RBAC manifests

# Quality
mise run fmt:go           # gofmt both modules
mise run fmt:md           # Format markdown (rumdl)
mise run vet              # go vet both modules
mise run lint:go          # golangci-lint both modules
mise run mod:tidy         # go mod tidy both modules

# Build & test
mise run build            # Build operator + gateway binaries
mise run test             # Run all Go tests (race detector)
mise run test:integration # Run operator envtest integration tests
mise run ci               # Full CI-parity suite (what .github/workflows/ci.yml runs)

# Docker images
mise run docker:build     # Build operator + gateway images
mise run docker:push      # Push operator + gateway images
mise run images:examples  # Build all example agent/tool/library images

# CRDs & cleanup
mise run crds:install     # Install CRDs into the current cluster
mise run crds:uninstall   # Remove CRDs from the current cluster
mise run clean            # Remove build artifacts
```

## Running Tests

```bash
# Run all tests with coverage
mise run test

# Run operator tests only
mise run test

# Run gateway tests only
mise run test

# View coverage report
go tool cover -html=operator/coverage.out
```

## Code Generation

After modifying CRD types in `operator/api/v1alpha1/`:

```bash
# Generate DeepCopy methods
mise run generate

# Generate CRD manifests and RBAC
mise run manifests
```

## Linting

```bash
# Run linter
mise run lint:go

# Fix common issues
cd operator && golangci-lint run --fix ./...
cd gateway && golangci-lint run --fix ./...
```

## Debugging

### Operator Logs

```bash
# Follow operator logs
kubectl -n mcp-fabric-system logs -f -l control-plane=controller-manager

# Check controller-runtime debug logs
kubectl -n mcp-fabric-system logs -l control-plane=controller-manager --tail=100 | grep -E "(level|error|reconcil)"
```

### Gateway Logs

```bash
# Follow gateway logs
kubectl -n mcp-fabric-gateway logs -f -l app=mcp-fabric-gateway

# Check for routing issues
kubectl -n mcp-fabric-gateway logs -l app=mcp-fabric-gateway | grep -E "(route|backend|error)"
```

### Agent Logs

```bash
# Follow specific agent logs
kubectl -n mcp-fabric-agents logs -f -l agent=text-assistant

# Check agent health
kubectl -n mcp-fabric-agents exec -it deploy/text-assistant -- curl -s localhost:8080/healthz
```

### Port Forwarding

```bash
# Gateway (for testing /v1/invoke)
kubectl port-forward -n mcp-fabric-gateway svc/mcp-fabric-gateway 8080:8080

# Operator metrics
kubectl port-forward -n mcp-fabric-system svc/mcp-fabric-operator-metrics 8081:8081

# Specific agent
kubectl port-forward -n mcp-fabric-agents svc/text-assistant 8082:8080
```

### Testing the Gateway

```bash
# List agents
curl http://localhost:8080/v1/agents

# List routes
curl http://localhost:8080/v1/routes

# Invoke an agent
curl -X POST http://localhost:8080/v1/invoke \
  -H "Content-Type: application/json" \
  -d '{
    "agent": "text-assistant",
    "query": "Reverse the string Hello"
  }'

# Health check
curl http://localhost:8080/healthz
```

### Testing MCP Protocol

```bash
# Initialize session
curl -X POST http://localhost:8080/mcp \
  -H "Content-Type: application/json" \
  -d '{
    "jsonrpc": "2.0",
    "id": 1,
    "method": "initialize",
    "params": {
      "protocolVersion": "2024-11-05",
      "capabilities": {},
      "clientInfo": {"name": "test", "version": "1.0.0"}
    }
  }'

# List tools
curl -X POST http://localhost:8080/mcp \
  -H "Content-Type: application/json" \
  -d '{"jsonrpc": "2.0", "id": 2, "method": "tools/list"}'

# Call a tool
curl -X POST http://localhost:8080/mcp \
  -H "Content-Type: application/json" \
  -d '{
    "jsonrpc": "2.0",
    "id": 3,
    "method": "tools/call",
    "params": {
      "name": "text-assistant__manipulate_text",
      "arguments": {"request": "Reverse the string Hello"}
    }
  }'
```

## Working with CRDs

### Agent CRD

```yaml
apiVersion: fabric.jarsater.ai/v1alpha1
kind: Agent
metadata:
  name: my-agent
  namespace: mcp-fabric-agents
spec:
  prompt: |
    You are a helpful assistant.
  model:
    provider: bedrock
    modelId: amazon.nova-lite-v1:0
    temperature: 0.3
    maxTokens: 4096
  replicas: 1
  resources:
    requests:
      memory: "256Mi"
      cpu: "100m"
```

### Tool CRD

```yaml
apiVersion: fabric.jarsater.ai/v1alpha1
kind: Tool
metadata:
  name: my-tools
  namespace: mcp-fabric-agents
spec:
  image: ghcr.io/example/my-tools:v1.0.0
  entryModule: my_tools.tools
  tools:
    - name: my_function
      description: Does something useful
```

### Route CRD

```yaml
apiVersion: fabric.jarsater.ai/v1alpha1
kind: Route
metadata:
  name: my-routes
  namespace: mcp-fabric-agents
spec:
  rules:
    - name: explicit-my-agent
      priority: 100
      match:
        agent: my-agent
      backends:
        - agentRef:
            name: my-agent
            namespace: mcp-fabric-agents
```

## Cleanup

```bash
# Delete example resources
kubectl delete -f examples/deploy/routes/
kubectl delete -f examples/deploy/agents/
kubectl delete -f examples/deploy/tools/

# Delete gateway
kubectl delete -k deploy/kustomize/base/gateway

# Delete operator
kubectl delete -k deploy/kustomize/base/operator

# Delete CRDs
kubectl delete -f operator/config/crd/bases/

# Delete Kind cluster
kind delete cluster --name mcp-fabric
```

## Troubleshooting

See [docs/TROUBLESHOOTING.md](docs/TROUBLESHOOTING.md) for common issues and
solutions.

## IDE Setup

### VS Code

Recommended extensions:

- Go (golang.go)
- YAML (redhat.vscode-yaml)
- Kubernetes (ms-kubernetes-tools.vscode-kubernetes-tools)

Settings (`.vscode/settings.json`):

```json
{
  "go.lintTool": "golangci-lint",
  "go.lintFlags": ["--fast"],
  "yaml.schemas": {
    "kubernetes": ["deploy/**/*.yaml"]
  }
}
```

### GoLand / IntelliJ

1. Enable Go modules support
2. Set GOROOT and GOPATH
3. Install Kubernetes plugin for YAML support
