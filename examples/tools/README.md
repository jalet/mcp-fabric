# Tool Packages

This directory contains Tool packages - bundles of Python functions that can be attached to agents.

## Available Tool Packages

| Package | Image | Description |
|---------|-------|-------------|
| `string-tools` | `ghcr.io/jarsater/string-tools:latest` | String manipulation utilities |

## Creating a New Tool Package

### 1. Directory Structure

```
tools/
└── my-tools/
    ├── pyproject.toml      # Python dependencies
    ├── Dockerfile          # Container build
    └── my_tools/           # Python module (underscore, not hyphen)
        ├── __init__.py
        └── tools.py        # Tool definitions
```

### 2. pyproject.toml

```toml
[project]
name = "my-tools"
version = "0.1.0"
description = "Description of your tools"
requires-python = ">=3.12"
dependencies = [
    "strands-agents",
]

[build-system]
requires = ["hatchling"]
build-backend = "hatchling.build"
```

### 3. tools.py

Use the `@tool` decorator from Strands to define tools:

```python
"""My custom tools using Strands @tool decorator."""

from strands import tool


@tool
def my_function(param1: str, param2: int = 10) -> str:
    """Tool description shown to LLM.

    Args:
        param1: Description of first parameter
        param2: Description of second parameter with default

    Returns:
        Description of return value
    """
    return f"Result: {param1}, {param2}"


@tool
def another_function(data: dict) -> dict:
    """Another tool with complex types.

    Args:
        data: Input dictionary

    Returns:
        Processed dictionary
    """
    return {"processed": True, **data}
```

### 4. Dockerfile

```dockerfile
FROM python:3.12-slim

ENV HOME=/tmp

WORKDIR /app

COPY --from=ghcr.io/astral-sh/uv:latest /uv /usr/local/bin/uv

COPY pyproject.toml .
COPY my_tools/ my_tools/

RUN uv sync --no-dev && \
    chmod -R 755 /app/my_tools

USER 65534:65534

# Tool packages are imported as Python modules
CMD ["python", "-c", "from my_tools.tools import *; print('Tools loaded successfully')"]
```

### 5. __init__.py

Export your tools:

```python
"""My tools package."""

from .tools import my_function, another_function

__all__ = ["my_function", "another_function"]
```

### 6. Build and Test

```bash
# Add to tools/Makefile
MY_TOOLS_IMG ?= ghcr.io/jarsater/my-tools:latest

.PHONY: docker-build-my-tools
docker-build-my-tools:
    docker build --load -t $(MY_TOOLS_IMG) -f my-tools/Dockerfile my-tools/

# Build
cd tools && make docker-build-my-tools

# Test locally
docker run --rm ghcr.io/jarsater/my-tools:latest
```

### 7. Create Tool CRD

```yaml
apiVersion: fabric.jarsater.ai/v1alpha1
kind: Tool
metadata:
  name: my-tools
  namespace: mcp-fabric-agents
spec:
  image: ghcr.io/jarsater/my-tools:latest
  imagePullPolicy: IfNotPresent
  entryModule: my_tools.tools
  tools:
    - name: my_function
      description: Tool description shown to LLM
      inputSchema:
        type: object
        properties:
          param1:
            type: string
            description: Description of first parameter
          param2:
            type: integer
            description: Description of second parameter with default
        required: ["param1"]
    - name: another_function
      description: Another tool with complex types
      inputSchema:
        type: object
        properties:
          data:
            type: object
        required: ["data"]
```

### 8. Reference from Agent

```yaml
apiVersion: fabric.jarsater.ai/v1alpha1
kind: Agent
metadata:
  name: my-agent
  namespace: mcp-fabric-agents
spec:
  toolPackages:
    - name: my-tools
  # ... rest of agent spec
```

## Best Practices

1. **Type Hints**: Always use type hints on function parameters and return values
2. **Docstrings**: Include clear docstrings - the LLM uses these to understand when to call tools
3. **Args Section**: Document each parameter in the Args section of docstrings
4. **Returns Section**: Document what the function returns
5. **Simple Types**: Prefer simple types (str, int, dict, list) for inputs/outputs
6. **Error Handling**: Raise exceptions with clear messages for error cases
7. **Stateless**: Tools should be stateless - no global state between calls

## Tool Discovery

The operator:
1. Loads the tool image as an init container
2. Imports the `entryModule` (e.g., `my_tools.tools`)
3. Discovers all `@tool` decorated functions
4. Exposes them to the agent

Tools declared in `spec.tools` are used for MCP protocol discovery. If not specified, tools are discovered at runtime.
