"""Custom gunicorn logger for JSON output matching Go's zap format."""

import json
import os
from datetime import datetime, timezone
from gunicorn.glogging import Logger

# Log level priorities (higher = more severe)
LOG_LEVELS = {
    "debug": 10,
    "info": 20,
    "warn": 30,
    "error": 40,
    "fatal": 50,
}


class JSONLogger(Logger):
    """Gunicorn logger that outputs JSON format matching Go's zap logger.

    Usage: gunicorn --logger-class agent_libs.gunicorn.JSONLogger server:app

    Set LOG_LEVEL environment variable to control verbosity (default: info).
    """

    def __init__(self, cfg):
        super().__init__(cfg)
        level_name = os.environ.get("LOG_LEVEL", "info").lower()
        self._log_level = LOG_LEVELS.get(level_name, LOG_LEVELS["info"])

    def _log(self, level, msg, *args, **kwargs):
        """Format log entry as JSON if level is enabled."""
        level_priority = LOG_LEVELS.get(level, 0)
        if level_priority < self._log_level:
            return

        if args:
            msg = msg % args

        log_entry = {
            "ts": datetime.now(timezone.utc).isoformat(timespec="milliseconds").replace("+00:00", "Z"),
            "level": level,
            "pid": os.getpid(),
            "msg": msg,
        }
        print(json.dumps(log_entry), flush=True)

    def critical(self, msg, *args, **kwargs):
        self._log("fatal", msg, *args, **kwargs)

    def error(self, msg, *args, **kwargs):
        self._log("error", msg, *args, **kwargs)

    def warning(self, msg, *args, **kwargs):
        self._log("warn", msg, *args, **kwargs)

    def info(self, msg, *args, **kwargs):
        self._log("info", msg, *args, **kwargs)

    def debug(self, msg, *args, **kwargs):
        self._log("debug", msg, *args, **kwargs)

    def access(self, resp, req, environ, request_time):
        """Log access in JSON format."""
        log_entry = {
            "ts": datetime.now(timezone.utc).isoformat(timespec="milliseconds").replace("+00:00", "Z"),
            "level": "info",
            "pid": os.getpid(),
            "msg": "request completed",
            "method": environ.get("REQUEST_METHOD"),
            "path": environ.get("PATH_INFO"),
            "status": resp.status_code if hasattr(resp, "status_code") else resp.status.split(None, 1)[0],
            "duration_ms": round(request_time.total_seconds() * 1000, 2),
            "remote_addr": environ.get("REMOTE_ADDR"),
        }
        print(json.dumps(log_entry), flush=True)
