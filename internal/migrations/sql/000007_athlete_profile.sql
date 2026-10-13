ALTER TABLE athletes
  ADD COLUMN IF NOT EXISTS started_at  date,
  ADD COLUMN IF NOT EXISTS avatar_path text;
