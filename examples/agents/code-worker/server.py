#!/usr/bin/env python3
"""Code Worker Agent Server - Strands LLM-powered version.

This agent executes individual tasks from the orchestrator using an LLM:
1. Receives task with acceptance criteria
2. Uses LLM to plan and execute changes using filesystem and git tools
3. Returns structured results with changes and learnings
"""

import fnmatch
import json
import os
import re
import shutil
import subprocess
import time
import uuid
from pathlib import Path

import httpx
from flask import Flask, request, jsonify
from strands import Agent, tool
from strands.models import BedrockModel

# Try to use agent_libs if available (injected via init container)
try:
    from agent_libs import setup_json_logging, get_logger
    setup_json_logging()
    logger = get_logger("code-worker")
except ImportError:
    import logging
    logging.basicConfig(
        level=logging.DEBUG if os.getenv("DEBUG") else logging.INFO,
        format="%(asctime)s - %(name)s - %(levelname)s - %(message)s",
    )
    logger = logging.getLogger("code-worker")

app = Flask(__name__)

# Configuration
WORKSPACE_DIR = Path(os.getenv("WORKSPACE_DIR", "/workspace"))
GIT_USERNAME = os.getenv("GIT_USERNAME", "")
GIT_TOKEN = os.getenv("GIT_TOKEN", "")
GIT_AUTHOR_NAME = os.getenv("GIT_AUTHOR_NAME", "MCP Fabric Task")
GIT_AUTHOR_EMAIL = os.getenv("GIT_AUTHOR_EMAIL", "task@mcp-fabric.local")
MODEL_ID = os.getenv("MODEL_ID", "amazon.nova-lite-v1:0")
MAX_TOKENS = int(os.getenv("MAX_TOKENS", "8192"))

# Global agent instance
agent = None

SYSTEM_PROMPT = """You are a Code Worker that implements specific tasks assigned by the orchestrator.

Your Purpose:
Execute focused, single-task code changes based on clear acceptance criteria.
Make minimal, targeted modifications to achieve the goal.

Available Tools:
- read_file: Read file contents
- write_file: Create or overwrite files
- edit_file: Make targeted text replacements
- list_directory: Explore the codebase structure
- search_files: Find code patterns and usages
- run_command: Execute build, test, or other commands
- create_directory: Create new directories
- delete_file: Remove files
- move_file: Move or rename files
- git_status: Check repository status
- git_add: Stage files for commit
- git_commit: Commit staged changes (for incremental commits within tasks)

Workflow:
1. Understand the task and acceptance criteria
2. Explore the codebase to understand existing patterns
3. Plan your changes
4. Implement changes incrementally
5. Verify your changes meet the criteria
6. Document what you changed

Rules:
- Stay focused on the assigned task only
- Don't refactor unrelated code
- Follow existing code patterns and conventions
- Make minimal changes to achieve the goal
- Document any gotchas or learnings discovered

Response Format:
After completing your work, respond with a JSON object:
{
  "passed": true/false,
  "changes": "Description of changes made",
  "learnings": "Insights or gotchas discovered",
  "error": "Error message if failed (omit if passed)"
}
"""


# ==============================================================================
# Helper Functions
# ==============================================================================


def _resolve_path(path: str) -> Path:
    """Resolve a path relative to the workspace, with security checks."""
    if not path:
        return WORKSPACE_DIR

    resolved = (WORKSPACE_DIR / path).resolve()

    try:
        resolved.relative_to(WORKSPACE_DIR.resolve())
    except ValueError:
        raise ValueError(f"Path '{path}' is outside the workspace")

    return resolved


def _run_git(*args: str, check: bool = True) -> subprocess.CompletedProcess:
    """Run a git command in the workspace."""
    cmd = ["git", *args]
    logger.debug(f"Running: {' '.join(cmd)}")

    result = subprocess.run(
        cmd,
        cwd=str(WORKSPACE_DIR),
        capture_output=True,
        text=True,
    )

    if check and result.returncode != 0:
        raise RuntimeError(f"Git command failed: {result.stderr}")

    return result


# ==============================================================================
# Strands Tools
# ==============================================================================


@tool
def read_file(path: str, start_line: int = None, end_line: int = None) -> str:
    """Read the contents of a file from the workspace.

    Args:
        path: Path to the file relative to workspace
        start_line: Optional starting line number (1-indexed)
        end_line: Optional ending line number

    Returns:
        The file contents or an error message
    """
    try:
        file_path = _resolve_path(path)

        if not file_path.exists():
            return f"Error: File not found: {path}"

        if not file_path.is_file():
            return f"Error: Not a file: {path}"

        content = file_path.read_text()
        lines = content.splitlines(keepends=True)

        if start_line is not None or end_line is not None:
            start = (start_line or 1) - 1
            end = end_line or len(lines)
            lines = lines[start:end]
            content = "".join(lines)

        return content

    except Exception as e:
        return f"Error: {e}"


@tool
def write_file(path: str, content: str) -> str:
    """Write content to a file, creating directories if needed.

    Args:
        path: Path to the file relative to workspace
        content: Content to write to the file

    Returns:
        Success message or error
    """
    try:
        file_path = _resolve_path(path)
        file_path.parent.mkdir(parents=True, exist_ok=True)

        existed = file_path.exists()
        file_path.write_text(content)

        action = "Modified" if existed else "Created"
        return f"{action} file: {path} ({len(content)} bytes)"

    except Exception as e:
        return f"Error: {e}"


@tool
def edit_file(path: str, old_text: str, new_text: str) -> str:
    """Edit a file by replacing text.

    Args:
        path: Path to the file relative to workspace
        old_text: Text to find and replace
        new_text: Text to replace with

    Returns:
        Success message with replacement count or error
    """
    try:
        file_path = _resolve_path(path)

        if not file_path.exists():
            return f"Error: File not found: {path}"

        content = file_path.read_text()
        count = content.count(old_text)

        if count == 0:
            return f"Error: Pattern not found in {path}"

        new_content = content.replace(old_text, new_text)
        file_path.write_text(new_content)

        return f"Replaced {count} occurrence(s) in {path}"

    except Exception as e:
        return f"Error: {e}"


@tool
def list_directory(path: str = "", recursive: bool = False, pattern: str = None) -> str:
    """List files and directories in the workspace.

    Args:
        path: Path relative to workspace (default: workspace root)
        recursive: If True, list recursively
        pattern: Optional glob pattern to filter files

    Returns:
        List of files and directories
    """
    try:
        dir_path = _resolve_path(path)

        if not dir_path.exists():
            return f"Error: Directory not found: {path}"

        if not dir_path.is_dir():
            return f"Error: Not a directory: {path}"

        entries = []

        if recursive:
            for item in dir_path.rglob("*"):
                if pattern and not fnmatch.fnmatch(item.name, pattern):
                    continue
                rel_path = str(item.relative_to(WORKSPACE_DIR))
                entry_type = "dir" if item.is_dir() else "file"
                entries.append(f"{entry_type}: {rel_path}")
        else:
            for item in sorted(dir_path.iterdir()):
                if pattern and not fnmatch.fnmatch(item.name, pattern):
                    continue
                entry_type = "dir" if item.is_dir() else "file"
                entries.append(f"{entry_type}: {item.name}")

        if not entries:
            return "Directory is empty" if not pattern else "No matching files"

        return "\n".join(entries[:100])

    except Exception as e:
        return f"Error: {e}"


@tool
def search_files(pattern: str, path: str = "", file_pattern: str = None) -> str:
    """Search for text patterns in files.

    Args:
        pattern: Text pattern to search for
        path: Path to search in (default: workspace root)
        file_pattern: Optional glob pattern to filter files (e.g., "*.py")

    Returns:
        Matching lines with file paths and line numbers
    """
    try:
        search_path = _resolve_path(path)

        if not search_path.exists():
            return f"Error: Path not found: {path}"

        matches = []
        files = search_path.rglob("*") if search_path.is_dir() else [search_path]

        for file_path in files:
            if not file_path.is_file():
                continue

            if file_pattern and not fnmatch.fnmatch(file_path.name, file_pattern):
                continue

            try:
                content = file_path.read_text()
            except (UnicodeDecodeError, PermissionError):
                continue

            lines = content.splitlines()

            for i, line in enumerate(lines, 1):
                if pattern in line:
                    rel_path = str(file_path.relative_to(WORKSPACE_DIR))
                    matches.append(f"{rel_path}:{i}: {line[:100]}")

            if len(matches) >= 50:
                break

        if not matches:
            return f"No matches found for '{pattern}'"

        result = "\n".join(matches[:50])
        if len(matches) > 50:
            result += f"\n... and more ({len(matches)} total matches)"
        return result

    except Exception as e:
        return f"Error: {e}"


@tool
def run_command(command: str, timeout: int = 60) -> str:
    """Run a shell command in the workspace.

    Args:
        command: Shell command to execute
        timeout: Timeout in seconds (default: 60)

    Returns:
        Command output (stdout and stderr)
    """
    try:
        result = subprocess.run(
            command,
            shell=True,
            capture_output=True,
            text=True,
            timeout=timeout,
            cwd=str(WORKSPACE_DIR),
        )

        output = ""
        if result.stdout:
            output += result.stdout[:5000]
        if result.stderr:
            output += f"\n[stderr]:\n{result.stderr[:2000]}"

        status = "succeeded" if result.returncode == 0 else f"failed (exit code {result.returncode})"
        return f"Command {status}:\n{output}" if output else f"Command {status} (no output)"

    except subprocess.TimeoutExpired:
        return f"Error: Command timed out after {timeout} seconds"
    except Exception as e:
        return f"Error: {e}"


@tool
def create_directory(path: str) -> str:
    """Create a directory in the workspace.

    Args:
        path: Path to create relative to workspace

    Returns:
        Success message or error
    """
    try:
        dir_path = _resolve_path(path)
        existed = dir_path.exists()
        dir_path.mkdir(parents=True, exist_ok=True)

        if existed:
            return f"Directory already exists: {path}"
        return f"Created directory: {path}"

    except Exception as e:
        return f"Error: {e}"


@tool
def delete_file(path: str) -> str:
    """Delete a file from the workspace.

    Args:
        path: Path to the file to delete

    Returns:
        Success message or error
    """
    try:
        file_path = _resolve_path(path)

        if not file_path.exists():
            return f"Error: File not found: {path}"

        if file_path.is_dir():
            return f"Error: Cannot delete directory with this tool: {path}"

        file_path.unlink()
        return f"Deleted file: {path}"

    except Exception as e:
        return f"Error: {e}"


@tool
def move_file(source: str, destination: str) -> str:
    """Move or rename a file in the workspace.

    Args:
        source: Source path
        destination: Destination path

    Returns:
        Success message or error
    """
    try:
        src_path = _resolve_path(source)
        dst_path = _resolve_path(destination)

        if not src_path.exists():
            return f"Error: Source not found: {source}"

        dst_path.parent.mkdir(parents=True, exist_ok=True)
        shutil.move(str(src_path), str(dst_path))

        return f"Moved {source} to {destination}"

    except Exception as e:
        return f"Error: {e}"


@tool
def git_status() -> str:
    """Get the current git repository status.

    Returns:
        Repository status including branch, modified files, etc.
    """
    try:
        result = _run_git("rev-parse", "--git-dir", check=False)
        if result.returncode != 0:
            return "Error: Not a git repository"

        branch_result = _run_git("branch", "--show-current", check=False)
        branch = branch_result.stdout.strip() or "(detached)"

        status_result = _run_git("status", "--porcelain", check=False)

        output = f"Branch: {branch}\n"
        if status_result.stdout.strip():
            output += f"Changes:\n{status_result.stdout}"
        else:
            output += "Working tree clean"

        return output

    except Exception as e:
        return f"Error: {e}"


@tool
def git_add(paths: str) -> str:
    """Stage files for commit.

    Args:
        paths: Space-separated list of paths to stage (use "." for all)

    Returns:
        Success message or error
    """
    try:
        path_list = paths.split()
        _run_git("add", *path_list)

        return f"Staged: {paths}"

    except Exception as e:
        return f"Error: {e}"


@tool
def git_commit(message: str) -> str:
    """Commit staged changes.

    Args:
        message: Commit message

    Returns:
        Success message with commit SHA or error
    """
    try:
        diff_result = _run_git("diff", "--cached", "--name-only", check=False)
        if not diff_result.stdout.strip():
            return "Error: No staged changes to commit"

        _run_git("commit", "-m", message)

        sha_result = _run_git("rev-parse", "--short", "HEAD", check=False)
        sha = sha_result.stdout.strip()

        return f"Committed: {sha} - {message}"

    except Exception as e:
        return f"Error: {e}"


# ==============================================================================
# Agent Setup
# ==============================================================================


def create_agent():
    """Create the Strands agent with Bedrock model and tools."""
    global agent

    logger.info(f"Creating Bedrock model: {MODEL_ID}")
    model = BedrockModel(
        model_id=MODEL_ID,
        max_tokens=MAX_TOKENS,
    )

    tools = [
        read_file,
        write_file,
        edit_file,
        list_directory,
        search_files,
        run_command,
        create_directory,
        delete_file,
        move_file,
        git_status,
        git_add,
        git_commit,
    ]

    agent = Agent(
        model=model,
        system_prompt=SYSTEM_PROMPT,
        tools=tools,
    )

    logger.info(f"Created Strands agent with {len(tools)} tools")
    return agent


# Initialize on startup
try:
    create_agent()
except Exception as e:
    logger.error(f"Failed to create agent: {e}")


# ==============================================================================
# HTTP Endpoints
# ==============================================================================


@app.route("/healthz")
def healthz():
    """Health check endpoint."""
    return jsonify({"status": "ok", "agent_ready": agent is not None})


@app.route("/invoke", methods=["POST"])
def invoke():
    """Handle task execution requests."""
    request_id = request.headers.get("X-Request-ID", str(uuid.uuid4())[:8])
    start_time = time.time()

    logger.info(f"[{request_id}] incoming request from {request.remote_addr}")

    if agent is None:
        logger.error(f"[{request_id}] agent not initialized")
        return jsonify({
            "success": False,
            "error": "Agent not initialized",
        }), 503

    data = request.get_json() or {}
    query = data.get("query", "")

    if not query:
        return jsonify({
            "success": False,
            "error": "Missing 'query' field",
        }), 400

    query_preview = query[:100].replace('\n', ' ') + "..." if len(query) > 100 else query
    logger.info(f"[{request_id}] query: {query_preview}")

    try:
        # Invoke the Strands agent
        logger.info(f"[{request_id}] invoking agent...")
        response = agent(query)

        elapsed = time.time() - start_time
        response_str = str(response)
        response_preview = response_str[:100].replace('\n', ' ') + "..."
        logger.info(f"[{request_id}] completed in {elapsed:.2f}s, response: {response_preview}")

        # Try to parse JSON from response
        result = {"response": response_str}
        try:
            # Look for JSON in the response
            json_match = re.search(r'\{[^{}]*"passed"[^{}]*\}', response_str, re.DOTALL)
            if json_match:
                parsed = json.loads(json_match.group())
                result = parsed
        except (json.JSONDecodeError, AttributeError):
            pass

        return jsonify({
            "success": True,
            "result": result,
        })

    except Exception as e:
        elapsed = time.time() - start_time
        logger.error(f"[{request_id}] failed after {elapsed:.2f}s: {e}")
        return jsonify({
            "success": False,
            "error": str(e),
            "result": {
                "passed": False,
                "error": str(e),
            },
        }), 500


if __name__ == "__main__":
    logger.info("Starting Code Worker Agent")
    logger.info(f"Workspace dir: {WORKSPACE_DIR}")
    logger.info(f"Model: {MODEL_ID}")
    app.run(host="0.0.0.0", port=8080)
