CREATE TABLE activity_weather (
    activity_id      uuid PRIMARY KEY REFERENCES activities(id) ON DELETE CASCADE,
    fetched_at       timestamptz NOT NULL DEFAULT now(),
    temperature_c    numeric(5,1),
    weather_code     integer,
    wind_speed_ms    numeric(6,2),
    precipitation_mm numeric(6,2)
);
