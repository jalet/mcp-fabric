# Example Agents

Reference implementations demonstrating how to build AI agents for MCP Fabric.

## Available Agents

| Agent | Image | Description |
|-------|-------|-------------|
| `default` | `strands-agent-runner` | Default agent runner, reads prompt from Agent CR spec |
| `engineering-artist` | `engineering-artist-agent` | Architecture diagram generation with draw.io output |

## Directory Structure

Each agent follows this structure:

```
my-agent/
├── pyproject.toml    # Python dependencies
├── server.py         # Agent implementation
└── Dockerfile        # Container build
```

## Building

Build all agents:

```bash
make docker-build
```

Build a specific agent:

```bash
make docker-build-default
make docker-build-engineering-artist
```

## Agent Patterns

### Basic Agent (default)

The default agent runner loads configuration from the Agent CR and executes queries:

```python
def create_agent():
    model = BedrockModel(model_id=config["model"]["modelId"])
    return Agent(model=model, system_prompt=config["prompt"])
```

### Native Tools Agent (engineering-artist)

Uses Python `@tool` decorated functions:

```python
from strands import tool

@tool
def create_diagram(title: str) -> str:
    """Create a new diagram."""
    return diagram_id

agent = Agent(model=model, tools=[create_diagram])
```

## Creating a New Agent

1. Copy an existing agent directory:
   ```bash
   cp -r default my-agent
   ```

2. Update `pyproject.toml`:
   ```toml
   [project]
   name = "my-agent"
   dependencies = ["strands-agents", "flask", "gunicorn"]
   ```

3. Implement `server.py`:
   - Load config from `/etc/agent/config/agent.json`
   - Implement `/healthz` and `/invoke` endpoints
   - Use Strands Agent for LLM interactions

4. Build the image:
   ```bash
   docker build -t my-agent:latest .
   ```

5. Add to Makefile (optional):
   ```makefile
   .PHONY: docker-build-my-agent
   docker-build-my-agent:
       docker build -t $(MY_AGENT_IMG) -f my-agent/Dockerfile my-agent/
   ```

## See Also

- [Writing Agents Guide](../../docs/writing-agents.md)
- [Agent CR Reference](../../docs/CRD-REFERENCE.md)
