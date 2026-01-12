"""JSON structured logging matching Go's zap format."""

import json
import logging
import os
from datetime import datetime, timezone

# Map Python log levels to Go/zap level names
LEVEL_MAP = {
    "DEBUG": "debug",
    "INFO": "info",
    "WARNING": "warn",
    "ERROR": "error",
    "CRITICAL": "fatal",
}


class JSONFormatter(logging.Formatter):
    """JSON log formatter matching Go's zap logger format.

    Output format: {"ts": "2026-01-14T21:26:06.123Z", "level": "info", "pid": 14, "msg": "..."}
    """

    def format(self, record: logging.LogRecord) -> str:
        level = LEVEL_MAP.get(record.levelname, record.levelname.lower())
        log_entry = {
            "ts": datetime.now(timezone.utc).isoformat(timespec="milliseconds").replace("+00:00", "Z"),
            "level": level,
            "pid": os.getpid(),
            "msg": record.getMessage(),
        }
        if record.exc_info:
            log_entry["error"] = self.formatException(record.exc_info)
        return json.dumps(log_entry)


def setup_json_logging(level: int = logging.INFO) -> None:
    """Configure root logger to use JSON format.

    Args:
        level: Logging level (default: INFO)
    """
    handler = logging.StreamHandler()
    handler.setFormatter(JSONFormatter())
    logging.root.handlers = []
    logging.root.addHandler(handler)
    logging.root.setLevel(level)


def get_logger(name: str) -> logging.Logger:
    """Get a logger with the given name.

    Args:
        name: Logger name

    Returns:
        Configured logger instance
    """
    return logging.getLogger(name)
