package migrations

import (
	"database/sql"
	"embed"
	"errors"
	"fmt"

	"github.com/golang-migrate/migrate/v4"
	"github.com/golang-migrate/migrate/v4/database/postgres"
	"github.com/golang-migrate/migrate/v4/source/iofs"
	_ "github.com/jackc/pgx/v5/stdlib"
)

type Direction string

const (
	Up        Direction = "up"
	ToVersion Direction = "to_version"
	StepBack  Direction = "step_back"
	Down      Direction = "down"
)

type MigrateConfig struct {
	Direction Direction
	Version   int
}

// PostgresMigrate applies SQL files from embed.FS. Full Down is rejected.
func PostgresMigrate(connectionStr string, cnf MigrateConfig, files embed.FS) (int, error) {
	if cnf.Direction == Down {
		return 0, fmt.Errorf("down migration not supported, use step_back instead")
	}

	db, err := sql.Open("pgx", connectionStr)
	if err != nil {
		return 0, fmt.Errorf("failed to open postgres connection: %w", err)
	}
	defer db.Close()

	driver, err := postgres.WithInstance(db, &postgres.Config{})
	if err != nil {
		return 0, fmt.Errorf("failed to init postgres driver: %w", err)
	}

	src, err := iofs.New(files, "postgres")
	if err != nil {
		return 0, fmt.Errorf("failed to create migrator source: %w", err)
	}

	m, err := migrate.NewWithInstance("iofs", src, "postgres", driver)
	if err != nil {
		return 0, fmt.Errorf("failed to create migrator instance: %w", err)
	}

	switch cnf.Direction {
	case Up:
		err = m.Up()
		if err != nil && !errors.Is(err, migrate.ErrNoChange) {
			var derr migrate.ErrDirty
			if errors.As(err, &derr) {
				return 0, fmt.Errorf("dirty at version %d: %w", derr.Version, err)
			}
			return 0, fmt.Errorf("failed to run postgres migrations: %w", err)
		}
	case ToVersion:
		if cnf.Version < 1 {
			return 0, fmt.Errorf("invalid migration config version: %d", cnf.Version)
		}
		v, dirty, verr := m.Version()
		if verr != nil {
			if errors.Is(verr, migrate.ErrNilVersion) {
				v = 0
			} else {
				return 0, fmt.Errorf("failed to get postgres migration version: %w", verr)
			}
		}
		if dirty {
			return 0, fmt.Errorf("postgres migration is dirty at version %d", v)
		}
		steps := cnf.Version - int(v)
		if steps != 0 {
			if err := m.Steps(steps); err != nil {
				return 0, fmt.Errorf("failed to run postgres migrations: %w", err)
			}
		}
	case StepBack:
		if err := m.Steps(-1); err != nil {
			return 0, fmt.Errorf("failed to run postgres migrations: %w", err)
		}
	default:
		return 0, fmt.Errorf("invalid migration direction: %s", cnf.Direction)
	}

	v, _, err := m.Version()
	if err != nil {
		return 0, fmt.Errorf("migration applied but version fetch failed: %w", err)
	}
	return int(v), nil
}
