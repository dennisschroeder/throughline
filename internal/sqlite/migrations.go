package sqlite

import (
	"context"
	"embed"
	"fmt"
	"io/fs"
	"sort"
	"strconv"
	"strings"
	"time"
)

//go:embed migrations/*.sql
var migrationFiles embed.FS

type migration struct {
	version int
	name    string
	sql     string
}

func (d *Database) Migrate(ctx context.Context) error {
	if err := d.ensureMigrationTable(ctx); err != nil {
		return err
	}
	migrations, err := loadMigrations()
	if err != nil {
		return err
	}
	applied, err := d.appliedMigrations(ctx)
	if err != nil {
		return err
	}
	if err := validateMigrationHistory(migrations, applied); err != nil {
		return err
	}
	for _, candidate := range migrations {
		if _, ok := applied[candidate.version]; ok {
			continue
		}
		if err := d.applyMigration(ctx, candidate); err != nil {
			return err
		}
	}
	return nil
}

func (d *Database) ensureMigrationTable(ctx context.Context) error {
	transaction, err := d.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin migration bootstrap: %w", err)
	}
	defer transaction.Rollback()
	_, err = transaction.ExecContext(ctx, `
CREATE TABLE IF NOT EXISTS schema_migrations (
  version INTEGER PRIMARY KEY,
  name TEXT NOT NULL UNIQUE,
  applied_at TEXT NOT NULL
)`)
	if err != nil {
		return fmt.Errorf("create migration table: %w", err)
	}
	if err := transaction.Commit(); err != nil {
		return fmt.Errorf("commit migration bootstrap: %w", err)
	}
	return nil
}

func (d *Database) appliedMigrations(ctx context.Context) (map[int]string, error) {
	rows, err := d.db.QueryContext(ctx, "SELECT version, name FROM schema_migrations ORDER BY version")
	if err != nil {
		return nil, fmt.Errorf("query schema migrations: %w", err)
	}
	defer rows.Close()
	applied := make(map[int]string)
	for rows.Next() {
		var version int
		var name string
		if err := rows.Scan(&version, &name); err != nil {
			return nil, fmt.Errorf("scan schema migration: %w", err)
		}
		applied[version] = name
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate schema migrations: %w", err)
	}
	return applied, nil
}

func validateMigrationHistory(migrations []migration, applied map[int]string) error {
	embedded := make(map[int]string, len(migrations))
	for _, candidate := range migrations {
		embedded[candidate.version] = candidate.name
	}
	for version, name := range applied {
		expected, ok := embedded[version]
		if !ok {
			return fmt.Errorf("applied migration %d (%s) is not recognized by this binary", version, name)
		}
		if name != expected {
			return fmt.Errorf("applied migration %d name mismatch: database has %q, binary has %q", version, name, expected)
		}
	}
	missing := false
	for _, candidate := range migrations {
		_, ok := applied[candidate.version]
		if !ok {
			missing = true
			continue
		}
		if missing {
			return fmt.Errorf("applied migration history is not a contiguous prefix at version %d", candidate.version)
		}
	}
	return nil
}

func (d *Database) applyMigration(ctx context.Context, candidate migration) error {
	transaction, err := d.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin migration %s: %w", candidate.name, err)
	}
	defer transaction.Rollback()
	if _, err := transaction.ExecContext(ctx, candidate.sql); err != nil {
		return fmt.Errorf("apply migration %s: %w", candidate.name, err)
	}
	if _, err := transaction.ExecContext(ctx,
		"INSERT INTO schema_migrations(version, name, applied_at) VALUES (?, ?, ?)",
		candidate.version, candidate.name, formatTime(time.Now()),
	); err != nil {
		return fmt.Errorf("track migration %s: %w", candidate.name, err)
	}
	if err := transaction.Commit(); err != nil {
		return fmt.Errorf("commit migration %s: %w", candidate.name, err)
	}
	return nil
}

func loadMigrations() ([]migration, error) {
	entries, err := fs.ReadDir(migrationFiles, "migrations")
	if err != nil {
		return nil, fmt.Errorf("read embedded migrations: %w", err)
	}
	result := make([]migration, 0, len(entries))
	seen := make(map[int]string)
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".sql") {
			continue
		}
		prefix, _, ok := strings.Cut(entry.Name(), "_")
		if !ok {
			return nil, fmt.Errorf("migration %q has no numeric prefix", entry.Name())
		}
		version, err := strconv.Atoi(prefix)
		if err != nil || version < 1 {
			return nil, fmt.Errorf("migration %q has invalid version", entry.Name())
		}
		if existing, duplicate := seen[version]; duplicate {
			return nil, fmt.Errorf("migrations %q and %q share version %d", existing, entry.Name(), version)
		}
		content, err := migrationFiles.ReadFile("migrations/" + entry.Name())
		if err != nil {
			return nil, fmt.Errorf("read migration %q: %w", entry.Name(), err)
		}
		seen[version] = entry.Name()
		result = append(result, migration{version: version, name: entry.Name(), sql: string(content)})
	}
	sort.Slice(result, func(i, j int) bool { return result[i].version < result[j].version })
	return result, nil
}
