#!/usr/bin/env python3
"""Install a daily launchd backup for a persistent Athlete Log data directory."""

import argparse
import os
from pathlib import Path
import plistlib
import shutil
import subprocess


LABEL = "app.athlete-log.sqlite-backup"


def main() -> None:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument(
        "--root", type=Path, default=Path.home() / "AthleteLog",
        help="Persistent directory containing data/app and backups",
    )
    args = parser.parse_args()
    root = args.root.expanduser().resolve()
    source = root / "data" / "app"
    if not (source / "athlete-log.db").is_file():
        parser.error(f"Database missing at {source / 'athlete-log.db'}")
    for directory in (root / "bin", root / "backups", root / "logs"):
        directory.mkdir(parents=True, exist_ok=True, mode=0o700)
        os.chmod(directory, 0o700)
    backup_script = root / "bin" / "backup-sqlite.py"
    shutil.copy2(Path(__file__).with_name("backup-sqlite.py"), backup_script)
    os.chmod(backup_script, 0o700)
    plist = {
        "Label": LABEL,
        "ProgramArguments": [
            "/usr/bin/python3", str(backup_script), str(source), str(root / "backups")
        ],
        "RunAtLoad": True,
        "StartCalendarInterval": {"Hour": 3, "Minute": 0},
        "StandardOutPath": str(root / "logs" / "backup.log"),
        "StandardErrorPath": str(root / "logs" / "backup-error.log"),
    }
    agent = Path.home() / "Library" / "LaunchAgents" / f"{LABEL}.plist"
    agent.parent.mkdir(parents=True, exist_ok=True)
    agent.write_bytes(plistlib.dumps(plist))
    domain = f"gui/{os.getuid()}"
    subprocess.run(["launchctl", "bootout", domain, str(agent)], capture_output=True)
    subprocess.run(["launchctl", "bootstrap", domain, str(agent)], check=True)
    print(f"Installed {agent}; first backup starts now, then daily at 03:00.")


if __name__ == "__main__":
    main()
