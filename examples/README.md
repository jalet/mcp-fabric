# Examples

This directory contains reference implementations demonstrating how to build agents, tools, and shared libraries for MCP Fabric.

## Structure

```
examples/
├── agents/           # Example agent implementations
├── tools/            # Example tool packages
├── libs/             # Shared libraries
└── deploy/           # Sample Kubernetes manifests
```

## Agents

Example agent implementations using the Strands AI framework:

| Agent | Description |
|-------|-------------|
| `default/` | Default agent runner for Agent CRs without custom images |
| `engineering-artist/` | Architecture diagram generation |

See [agents/README.md](agents/README.md) for details.

## Tools

Example tool packages:

| Tool | Description |
|------|-------------|
| `string-tools/` | String manipulation utilities |

See [tools/README.md](tools/README.md) for details.

## Libraries

Shared libraries used by agents:

| Library | Description |
|---------|-------------|
| `agent-libs/` | Common utilities including JSON logging and Gunicorn config |

See [libs/README.md](libs/README.md) for details.

## Deploy

Sample Kubernetes manifests for deploying the examples:

```
deploy/
├── agents/           # Agent CR examples
├── tools/            # Tool CR examples
├── routes/           # Route CR examples
└── monitoring/       # Prometheus stack
```

See [deploy/README.md](deploy/README.md) for details.

## Building

Build all example images:

```bash
# From repository root
make examples

# Or individually
make examples-agents
make examples-tools
make examples-libs
```

Build from within examples directory:

```bash
cd examples
make docker-build
```

## Customizing

These examples are meant to be copied and customized. To create your own agent:

1. Copy an existing agent directory
2. Modify `server.py` with your logic
3. Update `pyproject.toml` with your dependencies
4. Build and push your image
5. Create an Agent CR referencing your image

See [docs/writing-agents.md](../docs/writing-agents.md) for the complete guide.
