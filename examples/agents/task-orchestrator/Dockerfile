# Agent using uv slim image with git and gh CLI
FROM ghcr.io/astral-sh/uv:python3.12-bookworm-slim

# Install git and gh CLI
USER root
RUN apt-get update && apt-get install -y --no-install-recommends \
    git \
    curl \
    ca-certificates \
    && curl -fsSL https://cli.github.com/packages/githubcli-archive-keyring.gpg -o /usr/share/keyrings/githubcli-archive-keyring.gpg \
    && chmod go+r /usr/share/keyrings/githubcli-archive-keyring.gpg \
    && echo "deb [arch=$(dpkg --print-architecture) signed-by=/usr/share/keyrings/githubcli-archive-keyring.gpg] https://cli.github.com/packages stable main" > /etc/apt/sources.list.d/github-cli.list \
    && apt-get update \
    && apt-get install -y --no-install-recommends gh \
    && rm -rf /var/lib/apt/lists/*

WORKDIR /app

COPY pyproject.toml .
RUN uv sync --no-dev --no-install-project

COPY server.py .

# Create workspace and home directories with proper permissions
RUN mkdir -p /workspace /home/appuser \
    && chown -R 65532:65532 /app /workspace /home/appuser

ENV PATH="/app/.venv/bin:$PATH"
ENV HOME=/home/appuser
ENV PYTHONUNBUFFERED=1
ENV WORKSPACE_DIR=/workspace

# Gunicorn configuration (for HTTP mode)
ENV PORT=8080
ENV WEB_CONCURRENCY=2
ENV GUNICORN_CMD_ARGS="--timeout 300"

USER 65532:65532
EXPOSE 8080

# Use a shell entrypoint that detects Job vs HTTP mode
# - If TASK_CONFIG is set, run in Job mode (python server.py)
# - Otherwise, run in HTTP mode (gunicorn)
ENTRYPOINT ["/bin/sh", "-c", "if [ -n \"$TASK_CONFIG\" ]; then python server.py; else python -m gunicorn --logger-class agent_libs.gunicorn.JSONLogger server:app; fi"]
