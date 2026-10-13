CREATE TABLE athletes (
	id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
	display_name text,
	email text,
	created_at timestamptz NOT NULL DEFAULT now(),
	updated_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE source_connections (
	id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
	athlete_id uuid NOT NULL REFERENCES athletes(id) ON DELETE CASCADE,
	source text NOT NULL,
	external_athlete_id text,
	status text NOT NULL DEFAULT 'active',
	secret_key text,
	metadata jsonb NOT NULL DEFAULT '{}'::jsonb,
	created_at timestamptz NOT NULL DEFAULT now(),
	updated_at timestamptz NOT NULL DEFAULT now(),
	UNIQUE (athlete_id, source)
);

CREATE INDEX source_connections_athlete_id_idx ON source_connections (athlete_id);

CREATE TABLE source_activity_refs (
	id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
	athlete_id uuid NOT NULL REFERENCES athletes(id) ON DELETE CASCADE,
	source_connection_id uuid REFERENCES source_connections(id) ON DELETE SET NULL,
	source text NOT NULL,
	source_activity_id text NOT NULL,
	source_last_modified_at timestamptz,
	raw_blob_key text,
	raw_sha256 text,
	tombstone boolean NOT NULL DEFAULT false,
	created_at timestamptz NOT NULL DEFAULT now(),
	updated_at timestamptz NOT NULL DEFAULT now(),
	UNIQUE (athlete_id, source, source_activity_id)
);

CREATE INDEX source_activity_refs_athlete_id_idx ON source_activity_refs (athlete_id);
CREATE INDEX source_activity_refs_source_connection_id_idx ON source_activity_refs (source_connection_id);

CREATE TABLE activities (
	id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
	athlete_id uuid NOT NULL REFERENCES athletes(id) ON DELETE CASCADE,
	source_activity_ref_id uuid REFERENCES source_activity_refs(id) ON DELETE SET NULL,
	sport_type text NOT NULL,
	name text,
	app_id text,
	status text,
	start_at timestamptz NOT NULL,
	end_at timestamptz,
	timezone text,
	last_modified_at timestamptz,
	elapsed_duration_ms integer,
	moving_duration_ms integer,
	distance_meters numeric(12,3),
	elevation_gain_meters numeric(10,3),
	elevation_loss_meters numeric(10,3),
	calories integer,
	average_heart_rate numeric(6,2),
	max_heart_rate numeric(6,2),
	average_speed_meters_per_second numeric(8,3),
	average_pace_seconds_per_km numeric(8,3),
	indoor boolean NOT NULL DEFAULT false,
	delete_indicator boolean NOT NULL DEFAULT false,
	created_at timestamptz NOT NULL DEFAULT now(),
	updated_at timestamptz NOT NULL DEFAULT now(),
	UNIQUE (source_activity_ref_id)
);

CREATE INDEX activities_athlete_start_at_idx ON activities (athlete_id, start_at DESC);
CREATE INDEX activities_athlete_sport_type_idx ON activities (athlete_id, sport_type);
CREATE INDEX activities_source_activity_ref_id_idx ON activities (source_activity_ref_id);

CREATE TABLE activity_samples (
	id bigserial PRIMARY KEY,
	activity_id uuid NOT NULL REFERENCES activities(id) ON DELETE CASCADE,
	sample_at timestamptz NOT NULL,
	elapsed_ms integer,
	latitude double precision,
	longitude double precision,
	distance_meters numeric(12,3),
	elevation_meters numeric(10,3),
	heart_rate numeric(6,2),
	speed_meters_per_second numeric(8,3),
	cadence numeric(8,3),
	power_watts numeric(8,3),
	temperature_celsius numeric(6,2),
	raw jsonb NOT NULL DEFAULT '{}'::jsonb
);

CREATE INDEX activity_samples_activity_id_sample_at_idx ON activity_samples (activity_id, sample_at);

CREATE TABLE activity_laps (
	id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
	activity_id uuid NOT NULL REFERENCES activities(id) ON DELETE CASCADE,
	lap_index integer NOT NULL,
	start_at timestamptz NOT NULL,
	end_at timestamptz,
	duration_ms integer,
	distance_meters numeric(12,3),
	calories integer,
	average_heart_rate numeric(6,2),
	max_heart_rate numeric(6,2),
	raw jsonb NOT NULL DEFAULT '{}'::jsonb,
	UNIQUE (activity_id, lap_index)
);

CREATE INDEX activity_laps_activity_id_idx ON activity_laps (activity_id);

CREATE TABLE activity_tags (
	activity_id uuid NOT NULL REFERENCES activities(id) ON DELETE CASCADE,
	key text NOT NULL,
	value text,
	created_at timestamptz NOT NULL DEFAULT now(),
	PRIMARY KEY (activity_id, key)
);

CREATE TABLE jobs (
	id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
	athlete_id uuid REFERENCES athletes(id) ON DELETE SET NULL,
	type text NOT NULL,
	status text NOT NULL,
	phase text,
	message text,
	progress_current integer NOT NULL DEFAULT 0,
	progress_total integer,
	attempts integer NOT NULL DEFAULT 0,
	params jsonb NOT NULL DEFAULT '{}'::jsonb,
	result jsonb NOT NULL DEFAULT '{}'::jsonb,
	last_error text,
	created_at timestamptz NOT NULL DEFAULT now(),
	updated_at timestamptz NOT NULL DEFAULT now(),
	started_at timestamptz,
	completed_at timestamptz
);

CREATE INDEX jobs_athlete_id_idx ON jobs (athlete_id);
CREATE INDEX jobs_status_created_at_idx ON jobs (status, created_at DESC);

CREATE TABLE job_events (
	id bigserial PRIMARY KEY,
	job_id uuid NOT NULL REFERENCES jobs(id) ON DELETE CASCADE,
	event_at timestamptz NOT NULL DEFAULT now(),
	level text NOT NULL DEFAULT 'info',
	message text NOT NULL,
	data jsonb NOT NULL DEFAULT '{}'::jsonb
);

CREATE INDEX job_events_job_id_event_at_idx ON job_events (job_id, event_at);

CREATE TABLE artifacts (
	id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
	athlete_id uuid REFERENCES athletes(id) ON DELETE SET NULL,
	job_id uuid REFERENCES jobs(id) ON DELETE SET NULL,
	activity_id uuid REFERENCES activities(id) ON DELETE SET NULL,
	kind text NOT NULL,
	content_type text NOT NULL,
	filename text,
	storage_key text NOT NULL,
	byte_size bigint NOT NULL,
	checksum text,
	metadata jsonb NOT NULL DEFAULT '{}'::jsonb,
	created_at timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX artifacts_athlete_id_idx ON artifacts (athlete_id);
CREATE INDEX artifacts_job_id_idx ON artifacts (job_id);
CREATE INDEX artifacts_activity_id_idx ON artifacts (activity_id);

ALTER TABLE jobs
	ADD COLUMN artifact_id uuid REFERENCES artifacts(id) ON DELETE SET NULL;

CREATE INDEX jobs_artifact_id_idx ON jobs (artifact_id);
