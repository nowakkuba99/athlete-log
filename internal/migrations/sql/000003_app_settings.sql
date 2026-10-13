CREATE TABLE app_settings (
	key        text        PRIMARY KEY,
	value      text        NOT NULL DEFAULT '',
	updated_at timestamptz NOT NULL DEFAULT now()
);
