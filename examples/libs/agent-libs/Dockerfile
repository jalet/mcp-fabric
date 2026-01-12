# Shared agent libraries - loaded via init container
FROM ghcr.io/astral-sh/uv:python3.12-bookworm-slim

WORKDIR /app

COPY pyproject.toml .
COPY agent_libs/ agent_libs/

RUN uv sync --no-dev && chown -R 65532:65532 /app

ENV PATH="/app/.venv/bin:$PATH"

USER 65532:65532

CMD ["python", "-c", "from agent_libs import setup_json_logging"]
