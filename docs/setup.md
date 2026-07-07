# Setup

## Prerequisites

- [Docker Desktop](https://docs.docker.com/get-docker/) (Mac, Windows, or Linux)
- Git

That's all for the default Docker Compose setup. Local development additionally requires Go 1.23+ and Node.js 20+.

## Installation

```sh
git clone https://github.com/nowakkuba99/athlete-log
cd athlete-log
docker compose up
```

This single command:

1. Creates a local SQLite database under `${DATA_DIR:-./data}/app/`
2. Builds the Go API and React frontend
3. Serves everything at **http://127.0.0.1:8080**

## Configuration

The only values needed before first launch are in `docker-compose.yml`:

| Variable | Default | Description |
|---|---|---|
| `APP_DB` | `sqlite` | Storage backend. Use `sqlite` for the local default or `postgres` to bring your own database |
| `POSTGRES_PASSWORD` | `athlete` | Postgres password — **change this before any network exposure** |
| `DATABASE_URL` | Compose-managed Postgres URL | Existing Postgres connection string. Set this when bringing your own Postgres database |
| `SQLITE_PATH` | `/data/athlete-log.db` in Docker | SQLite database path |
| `BASE_URL` | _(unset)_ | Public origin, e.g. `https://vault.example.com` — required for Strava OAuth on non-localhost |
| `APP_PORT` | `8080` | Host port the app listens on |
| `DATA_DIR` | `./data` | Host path for SQLite data, optional Postgres volume, and activity blobs |
| `APP_PUBLIC_MODE` | _(unset)_ | Set to `true` to allow anyone to view data while restricting writes to authenticated owner only |
| `APP_AUTH_RESET` | _(unset)_ | Set to `true` on a single startup to clear a forgotten password and disable auth — remove immediately after logging back in |

All other settings (Strava credentials, Protomaps API key, HR max, units, password protection) are configured through the **Settings page** in the UI and stored in the database — no restart required.

## Security

By default Athlete Log is designed for **localhost use** — no authentication required. If you expose it beyond your own machine, two steps are strongly recommended:

### Change the Postgres password

Only needed when using Postgres. Set `POSTGRES_PASSWORD` and update the matching value in `DATABASE_URL` to something strong. The default `athlete` password is suitable only for localhost.

### Enable password protection

1. Open **Settings → Password protection**
2. Toggle **Enable password protection** on
3. Enter and confirm a password → **Save**

Once enabled, every browser session must enter the password before accessing the app. The password is hashed with bcrypt and stored in the database — never in plaintext. Sessions are scoped to the browser tab (cleared on close).

To disable again: open Settings, toggle off, Save — no password prompt to re-disable since you're already authenticated.

**Forgotten password?** Restart the container with `APP_AUTH_RESET=true` — this clears the password and disables auth so you can log back in and set a new one. See the [Security page](security.md#forgotten-password) for the full steps.

## Development mode

For faster iteration, run the API and frontend separately (no Docker for the app itself). This uses SQLite by default:

```sh
make run-dev     # start Go API + Vite dev server with hot reload
```

- Frontend: **http://127.0.0.1:5173** (Vite, hot reload)
- API: **http://127.0.0.1:8080** (Vite proxies `/api` requests here)

## Useful Make targets

| Target | What it does |
|---|---|
| `make run` | Full stack via Docker Compose |
| `make run-dev` | Local API + Vite dev server using SQLite by default |
| `make db-up` / `make db-down` | Start / stop optional local Postgres |
| `make db-reset` | Wipe and recreate Postgres data |
| `make migrate` | Apply pending SQL migrations |
| `make test` | Go tests + frontend production build |
| `make deploy-frontend` | Rebuild frontend and copy into the running container |
| `make backup` | Dump the Postgres database to `backups/` |
| `make restore FILE=…` | Restore from a `.sql.gz` backup file |
| `make docs-serve` | Serve this documentation at http://127.0.0.1:8090 |

## Postgres

SQLite is the default for localhost and small self-hosted installs. To use Postgres instead, point the app at an existing database:

```sh
APP_DB=postgres DATABASE_URL=postgres://user:password@host:5432/athlete_log?sslmode=require docker compose up
```

Or start the optional Compose Postgres service:

```sh
APP_DB=postgres docker compose --profile postgres up
```

## Backups

With SQLite, back up `${DATA_DIR:-./data}/app/athlete-log.db` while the app is stopped, or copy the full `${DATA_DIR:-./data}/app/` directory.

When using the optional Compose Postgres service, data lives on the host at `${DATA_DIR:-./data}/postgres/`. The simplest way to back it up is:

```sh
make backup
```

This runs `pg_dump` inside the running Postgres container and writes a gzip-compressed SQL dump to `backups/athlete_log_YYYYMMDD_HHMMSS.sql.gz`. To restore:

```sh
make restore FILE=backups/athlete_log_20260630_120000.sql.gz
```

### Automatic daily backups

The `docker-compose.yml` includes an optional `backup` sidecar. Enable it once and it runs nightly, keeping the last 7 days:

```sh
DATA_DIR=./data docker compose --profile backup up -d backup
```

Backups are written to `${DATA_DIR:-./data}/backups/` and files older than 7 days are deleted automatically.

---

## Updating

```sh
git pull
make run   # rebuilds image and applies any new migrations automatically
```
