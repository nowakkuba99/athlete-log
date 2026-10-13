CREATE TABLE gear (
    id                     uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    athlete_id             uuid NOT NULL REFERENCES athletes(id) ON DELETE CASCADE,
    name                   text NOT NULL,
    type                   text NOT NULL DEFAULT 'other',  -- shoe | bike | wetsuit | other
    brand_model            text NOT NULL DEFAULT '',
    purchase_date          date,
    distance_alert_meters  integer,
    retired_at             timestamptz,
    created_at             timestamptz NOT NULL DEFAULT now(),
    updated_at             timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX gear_athlete ON gear (athlete_id, retired_at);

CREATE TABLE activity_gear (
    activity_id uuid NOT NULL REFERENCES activities(id) ON DELETE CASCADE,
    gear_id     uuid NOT NULL REFERENCES gear(id) ON DELETE CASCADE,
    PRIMARY KEY (activity_id, gear_id)
);
