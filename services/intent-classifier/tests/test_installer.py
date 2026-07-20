from __future__ import annotations

import errno
import json
import os
import stat
from concurrent.futures import ThreadPoolExecutor
from pathlib import Path

import pytest
from conftest import PackageFactory

import intent_classifier.installer as installer
from intent_classifier.cli import EXIT_SUCCESS, EXIT_VALIDATION, main
from intent_classifier.errors import ModelPackageError, ServiceError
from intent_classifier.installer import install_model_package
from intent_classifier.model_package import load_model_package


def test_install_cli_validates_and_atomically_installs_package(
    package_factory: PackageFactory, tmp_path: Path, capsys: pytest.CaptureFixture[str]
) -> None:
    source = package_factory(tmp_path / "imports", "fixture-v1")
    models_dir = tmp_path / "models"

    result = main(["install", str(source), "--models-dir", str(models_dir)])
    captured = capsys.readouterr()

    assert result == EXIT_SUCCESS
    payload = json.loads(captured.out)
    assert payload["status"] == "installed"
    assert payload["model_version"] == "fixture-v1"
    assert Path(payload["installed_path"]) == models_dir / "fixture-v1"
    assert load_model_package(models_dir, "fixture-v1").model_version == "fixture-v1"
    assert not list(models_dir.glob(".install-*"))


def test_install_cli_refuses_to_overwrite_existing_version(
    package_factory: PackageFactory, tmp_path: Path, capsys: pytest.CaptureFixture[str]
) -> None:
    source = package_factory(tmp_path / "imports", "fixture-v1")
    models_dir = tmp_path / "models"
    assert main(["install", str(source), "--models-dir", str(models_dir)]) == EXIT_SUCCESS
    capsys.readouterr()
    manifest_before = (models_dir / "fixture-v1" / "manifest.json").read_bytes()

    result = main(["install", str(source), "--models-dir", str(models_dir)])
    captured = capsys.readouterr()

    assert result == EXIT_VALIDATION
    assert json.loads(captured.err)["code"] == "model_version_exists"
    assert (models_dir / "fixture-v1" / "manifest.json").read_bytes() == manifest_before
    assert not list(models_dir.glob(".install-*"))


def test_concurrent_installs_only_publish_one_version(
    package_factory: PackageFactory, tmp_path: Path
) -> None:
    source = package_factory(tmp_path / "imports", "fixture-v1")
    models_dir = tmp_path / "models"

    def install() -> str:
        try:
            install_model_package(source, models_dir)
        except ServiceError as exc:
            return exc.code
        return "installed"

    with ThreadPoolExecutor(max_workers=2) as executor:
        results = list(executor.map(lambda _: install(), range(2)))

    assert results.count("installed") == 1
    assert set(results) <= {"installed", "install_in_progress", "model_version_exists"}
    assert load_model_package(models_dir, "fixture-v1").model_version == "fixture-v1"
    assert not list(models_dir.glob(".install-*"))


def test_failed_second_validation_cleans_temporary_directory(
    package_factory: PackageFactory,
    tmp_path: Path,
    monkeypatch: pytest.MonkeyPatch,
) -> None:
    source = package_factory(tmp_path / "imports", "fixture-v1")
    models_dir = tmp_path / "models"
    real_loader = installer.load_model_package_path
    calls = 0

    def fail_copied_package(path: Path) -> object:
        nonlocal calls
        calls += 1
        if calls == 2:
            raise ModelPackageError("Copied package failed validation")
        return real_loader(path)

    monkeypatch.setattr(installer, "load_model_package_path", fail_copied_package)

    with pytest.raises(ModelPackageError, match="Copied package failed validation"):
        install_model_package(source, models_dir)

    assert not (models_dir / "fixture-v1").exists()
    assert not list(models_dir.glob(".install-*"))


def test_install_cli_rejects_non_directory_source(
    tmp_path: Path, capsys: pytest.CaptureFixture[str]
) -> None:
    archive = tmp_path / "intent-model.zip"
    archive.write_bytes(b"not-an-exported-directory")

    result = main(["install", str(archive), "--models-dir", str(tmp_path / "models")])
    captured = capsys.readouterr()

    assert result == EXIT_VALIDATION
    assert json.loads(captured.err)["code"] == "invalid_model_package"


@pytest.mark.parametrize("unsafe_entry", ["python", "undeclared"])
def test_install_still_rejects_code_and_undeclared_files(
    package_factory: PackageFactory, tmp_path: Path, unsafe_entry: str
) -> None:
    source = package_factory(
        tmp_path / "imports",
        "fixture-v1",
        tokenizer_directory=unsafe_entry == "undeclared",
    )
    if unsafe_entry == "python":
        (source / "payload.py").write_text("raise RuntimeError()", encoding="utf-8")
    else:
        (source / "tokenizer" / "notes.txt").write_text("not declared", encoding="utf-8")
    models_dir = tmp_path / "models"

    with pytest.raises(ModelPackageError):
        install_model_package(source, models_dir)

    assert not (models_dir / "fixture-v1").exists()


def test_install_succeeds_when_filesystem_does_not_support_chmod(
    package_factory: PackageFactory,
    tmp_path: Path,
    monkeypatch: pytest.MonkeyPatch,
) -> None:
    source = package_factory(tmp_path / "imports", "fixture-v1")
    models_dir = tmp_path / "models"

    def unsupported_chmod(_path: Path, _mode: int) -> None:
        raise PermissionError(errno.EPERM, "chmod is unsupported")

    monkeypatch.setattr(Path, "chmod", unsupported_chmod)

    report = install_model_package(source, models_dir)

    assert Path(report.installed_path) == models_dir / "fixture-v1"
    assert load_model_package(models_dir, "fixture-v1").model_version == "fixture-v1"


def test_install_does_not_hide_chmod_io_errors(
    package_factory: PackageFactory,
    tmp_path: Path,
    monkeypatch: pytest.MonkeyPatch,
) -> None:
    source = package_factory(tmp_path / "imports", "fixture-v1")
    models_dir = tmp_path / "models"

    def broken_chmod(_path: Path, _mode: int) -> None:
        raise OSError(errno.EIO, "filesystem I/O failure")

    monkeypatch.setattr(Path, "chmod", broken_chmod)

    with pytest.raises(ServiceError) as error:
        install_model_package(source, models_dir)

    assert error.value.code == "install_destination_unavailable"
    assert not (models_dir / "fixture-v1").exists()


@pytest.mark.skipif(os.name == "nt", reason="POSIX mode bits are not portable")
def test_install_accepts_0777_source_and_normalizes_permissions(
    package_factory: PackageFactory, tmp_path: Path
) -> None:
    source = package_factory(tmp_path / "imports", "fixture-v1", tokenizer_directory=True)
    source.chmod(0o777)
    for path in source.rglob("*"):
        path.chmod(0o777)
    models_dir = tmp_path / "models"

    report = install_model_package(source, models_dir)
    target = Path(report.installed_path)

    for path in [target, *(entry for entry in target.rglob("*") if entry.is_dir())]:
        assert stat.S_IMODE(path.stat().st_mode) == 0o755
    for path in (entry for entry in target.rglob("*") if entry.is_file()):
        assert stat.S_IMODE(path.stat().st_mode) == 0o644
