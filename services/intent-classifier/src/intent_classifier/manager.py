from __future__ import annotations

import logging
import threading
from collections.abc import Callable
from pathlib import Path
from typing import Any

from .errors import ServiceError
from .model_package import (
    MODEL_VERSION_PATTERN,
    LoadedModel,
    Prediction,
    ValidationReport,
    load_model_package,
)
from .state_store import RuntimeState, StateStore

ModelLoader = Callable[[Path, str], LoadedModel]
logger = logging.getLogger(__name__)


class ModelManager:
    def __init__(
        self,
        model_root: Path,
        state_dir: Path,
        loader: ModelLoader = load_model_package,
    ) -> None:
        self.model_root = model_root.expanduser()
        self._state_store = StateStore(state_dir)
        self._loader = loader
        self._state_lock = threading.RLock()
        self._operation_lock = threading.Lock()
        self._active: LoadedModel | None = None
        self._candidate: LoadedModel | None = None
        self._previous: LoadedModel | None = None

    def start(self, version: str) -> dict[str, Any]:
        restored = self.restore(version)
        if restored is None:
            raise ServiceError(503, "model_not_ready", "No startup model is configured")
        return restored

    def restore(self, explicit_version: str = "") -> dict[str, Any] | None:
        with self._operation_lock:
            persisted = self._state_store.load()
            active_version, previous_version = self._restore_versions(explicit_version, persisted)
            if not active_version:
                return None
            loaded = self._loader(self.model_root, active_version)
            previous: LoadedModel | None = None
            if previous_version and previous_version != active_version:
                try:
                    previous = self._loader(self.model_root, previous_version)
                except ServiceError as exc:
                    logger.error(
                        "previous_model_restore_failed",
                        extra={
                            "event": "previous_model_restore_failed",
                            "model_version": previous_version,
                            "error_code": exc.code,
                        },
                    )
                    previous_version = None
            self._state_store.save(active_version, previous_version)
            with self._state_lock:
                self._active = loaded
                self._candidate = None
                self._previous = previous
        logger.info(
            "startup_model_activated",
            extra={
                "event": "startup_model_activated",
                "model_version": loaded.model_version,
                "preprocessing_version": loaded.preprocessing_version,
            },
        )
        return self._model_state(loaded, "active")

    def validate(self, version: str) -> ValidationReport:
        with self._operation_lock:
            loaded = self._loader(self.model_root, version)
        logger.info(
            "model_package_validated",
            extra={"event": "model_package_validated", "model_version": version},
        )
        return loaded.report

    def preload(self, version: str) -> dict[str, Any]:
        with self._operation_lock:
            loaded = self._loader(self.model_root, version)
            with self._state_lock:
                self._candidate = loaded
        logger.info(
            "candidate_model_preloaded",
            extra={
                "event": "candidate_model_preloaded",
                "model_version": loaded.model_version,
                "preprocessing_version": loaded.preprocessing_version,
            },
        )
        return self._model_state(loaded, "candidate")

    def activate(self, version: str) -> dict[str, Any]:
        with self._state_lock:
            if self._active is not None and self._active.model_version == version:
                return self._model_state(self._active, "active")
            if self._candidate is None or self._candidate.model_version != version:
                raise ServiceError(
                    409,
                    "candidate_not_preloaded",
                    "Requested model version is not the preloaded candidate",
                )
            active = self._candidate
            previous = self._active
            self._state_store.save(
                active.model_version,
                previous.model_version if previous is not None else None,
            )
            self._active = active
            self._candidate = None
            self._previous = previous
        logger.info(
            "candidate_model_activated",
            extra={
                "event": "candidate_model_activated",
                "model_version": active.model_version,
                "preprocessing_version": active.preprocessing_version,
            },
        )
        return self._model_state(active, "active")

    def rollback(self) -> dict[str, Any]:
        with self._state_lock:
            if self._previous is None:
                raise ServiceError(409, "rollback_unavailable", "No previous model is available")
            active = self._previous
            current = self._active
            self._state_store.save(
                active.model_version,
                current.model_version if current is not None else None,
            )
            self._active = active
            self._previous = current
        logger.info(
            "model_rollback_completed",
            extra={
                "event": "model_rollback_completed",
                "model_version": active.model_version,
                "preprocessing_version": active.preprocessing_version,
            },
        )
        return self._model_state(active, "active")

    def predict(self, text: str, matched_keyword: str) -> tuple[LoadedModel, Prediction]:
        with self._state_lock:
            active = self._active
        if active is None:
            raise ServiceError(503, "model_not_ready", "No active model is loaded")
        return active, active.predict(text, matched_keyword)

    def ready_state(self) -> tuple[str, str] | None:
        with self._state_lock:
            active = self._active
        if active is None:
            return None
        return active.model_version, active.preprocessing_version

    def list_models(self) -> dict[str, Any]:
        with self._state_lock:
            active = self._active
            candidate = self._candidate
            previous = self._previous
        available: list[str] = []
        if self.model_root.exists() and self.model_root.is_dir():
            for child in self.model_root.iterdir():
                if (
                    child.is_dir()
                    and not child.is_symlink()
                    and MODEL_VERSION_PATTERN.fullmatch(child.name)
                ):
                    available.append(child.name)
        return {
            "active": self._optional_model_state(active, "active"),
            "candidate": self._optional_model_state(candidate, "candidate"),
            "previous": self._optional_model_state(previous, "previous"),
            "available_versions": sorted(available),
        }

    @staticmethod
    def _restore_versions(
        explicit_version: str, persisted: RuntimeState | None
    ) -> tuple[str, str | None]:
        if not explicit_version:
            if persisted is None:
                return "", None
            return persisted.active_model_version, persisted.previous_model_version
        if persisted is None:
            return explicit_version, None
        if persisted.active_model_version == explicit_version:
            return explicit_version, persisted.previous_model_version
        return explicit_version, persisted.active_model_version

    @staticmethod
    def _model_state(model: LoadedModel, status: str) -> dict[str, Any]:
        return {
            "status": status,
            "model_version": model.model_version,
            "preprocessing_version": model.preprocessing_version,
            "loaded_at": model.loaded_at,
        }

    @classmethod
    def _optional_model_state(cls, model: LoadedModel | None, status: str) -> dict[str, Any] | None:
        if model is None:
            return None
        return cls._model_state(model, status)
