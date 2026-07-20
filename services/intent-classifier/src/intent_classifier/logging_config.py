from __future__ import annotations

import json
import logging
from datetime import UTC, datetime
from typing import Any

_EXTRA_FIELDS = (
    "event",
    "trace_id",
    "path",
    "method",
    "status_code",
    "latency_ms",
    "model_version",
    "preprocessing_version",
    "error_code",
)


class JSONFormatter(logging.Formatter):
    def format(self, record: logging.LogRecord) -> str:
        payload: dict[str, Any] = {
            "timestamp": datetime.now(UTC).isoformat(),
            "level": record.levelname.lower(),
            "message": record.getMessage(),
        }
        for field in _EXTRA_FIELDS:
            value = getattr(record, field, None)
            if value not in (None, ""):
                payload[field] = value
        return json.dumps(payload, ensure_ascii=True, separators=(",", ":"))


def configure_logging(level: str) -> None:
    handler = logging.StreamHandler()
    handler.setFormatter(JSONFormatter())
    root = logging.getLogger()
    root.handlers.clear()
    root.addHandler(handler)
    root.setLevel(getattr(logging, level.upper(), logging.INFO))

    logging.getLogger("uvicorn.access").disabled = True
