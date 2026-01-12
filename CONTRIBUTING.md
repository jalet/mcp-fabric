# Contributing to MCP Fabric

Thank you for your interest in contributing to MCP Fabric! This document provides guidelines and information for contributors.

## Code of Conduct

Please be respectful and constructive in all interactions. We aim to maintain a welcoming environment for all contributors.

## Getting Started

### Prerequisites

- Go 1.21+
- Python 3.12+
- Docker
- kubectl
- Kind (for local development)

### Development Setup

1. Fork and clone the repository:
   ```bash
   git clone https://github.com/YOUR_USERNAME/mcp-fabric.git
   cd mcp-fabric
   ```

2. Set up the development environment:
   ```bash
   # Install Go dependencies
   cd operator && go mod download
   cd ../gateway && go mod download

   # Create Kind cluster
   kind create cluster --config examples/kind-config.yaml
   ```

3. Build and deploy locally:
   ```bash
   # Build images
   make docker-build

   # Load into Kind
   make kind-load

   # Deploy
   kubectl apply -k deploy/kustomize/base/
   ```

## How to Contribute

### Reporting Issues

- Check existing issues before creating a new one
- Use the issue templates when available
- Include relevant details: Kubernetes version, error messages, reproduction steps

### Submitting Changes

1. Create a feature branch:
   ```bash
   git checkout -b feature/your-feature-name
   ```

2. Make your changes following our coding standards (see below)

3. Test your changes:
   ```bash
   # Run Go tests
   cd operator && go test ./...
   cd ../gateway && go test ./...

   # Run integration tests (requires Kind cluster)
   make test-e2e
   ```

4. Commit with clear messages:
   ```bash
   git commit -m "feat: add support for custom health checks"
   ```

5. Push and create a pull request

### Commit Message Format

We follow conventional commits:

- `feat:` - New features
- `fix:` - Bug fixes
- `docs:` - Documentation changes
- `refactor:` - Code refactoring
- `test:` - Adding or updating tests
- `chore:` - Maintenance tasks

## Coding Standards

### Go Code

- Follow [Effective Go](https://golang.org/doc/effective_go.html)
- Run `go fmt` and `go vet` before committing
- Use meaningful variable and function names
- Add comments for exported functions and types
- Handle errors explicitly

### Python Code

- Follow PEP 8 style guide
- Use type hints where appropriate
- Document functions with docstrings

### Kubernetes Resources

- Use `kubectl apply --dry-run=client` to validate manifests
- Follow Kubernetes API conventions
- Include appropriate labels and annotations

## Project Structure

```
mcp-fabric/
├── operator/           # Kubernetes operator (Go)
│   ├── api/           # CRD type definitions
│   ├── cmd/           # Entry points
│   └── internal/      # Internal packages
├── gateway/           # HTTP gateway (Go)
│   ├── cmd/           # Entry point
│   └── internal/      # Internal packages
├── agents/            # Agent implementations (Python)
├── deploy/            # Deployment manifests
│   ├── kustomize/     # Kustomize bases and overlays
│   └── samples/       # Example resources
└── docs/              # Documentation
```

## Adding New Features

### Adding a New CRD

1. Define types in `operator/api/v1alpha1/{kind}_types.go`
2. Register in `operator/api/v1alpha1/groupversion_info.go`
3. Generate CRD manifests:
   ```bash
   make generate
   ```
4. Create controller in `operator/internal/controllers/`
5. Register controller in `operator/cmd/manager/main.go`
6. Add sample manifests in `deploy/samples/`
7. Update documentation

### Adding Render Helpers

1. Create file in `operator/internal/render/{resource}.go`
2. Define `{Resource}Params` struct
3. Implement render function returning Kubernetes object
4. Add tests in `{resource}_test.go`

## Testing

### Unit Tests

```bash
# Run all Go tests
go test ./...

# Run with coverage
go test -coverprofile=coverage.out ./...
go tool cover -html=coverage.out
```

### Integration Tests

```bash
# Requires running Kind cluster
make test-e2e
```

### Manual Testing

```bash
# Deploy test agent
kubectl apply -f deploy/samples/agent-finops-assistant.yaml

# Check logs
kubectl logs -l app.kubernetes.io/name=finops-assistant

# Test invocation
kubectl run test-curl --rm -i --restart=Never --image=curlimages/curl -- \
  curl -s -X POST http://agent-gateway:8080/v1/invoke \
  -H "Content-Type: application/json" \
  -d '{"agent": "finops-assistant", "query": "Hello"}'
```

## Documentation

- Update README.md for user-facing changes
- Add/update docs in `docs/` directory
- Create ADRs for significant architectural decisions in `docs/adr/`
- Keep AGENTS.md current with CRD patterns and examples

## Release Process

1. Update version numbers
2. Update CHANGELOG.md
3. Create git tag
4. Build and push images
5. Create GitHub release

## Getting Help

- Open an issue for questions
- Check existing documentation in `docs/`
- Review AGENTS.md for patterns and examples

## License

By contributing, you agree that your contributions will be licensed under the Apache License 2.0.
