from __future__ import annotations


class ServiceError(Exception):
    """A safe error that may be returned through the HTTP or CLI boundary."""

    def __init__(self, status_code: int, code: str, message: str, *, detail: str = "") -> None:
        super().__init__(message)
        self.status_code = status_code
        self.code = code
        self.message = message
        self.detail = detail


class ModelPackageError(ServiceError):
    def __init__(self, message: str, *, detail: str = "") -> None:
        internal_detail = f"{message}: {detail}" if detail else message
        super().__init__(
            422,
            "invalid_model_package",
            "Model package validation failed",
            detail=internal_detail,
        )

    def __str__(self) -> str:
        return self.detail
