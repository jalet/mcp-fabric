# ToolPackage images only run as init containers to copy files
FROM ghcr.io/astral-sh/uv:python3.12-bookworm-slim

WORKDIR /app

COPY pyproject.toml .
COPY string_tools/ string_tools/

RUN uv sync --no-dev && chown -R 65532:65532 /app

ENV PATH="/app/.venv/bin:$PATH"

USER 65532:65532

CMD ["python", "-c", "from string_tools.tools import *"]
