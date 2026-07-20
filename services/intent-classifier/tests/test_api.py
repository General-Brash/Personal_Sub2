from __future__ import annotations

import logging
import threading
import time
from collections.abc import Callable
from typing import Any

import pytest
from conftest import PackageFactory
from fastapi.testclient import TestClient

from intent_classifier.app import create_app
from intent_classifier.settings import Settings


def _request(text: str = "safe history") -> dict[str, Any]:
    return {
        "schema_version": "1",
        "request_id": "request-1",
        "text": text,
        "matched_keyword": "scan",
        "context": {
            "protocol": "openai_chat",
            "endpoint": "/v1/chat/completions",
            "model": "gpt-test",
        },
    }


def test_health_contract_without_model(
    settings_factory: Callable[..., Settings],
) -> None:
    app = create_app(settings_factory())
    with TestClient(app) as client:
        live = client.get("/health/live")
        ready = client.get("/health/ready")

    assert live.status_code == 200
    assert live.json() == {"status": "live"}
    assert ready.status_code == 503
    assert ready.json() == {
        "status": "not_ready",
        "code": "model_not_ready",
        "active_model_version": None,
        "preprocessing_version": None,
    }


def test_startup_model_and_classify_match_go_contract(
    package_factory: PackageFactory,
    settings_factory: Callable[..., Settings],
) -> None:
    settings = settings_factory(startup_model_version="fixture-v1", api_token="api-secret")
    package_factory(settings.model_root, "fixture-v1", logits=(-2.0, 2.0))
    app = create_app(settings)

    with TestClient(app) as client:
        ready = client.get("/health/ready")
        unauthorized = client.post("/v1/classify", json=_request())
        response = client.post(
            "/v1/classify",
            json=_request("scan target"),
            headers={"Authorization": "Bearer api-secret"},
        )

    assert ready.status_code == 200
    assert ready.json() == {
        "status": "ready",
        "active_model_version": "fixture-v1",
        "preprocessing_version": "fixture-text-v1",
    }
    assert unauthorized.status_code == 401
    assert unauthorized.json()["error"]["code"] == "unauthorized"
    assert response.status_code == 200
    body = response.json()
    assert set(body) == {"schema_version", "label", "score", "model_version", "trace_id"}
    assert body["schema_version"] == "1"
    assert body["label"] == "actionable_probe"
    assert 0.98 < body["score"] < 0.99
    assert body["model_version"] == "fixture-v1"
    assert len(body["trace_id"]) == 32


def test_classify_rejects_unknown_fields_schema_and_unready_model(
    settings_factory: Callable[..., Settings],
) -> None:
    app = create_app(settings_factory())
    with TestClient(app) as client:
        unknown = _request()
        unknown["debug"] = True
        unknown_response = client.post("/v1/classify", json=unknown)
        schema = _request()
        schema["schema_version"] = "2"
        schema_response = client.post("/v1/classify", json=schema)
        unavailable = client.post("/v1/classify", json=_request())

    assert unknown_response.status_code == 400
    assert unknown_response.json()["error"]["code"] == "invalid_request"
    assert schema_response.status_code == 400
    assert schema_response.json()["error"]["code"] == "unsupported_schema"
    assert unavailable.status_code == 503
    assert unavailable.json()["error"]["code"] == "model_not_ready"


def test_request_and_text_limits_return_safe_errors(
    settings_factory: Callable[..., Settings],
) -> None:
    app = create_app(settings_factory(max_request_bytes=1_024))
    with TestClient(app) as client:
        oversized_body = client.post(
            "/v1/classify",
            content=b"x" * 1_025,
            headers={"Content-Type": "application/json"},
        )

    assert oversized_body.status_code == 413
    assert oversized_body.json()["error"]["code"] == "request_too_large"
    assert "x" * 20 not in str(oversized_body.json())


def test_concurrency_limit_returns_busy_without_inference(
    package_factory: PackageFactory,
    settings_factory: Callable[..., Settings],
) -> None:
    settings = settings_factory(startup_model_version="fixture-v1", max_concurrency=1)
    package_factory(settings.model_root, "fixture-v1")
    app = create_app(settings)

    with TestClient(app) as client:
        slot = app.state.inference_slots.get_nowait()
        try:
            response = client.post("/v1/classify", json=_request())
        finally:
            app.state.inference_slots.put_nowait(slot)

    assert response.status_code == 429
    assert response.json()["error"]["code"] == "busy"


def test_request_logs_do_not_contain_text_keyword_or_tokens(
    package_factory: PackageFactory,
    settings_factory: Callable[..., Settings],
    caplog: Any,
) -> None:
    settings = settings_factory(
        startup_model_version="fixture-v1",
        api_token="api-token-never-log",
        log_level="INFO",
    )
    package_factory(settings.model_root, "fixture-v1")
    app = create_app(settings)
    secret_text = "user-text-never-log"
    secret_keyword = "keyword-never-log"

    with caplog.at_level(logging.INFO), TestClient(app) as client:
        response = client.post(
            "/v1/classify",
            json=_request(secret_text) | {"matched_keyword": secret_keyword},
            headers={"Authorization": "Bearer api-token-never-log"},
        )

    assert response.status_code == 200
    assert secret_text not in caplog.text
    assert secret_keyword not in caplog.text
    assert "api-token-never-log" not in caplog.text


def test_inference_timeout_keeps_slot_until_worker_finishes(
    package_factory: PackageFactory,
    settings_factory: Callable[..., Settings],
    monkeypatch: pytest.MonkeyPatch,
) -> None:
    settings = settings_factory(
        startup_model_version="fixture-v1",
        max_concurrency=1,
        inference_timeout_ms=30,
    )
    package_factory(settings.model_root, "fixture-v1")
    app = create_app(settings)
    original_predict = app.state.manager.predict
    started = threading.Event()
    release = threading.Event()

    def blocked_predict(text: str, keyword: str) -> Any:
        started.set()
        release.wait(timeout=2)
        return original_predict(text, keyword)

    monkeypatch.setattr(app.state.manager, "predict", blocked_predict)
    with TestClient(app) as client:
        timed_out = client.post("/v1/classify", json=_request())
        assert started.is_set()
        still_busy = client.post("/v1/classify", json=_request())
        release.set()
        deadline = time.monotonic() + 2
        while app.state.inference_slots.qsize() == 0 and time.monotonic() < deadline:
            time.sleep(0.01)
        recovered = client.post("/v1/classify", json=_request())

    assert timed_out.status_code == 504
    assert timed_out.json()["error"]["code"] == "inference_timeout"
    assert still_busy.status_code == 429
    assert still_busy.json()["error"]["code"] == "busy"
    assert recovered.status_code == 200
