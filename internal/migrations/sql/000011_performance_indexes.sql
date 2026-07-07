-- Performance indexes identified by audit.

-- 1. Partial active-rows index replaces the full start_at index for all queries
--    that always filter delete_indicator = false (list, stats, timeseries, streaks).
CREATE INDEX IF NOT EXISTS activities_athlete_start_at_active
    ON activities (athlete_id, start_at DESC)
    WHERE delete_indicator = false;

-- 2. Composite index for sport-type + start_at lookups (FindActivityNearTime,
--    GetSimilarActivities distance filter, GetStreaks).
CREATE INDEX IF NOT EXISTS activities_athlete_sport_start
    ON activities (athlete_id, sport_type, start_at DESC)
    WHERE delete_indicator = false;

-- 3. Expression index on jobs.params->>'source_connection_id' used by
--    GetLastJobForSourceConnection / GetLastJobsForSourceConnections.
CREATE INDEX IF NOT EXISTS jobs_params_source_conn_id
    ON jobs ((params->>'source_connection_id'), athlete_id, created_at DESC);

-- 4. Covering index for activity_samples sliding-window (best efforts, heatmap).
--    Partial: only rows with both distance and elapsed_ms populated.
CREATE INDEX IF NOT EXISTS activity_samples_activity_distance
    ON activity_samples (activity_id, distance_meters)
    WHERE distance_meters IS NOT NULL AND elapsed_ms IS NOT NULL;

-- 5. Reverse lookup on activity_gear for ListGear total-distance joins.
CREATE INDEX IF NOT EXISTS activity_gear_gear_id
    ON activity_gear (gear_id);

-- 6. Covering index for GetBests (best week / best month) avoiding heap access.
CREATE INDEX IF NOT EXISTS activities_athlete_start_distance
    ON activities (athlete_id, start_at, distance_meters)
    WHERE delete_indicator = false AND distance_meters IS NOT NULL;
