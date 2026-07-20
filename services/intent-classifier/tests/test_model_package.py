from __future__ import annotations

import hashlib
import json
from pathlib import Path

import pytest
from conftest import PackageFactory

from intent_classifier.errors import ModelPackageError
from intent_classifier.model_package import load_model_package, normalize_text


def _manifest(package: Path) -> dict[str, object]:
    return json.loads((package / "manifest.json").read_text(encoding="utf-8"))


def _write_manifest(package: Path, manifest: dict[str, object]) -> None:
    (package / "manifest.json").write_text(
        json.dumps(manifest, ensure_ascii=True, separators=(",", ":")) + "\n",
        encoding="utf-8",
    )


def _refresh_hash(package: Path, relative: str) -> None:
    manifest = _manifest(package)
    files = manifest["files"]
    assert isinstance(files, dict)
    path = package / relative
    files[relative] = {
        "sha256": hashlib.sha256(path.read_bytes()).hexdigest(),
        "size": path.stat().st_size,
    }
    _write_manifest(package, manifest)


def test_loads_flat_tokenizer_and_returns_calibrated_actionable_probability(
    package_factory: PackageFactory, tmp_path: Path
) -> None:
    model_root = tmp_path / "models"
    package_factory(model_root, "fixture-v1", logits=(-2.0, 2.0))

    loaded = load_model_package(model_root, "fixture-v1")
    prediction = loaded.predict("scan\u0000target", "scan")

    assert loaded.report.file_count == 7
    assert prediction.label == "actionable_probe"
    assert 0.98 < prediction.score < 0.99


def test_loads_tokenizer_directory_and_hashes_every_leaf(
    package_factory: PackageFactory, tmp_path: Path
) -> None:
    model_root = tmp_path / "models"
    package_factory(model_root, "fixture-dir-v1", tokenizer_directory=True)

    loaded = load_model_package(model_root, "fixture-dir-v1")

    assert loaded.report.file_count == 8
    manifest_paths = set(loaded.manifest.files)
    assert "tokenizer/tokenizer.json" in manifest_paths
    assert "tokenizer/vocab.txt" in manifest_paths


def test_normalization_is_nfkc_control_replacement_and_whitespace_collapse() -> None:
    assert normalize_text("Ａ\u0000  B\n\tC") == "A B C"


def test_rejects_hash_mismatch_and_extra_root_file(
    package_factory: PackageFactory, tmp_path: Path
) -> None:
    model_root = tmp_path / "models"
    package = package_factory(model_root, "fixture-v1")
    (package / "labels.json").write_text("{}", encoding="utf-8")
    with pytest.raises(ModelPackageError, match=r"File (?:size|hash) mismatch"):
        load_model_package(model_root, "fixture-v1")

    package = package_factory(model_root, "fixture-v2")
    (package / "payload.py").write_text("raise RuntimeError()", encoding="utf-8")
    with pytest.raises(ModelPackageError, match="root"):
        load_model_package(model_root, "fixture-v2")


def test_rejects_unlisted_and_disallowed_tokenizer_files(
    package_factory: PackageFactory, tmp_path: Path
) -> None:
    model_root = tmp_path / "models"
    package = package_factory(model_root, "fixture-dir-v1", tokenizer_directory=True)
    (package / "tokenizer" / "notes.txt").write_text("not declared", encoding="utf-8")
    with pytest.raises(ModelPackageError, match="Manifest file list"):
        load_model_package(model_root, "fixture-dir-v1")

    package = package_factory(model_root, "fixture-dir-v2", tokenizer_directory=True)
    (package / "tokenizer" / "loader.py").write_text("pass", encoding="utf-8")
    with pytest.raises(ModelPackageError, match="disallowed file type"):
        load_model_package(model_root, "fixture-dir-v2")


def test_rejects_tokenizer_file_count_limit(
    package_factory: PackageFactory, tmp_path: Path
) -> None:
    model_root = tmp_path / "models"
    package = package_factory(model_root, "fixture-dir-v1", tokenizer_directory=True)
    for index in range(60):
        (package / "tokenizer" / f"data-{index}.txt").write_text("data", encoding="utf-8")

    with pytest.raises(ModelPackageError, match="too many files"):
        load_model_package(model_root, "fixture-dir-v1")


def test_rejects_unsafe_manifest_paths_and_duplicate_json_keys(
    package_factory: PackageFactory, tmp_path: Path
) -> None:
    model_root = tmp_path / "models"
    package = package_factory(model_root, "fixture-v1")
    manifest = _manifest(package)
    files = manifest["files"]
    assert isinstance(files, dict)
    files["../outside.json"] = next(iter(files.values()))
    _write_manifest(package, manifest)
    with pytest.raises(ModelPackageError, match="unsafe file path"):
        load_model_package(model_root, "fixture-v1")

    package = package_factory(model_root, "fixture-v2")
    manifest = _manifest(package)
    files = manifest["files"]
    assert isinstance(files, dict)
    files["/absolute/tokenizer.json"] = next(iter(files.values()))
    _write_manifest(package, manifest)
    with pytest.raises(ModelPackageError, match="unsafe file path"):
        load_model_package(model_root, "fixture-v2")

    package = package_factory(model_root, "fixture-v3")
    raw = (package / "manifest.json").read_text(encoding="utf-8")
    duplicate = raw.replace(
        '"schema_version":"1",',
        '"schema_version":"1","schema_version":"1",',
        1,
    )
    (package / "manifest.json").write_text(duplicate, encoding="utf-8")
    with pytest.raises(ModelPackageError, match="Invalid JSON"):
        load_model_package(model_root, "fixture-v3")


def test_rejects_model_version_path_traversal(tmp_path: Path) -> None:
    with pytest.raises(ModelPackageError, match="version is invalid"):
        load_model_package(tmp_path, "../outside")


def test_rejects_both_tokenizer_forms_and_symlinks(
    package_factory: PackageFactory, tmp_path: Path
) -> None:
    model_root = tmp_path / "models"
    package = package_factory(model_root, "fixture-v1")
    tokenizer_directory = package / "tokenizer"
    tokenizer_directory.mkdir()
    (tokenizer_directory / "tokenizer.json").write_text("{}", encoding="utf-8")
    with pytest.raises(ModelPackageError, match="but not both"):
        load_model_package(model_root, "fixture-v1")

    package = package_factory(model_root, "fixture-v2", tokenizer_directory=True)
    outside = tmp_path / "outside.txt"
    outside.write_text("outside", encoding="utf-8")
    link = package / "tokenizer" / "linked.txt"
    try:
        link.symlink_to(outside)
    except OSError:
        pytest.skip("symlink creation is unavailable")
    with pytest.raises(ModelPackageError, match="regular file"):
        load_model_package(model_root, "fixture-v2")


def test_golden_cases_and_preprocessing_versions_are_enforced(
    package_factory: PackageFactory, tmp_path: Path
) -> None:
    model_root = tmp_path / "models"
    package = package_factory(model_root, "fixture-v1")
    golden_path = package / "golden_cases.json"
    golden = json.loads(golden_path.read_text(encoding="utf-8"))
    golden["cases"][0]["expected_label"] = "actionable_probe"
    golden_path.write_text(json.dumps(golden), encoding="utf-8")
    _refresh_hash(package, "golden_cases.json")
    with pytest.raises(ModelPackageError, match="Golden case label mismatch"):
        load_model_package(model_root, "fixture-v1")

    package = package_factory(model_root, "fixture-v2")
    preprocessing_path = package / "preprocessing.json"
    preprocessing = json.loads(preprocessing_path.read_text(encoding="utf-8"))
    preprocessing["version"] = "different-v1"
    preprocessing_path.write_text(json.dumps(preprocessing), encoding="utf-8")
    _refresh_hash(package, "preprocessing.json")
    with pytest.raises(ModelPackageError, match="Preprocessing version"):
        load_model_package(model_root, "fixture-v2")


def test_identity_and_temperature_calibration_are_applied(
    package_factory: PackageFactory, tmp_path: Path
) -> None:
    model_root = tmp_path / "models"
    package_factory(
        model_root,
        "identity-v1",
        logits=(-2.0, 2.0),
        calibration_temperature=None,
    )
    package_factory(
        model_root,
        "temperature-v1",
        logits=(-2.0, 2.0),
        calibration_temperature=2.0,
    )

    identity = load_model_package(model_root, "identity-v1").predict("scan target", "scan")
    temperature = load_model_package(model_root, "temperature-v1").predict("scan target", "scan")

    assert 0.98 < identity.score < 0.99
    assert 0.88 < temperature.score < 0.89


def test_calibration_cannot_override_business_thresholds(
    package_factory: PackageFactory, tmp_path: Path
) -> None:
    model_root = tmp_path / "models"
    package = package_factory(model_root, "fixture-v1")
    calibration_path = package / "calibration.json"
    calibration = json.loads(calibration_path.read_text(encoding="utf-8"))
    calibration["decision_threshold"] = 0.99
    calibration_path.write_text(json.dumps(calibration), encoding="utf-8")
    _refresh_hash(package, "calibration.json")

    with pytest.raises(ModelPackageError, match="Invalid schema: calibration.json"):
        load_model_package(model_root, "fixture-v1")
