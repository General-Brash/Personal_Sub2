from __future__ import annotations

import argparse
import json
import os
import sys
import urllib.error
import urllib.parse
import urllib.request
from collections.abc import Sequence
from pathlib import Path
from typing import Any

import uvicorn

from .app import create_app
from .errors import ServiceError
from .installer import install_model_package
from .model_package import load_model_package
from .settings import Settings

EXIT_SUCCESS = 0
EXIT_VALIDATION = 2
EXIT_ADMIN_REJECTED = 3
EXIT_UNAVAILABLE = 4


def build_parser() -> argparse.ArgumentParser:
    parser = argparse.ArgumentParser(prog="intent-classifier")
    commands = parser.add_subparsers(dest="command", required=True)

    commands.add_parser("serve", help="Run the HTTP inference service")

    validate = commands.add_parser("validate", help="Validate a model package offline")
    validate.add_argument("version")
    validate.add_argument(
        "--model-root",
        default=os.getenv("INTENT_CLASSIFIER_MODEL_ROOT", "/models"),
    )

    install = commands.add_parser(
        "install", help="Validate and atomically install an exported model package"
    )
    install.add_argument("source")
    install.add_argument(
        "--models-dir",
        default=os.getenv("INTENT_CLASSIFIER_MODEL_ROOT", "/models"),
    )

    for name in ("preload", "activate"):
        command = commands.add_parser(name, help=f"{name.title()} a model through the admin API")
        command.add_argument("version")
        _add_admin_url(command)

    rollback = commands.add_parser("rollback", help="Atomically restore the previous model")
    _add_admin_url(rollback)

    list_command = commands.add_parser("list", help="List model packages and runtime slots")
    _add_admin_url(list_command)
    return parser


def _add_admin_url(parser: argparse.ArgumentParser) -> None:
    configured = os.getenv("INTENT_CLASSIFIER_ADMIN_URL", "").strip()
    if not configured:
        port = os.getenv("INTENT_CLASSIFIER_PORT", "8080").strip() or "8080"
        configured = f"http://127.0.0.1:{port}"
    parser.add_argument("--admin-url", default=configured.rstrip("/"))


def main(argv: Sequence[str] | None = None) -> int:
    parser = build_parser()
    args = parser.parse_args(argv)
    if args.command == "serve":
        return _serve()
    if args.command == "validate":
        return _validate(args.version, Path(args.model_root))
    if args.command == "install":
        return _install(Path(args.source), Path(args.models_dir))
    return _admin_command(args.command, getattr(args, "version", None), args.admin_url)


def _serve() -> int:
    try:
        settings = Settings.from_env()
    except ValueError as exc:
        _print_error("invalid_configuration", str(exc), stream=sys.stderr)
        return EXIT_VALIDATION
    uvicorn.run(
        create_app(settings),
        host=settings.host,
        port=settings.port,
        log_level=settings.log_level.lower(),
        access_log=False,
        proxy_headers=False,
        server_header=False,
    )
    return EXIT_SUCCESS


def _validate(version: str, model_root: Path) -> int:
    try:
        loaded = load_model_package(model_root, version)
    except ServiceError as exc:
        _print_error(exc.code, exc.detail or exc.message, stream=sys.stderr)
        return EXIT_VALIDATION
    print(
        json.dumps(
            {"status": "valid", **loaded.report.as_dict()},
            ensure_ascii=True,
            separators=(",", ":"),
        )
    )
    return EXIT_SUCCESS


def _install(source: Path, models_dir: Path) -> int:
    try:
        report = install_model_package(source, models_dir)
    except ServiceError as exc:
        _print_error(exc.code, exc.detail or exc.message, stream=sys.stderr)
        return EXIT_VALIDATION
    print(json.dumps(report.as_dict(), ensure_ascii=True, separators=(",", ":")))
    return EXIT_SUCCESS


def _admin_command(command: str, version: str | None, admin_url: str) -> int:
    token = os.getenv("INTENT_CLASSIFIER_ADMIN_TOKEN", "").strip()
    if not token:
        _print_error(
            "admin_token_missing",
            "INTENT_CLASSIFIER_ADMIN_TOKEN is required",
            stream=sys.stderr,
        )
        return EXIT_ADMIN_REJECTED

    quoted_version = urllib.parse.quote(version or "", safe="")
    if command == "list":
        path = "/admin/v1/models"
        method = "GET"
    elif command == "rollback":
        path = "/admin/v1/models/rollback"
        method = "POST"
    else:
        path = f"/admin/v1/models/{quoted_version}/{command}"
        method = "POST"
    request = urllib.request.Request(
        admin_url.rstrip("/") + path,
        data=None if method == "GET" else b"{}",
        method=method,
        headers={
            "Accept": "application/json",
            "Authorization": f"Bearer {token}",
            "Content-Type": "application/json",
        },
    )
    try:
        with urllib.request.urlopen(request, timeout=300) as response:
            payload = json.loads(response.read().decode("utf-8"))
    except urllib.error.HTTPError as exc:
        code, message = _safe_http_error(exc)
        _print_error(code, message, stream=sys.stderr)
        return EXIT_ADMIN_REJECTED if 400 <= exc.code < 500 else EXIT_UNAVAILABLE
    except (urllib.error.URLError, TimeoutError, OSError, UnicodeError, json.JSONDecodeError):
        _print_error("admin_unavailable", "Admin API is unavailable", stream=sys.stderr)
        return EXIT_UNAVAILABLE
    print(json.dumps(payload, ensure_ascii=True, separators=(",", ":")))
    return EXIT_SUCCESS


def _safe_http_error(exc: urllib.error.HTTPError) -> tuple[str, str]:
    try:
        raw = json.loads(exc.read(65_536).decode("utf-8"))
        error = raw.get("error", {}) if isinstance(raw, dict) else {}
        code = error.get("code") if isinstance(error, dict) else None
        message = error.get("message") if isinstance(error, dict) else None
        if isinstance(code, str) and isinstance(message, str):
            return code[:100], message[:300]
    except (OSError, UnicodeError, json.JSONDecodeError):
        pass
    return "admin_request_failed", "Admin API rejected the request"


def _print_error(code: str, message: str, *, stream: Any) -> None:
    print(
        json.dumps(
            {"status": "error", "code": code, "message": message},
            ensure_ascii=True,
            separators=(",", ":"),
        ),
        file=stream,
    )
