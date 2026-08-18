package repository

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"telegram-tracker/internal/errorx"
	"telegram-tracker/internal/model"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Repo interface {
	UpsertUser(ctx context.Context, userID int64, username, firstName string) error
	SetUserActive(ctx context.Context, userID int64, active bool) error
	ListActiveUserIDs(ctx context.Context) ([]int64, error)

	CreateCheckin(ctx context.Context, c *model.Checkin) (int64, error)
	GetCheckin(ctx context.Context, userID, id int64) (*model.Checkin, error)
	UpdateCheckinField(ctx context.Context, userID, id int64, field string, value any) error
	ListCheckinsByDay(ctx context.Context, userID int64, localDate time.Time) ([]model.Checkin, error)

	CreateFollowup(ctx context.Context, f *model.CravingFollowup) error

	UpsertDailyReview(ctx context.Context, r *model.DailyReview) error
	GetDailyReview(ctx context.Context, userID int64, localDate time.Time) (*model.DailyReview, error)

	CreateJob(ctx context.Context, j *model.Job) error
	ClaimDueJobs(ctx context.Context, now time.Time, grace time.Duration) ([]model.Job, error)
	MarkJobStatus(ctx context.Context, id int64, status model.JobStatus) error

	HistoryDays(ctx context.Context, userID int64, from, to time.Time) ([]model.DaySummary, error)
	TodayStats(ctx context.Context, userID int64, localDate time.Time) (*model.TodayStats, error)
	WeekStats(ctx context.Context, userID int64, from, to time.Time) (*model.WeekStats, error)
}

type repo struct {
	pool *pgxpool.Pool
	log  *slog.Logger
}

func New(pool *pgxpool.Pool, log *slog.Logger) Repo {
	return &repo{pool: pool, log: log}
}

func OpenPool(ctx context.Context, url string, maxConns, minConns int32, maxLifetime, maxIdle time.Duration) (*pgxpool.Pool, error) {
	cfg, err := pgxpool.ParseConfig(url)
	if err != nil {
		return nil, fmt.Errorf("parse database url: %w", err)
	}
	cfg.MaxConns = maxConns
	cfg.MinConns = minConns
	cfg.MaxConnLifetime = maxLifetime
	cfg.MaxConnIdleTime = maxIdle
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("open pool: %w", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("ping postgres: %w", err)
	}
	return pool, nil
}

func (r *repo) UpsertUser(ctx context.Context, userID int64, username, firstName string) error {
	now := time.Now().UTC()
	_, err := r.pool.Exec(ctx, `
		INSERT INTO users (id, username, first_name, is_active, created_at, updated_at)
		VALUES ($1, NULLIF($2, ''), NULLIF($3, ''), TRUE, $4, $4)
		ON CONFLICT (id) DO UPDATE SET
			username = EXCLUDED.username,
			first_name = EXCLUDED.first_name,
			is_active = TRUE,
			updated_at = EXCLUDED.updated_at
	`, userID, username, firstName, now)
	if err != nil {
		return fmt.Errorf("upsert user: %w", err)
	}
	return nil
}

func (r *repo) SetUserActive(ctx context.Context, userID int64, active bool) error {
	_, err := r.pool.Exec(ctx, `
		UPDATE users SET is_active = $2, updated_at = $3 WHERE id = $1
	`, userID, active, time.Now().UTC())
	if err != nil {
		return fmt.Errorf("set user active: %w", err)
	}
	return nil
}

func (r *repo) ListActiveUserIDs(ctx context.Context) ([]int64, error) {
	rows, err := r.pool.Query(ctx, `SELECT id FROM users WHERE is_active = TRUE`)
	if err != nil {
		return nil, fmt.Errorf("list active users: %w", err)
	}
	defer rows.Close()
	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

func (r *repo) CreateCheckin(ctx context.Context, c *model.Checkin) (int64, error) {
	var id int64
	err := r.pool.QueryRow(ctx, `
		INSERT INTO checkins (
			user_id, created_at, local_date, updated_at,
			mood, stress, irritation, smoking_craving, alcohol_craving, life_enjoyment,
			need, context, coping_action, source, status
		) VALUES (
			$1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15
		) RETURNING id
	`, c.UserID, c.CreatedAt, c.LocalDate, c.UpdatedAt,
		c.Mood, c.Stress, c.Irritation, c.SmokingCraving, c.AlcoholCraving, c.LifeEnjoyment,
		c.Need, c.Context, c.CopingAction, c.Source, c.Status,
	).Scan(&id)
	if err != nil {
		return 0, fmt.Errorf("create checkin: %w", err)
	}
	return id, nil
}

func (r *repo) GetCheckin(ctx context.Context, userID, id int64) (*model.Checkin, error) {
	row := r.pool.QueryRow(ctx, `
		SELECT id, user_id, created_at, local_date, updated_at,
			mood, stress, irritation, smoking_craving, alcohol_craving, life_enjoyment,
			need, context, coping_action, source, status
		FROM checkins WHERE id = $1 AND user_id = $2
	`, id, userID)
	c, err := scanCheckin(row)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, errorx.ErrNotFound
		}
		return nil, fmt.Errorf("get checkin: %w", err)
	}
	return c, nil
}

func (r *repo) UpdateCheckinField(ctx context.Context, userID, id int64, field string, value any) error {
	allowed := map[string]string{
		"mood":             "mood",
		"stress":           "stress",
		"irritation":       "irritation",
		"smoking_craving":  "smoking_craving",
		"alcohol_craving":  "alcohol_craving",
		"life_enjoyment":   "life_enjoyment",
		"need":             "need",
		"context":          "context",
		"coping_action":    "coping_action",
	}
	col, ok := allowed[field]
	if !ok {
		return fmt.Errorf("unknown checkin field %q", field)
	}
	q := fmt.Sprintf(`UPDATE checkins SET %s = $3, updated_at = $4 WHERE id = $1 AND user_id = $2`, col)
	tag, err := r.pool.Exec(ctx, q, id, userID, value, time.Now().UTC())
	if err != nil {
		return fmt.Errorf("update checkin field: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return errorx.ErrNotFound
	}
	return nil
}

func (r *repo) ListCheckinsByDay(ctx context.Context, userID int64, localDate time.Time) ([]model.Checkin, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT id, user_id, created_at, local_date, updated_at,
			mood, stress, irritation, smoking_craving, alcohol_craving, life_enjoyment,
			need, context, coping_action, source, status
		FROM checkins
		WHERE user_id = $1 AND local_date = $2
		ORDER BY created_at
	`, userID, localDate)
	if err != nil {
		return nil, fmt.Errorf("list checkins: %w", err)
	}
	defer rows.Close()
	var out []model.Checkin
	for rows.Next() {
		c, err := scanCheckin(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *c)
	}
	return out, rows.Err()
}

func (r *repo) CreateFollowup(ctx context.Context, f *model.CravingFollowup) error {
	_, err := r.pool.Exec(ctx, `
		INSERT INTO craving_followups (checkin_id, created_at, smoking_craving, alcohol_craving)
		VALUES ($1,$2,$3,$4)
	`, f.CheckinID, f.CreatedAt, f.SmokingCraving, f.AlcoholCraving)
	if err != nil {
		return fmt.Errorf("create followup: %w", err)
	}
	return nil
}

func (r *repo) UpsertDailyReview(ctx context.Context, rev *model.DailyReview) error {
	_, err := r.pool.Exec(ctx, `
		INSERT INTO daily_reviews (
			user_id, local_date, max_smoking_craving, max_alcohol_craving,
			hardest_moment, best_coping_action, smoked, drank_alcohol,
			created_at, updated_at
		) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)
		ON CONFLICT (user_id, local_date) DO UPDATE SET
			max_smoking_craving = EXCLUDED.max_smoking_craving,
			max_alcohol_craving = EXCLUDED.max_alcohol_craving,
			hardest_moment = EXCLUDED.hardest_moment,
			best_coping_action = EXCLUDED.best_coping_action,
			smoked = EXCLUDED.smoked,
			drank_alcohol = EXCLUDED.drank_alcohol,
			updated_at = EXCLUDED.updated_at
	`, rev.UserID, rev.LocalDate, rev.MaxSmokingCraving, rev.MaxAlcoholCraving,
		rev.HardestMoment, rev.BestCopingAction, rev.Smoked, rev.DrankAlcohol,
		rev.CreatedAt, rev.UpdatedAt)
	if err != nil {
		return fmt.Errorf("upsert daily review: %w", err)
	}
	return nil
}

func (r *repo) GetDailyReview(ctx context.Context, userID int64, localDate time.Time) (*model.DailyReview, error) {
	row := r.pool.QueryRow(ctx, `
		SELECT id, user_id, local_date, max_smoking_craving, max_alcohol_craving,
			hardest_moment, best_coping_action, smoked, drank_alcohol, created_at, updated_at
		FROM daily_reviews WHERE user_id = $1 AND local_date = $2
	`, userID, localDate)
	var d model.DailyReview
	err := row.Scan(&d.ID, &d.UserID, &d.LocalDate, &d.MaxSmokingCraving, &d.MaxAlcoholCraving,
		&d.HardestMoment, &d.BestCopingAction, &d.Smoked, &d.DrankAlcohol, &d.CreatedAt, &d.UpdatedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, errorx.ErrNotFound
		}
		return nil, fmt.Errorf("get daily review: %w", err)
	}
	return &d, nil
}

func (r *repo) CreateJob(ctx context.Context, j *model.Job) error {
	_, err := r.pool.Exec(ctx, `
		INSERT INTO jobs (user_id, kind, payload, fire_at, status, created_at)
		VALUES ($1,$2,$3,$4,$5,$6)
	`, j.UserID, j.Kind, j.Payload, j.FireAt, j.Status, j.CreatedAt)
	if err != nil {
		return fmt.Errorf("create job: %w", err)
	}
	return nil
}

func (r *repo) ClaimDueJobs(ctx context.Context, now time.Time, grace time.Duration) ([]model.Job, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	cutoff := now.Add(-grace)
	if _, err := tx.Exec(ctx, `
		UPDATE jobs SET status = 'expired'
		WHERE status = 'pending' AND fire_at < $1
	`, cutoff); err != nil {
		return nil, fmt.Errorf("expire jobs: %w", err)
	}

	rows, err := tx.Query(ctx, `
		SELECT id, user_id, kind, payload, fire_at, status, created_at
		FROM jobs
		WHERE status = 'pending' AND fire_at <= $1
		ORDER BY fire_at
		FOR UPDATE SKIP LOCKED
	`, now)
	if err != nil {
		return nil, fmt.Errorf("claim jobs: %w", err)
	}
	defer rows.Close()

	var jobs []model.Job
	for rows.Next() {
		var j model.Job
		if err := rows.Scan(&j.ID, &j.UserID, &j.Kind, &j.Payload, &j.FireAt, &j.Status, &j.CreatedAt); err != nil {
			return nil, err
		}
		jobs = append(jobs, j)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	rows.Close()

	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return jobs, nil
}

func (r *repo) MarkJobStatus(ctx context.Context, id int64, status model.JobStatus) error {
	_, err := r.pool.Exec(ctx, `UPDATE jobs SET status = $2 WHERE id = $1`, id, status)
	if err != nil {
		return fmt.Errorf("mark job: %w", err)
	}
	return nil
}

func (r *repo) HistoryDays(ctx context.Context, userID int64, from, to time.Time) ([]model.DaySummary, error) {
	rows, err := r.pool.Query(ctx, `
		WITH days AS (
			SELECT gs::date AS d
			FROM generate_series($2::date, $3::date, interval '1 day') gs
		)
		SELECT
			days.d,
			COALESCE((SELECT COUNT(*) FROM checkins c WHERE c.user_id = $1 AND c.local_date = days.d), 0) AS cnt,
			(SELECT COUNT(*) FROM daily_reviews dr WHERE dr.user_id = $1 AND dr.local_date = days.d) > 0 AS has_review,
			(SELECT smoked FROM daily_reviews dr WHERE dr.user_id = $1 AND dr.local_date = days.d),
			(SELECT drank_alcohol FROM daily_reviews dr WHERE dr.user_id = $1 AND dr.local_date = days.d)
		FROM days
		ORDER BY days.d DESC
	`, userID, from, to)
	if err != nil {
		return nil, fmt.Errorf("history days: %w", err)
	}
	defer rows.Close()
	var out []model.DaySummary
	for rows.Next() {
		var s model.DaySummary
		if err := rows.Scan(&s.Date, &s.CheckinCount, &s.HasReview, &s.Smoked, &s.DrankAlcohol); err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

func (r *repo) TodayStats(ctx context.Context, userID int64, localDate time.Time) (*model.TodayStats, error) {
	var s model.TodayStats
	err := r.pool.QueryRow(ctx, `
		SELECT
			COUNT(*) FILTER (WHERE status = 'completed'),
			AVG(smoking_craving) FILTER (WHERE status = 'completed'),
			MAX(smoking_craving) FILTER (WHERE status = 'completed'),
			AVG(alcohol_craving) FILTER (WHERE status = 'completed'),
			MAX(alcohol_craving) FILTER (WHERE status = 'completed'),
			AVG(stress) FILTER (WHERE status = 'completed'),
			AVG(irritation) FILTER (WHERE status = 'completed')
		FROM checkins
		WHERE user_id = $1 AND local_date = $2
	`, userID, localDate).Scan(
		&s.CheckinCount, &s.AvgSmoking, &s.MaxSmoking, &s.AvgAlcohol, &s.MaxAlcohol, &s.AvgStress, &s.AvgIrritation,
	)
	if err != nil {
		return nil, fmt.Errorf("today stats: %w", err)
	}
	return &s, nil
}

func (r *repo) WeekStats(ctx context.Context, userID int64, from, to time.Time) (*model.WeekStats, error) {
	var s model.WeekStats
	err := r.pool.QueryRow(ctx, `
		SELECT
			COUNT(*) FILTER (WHERE status = 'completed'),
			AVG(mood) FILTER (WHERE status = 'completed'),
			AVG(stress) FILTER (WHERE status = 'completed'),
			AVG(irritation) FILTER (WHERE status = 'completed'),
			AVG(smoking_craving) FILTER (WHERE status = 'completed'),
			MAX(smoking_craving) FILTER (WHERE status = 'completed'),
			AVG(alcohol_craving) FILTER (WHERE status = 'completed'),
			MAX(alcohol_craving) FILTER (WHERE status = 'completed'),
			AVG(life_enjoyment) FILTER (WHERE status = 'completed')
		FROM checkins
		WHERE user_id = $1 AND local_date >= $2 AND local_date <= $3
	`, userID, from, to).Scan(
		&s.CheckinCount, &s.AvgMood, &s.AvgStress, &s.AvgIrritation,
		&s.AvgSmoking, &s.MaxSmoking, &s.AvgAlcohol, &s.MaxAlcohol, &s.AvgLife,
	)
	if err != nil {
		return nil, fmt.Errorf("week stats: %w", err)
	}

	err = r.pool.QueryRow(ctx, `
		SELECT
			COUNT(*),
			COUNT(*) FILTER (WHERE smoked IS FALSE),
			COUNT(*) FILTER (WHERE drank_alcohol IS FALSE)
		FROM daily_reviews
		WHERE user_id = $1 AND local_date >= $2 AND local_date <= $3
	`, userID, from, to).Scan(&s.ReviewDays, &s.SmokeFreeDays, &s.AlcoholFreeDays)
	if err != nil {
		return nil, fmt.Errorf("week reviews: %w", err)
	}

	s.TopContexts, err = r.topText(ctx, userID, from, to, "context")
	if err != nil {
		return nil, err
	}
	s.TopCoping, err = r.topText(ctx, userID, from, to, "coping_action")
	if err != nil {
		return nil, err
	}

	if err := r.followupStats(ctx, userID, from, to, &s); err != nil {
		return nil, err
	}
	return &s, nil
}

func (r *repo) topText(ctx context.Context, userID int64, from, to time.Time, col string) ([]string, error) {
	if col != "context" && col != "coping_action" {
		return nil, fmt.Errorf("invalid column")
	}
	q := fmt.Sprintf(`
		SELECT %s, COUNT(*) AS n
		FROM checkins
		WHERE user_id = $1 AND local_date >= $2 AND local_date <= $3
			AND status = 'completed' AND %s IS NOT NULL AND %s <> ''
		GROUP BY 1
		ORDER BY n DESC
		LIMIT 3
	`, col, col, col)
	rows, err := r.pool.Query(ctx, q, userID, from, to)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var v string
		var n int
		if err := rows.Scan(&v, &n); err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

func (r *repo) followupStats(ctx context.Context, userID int64, from, to time.Time, s *model.WeekStats) error {
	rows, err := r.pool.Query(ctx, `
		SELECT c.smoking_craving, f.smoking_craving, c.alcohol_craving, f.alcohol_craving
		FROM craving_followups f
		JOIN checkins c ON c.id = f.checkin_id
		WHERE c.user_id = $1 AND c.local_date >= $2 AND c.local_date <= $3
	`, userID, from, to)
	if err != nil {
		return fmt.Errorf("followup stats: %w", err)
	}
	defer rows.Close()

	var beforeSum, afterSum float64
	var n int
	for rows.Next() {
		var cs, fs, ca, fa *int16
		if err := rows.Scan(&cs, &fs, &ca, &fa); err != nil {
			return err
		}
		if cs != nil && fs != nil && *cs >= 6 {
			beforeSum += float64(*cs)
			afterSum += float64(*fs)
			n++
		}
		if ca != nil && fa != nil && *ca >= 6 {
			beforeSum += float64(*ca)
			afterSum += float64(*fa)
			n++
		}
	}
	if err := rows.Err(); err != nil {
		return err
	}
	s.HighCravingCount = n
	if n > 0 {
		b := beforeSum / float64(n)
		a := afterSum / float64(n)
		d := a - b
		s.HighCravingAvg = &b
		s.FollowupAvg = &a
		s.FollowupDelta = &d
	}
	return nil
}

type rowScanner interface {
	Scan(dest ...any) error
}

func scanCheckin(row rowScanner) (*model.Checkin, error) {
	var c model.Checkin
	err := row.Scan(
		&c.ID, &c.UserID, &c.CreatedAt, &c.LocalDate, &c.UpdatedAt,
		&c.Mood, &c.Stress, &c.Irritation, &c.SmokingCraving, &c.AlcoholCraving, &c.LifeEnjoyment,
		&c.Need, &c.Context, &c.CopingAction, &c.Source, &c.Status,
	)
	if err != nil {
		return nil, err
	}
	return &c, nil
}
