# Code Worker Agent - HTTP service (default) with CLI/Job mode fallback
FROM ghcr.io/astral-sh/uv:python3.12-bookworm-slim

# Install git and GitHub CLI
RUN apt-get update && apt-get install -y --no-install-recommends \
    git \
    curl \
    && curl -fsSL https://cli.github.com/packages/githubcli-archive-keyring.gpg | dd of=/usr/share/keyrings/githubcli-archive-keyring.gpg \
    && chmod go+r /usr/share/keyrings/githubcli-archive-keyring.gpg \
    && echo "deb [arch=$(dpkg --print-architecture) signed-by=/usr/share/keyrings/githubcli-archive-keyring.gpg] https://cli.github.com/packages stable main" | tee /etc/apt/sources.list.d/github-cli.list > /dev/null \
    && apt-get update \
    && apt-get install -y gh \
    && rm -rf /var/lib/apt/lists/*

WORKDIR /app

COPY pyproject.toml .
RUN uv sync --no-dev --no-install-project

# server.py is the HTTP service the orchestrator dispatches to over /invoke.
# worker.py is retained for standalone CLI/Job execution (reads TASK_JSON).
COPY server.py .
COPY worker.py .

# Create workspace directory
RUN mkdir -p /workspace && chown -R 65532:65532 /app /workspace

ENV PATH="/app/.venv/bin:$PATH"
ENV HOME=/tmp
ENV PYTHONUNBUFFERED=1
ENV WORKSPACE_DIR=/workspace

# Gunicorn configuration (HTTP mode). Timeout matches the CRD default
# iterationTimeout (30m = 1800s) so the worker does not abort a long LLM call
# before the orchestrator's per-task timeout elapses. Tasks that set a larger
# iterationTimeout need a worker image with a correspondingly larger timeout.
ENV PORT=8080
ENV WEB_CONCURRENCY=2
ENV GUNICORN_CMD_ARGS="--timeout 1800"

USER 65532:65532
EXPOSE 8080

# Detect mode the same way the orchestrator does:
# - If TASK_JSON is set, run the one-shot CLI/Job worker (python worker.py).
# - Otherwise serve HTTP via gunicorn, which is how the operator deploys the
#   worker Agent and how the orchestrator reaches it.
# The agent_libs JSON logger is only available when injected via the /tools init
# container (standalone Deployment). As a Task sidecar there is no such
# injection, so fall back to gunicorn's default logger when it is absent.
ENTRYPOINT ["/bin/sh", "-c", "if [ -n \"$TASK_JSON\" ]; then exec python worker.py; elif python -c 'import agent_libs.gunicorn' 2>/dev/null; then exec python -m gunicorn --logger-class agent_libs.gunicorn.JSONLogger server:app; else exec python -m gunicorn server:app; fi"]
