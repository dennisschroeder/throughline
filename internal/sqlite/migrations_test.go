package sqlite

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestFormatTimePreservesLexicalOrder(t *testing.T) {
	earlier := time.Date(2026, 8, 21, 12, 30, 0, 0, time.UTC)
	later := earlier.Add(time.Nanosecond)

	formattedEarlier := formatTime(earlier)
	formattedLater := formatTime(later)
	if formattedEarlier != "2026-08-21T12:30:00.000000000Z" {
		t.Fatalf("formatted timestamp %q is not fixed-width", formattedEarlier)
	}
	if formattedEarlier >= formattedLater {
		t.Fatalf("timestamp text order is not chronological: %q >= %q", formattedEarlier, formattedLater)
	}
}

func TestMigrationFailureRollsBackSchemaAndTracking(t *testing.T) {
	ctx := context.Background()
	database, err := Open(ctx, filepath.Join(t.TempDir(), "rollback.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	if err := database.ensureMigrationTable(ctx); err != nil {
		t.Fatal(err)
	}
	err = database.applyMigration(ctx, migration{
		version: 99,
		name:    "0099_broken.sql",
		sql:     "CREATE TABLE migration_probe (id INTEGER PRIMARY KEY); INSERT INTO missing_table VALUES (1);",
	})
	if err == nil {
		t.Fatal("expected migration failure")
	}
	var tableCount int
	if err := database.db.QueryRowContext(ctx,
		"SELECT COUNT(*) FROM sqlite_master WHERE type = 'table' AND name = 'migration_probe'",
	).Scan(&tableCount); err != nil {
		t.Fatal(err)
	}
	if tableCount != 0 {
		t.Fatalf("failed migration left %d probe tables", tableCount)
	}
	var migrationCount int
	if err := database.db.QueryRowContext(ctx,
		"SELECT COUNT(*) FROM schema_migrations WHERE version = 99",
	).Scan(&migrationCount); err != nil {
		t.Fatal(err)
	}
	if migrationCount != 0 {
		t.Fatalf("failed migration left %d tracking rows", migrationCount)
	}
}

func TestMigrateUpgradesEverySupportedPrefix(t *testing.T) {
	migrations, err := loadMigrations()
	if err != nil {
		t.Fatal(err)
	}
	for prefix := 1; prefix < len(migrations); prefix++ {
		t.Run(fmt.Sprintf("from version %d", prefix), func(t *testing.T) {
			ctx := context.Background()
			database, err := Open(ctx, filepath.Join(t.TempDir(), "upgrade.db"))
			if err != nil {
				t.Fatal(err)
			}
			defer database.Close()
			if err := database.ensureMigrationTable(ctx); err != nil {
				t.Fatal(err)
			}
			for _, migration := range migrations[:prefix] {
				if err := database.applyMigration(ctx, migration); err != nil {
					t.Fatal(err)
				}
			}
			if err := database.Migrate(ctx); err != nil {
				t.Fatal(err)
			}
			var migrationCount int
			if err := database.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM schema_migrations").Scan(&migrationCount); err != nil {
				t.Fatal(err)
			}
			if migrationCount != len(migrations) {
				t.Fatalf("migration count = %d, want %d", migrationCount, len(migrations))
			}
			for _, table := range []string{"context_records", "activity", "actors", "external_actions", "authority_grants"} {
				var count int
				if err := database.db.QueryRowContext(ctx,
					"SELECT COUNT(*) FROM sqlite_master WHERE type = 'table' AND name = ?", table,
				).Scan(&count); err != nil {
					t.Fatal(err)
				}
				if count != 1 {
					t.Fatalf("table %s was not added", table)
				}
			}
		})
	}
}

func TestMigrationNinePreservesIdempotencyRecordsAndRemovesActorForeignKey(t *testing.T) {
	ctx := context.Background()
	migrations, err := loadMigrations()
	if err != nil {
		t.Fatal(err)
	}
	database, err := Open(ctx, filepath.Join(t.TempDir(), "idempotency-upgrade.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	if err := database.ensureMigrationTable(ctx); err != nil {
		t.Fatal(err)
	}
	for _, migration := range migrations[:8] {
		if err := database.applyMigration(ctx, migration); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := database.db.ExecContext(ctx, "INSERT INTO actors (id, kind, display_name, created_at) VALUES ('agent:legacy', 'agent', 'Legacy', '2026-08-23T00:00:00.000000000Z')"); err != nil {
		t.Fatal(err)
	}
	if _, err := database.db.ExecContext(ctx, "INSERT INTO idempotency_records (actor_id, key, operation, request_hash, response_json, created_at) VALUES ('agent:legacy', 'retry', 'create_objective', 'hash', '{\"result\":{}}', '2026-08-23T00:00:00.000000000Z')"); err != nil {
		t.Fatal(err)
	}
	if err := database.applyMigration(ctx, migrations[8]); err != nil {
		t.Fatal(err)
	}
	var operation, hash, response string
	if err := database.db.QueryRowContext(ctx, "SELECT operation, request_hash, response_json FROM idempotency_records WHERE actor_id = 'agent:legacy' AND key = 'retry'").Scan(&operation, &hash, &response); err != nil {
		t.Fatal(err)
	}
	if operation != "create_objective" || hash != "hash" || response != `{"result":{}}` {
		t.Fatalf("preserved idempotency record = %q, %q, %q", operation, hash, response)
	}
	if _, err := database.db.ExecContext(ctx, "INSERT INTO idempotency_records (actor_id, key, operation, request_hash, response_json, created_at) VALUES ('agent:unregistered', 'retry', 'create_objective', 'hash', '{\"result\":{}}', '2026-08-23T00:00:00.000000000Z')"); err != nil {
		t.Fatalf("unregistered actor idempotency record rejected: %v", err)
	}
}

func TestMigrateRejectsIncompatibleHistory(t *testing.T) {
	for _, test := range []struct {
		name        string
		version     int
		migration   string
		wantMessage string
	}{
		{name: "renamed applied migration", version: 1, migration: "0001_other.sql", wantMessage: "name mismatch"},
		{name: "migration from newer binary", version: 99, migration: "0099_future.sql", wantMessage: "not recognized"},
	} {
		t.Run(test.name, func(t *testing.T) {
			ctx := context.Background()
			database, err := Open(ctx, filepath.Join(t.TempDir(), "history.db"))
			if err != nil {
				t.Fatal(err)
			}
			defer database.Close()
			if err := database.ensureMigrationTable(ctx); err != nil {
				t.Fatal(err)
			}
			if _, err := database.db.ExecContext(ctx,
				"INSERT INTO schema_migrations(version, name, applied_at) VALUES (?, ?, ?)",
				test.version, test.migration, formatTime(time.Now()),
			); err != nil {
				t.Fatal(err)
			}

			err = database.Migrate(ctx)
			if err == nil || !strings.Contains(err.Error(), test.wantMessage) {
				t.Fatalf("Migrate() error = %v, want message containing %q", err, test.wantMessage)
			}
		})
	}
}
