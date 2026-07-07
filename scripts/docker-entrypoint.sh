#!/bin/sh
set -eu

if [ "${APP_DB:-sqlite}" = "postgres" ] && [ -n "${DATABASE_URL:-}" ]; then
  echo "Running database migrations..."
  for attempt in $(seq 1 30); do
    if athlete-log-migrate; then
      break
    fi
    if [ "$attempt" -eq 30 ]; then
      echo "Database migrations failed after $attempt attempts." >&2
      exit 1
    fi
    sleep 1
  done
fi

exec athlete-log \
  -addr "${APP_ADDR:-0.0.0.0:8080}" \
  -token-file "${TOKEN_FILE:-/data/bearer.token}" \
  -static-dir "${STATIC_DIR:-/app/frontend/dist}"
