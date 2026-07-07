CREATE TABLE IF NOT EXISTS synced_files (
  id                  UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
  source_connection_id UUID        NOT NULL REFERENCES source_connections(id) ON DELETE CASCADE,
  file_path           TEXT        NOT NULL,
  sha256              TEXT        NOT NULL,
  status              TEXT        NOT NULL DEFAULT 'imported',
  error_message       TEXT,
  synced_at           TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  UNIQUE (source_connection_id, sha256)
);

CREATE INDEX IF NOT EXISTS idx_synced_files_conn_time
  ON synced_files (source_connection_id, synced_at DESC);
