from __future__ import annotations

from typing import Literal

from pydantic import BaseModel, ConfigDict, Field


class StrictModel(BaseModel):
    model_config = ConfigDict(extra="forbid", strict=True)


class ClassifyContext(StrictModel):
    protocol: str | None = Field(default=None, max_length=64)
    endpoint: str | None = Field(default=None, max_length=256)
    model: str | None = Field(default=None, max_length=256)


class ClassifyRequest(StrictModel):
    schema_version: Literal["1"]
    request_id: str | None = Field(default=None, max_length=128)
    text: str = Field(min_length=1, max_length=12_000)
    matched_keyword: str = Field(min_length=1, max_length=200)
    context: ClassifyContext


class ClassifyResponse(StrictModel):
    schema_version: Literal["1"] = "1"
    label: Literal["benign", "actionable_probe"]
    score: float = Field(ge=0, le=1)
    model_version: str = Field(min_length=1, max_length=200)
    trace_id: str = Field(min_length=1, max_length=200)


class ErrorDetail(StrictModel):
    code: str
    message: str
    trace_id: str


class ErrorEnvelope(StrictModel):
    schema_version: Literal["1"] = "1"
    error: ErrorDetail
