FROM ghcr.io/jarsater/agent-libs:latest

# Copy agent code
COPY server.py /app/server.py

# Set environment variables
ENV PYTHONUNBUFFERED=1
ENV MCP_ENTRY_MODULE=/app/server.py
ENV WORKSPACE_DIR=/workspace

# Create workspace directory
RUN mkdir -p /workspace

# Health check
HEALTHCHECK --interval=30s --timeout=10s --start-period=5s --retries=3 \
    CMD python -c "import sys; sys.exit(0)"

# Run the MCP server
CMD ["python", "/app/server.py"]
