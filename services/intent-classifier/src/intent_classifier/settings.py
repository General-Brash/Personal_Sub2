from __future__ import annotations

import os
import secrets
from dataclasses import dataclass
from pathlib import Path


def _integer_env(name: str, default: int, minimum: int, maximum: int) -> int:
    raw = os.getenv(name, str(default)).strip()
    try:
        value = int(raw)
    except ValueError as exc:
        raise ValueError(f"{name} must be an integer") from exc
    if value < minimum or value > maximum:
        raise ValueError(f"{name} must be between {minimum} and {maximum}")
    return value


@dataclass(frozen=True, slots=True)
class Settings:
    host: str = "0.0.0.0"
    port: int = 8080
    model_root: Path = Path("/models")
    state_dir: Path = Path("/state")
    startup_model_version: str = ""
    api_token: str = ""
    admin_token: str = ""
    admin_url: str = "http://127.0.0.1:8080"
    max_concurrency: int = 4
    inference_timeout_ms: int = 250
    max_request_bytes: int = 65_536
    log_level: str = "INFO"
    admin_loopback_only: bool = True

    def __post_init__(self) -> None:
        if (
            self.api_token
            and self.admin_token
            and secrets.compare_digest(self.api_token, self.admin_token)
        ):
            raise ValueError("API and admin tokens must be different")
        if len(self.api_token) > 8192 or len(self.admin_token) > 8192:
            raise ValueError("classifier tokens must not exceed 8192 characters")
        if self.model_root.expanduser().absolute() == self.state_dir.expanduser().absolute():
            raise ValueError("model root and state directory must be different")
        if self.max_concurrency < 1 or self.max_concurrency > 64:
            raise ValueError("max_concurrency must be between 1 and 64")
        if self.inference_timeout_ms < 1 or self.inference_timeout_ms > 30_000:
            raise ValueError("inference_timeout_ms must be between 1 and 30000")
        if self.max_request_bytes < 1024 or self.max_request_bytes > 1_048_576:
            raise ValueError("max_request_bytes must be between 1024 and 1048576")

    @classmethod
    def from_env(cls) -> Settings:
        port = _integer_env("INTENT_CLASSIFIER_PORT", 8080, 1, 65_535)
        admin_url = os.getenv("INTENT_CLASSIFIER_ADMIN_URL", "").strip()
        if not admin_url:
            admin_url = f"http://127.0.0.1:{port}"
        active_version = os.getenv("INTENT_CLASSIFIER_ACTIVE_VERSION", "").strip()
        if not active_version:
            active_version = os.getenv("INTENT_CLASSIFIER_MODEL_VERSION", "").strip()
        return cls(
            host=os.getenv("INTENT_CLASSIFIER_HOST", "0.0.0.0").strip() or "0.0.0.0",
            port=port,
            model_root=Path(os.getenv("INTENT_CLASSIFIER_MODEL_ROOT", "/models")).expanduser(),
            state_dir=Path(os.getenv("INTENT_CLASSIFIER_STATE_DIR", "/state")).expanduser(),
            startup_model_version=active_version,
            api_token=os.getenv("INTENT_CLASSIFIER_API_TOKEN", "").strip(),
            admin_token=os.getenv("INTENT_CLASSIFIER_ADMIN_TOKEN", "").strip(),
            admin_url=admin_url.rstrip("/"),
            max_concurrency=_integer_env("INTENT_CLASSIFIER_MAX_CONCURRENCY", 4, 1, 64),
            inference_timeout_ms=_integer_env(
                "INTENT_CLASSIFIER_INFERENCE_TIMEOUT_MS", 250, 1, 30_000
            ),
            max_request_bytes=_integer_env(
                "INTENT_CLASSIFIER_MAX_REQUEST_BYTES", 65_536, 1_024, 1_048_576
            ),
            log_level=os.getenv("INTENT_CLASSIFIER_LOG_LEVEL", "INFO").strip().upper() or "INFO",
        )
