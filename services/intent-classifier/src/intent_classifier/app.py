from __future__ import annotations

import asyncio
import ipaddress
import logging
import secrets
import time
import uuid
from collections.abc import AsyncIterator, Awaitable, Callable
from concurrent.futures import ThreadPoolExecutor
from contextlib import asynccontextmanager
from typing import Any

from fastapi import Depends, FastAPI, Request
from fastapi.exceptions import RequestValidationError
from fastapi.responses import JSONResponse
from starlette.concurrency import run_in_threadpool
from starlette.types import ASGIApp, Message, Receive, Scope, Send

from .errors import ServiceError
from .logging_config import configure_logging
from .manager import ModelLoader, ModelManager
from .model_package import load_model_package
from .schemas import ClassifyRequest, ClassifyResponse, ErrorDetail, ErrorEnvelope
from .settings import Settings

logger = logging.getLogger(__name__)


class RequestBodyLimitMiddleware:
    def __init__(self, app: ASGIApp, max_bytes: int) -> None:
        self.app = app
        self.max_bytes = max_bytes

    async def __call__(self, scope: Scope, receive: Receive, send: Send) -> None:
        if scope["type"] != "http" or scope.get("method") not in {"POST", "PUT", "PATCH"}:
            await self.app(scope, receive, send)
            return
        headers = {key.lower(): value for key, value in scope.get("headers", [])}
        raw_content_length = headers.get(b"content-length")
        if raw_content_length is not None:
            try:
                content_length = int(raw_content_length)
            except ValueError:
                await self._reject(scope, send, "invalid_request", 400)
                return
            if content_length > self.max_bytes:
                await self._reject(scope, send, "request_too_large", 413)
                return

        body = bytearray()
        while True:
            message = await receive()
            if message["type"] == "http.disconnect":
                return
            if message["type"] != "http.request":
                continue
            body.extend(message.get("body", b""))
            if len(body) > self.max_bytes:
                await self._reject(scope, send, "request_too_large", 413)
                return
            if not message.get("more_body", False):
                break

        delivered = False

        async def replay_receive() -> Message:
            nonlocal delivered
            if delivered:
                return {"type": "http.disconnect"}
            delivered = True
            return {"type": "http.request", "body": bytes(body), "more_body": False}

        await self.app(scope, replay_receive, send)

    @staticmethod
    async def _reject(scope: Scope, send: Send, code: str, status_code: int) -> None:
        trace_id = uuid.uuid4().hex
        message = (
            "Request body exceeds the configured limit"
            if code == "request_too_large"
            else "Request headers are invalid"
        )
        response = JSONResponse(
            status_code=status_code,
            content=ErrorEnvelope(
                error=ErrorDetail(code=code, message=message, trace_id=trace_id)
            ).model_dump(),
            headers={"X-Trace-ID": trace_id},
        )
        logger.warning(
            "request_rejected",
            extra={
                "event": "request_rejected",
                "trace_id": trace_id,
                "path": scope.get("path", ""),
                "method": scope.get("method", ""),
                "status_code": status_code,
                "error_code": code,
            },
        )
        await response(scope, _empty_receive, send)


async def _empty_receive() -> Message:
    return {"type": "http.request", "body": b"", "more_body": False}


def create_app(
    settings: Settings | None = None,
    *,
    model_loader: ModelLoader = load_model_package,
) -> FastAPI:
    resolved_settings = settings or Settings.from_env()
    configure_logging(resolved_settings.log_level)
    manager = ModelManager(
        resolved_settings.model_root,
        resolved_settings.state_dir,
        loader=model_loader,
    )

    @asynccontextmanager
    async def lifespan(application: FastAPI) -> AsyncIterator[None]:
        application.state.manager = manager
        application.state.settings = resolved_settings
        application.state.inference_slots = asyncio.Queue(maxsize=resolved_settings.max_concurrency)
        application.state.inference_executor = ThreadPoolExecutor(
            max_workers=resolved_settings.max_concurrency,
            thread_name_prefix="intent-inference",
        )
        for _ in range(resolved_settings.max_concurrency):
            application.state.inference_slots.put_nowait(object())
        try:
            await run_in_threadpool(manager.restore, resolved_settings.startup_model_version)
        except ServiceError as exc:
            logger.error(
                "startup_model_load_failed",
                extra={
                    "event": "startup_model_load_failed",
                    "model_version": resolved_settings.startup_model_version,
                    "error_code": exc.code,
                },
            )
        try:
            yield
        finally:
            application.state.inference_executor.shutdown(wait=False, cancel_futures=True)

    app = FastAPI(
        title="Sub2API Intent Classifier",
        version="1",
        docs_url=None,
        redoc_url=None,
        openapi_url=None,
        lifespan=lifespan,
    )
    app.state.manager = manager
    app.state.settings = resolved_settings
    app.add_middleware(RequestBodyLimitMiddleware, max_bytes=resolved_settings.max_request_bytes)

    @app.middleware("http")
    async def request_observability(
        request: Request, call_next: Callable[[Request], Awaitable[Any]]
    ) -> Any:
        trace_id = uuid.uuid4().hex
        request.state.trace_id = trace_id
        started = time.perf_counter()
        response = await call_next(request)
        latency_ms = round((time.perf_counter() - started) * 1000, 3)
        response.headers["X-Trace-ID"] = trace_id
        logger.info(
            "request_complete",
            extra={
                "event": "request_complete",
                "trace_id": trace_id,
                "path": request.url.path,
                "method": request.method,
                "status_code": response.status_code,
                "latency_ms": latency_ms,
                "model_version": getattr(request.state, "model_version", ""),
            },
        )
        return response

    @app.exception_handler(ServiceError)
    async def service_error_handler(request: Request, exc: ServiceError) -> JSONResponse:
        if exc.status_code >= 500:
            logger.error(
                "service_error",
                extra={
                    "event": "service_error",
                    "trace_id": _trace_id(request),
                    "error_code": exc.code,
                },
            )
        return _error_response(request, exc.status_code, exc.code, exc.message)

    @app.exception_handler(RequestValidationError)
    async def validation_error_handler(
        request: Request, exc: RequestValidationError
    ) -> JSONResponse:
        body = exc.body
        code = "invalid_request"
        status_code = 400
        message = "Request does not match the V1 schema"
        if isinstance(body, dict) and body.get("schema_version") not in (None, "1"):
            code = "unsupported_schema"
            message = "Unsupported schema version"
        elif any(
            error.get("loc", ())[-1:] == ("text",) and error.get("type") == "string_too_long"
            for error in exc.errors()
        ):
            code = "text_too_long"
            status_code = 413
            message = "Text exceeds the maximum length"
        return _error_response(request, status_code, code, message)

    @app.exception_handler(Exception)
    async def unexpected_error_handler(request: Request, exc: Exception) -> JSONResponse:
        logger.error(
            "unexpected_error",
            extra={
                "event": "unexpected_error",
                "trace_id": _trace_id(request),
                "error_code": type(exc).__name__,
            },
        )
        return _error_response(
            request, 500, "internal_error", "Intent classifier failed unexpectedly"
        )

    async def require_api_token(request: Request) -> None:
        configured = resolved_settings.api_token
        if configured and not _valid_bearer(request, configured):
            raise ServiceError(401, "unauthorized", "Classifier authorization failed")

    async def require_admin_token(request: Request) -> None:
        if resolved_settings.admin_loopback_only and not _is_loopback(request):
            raise ServiceError(404, "not_found", "Resource not found")
        if not resolved_settings.admin_token:
            raise ServiceError(503, "admin_disabled", "Admin API is disabled")
        if not _valid_bearer(request, resolved_settings.admin_token):
            raise ServiceError(401, "unauthorized", "Admin authorization failed")

    @app.get("/health/live")
    async def health_live() -> dict[str, str]:
        return {"status": "live"}

    @app.get("/health/ready")
    async def health_ready() -> JSONResponse:
        state = manager.ready_state()
        if state is None:
            return JSONResponse(
                status_code=503,
                content={
                    "status": "not_ready",
                    "code": "model_not_ready",
                    "active_model_version": None,
                    "preprocessing_version": None,
                },
            )
        model_version, preprocessing_version = state
        return JSONResponse(
            content={
                "status": "ready",
                "active_model_version": model_version,
                "preprocessing_version": preprocessing_version,
            }
        )

    @app.post("/v1/classify", response_model=ClassifyResponse)
    async def classify(
        payload: ClassifyRequest,
        request: Request,
        _authorized: None = Depends(require_api_token),
    ) -> ClassifyResponse:
        slots: asyncio.Queue[object] = request.app.state.inference_slots
        try:
            slot = slots.get_nowait()
        except asyncio.QueueEmpty as exc:
            raise ServiceError(429, "busy", "Classifier concurrency limit reached") from exc
        try:
            loop = asyncio.get_running_loop()
            executor: ThreadPoolExecutor = request.app.state.inference_executor
            inference = loop.run_in_executor(
                executor, manager.predict, payload.text, payload.matched_keyword
            )
        except Exception:
            slots.put_nowait(slot)
            raise

        def release_slot(_completed: asyncio.Future[Any]) -> None:
            slots.put_nowait(slot)

        inference.add_done_callback(release_slot)
        try:
            active, prediction = await asyncio.wait_for(
                asyncio.shield(inference),
                timeout=resolved_settings.inference_timeout_ms / 1000,
            )
        except TimeoutError as exc:
            raise ServiceError(504, "inference_timeout", "Model inference timed out") from exc
        request.state.model_version = active.model_version
        return ClassifyResponse(
            label=prediction.label,
            score=prediction.score,
            model_version=active.model_version,
            trace_id=_trace_id(request),
        )

    @app.get("/admin/v1/models", dependencies=[Depends(require_admin_token)])
    async def list_models() -> dict[str, Any]:
        return manager.list_models()

    @app.post(
        "/admin/v1/models/{version}/validate",
        dependencies=[Depends(require_admin_token)],
    )
    async def validate_model(version: str) -> dict[str, Any]:
        report = await run_in_threadpool(manager.validate, version)
        return {"status": "valid", **report.as_dict()}

    @app.post(
        "/admin/v1/models/{version}/preload",
        dependencies=[Depends(require_admin_token)],
    )
    async def preload_model(version: str) -> dict[str, Any]:
        model = await run_in_threadpool(manager.preload, version)
        return {"status": "preloaded", "model": model}

    @app.post(
        "/admin/v1/models/{version}/activate",
        dependencies=[Depends(require_admin_token)],
    )
    async def activate_model(version: str) -> dict[str, Any]:
        model = manager.activate(version)
        return {"status": "active", "model": model}

    @app.post(
        "/admin/v1/models/rollback",
        dependencies=[Depends(require_admin_token)],
    )
    async def rollback_model() -> dict[str, Any]:
        model = manager.rollback()
        return {"status": "active", "model": model}

    return app


def _trace_id(request: Request) -> str:
    return str(getattr(request.state, "trace_id", uuid.uuid4().hex))


def _error_response(request: Request, status_code: int, code: str, message: str) -> JSONResponse:
    return JSONResponse(
        status_code=status_code,
        content=ErrorEnvelope(
            error=ErrorDetail(code=code, message=message, trace_id=_trace_id(request))
        ).model_dump(),
    )


def _valid_bearer(request: Request, expected: str) -> bool:
    authorization = request.headers.get("Authorization", "")
    scheme, separator, token = authorization.partition(" ")
    return (
        separator == " "
        and scheme.lower() == "bearer"
        and bool(token)
        and secrets.compare_digest(token, expected)
    )


def _is_loopback(request: Request) -> bool:
    if request.client is None:
        return False
    try:
        return ipaddress.ip_address(request.client.host).is_loopback
    except ValueError:
        return False
