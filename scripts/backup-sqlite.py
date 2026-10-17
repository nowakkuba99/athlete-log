#!/usr/bin/env python3
"""Create and verify a complete, point-in-time Athlete Log SQLite backup."""

import argparse
from contextlib import closing
import fcntl
import json
import os
from pathlib import Path
import shutil
import sqlite3
import tarfile
import tempfile
from datetime import datetime, timezone
from typing import Optional, Tuple


DATABASE = "athlete-log.db"
KEEP = 7


def check_database(path: Path) -> Tuple[int, Optional[str]]:
    with closing(sqlite3.connect(path)) as db:
        if db.execute("PRAGMA quick_check").fetchone()[0] != "ok":
            raise RuntimeError("SQLite integrity check failed")
        if db.execute("PRAGMA foreign_key_check").fetchone() is not None:
            raise RuntimeError("SQLite foreign key check failed")
        count, latest = db.execute(
            "SELECT count(*), max(start_at) FROM activities WHERE delete_indicator = 0"
        ).fetchone()
        if count == 0:
            raise RuntimeError("Refusing to back up an empty activities database")
        return count, latest


def check_blobs(database: Path, root: Path) -> None:
    with closing(sqlite3.connect(database)) as db:
        for table, column in (
            ("source_activity_refs", "raw_blob_key"),
            ("artifacts", "storage_key"),
        ):
            for (key,) in db.execute(f"SELECT {column} FROM {table} WHERE {column} != ''"):
                candidate = root / key
                if not candidate.is_file() or not candidate.resolve().is_relative_to(root.resolve()):
                    raise RuntimeError(f"Missing activity file: {key}")


def create_backup(source: Path, destination: Path) -> Path:
    if not source.is_dir() or not (source / DATABASE).is_file():
        raise RuntimeError(f"SQLite data directory not found: {source}")
    if destination == source or source in destination.parents:
        raise RuntimeError("Backup destination must be outside the app data directory")
    destination.mkdir(mode=0o700, parents=True, exist_ok=True)
    os.chmod(destination, 0o700)
    with (destination / ".backup.lock").open("a+b") as lock:
        fcntl.flock(lock, fcntl.LOCK_EX | fcntl.LOCK_NB)
        with tempfile.TemporaryDirectory(prefix=".athlete-log-stage-", dir=destination) as temporary:
            stage = Path(temporary) / "app"
            stage.mkdir()
            # WAL databases may need to create their -wal/-shm sidecars on first open.
            with closing(sqlite3.connect(f"file:{source / DATABASE}?mode=rw", uri=True)) as live:
                with closing(sqlite3.connect(stage / DATABASE)) as snapshot:
                    live.backup(snapshot)
            count, latest = check_database(stage / DATABASE)
            for item in source.iterdir():
                if item.name in {DATABASE, DATABASE + "-wal", DATABASE + "-shm", DATABASE + "-journal"}:
                    continue
                if item.is_symlink():
                    raise RuntimeError(f"Unexpected symlink in app data: {item}")
                if item.is_dir():
                    shutil.copytree(item, stage / item.name, symlinks=True)
                elif item.is_file():
                    shutil.copy2(item, stage / item.name)
            check_blobs(stage / DATABASE, stage)
            (Path(temporary) / "manifest.json").write_text(
                json.dumps({"activities": count, "latest_activity": latest}, indent=2) + "\n"
            )
            timestamp = datetime.now(timezone.utc).strftime("%Y%m%dT%H%M%S%fZ")
            target = destination / f"athlete-log-{timestamp}.tar.gz"
            partial = destination / (target.name + ".partial")
            try:
                descriptor = os.open(partial, os.O_WRONLY | os.O_CREAT | os.O_EXCL, 0o600)
                with os.fdopen(descriptor, "wb") as output:
                    with tarfile.open(fileobj=output, mode="w:gz", compresslevel=1) as archive:
                        archive.add(stage, arcname="app")
                        archive.add(Path(temporary) / "manifest.json", arcname="manifest.json")
                with tarfile.open(partial, "r:gz") as archive:
                    for member in archive:
                        if member.isfile():
                            stream = archive.extractfile(member)
                            while stream.read(1024 * 1024):
                                pass
                partial.replace(target)
            finally:
                partial.unlink(missing_ok=True)
        backups = sorted(destination.glob("athlete-log-*.tar.gz"), reverse=True)
        for old in backups[KEEP:]:
            old.unlink()
        print(f"Backup OK: {target} ({count} activities; newest {latest})")
        return target


def main() -> None:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("source", type=Path, help="Persistent app data directory")
    parser.add_argument("destination", type=Path, help="Backup archive directory")
    args = parser.parse_args()
    create_backup(args.source.expanduser().resolve(), args.destination.expanduser().resolve())


if __name__ == "__main__":
    main()
