# Example Agents

Reference implementations demonstrating how to build AI agents for MCP Fabric.

## Available Agents

| Agent | Image | Description |
|-------|-------|-------------|
| `default` | `strands-agent-runner` | Default agent runner, reads prompt from Agent CR spec |
| `engineering-artist` | `engineering-artist-agent` | Architecture diagram generation with draw.io output |
| `task-orchestrator` | `task-orchestrator` | Orchestrates Task execution, manages progress, dispatches work to workers |
| `code-worker` | `code-worker` | Implements individual tasks from the orchestrator with code changes |

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
make docker-build-task-orchestrator
make docker-build-code-worker
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

### Task Orchestrator (task-orchestrator)

Orchestrates multi-step Task execution without using an LLM. The orchestrator:

1. Parses the PRD to find incomplete tasks sorted by priority
2. Dispatches tasks to a worker agent (e.g., `code-worker`)
3. Runs quality gates after each task completion
4. Manages Git operations: commit, push, and PR creation

Supports two execution modes:
- **HTTP Service Mode**: Runs as a Flask server for single-iteration requests
- **Job Mode**: Runs as a Kubernetes Job for complete task execution loops

```python
# The orchestrator extracts PRD from the query and processes tasks
def orchestrate(query: str, metadata: dict) -> dict:
    prd = extract_prd_from_query(query)
    task = get_next_task(prd)
    result = dispatch_to_worker(task, context)
    return result
```

### Code Worker (code-worker)

An LLM-powered agent that implements individual tasks using filesystem and Git tools:

```python
@tool
def read_file(path: str) -> str:
    """Read file contents from the workspace."""
    ...

@tool
def write_file(path: str, content: str) -> str:
    """Write content to a file."""
    ...

@tool
def run_command(command: str, timeout: int = 60) -> str:
    """Run shell commands in the workspace."""
    ...

agent = Agent(
    model=BedrockModel(model_id=MODEL_ID),
    system_prompt=SYSTEM_PROMPT,
    tools=[read_file, write_file, edit_file, search_files, ...]
)
```

The code worker provides these tools to the LLM:
- File operations: `read_file`, `write_file`, `edit_file`, `delete_file`, `move_file`
- Directory operations: `list_directory`, `create_directory`
- Search: `search_files` (text pattern search)
- Commands: `run_command` (shell execution)
- Git: `git_status`, `git_add`, `git_commit`

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
