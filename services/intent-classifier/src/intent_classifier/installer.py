from __future__ import annotations

import errno
import os
import shutil
import uuid
from dataclasses import dataclass
from pathlib import Path
from typing import Any

from .errors import ServiceError
from .model_package import load_model_package_path

CHMOD_UNSUPPORTED_ERRNOS = frozenset(
    code
    for code in (
        errno.EPERM,
        errno.EACCES,
        getattr(errno, "ENOTSUP", None),
        getattr(errno, "EOPNOTSUPP", None),
        getattr(errno, "ENOSYS", None),
    )
    if code is not None
)


@dataclass(frozen=True, slots=True)
class InstallReport:
    model_version: str
    preprocessing_version: str
    installed_path: str

    def as_dict(self) -> dict[str, Any]:
        return {
            "status": "installed",
            "model_version": self.model_version,
            "preprocessing_version": self.preprocessing_version,
            "installed_path": self.installed_path,
        }


def install_model_package(source: Path, models_dir: Path) -> InstallReport:
    source_model = load_model_package_path(source)
    source_path = source_model.package_path
    version = source_model.model_version
    destination_root = _prepare_destination_root(models_dir)
    if source_path == destination_root or source_path in destination_root.parents:
        raise _install_error("invalid_install_destination", "destination is inside source")

    target = destination_root / version
    if _path_exists(target):
        raise _install_error("model_version_exists", "target version already exists")

    lock_path = destination_root / f".install-{version}.lock"
    lock_descriptor = _acquire_lock(lock_path)
    temporary = destination_root / f".install-{version}-{uuid.uuid4().hex}.tmp"
    try:
        if _path_exists(target):
            raise _install_error("model_version_exists", "target version already exists")
        shutil.copytree(source_path, temporary, symlinks=False, copy_function=shutil.copyfile)
        _normalize_permissions(temporary)
        copied_model = load_model_package_path(temporary)
        if copied_model.model_version != version:
            raise _install_error("install_validation_failed", "copied version changed")
        _fsync_tree(temporary)
        if _path_exists(target):
            raise _install_error("model_version_exists", "target version already exists")
        os.rename(temporary, target)
        _fsync_directory(destination_root)
    except ServiceError:
        _cleanup_temporary(temporary, destination_root)
        raise
    except (OSError, shutil.Error) as exc:
        _cleanup_temporary(temporary, destination_root)
        raise _install_error("install_failed", type(exc).__name__) from exc
    finally:
        os.close(lock_descriptor)
        lock_path.unlink(missing_ok=True)
    return InstallReport(
        model_version=version,
        preprocessing_version=copied_model.preprocessing_version,
        installed_path=str(target),
    )


def _prepare_destination_root(models_dir: Path) -> Path:
    destination = models_dir.expanduser()
    try:
        if destination.is_symlink():
            raise OSError("destination is a symlink")
        destination.mkdir(parents=True, exist_ok=True, mode=0o755)
        if not destination.is_dir():
            raise OSError("destination is not a directory")
        _chmod_if_supported(destination, 0o755)
        return destination.resolve()
    except OSError as exc:
        raise _install_error("install_destination_unavailable", type(exc).__name__) from exc


def _acquire_lock(lock_path: Path) -> int:
    try:
        return os.open(lock_path, os.O_CREAT | os.O_EXCL | os.O_WRONLY, 0o600)
    except FileExistsError as exc:
        raise _install_error("install_in_progress", "version install is already running") from exc
    except OSError as exc:
        raise _install_error("install_failed", type(exc).__name__) from exc


def _normalize_permissions(root: Path) -> None:
    _chmod_if_supported(root, 0o755)
    for current_root, directories, filenames in os.walk(root, followlinks=False):
        current = Path(current_root)
        for directory_name in directories:
            directory = current / directory_name
            if directory.is_symlink():
                raise _install_error("install_validation_failed", "copied symlink detected")
            _chmod_if_supported(directory, 0o755)
        for filename in filenames:
            path = current / filename
            if path.is_symlink() or not path.is_file():
                raise _install_error("install_validation_failed", "copied file is unsafe")
            _chmod_if_supported(path, 0o644)


def _chmod_if_supported(path: Path, mode: int) -> None:
    try:
        path.chmod(mode)
    except NotImplementedError:
        return
    except OSError as exc:
        if exc.errno in CHMOD_UNSUPPORTED_ERRNOS:
            return
        raise


def _fsync_tree(root: Path) -> None:
    directories: list[Path] = []
    for current_root, _, filenames in os.walk(root, followlinks=False):
        current = Path(current_root)
        directories.append(current)
        for filename in filenames:
            path = current / filename
            with path.open("r+b") as handle:
                os.fsync(handle.fileno())
    if os.name != "nt":
        for directory in reversed(directories):
            _fsync_directory(directory)


def _fsync_directory(directory: Path) -> None:
    if os.name == "nt":
        return
    descriptor = os.open(directory, os.O_RDONLY)
    try:
        os.fsync(descriptor)
    finally:
        os.close(descriptor)


def _cleanup_temporary(temporary: Path, destination_root: Path) -> None:
    try:
        resolved_parent = temporary.parent.resolve()
    except OSError:
        return
    if resolved_parent != destination_root or not temporary.name.startswith(".install-"):
        return
    if temporary.is_symlink():
        temporary.unlink(missing_ok=True)
    elif temporary.exists():
        shutil.rmtree(temporary)


def _path_exists(path: Path) -> bool:
    return path.exists() or path.is_symlink()


def _install_error(code: str, detail: str) -> ServiceError:
    return ServiceError(
        422,
        code,
        "Model package installation failed",
        detail=detail,
    )
