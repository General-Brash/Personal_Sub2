from __future__ import annotations

from pathlib import Path

import pytest
from conftest import PackageFactory

from intent_classifier.errors import ServiceError
from intent_classifier.manager import ModelManager
from intent_classifier.state_store import StateStore


def test_state_store_round_trip_is_atomic(tmp_path: Path) -> None:
    state_dir = tmp_path / "state"
    store = StateStore(state_dir)

    store.save("fixture-v2", "fixture-v1")
    state = store.load()

    assert state is not None
    assert state.active_model_version == "fixture-v2"
    assert state.previous_model_version == "fixture-v1"
    assert not list(state_dir.glob(".*.tmp"))


def test_state_store_rejects_duplicate_keys_and_non_file_destination(tmp_path: Path) -> None:
    state_dir = tmp_path / "state"
    state_dir.mkdir()
    state_path = state_dir / "active.json"
    state_path.write_text(
        '{"schema_version":"1","active_model_version":"v1",'
        '"active_model_version":"v2","previous_model_version":null}',
        encoding="utf-8",
    )
    with pytest.raises(ServiceError) as duplicate:
        StateStore(state_dir).load()
    assert duplicate.value.code == "state_invalid"

    state_path.unlink()
    state_path.mkdir()
    with pytest.raises(ServiceError) as destination:
        StateStore(state_dir).save("v1", None)
    assert destination.value.code == "state_persist_failed"


def test_failed_state_persist_does_not_swap_active_model(
    package_factory: PackageFactory, tmp_path: Path
) -> None:
    model_root = tmp_path / "models"
    state_dir = tmp_path / "state"
    package_factory(model_root, "fixture-v1")
    package_factory(model_root, "fixture-v2", logits=(-2.0, 2.0))
    manager = ModelManager(model_root, state_dir)
    manager.start("fixture-v1")
    manager.preload("fixture-v2")

    state_path = state_dir / "active.json"
    state_path.unlink()
    state_path.mkdir()
    with pytest.raises(ServiceError) as failure:
        manager.activate("fixture-v2")

    assert failure.value.code == "state_persist_failed"
    assert manager.list_models()["active"]["model_version"] == "fixture-v1"
    assert manager.list_models()["candidate"]["model_version"] == "fixture-v2"
