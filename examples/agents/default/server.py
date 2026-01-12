#!/usr/bin/env python3
"""Default Strands agent runner that reads configuration from CRD spec.

Supports loading tools from ToolPackages via init containers that extract
Python modules to /tools/ (added to PYTHONPATH).
"""

import json
import os
import time
import uuid
from flask import Flask, request, jsonify
from strands import Agent
from strands.models import BedrockModel
from tool_loader import load_tools_from_config
from agent_libs import setup_json_logging, get_logger

# Configure JSON logging
setup_json_logging()
logger = get_logger("default-agent")

app = Flask(__name__)

# Load config from mounted ConfigMap/Secret
config_path = os.environ.get("AGENT_CONFIG_PATH", "/etc/agent/config/agent.json")
config = {}
agent = None
agent_name = os.environ.get("AGENT_NAME", "unknown")
model_id = "unknown"
provider = "bedrock"


def load_config():
    """Load agent configuration from the mounted config file."""
    global config
    try:
        with open(config_path) as f:
            config = json.load(f)
        logger.info(f"loaded config from {config_path}")
        return True
    except Exception as e:
        logger.error(f"could not load config: {e}")
        return False


def create_model():
    """Create the Bedrock model based on config."""
    model_config = config.get("model", {})
    model_id = model_config.get("modelId", "us.anthropic.claude-3-5-sonnet-20241022-v2:0")
    max_tokens = model_config.get("maxTokens", 4096)

    logger.info(f"creating bedrock model: {model_id}, max_tokens={max_tokens}")

    # AWS credentials and region are provided via environment variables
    # (AWS_ACCESS_KEY_ID, AWS_SECRET_ACCESS_KEY, AWS_DEFAULT_REGION)
    return BedrockModel(
        model_id=model_id,
        max_tokens=max_tokens,
    )


def create_agent():
    """Create the Strands agent with prompt from CRD spec and tools from ToolPackages."""
    global agent, model_id

    # Read system prompt from CRD spec
    system_prompt = config.get("prompt", "You are a helpful assistant.")
    prompt_preview = system_prompt[:100].replace('\n', ' ') + "..." if len(system_prompt) > 100 else system_prompt

    # Get model ID for metrics
    model_id = config.get("model", {}).get("modelId", "us.anthropic.claude-3-5-sonnet-20241022-v2:0")

    # Create the model
    model = create_model()

    # Load tools from ToolPackages (if any)
    tools = load_tools_from_config(config)

    # Create the agent with the system prompt and tools
    agent = Agent(
        model=model,
        system_prompt=system_prompt,
        tools=tools if tools else None,
    )

    logger.info(f"created strands agent with prompt: {prompt_preview}")
    if tools:
        logger.info(f"agent has {len(tools)} tools from ToolPackages")
    return agent


# Initialize on startup
if load_config():
    try:
        create_agent()
    except Exception as e:
        logger.error(f"failed to create agent: {e}")


@app.route("/healthz")
def healthz():
    """Health check endpoint."""
    return jsonify({"status": "ok", "agent_ready": agent is not None})


@app.route("/invoke", methods=["POST"])
def run():
    """Handle agent invocation requests."""
    # Generate request ID for tracing
    request_id = request.headers.get("X-Request-ID", str(uuid.uuid4())[:8])
    start_time = time.time()

    # Log incoming request
    client_ip = request.remote_addr
    logger.info(f"[{request_id}] incoming request from {client_ip}")

    if agent is None:
        logger.error(f"[{request_id}] agent not initialized")
        return jsonify({
            "success": False,
            "error": "Agent not initialized",
        }), 503

    data = request.get_json() or {}
    query = data.get("query", "")
    tenant_id = data.get("tenantId", "unknown")
    correlation_id = data.get("correlationId", request_id)

    if not query:
        logger.warning(f"[{request_id}] missing query field")
        return jsonify({
            "success": False,
            "error": "Missing 'query' field",
        }), 400

    # Log query details
    query_preview = query[:100].replace('\n', ' ') + "..." if len(query) > 100 else query
    logger.info(f"[{request_id}] tenant={tenant_id} correlation={correlation_id} query={query_preview}")

    try:
        # Invoke the Strands agent
        logger.info(f"[{request_id}] invoking agent...")
        response = agent(query)

        elapsed = time.time() - start_time
        response_preview = str(response)[:100].replace('\n', ' ') + "..." if len(str(response)) > 100 else str(response)
        logger.info(f"[{request_id}] completed in {elapsed:.2f}s, response={response_preview}")

        return jsonify({
            "success": True,
            "result": {
                "response": str(response),
                "model": config.get("model", {}).get("modelId", "unknown"),
            },
        })
    except Exception as e:
        elapsed = time.time() - start_time
        logger.error(f"[{request_id}] failed after {elapsed:.2f}s: {e}")

        return jsonify({
            "success": False,
            "error": str(e),
        }), 500


if __name__ == "__main__":
    app.run(host="0.0.0.0", port=8080)
