package cli

import (
	"bytes"
	"context"
	"database/sql"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/dennisschroeder/throughline/internal/app"
	"github.com/dennisschroeder/throughline/internal/config"
	"github.com/dennisschroeder/throughline/internal/domain/work"
	throughlinesqlite "github.com/dennisschroeder/throughline/internal/sqlite"
	_ "modernc.org/sqlite"
)

func initWorkspaceWithActors(t *testing.T) (string, config.Workspace) {
	t.Helper()
	withTestRegistry(t)
	root := t.TempDir()
	var stdout, stderr bytes.Buffer
	if code := Run(context.Background(), []string{"init", root}, &stdout, &stderr); code != 0 {
		t.Fatalf("init exited %d: %s", code, stderr.String())
	}
	workspace, err := config.Load(root)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	database, err := throughlinesqlite.Open(ctx, workspace.DatabasePath)
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	service := app.NewService(database.Store(), app.UUIDv7Generator{}, app.SystemClock{})
	for _, actor := range []work.Actor{
		{ID: "human:dennis", Kind: work.ActorTypeHuman, DisplayName: "Dennis"},
		{ID: "agent:worker", Kind: work.ActorTypeAgent, DisplayName: "Worker"},
		{ID: "service:ci", Kind: work.ActorTypeService, DisplayName: "CI"},
	} {
		if _, err := service.RegisterActor(ctx, app.RegisterActorCommand{Actor: actor, IdempotencyKey: "register-" + actor.ID}); err != nil {
			t.Fatal(err)
		}
	}
	return root, workspace
}

func capabilityHeld(t *testing.T, workspace config.Workspace, actorID, slug string) bool {
	t.Helper()
	db, err := sql.Open("sqlite", workspace.DatabasePath)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var count int
	if err := db.QueryRow("SELECT COUNT(*) FROM actor_capabilities WHERE actor_id = ? AND capability_slug = ?", actorID, slug).Scan(&count); err != nil {
		t.Fatal(err)
	}
	return count == 1
}

// TestCapabilityGrantRequiresARegisteredHuman is REP-11's first criterion:
// an agent must not be able to grant itself what a claim requires.
func TestCapabilityGrantRequiresARegisteredHuman(t *testing.T) {
	root, workspace := initWorkspaceWithActors(t)
	grant := func(granter string) (int, string, string) {
		var stdout, stderr bytes.Buffer
		code := Run(context.Background(), []string{"capability", "grant", "--actor", "agent:worker", "--capability", "web_research", "--as", granter, root}, &stdout, &stderr)
		return code, stdout.String(), stderr.String()
	}
	if code, _, stderr := grant("agent:worker"); code == 0 || !strings.Contains(stderr, "registered human") {
		t.Fatalf("agent granting itself = exit %d, %q; want a refusal naming the human requirement", code, stderr)
	}
	if code, _, stderr := grant("service:ci"); code == 0 || !strings.Contains(stderr, "registered human") {
		t.Fatalf("service granting = exit %d, %q; want a refusal naming the human requirement", code, stderr)
	}
	if code, _, _ := grant("human:nobody"); code == 0 {
		t.Fatal("an unregistered granter was accepted")
	}
	if capabilityHeld(t, workspace, "agent:worker", "web_research") {
		t.Fatal("a refused grant assigned the capability")
	}
	code, stdout, stderr := grant("human:dennis")
	if code != 0 || !strings.Contains(stdout, "granted capability web_research to agent:worker as human:dennis") {
		t.Fatalf("human grant = exit %d, stdout %q, stderr %q", code, stdout, stderr)
	}
	if !capabilityHeld(t, workspace, "agent:worker", "web_research") {
		t.Fatal("the human grant did not assign the capability")
	}
}

// TestCapabilityGrantNeverMigratesAMismatchedSchema is REP-11's second
// criterion: only the daemon migrates. A CLI older or newer than the database
// fails, names the update-and-restart remediation, and leaves the schema as
// it found it.
func TestCapabilityGrantNeverMigratesAMismatchedSchema(t *testing.T) {
	root, workspace := initWorkspaceWithActors(t)
	db, err := sql.Open("sqlite", workspace.DatabasePath)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var latest int
	if err := db.QueryRow("SELECT MAX(version) FROM schema_migrations").Scan(&latest); err != nil {
		t.Fatal(err)
	}
	var latestName string
	if err := db.QueryRow("SELECT name FROM schema_migrations WHERE version = ?", latest).Scan(&latestName); err != nil {
		t.Fatal(err)
	}
	count := func() int {
		t.Helper()
		var n int
		if err := db.QueryRow("SELECT COUNT(*) FROM schema_migrations").Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}
	grant := func() (int, string) {
		var stdout, stderr bytes.Buffer
		code := Run(context.Background(), []string{"capability", "grant", "--actor", "agent:worker", "--capability", "web_research", "--as", "human:dennis", root}, &stdout, &stderr)
		return code, stderr.String()
	}

	// The database looks one migration behind this binary: the daemon has not
	// been updated or restarted.
	if _, err := db.Exec("DELETE FROM schema_migrations WHERE version = ?", latest); err != nil {
		t.Fatal(err)
	}
	before := count()
	wantOlder := fmt.Sprintf("database is at migration %d, this binary carries %d", latest-1, latest)
	if code, stderr := grant(); code == 0 || !strings.Contains(stderr, wantOlder) || !strings.Contains(stderr, "throughline daemon restart") || !strings.Contains(stderr, "open the workspace once through the daemon") {
		t.Fatalf("grant against an older schema = exit %d, %q; want %q and the restart-then-open remediation", code, stderr, wantOlder)
	}
	if count() != before {
		t.Fatal("the CLI migrated the workspace")
	}

	// The database is ahead of this binary: the CLI itself is out of date.
	if _, err := db.Exec("INSERT INTO schema_migrations (version, name, applied_at) VALUES (?, ?, '2026-09-15T00:00:00Z')", latest, latestName); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec("INSERT INTO schema_migrations (version, name, applied_at) VALUES (?, 'future.sql', '2026-09-15T00:00:00Z')", latest+1); err != nil {
		t.Fatal(err)
	}
	if code, stderr := grant(); code == 0 || !strings.Contains(stderr, fmt.Sprintf("applied migration %d (future.sql) is not recognized", latest+1)) || !strings.Contains(stderr, "same throughline release") {
		t.Fatalf("grant against a newer schema = exit %d, %q", code, stderr)
	}

	// Same length, different history: a renamed migration is a different
	// release, not an unmigrated one.
	if _, err := db.Exec("DELETE FROM schema_migrations WHERE version = ?", latest+1); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec("UPDATE schema_migrations SET name = 'renamed.sql' WHERE version = ?", latest); err != nil {
		t.Fatal(err)
	}
	if code, stderr := grant(); code == 0 || !strings.Contains(stderr, "name mismatch") {
		t.Fatalf("grant against a renamed migration = exit %d, %q", code, stderr)
	}
	if capabilityHeld(t, workspace, "agent:worker", "web_research") {
		t.Fatal("a grant against a mismatched schema assigned the capability")
	}
}

// TestCapabilityGrantRefusesAMissingDatabaseWithoutCreatingOne keeps a wrong
// or unmounted database path from turning into an empty database that an
// upgraded daemon would then migrate, hiding the missing data.
func TestCapabilityGrantRefusesAMissingDatabaseWithoutCreatingOne(t *testing.T) {
	root, workspace := initWorkspaceWithActors(t)
	for _, suffix := range []string{"", "-wal", "-shm"} {
		if err := os.Remove(workspace.DatabasePath + suffix); err != nil && !os.IsNotExist(err) {
			t.Fatal(err)
		}
	}
	var stdout, stderr bytes.Buffer
	if code := Run(context.Background(), []string{"capability", "grant", "--actor", "agent:worker", "--capability", "web_research", "--as", "human:dennis", root}, &stdout, &stderr); code == 0 || !strings.Contains(stderr.String(), "is not readable") {
		t.Fatalf("grant with a missing database = exit %d, %q", code, stderr.String())
	}
	if _, err := os.Stat(workspace.DatabasePath); !os.IsNotExist(err) {
		t.Fatalf("the grant created a database at %s: %v", workspace.DatabasePath, err)
	}
	stderr.Reset()
	if code := Run(context.Background(), []string{"capability", "grant", "--actor", "agent:worker", "--capability", "web_research", "--as", "human:dennis", root, "extra"}, &stdout, &stderr); code == 0 || !strings.Contains(stderr.String(), "at most one workspace directory") {
		t.Fatalf("grant with two directories = exit %d, %q", code, stderr.String())
	}
}

// TestCapabilityGrantWaitsOutAWriterHoldingTheLock covers the daemon writing
// while a person grants: the grant retries instead of failing at once.
func TestCapabilityGrantWaitsOutAWriterHoldingTheLock(t *testing.T) {
	root, workspace := initWorkspaceWithActors(t)
	holder, err := sql.Open("sqlite", workspace.DatabasePath)
	if err != nil {
		t.Fatal(err)
	}
	defer holder.Close()
	holder.SetMaxOpenConns(1)
	transaction, err := holder.Begin()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := transaction.Exec("UPDATE actors SET display_name = display_name WHERE id = 'human:dennis'"); err != nil {
		t.Fatal(err)
	}
	released := make(chan struct{})
	go func() {
		time.Sleep(400 * time.Millisecond)
		_ = transaction.Commit()
		close(released)
	}()
	var stdout, stderr bytes.Buffer
	code := Run(context.Background(), []string{"capability", "grant", "--actor", "agent:worker", "--capability", "web_research", "--as", "human:dennis", root}, &stdout, &stderr)
	<-released
	if code != 0 || !capabilityHeld(t, workspace, "agent:worker", "web_research") {
		t.Fatalf("grant while another writer held the lock = exit %d, %q", code, stderr.String())
	}
}
