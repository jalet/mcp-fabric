#!/usr/bin/env python3
"""Task Orchestrator Agent Server.

This agent orchestrates the Ralph-pattern task execution loop:
1. Reads PRD and identifies the next incomplete task
2. Dispatches tasks to worker agents
3. Runs quality gates after task completion
4. Updates task status and progress
5. Signals completion when all tasks pass
"""

import json
import logging
import os
import subprocess
import sys
from datetime import datetime
from typing import Any

import httpx
from mcp.server import Server
from mcp.server.stdio import stdio_server
from mcp.types import TextContent, Tool

# Configure logging
logging.basicConfig(
    level=logging.DEBUG if os.getenv("DEBUG") else logging.INFO,
    format="%(asctime)s - %(name)s - %(levelname)s - %(message)s",
)
logger = logging.getLogger("task-orchestrator")

# MCP Server instance
server = Server("task-orchestrator")

# In-memory storage for current task context
_current_prd: dict | None = None
_current_progress: str = ""
_worker_endpoint: str = ""


@server.list_tools()
async def list_tools() -> list[Tool]:
    """List available orchestration tools."""
    return [
        Tool(
            name="get_next_task",
            description="Get the next incomplete task from the PRD. Returns the highest-priority task where passes=false.",
            inputSchema={
                "type": "object",
                "properties": {
                    "prd_json": {
                        "type": "string",
                        "description": "The PRD JSON content containing the task list",
                    }
                },
                "required": ["prd_json"],
            },
        ),
        Tool(
            name="dispatch_task",
            description="Send a task to the worker agent for execution. Returns the worker's response.",
            inputSchema={
                "type": "object",
                "properties": {
                    "task_id": {
                        "type": "string",
                        "description": "The ID of the task to execute",
                    },
                    "task_title": {
                        "type": "string",
                        "description": "The title/description of the task",
                    },
                    "acceptance_criteria": {
                        "type": "array",
                        "items": {"type": "string"},
                        "description": "List of acceptance criteria for the task",
                    },
                    "context": {
                        "type": "string",
                        "description": "Additional context to pass to the worker",
                    },
                },
                "required": ["task_id", "task_title", "acceptance_criteria"],
            },
        ),
        Tool(
            name="run_quality_gate",
            description="Execute a quality gate command (tests, linting, etc.) and return the result.",
            inputSchema={
                "type": "object",
                "properties": {
                    "name": {
                        "type": "string",
                        "description": "Name of the quality gate",
                    },
                    "command": {
                        "type": "array",
                        "items": {"type": "string"},
                        "description": "Command and arguments to execute",
                    },
                    "timeout_seconds": {
                        "type": "integer",
                        "description": "Timeout in seconds (default: 300)",
                    },
                },
                "required": ["name", "command"],
            },
        ),
        Tool(
            name="update_task_status",
            description="Update the status of a task in the PRD (mark as passed or failed).",
            inputSchema={
                "type": "object",
                "properties": {
                    "prd_json": {
                        "type": "string",
                        "description": "The current PRD JSON content",
                    },
                    "task_id": {
                        "type": "string",
                        "description": "The ID of the task to update",
                    },
                    "passed": {
                        "type": "boolean",
                        "description": "Whether the task passed (true) or failed (false)",
                    },
                },
                "required": ["prd_json", "task_id", "passed"],
            },
        ),
        Tool(
            name="append_progress",
            description="Append learnings and iteration results to the progress content.",
            inputSchema={
                "type": "object",
                "properties": {
                    "current_progress": {
                        "type": "string",
                        "description": "The current progress content",
                    },
                    "task_id": {
                        "type": "string",
                        "description": "The task ID that was worked on",
                    },
                    "task_title": {
                        "type": "string",
                        "description": "The task title",
                    },
                    "passed": {
                        "type": "boolean",
                        "description": "Whether the task passed",
                    },
                    "learnings": {
                        "type": "string",
                        "description": "What was learned during this iteration",
                    },
                    "changes": {
                        "type": "string",
                        "description": "Description of changes made",
                    },
                },
                "required": ["task_id", "task_title", "passed", "learnings"],
            },
        ),
        Tool(
            name="check_all_complete",
            description="Check if all tasks in the PRD are complete. Returns completion status and summary.",
            inputSchema={
                "type": "object",
                "properties": {
                    "prd_json": {
                        "type": "string",
                        "description": "The PRD JSON content to check",
                    }
                },
                "required": ["prd_json"],
            },
        ),
    ]


@server.call_tool()
async def call_tool(name: str, arguments: dict[str, Any]) -> list[TextContent]:
    """Handle tool calls."""
    logger.info(f"Tool called: {name} with args: {json.dumps(arguments)[:500]}")

    try:
        if name == "get_next_task":
            result = get_next_task(arguments["prd_json"])
        elif name == "dispatch_task":
            result = await dispatch_task(
                arguments["task_id"],
                arguments["task_title"],
                arguments["acceptance_criteria"],
                arguments.get("context", ""),
            )
        elif name == "run_quality_gate":
            result = run_quality_gate(
                arguments["name"],
                arguments["command"],
                arguments.get("timeout_seconds", 300),
            )
        elif name == "update_task_status":
            result = update_task_status(
                arguments["prd_json"],
                arguments["task_id"],
                arguments["passed"],
            )
        elif name == "append_progress":
            result = append_progress(
                arguments.get("current_progress", ""),
                arguments["task_id"],
                arguments["task_title"],
                arguments["passed"],
                arguments["learnings"],
                arguments.get("changes", ""),
            )
        elif name == "check_all_complete":
            result = check_all_complete(arguments["prd_json"])
        else:
            result = {"error": f"Unknown tool: {name}"}

        return [TextContent(type="text", text=json.dumps(result, indent=2))]

    except Exception as e:
        logger.exception(f"Error calling tool {name}")
        return [TextContent(type="text", text=json.dumps({"error": str(e)}))]


def get_next_task(prd_json: str) -> dict:
    """Find the highest-priority incomplete task from the PRD."""
    try:
        prd = json.loads(prd_json)
    except json.JSONDecodeError as e:
        return {"error": f"Invalid PRD JSON: {e}"}

    stories = prd.get("stories", [])
    if not stories:
        return {"error": "No stories found in PRD"}

    # Find incomplete tasks sorted by priority
    incomplete = [s for s in stories if not s.get("passes", False)]

    if not incomplete:
        return {
            "task": None,
            "message": "All tasks are complete!",
            "complete": True,
        }

    # Sort by priority (lower number = higher priority)
    incomplete.sort(key=lambda x: x.get("priority", 999))
    next_task = incomplete[0]

    return {
        "task": {
            "id": next_task.get("id"),
            "title": next_task.get("title"),
            "priority": next_task.get("priority"),
            "acceptanceCriteria": next_task.get("acceptanceCriteria", []),
        },
        "remaining_tasks": len(incomplete),
        "complete": False,
    }


async def dispatch_task(
    task_id: str,
    task_title: str,
    acceptance_criteria: list[str],
    context: str,
) -> dict:
    """Dispatch a task to the worker agent."""
    worker_endpoint = os.getenv("WORKER_ENDPOINT", "")

    if not worker_endpoint:
        return {
            "error": "WORKER_ENDPOINT environment variable not set",
            "task_id": task_id,
            "passed": False,
        }

    # Build the request for the worker
    query = f"""Execute the following task:

## Task: {task_title}
ID: {task_id}

## Acceptance Criteria:
{chr(10).join(f'- {c}' for c in acceptance_criteria)}

{f'## Additional Context:{chr(10)}{context}' if context else ''}

## Instructions:
1. Implement the required changes to meet all acceptance criteria
2. Make minimal, focused changes
3. Document what you changed and any learnings
4. Return a JSON object with:
   - passed: boolean
   - changes: description of changes made
   - learnings: any insights or gotchas discovered
   - error: error message if failed
"""

    try:
        async with httpx.AsyncClient(timeout=300.0) as client:
            response = await client.post(
                f"http://{worker_endpoint}/invoke",
                json={"query": query, "metadata": {"taskId": task_id}},
            )
            response.raise_for_status()
            result = response.json()

            return {
                "task_id": task_id,
                "worker_response": result,
                "dispatched": True,
            }

    except httpx.TimeoutException:
        return {
            "error": "Worker request timed out",
            "task_id": task_id,
            "passed": False,
        }
    except Exception as e:
        return {
            "error": f"Failed to dispatch to worker: {e}",
            "task_id": task_id,
            "passed": False,
        }


def run_quality_gate(name: str, command: list[str], timeout_seconds: int) -> dict:
    """Execute a quality gate command."""
    logger.info(f"Running quality gate '{name}': {' '.join(command)}")

    try:
        result = subprocess.run(
            command,
            capture_output=True,
            text=True,
            timeout=timeout_seconds,
            cwd=os.getenv("WORKSPACE_DIR", "/workspace"),
        )

        passed = result.returncode == 0

        return {
            "name": name,
            "passed": passed,
            "returncode": result.returncode,
            "stdout": result.stdout[:5000] if result.stdout else "",
            "stderr": result.stderr[:5000] if result.stderr else "",
        }

    except subprocess.TimeoutExpired:
        return {
            "name": name,
            "passed": False,
            "error": f"Quality gate timed out after {timeout_seconds} seconds",
        }
    except FileNotFoundError:
        return {
            "name": name,
            "passed": False,
            "error": f"Command not found: {command[0]}",
        }
    except Exception as e:
        return {
            "name": name,
            "passed": False,
            "error": str(e),
        }


def update_task_status(prd_json: str, task_id: str, passed: bool) -> dict:
    """Update the status of a task in the PRD."""
    try:
        prd = json.loads(prd_json)
    except json.JSONDecodeError as e:
        return {"error": f"Invalid PRD JSON: {e}"}

    stories = prd.get("stories", [])
    updated = False

    for story in stories:
        if story.get("id") == task_id:
            story["passes"] = passed
            updated = True
            break

    if not updated:
        return {"error": f"Task '{task_id}' not found in PRD"}

    # Return the updated PRD
    return {
        "updated": True,
        "task_id": task_id,
        "passed": passed,
        "prd_json": json.dumps(prd, indent=2),
    }


def append_progress(
    current_progress: str,
    task_id: str,
    task_title: str,
    passed: bool,
    learnings: str,
    changes: str,
) -> dict:
    """Append iteration results to progress content."""
    timestamp = datetime.utcnow().isoformat() + "Z"
    status = "PASSED" if passed else "FAILED"

    entry = f"""
---

## Iteration - {timestamp}
**Task:** {task_id} - {task_title}
**Status:** {status}
**Changes:**
{changes if changes else 'No changes recorded'}
**Learnings:**
{learnings}
"""

    new_progress = current_progress + entry

    return {
        "appended": True,
        "task_id": task_id,
        "progress": new_progress,
    }


def check_all_complete(prd_json: str) -> dict:
    """Check if all tasks in the PRD are complete."""
    try:
        prd = json.loads(prd_json)
    except json.JSONDecodeError as e:
        return {"error": f"Invalid PRD JSON: {e}"}

    stories = prd.get("stories", [])
    total = len(stories)
    passed = sum(1 for s in stories if s.get("passes", False))
    incomplete = [s.get("id") for s in stories if not s.get("passes", False)]

    all_complete = passed == total and total > 0

    result = {
        "all_complete": all_complete,
        "total_tasks": total,
        "passed_tasks": passed,
        "incomplete_tasks": incomplete,
    }

    if all_complete:
        result["completion_signal"] = "<promise>COMPLETE</promise>"
        result["message"] = "All tasks completed successfully!"

    return result


async def main():
    """Run the MCP server."""
    logger.info("Starting Task Orchestrator Agent")
    logger.info(f"Worker endpoint: {os.getenv('WORKER_ENDPOINT', 'not set')}")
    logger.info(f"Workspace dir: {os.getenv('WORKSPACE_DIR', '/workspace')}")

    async with stdio_server() as (read_stream, write_stream):
        await server.run(
            read_stream,
            write_stream,
            server.create_initialization_options(),
        )


if __name__ == "__main__":
    import asyncio

    asyncio.run(main())
