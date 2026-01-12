# Writing Agents

This guide explains how to build custom AI agents for MCP Fabric.

## Agent Contract

Every agent must implement two HTTP endpoints:

| Endpoint | Method | Purpose |
|----------|--------|---------|
| `/healthz` | GET | Health check, return 200 if healthy |
| `/invoke` | POST | Handle agent invocation |

### Request Format

```json
{
  "query": "User's question or request",
  "tenantId": "optional-tenant-id",
  "correlationId": "optional-correlation-id"
}
```

### Response Format

```json
{
  "success": true,
  "result": {
    "response": "Agent's response text",
    "model": "model-id-used"
  }
}
```

Or on error:

```json
{
  "success": false,
  "error": "Error message"
}
```

## Using the Default Agent Runner

The simplest approach is to use the default agent runner (`strands-agent-runner`). It handles HTTP serving, config loading, and agent execution. You only need to create an Agent CR:

```yaml
apiVersion: fabric.jarsater.ai/v1alpha1
kind: Agent
metadata:
  name: my-agent
spec:
  prompt: |
    You are a helpful assistant.
  model:
    provider: bedrock
    modelId: eu.anthropic.claude-3-7-sonnet-20250219-v1:0
  envFrom:
    - secretRef:
        name: aws-credentials
```

## Building a Custom Agent

For custom logic beyond the default runner, build your own agent image.

### Project Structure

```
my-agent/
├── pyproject.toml
├── server.py
└── Dockerfile
```

### pyproject.toml

```toml
[project]
name = "my-agent"
version = "0.1.0"
requires-python = ">=3.12"
dependencies = [
    "strands-agents",
    "flask",
    "boto3",
    "gunicorn",
]
```

### server.py

```python
#!/usr/bin/env python3
import json
import os
import logging
from flask import Flask, request, jsonify
from strands import Agent
from strands.models import BedrockModel

logging.basicConfig(level=logging.INFO)
logger = logging.getLogger(__name__)

app = Flask(__name__)
config = {}

def load_config():
    global config
    config_path = os.environ.get("AGENT_CONFIG_PATH", "/etc/agent/config/agent.json")
    with open(config_path) as f:
        config = json.load(f)

def create_agent():
    model_config = config.get("model", {})
    model = BedrockModel(
        model_id=model_config.get("modelId"),
        max_tokens=model_config.get("maxTokens", 4096),
    )
    return Agent(
        model=model,
        system_prompt=config.get("prompt", ""),
    )

load_config()

@app.route("/healthz")
def healthz():
    return jsonify({"status": "ok"})

@app.route("/invoke", methods=["POST"])
def invoke():
    data = request.get_json() or {}
    query = data.get("query", "")

    if not query:
        return jsonify({"success": False, "error": "Missing query"}), 400

    try:
        agent = create_agent()
        response = agent(query)
        return jsonify({
            "success": True,
            "result": {"response": str(response)}
        })
    except Exception as e:
        logger.error(f"Agent error: {e}")
        return jsonify({"success": False, "error": str(e)}), 500

if __name__ == "__main__":
    app.run(host="0.0.0.0", port=8080)
```

### Dockerfile

```dockerfile
FROM ghcr.io/astral-sh/uv:python3.12-bookworm-slim

WORKDIR /app

COPY pyproject.toml .
RUN uv sync --no-dev --no-install-project

COPY server.py ./

ENV PATH="/app/.venv/bin:$PATH"
ENV HOME=/tmp
ENV PORT=8080

USER 65532:65532
EXPOSE 8080

CMD ["python", "-m", "gunicorn", "server:app"]
```

### Build and Deploy

```bash
# Build image
docker build -t my-registry/my-agent:latest .

# Push to registry
docker push my-registry/my-agent:latest

# Create Agent CR with custom image
kubectl apply -f - <<EOF
apiVersion: fabric.jarsater.ai/v1alpha1
kind: Agent
metadata:
  name: my-agent
spec:
  image: my-registry/my-agent:latest
  prompt: |
    You are a helpful assistant.
  model:
    provider: bedrock
    modelId: eu.anthropic.claude-3-7-sonnet-20250219-v1:0
EOF
```

## Adding Tools

### Using Tool Packages

Reference Tool CRs to add capabilities:

```yaml
spec:
  toolPackages:
    - name: string-tools
```

The operator will mount the tool package as an init container and load tools at runtime.

### Using MCP Servers

For MCP server integration, use the MCPClient:

```python
from mcp import stdio_client, StdioServerParameters
from strands.tools.mcp import MCPClient

mcp_client = MCPClient(lambda: stdio_client(
    StdioServerParameters(
        command="mcp-server-command",
        args=[],
    )
))

with mcp_client:
    tools = mcp_client.list_tools_sync()
    agent = Agent(model=model, tools=tools)
```

### Native Tools

Define tools directly using the `@tool` decorator:

```python
from strands import tool

@tool
def get_weather(city: str) -> str:
    """Get current weather for a city.

    Args:
        city: The city name
    """
    # Implementation
    return f"Weather in {city}: Sunny, 22°C"

agent = Agent(model=model, tools=[get_weather])
```

## Configuration

The operator mounts configuration at `/etc/agent/config/agent.json`:

```json
{
  "prompt": "System prompt from Agent CR",
  "model": {
    "provider": "bedrock",
    "modelId": "eu.anthropic.claude-3-7-sonnet-20250219-v1:0",
    "maxTokens": 4096
  }
}
```

Environment variables from `env` and `envFrom` are available in the container.

## Logging

Use JSON structured logging for consistency with the gateway:

```python
import logging
import json

class JSONFormatter(logging.Formatter):
    def format(self, record):
        return json.dumps({
            "ts": self.formatTime(record),
            "level": record.levelname.lower(),
            "msg": record.getMessage(),
        })

handler = logging.StreamHandler()
handler.setFormatter(JSONFormatter())
logger = logging.getLogger()
logger.addHandler(handler)
```

## Testing Locally

```bash
# Set environment variables
export AGENT_CONFIG_PATH=./test-config.json
export AWS_REGION=eu-north-1

# Create test config
cat > test-config.json <<EOF
{
  "prompt": "You are a test assistant.",
  "model": {
    "modelId": "eu.anthropic.claude-3-7-sonnet-20250219-v1:0",
    "maxTokens": 4096
  }
}
EOF

# Run the agent
python server.py

# Test in another terminal
curl -X POST http://localhost:8080/invoke \
  -H "Content-Type: application/json" \
  -d '{"query": "Hello"}'
```

## Examples

See [examples/agents/](../examples/agents/) for complete agent implementations:

- `default/` - Default agent runner
- `aws-finops/` - FinOps analysis agent
- `aws-docs/` - AWS documentation search
- `engineering-artist/` - Diagram generation
