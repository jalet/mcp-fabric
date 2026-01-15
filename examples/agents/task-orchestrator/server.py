#!/usr/bin/env python3
"""Task Orchestrator Agent Server - Direct orchestration without LLM.

This agent orchestrates the task execution loop:
1. Parses the PRD to find the next incomplete task
2. Dispatches tasks to worker agents
3. Runs quality gates after task completion
4. Returns structured results for the operator

Supports two modes:
- HTTP Service Mode: Runs as a Flask server (default)
- Job Mode: Runs as a Kubernetes Job when TASK_CONFIG env var is set
"""

import json
import os
import re
import subprocess
import sys
import time
import uuid
from datetime import datetime

import httpx
from flask import Flask, request, jsonify

# Try to use agent_libs if available (injected via init container)
try:
    from agent_libs import setup_json_logging, get_logger
    setup_json_logging()
    logger = get_logger("task-orchestrator")
except ImportError:
    import logging
    logging.basicConfig(
        level=logging.DEBUG if os.getenv("DEBUG") else logging.INFO,
        format="%(asctime)s - %(name)s - %(levelname)s - %(message)s",
    )
    logger = logging.getLogger("task-orchestrator")

app = Flask(__name__)

# Configuration
WORKER_ENDPOINT = os.getenv("WORKER_ENDPOINT", "code-worker.mcp-fabric-agents.svc.cluster.local:8080")
WORKSPACE_DIR = os.getenv("WORKSPACE_DIR", "/workspace")


def extract_prd_from_query(query: str) -> dict | None:
    """Extract PRD JSON from the orchestrator query."""
    # Look for JSON between ```json and ```
    match = re.search(r'```json\s*\n(.*?)\n```', query, re.DOTALL)
    if match:
        try:
            return json.loads(match.group(1))
        except json.JSONDecodeError as e:
            logger.error(f"Failed to parse PRD JSON: {e}")
            return None
    return None


def extract_context_from_query(query: str) -> str:
    """Extract additional context from the query."""
    match = re.search(r'## Additional Context\n(.*?)(?=\n##|\Z)', query, re.DOTALL)
    if match:
        return match.group(1).strip()
    return ""


def get_next_task(prd: dict) -> dict | None:
    """Find the highest-priority incomplete task from the PRD."""
    stories = prd.get("stories", [])
    if not stories:
        return None

    # Find incomplete tasks sorted by priority
    incomplete = [s for s in stories if not s.get("passes", False)]
    if not incomplete:
        return None

    # Sort by priority (lower number = higher priority)
    incomplete.sort(key=lambda x: x.get("priority", 999))
    return incomplete[0]


def dispatch_to_worker(task: dict, context: str) -> dict:
    """Dispatch a task to the worker agent."""
    task_id = task.get("id", "unknown")
    task_title = task.get("title", "Unknown Task")
    acceptance_criteria = task.get("acceptanceCriteria", [])

    logger.info(f"Dispatching task {task_id}: {task_title}")

    # Build the worker query
    worker_query = f"""Execute the following task:

## Task: {task_title}
ID: {task_id}

## Acceptance Criteria:
{chr(10).join(f'- {c}' for c in acceptance_criteria)}

{f'## Additional Context:{chr(10)}{context}' if context else ''}

## Instructions:
1. Implement the required changes to meet all acceptance criteria
2. Make minimal, focused changes
3. Return a JSON object with:
   - passed: boolean (true if all criteria met)
   - changes: description of changes made
   - learnings: any insights or gotchas discovered
   - error: error message if failed
"""

    try:
        with httpx.Client(timeout=300.0) as client:
            response = client.post(
                f"http://{WORKER_ENDPOINT}/invoke",
                json={"query": worker_query, "metadata": {"taskId": task_id}},
            )
            response.raise_for_status()
            result = response.json()

            logger.info(f"Worker response for {task_id}: {result}")
            return {
                "success": True,
                "result": result.get("result", {}),
            }

    except httpx.TimeoutException:
        logger.error(f"Worker request timed out for task {task_id}")
        return {
            "success": False,
            "error": "Worker request timed out",
        }
    except Exception as e:
        logger.error(f"Failed to dispatch to worker: {e}")
        return {
            "success": False,
            "error": str(e),
        }


def run_quality_gates(quality_gates: list[dict]) -> tuple[bool, list[dict]]:
    """Run quality gate commands and return results."""
    results = []
    all_passed = True

    for gate in quality_gates:
        name = gate.get("name", "unnamed")
        command = gate.get("command", [])
        timeout_str = gate.get("timeout", "60s")
        failure_policy = gate.get("failurePolicy", "Fail")

        # Parse timeout (e.g., "30s", "2m")
        timeout_seconds = 60
        if timeout_str.endswith("s"):
            timeout_seconds = int(timeout_str[:-1])
        elif timeout_str.endswith("m"):
            timeout_seconds = int(timeout_str[:-1]) * 60

        logger.info(f"Running quality gate '{name}': {' '.join(command)}")

        try:
            result = subprocess.run(
                command,
                capture_output=True,
                text=True,
                timeout=timeout_seconds,
                cwd=WORKSPACE_DIR,
            )

            passed = result.returncode == 0
            gate_result = {
                "name": name,
                "passed": passed,
                "returncode": result.returncode,
                "stdout": result.stdout[:2000] if result.stdout else "",
                "stderr": result.stderr[:2000] if result.stderr else "",
            }

            if not passed and failure_policy == "Fail":
                all_passed = False

            results.append(gate_result)
            logger.info(f"Quality gate '{name}': {'PASSED' if passed else 'FAILED'}")

        except subprocess.TimeoutExpired:
            gate_result = {
                "name": name,
                "passed": False,
                "error": f"Timed out after {timeout_seconds}s",
            }
            if failure_policy == "Fail":
                all_passed = False
            results.append(gate_result)
            logger.error(f"Quality gate '{name}' timed out")

        except FileNotFoundError:
            gate_result = {
                "name": name,
                "passed": False,
                "error": f"Command not found: {command[0] if command else 'empty'}",
            }
            if failure_policy == "Fail":
                all_passed = False
            results.append(gate_result)
            logger.error(f"Quality gate '{name}' command not found")

        except Exception as e:
            gate_result = {
                "name": name,
                "passed": False,
                "error": str(e),
            }
            if failure_policy == "Fail":
                all_passed = False
            results.append(gate_result)
            logger.error(f"Quality gate '{name}' failed: {e}")

    return all_passed, results


def update_prd_task_status(prd: dict, task_id: str, passed: bool) -> dict:
    """Update the status of a task in the PRD."""
    stories = prd.get("stories", [])
    for story in stories:
        if story.get("id") == task_id:
            story["passes"] = passed
            break
    return prd


def check_all_complete(prd: dict) -> bool:
    """Check if all tasks in the PRD are complete."""
    stories = prd.get("stories", [])
    if not stories:
        return False
    return all(s.get("passes", False) for s in stories)


def orchestrate(query: str, metadata: dict) -> dict:
    """Main orchestration logic - process one iteration."""
    logger.info("Starting orchestration iteration")

    # Extract PRD from query
    prd = extract_prd_from_query(query)
    if prd is None:
        return {
            "passed": False,
            "error": "Failed to extract PRD from query",
            "taskId": "",
            "taskTitle": "",
            "learnings": "",
        }

    # Extract context
    context = extract_context_from_query(query)

    # Check if all tasks are already complete
    if check_all_complete(prd):
        logger.info("All tasks are complete!")
        return {
            "passed": True,
            "taskId": "all-complete",
            "taskTitle": "All Tasks Complete",
            "learnings": "All tasks in the PRD have passed.",
            "complete": True,
            "updatedPrd": prd,
        }

    # Get next task
    task = get_next_task(prd)
    if task is None:
        return {
            "passed": False,
            "error": "No incomplete tasks found in PRD",
            "taskId": "",
            "taskTitle": "",
            "learnings": "",
        }

    task_id = task.get("id", "unknown")
    task_title = task.get("title", "Unknown Task")

    logger.info(f"Processing task {task_id}: {task_title}")

    # Dispatch to worker
    worker_result = dispatch_to_worker(task, context)

    if not worker_result.get("success"):
        return {
            "passed": False,
            "error": worker_result.get("error", "Worker dispatch failed"),
            "taskId": task_id,
            "taskTitle": task_title,
            "learnings": f"Failed to dispatch task to worker: {worker_result.get('error')}",
            "updatedPrd": prd,
        }

    # Extract worker response
    worker_response = worker_result.get("result", {})

    # For now, consider the task passed if worker returned success
    # In a real scenario, we'd run quality gates here
    task_passed = worker_response.get("passed", False) if isinstance(worker_response, dict) else False
    learnings = worker_response.get("learnings", "") if isinstance(worker_response, dict) else str(worker_response)
    changes = worker_response.get("changes", "") if isinstance(worker_response, dict) else ""

    # Update PRD with task status
    updated_prd = update_prd_task_status(prd, task_id, task_passed)

    # Check if this was the last task
    all_complete = check_all_complete(updated_prd)

    return {
        "passed": task_passed,
        "taskId": task_id,
        "taskTitle": task_title,
        "learnings": learnings or f"Task {'completed' if task_passed else 'failed'}. Changes: {changes}",
        "complete": all_complete,
        "updatedPrd": updated_prd,
    }


# ==============================================================================
# HTTP Endpoints
# ==============================================================================


@app.route("/healthz")
def healthz():
    """Health check endpoint."""
    return jsonify({"status": "ok", "agent": "task-orchestrator"})


@app.route("/invoke", methods=["POST"])
def invoke():
    """Handle orchestration requests from the Task controller."""
    request_id = request.headers.get("X-Request-ID", str(uuid.uuid4())[:8])
    start_time = time.time()

    logger.info(f"[{request_id}] incoming orchestration request from {request.remote_addr}")

    data = request.get_json() or {}
    query = data.get("query", "")
    metadata = data.get("metadata", {})

    if not query:
        return jsonify({
            "success": False,
            "error": "Missing 'query' field",
        }), 400

    try:
        # Run orchestration
        result = orchestrate(query, metadata)
        elapsed = time.time() - start_time

        logger.info(f"[{request_id}] orchestration completed in {elapsed:.2f}s, passed={result.get('passed')}")

        return jsonify({
            "success": True,
            "result": result,
        })

    except Exception as e:
        elapsed = time.time() - start_time
        logger.error(f"[{request_id}] orchestration failed after {elapsed:.2f}s: {e}")
        return jsonify({
            "success": False,
            "error": str(e),
            "result": {
                "passed": False,
                "error": str(e),
                "taskId": "",
                "taskTitle": "",
                "learnings": "",
            },
        }), 500


# ==============================================================================
# Job Mode - Full orchestration loop with git finalization
# ==============================================================================


def generate_commit_message(prd: dict, task_name: str) -> str:
    """Generate a commit message summarizing completed tasks."""
    stories = prd.get("stories", [])
    completed = [s for s in stories if s.get("passes", False)]
    total = len(stories)

    title = prd.get("title", task_name)
    if len(completed) == total:
        return f"feat: {title} - all {total} tasks completed\n\nCompleted tasks:\n" + "\n".join(
            f"- {s.get('title', s.get('id'))}" for s in completed
        )
    else:
        return f"wip: {title} - {len(completed)}/{total} tasks completed\n\nCompleted tasks:\n" + "\n".join(
            f"- {s.get('title', s.get('id'))}" for s in completed
        )


def generate_pr_body(git_config: dict, prd: dict, task_name: str) -> str:
    """Generate PR body from template or default."""
    stories = prd.get("stories", [])
    completed = [s for s in stories if s.get("passes", False)]
    total = len(stories)

    # Use custom template if provided
    template = git_config.get("prBody", "")
    if template:
        return template.replace("{task}", task_name).replace("{completed}", str(len(completed))).replace("{total}", str(total))

    # Default PR body
    body = f"""## Summary

This PR was automatically generated by MCP Fabric Task: **{task_name}**

### Progress: {len(completed)}/{total} tasks completed

### Completed Tasks
"""
    for s in completed:
        body += f"- [x] {s.get('title', s.get('id'))}\n"

    incomplete = [s for s in stories if not s.get("passes", False)]
    if incomplete:
        body += "\n### Remaining Tasks\n"
        for s in incomplete:
            body += f"- [ ] {s.get('title', s.get('id'))}\n"

    body += "\n---\n*Generated by [MCP Fabric](https://github.com/jarsater/mcp-fabric)*"
    return body


def finalize_git(git_config: dict, prd: dict, task_name: str) -> dict:
    """Commit, push, and create PR."""
    result = {}
    logger.info("Starting git finalization...")

    try:
        # Check if there are changes to commit
        status_result = subprocess.run(
            ["git", "status", "--porcelain"],
            capture_output=True,
            text=True,
            cwd=WORKSPACE_DIR,
        )

        if not status_result.stdout.strip():
            logger.info("No changes to commit")
            result["noChanges"] = True
            return result

        # Stage all changes
        logger.info("Staging changes...")
        subprocess.run(["git", "add", "-A"], cwd=WORKSPACE_DIR, check=True)

        # Commit changes
        commit_msg = generate_commit_message(prd, task_name)
        logger.info(f"Committing with message: {commit_msg.split(chr(10))[0]}")
        subprocess.run(
            ["git", "commit", "-m", commit_msg],
            cwd=WORKSPACE_DIR,
            check=True,
        )

        # Get commit SHA
        sha_result = subprocess.run(
            ["git", "rev-parse", "HEAD"],
            capture_output=True,
            text=True,
            cwd=WORKSPACE_DIR,
            check=True,
        )
        result["commitSha"] = sha_result.stdout.strip()
        logger.info(f"Committed: {result['commitSha']}")

        # Push if enabled (default: true)
        if git_config.get("autoPush", True):
            logger.info("Pushing to remote...")
            push_result = subprocess.run(
                ["git", "push", "-u", "origin", "HEAD"],
                capture_output=True,
                text=True,
                cwd=WORKSPACE_DIR,
            )
            if push_result.returncode != 0:
                logger.error(f"Push failed: {push_result.stderr}")
                result["pushError"] = push_result.stderr
            else:
                logger.info("Push successful")
                result["pushed"] = True

        # Create PR if enabled (default: true)
        if git_config.get("createPR", True) and result.get("pushed"):
            logger.info("Creating pull request...")
            pr_url = create_pull_request(git_config, prd, task_name)
            if pr_url:
                result["pullRequestUrl"] = pr_url
                logger.info(f"PR created: {pr_url}")
            else:
                logger.warning("Failed to create PR")

    except subprocess.CalledProcessError as e:
        logger.error(f"Git operation failed: {e}")
        result["gitError"] = str(e)
    except Exception as e:
        logger.error(f"Git finalization failed: {e}")
        result["error"] = str(e)

    return result


def create_pull_request(git_config: dict, prd: dict, task_name: str) -> str | None:
    """Create PR using GitHub CLI."""
    try:
        # Get PR title
        title = git_config.get("prTitle", "").replace("{task}", task_name)
        if not title:
            title = f"Task: {task_name}"

        # Get PR body
        body = generate_pr_body(git_config, prd, task_name)

        # Build gh command
        cmd = ["gh", "pr", "create", "--title", title, "--body", body]

        # Add draft flag if enabled (default: true)
        if git_config.get("draftPR", True):
            cmd.append("--draft")

        # Get base branch if specified
        base_branch = git_config.get("baseBranch")
        if base_branch:
            cmd.extend(["--base", base_branch])

        logger.info(f"Running: {' '.join(cmd[:4])}...")  # Don't log full body
        result = subprocess.run(
            cmd,
            capture_output=True,
            text=True,
            cwd=WORKSPACE_DIR,
        )

        if result.returncode == 0:
            # gh pr create outputs the PR URL
            return result.stdout.strip()
        else:
            logger.error(f"gh pr create failed: {result.stderr}")
            return None

    except FileNotFoundError:
        logger.error("gh CLI not found - cannot create PR")
        return None
    except Exception as e:
        logger.error(f"Failed to create PR: {e}")
        return None


def dispatch_to_worker_with_endpoint(task: dict, context: str, worker_endpoint: str) -> dict:
    """Dispatch a task to the worker agent using specified endpoint."""
    task_id = task.get("id", "unknown")
    task_title = task.get("title", "Unknown Task")
    acceptance_criteria = task.get("acceptanceCriteria", [])

    logger.info(f"Dispatching task {task_id}: {task_title} to {worker_endpoint}")

    # Build the worker query
    worker_query = f"""Execute the following task:

## Task: {task_title}
ID: {task_id}

## Acceptance Criteria:
{chr(10).join(f'- {c}' for c in acceptance_criteria)}

{f'## Additional Context:{chr(10)}{context}' if context else ''}

## Instructions:
1. Implement the required changes to meet all acceptance criteria
2. Make minimal, focused changes
3. Return a JSON object with:
   - passed: boolean (true if all criteria met)
   - changes: description of changes made
   - learnings: any insights or gotchas discovered
   - error: error message if failed
"""

    try:
        with httpx.Client(timeout=600.0) as client:  # 10 minute timeout for worker
            response = client.post(
                f"http://{worker_endpoint}/invoke",
                json={"query": worker_query, "metadata": {"taskId": task_id}},
            )
            response.raise_for_status()
            result = response.json()

            logger.info(f"Worker response for {task_id}: passed={result.get('result', {}).get('passed')}")
            return {
                "success": True,
                "result": result.get("result", {}),
            }

    except httpx.TimeoutException:
        logger.error(f"Worker request timed out for task {task_id}")
        return {
            "success": False,
            "error": "Worker request timed out",
        }
    except Exception as e:
        logger.error(f"Failed to dispatch to worker: {e}")
        return {
            "success": False,
            "error": str(e),
        }


def run_job_mode():
    """Run orchestrator as a Job - full loop until completion."""
    logger.info("=" * 60)
    logger.info("Starting Task Orchestrator in Job Mode")
    logger.info("=" * 60)

    # Parse config from environment
    task_config_str = os.getenv("TASK_CONFIG")
    if not task_config_str:
        logger.error("TASK_CONFIG environment variable not set")
        print("ORCHESTRATOR_RESULT:" + json.dumps({"passed": False, "error": "TASK_CONFIG not set"}))
        sys.exit(1)

    try:
        config = json.loads(task_config_str)
    except json.JSONDecodeError as e:
        logger.error(f"Failed to parse TASK_CONFIG: {e}")
        print("ORCHESTRATOR_RESULT:" + json.dumps({"passed": False, "error": f"Invalid TASK_CONFIG: {e}"}))
        sys.exit(1)

    # Extract configuration
    task_name = config.get("taskName", "unknown")
    prd = config.get("prd", {})
    worker_endpoint = config.get("workerEndpoint", WORKER_ENDPOINT)
    git_config = config.get("git")
    quality_gates = config.get("qualityGates", [])
    limits = config.get("limits", {})
    context = config.get("context", "")

    max_iterations = limits.get("maxIterations", 100)
    max_consecutive_failures = limits.get("maxConsecutiveFailures", 3)

    logger.info(f"Task: {task_name}")
    logger.info(f"Worker endpoint: {worker_endpoint}")
    logger.info(f"Max iterations: {max_iterations}")
    logger.info(f"Quality gates: {len(quality_gates)}")
    logger.info(f"Git configured: {git_config is not None}")

    # Initialize tracking
    iteration = 0
    consecutive_failures = 0
    all_learnings = []

    # Main orchestration loop
    while iteration < max_iterations:
        iteration += 1
        logger.info(f"\n{'=' * 40}")
        logger.info(f"ITERATION {iteration}/{max_iterations}")
        logger.info(f"{'=' * 40}")

        # Check if all tasks are complete
        if check_all_complete(prd):
            logger.info("All tasks are complete!")
            break

        # Get next task
        task = get_next_task(prd)
        if task is None:
            logger.info("No incomplete tasks found")
            break

        task_id = task.get("id", "unknown")
        task_title = task.get("title", "Unknown Task")
        logger.info(f"Processing: {task_id} - {task_title}")

        # Dispatch to worker
        worker_result = dispatch_to_worker_with_endpoint(task, context, worker_endpoint)

        if not worker_result.get("success"):
            consecutive_failures += 1
            error_msg = worker_result.get("error", "Unknown error")
            logger.error(f"Worker dispatch failed: {error_msg}")
            all_learnings.append(f"Iteration {iteration}: Task {task_id} failed - {error_msg}")

            if consecutive_failures >= max_consecutive_failures:
                logger.error(f"Max consecutive failures ({max_consecutive_failures}) reached")
                break
            continue

        # Extract worker response
        worker_response = worker_result.get("result", {})
        task_passed = worker_response.get("passed", False) if isinstance(worker_response, dict) else False
        learnings = worker_response.get("learnings", "") if isinstance(worker_response, dict) else str(worker_response)
        changes = worker_response.get("changes", "") if isinstance(worker_response, dict) else ""

        # Run quality gates if task passed and gates are configured
        if task_passed and quality_gates:
            logger.info("Running quality gates...")
            gates_passed, gate_results = run_quality_gates(quality_gates)
            if not gates_passed:
                logger.warning("Quality gates failed - marking task as not passed")
                task_passed = False
                learnings += f"\nQuality gates failed: {gate_results}"

        # Update PRD with task status
        if task_passed:
            consecutive_failures = 0
            prd = update_prd_task_status(prd, task_id, True)
            logger.info(f"Task {task_id} PASSED")
        else:
            consecutive_failures += 1
            logger.warning(f"Task {task_id} FAILED (consecutive failures: {consecutive_failures})")

            if consecutive_failures >= max_consecutive_failures:
                logger.error(f"Max consecutive failures ({max_consecutive_failures}) reached")
                break

        # Track learnings
        learning_entry = f"Iteration {iteration}: Task {task_id} {'passed' if task_passed else 'failed'}"
        if learnings:
            learning_entry += f" - {learnings[:200]}"
        if changes:
            learning_entry += f" | Changes: {changes[:200]}"
        all_learnings.append(learning_entry)

        # Log progress
        stories = prd.get("stories", [])
        completed = sum(1 for s in stories if s.get("passes", False))
        logger.info(f"Progress: {completed}/{len(stories)} tasks completed")

    # Final status
    all_complete = check_all_complete(prd)
    stories = prd.get("stories", [])
    completed_count = sum(1 for s in stories if s.get("passes", False))

    logger.info(f"\n{'=' * 60}")
    logger.info("ORCHESTRATION COMPLETE")
    logger.info(f"Status: {'SUCCESS' if all_complete else 'INCOMPLETE'}")
    logger.info(f"Completed: {completed_count}/{len(stories)} tasks")
    logger.info(f"Iterations: {iteration}")
    logger.info(f"{'=' * 60}")

    # Build final result
    final_result = {
        "passed": all_complete,
        "completedTasks": completed_count,
        "totalTasks": len(stories),
        "iterations": iteration,
        "prd": prd,
        "learnings": "\n".join(all_learnings[-10:]),  # Last 10 learnings
    }

    # Git finalization if all tasks complete and git is configured
    if git_config and all_complete:
        logger.info("\nStarting git finalization...")
        git_result = finalize_git(git_config, prd, task_name)
        final_result.update(git_result)
    elif git_config and not all_complete:
        logger.info("Skipping git finalization - not all tasks complete")

    # Output final result for controller extraction
    # This marker is used by the controller to find the result in logs
    print("ORCHESTRATOR_RESULT:" + json.dumps(final_result))
    logger.info("Final result output complete")

    # Exit with appropriate code
    sys.exit(0 if all_complete else 1)


if __name__ == "__main__":
    # Check if running in Job mode (TASK_CONFIG env var is set)
    if os.getenv("TASK_CONFIG"):
        run_job_mode()
    else:
        # HTTP service mode
        logger.info("Starting Task Orchestrator Agent (HTTP Mode)")
        logger.info(f"Worker endpoint: {WORKER_ENDPOINT}")
        logger.info(f"Workspace dir: {WORKSPACE_DIR}")
        app.run(host="0.0.0.0", port=8080)
