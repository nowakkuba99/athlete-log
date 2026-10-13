CREATE TABLE best_efforts (
    id           uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    athlete_id   uuid NOT NULL REFERENCES athletes(id) ON DELETE CASCADE,
    activity_id  uuid NOT NULL REFERENCES activities(id) ON DELETE CASCADE,
    sport_type   text NOT NULL,
    distance_meters integer NOT NULL,
    elapsed_ms   integer NOT NULL,
    achieved_at  timestamptz NOT NULL,
    created_at   timestamptz NOT NULL DEFAULT now(),
    UNIQUE (athlete_id, sport_type, distance_meters, elapsed_ms, activity_id)
);

CREATE INDEX best_efforts_athlete_sport ON best_efforts (athlete_id, sport_type, distance_meters);
