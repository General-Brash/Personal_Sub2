from __future__ import annotations

from typing import Literal

from pydantic import BaseModel, ConfigDict, Field, model_validator


class StrictPackageModel(BaseModel):
    model_config = ConfigDict(extra="forbid", strict=True)


class FileIntegrity(StrictPackageModel):
    sha256: str = Field(pattern=r"^[0-9a-f]{64}$")
    size: int = Field(gt=0, le=536_870_912)


class RuntimeContract(StrictPackageModel):
    format: Literal["onnx"]
    opset: int = Field(ge=11, le=21)


class InputContract(StrictPackageModel):
    dtype: Literal["int64"]
    shape: list[int] = Field(min_length=2, max_length=2)


class OutputContract(StrictPackageModel):
    name: Literal["logits"]
    dtype: Literal["float32"]
    shape: list[int] = Field(min_length=2, max_length=2)


class Manifest(StrictPackageModel):
    schema_version: Literal["1"]
    model_version: str = Field(pattern=r"^[A-Za-z0-9][A-Za-z0-9._-]{0,199}$")
    preprocessing_version: str = Field(pattern=r"^[A-Za-z0-9][A-Za-z0-9._-]{0,199}$")
    created_at: str = Field(min_length=1, max_length=64)
    runtime: RuntimeContract
    files: dict[str, FileIntegrity]
    inputs: dict[str, InputContract]
    output: OutputContract


class Labels(StrictPackageModel):
    schema_version: Literal["1"]
    labels: list[Literal["benign", "actionable_probe"]] = Field(min_length=2, max_length=2)
    actionable_probe_index: int


class Preprocessing(StrictPackageModel):
    schema_version: Literal["1"]
    version: str = Field(pattern=r"^[A-Za-z0-9][A-Za-z0-9._-]{0,199}$")
    normalization: Literal["NFKC"]
    control_characters: Literal["replace_with_space"]
    whitespace: Literal["collapse"]
    input_template: Literal["text", "keyword_text_v1"]
    max_text_characters: Literal[12000]
    max_keyword_characters: Literal[200]
    max_length: int = Field(ge=8, le=512)
    stride: int = Field(ge=0, le=511)
    max_chunks: int = Field(ge=1, le=256)
    pad_id: int = Field(ge=0)
    pad_token: str = Field(min_length=1, max_length=100)

    @model_validator(mode="after")
    def validate_stride(self) -> Preprocessing:
        if self.stride >= self.max_length:
            raise ValueError("stride must be smaller than max_length")
        return self


class CalibrationConfig(StrictPackageModel):
    schema_version: Literal["1"]
    method: Literal["identity", "temperature"]
    temperature: float | None = Field(default=None, gt=0, le=100)

    @model_validator(mode="after")
    def validate_method_parameters(self) -> CalibrationConfig:
        if self.method == "temperature" and self.temperature is None:
            raise ValueError("temperature calibration requires temperature")
        if self.method == "identity" and self.temperature is not None:
            raise ValueError("identity calibration must not define temperature")
        return self


class GoldenCase(StrictPackageModel):
    id: str = Field(pattern=r"^[A-Za-z0-9][A-Za-z0-9._-]{0,99}$")
    text: str = Field(min_length=1, max_length=12_000)
    matched_keyword: str = Field(min_length=1, max_length=200)
    expected_label: Literal["benign", "actionable_probe"]
    min_score: float = Field(ge=0, le=1)
    max_score: float = Field(ge=0, le=1)

    @model_validator(mode="after")
    def validate_score_range(self) -> GoldenCase:
        if self.min_score > self.max_score:
            raise ValueError("min_score must not exceed max_score")
        return self


class GoldenCases(StrictPackageModel):
    schema_version: Literal["1"]
    cases: list[GoldenCase] = Field(min_length=1, max_length=200)
