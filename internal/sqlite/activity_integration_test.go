package sqlite

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/dennisschroeder/workgraph/internal/app"
	"github.com/dennisschroeder/workgraph/internal/domain/work"
)

func TestMutationRollsBackWhenActivityCannotBeRecorded(t *testing.T) {
	ctx := context.Background()
	database, err := Open(ctx, filepath.Join(t.TempDir(), "activity.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	if err := database.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := database.db.ExecContext(ctx, `
INSERT INTO activity
  (id, entity_kind, entity_id, actor_id, event_type, summary, payload_json, created_at)
VALUES ('duplicate-activity', 'test', 'test', 'test:actor', 'test.seeded', 'Seed collision.', '{}', '2026-08-21T00:00:00.000000000Z')`); err != nil {
		t.Fatal(err)
	}
	service := app.NewService(database.Store(), &orderedIDs{values: []string{"objective-new", "duplicate-activity"}}, staticClock{})
	if _, err := service.CreateObjective(ctx, app.CreateObjectiveCommand{
		ActorID: "human:owner", IdempotencyKey: "create-objective-rollback", Key: "OBJ-ROLLBACK", Title: "Verify atomic activity", Phase: work.ObjectivePlanning,
	}); err == nil {
		t.Fatal("expected duplicate activity id to fail the mutation")
	}
	var count int
	if err := database.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM objectives WHERE key = 'OBJ-ROLLBACK'").Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatal("objective persisted without its required activity")
	}
}

type orderedIDs struct {
	values []string
	next   int
}

func (ids *orderedIDs) New() (string, error) {
	value := ids.values[ids.next]
	ids.next++
	return value, nil
}

type staticClock struct{}

func (staticClock) Now() time.Time {
	return time.Date(2026, 8, 21, 20, 0, 0, 0, time.UTC)
}
