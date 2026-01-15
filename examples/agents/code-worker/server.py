#!/usr/bin/env python3
"""Code Worker Agent Server.

This agent executes individual tasks from the orchestrator:
1. Receives task with acceptance criteria
2. Implements required changes using filesystem tools
3. Returns structured results with changes and learnings
"""

import json
import logging
import os
import subprocess
from pathlib import Path
from typing import Any

from mcp.server import Server
from mcp.server.stdio import stdio_server
from mcp.types import TextContent, Tool

# Configure logging
logging.basicConfig(
    level=logging.DEBUG if os.getenv("DEBUG") else logging.INFO,
    format="%(asctime)s - %(name)s - %(levelname)s - %(message)s",
)
logger = logging.getLogger("code-worker")

# MCP Server instance
server = Server("code-worker")

# Workspace directory
WORKSPACE_DIR = Path(os.getenv("WORKSPACE_DIR", "/workspace"))


@server.list_tools()
async def list_tools() -> list[Tool]:
    """List available code manipulation tools."""
    return [
        Tool(
            name="read_file",
            description="Read the contents of a file from the workspace.",
            inputSchema={
                "type": "object",
                "properties": {
                    "path": {
                        "type": "string",
                        "description": "Path to the file (relative to workspace)",
                    },
                    "start_line": {
                        "type": "integer",
                        "description": "Start reading from this line (1-based, optional)",
                    },
                    "end_line": {
                        "type": "integer",
                        "description": "Stop reading at this line (inclusive, optional)",
                    },
                },
                "required": ["path"],
            },
        ),
        Tool(
            name="write_file",
            description="Write content to a file in the workspace. Creates parent directories if needed.",
            inputSchema={
                "type": "object",
                "properties": {
                    "path": {
                        "type": "string",
                        "description": "Path to the file (relative to workspace)",
                    },
                    "content": {
                        "type": "string",
                        "description": "Content to write to the file",
                    },
                },
                "required": ["path", "content"],
            },
        ),
        Tool(
            name="edit_file",
            description="Edit a file by replacing text. Supports regex patterns.",
            inputSchema={
                "type": "object",
                "properties": {
                    "path": {
                        "type": "string",
                        "description": "Path to the file (relative to workspace)",
                    },
                    "old_text": {
                        "type": "string",
                        "description": "Text to find and replace",
                    },
                    "new_text": {
                        "type": "string",
                        "description": "Text to replace with",
                    },
                    "regex": {
                        "type": "boolean",
                        "description": "Treat old_text as a regex pattern (default: false)",
                    },
                },
                "required": ["path", "old_text", "new_text"],
            },
        ),
        Tool(
            name="list_directory",
            description="List files and directories in a path.",
            inputSchema={
                "type": "object",
                "properties": {
                    "path": {
                        "type": "string",
                        "description": "Path to list (relative to workspace, default: root)",
                    },
                    "recursive": {
                        "type": "boolean",
                        "description": "List recursively (default: false)",
                    },
                    "pattern": {
                        "type": "string",
                        "description": "Glob pattern to filter results",
                    },
                },
            },
        ),
        Tool(
            name="search_files",
            description="Search for text patterns across files in the workspace.",
            inputSchema={
                "type": "object",
                "properties": {
                    "pattern": {
                        "type": "string",
                        "description": "Text or regex pattern to search for",
                    },
                    "path": {
                        "type": "string",
                        "description": "Path to search in (relative to workspace, default: root)",
                    },
                    "file_pattern": {
                        "type": "string",
                        "description": "Glob pattern to filter files (e.g., '*.py')",
                    },
                    "regex": {
                        "type": "boolean",
                        "description": "Treat pattern as regex (default: false)",
                    },
                },
                "required": ["pattern"],
            },
        ),
        Tool(
            name="run_command",
            description="Run a shell command in the workspace. Use for build, test, lint commands.",
            inputSchema={
                "type": "object",
                "properties": {
                    "command": {
                        "type": "string",
                        "description": "Command to execute",
                    },
                    "timeout": {
                        "type": "integer",
                        "description": "Timeout in seconds (default: 60)",
                    },
                },
                "required": ["command"],
            },
        ),
        Tool(
            name="create_directory",
            description="Create a directory and any necessary parent directories.",
            inputSchema={
                "type": "object",
                "properties": {
                    "path": {
                        "type": "string",
                        "description": "Path to create (relative to workspace)",
                    },
                },
                "required": ["path"],
            },
        ),
        Tool(
            name="delete_file",
            description="Delete a file from the workspace.",
            inputSchema={
                "type": "object",
                "properties": {
                    "path": {
                        "type": "string",
                        "description": "Path to the file to delete (relative to workspace)",
                    },
                },
                "required": ["path"],
            },
        ),
        Tool(
            name="move_file",
            description="Move or rename a file in the workspace.",
            inputSchema={
                "type": "object",
                "properties": {
                    "source": {
                        "type": "string",
                        "description": "Source path (relative to workspace)",
                    },
                    "destination": {
                        "type": "string",
                        "description": "Destination path (relative to workspace)",
                    },
                },
                "required": ["source", "destination"],
            },
        ),
    ]


@server.call_tool()
async def call_tool(name: str, arguments: dict[str, Any]) -> list[TextContent]:
    """Handle tool calls."""
    logger.info(f"Tool called: {name} with args: {json.dumps(arguments)[:500]}")

    try:
        if name == "read_file":
            result = read_file(
                arguments["path"],
                arguments.get("start_line"),
                arguments.get("end_line"),
            )
        elif name == "write_file":
            result = write_file(arguments["path"], arguments["content"])
        elif name == "edit_file":
            result = edit_file(
                arguments["path"],
                arguments["old_text"],
                arguments["new_text"],
                arguments.get("regex", False),
            )
        elif name == "list_directory":
            result = list_directory(
                arguments.get("path", ""),
                arguments.get("recursive", False),
                arguments.get("pattern"),
            )
        elif name == "search_files":
            result = search_files(
                arguments["pattern"],
                arguments.get("path", ""),
                arguments.get("file_pattern"),
                arguments.get("regex", False),
            )
        elif name == "run_command":
            result = run_command(
                arguments["command"],
                arguments.get("timeout", 60),
            )
        elif name == "create_directory":
            result = create_directory(arguments["path"])
        elif name == "delete_file":
            result = delete_file(arguments["path"])
        elif name == "move_file":
            result = move_file(arguments["source"], arguments["destination"])
        else:
            result = {"error": f"Unknown tool: {name}"}

        return [TextContent(type="text", text=json.dumps(result, indent=2))]

    except Exception as e:
        logger.exception(f"Error calling tool {name}")
        return [TextContent(type="text", text=json.dumps({"error": str(e)}))]


def _resolve_path(path: str) -> Path:
    """Resolve a path relative to the workspace, with security checks."""
    if not path:
        return WORKSPACE_DIR

    # Resolve the path
    resolved = (WORKSPACE_DIR / path).resolve()

    # Security: ensure the resolved path is within the workspace
    try:
        resolved.relative_to(WORKSPACE_DIR.resolve())
    except ValueError:
        raise ValueError(f"Path '{path}' is outside the workspace")

    return resolved


def read_file(path: str, start_line: int | None, end_line: int | None) -> dict:
    """Read a file's contents."""
    try:
        file_path = _resolve_path(path)

        if not file_path.exists():
            return {"error": f"File not found: {path}"}

        if not file_path.is_file():
            return {"error": f"Not a file: {path}"}

        content = file_path.read_text()
        lines = content.splitlines(keepends=True)

        # Apply line range if specified
        if start_line is not None or end_line is not None:
            start = (start_line or 1) - 1  # Convert to 0-based
            end = end_line or len(lines)
            lines = lines[start:end]
            content = "".join(lines)

        return {
            "path": path,
            "content": content,
            "lines": len(lines),
            "size_bytes": len(content.encode()),
        }

    except Exception as e:
        return {"error": str(e)}


def write_file(path: str, content: str) -> dict:
    """Write content to a file."""
    try:
        file_path = _resolve_path(path)

        # Create parent directories if needed
        file_path.parent.mkdir(parents=True, exist_ok=True)

        existed = file_path.exists()
        file_path.write_text(content)

        return {
            "path": path,
            "created": not existed,
            "modified": existed,
            "size_bytes": len(content.encode()),
        }

    except Exception as e:
        return {"error": str(e)}


def edit_file(path: str, old_text: str, new_text: str, regex: bool) -> dict:
    """Edit a file by replacing text."""
    import re

    try:
        file_path = _resolve_path(path)

        if not file_path.exists():
            return {"error": f"File not found: {path}"}

        content = file_path.read_text()

        if regex:
            new_content, count = re.subn(old_text, new_text, content)
        else:
            count = content.count(old_text)
            new_content = content.replace(old_text, new_text)

        if count == 0:
            return {
                "error": "Pattern not found in file",
                "path": path,
                "pattern": old_text[:100],
            }

        file_path.write_text(new_content)

        return {
            "path": path,
            "replacements": count,
            "regex": regex,
        }

    except Exception as e:
        return {"error": str(e)}


def list_directory(path: str, recursive: bool, pattern: str | None) -> dict:
    """List directory contents."""
    import fnmatch

    try:
        dir_path = _resolve_path(path)

        if not dir_path.exists():
            return {"error": f"Directory not found: {path}"}

        if not dir_path.is_dir():
            return {"error": f"Not a directory: {path}"}

        entries = []

        if recursive:
            for item in dir_path.rglob("*"):
                rel_path = str(item.relative_to(WORKSPACE_DIR))
                if pattern and not fnmatch.fnmatch(item.name, pattern):
                    continue
                entries.append({
                    "path": rel_path,
                    "type": "file" if item.is_file() else "directory",
                    "size": item.stat().st_size if item.is_file() else None,
                })
        else:
            for item in dir_path.iterdir():
                if pattern and not fnmatch.fnmatch(item.name, pattern):
                    continue
                entries.append({
                    "name": item.name,
                    "type": "file" if item.is_file() else "directory",
                    "size": item.stat().st_size if item.is_file() else None,
                })

        # Sort by name
        entries.sort(key=lambda x: x.get("name", x.get("path", "")))

        return {
            "path": path or ".",
            "entries": entries[:500],  # Limit results
            "total": len(entries),
        }

    except Exception as e:
        return {"error": str(e)}


def search_files(
    pattern: str,
    path: str,
    file_pattern: str | None,
    regex: bool,
) -> dict:
    """Search for text patterns in files."""
    import fnmatch
    import re

    try:
        search_path = _resolve_path(path)

        if not search_path.exists():
            return {"error": f"Path not found: {path}"}

        matches = []

        # Compile regex if needed
        if regex:
            try:
                compiled = re.compile(pattern)
            except re.error as e:
                return {"error": f"Invalid regex: {e}"}
        else:
            compiled = None

        # Search files
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
                if regex:
                    if compiled.search(line):
                        matches.append({
                            "file": str(file_path.relative_to(WORKSPACE_DIR)),
                            "line": i,
                            "content": line[:200],
                        })
                else:
                    if pattern in line:
                        matches.append({
                            "file": str(file_path.relative_to(WORKSPACE_DIR)),
                            "line": i,
                            "content": line[:200],
                        })

            if len(matches) >= 100:
                break

        return {
            "pattern": pattern,
            "matches": matches[:100],
            "total_matches": len(matches),
            "truncated": len(matches) > 100,
        }

    except Exception as e:
        return {"error": str(e)}


def run_command(command: str, timeout: int) -> dict:
    """Run a shell command."""
    try:
        result = subprocess.run(
            command,
            shell=True,
            capture_output=True,
            text=True,
            timeout=timeout,
            cwd=str(WORKSPACE_DIR),
        )

        return {
            "command": command,
            "returncode": result.returncode,
            "success": result.returncode == 0,
            "stdout": result.stdout[:10000] if result.stdout else "",
            "stderr": result.stderr[:10000] if result.stderr else "",
        }

    except subprocess.TimeoutExpired:
        return {
            "error": f"Command timed out after {timeout} seconds",
            "command": command,
        }
    except Exception as e:
        return {"error": str(e), "command": command}


def create_directory(path: str) -> dict:
    """Create a directory."""
    try:
        dir_path = _resolve_path(path)
        existed = dir_path.exists()
        dir_path.mkdir(parents=True, exist_ok=True)

        return {
            "path": path,
            "created": not existed,
            "already_existed": existed,
        }

    except Exception as e:
        return {"error": str(e)}


def delete_file(path: str) -> dict:
    """Delete a file."""
    try:
        file_path = _resolve_path(path)

        if not file_path.exists():
            return {"error": f"File not found: {path}"}

        if file_path.is_dir():
            return {"error": f"Cannot delete directory with this tool: {path}"}

        file_path.unlink()

        return {
            "path": path,
            "deleted": True,
        }

    except Exception as e:
        return {"error": str(e)}


def move_file(source: str, destination: str) -> dict:
    """Move or rename a file."""
    import shutil

    try:
        src_path = _resolve_path(source)
        dst_path = _resolve_path(destination)

        if not src_path.exists():
            return {"error": f"Source not found: {source}"}

        # Create parent directories if needed
        dst_path.parent.mkdir(parents=True, exist_ok=True)

        shutil.move(str(src_path), str(dst_path))

        return {
            "source": source,
            "destination": destination,
            "moved": True,
        }

    except Exception as e:
        return {"error": str(e)}


async def main():
    """Run the MCP server."""
    logger.info("Starting Code Worker Agent")
    logger.info(f"Workspace dir: {WORKSPACE_DIR}")

    async with stdio_server() as (read_stream, write_stream):
        await server.run(
            read_stream,
            write_stream,
            server.create_initialization_options(),
        )


if __name__ == "__main__":
    import asyncio

    asyncio.run(main())
