package plugin

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

var ErrMigration = errors.New("plugin migration history or execution rejected")

func appliedMigrations(ctx context.Context, tx *sql.Tx, id string, steps []Migration) (int, error) {
	rows, err := tx.QueryContext(ctx, "SELECT position,name,checksum FROM plugin_migrations WHERE plugin_id=? ORDER BY position", id)
	if err != nil {
		return 0, err
	}
	defer rows.Close()
	count := 0
	for rows.Next() {
		var position int
		var recorded Migration
		if err := rows.Scan(&position, &recorded.Name, &recorded.Checksum); err != nil {
			return 0, err
		}
		if position != count || count >= len(steps) || steps[count] != recorded {
			return 0, ErrMigration
		}
		count++
	}
	return count, rows.Err()
}

func checkMigrationHistory(ctx context.Context, tx *sql.Tx, id string, steps []Migration) error {
	_, err := appliedMigrations(ctx, tx, id, steps)
	return err
}

func (s *Service) applyMigrations(ctx context.Context, tx *sql.Tx, p *loaded) error {
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	start, err := appliedMigrations(ctx, tx, p.installID, p.manifest.Migrations)
	if err != nil {
		return err
	}
	for i := start; i < len(p.manifest.Migrations); i++ {
		step := p.manifest.Migrations[i]
		if err := s.Runtime.invokeTask(ctx, p, "migration", step.Name, tx); err != nil {
			return ErrMigration
		}
		if _, err := tx.ExecContext(ctx, "INSERT INTO plugin_migrations VALUES(?,?,?,?)", p.installID, i, step.Name, step.Checksum); err != nil {
			return err
		}
	}
	return nil
}
