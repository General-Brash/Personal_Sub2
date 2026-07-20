from __future__ import annotations

import hashlib
import json
import math
import os
import re
import threading
import unicodedata
from dataclasses import dataclass
from datetime import UTC, datetime
from pathlib import Path, PurePosixPath
from typing import Any, Literal, TypeVar

import numpy as np
import onnxruntime as ort
from pydantic import BaseModel, ValidationError
from tokenizers import Tokenizer

from .errors import ModelPackageError, ServiceError
from .package_schema import CalibrationConfig, GoldenCases, Labels, Manifest, Preprocessing

PACKAGE_SCHEMA_VERSION = "1"
MODEL_VERSION_PATTERN = re.compile(r"^[A-Za-z0-9][A-Za-z0-9._-]{0,199}$")
REQUIRED_ROOT_FILES = frozenset(
    {
        "manifest.json",
        "model.onnx",
        "preprocessing.json",
        "labels.json",
        "calibration.json",
        "golden_cases.json",
    }
)
MAX_CONFIGURATION_BYTES = 2 * 1024 * 1024
MAX_TOKENIZER_BYTES = 128 * 1024 * 1024
MAX_MODEL_BYTES = 512 * 1024 * 1024
MAX_PACKAGE_BYTES = 768 * 1024 * 1024
MAX_PACKAGE_FILES = 64
MAX_TOKENIZER_DEPTH = 4
SAFE_TOKENIZER_EXTENSIONS = frozenset({".json", ".merges", ".model", ".txt", ".vocab"})

TModel = TypeVar("TModel", bound=BaseModel)


@dataclass(frozen=True, slots=True)
class Prediction:
    label: Literal["benign", "actionable_probe"]
    score: float


@dataclass(frozen=True, slots=True)
class ValidationReport:
    model_version: str
    preprocessing_version: str
    package_path: str
    file_count: int
    golden_case_count: int
    validated_at: str

    def as_dict(self) -> dict[str, Any]:
        return {
            "model_version": self.model_version,
            "preprocessing_version": self.preprocessing_version,
            "package_path": self.package_path,
            "file_count": self.file_count,
            "golden_case_count": self.golden_case_count,
            "validated_at": self.validated_at,
        }


@dataclass(slots=True)
class LoadedModel:
    model_version: str
    preprocessing_version: str
    package_path: Path
    manifest: Manifest
    preprocessing: Preprocessing
    labels: Labels
    calibration: CalibrationConfig
    tokenizer: Tokenizer
    session: ort.InferenceSession
    report: ValidationReport
    loaded_at: str
    _tokenizer_lock: threading.Lock

    def predict(self, text: str, matched_keyword: str) -> Prediction:
        model_input = build_model_input(text, matched_keyword, self.preprocessing)
        with self._tokenizer_lock:
            encoding = self.tokenizer.encode(model_input, add_special_tokens=True)
            encodings = [encoding, *encoding.overflowing]
        if len(encodings) > self.preprocessing.max_chunks:
            raise ServiceError(
                413,
                "text_too_long",
                "Text exceeds the configured token chunk limit",
            )

        scores: list[float] = []
        for chunk in encodings:
            input_ids = np.asarray([chunk.ids], dtype=np.int64)
            attention_mask = np.asarray([chunk.attention_mask], dtype=np.int64)
            expected_shape = (1, self.preprocessing.max_length)
            if input_ids.shape != expected_shape or attention_mask.shape != expected_shape:
                raise ServiceError(500, "inference_failed", "Inference preprocessing failed")
            try:
                output = self.session.run(
                    [self.manifest.output.name],
                    {"input_ids": input_ids, "attention_mask": attention_mask},
                )[0]
            except Exception as exc:  # onnxruntime exposes provider-specific exception types
                raise ServiceError(
                    500,
                    "inference_failed",
                    "Model inference failed",
                    detail=type(exc).__name__,
                ) from exc
            logits = np.asarray(output, dtype=np.float64)
            if logits.shape != (1, 2) or not np.isfinite(logits).all():
                raise ServiceError(500, "inference_failed", "Model returned invalid logits")
            scores.append(_actionable_probability(logits[0], self.calibration))

        if not scores:
            raise ServiceError(500, "inference_failed", "Model produced no token chunks")
        score = max(scores)
        label: Literal["benign", "actionable_probe"] = (
            "actionable_probe" if score >= 0.5 else "benign"
        )
        return Prediction(label=label, score=score)


def normalize_text(value: str) -> str:
    normalized = unicodedata.normalize("NFKC", value)
    without_controls = "".join(
        " " if unicodedata.category(character) in {"Cc", "Cf"} else character
        for character in normalized
    )
    return " ".join(without_controls.split())


def build_model_input(text: str, matched_keyword: str, config: Preprocessing) -> str:
    normalized_text = normalize_text(text)
    normalized_keyword = normalize_text(matched_keyword)
    if not normalized_text or not normalized_keyword:
        raise ServiceError(400, "invalid_request", "Text and matched keyword are required")
    if len(normalized_text) > config.max_text_characters:
        raise ServiceError(413, "text_too_long", "Text exceeds the maximum length")
    if len(normalized_keyword) > config.max_keyword_characters:
        raise ServiceError(400, "invalid_request", "Matched keyword exceeds the maximum length")
    if config.input_template == "keyword_text_v1":
        return f"{normalized_keyword} [SEP] {normalized_text}"
    return normalized_text


def load_model_package(model_root: Path, version: str) -> LoadedModel:
    package_path = _resolve_package_path(model_root, version)
    return _load_model_package_at_path(package_path, expected_version=version)


def load_model_package_path(package_path: Path) -> LoadedModel:
    source = package_path.expanduser()
    if source.is_symlink() or not source.exists() or not source.is_dir():
        raise ModelPackageError("Model package source must be a regular directory")
    return _load_model_package_at_path(source.resolve(), expected_version=None)


def _load_model_package_at_path(package_path: Path, *, expected_version: str | None) -> LoadedModel:
    package_files, tokenizer_path = _validate_package_tree(package_path)
    manifest = _load_json_model(package_path / "manifest.json", Manifest)
    if expected_version is not None and manifest.model_version != expected_version:
        raise ModelPackageError("Manifest model version does not match its directory")
    _validate_manifest_paths(manifest)
    if set(manifest.files) != set(package_files) - {"manifest.json"}:
        raise ModelPackageError("Manifest file list does not match the V1 package contract")
    _validate_hashes(package_files, manifest)

    preprocessing = _load_json_model(package_path / "preprocessing.json", Preprocessing)
    labels = _load_json_model(package_path / "labels.json", Labels)
    calibration = _load_json_model(package_path / "calibration.json", CalibrationConfig)
    golden_cases = _load_json_model(package_path / "golden_cases.json", GoldenCases)
    _validate_declarations(manifest, preprocessing, labels)
    _validate_golden_declarations(golden_cases)
    _validate_tokenizer_json(tokenizer_path)

    try:
        tokenizer = Tokenizer.from_file(str(tokenizer_path))
    except Exception as exc:
        raise ModelPackageError("Tokenizer could not be loaded", detail=type(exc).__name__) from exc
    if tokenizer.token_to_id(preprocessing.pad_token) != preprocessing.pad_id:
        raise ModelPackageError("Tokenizer padding token does not match preprocessing.json")
    tokenizer.enable_truncation(
        max_length=preprocessing.max_length,
        stride=preprocessing.stride,
        direction="right",
    )
    tokenizer.enable_padding(
        direction="right",
        pad_id=preprocessing.pad_id,
        pad_type_id=0,
        pad_token=preprocessing.pad_token,
        length=preprocessing.max_length,
    )

    session = _load_onnx_session(package_path / "model.onnx")
    _validate_onnx_contract(session, manifest, preprocessing)

    validated_at = datetime.now(UTC).isoformat()
    report = ValidationReport(
        model_version=manifest.model_version,
        preprocessing_version=preprocessing.version,
        package_path=str(package_path),
        file_count=len(package_files),
        golden_case_count=len(golden_cases.cases),
        validated_at=validated_at,
    )
    loaded = LoadedModel(
        model_version=manifest.model_version,
        preprocessing_version=preprocessing.version,
        package_path=package_path,
        manifest=manifest,
        preprocessing=preprocessing,
        labels=labels,
        calibration=calibration,
        tokenizer=tokenizer,
        session=session,
        report=report,
        loaded_at=validated_at,
        _tokenizer_lock=threading.Lock(),
    )
    _warmup_and_validate_golden_cases(loaded, golden_cases)
    return loaded


def _resolve_package_path(model_root: Path, version: str) -> Path:
    if not MODEL_VERSION_PATTERN.fullmatch(version):
        raise ModelPackageError("Model version is invalid")
    root = model_root.expanduser().resolve()
    package_path = root / version
    if not package_path.exists() or not package_path.is_dir() or package_path.is_symlink():
        raise ModelPackageError("Model package directory does not exist")
    resolved = package_path.resolve()
    if resolved.parent != root:
        raise ModelPackageError("Model package must be a direct child of the model root")
    return resolved


def _validate_package_tree(package_path: Path) -> tuple[dict[str, Path], Path]:
    root_entries = {child.name: child for child in package_path.iterdir()}
    flat_tokenizer = root_entries.get("tokenizer.json")
    tokenizer_directory = root_entries.get("tokenizer")
    if (flat_tokenizer is None) == (tokenizer_directory is None):
        raise ModelPackageError(
            "Model package must contain tokenizer.json or tokenizer/, but not both"
        )
    expected_root = set(REQUIRED_ROOT_FILES)
    expected_root.add("tokenizer.json" if flat_tokenizer is not None else "tokenizer")
    if set(root_entries) != expected_root:
        raise ModelPackageError("Model package root does not match the V1 contract")

    files: dict[str, Path] = {}
    for name in REQUIRED_ROOT_FILES:
        path = root_entries[name]
        _validate_regular_data_file(path, name)
        files[name] = path

    if flat_tokenizer is not None:
        _validate_regular_data_file(flat_tokenizer, "tokenizer.json")
        files["tokenizer.json"] = flat_tokenizer
        tokenizer_path = flat_tokenizer
    else:
        assert tokenizer_directory is not None
        if tokenizer_directory.is_symlink() or not tokenizer_directory.is_dir():
            raise ModelPackageError("tokenizer must be a regular directory")
        tokenizer_path = tokenizer_directory / "tokenizer.json"
        if not tokenizer_path.exists():
            raise ModelPackageError("tokenizer/tokenizer.json is required")
        for current_root, directories, filenames in os.walk(tokenizer_directory, followlinks=False):
            current = Path(current_root)
            relative_directory = current.relative_to(tokenizer_directory)
            if len(relative_directory.parts) > MAX_TOKENIZER_DEPTH:
                raise ModelPackageError("Tokenizer directory nesting is too deep")
            for directory_name in directories:
                directory = current / directory_name
                if directory.is_symlink() or not _safe_component(directory_name):
                    raise ModelPackageError("Tokenizer directory contains an unsafe path")
            for filename in filenames:
                path = current / filename
                relative = path.relative_to(package_path).as_posix()
                if not _safe_component(filename):
                    raise ModelPackageError("Tokenizer directory contains an unsafe path")
                if path.suffix.lower() not in SAFE_TOKENIZER_EXTENSIONS:
                    raise ModelPackageError("Tokenizer directory contains a disallowed file type")
                _validate_regular_data_file(path, relative)
                files[relative] = path

    if len(files) > MAX_PACKAGE_FILES:
        raise ModelPackageError("Model package contains too many files")
    total_size = sum(path.stat().st_size for path in files.values())
    if total_size > MAX_PACKAGE_BYTES:
        raise ModelPackageError("Model package exceeds the total size limit")
    if files["model.onnx"].stat().st_size > MAX_MODEL_BYTES:
        raise ModelPackageError("model.onnx exceeds the size limit")
    for relative, path in files.items():
        if relative.startswith("tokenizer") and path.stat().st_size > MAX_TOKENIZER_BYTES:
            raise ModelPackageError("Tokenizer file exceeds the size limit")
    return files, tokenizer_path


def _validate_regular_data_file(path: Path, relative: str) -> None:
    if path.is_symlink() or not path.is_file():
        raise ModelPackageError(f"Package entry is not a regular file: {relative}")
    if path.stat().st_size <= 0:
        raise ModelPackageError(f"Empty package files are not allowed: {relative}")


def _safe_component(value: str) -> bool:
    return (
        bool(value)
        and value not in {".", ".."}
        and bool(re.fullmatch(r"[A-Za-z0-9][A-Za-z0-9._-]{0,199}", value))
    )


def _validate_manifest_paths(manifest: Manifest) -> None:
    for raw_path in manifest.files:
        path = PurePosixPath(raw_path)
        if (
            not raw_path
            or "\\" in raw_path
            or path.is_absolute()
            or path.as_posix() != raw_path
            or any(part in {"", ".", ".."} for part in path.parts)
            or len(path.parts) > MAX_TOKENIZER_DEPTH + 2
        ):
            raise ModelPackageError("Manifest contains an unsafe file path")
        if (
            raw_path != "model.onnx"
            and raw_path
            not in {
                "preprocessing.json",
                "labels.json",
                "calibration.json",
                "golden_cases.json",
                "tokenizer.json",
            }
            and not raw_path.startswith("tokenizer/")
        ):
            raise ModelPackageError("Manifest contains a disallowed file path")


def _validate_hashes(package_files: dict[str, Path], manifest: Manifest) -> None:
    for filename, expected in manifest.files.items():
        path = package_files[filename]
        size = path.stat().st_size
        if size != expected.size:
            raise ModelPackageError(f"File size mismatch: {filename}")
        digest = hashlib.sha256()
        with path.open("rb") as handle:
            for chunk in iter(lambda: handle.read(1024 * 1024), b""):
                digest.update(chunk)
        if not _constant_time_hex_equal(digest.hexdigest(), expected.sha256):
            raise ModelPackageError(f"File hash mismatch: {filename}")


def _constant_time_hex_equal(left: str, right: str) -> bool:
    import secrets

    return secrets.compare_digest(left, right)


def _load_json_model(path: Path, model_type: type[TModel]) -> TModel:
    raw = _load_json(path, MAX_CONFIGURATION_BYTES)
    try:
        return model_type.model_validate(raw)
    except ValidationError as exc:
        raise ModelPackageError(f"Invalid schema: {path.name}", detail=type(exc).__name__) from exc


def _load_json(path: Path, limit: int) -> Any:
    if path.stat().st_size > limit:
        raise ModelPackageError(f"JSON file is too large: {path.name}")
    try:
        return json.loads(
            path.read_text(encoding="utf-8"),
            object_pairs_hook=_reject_duplicate_keys,
            parse_constant=lambda value: _raise_invalid_json_constant(value),
        )
    except (OSError, UnicodeError, json.JSONDecodeError, ValueError) as exc:
        raise ModelPackageError(f"Invalid JSON: {path.name}", detail=type(exc).__name__) from exc


def _reject_duplicate_keys(pairs: list[tuple[str, Any]]) -> dict[str, Any]:
    result: dict[str, Any] = {}
    for key, value in pairs:
        if key in result:
            raise ValueError(f"duplicate key: {key}")
        result[key] = value
    return result


def _raise_invalid_json_constant(value: str) -> Any:
    raise ValueError(f"invalid JSON constant: {value}")


def _validate_tokenizer_json(path: Path) -> None:
    raw = _load_json(path, MAX_TOKENIZER_BYTES)
    if not isinstance(raw, dict):
        raise ModelPackageError("tokenizer.json must contain a JSON object")
    model = raw.get("model")
    if not isinstance(model, dict) or model.get("type") not in {
        "BPE",
        "Unigram",
        "WordLevel",
        "WordPiece",
    }:
        raise ModelPackageError("tokenizer.json uses an unsupported tokenizer model")


def _validate_declarations(
    manifest: Manifest, preprocessing: Preprocessing, labels: Labels
) -> None:
    try:
        created_at = datetime.fromisoformat(manifest.created_at.replace("Z", "+00:00"))
    except ValueError as exc:
        raise ModelPackageError("manifest.json created_at must be RFC3339") from exc
    if created_at.tzinfo is None or created_at.utcoffset() != UTC.utcoffset(created_at):
        raise ModelPackageError("manifest.json created_at must use UTC")
    if preprocessing.version != manifest.preprocessing_version:
        raise ModelPackageError("Preprocessing version does not match manifest.json")
    if labels.labels != ["benign", "actionable_probe"]:
        raise ModelPackageError("labels.json must use the frozen V1 label order")
    if labels.actionable_probe_index != 1:
        raise ModelPackageError("actionable_probe_index must be 1")
    if set(manifest.inputs) != {"input_ids", "attention_mask"}:
        raise ModelPackageError("ONNX inputs must be input_ids and attention_mask")
    expected_input_shape = (1, preprocessing.max_length)
    for contract in manifest.inputs.values():
        if tuple(contract.shape) != expected_input_shape:
            raise ModelPackageError("Manifest input shape does not match preprocessing")
    if manifest.output.shape != [1, 2]:
        raise ModelPackageError("Manifest output shape must be [1, 2]")


def _validate_golden_declarations(golden_cases: GoldenCases) -> None:
    identifiers = [case.id for case in golden_cases.cases]
    if len(identifiers) != len(set(identifiers)):
        raise ModelPackageError("Golden case identifiers must be unique")


def _load_onnx_session(path: Path) -> ort.InferenceSession:
    options = ort.SessionOptions()
    options.intra_op_num_threads = 1
    options.inter_op_num_threads = 1
    options.graph_optimization_level = ort.GraphOptimizationLevel.ORT_ENABLE_ALL
    try:
        return ort.InferenceSession(
            str(path),
            sess_options=options,
            providers=["CPUExecutionProvider"],
        )
    except Exception as exc:
        raise ModelPackageError(
            "ONNX model could not be loaded", detail=type(exc).__name__
        ) from exc


def _validate_onnx_contract(
    session: ort.InferenceSession,
    manifest: Manifest,
    preprocessing: Preprocessing,
) -> None:
    inputs = {item.name: item for item in session.get_inputs()}
    if set(inputs) != {"input_ids", "attention_mask"}:
        raise ModelPackageError("ONNX input names do not match the V1 contract")
    expected_shape = [1, preprocessing.max_length]
    for name, value in inputs.items():
        if value.type != "tensor(int64)" or list(value.shape) != expected_shape:
            raise ModelPackageError(f"ONNX input contract mismatch: {name}")
    outputs = session.get_outputs()
    if len(outputs) != 1:
        raise ModelPackageError("ONNX model must expose exactly one output")
    output = outputs[0]
    if output.name != manifest.output.name or output.type != "tensor(float)":
        raise ModelPackageError("ONNX output name or dtype does not match manifest")
    if list(output.shape) != [1, 2]:
        raise ModelPackageError("ONNX output shape must be [1, 2]")

    metadata = session.get_modelmeta().custom_metadata_map
    expected_metadata = {
        "intent_classifier_schema_version": PACKAGE_SCHEMA_VERSION,
        "model_version": manifest.model_version,
        "preprocessing_version": manifest.preprocessing_version,
        "onnx_opset": str(manifest.runtime.opset),
    }
    for key, expected in expected_metadata.items():
        if metadata.get(key) != expected:
            raise ModelPackageError(f"ONNX metadata mismatch: {key}")


def _actionable_probability(logits: np.ndarray[Any, Any], calibration: CalibrationConfig) -> float:
    temperature = calibration.temperature if calibration.method == "temperature" else 1.0
    assert temperature is not None
    calibrated = logits / temperature
    calibrated -= np.max(calibrated)
    exponentials = np.exp(calibrated)
    denominator = float(np.sum(exponentials))
    if not math.isfinite(denominator) or denominator <= 0:
        raise ServiceError(500, "inference_failed", "Calibration produced invalid output")
    score = float(exponentials[1] / denominator)
    if not math.isfinite(score) or score < 0 or score > 1:
        raise ServiceError(500, "inference_failed", "Calibration produced invalid score")
    return score


def _warmup_and_validate_golden_cases(loaded: LoadedModel, golden_cases: GoldenCases) -> None:
    first = golden_cases.cases[0]
    try:
        loaded.predict(first.text, first.matched_keyword)
        for case in golden_cases.cases:
            prediction = loaded.predict(case.text, case.matched_keyword)
            if prediction.label != case.expected_label:
                raise ModelPackageError(f"Golden case label mismatch: {case.id}")
            if prediction.score < case.min_score or prediction.score > case.max_score:
                raise ModelPackageError(f"Golden case score mismatch: {case.id}")
    except ModelPackageError:
        raise
    except ServiceError as exc:
        raise ModelPackageError(
            "Model warmup or golden-case validation failed", detail=exc.code
        ) from exc
