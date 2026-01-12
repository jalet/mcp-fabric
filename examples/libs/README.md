# Shared Libraries

This directory contains shared libraries that can be used across multiple agents.

## Available Libraries

| Library | Description |
|---------|-------------|
| `agent-libs` | Common utilities including JSON logging and Gunicorn configuration |

## agent-libs

Provides:
- `agent_libs.logging` - JSON structured logging matching Go's zap format
- `agent_libs.gunicorn` - Custom Gunicorn logger for JSON output

### Usage

Add as a dependency in your agent's `pyproject.toml`:

```toml
dependencies = [
    "agent-libs",
]
```

Or install from the container:

```dockerfile
COPY --from=ghcr.io/jarsater/agent-libs:latest /app /tools/agent-libs
```

### JSON Logging

```python
from agent_libs.logging import setup_logging

logger = setup_logging("my-agent")
logger.info("message with lowercase first letter")
```

Output format matches Go's zap logger:
```json
{"ts": "2024-01-15T10:30:00.000Z", "level": "info", "pid": 1, "msg": "message"}
```

### Gunicorn Integration

```bash
gunicorn --logger-class agent_libs.gunicorn.JSONLogger server:app
```

## Building

```bash
make docker-build
```

## Creating a New Library

1. Create directory structure:
   ```
   my-lib/
   ├── pyproject.toml
   ├── Dockerfile
   └── my_lib/
       ├── __init__.py
       └── module.py
   ```

2. Build and push:
   ```bash
   docker build -t my-lib:latest .
   docker push my-registry/my-lib:latest
   ```

3. Use in agents via multi-stage build or init container.
