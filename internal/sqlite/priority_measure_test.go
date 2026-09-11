package sqlite

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/dennisschroeder/throughline/internal/app"
	"github.com/dennisschroeder/throughline/internal/domain/work"
)

// TestObjectivePriorityAndAppetiteSurvivePatchAndReopen is REP-06's first
// criterion for objectives: both fields must persist through a patch and
// still read back correctly after the database is closed and reopened, not
// only within one open connection.
func TestObjectivePriorityAndAppetiteSurvivePatchAndReopen(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "priority.db")
	database, err := Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	if err := database.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	service := app.NewService(database.Store(), &testIDs{}, testClock{})
	if _, err := app.UnwrapMutation(service.RegisterActor(ctx, app.RegisterActorCommand{Actor: work.Actor{ID: "human:owner", Kind: work.ActorTypeHuman, DisplayName: "Owner"}, IdempotencyKey: "priority-owner"})); err != nil {
		t.Fatal(err)
	}
	objective, err := app.UnwrapMutation(service.CreateObjective(ctx, app.CreateObjectiveCommand{
		ActorID: "human:owner", IdempotencyKey: "priority-objective", Key: "OBJ-PRIORITY",
		Title: "Carries priority and appetite", DesiredOutcome: "Values survive.", Phase: work.ObjectivePlanning,
		Priority: work.PriorityHigh,
	}))
	if err != nil {
		t.Fatal(err)
	}
	if objective.Priority != work.PriorityHigh {
		t.Fatalf("priority at creation = %q, want high", objective.Priority)
	}

	appetite := work.Measure{Value: 200, Unit: "tokens", Basis: work.MeasureEstimated}
	patched, err := app.UnwrapMutation(service.PatchObjective(ctx, app.PatchObjectiveCommand{
		ObjectiveID: objective.ID, ActorID: "human:owner", IdempotencyKey: "priority-appetite",
		ExpectedVersion: objective.Version, Appetite: &appetite,
	}))
	if err != nil {
		t.Fatal(err)
	}
	if patched.Appetite != appetite {
		t.Fatalf("appetite after patch = %+v, want %+v", patched.Appetite, appetite)
	}

	// An unrelated patch must not disturb either field.
	title := "Renamed, priority and appetite unchanged"
	renamed, err := app.UnwrapMutation(service.PatchObjective(ctx, app.PatchObjectiveCommand{
		ObjectiveID: objective.ID, ActorID: "human:owner", IdempotencyKey: "priority-rename",
		ExpectedVersion: patched.Version, Title: &title,
	}))
	if err != nil {
		t.Fatal(err)
	}
	if renamed.Priority != work.PriorityHigh || renamed.Appetite != appetite {
		t.Fatalf("an unrelated patch changed priority/appetite: %+v", renamed)
	}

	if err := database.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	restarted, err := app.NewService(reopened.Store(), &testIDs{}, testClock{}).GetObjectiveContext(ctx, objective.ID)
	if err != nil {
		t.Fatal(err)
	}
	if restarted.Objective.Priority != work.PriorityHigh || restarted.Objective.Appetite != appetite {
		t.Fatalf("after reopening: priority=%q appetite=%+v, want high and %+v", restarted.Objective.Priority, restarted.Objective.Appetite, appetite)
	}
}

// TestWorkItemMeasureSurvivesPatchAndReopen mirrors the objective test for a
// work item's Measure, which sits beside the existing coarse EstimatedScope
// hint rather than replacing it.
func TestWorkItemMeasureSurvivesPatchAndReopen(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "measure.db")
	database, err := Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	if err := database.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	service := app.NewService(database.Store(), &testIDs{}, testClock{})
	if _, err := app.UnwrapMutation(service.RegisterActor(ctx, app.RegisterActorCommand{Actor: work.Actor{ID: "human:owner", Kind: work.ActorTypeHuman, DisplayName: "Owner"}, IdempotencyKey: "measure-owner"})); err != nil {
		t.Fatal(err)
	}
	objective, err := app.UnwrapMutation(service.CreateObjective(ctx, app.CreateObjectiveCommand{
		ActorID: "human:owner", IdempotencyKey: "measure-objective", Key: "OBJ-MEASURE",
		Title: "Carries a measured item", DesiredOutcome: "The measure survives.", Phase: work.ObjectivePlanning,
	}))
	if err != nil {
		t.Fatal(err)
	}
	item, err := app.UnwrapMutation(service.CreateWorkItem(ctx, app.CreateWorkItemCommand{
		ActorID: "human:owner", IdempotencyKey: "measure-item", Key: "TH-MEASURE", ObjectiveID: objective.ID,
		Title: "Carries the measure", Kind: "research", CommitmentState: work.ItemProposed, ExecutionStatus: work.StatusBacklog,
		Priority: work.PriorityMedium, EstimatedScope: work.ScopeLarge, ExecutionPolicy: work.PolicyAgentMayPropose,
		RequiredActorKind: work.ActorAny, AttentionState: work.AttentionNone,
	}))
	if err != nil {
		t.Fatal(err)
	}
	if item.EstimatedScope != work.ScopeLarge {
		t.Fatalf("estimated scope = %q, want the coarse hint to remain settable alongside measure", item.EstimatedScope)
	}

	measure := work.Measure{Value: 350000, Unit: "bytes", Basis: work.MeasureMeasured}
	patched, err := app.UnwrapMutation(service.PatchWorkItem(ctx, app.PatchWorkItemCommand{
		WorkItemID: item.ID, ActorID: "human:owner", IdempotencyKey: "measure-patch",
		ExpectedVersion: item.Version, Measure: &measure,
	}))
	if err != nil {
		t.Fatal(err)
	}
	if patched.Measure != measure {
		t.Fatalf("measure after patch = %+v, want %+v", patched.Measure, measure)
	}
	if patched.EstimatedScope != work.ScopeLarge {
		t.Fatal("patching measure changed the unrelated estimated scope")
	}

	if err := database.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	restarted, err := app.NewService(reopened.Store(), &testIDs{}, testClock{}).GetWorkItem(ctx, item.ID)
	if err != nil {
		t.Fatal(err)
	}
	if restarted.WorkItem.Measure != measure {
		t.Fatalf("after reopening, measure = %+v, want %+v", restarted.WorkItem.Measure, measure)
	}
}

// TestContextNonGoalAndAffectedTransitionThroughTheService is REP-06's second
// criterion exercised through the actual app.Service mutation an agent calls,
// not only through the domain functions in isolation.
func TestContextNonGoalAndAffectedTransitionThroughTheService(t *testing.T) {
	ctx := context.Background()
	database, err := Open(ctx, filepath.Join(t.TempDir(), "context-kinds.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	if err := database.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	service := app.NewService(database.Store(), &testIDs{}, testClock{})
	if _, err := app.UnwrapMutation(service.RegisterActor(ctx, app.RegisterActorCommand{Actor: work.Actor{ID: "human:owner", Kind: work.ActorTypeHuman, DisplayName: "Owner"}, IdempotencyKey: "kind-owner"})); err != nil {
		t.Fatal(err)
	}
	objective, err := app.UnwrapMutation(service.CreateObjective(ctx, app.CreateObjectiveCommand{
		ActorID: "human:owner", IdempotencyKey: "kind-objective", Key: "OBJ-KINDS",
		Title: "Carries the new context kinds", DesiredOutcome: "Both kinds transition correctly.", Phase: work.ObjectivePlanning,
	}))
	if err != nil {
		t.Fatal(err)
	}

	for _, kind := range []work.ContextKind{work.ContextNonGoal, work.ContextAffected} {
		created, err := app.UnwrapMutation(service.RecordContext(ctx, app.RecordContextCommand{
			ObjectiveID: objective.ID, ActorID: "human:owner", IdempotencyKey: "kind-record-" + string(kind),
			Kind: kind, Title: "Explicitly scoped", Status: work.ContextProposed,
		}))
		if err != nil {
			t.Fatalf("%s: record: %v", kind, err)
		}
		accepted, err := app.UnwrapMutation(service.TransitionContext(ctx, app.TransitionContextCommand{
			ContextRecordID: created.ID, ActorID: "human:owner", IdempotencyKey: "kind-accept-" + string(kind),
			ExpectedVersion: created.Version, TargetStatus: work.ContextAccepted,
		}))
		if err != nil {
			t.Fatalf("%s: proposed->accepted: %v", kind, err)
		}
		if accepted.Status != work.ContextAccepted {
			t.Fatalf("%s: status = %q, want accepted", kind, accepted.Status)
		}
	}
}

// TestMigration0013AppliesToAWorkspaceWithAnExistingContextSupersession is the
// upgrade fixture the storage layer needs: context_records.supersedes_id is
// not a column this migration adds, unlike migration 0012's, so a workspace
// that superseded a context record before upgrading already has rows
// referencing each other through it. Rebuilding the table by dropping the
// original enforces that self-referencing foreign key immediately unless
// enforcement is deferred to the commit, and the only way to catch a
// regression here is a fixture that seeds a real supersession before the
// migration runs — an empty table never exercises the constraint at all.
func TestMigration0013AppliesToAWorkspaceWithAnExistingContextSupersession(t *testing.T) {
	ctx := context.Background()
	migrations, err := loadMigrations()
	if err != nil {
		t.Fatal(err)
	}
	beforeContextKinds := -1
	for index, migration := range migrations {
		if migration.version == 13 {
			beforeContextKinds = index
		}
	}
	if beforeContextKinds < 0 {
		t.Fatal("the priority/measure/context-kinds migration is missing")
	}
	database, err := Open(ctx, filepath.Join(t.TempDir(), "supersession-upgrade.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	if err := database.ensureMigrationTable(ctx); err != nil {
		t.Fatal(err)
	}
	for _, migration := range migrations[:beforeContextKinds] {
		if err := database.applyMigration(ctx, migration); err != nil {
			t.Fatal(err)
		}
	}

	const (
		timestamp   = "2026-09-01T00:00:00.000000000Z"
		objectiveID = "legacy-objective"
		predecessor = "legacy-predecessor"
		replacement = "legacy-replacement"
	)
	for _, statement := range []string{
		`INSERT INTO actors (id, kind, display_name, created_at) VALUES ('human:legacy', 'human', 'Legacy owner', '` + timestamp + `')`,
		`INSERT INTO objectives (id, key, title, description, desired_outcome, phase, version, created_at, updated_at)
		 VALUES ('` + objectiveID + `', 'OBJ-LEGACY-SUPERSESSION', 'Predates priority/measure', '', 'A pre-existing supersession survives the upgrade.', 'planning', 1, '` + timestamp + `', '` + timestamp + `')`,
		`INSERT INTO context_records (id, objective_id, kind, title, status, version, created_at, updated_at, created_by)
		 VALUES ('` + predecessor + `', '` + objectiveID + `', 'requirement', 'The original requirement', 'superseded', 2, '` + timestamp + `', '` + timestamp + `', 'human:legacy')`,
		`INSERT INTO context_records (id, objective_id, kind, title, status, supersedes_id, version, created_at, updated_at, created_by)
		 VALUES ('` + replacement + `', '` + objectiveID + `', 'requirement', 'The corrected requirement', 'proposed', '` + predecessor + `', 1, '` + timestamp + `', '` + timestamp + `', 'human:legacy')`,
	} {
		if _, err := database.db.ExecContext(ctx, statement); err != nil {
			t.Fatalf("seed pre-upgrade row: %v\n%s", err, statement)
		}
	}

	if err := database.Migrate(ctx); err != nil {
		t.Fatalf("migrate over an existing context supersession: %v", err)
	}

	service := app.NewService(database.Store(), &testIDs{}, testClock{})
	objectiveContext, err := service.GetObjectiveContext(ctx, objectiveID)
	if err != nil {
		t.Fatal(err)
	}
	var found int
	for _, record := range objectiveContext.ContextRecords {
		if record.ID == predecessor && record.Status == work.ContextSuperseded {
			found++
		}
		if record.ID == replacement && record.SupersedesID == predecessor {
			found++
		}
	}
	if found != 2 {
		t.Fatalf("supersession pair after migrating = %#v, want both rows intact and linked", objectiveContext.ContextRecords)
	}
}

// TestWorkItemMeasureDistinguishesGenuineZeroFromUnset exercises the same
// zero-value convention end to end: a Measure whose Value is a real zero,
// not the unset sentinel, must round-trip through creation, storage and a
// database reopen without being silently collapsed into "no measure".
func TestWorkItemMeasureDistinguishesGenuineZeroFromUnset(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "zero-measure.db")
	database, err := Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	if err := database.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	service := app.NewService(database.Store(), &testIDs{}, testClock{})
	if _, err := app.UnwrapMutation(service.RegisterActor(ctx, app.RegisterActorCommand{Actor: work.Actor{ID: "human:owner", Kind: work.ActorTypeHuman, DisplayName: "Owner"}, IdempotencyKey: "zero-owner"})); err != nil {
		t.Fatal(err)
	}
	objective, err := app.UnwrapMutation(service.CreateObjective(ctx, app.CreateObjectiveCommand{
		ActorID: "human:owner", IdempotencyKey: "zero-objective", Key: "OBJ-ZERO",
		Title: "Carries a genuinely zero measure", DesiredOutcome: "The zero survives.", Phase: work.ObjectivePlanning,
	}))
	if err != nil {
		t.Fatal(err)
	}
	zero := work.Measure{Value: 0, Unit: "defects", Basis: work.MeasureMeasured}
	item, err := app.UnwrapMutation(service.CreateWorkItem(ctx, app.CreateWorkItemCommand{
		ActorID: "human:owner", IdempotencyKey: "zero-item", Key: "TH-ZERO", ObjectiveID: objective.ID,
		Title: "Zero defects found", Kind: "research", CommitmentState: work.ItemProposed, ExecutionStatus: work.StatusBacklog,
		Priority: work.PriorityMedium, EstimatedScope: work.ScopeSmall, ExecutionPolicy: work.PolicyAgentMayPropose,
		RequiredActorKind: work.ActorAny, AttentionState: work.AttentionNone, Measure: zero,
	}))
	if err != nil {
		t.Fatal(err)
	}
	if item.Measure != zero {
		t.Fatalf("measure at creation = %+v, want the genuine zero %+v", item.Measure, zero)
	}

	if err := database.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	restarted, err := app.NewService(reopened.Store(), &testIDs{}, testClock{}).GetWorkItem(ctx, item.ID)
	if err != nil {
		t.Fatal(err)
	}
	if restarted.WorkItem.Measure != zero {
		t.Fatalf("after reopening, measure = %+v, want the genuine zero %+v, not collapsed to unset", restarted.WorkItem.Measure, zero)
	}
}

// TestMigration0013PreservesTheOriginalContextRecordsConstraintsAndIndex
// pins the three properties the rebuild claims to carry across unchanged:
// the pre-existing index, and the version CHECK. A regression in any of
// these previously passed the whole suite silently.
func TestMigration0013PreservesTheOriginalContextRecordsConstraintsAndIndex(t *testing.T) {
	ctx := context.Background()
	database, err := Open(ctx, filepath.Join(t.TempDir(), "rebuild-constraints.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	if err := database.Migrate(ctx); err != nil {
		t.Fatal(err)
	}

	var indexCount int
	if err := database.db.QueryRowContext(ctx,
		"SELECT count(*) FROM sqlite_master WHERE type = 'index' AND name = 'context_by_objective_kind' AND tbl_name = 'context_records'",
	).Scan(&indexCount); err != nil {
		t.Fatal(err)
	}
	if indexCount != 1 {
		t.Fatal("context_by_objective_kind index is missing after the migration 0013 rebuild")
	}

	service := app.NewService(database.Store(), &testIDs{}, testClock{})
	if _, err := app.UnwrapMutation(service.RegisterActor(ctx, app.RegisterActorCommand{Actor: work.Actor{ID: "human:owner", Kind: work.ActorTypeHuman, DisplayName: "Owner"}, IdempotencyKey: "rebuild-owner"})); err != nil {
		t.Fatal(err)
	}
	objective, err := app.UnwrapMutation(service.CreateObjective(ctx, app.CreateObjectiveCommand{
		ActorID: "human:owner", IdempotencyKey: "rebuild-objective", Key: "OBJ-REBUILD",
		Title: "Checks the rebuilt table's constraints", DesiredOutcome: "Both constraints still hold.", Phase: work.ObjectivePlanning,
	}))
	if err != nil {
		t.Fatal(err)
	}

	// The version CHECK (version > 0) must still reject a non-positive version.
	if _, err := database.db.ExecContext(ctx,
		`INSERT INTO context_records (id, objective_id, kind, title, status, version, created_at, updated_at, created_by)
		 VALUES ('bad-version', ?, 'requirement', 'Invalid version', 'proposed', 0, '2026-09-11T00:00:00Z', '2026-09-11T00:00:00Z', 'human:owner')`,
		objective.ID); err == nil {
		t.Fatal("a context_records row with version 0 was accepted, want the CHECK to reject it")
	}

	// supersedes_id must still enforce referential integrity (NO ACTION still
	// checks, it just no longer blocks a direct delete — nothing in this
	// codebase performs one).
	if _, err := database.db.ExecContext(ctx,
		`INSERT INTO context_records (id, objective_id, kind, title, status, supersedes_id, version, created_at, updated_at, created_by)
		 VALUES ('dangling', ?, 'requirement', 'Points nowhere', 'proposed', 'does-not-exist', 1, '2026-09-11T00:00:00Z', '2026-09-11T00:00:00Z', 'human:owner')`,
		objective.ID); err == nil {
		t.Fatal("a context_records row superseding a nonexistent id was accepted")
	}
}
