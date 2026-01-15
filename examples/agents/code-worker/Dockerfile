# Code Worker Agent - CLI mode for Kubernetes Job execution
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

COPY worker.py .

# Create workspace directory
RUN mkdir -p /workspace && chown -R 65532:65532 /app /workspace

ENV PATH="/app/.venv/bin:$PATH"
ENV HOME=/tmp
ENV PYTHONUNBUFFERED=1
ENV WORKSPACE_DIR=/workspace

USER 65532:65532

CMD ["python", "worker.py"]
