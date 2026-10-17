# Persistent macOS installation

Conductor workspaces are separate working directories. With the default `./data` mount, each workspace gets its own SQLite database and settings. For regular use, keep the app data in one directory outside Conductor, and run a single app container against it.

## Data location and app startup

Use `~/AthleteLog/data/app` for the SQLite database and activity files. The database is `athlete-log.db`; activity source files are under `blobs/`. The production Compose overlay refuses to create a missing host directory and the app refuses to start if this database has no activities, so a wrong path cannot silently start an empty installation.

After placing a **verified** database and its `blobs/` directory there, start the app from a stable checkout of the repository:

```sh
cp compose.production.yml "$HOME/AthleteLog/compose.production.yml"
printf 'ATHLETE_LOG_APP_DATA=%s/AthleteLog/data/app\n' "$HOME" > "$HOME/AthleteLog/compose.env"
chmod 600 "$HOME/AthleteLog/compose.production.yml" "$HOME/AthleteLog/compose.env"
docker compose --env-file "$HOME/AthleteLog/compose.env" \
  -f docker-compose.yml -f "$HOME/AthleteLog/compose.production.yml" \
  up -d --build app
```

The overlay uses the fixed Compose project `athlete-log` and `restart: unless-stopped`. Docker Desktop must be running; the app is available at `http://127.0.0.1:8080`. Stop any other Athlete Log container first so it does not occupy port 8080 or write to the same database. To manage the installation later, use the same two Compose files and `compose.env` from a repository checkout. The operational configuration and data live outside Conductor workspaces. Never use `docker compose down -v` for a database you intend to keep.

Before replacing the current database with one from another workspace, stop the app, preserve the current `~/AthleteLog/data/app`, and compare the activity count, latest activity date, and connected sources. Do not combine SQLite database files by copying one over another while the app is running.

## Automated local backups

On macOS, install a daily `launchd` job:

```sh
/usr/bin/python3 scripts/install-backup-macos.py --root "$HOME/AthleteLog"
```

It runs immediately and then at 03:00 each day, including after a sleeping Mac wakes. The backup script uses SQLite's online backup API, includes activity files and settings, verifies database integrity and referenced files, reads the archive back, and keeps the seven newest archives. It refuses an empty activities database, which protects against silently replacing a useful backup set with snapshots from a wrong workspace. The app container can be stopped when the job runs.

Check a successful run:

```sh
ls -lh "$HOME/AthleteLog/backups"/athlete-log-*.tar.gz
tail -20 "$HOME/AthleteLog/logs/backup.log"
cat "$HOME/AthleteLog/logs/backup-error.log"
```

These archives sit on the same Mac as the live data. Configure an **encrypted Time Machine backup to an external disk or NAS** to survive disk failure or theft. No Time Machine destination is configured by these scripts. Protect the archives: the database can contain Strava credentials, Nike tokens, a Protomaps key, and location history. Do not commit it to Git or place it in a public cloud folder unencrypted.

## Restore drill

Restore into a separate temporary directory first. This does not affect the running app:

```sh
mkdir -p "$HOME/AthleteLog/restore-test"
latest=$(ls -t "$HOME/AthleteLog/backups"/athlete-log-*.tar.gz | head -1)
tar -xzf "$latest" -C "$HOME/AthleteLog/restore-test"
sqlite3 "$HOME/AthleteLog/restore-test/app/athlete-log.db" 'PRAGMA quick_check; SELECT count(*), max(start_at) FROM activities WHERE delete_indicator = 0;'
```

Compare the count and latest date with `manifest.json` in the restored directory. For a real restore, stop the app, rename the current `data/app` as a safety copy, move the restored `app` directory into `data/app`, then start the app using the production Compose command above. Keep the safety copy until the UI shows the expected activities and connections.
