package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"athlete-log/internal/storage"

	"github.com/google/uuid"
)

func (r *Repository) CreateGear(ctx context.Context, athleteID, name, gearType, brandModel string, purchaseDate *string, alertMeters *int) (storage.Gear, error) {
	if gearType == "" {
		gearType = "other"
	}
	now := time.Now().UTC()
	g := storage.Gear{
		ID:                  uuid.NewString(),
		Name:                name,
		Type:                gearType,
		BrandModel:          brandModel,
		PurchaseDate:        nonEmptyStringPtr(purchaseDate),
		DistanceAlertMeters: alertMeters,
		CreatedAt:           now,
	}
	_, err := r.db.ExecContext(ctx, `
		INSERT INTO gear (
			id, athlete_id, name, type, brand_model, purchase_date,
			distance_alert_meters, created_at, updated_at
		)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		g.ID,
		athleteID,
		name,
		gearType,
		brandModel,
		nullableStringPtr(g.PurchaseDate),
		alertMeters,
		formatTime(now),
		formatTime(now),
	)
	if err != nil {
		return storage.Gear{}, err
	}
	return g, nil
}

func (r *Repository) ListGear(ctx context.Context, athleteID string, includeRetired bool) ([]storage.Gear, error) {
	args := []any{athleteID}
	retiredClause := ""
	if !includeRetired {
		retiredClause = " AND g.retired_at IS NULL"
	}
	rows, err := r.db.QueryContext(ctx, `
		SELECT g.id, g.name, g.type, g.brand_model,
		       g.purchase_date, g.distance_alert_meters, g.retired_at,
		       COALESCE(SUM(COALESCE(a.distance_meters, 0)), 0) AS total_distance,
		       COUNT(DISTINCT ag.activity_id) AS activity_count,
		       g.created_at
		FROM gear g
		LEFT JOIN activity_gear ag ON ag.gear_id = g.id
		LEFT JOIN activities a ON a.id = ag.activity_id
		WHERE g.athlete_id = ?`+retiredClause+`
		GROUP BY g.id, g.name, g.type, g.brand_model, g.purchase_date,
		         g.distance_alert_meters, g.retired_at, g.created_at
		ORDER BY g.created_at ASC`,
		args...,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanGearRows(rows)
}

func (r *Repository) UpdateGear(ctx context.Context, athleteID, id, name, gearType, brandModel string, purchaseDate *string, alertMeters *int, retire bool) (storage.Gear, error) {
	if gearType == "" {
		gearType = "other"
	}
	var retiredAt any
	if retire {
		retiredAt = formatTime(time.Now().UTC())
	}
	res, err := r.db.ExecContext(ctx, `
		UPDATE gear
		SET name = ?,
		    type = ?,
		    brand_model = ?,
		    purchase_date = ?,
		    distance_alert_meters = ?,
		    retired_at = ?,
		    updated_at = ?
		WHERE id = ? AND athlete_id = ?`,
		name,
		gearType,
		brandModel,
		nullableStringPtr(nonEmptyStringPtr(purchaseDate)),
		alertMeters,
		retiredAt,
		formatTime(time.Now().UTC()),
		id,
		athleteID,
	)
	if err := requireAffected(res, err, "gear not found"); err != nil {
		return storage.Gear{}, err
	}
	return r.getGear(ctx, athleteID, id)
}

func (r *Repository) DeleteGear(ctx context.Context, athleteID, id string) error {
	res, err := r.db.ExecContext(ctx,
		`DELETE FROM gear WHERE id = ? AND athlete_id = ?`,
		id, athleteID,
	)
	return requireAffected(res, err, "gear not found")
}

func (r *Repository) GetActivityGear(ctx context.Context, activityID string) ([]storage.Gear, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT g.id, g.name, g.type, g.brand_model,
		       g.purchase_date, g.distance_alert_meters, g.retired_at,
		       0 AS total_distance, 0 AS activity_count, g.created_at
		FROM gear g
		JOIN activity_gear ag ON ag.gear_id = g.id
		WHERE ag.activity_id = ?
		ORDER BY g.name`,
		activityID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanGearRows(rows)
}

func (r *Repository) SetActivityGear(ctx context.Context, activityID string, gearIDs []string) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback() //nolint:errcheck

	if _, err := tx.ExecContext(ctx, `DELETE FROM activity_gear WHERE activity_id = ?`, activityID); err != nil {
		return err
	}
	for _, gearID := range gearIDs {
		if _, err := tx.ExecContext(ctx, `
			INSERT OR IGNORE INTO activity_gear (activity_id, gear_id)
			VALUES (?, ?)`,
			activityID, gearID,
		); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (r *Repository) getGear(ctx context.Context, athleteID, id string) (storage.Gear, error) {
	row := r.db.QueryRowContext(ctx, `
		SELECT g.id, g.name, g.type, g.brand_model,
		       g.purchase_date, g.distance_alert_meters, g.retired_at,
		       COALESCE(SUM(COALESCE(a.distance_meters, 0)), 0) AS total_distance,
		       COUNT(DISTINCT ag.activity_id) AS activity_count,
		       g.created_at
		FROM gear g
		LEFT JOIN activity_gear ag ON ag.gear_id = g.id
		LEFT JOIN activities a ON a.id = ag.activity_id
		WHERE g.athlete_id = ? AND g.id = ?
		GROUP BY g.id, g.name, g.type, g.brand_model, g.purchase_date,
		         g.distance_alert_meters, g.retired_at, g.created_at`,
		athleteID, id,
	)
	g, err := scanGear(row)
	if errors.Is(err, sql.ErrNoRows) {
		return storage.Gear{}, fmt.Errorf("gear not found")
	}
	return g, err
}

func scanGearRows(rows *sql.Rows) ([]storage.Gear, error) {
	var out []storage.Gear
	for rows.Next() {
		g, err := scanGear(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, g)
	}
	return out, rows.Err()
}

func scanGear(s scanner) (storage.Gear, error) {
	var (
		g            storage.Gear
		purchaseDate sql.NullString
		alertMeters  sql.NullInt64
		retiredAt    sql.NullString
		createdAt    string
	)
	if err := s.Scan(
		&g.ID,
		&g.Name,
		&g.Type,
		&g.BrandModel,
		&purchaseDate,
		&alertMeters,
		&retiredAt,
		&g.TotalDistanceMeters,
		&g.ActivityCount,
		&createdAt,
	); err != nil {
		return storage.Gear{}, err
	}
	if purchaseDate.Valid {
		g.PurchaseDate = &purchaseDate.String
	}
	if alertMeters.Valid {
		v := int(alertMeters.Int64)
		g.DistanceAlertMeters = &v
	}
	if retiredAt.Valid {
		t, err := parseTime(retiredAt.String)
		if err != nil {
			return storage.Gear{}, err
		}
		g.RetiredAt = &t
	}
	t, err := parseTime(createdAt)
	if err != nil {
		return storage.Gear{}, err
	}
	g.CreatedAt = t
	return g, nil
}

func nonEmptyStringPtr(value *string) *string {
	if value == nil || *value == "" {
		return nil
	}
	return value
}

func nullableStringPtr(value *string) any {
	if value == nil {
		return nil
	}
	return *value
}
