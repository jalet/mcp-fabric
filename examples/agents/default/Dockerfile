# Agent runner using uv slim image
FROM ghcr.io/astral-sh/uv:python3.12-bookworm-slim

WORKDIR /app

COPY pyproject.toml .
RUN uv sync --no-dev --no-install-project

COPY server.py tool_loader.py ./

RUN chown -R 65532:65532 /app

ENV PATH="/app/.venv/bin:$PATH"
ENV HOME=/tmp

# Gunicorn configuration (override at runtime)
ENV PORT=8080
ENV WEB_CONCURRENCY=1
ENV GUNICORN_CMD_ARGS="--timeout 300"

USER 65532:65532
EXPOSE 8080

CMD ["--logger-class", "agent_libs.gunicorn.JSONLogger", "server:app"]
ENTRYPOINT ["python", "-m", "gunicorn"]
