from __future__ import annotations

import hashlib
import json
import math
from collections.abc import Callable
from pathlib import Path
from typing import Any

import onnx
import pytest
from onnx import TensorProto, helper
from tokenizers import Tokenizer
from tokenizers.models import WordLevel
from tokenizers.pre_tokenizers import Whitespace

from intent_classifier.settings import Settings

PackageFactory = Callable[..., Path]


def _write_json(path: Path, value: Any) -> None:
    path.write_text(
        json.dumps(value, ensure_ascii=True, separators=(",", ":")) + "\n",
        encoding="utf-8",
    )


def _sha256(path: Path) -> str:
    return hashlib.sha256(path.read_bytes()).hexdigest()


@pytest.fixture
def package_factory() -> PackageFactory:
    def create(
        model_root: Path,
        version: str,
        *,
        logits: tuple[float, float] = (2.0, -2.0),
        tokenizer_directory: bool = False,
        preprocessing_version: str = "fixture-text-v1",
        max_length: int = 8,
        calibration_temperature: float | None = 1.0,
    ) -> Path:
        package = model_root / version
        package.mkdir(parents=True)

        input_ids = helper.make_tensor_value_info("input_ids", TensorProto.INT64, [1, max_length])
        attention_mask = helper.make_tensor_value_info(
            "attention_mask", TensorProto.INT64, [1, max_length]
        )
        logits_output = helper.make_tensor_value_info("logits", TensorProto.FLOAT, [1, 2])
        tensor = helper.make_tensor("fixture_logits", TensorProto.FLOAT, [1, 2], list(logits))
        node = helper.make_node("Constant", inputs=[], outputs=["logits"], value=tensor)
        graph = helper.make_graph(
            [node],
            "intent-classifier-fixture",
            [input_ids, attention_mask],
            [logits_output],
        )
        model = helper.make_model(graph, opset_imports=[helper.make_opsetid("", 17)])
        model.ir_version = 9
        helper.set_model_props(
            model,
            {
                "intent_classifier_schema_version": "1",
                "model_version": version,
                "preprocessing_version": preprocessing_version,
                "onnx_opset": "17",
            },
        )
        onnx.checker.check_model(model)
        onnx.save(model, package / "model.onnx")

        tokenizer = Tokenizer(
            WordLevel(
                vocab={
                    "[PAD]": 0,
                    "[UNK]": 1,
                    "safe": 2,
                    "scan": 3,
                    "target": 4,
                    "history": 5,
                    "[SEP]": 6,
                },
                unk_token="[UNK]",
            )
        )
        tokenizer.pre_tokenizer = Whitespace()
        if tokenizer_directory:
            tokenizer_root = package / "tokenizer"
            tokenizer_root.mkdir()
            tokenizer.save(str(tokenizer_root / "tokenizer.json"))
            (tokenizer_root / "vocab.txt").write_text(
                "[PAD]\n[UNK]\nsafe\nscan\ntarget\nhistory\n[SEP]\n", encoding="utf-8"
            )
        else:
            tokenizer.save(str(package / "tokenizer.json"))

        _write_json(
            package / "preprocessing.json",
            {
                "schema_version": "1",
                "version": preprocessing_version,
                "normalization": "NFKC",
                "control_characters": "replace_with_space",
                "whitespace": "collapse",
                "input_template": "text",
                "max_text_characters": 12000,
                "max_keyword_characters": 200,
                "max_length": max_length,
                "stride": 2,
                "max_chunks": 16,
                "pad_id": 0,
                "pad_token": "[PAD]",
            },
        )
        _write_json(
            package / "labels.json",
            {
                "schema_version": "1",
                "labels": ["benign", "actionable_probe"],
                "actionable_probe_index": 1,
            },
        )
        calibration: dict[str, Any] = {
            "schema_version": "1",
            "method": "identity" if calibration_temperature is None else "temperature",
        }
        effective_temperature = 1.0
        if calibration_temperature is not None:
            calibration["temperature"] = calibration_temperature
            effective_temperature = calibration_temperature
        _write_json(package / "calibration.json", calibration)
        calibrated_logits = (
            logits[0] / effective_temperature,
            logits[1] / effective_temperature,
        )
        score = math.exp(calibrated_logits[1]) / (
            math.exp(calibrated_logits[0]) + math.exp(calibrated_logits[1])
        )
        expected_label = "actionable_probe" if score >= 0.5 else "benign"
        _write_json(
            package / "golden_cases.json",
            {
                "schema_version": "1",
                "cases": [
                    {
                        "id": "fixture-case",
                        "text": "safe scan target",
                        "matched_keyword": "scan",
                        "expected_label": expected_label,
                        "min_score": max(0.0, score - 0.001),
                        "max_score": min(1.0, score + 0.001),
                    }
                ],
            },
        )

        files: dict[str, dict[str, Any]] = {}
        for path in package.rglob("*"):
            if path.is_file() and path.name != "manifest.json":
                relative = path.relative_to(package).as_posix()
                files[relative] = {"sha256": _sha256(path), "size": path.stat().st_size}
        _write_json(
            package / "manifest.json",
            {
                "schema_version": "1",
                "model_version": version,
                "preprocessing_version": preprocessing_version,
                "created_at": "2026-07-20T12:00:00Z",
                "runtime": {"format": "onnx", "opset": 17},
                "files": files,
                "inputs": {
                    "input_ids": {"dtype": "int64", "shape": [1, max_length]},
                    "attention_mask": {"dtype": "int64", "shape": [1, max_length]},
                },
                "output": {"name": "logits", "dtype": "float32", "shape": [1, 2]},
            },
        )
        return package

    return create


@pytest.fixture
def settings_factory(tmp_path: Path) -> Callable[..., Settings]:
    def create(**overrides: Any) -> Settings:
        values: dict[str, Any] = {
            "host": "127.0.0.1",
            "port": 8080,
            "model_root": tmp_path / "models",
            "state_dir": tmp_path / "state",
            "startup_model_version": "",
            "api_token": "",
            "admin_token": "admin-secret",
            "admin_url": "http://127.0.0.1:8080",
            "max_concurrency": 2,
            "inference_timeout_ms": 250,
            "max_request_bytes": 65_536,
            "log_level": "CRITICAL",
            "admin_loopback_only": False,
        }
        values.update(overrides)
        settings = Settings(**values)
        settings.model_root.mkdir(parents=True, exist_ok=True)
        settings.state_dir.mkdir(parents=True, exist_ok=True)
        return settings

    return create
