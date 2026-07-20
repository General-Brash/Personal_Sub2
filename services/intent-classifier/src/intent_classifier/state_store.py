from __future__ import annotations

import json
import os
import uuid
from pathlib import Path
from typing import Any, Literal

from pydantic import BaseModel, ConfigDict, Field, ValidationError

from .errors import ServiceError
from .model_package import MODEL_VERSION_PATTERN

STATE_FILENAME = "active.json"
MAX_STATE_BYTES = 4096


class RuntimeState(BaseModel):
    model_config = ConfigDict(extra="forbid", strict=True)

    schema_version: Literal["1"] = "1"
    active_model_version: str = Field(min_length=1, max_length=200)
    previous_model_version: str | None = Field(default=None, max_length=200)


class StateStore:
    def __init__(self, state_dir: Path) -> None:
        self.state_dir = state_dir.expanduser()

    def load(self) -> RuntimeState | None:
        directory = self._prepare_directory()
        state_path = directory / STATE_FILENAME
        if state_path.is_symlink():
            raise ServiceError(500, "state_invalid", "Runtime model state is invalid")
        if not state_path.exists():
            return None
        if not state_path.is_file():
            raise ServiceError(500, "state_invalid", "Runtime model state is invalid")
        try:
            if state_path.stat().st_size > MAX_STATE_BYTES:
                raise ValueError("state file too large")
            raw = json.loads(
                state_path.read_text(encoding="utf-8"),
                object_pairs_hook=_reject_duplicate_keys,
                parse_constant=lambda value: _raise_invalid_constant(value),
            )
            state = RuntimeState.model_validate(raw)
        except (OSError, UnicodeError, ValueError, ValidationError, json.JSONDecodeError) as exc:
            raise ServiceError(
                500,
                "state_invalid",
                "Runtime model state is invalid",
                detail=type(exc).__name__,
            ) from exc
        if not MODEL_VERSION_PATTERN.fullmatch(state.active_model_version):
            raise ServiceError(500, "state_invalid", "Runtime model state is invalid")
        if state.previous_model_version and not MODEL_VERSION_PATTERN.fullmatch(
            state.previous_model_version
        ):
            raise ServiceError(500, "state_invalid", "Runtime model state is invalid")
        return state

    def save(self, active_version: str, previous_version: str | None) -> None:
        try:
            state = RuntimeState(
                active_model_version=active_version,
                previous_model_version=previous_version,
            )
        except ValidationError as exc:
            raise ServiceError(
                500,
                "state_persist_failed",
                "Runtime model state could not be persisted",
                detail=type(exc).__name__,
            ) from exc
        if not MODEL_VERSION_PATTERN.fullmatch(active_version) or (
            previous_version and not MODEL_VERSION_PATTERN.fullmatch(previous_version)
        ):
            raise ServiceError(
                500,
                "state_persist_failed",
                "Runtime model state could not be persisted",
            )

        directory = self._prepare_directory()
        destination = directory / STATE_FILENAME
        if destination.is_symlink() or (destination.exists() and not destination.is_file()):
            raise ServiceError(
                500,
                "state_persist_failed",
                "Runtime model state could not be persisted",
            )
        temporary = directory / f".{STATE_FILENAME}.{os.getpid()}.{uuid.uuid4().hex}.tmp"
        payload = (
            json.dumps(state.model_dump(), ensure_ascii=True, separators=(",", ":")) + "\n"
        ).encode("utf-8")
        try:
            descriptor = os.open(
                temporary,
                os.O_CREAT | os.O_EXCL | os.O_WRONLY,
                0o600,
            )
            try:
                with os.fdopen(descriptor, "wb") as handle:
                    handle.write(payload)
                    handle.flush()
                    os.fsync(handle.fileno())
                os.replace(temporary, destination)
                _fsync_directory(directory)
            except Exception:
                temporary.unlink(missing_ok=True)
                raise
        except OSError as exc:
            raise ServiceError(
                500,
                "state_persist_failed",
                "Runtime model state could not be persisted",
                detail=type(exc).__name__,
            ) from exc

    def _prepare_directory(self) -> Path:
        try:
            if self.state_dir.is_symlink():
                raise OSError("state directory is a symlink")
            self.state_dir.mkdir(parents=True, exist_ok=True)
            if not self.state_dir.is_dir():
                raise OSError("state path is not a directory")
            return self.state_dir.resolve()
        except OSError as exc:
            raise ServiceError(
                500,
                "state_unavailable",
                "Runtime model state directory is unavailable",
                detail=type(exc).__name__,
            ) from exc


def _fsync_directory(directory: Path) -> None:
    if os.name == "nt":
        return
    descriptor = os.open(directory, os.O_RDONLY)
    try:
        os.fsync(descriptor)
    finally:
        os.close(descriptor)


def _reject_duplicate_keys(pairs: list[tuple[str, Any]]) -> dict[str, Any]:
    result: dict[str, Any] = {}
    for key, value in pairs:
        if key in result:
            raise ValueError(f"duplicate key: {key}")
        result[key] = value
    return result


def _raise_invalid_constant(value: str) -> Any:
    raise ValueError(f"invalid JSON constant: {value}")
