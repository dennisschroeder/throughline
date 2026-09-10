package sqlite

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/dennisschroeder/throughline/internal/app"
	"github.com/dennisschroeder/throughline/internal/domain/work"
)

// TestEffectsWorkOnADatabaseUpgradedWithDataPresent is the upgrade fixture the
// third acceptance criterion asks for. Migration 0011 adds the version columns
// the collector reads to tables that already hold rows, so the interesting case
// is not an empty database but one whose existing rows must be backfilled to a
// version the collector accepts and can report.
//
// The pre-upgrade rows are written with the SQL of the schema as it stood then,
// the way the older binary wrote them; the current Go code cannot address that
// schema at all, which is exactly what makes this a fixture rather than a
// normal test.
func TestEffectsWorkOnADatabaseUpgradedWithDataPresent(t *testing.T) {
	ctx := context.Background()
	migrations, err := loadMigrations()
	if err != nil {
		t.Fatal(err)
	}
	// The fixture has to stop short of the migration that adds the version
	// columns, whatever else has been added since, because rows written before
	// those columns existed are the whole point of it.
	relationVersions := -1
	for index, migration := range migrations {
		if migration.version == 11 {
			relationVersions = index
		}
	}
	if relationVersions < 0 {
		t.Fatal("the relation-version migration is missing")
	}
	database, err := Open(ctx, filepath.Join(t.TempDir(), "upgraded.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	if err := database.ensureMigrationTable(ctx); err != nil {
		t.Fatal(err)
	}
	for _, migration := range migrations[:relationVersions] {
		if err := database.applyMigration(ctx, migration); err != nil {
			t.Fatal(err)
		}
	}

	const (
		timestamp   = "2026-08-23T00:00:00.000000000Z"
		objectiveID = "legacy-objective"
		itemID      = "legacy-item"
		criterionID = "legacy-criterion"
	)
	for _, statement := range []string{
		`INSERT INTO actors (id, kind, display_name, created_at) VALUES ('human:legacy', 'human', 'Legacy owner', '` + timestamp + `')`,
		`INSERT INTO objectives (id, key, title, description, desired_outcome, phase, version, created_at, updated_at)
		 VALUES ('` + objectiveID + `', 'OBJ-UPGRADE', 'Predates the version columns', '', 'Existing rows survive the upgrade.', 'planning', 1, '` + timestamp + `', '` + timestamp + `')`,
		`INSERT INTO work_items (id, key, objective_id, title, description, kind, commitment_state, execution_status, priority, estimated_scope, execution_policy, required_actor_kind, attention_state, version, created_at, updated_at)
		 VALUES ('` + itemID + `', 'TH-UPGRADE', '` + objectiveID + `', 'Carries collateral rows', '', 'research', 'proposed', 'backlog', 'medium', 'small', 'agent_may_propose', 'any', 'none', 1, '` + timestamp + `', '` + timestamp + `')`,
		`INSERT INTO acceptance_criteria (id, work_item_id, ordinal, text, required, status) VALUES ('` + criterionID + `', '` + itemID + `', 1, 'Recorded before the upgrade.', 1, 'pending')`,
		`INSERT INTO capabilities (slug, description) VALUES ('legacy_capability', 'Assigned before the upgrade.')`,
		`INSERT INTO work_item_capabilities (work_item_id, capability_slug) VALUES ('` + itemID + `', 'legacy_capability')`,
	} {
		if _, err := database.db.ExecContext(ctx, statement); err != nil {
			t.Fatalf("seed pre-upgrade row: %v\n%s", err, statement)
		}
	}

	if err := database.Migrate(ctx); err != nil {
		t.Fatal(err)
	}

	service := app.NewService(database.Store(), &testIDs{}, testClock{})
	itemContext, err := service.GetWorkItem(ctx, itemID)
	if err != nil {
		t.Fatal(err)
	}
	if len(itemContext.AcceptanceCriteria) != 1 || itemContext.AcceptanceCriteria[0].Version < 1 {
		t.Fatalf("backfilled acceptance criterion = %#v", itemContext.AcceptanceCriteria)
	}

	// Touching a row that predates the version columns must report it at the
	// version the upgrade gave it, not at zero.
	resolved, err := service.ResolveAcceptanceCriterion(ctx, app.ResolveAcceptanceCriterionCommand{
		CriterionID: criterionID, Status: work.AcceptanceSatisfied, ActorID: "human:legacy",
		Rationale: "Satisfied after the upgrade.", ExpectedWorkItemVersion: 1, IdempotencyKey: "upgrade-resolve",
	})
	if err != nil {
		t.Fatal(err)
	}
	assertEffects(t, resolved.Effects, map[string]int{
		"acceptance_criterion:" + criterionID: itemContext.AcceptanceCriteria[0].Version + 1,
		"work_item:" + itemID:                 2,
	})

	// A pre-upgrade relationship row is reportable too, including when it is removed.
	kept := []string{"replacement_capability"}
	patched, err := service.PatchWorkItem(ctx, app.PatchWorkItemCommand{
		WorkItemID: itemID, ActorID: "human:legacy", IdempotencyKey: "upgrade-patch",
		ExpectedVersion: 2, RequiredCapabilities: &kept,
	})
	if err != nil {
		t.Fatal(err)
	}
	assertHasEffect(t, patched.Effects, "work_item_capability", itemID+":legacy_capability")
	assertHasEffect(t, patched.Effects, "work_item_capability", itemID+":replacement_capability")
}
