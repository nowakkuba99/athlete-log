CREATE TABLE dashboards (
    id         uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    athlete_id uuid NOT NULL REFERENCES athletes(id) ON DELETE CASCADE,
    name       text NOT NULL DEFAULT 'Untitled',
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX dashboards_athlete ON dashboards (athlete_id, created_at);

CREATE TABLE dashboard_widgets (
    id           uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    dashboard_id uuid NOT NULL REFERENCES dashboards(id) ON DELETE CASCADE,
    type         text NOT NULL,
    config       jsonb NOT NULL DEFAULT '{}',
    position     jsonb NOT NULL DEFAULT '{"x":0,"y":0,"w":4,"h":3}',
    created_at   timestamptz NOT NULL DEFAULT now(),
    updated_at   timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX dashboard_widgets_dashboard ON dashboard_widgets (dashboard_id);
