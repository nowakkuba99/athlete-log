package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"athlete-log/internal/storage"

	"github.com/google/uuid"
)

func (r *Repository) CreateDashboard(ctx context.Context, athleteID, name string) (storage.Dashboard, error) {
	now := time.Now().UTC()
	d := storage.Dashboard{
		ID:        uuid.NewString(),
		Name:      name,
		CreatedAt: now,
		UpdatedAt: now,
	}
	_, err := r.db.ExecContext(ctx, `
		INSERT INTO dashboards (id, athlete_id, name, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?)`,
		d.ID, athleteID, name, formatTime(now), formatTime(now),
	)
	if err != nil {
		return storage.Dashboard{}, err
	}
	return d, nil
}

func (r *Repository) ListDashboards(ctx context.Context, athleteID string) ([]storage.Dashboard, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT d.id, d.name, COUNT(w.id), d.created_at, d.updated_at
		FROM dashboards d
		LEFT JOIN dashboard_widgets w ON w.dashboard_id = d.id
		WHERE d.athlete_id = ?
		GROUP BY d.id, d.name, d.created_at, d.updated_at
		ORDER BY d.created_at ASC`,
		athleteID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []storage.Dashboard
	for rows.Next() {
		d, err := scanDashboard(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

func (r *Repository) GetDashboard(ctx context.Context, athleteID, id string) (storage.Dashboard, error) {
	row := r.db.QueryRowContext(ctx, `
		SELECT d.id, d.name, COUNT(w.id), d.created_at, d.updated_at
		FROM dashboards d
		LEFT JOIN dashboard_widgets w ON w.dashboard_id = d.id
		WHERE d.athlete_id = ? AND d.id = ?
		GROUP BY d.id, d.name, d.created_at, d.updated_at`,
		athleteID, id,
	)
	d, err := scanDashboard(row)
	if errors.Is(err, sql.ErrNoRows) {
		return storage.Dashboard{}, fmt.Errorf("dashboard not found")
	}
	return d, err
}

func (r *Repository) UpdateDashboard(ctx context.Context, athleteID, id, name string) (storage.Dashboard, error) {
	res, err := r.db.ExecContext(ctx, `
		UPDATE dashboards
		SET name = ?, updated_at = ?
		WHERE athlete_id = ? AND id = ?`,
		name, formatTime(time.Now().UTC()), athleteID, id,
	)
	if err := requireAffected(res, err, "dashboard not found"); err != nil {
		return storage.Dashboard{}, err
	}
	return r.GetDashboard(ctx, athleteID, id)
}

func (r *Repository) DeleteDashboard(ctx context.Context, athleteID, id string) error {
	res, err := r.db.ExecContext(ctx,
		`DELETE FROM dashboards WHERE athlete_id = ? AND id = ?`,
		athleteID, id,
	)
	return requireAffected(res, err, "dashboard not found")
}

func (r *Repository) CreateWidget(ctx context.Context, dashboardID, athleteID, widgetType string, config, position json.RawMessage) (storage.Widget, error) {
	if ok, err := r.dashboardBelongsToAthlete(ctx, dashboardID, athleteID); err != nil {
		return storage.Widget{}, err
	} else if !ok {
		return storage.Widget{}, fmt.Errorf("dashboard not found")
	}
	if len(config) == 0 {
		config = json.RawMessage(`{}`)
	}
	if len(position) == 0 {
		position = json.RawMessage(`{"x":0,"y":0,"w":4,"h":3}`)
	}
	now := time.Now().UTC()
	w := storage.Widget{
		ID:          uuid.NewString(),
		DashboardID: dashboardID,
		Type:        widgetType,
		Config:      config,
		Position:    position,
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	_, err := r.db.ExecContext(ctx, `
		INSERT INTO dashboard_widgets (
			id, dashboard_id, type, config, position, created_at, updated_at
		)
		VALUES (?, ?, ?, ?, ?, ?, ?)`,
		w.ID,
		dashboardID,
		widgetType,
		string(config),
		string(position),
		formatTime(now),
		formatTime(now),
	)
	if err != nil {
		return storage.Widget{}, err
	}
	return w, nil
}

func (r *Repository) ListWidgets(ctx context.Context, dashboardID, athleteID string) ([]storage.Widget, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT w.id, w.dashboard_id, w.type, w.config, w.position, w.created_at, w.updated_at
		FROM dashboard_widgets w
		JOIN dashboards d ON d.id = w.dashboard_id
		WHERE w.dashboard_id = ? AND d.athlete_id = ?
		ORDER BY w.created_at ASC`,
		dashboardID, athleteID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []storage.Widget
	for rows.Next() {
		w, err := scanWidget(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, w)
	}
	return out, rows.Err()
}

func (r *Repository) UpdateWidget(ctx context.Context, widgetID, dashboardID, athleteID string, config, position json.RawMessage) (storage.Widget, error) {
	res, err := r.db.ExecContext(ctx, `
		UPDATE dashboard_widgets
		SET config = COALESCE(?, config),
		    position = COALESCE(?, position),
		    updated_at = ?
		WHERE id = ?
		  AND dashboard_id = ?
		  AND EXISTS (
		    SELECT 1 FROM dashboards
		    WHERE dashboards.id = dashboard_widgets.dashboard_id
		      AND dashboards.athlete_id = ?
		  )`,
		nullableJSON(config),
		nullableJSON(position),
		formatTime(time.Now().UTC()),
		widgetID,
		dashboardID,
		athleteID,
	)
	if err := requireAffected(res, err, "widget not found"); err != nil {
		return storage.Widget{}, err
	}
	return r.getWidget(ctx, widgetID, dashboardID, athleteID)
}

func (r *Repository) DeleteWidget(ctx context.Context, widgetID, dashboardID, athleteID string) error {
	res, err := r.db.ExecContext(ctx, `
		DELETE FROM dashboard_widgets
		WHERE id = ?
		  AND dashboard_id = ?
		  AND EXISTS (
		    SELECT 1 FROM dashboards
		    WHERE dashboards.id = dashboard_widgets.dashboard_id
		      AND dashboards.athlete_id = ?
		  )`,
		widgetID, dashboardID, athleteID,
	)
	return requireAffected(res, err, "widget not found")
}

func (r *Repository) dashboardBelongsToAthlete(ctx context.Context, dashboardID, athleteID string) (bool, error) {
	var n int
	err := r.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM dashboards WHERE id = ? AND athlete_id = ?`,
		dashboardID, athleteID,
	).Scan(&n)
	return n > 0, err
}

func (r *Repository) getWidget(ctx context.Context, widgetID, dashboardID, athleteID string) (storage.Widget, error) {
	row := r.db.QueryRowContext(ctx, `
		SELECT w.id, w.dashboard_id, w.type, w.config, w.position, w.created_at, w.updated_at
		FROM dashboard_widgets w
		JOIN dashboards d ON d.id = w.dashboard_id
		WHERE w.id = ? AND w.dashboard_id = ? AND d.athlete_id = ?`,
		widgetID, dashboardID, athleteID,
	)
	w, err := scanWidget(row)
	if errors.Is(err, sql.ErrNoRows) {
		return storage.Widget{}, fmt.Errorf("widget not found")
	}
	return w, err
}

func scanDashboard(s scanner) (storage.Dashboard, error) {
	var (
		d             storage.Dashboard
		createdAtText string
		updatedAtText string
	)
	if err := s.Scan(&d.ID, &d.Name, &d.WidgetCount, &createdAtText, &updatedAtText); err != nil {
		return storage.Dashboard{}, err
	}
	createdAt, err := parseTime(createdAtText)
	if err != nil {
		return storage.Dashboard{}, err
	}
	updatedAt, err := parseTime(updatedAtText)
	if err != nil {
		return storage.Dashboard{}, err
	}
	d.CreatedAt = createdAt
	d.UpdatedAt = updatedAt
	return d, nil
}

func scanWidget(s scanner) (storage.Widget, error) {
	var (
		w             storage.Widget
		config        string
		position      string
		createdAtText string
		updatedAtText string
	)
	if err := s.Scan(
		&w.ID,
		&w.DashboardID,
		&w.Type,
		&config,
		&position,
		&createdAtText,
		&updatedAtText,
	); err != nil {
		return storage.Widget{}, err
	}
	createdAt, err := parseTime(createdAtText)
	if err != nil {
		return storage.Widget{}, err
	}
	updatedAt, err := parseTime(updatedAtText)
	if err != nil {
		return storage.Widget{}, err
	}
	w.Config = json.RawMessage(config)
	w.Position = json.RawMessage(position)
	w.CreatedAt = createdAt
	w.UpdatedAt = updatedAt
	return w, nil
}

func nullableJSON(value json.RawMessage) any {
	if len(value) == 0 || string(value) == "null" {
		return nil
	}
	return string(value)
}
