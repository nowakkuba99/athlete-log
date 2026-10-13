-- Add bounding-box columns to activities for efficient route-repeat detection.
ALTER TABLE activities
  ADD COLUMN IF NOT EXISTS bbox_min_lat double precision,
  ADD COLUMN IF NOT EXISTS bbox_max_lat double precision,
  ADD COLUMN IF NOT EXISTS bbox_min_lng double precision,
  ADD COLUMN IF NOT EXISTS bbox_max_lng double precision;

-- Backfill from existing samples.
UPDATE activities a
SET
  bbox_min_lat = sub.min_lat,
  bbox_max_lat = sub.max_lat,
  bbox_min_lng = sub.min_lng,
  bbox_max_lng = sub.max_lng
FROM (
  SELECT activity_id,
         MIN(latitude)  AS min_lat,
         MAX(latitude)  AS max_lat,
         MIN(longitude) AS min_lng,
         MAX(longitude) AS max_lng
  FROM activity_samples
  WHERE latitude IS NOT NULL AND longitude IS NOT NULL
  GROUP BY activity_id
) sub
WHERE a.id = sub.activity_id;

-- Index for center-point proximity queries.
CREATE INDEX IF NOT EXISTS activities_bbox_center_idx
  ON activities (
    ((bbox_min_lat + bbox_max_lat) / 2),
    ((bbox_min_lng + bbox_max_lng) / 2)
  )
  WHERE bbox_min_lat IS NOT NULL;
