package sqlite

import (
	"context"
	"fmt"
	"sort"
	"time"

	"athlete-log/internal/storage"

	"github.com/google/uuid"
)

func (r *Repository) ComputeAndStoreBestEfforts(ctx context.Context, athleteID, sportType string) error {
	targets := []int{400, 1000, 1609, 5000, 10000, 15000, 21097, 42195}

	rows, err := r.db.QueryContext(ctx, `
		SELECT s.activity_id, a.start_at, s.elapsed_ms, s.distance_meters
		FROM activity_samples s
		JOIN activities a ON a.id = s.activity_id
		WHERE a.athlete_id = ?
		  AND a.sport_type = ?
		  AND s.distance_meters IS NOT NULL
		  AND s.elapsed_ms IS NOT NULL
		  AND a.delete_indicator = 0
		  AND NOT EXISTS (
		    SELECT 1 FROM activity_tags
		    WHERE activity_id = a.id AND key = 'exclude_from_records'
		  )
		ORDER BY s.activity_id, s.distance_meters`,
		athleteID, sportType,
	)
	if err != nil {
		return fmt.Errorf("fetch samples: %w", err)
	}
	defer rows.Close()

	type sampleRow struct {
		elapsedMs  int64
		distMeters float64
	}
	type actEntry struct {
		startAt time.Time
		samples []sampleRow
	}

	var actOrder []string
	acts := map[string]*actEntry{}
	for rows.Next() {
		var actID string
		var startAtText string
		var elapsedMs int64
		var distMeters float64
		if err := rows.Scan(&actID, &startAtText, &elapsedMs, &distMeters); err != nil {
			return fmt.Errorf("scan sample: %w", err)
		}
		startAt, err := parseTime(startAtText)
		if err != nil {
			return err
		}
		if _, exists := acts[actID]; !exists {
			acts[actID] = &actEntry{startAt: startAt}
			actOrder = append(actOrder, actID)
		}
		acts[actID].samples = append(acts[actID].samples, sampleRow{elapsedMs, distMeters})
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("iterate samples: %w", err)
	}

	type topEntry struct {
		ms    int64
		actID string
		at    time.Time
	}
	type distResult struct {
		dist int
		top  []topEntry
	}
	var results []distResult

	for _, targetDist := range targets {
		var top3 []topEntry
		for _, actID := range actOrder {
			act := acts[actID]
			ss := act.samples
			actBestMs := int64(-1)
			hi := 0
			for lo := 0; lo < len(ss); lo++ {
				for hi < len(ss) && ss[hi].distMeters-ss[lo].distMeters < float64(targetDist) {
					hi++
				}
				if hi >= len(ss) {
					break
				}
				elapsed := ss[hi].elapsedMs - ss[lo].elapsedMs
				if elapsed > 0 && (actBestMs < 0 || elapsed < actBestMs) {
					actBestMs = elapsed
				}
			}
			if actBestMs < 0 {
				continue
			}
			entry := topEntry{actBestMs, actID, act.startAt}
			if len(top3) < 3 {
				top3 = append(top3, entry)
				sort.Slice(top3, func(i, j int) bool { return top3[i].ms < top3[j].ms })
			} else if actBestMs < top3[2].ms {
				top3[2] = entry
				sort.Slice(top3, func(i, j int) bool { return top3[i].ms < top3[j].ms })
			}
		}
		if len(top3) > 0 {
			results = append(results, distResult{dist: targetDist, top: top3})
		}
	}

	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck

	if _, err := tx.ExecContext(ctx,
		`DELETE FROM best_efforts WHERE athlete_id = ? AND sport_type = ?`,
		athleteID, sportType,
	); err != nil {
		return fmt.Errorf("delete best efforts: %w", err)
	}
	now := formatTime(time.Now().UTC())
	for _, res := range results {
		for _, e := range res.top {
			if _, err := tx.ExecContext(ctx, `
				INSERT INTO best_efforts (
					id, athlete_id, activity_id, sport_type,
					distance_meters, elapsed_ms, achieved_at, created_at
				)
				VALUES (?, ?, ?, ?, ?, ?, ?, ?)
				ON CONFLICT (athlete_id, sport_type, distance_meters, elapsed_ms, activity_id)
				DO UPDATE SET achieved_at = excluded.achieved_at`,
				uuid.NewString(),
				athleteID,
				e.actID,
				sportType,
				res.dist,
				e.ms,
				formatTime(e.at),
				now,
			); err != nil {
				return fmt.Errorf("upsert best effort %dm: %w", res.dist, err)
			}
		}
	}
	return tx.Commit()
}

func (r *Repository) GetBestEfforts(ctx context.Context, athleteID, sportType string) ([]storage.BestEffort, error) {
	args := []any{athleteID}
	clause := ""
	if sportType != "" {
		clause = " AND be.sport_type = ?"
		args = append(args, sportType)
	}
	rows, err := r.db.QueryContext(ctx, `
		SELECT be.distance_meters, be.elapsed_ms, be.activity_id, a.name, be.achieved_at
		FROM best_efforts be
		JOIN activities a ON a.id = be.activity_id
		WHERE be.athlete_id = ?`+clause+`
		ORDER BY be.distance_meters ASC, be.elapsed_ms ASC`,
		args...,
	)
	if err != nil {
		return nil, fmt.Errorf("get best efforts: %w", err)
	}
	defer rows.Close()

	var efforts []storage.BestEffort
	for rows.Next() {
		var e storage.BestEffort
		var achievedAtText string
		if err := rows.Scan(&e.DistanceMeters, &e.ElapsedMs, &e.ActivityID, &e.ActivityName, &achievedAtText); err != nil {
			return nil, err
		}
		achievedAt, err := parseTime(achievedAtText)
		if err != nil {
			return nil, err
		}
		e.AchievedAt = achievedAt
		efforts = append(efforts, e)
	}
	return efforts, rows.Err()
}
