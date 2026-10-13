# Contributing to Athlete Log

Thanks for taking the time to contribute. Athlete Log is a self-hosted fitness analytics app built with Go, PostgreSQL, and React.

By contributing to this repository, you agree that your contributions will be licensed under the repository license (`AGPL-3.0-or-later`).

## Before you start

- **Open an issue first** for anything non-trivial. It's the fastest way to confirm a feature or fix is in scope before you write code.
- **Bug reports**: include steps to reproduce, what you expected, and what actually happened. Attach relevant logs from `docker compose logs app`.

## Development setup

```sh
git clone https://github.com/nowakkuba99/athlete-log
cd athlete-log
make db-up      # start Postgres
make migrate    # apply migrations
make run-dev    # Go API + Vite dev server with hot reload
```

Frontend: **http://127.0.0.1:5173** · API: **http://127.0.0.1:8080**

Requires Go 1.23+ and Node.js 20+.

## Running tests

```sh
make test
```

This runs the Go test suite (with `-race`) and verifies the frontend builds cleanly. Run this before opening a PR.

Integration tests require a running Postgres (started by `make db-up`) and the `DATABASE_URL` env var pointing at it:

```sh
export DATABASE_URL="postgres://athlete:athlete@127.0.0.1:5432/athlete_log?sslmode=disable"
go test ./...
```

## Database migrations

Add new SQL files under `internal/migrations/sql/` using the next sequential number (`000013_...sql`). Use `IF NOT EXISTS` / `IF EXISTS` in every statement so migrations are safe to re-run. The runner wraps each file in a transaction; a single failing statement rolls back the whole migration.

## Code style

- Go: standard `gofmt` formatting. Run `go vet ./...` before committing.
- Frontend: no formatter is enforced — match the surrounding code style.
- No comments that describe *what* code does — only *why* when non-obvious.

## Pull requests

- Keep PRs focused: one feature or fix per PR.
- Reference the related issue (`Closes #N`) in the PR description.
- CI must pass (Go tests, vet, frontend build, Docker smoke test).
- For larger changes, include a brief description of what was tested manually.
