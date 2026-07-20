from __future__ import annotations

from pathlib import Path

import pytest

from intent_classifier.settings import Settings


def test_api_and_admin_tokens_must_be_separate(tmp_path: Path) -> None:
    with pytest.raises(ValueError, match="must be different"):
        Settings(
            model_root=tmp_path / "models",
            state_dir=tmp_path / "state",
            api_token="same-token",
            admin_token="same-token",
        )


def test_model_and_state_directories_must_be_separate(tmp_path: Path) -> None:
    with pytest.raises(ValueError, match="must be different"):
        Settings(model_root=tmp_path, state_dir=tmp_path)


def test_admin_url_defaults_to_configured_service_port(monkeypatch: pytest.MonkeyPatch) -> None:
    monkeypatch.delenv("INTENT_CLASSIFIER_ADMIN_URL", raising=False)
    monkeypatch.setenv("INTENT_CLASSIFIER_PORT", "18080")
    settings = Settings.from_env()
    assert settings.admin_url == "http://127.0.0.1:18080"


def test_active_version_takes_precedence_over_legacy_name(
    monkeypatch: pytest.MonkeyPatch,
) -> None:
    monkeypatch.setenv("INTENT_CLASSIFIER_ACTIVE_VERSION", "active-v2")
    monkeypatch.setenv("INTENT_CLASSIFIER_MODEL_VERSION", "legacy-v1")
    assert Settings.from_env().startup_model_version == "active-v2"


@pytest.mark.parametrize("timeout_ms", [0, 30_001])
def test_inference_timeout_range_is_strict(tmp_path: Path, timeout_ms: int) -> None:
    with pytest.raises(ValueError, match="inference_timeout_ms"):
        Settings(
            model_root=tmp_path / "models",
            state_dir=tmp_path / "state",
            inference_timeout_ms=timeout_ms,
        )
