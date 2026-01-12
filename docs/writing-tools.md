# Writing Tools

This guide explains how to build tool packages for MCP Fabric agents.

## Overview

Tool packages bundle Python functions into OCI images that agents can load at runtime. Each tool is a decorated Python function that becomes available to the AI agent.

## Tool Package Structure

```
my-tools/
├── pyproject.toml
├── my_tools/
│   ├── __init__.py
│   └── tools.py
└── Dockerfile
```

## Creating Tools

### pyproject.toml

```toml
[project]
name = "my-tools"
version = "0.1.0"
requires-python = ">=3.12"
dependencies = [
    "strands-agents",
]

[tool.setuptools.packages.find]
where = ["."]
```

### tools.py

Use the `@tool` decorator from Strands to define tools:

```python
from strands import tool

@tool
def reverse_string(text: str) -> str:
    """Reverse a string.

    Args:
        text: The string to reverse
    """
    return text[::-1]

@tool
def count_words(text: str) -> int:
    """Count words in text.

    Args:
        text: The text to count words in
    """
    return len(text.split())

@tool
def to_uppercase(text: str) -> str:
    """Convert text to uppercase.

    Args:
        text: The text to convert
    """
    return text.upper()

# Export all tools
__all__ = ["reverse_string", "count_words", "to_uppercase"]
```

### __init__.py

```python
from .tools import *
```

### Dockerfile

```dockerfile
FROM python:3.12-slim

WORKDIR /tools

COPY pyproject.toml .
COPY my_tools/ my_tools/

RUN pip install -e .

# Tool packages are copied to agents via init container
# The agent loads tools from /tools/{package_name}
```

## Tool Decorator

The `@tool` decorator converts a function into an agent-callable tool. Key requirements:

### Docstrings

Include a clear description and argument documentation:

```python
@tool
def search_database(query: str, limit: int = 10) -> list[dict]:
    """Search the database for matching records.

    Use this tool when the user asks about finding or searching data.

    Args:
        query: Search query string
        limit: Maximum number of results to return (default: 10)
    """
    # Implementation
```

### Type Hints

Always include type hints for parameters and return values:

```python
@tool
def calculate_cost(amount: float, rate: float) -> float:
    """Calculate total cost with tax rate."""
    return amount * (1 + rate)
```

### Return Values

Return JSON-serializable values:

```python
@tool
def get_user_info(user_id: str) -> dict:
    """Get user information."""
    return {
        "id": user_id,
        "name": "John Doe",
        "email": "john@example.com"
    }
```

## Building the Tool Package

```bash
# Build image
docker build -t my-registry/my-tools:latest .

# Push to registry
docker push my-registry/my-tools:latest
```

## Registering with Tool CR

Create a Tool CR to make the package available:

```yaml
apiVersion: fabric.jarsater.ai/v1alpha1
kind: Tool
metadata:
  name: my-tools
  namespace: mcp-fabric-agents
spec:
  image: my-registry/my-tools:latest
  entryModule: my_tools.tools
  tools:
    - name: reverse_string
      description: Reverse a string
    - name: count_words
      description: Count words in text
    - name: to_uppercase
      description: Convert text to uppercase
```

## Using in an Agent

Reference the tool package in your Agent CR:

```yaml
apiVersion: fabric.jarsater.ai/v1alpha1
kind: Agent
metadata:
  name: text-assistant
spec:
  prompt: |
    You are a text processing assistant.
    Use the available tools to help users manipulate text.
  toolPackages:
    - name: my-tools
  model:
    provider: bedrock
    modelId: eu.anthropic.claude-3-7-sonnet-20250219-v1:0
```

## How Tool Loading Works

1. The operator sees `toolPackages` in the Agent spec
2. It adds an init container for each tool package
3. The init container copies tools to `/tools/{package_name}/`
4. The agent runtime loads tools from the entry module at startup

## Testing Tools Locally

```python
# test_tools.py
from my_tools.tools import reverse_string, count_words

def test_reverse():
    assert reverse_string("hello") == "olleh"

def test_count():
    assert count_words("hello world") == 2

if __name__ == "__main__":
    test_reverse()
    test_count()
    print("All tests passed!")
```

## Best Practices

### Clear Descriptions

The docstring is used by the LLM to decide when to call the tool:

```python
# Good - clear when to use
@tool
def get_stock_price(symbol: str) -> float:
    """Get the current stock price for a ticker symbol.

    Use this when the user asks about stock prices, market data,
    or wants to know how much a stock is worth.

    Args:
        symbol: Stock ticker symbol (e.g., AAPL, GOOGL)
    """

# Bad - vague description
@tool
def stock(s: str) -> float:
    """Get stock."""
```

### Error Handling

Return errors as part of the result, don't raise exceptions:

```python
@tool
def divide(a: float, b: float) -> dict:
    """Divide two numbers."""
    if b == 0:
        return {"error": "Cannot divide by zero"}
    return {"result": a / b}
```

### Idempotency

Tools should be safe to retry:

```python
@tool
def get_user(user_id: str) -> dict:
    """Get user by ID (safe to call multiple times)."""
    return db.get_user(user_id)
```

### Minimal Dependencies

Keep tool packages lightweight. Only include necessary dependencies.

## Examples

See [examples/tools/](../examples/tools/) for complete implementations:

- `string-tools/` - String manipulation utilities
