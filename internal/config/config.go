package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

type MigrationDirection string

const (
	MigrationUp       MigrationDirection = "up"
	MigrationToVersion MigrationDirection = "to_version"
	MigrationStepBack  MigrationDirection = "step_back"
)

// Config is loaded from the process environment. Secrets stay out of code.
type Config struct {
	TelegramBotToken string
	Timezone         string
	DatabaseURL      string

	CheckinTimes      []string
	EveningReviewTime string
	FollowupDelay     time.Duration
	SnoozeDelay       time.Duration
	JobPollInterval   time.Duration
	JobGrace          time.Duration

	BackupDir  string
	BackupKeep int

	MigrationDirection MigrationDirection
	MigrationVersion   uint

	DBMaxOpenConns    int32
	DBMinConns        int32
	DBMaxConnLifetime time.Duration
	DBMaxConnIdleTime time.Duration
}

func Load() (*Config, error) {
	_ = loadDotEnv(".env")

	token := strings.TrimSpace(os.Getenv("TELEGRAM_BOT_TOKEN"))
	if token == "" {
		return nil, fmt.Errorf("TELEGRAM_BOT_TOKEN is required")
	}

	dbURL := strings.TrimSpace(os.Getenv("DATABASE_URL"))
	if dbURL == "" {
		return nil, fmt.Errorf("DATABASE_URL is required")
	}

	tz := envOr("TIMEZONE", "Europe/Belgrade")
	if _, err := time.LoadLocation(tz); err != nil {
		return nil, fmt.Errorf("TIMEZONE %q is invalid: %w", tz, err)
	}

	times, err := parseCheckinTimes(envOr("CHECKIN_TIMES", "09:00,13:00,18:00,21:30"))
	if err != nil {
		return nil, err
	}

	evening := envOr("EVENING_REVIEW_TIME", "22:30")
	if err := validateClock(evening); err != nil {
		return nil, fmt.Errorf("EVENING_REVIEW_TIME: %w", err)
	}

	dir := MigrationDirection(strings.ToLower(envOr("MIGRATION_DIRECTION", "up")))
	switch dir {
	case MigrationUp, MigrationToVersion, MigrationStepBack:
	default:
		return nil, fmt.Errorf("MIGRATION_DIRECTION must be up, to_version or step_back")
	}

	cfg := &Config{
		TelegramBotToken:   token,
		Timezone:           tz,
		DatabaseURL:        dbURL,
		CheckinTimes:       times,
		EveningReviewTime:  evening,
		FollowupDelay:      minutesOr("FOLLOWUP_DELAY_MIN", 20),
		SnoozeDelay:        minutesOr("SNOOZE_DELAY_MIN", 15),
		JobPollInterval:    secondsOr("JOB_POLL_INTERVAL_SEC", 20),
		JobGrace:           minutesOr("JOB_GRACE_MIN", 120),
		BackupDir:          envOr("BACKUP_DIR", "/backups"),
		BackupKeep:         intOr("BACKUP_KEEP", 14),
		MigrationDirection: dir,
		MigrationVersion:   uint(intOr("MIGRATION_VERSION", 0)),
		DBMaxOpenConns:     int32(intOr("DB_MAX_OPEN_CONNS", 10)),
		DBMinConns:         int32(intOr("DB_MAX_IDLE_CONNS", 5)),
		DBMaxConnLifetime:  durationOr("DB_CONN_MAX_LIFETIME", time.Hour),
		DBMaxConnIdleTime:  durationOr("DB_CONN_MAX_IDLE_TIME", time.Hour),
	}

	if cfg.DBMinConns < 0 || cfg.DBMaxOpenConns < 1 || cfg.DBMinConns > cfg.DBMaxOpenConns {
		return nil, fmt.Errorf("invalid DB pool settings")
	}
	if dir == MigrationToVersion && cfg.MigrationVersion == 0 {
		return nil, fmt.Errorf("MIGRATION_VERSION is required when MIGRATION_DIRECTION=to_version")
	}

	return cfg, nil
}

func parseCheckinTimes(raw string) ([]string, error) {
	parts := strings.Split(raw, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		if err := validateClock(p); err != nil {
			return nil, fmt.Errorf("CHECKIN_TIMES %q: %w", p, err)
		}
		out = append(out, p)
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("CHECKIN_TIMES is empty")
	}
	return out, nil
}

func validateClock(v string) error {
	_, err := time.Parse("15:04", v)
	if err != nil {
		return fmt.Errorf("want HH:MM")
	}
	return nil
}

func envOr(key, fallback string) string {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		return v
	}
	return fallback
}

func intOr(key string, fallback int) int {
	v := strings.TrimSpace(os.Getenv(key))
	if v == "" {
		return fallback
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return fallback
	}
	return n
}

func minutesOr(key string, fallback int) time.Duration {
	return time.Duration(intOr(key, fallback)) * time.Minute
}

func secondsOr(key string, fallback int) time.Duration {
	return time.Duration(intOr(key, fallback)) * time.Second
}

func durationOr(key string, fallback time.Duration) time.Duration {
	v := strings.TrimSpace(os.Getenv(key))
	if v == "" {
		return fallback
	}
	d, err := time.ParseDuration(v)
	if err != nil {
		return fallback
	}
	return d
}

// loadDotEnv reads KEY=VAL lines from path. Missing file is not an error.
func loadDotEnv(path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, val, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		key = strings.TrimSpace(key)
		val = strings.TrimSpace(val)
		val = strings.Trim(val, `"'`)
		if os.Getenv(key) == "" {
			_ = os.Setenv(key, val)
		}
	}
	return nil
}
