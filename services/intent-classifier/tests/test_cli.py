from __future__ import annotations

import json
from pathlib import Path

from conftest import PackageFactory

from intent_classifier.cli import (
    EXIT_ADMIN_REJECTED,
    EXIT_SUCCESS,
    EXIT_VALIDATION,
    build_parser,
    main,
)


def test_validate_cli_success_and_failure(
    package_factory: PackageFactory, tmp_path: Path, capsys: object
) -> None:
    model_root = tmp_path / "models"
    package_factory(model_root, "fixture-v1")

    success = main(["validate", "fixture-v1", "--model-root", str(model_root)])
    captured = capsys.readouterr()  # type: ignore[attr-defined]
    assert success == EXIT_SUCCESS
    assert json.loads(captured.out)["status"] == "valid"

    failure = main(["validate", "missing-v1", "--model-root", str(model_root)])
    captured = capsys.readouterr()  # type: ignore[attr-defined]
    assert failure == EXIT_VALIDATION
    assert json.loads(captured.err)["code"] == "invalid_model_package"


def test_admin_cli_requires_separate_token(monkeypatch: object, capsys: object) -> None:
    monkeypatch.delenv("INTENT_CLASSIFIER_ADMIN_TOKEN", raising=False)  # type: ignore[attr-defined]
    result = main(["list", "--admin-url", "http://127.0.0.1:8080"])
    captured = capsys.readouterr()  # type: ignore[attr-defined]
    assert result == EXIT_ADMIN_REJECTED
    assert json.loads(captured.err)["code"] == "admin_token_missing"


def test_cli_default_admin_url_tracks_service_port(monkeypatch: object) -> None:
    monkeypatch.delenv("INTENT_CLASSIFIER_ADMIN_URL", raising=False)  # type: ignore[attr-defined]
    monkeypatch.setenv("INTENT_CLASSIFIER_PORT", "18080")  # type: ignore[attr-defined]
    args = build_parser().parse_args(["list"])
    assert args.admin_url == "http://127.0.0.1:18080"
