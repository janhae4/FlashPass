package database

import (
	"context"
	"embed"
	"fmt"
	"log"
	"sort"
	"strings"
	"time"
)

//go:embed migrations/*.sql
var MigrationFS embed.FS

type Migrator struct {
	db *DB
}

func NewMigrator(db *DB) *Migrator {
	return &Migrator{db: db}
}

// EnsureMigrationTable creates the schema_migrations table if not present
func (m *Migrator) EnsureMigrationTable(ctx context.Context) error {
	query := `
	CREATE TABLE IF NOT EXISTS schema_migrations (
		version VARCHAR(255) PRIMARY KEY,
		applied_at TIMESTAMPTZ DEFAULT NOW()
	);`
	_, err := m.db.Pool.Exec(ctx, query)
	return err
}

// GetAppliedMigrations returns a set of applied migration versions
func (m *Migrator) GetAppliedMigrations(ctx context.Context) (map[string]time.Time, error) {
	if err := m.EnsureMigrationTable(ctx); err != nil {
		return nil, fmt.Errorf("failed to ensure migration table: %w", err)
	}

	rows, err := m.db.Pool.Query(ctx, "SELECT version, applied_at FROM schema_migrations ORDER BY version ASC")
	if err != nil {
		return nil, fmt.Errorf("failed to query schema_migrations: %w", err)
	}
	defer rows.Close()

	applied := make(map[string]time.Time)
	for rows.Next() {
		var ver string
		var appliedAt time.Time
		if err := rows.Scan(&ver, &appliedAt); err != nil {
			return nil, err
		}
		applied[ver] = appliedAt
	}

	return applied, nil
}

// Up applies all pending UP migrations
func (m *Migrator) Up(ctx context.Context) error {
	applied, err := m.GetAppliedMigrations(ctx)
	if err != nil {
		return err
	}

	entries, err := MigrationFS.ReadDir("migrations")
	if err != nil {
		return fmt.Errorf("failed to read migrations dir: %w", err)
	}

	var upFiles []string
	for _, entry := range entries {
		if !entry.IsDir() && strings.HasSuffix(entry.Name(), ".up.sql") {
			upFiles = append(upFiles, entry.Name())
		}
	}
	sort.Strings(upFiles)

	for _, fileName := range upFiles {
		version := strings.TrimSuffix(fileName, ".up.sql")
		if _, exists := applied[version]; exists {
			log.Printf("[Migrator] Migration %s already applied, skipping", version)
			continue
		}

		content, err := MigrationFS.ReadFile("migrations/" + fileName)
		if err != nil {
			return fmt.Errorf("failed to read migration file %s: %w", fileName, err)
		}

		log.Printf("[Migrator] Applying migration: %s", fileName)

		tx, err := m.db.Pool.Begin(ctx)
		if err != nil {
			return fmt.Errorf("failed to start tx for migration %s: %w", version, err)
		}

		if _, err := tx.Exec(ctx, string(content)); err != nil {
			_ = tx.Rollback(ctx)
			return fmt.Errorf("failed to execute migration %s: %w", version, err)
		}

		if _, err := tx.Exec(ctx, "INSERT INTO schema_migrations (version) VALUES ($1)", version); err != nil {
			_ = tx.Rollback(ctx)
			return fmt.Errorf("failed to record migration %s: %w", version, err)
		}

		if err := tx.Commit(ctx); err != nil {
			return fmt.Errorf("failed to commit migration %s: %w", version, err)
		}

		log.Printf("[Migrator] Successfully applied migration: %s", version)
	}

	return nil
}

// Down rolls back the most recent migration or all if specified
func (m *Migrator) Down(ctx context.Context) error {
	applied, err := m.GetAppliedMigrations(ctx)
	if err != nil {
		return err
	}

	if len(applied) == 0 {
		log.Println("[Migrator] No migrations to revert.")
		return nil
	}

	entries, err := MigrationFS.ReadDir("migrations")
	if err != nil {
		return fmt.Errorf("failed to read migrations dir: %w", err)
	}

	var downFiles []string
	for _, entry := range entries {
		if !entry.IsDir() && strings.HasSuffix(entry.Name(), ".down.sql") {
			downFiles = append(downFiles, entry.Name())
		}
	}
	sort.Sort(sort.Reverse(sort.StringSlice(downFiles)))

	for _, fileName := range downFiles {
		version := strings.TrimSuffix(fileName, ".down.sql")
		if _, exists := applied[version]; !exists {
			continue
		}

		content, err := MigrationFS.ReadFile("migrations/" + fileName)
		if err != nil {
			return fmt.Errorf("failed to read migration file %s: %w", fileName, err)
		}

		log.Printf("[Migrator] Reverting migration: %s", fileName)

		tx, err := m.db.Pool.Begin(ctx)
		if err != nil {
			return fmt.Errorf("failed to start tx for rollback %s: %w", version, err)
		}

		if _, err := tx.Exec(ctx, string(content)); err != nil {
			_ = tx.Rollback(ctx)
			return fmt.Errorf("failed to execute rollback %s: %w", version, err)
		}

		if _, err := tx.Exec(ctx, "DELETE FROM schema_migrations WHERE version = $1", version); err != nil {
			_ = tx.Rollback(ctx)
			return fmt.Errorf("failed to remove migration record %s: %w", version, err)
		}

		if err := tx.Commit(ctx); err != nil {
			return fmt.Errorf("failed to commit rollback %s: %w", version, err)
		}

		log.Printf("[Migrator] Successfully reverted migration: %s", version)
		break // Revert one batch at a time
	}

	return nil
}
