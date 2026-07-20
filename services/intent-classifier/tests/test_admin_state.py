from __future__ import annotations

import json
from collections.abc import Callable

from conftest import PackageFactory
from fastapi.testclient import TestClient

from intent_classifier.app import create_app
from intent_classifier.settings import Settings

ADMIN_HEADERS = {"Authorization": "Bearer admin-secret"}


def test_admin_preload_activate_rollback_and_restart_restore(
    package_factory: PackageFactory,
    settings_factory: Callable[..., Settings],
) -> None:
    settings = settings_factory()
    package_factory(settings.model_root, "fixture-v1", logits=(2.0, -2.0))
    package_factory(settings.model_root, "fixture-v2", logits=(-2.0, 2.0))
    app = create_app(settings)

    with TestClient(app) as client:
        validated = client.post("/admin/v1/models/fixture-v1/validate", headers=ADMIN_HEADERS)
        preloaded_v1 = client.post("/admin/v1/models/fixture-v1/preload", headers=ADMIN_HEADERS)
        activated_v1 = client.post("/admin/v1/models/fixture-v1/activate", headers=ADMIN_HEADERS)
        client.post("/admin/v1/models/fixture-v2/preload", headers=ADMIN_HEADERS)
        activated_v2 = client.post("/admin/v1/models/fixture-v2/activate", headers=ADMIN_HEADERS)
        listed = client.get("/admin/v1/models", headers=ADMIN_HEADERS)

    assert validated.status_code == 200
    assert validated.json()["status"] == "valid"
    assert preloaded_v1.json()["model"]["status"] == "candidate"
    assert activated_v1.json()["model"]["model_version"] == "fixture-v1"
    assert activated_v2.json()["model"]["model_version"] == "fixture-v2"
    assert listed.json()["active"]["model_version"] == "fixture-v2"
    assert listed.json()["previous"]["model_version"] == "fixture-v1"

    state_path = settings.state_dir / "active.json"
    assert json.loads(state_path.read_text(encoding="utf-8")) == {
        "schema_version": "1",
        "active_model_version": "fixture-v2",
        "previous_model_version": "fixture-v1",
    }
    assert not list(settings.state_dir.glob("*.tmp"))

    restarted = create_app(settings)
    with TestClient(restarted) as client:
        ready = client.get("/health/ready")
        rolled_back = client.post("/admin/v1/models/rollback", headers=ADMIN_HEADERS)

    assert ready.json()["active_model_version"] == "fixture-v2"
    assert rolled_back.status_code == 200
    assert rolled_back.json()["model"]["model_version"] == "fixture-v1"

    restored_again = create_app(settings)
    with TestClient(restored_again) as client:
        ready_after_rollback = client.get("/health/ready")
        models_after_rollback = client.get("/admin/v1/models", headers=ADMIN_HEADERS)

    assert ready_after_rollback.json()["active_model_version"] == "fixture-v1"
    assert models_after_rollback.json()["previous"]["model_version"] == "fixture-v2"


def test_explicit_active_version_overrides_persisted_state_and_keeps_rollback(
    package_factory: PackageFactory,
    settings_factory: Callable[..., Settings],
) -> None:
    base = settings_factory(startup_model_version="fixture-v1")
    package_factory(base.model_root, "fixture-v1")
    package_factory(base.model_root, "fixture-v2", logits=(-2.0, 2.0))
    with TestClient(create_app(base)) as client:
        assert client.get("/health/ready").json()["active_model_version"] == "fixture-v1"

    explicit = settings_factory(startup_model_version="fixture-v2")
    with TestClient(create_app(explicit)) as client:
        ready = client.get("/health/ready")
        models = client.get("/admin/v1/models", headers=ADMIN_HEADERS)

    assert ready.json()["active_model_version"] == "fixture-v2"
    assert models.json()["previous"]["model_version"] == "fixture-v1"


def test_admin_requires_loopback_and_separate_token(
    settings_factory: Callable[..., Settings],
) -> None:
    loopback_only = settings_factory(admin_loopback_only=True)
    with TestClient(create_app(loopback_only)) as client:
        hidden = client.get("/admin/v1/models", headers=ADMIN_HEADERS)
    assert hidden.status_code == 404
    assert hidden.json()["error"]["code"] == "not_found"

    token_required = settings_factory(admin_loopback_only=False)
    with TestClient(create_app(token_required)) as client:
        missing = client.get("/admin/v1/models")
        api_token_is_not_admin = client.get(
            "/admin/v1/models", headers={"Authorization": "Bearer api-secret"}
        )
    assert missing.status_code == 401
    assert api_token_is_not_admin.status_code == 401


def test_admin_disabled_without_admin_token(
    settings_factory: Callable[..., Settings],
) -> None:
    settings = settings_factory(admin_token="", admin_loopback_only=False)
    with TestClient(create_app(settings)) as client:
        response = client.get("/admin/v1/models")
    assert response.status_code == 503
    assert response.json()["error"]["code"] == "admin_disabled"


def test_admin_reports_safe_validation_and_activation_errors(
    package_factory: PackageFactory,
    settings_factory: Callable[..., Settings],
) -> None:
    settings = settings_factory()
    package = package_factory(settings.model_root, "fixture-v1")
    (package / "labels.json").write_text("{}", encoding="utf-8")
    with TestClient(create_app(settings)) as client:
        invalid = client.post("/admin/v1/models/fixture-v1/validate", headers=ADMIN_HEADERS)
        not_preloaded = client.post("/admin/v1/models/fixture-v1/activate", headers=ADMIN_HEADERS)

    assert invalid.status_code == 422
    assert invalid.json()["error"]["code"] == "invalid_model_package"
    assert "labels.json" not in invalid.json()["error"]["message"]
    assert not_preloaded.status_code == 409
    assert not_preloaded.json()["error"]["code"] == "candidate_not_preloaded"
