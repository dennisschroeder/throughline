package sqlite

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"

	"github.com/dennisschroeder/throughline/internal/app"
	"github.com/dennisschroeder/throughline/internal/domain/work"
)

// TestObjectivePhaseTransitionReasonSurvivesRestart is REP-10's first
// criterion: the reason and actor a transition requires used to be discarded.
// The latest transition now survives a reopen on the objective itself, every
// transition carries its reason in the change feed, a refused transition
// records neither, and an unrelated patch keeps the transition intact.
func TestObjectivePhaseTransitionReasonSurvivesRestart(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "objective-transition.db")
	database, err := Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	if err := database.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	service := app.NewService(database.Store(), &testIDs{}, testClock{})
	objective, err := app.UnwrapMutation(service.CreateObjective(ctx, app.CreateObjectiveCommand{
		ActorID: "human:owner", IdempotencyKey: "objective", Key: "OBJ-WHY", Title: "Explains its phase", DesiredOutcome: "The reason is kept.", Phase: work.ObjectiveIdea,
	}))
	if err != nil {
		t.Fatal(err)
	}
	if objective.LastPhaseTransition != nil {
		t.Fatalf("new objective carries a transition: %#v", objective.LastPhaseTransition)
	}
	discovery, err := app.UnwrapMutation(service.TransitionObjective(ctx, app.TransitionObjectiveCommand{
		ObjectiveID: objective.ID, TargetPhase: work.ObjectiveDiscovery, ActorID: "human:owner", Reason: "The idea survived the first exchange.", ExpectedVersion: objective.Version, IdempotencyKey: "to-discovery",
	}))
	if err != nil {
		t.Fatal(err)
	}
	planning, err := app.UnwrapMutation(service.TransitionObjective(ctx, app.TransitionObjectiveCommand{
		ObjectiveID: objective.ID, TargetPhase: work.ObjectivePlanning, ActorID: "agent:planner", Reason: "No open question changes the plan's shape.", ExpectedVersion: discovery.Version, IdempotencyKey: "to-planning",
	}))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.TransitionObjective(ctx, app.TransitionObjectiveCommand{
		ObjectiveID: objective.ID, TargetPhase: work.ObjectiveExecution, ActorID: "agent:planner", Reason: "Premature.", ExpectedVersion: planning.Version, IdempotencyKey: "to-execution-refused",
	}); err == nil {
		t.Fatal("entering execution without an approved plan was accepted")
	}
	title := "Still explains its phase"
	if _, err := service.PatchObjective(ctx, app.PatchObjectiveCommand{ObjectiveID: objective.ID, ActorID: "human:owner", IdempotencyKey: "rename", ExpectedVersion: planning.Version, Title: &title}); err != nil {
		t.Fatal(err)
	}
	if err := database.Close(); err != nil {
		t.Fatal(err)
	}

	reopened, err := Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	if err := reopened.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	service = app.NewService(reopened.Store(), &testIDs{}, testClock{})
	objectiveContext, err := service.GetObjectiveContext(ctx, objective.ID)
	if err != nil {
		t.Fatal(err)
	}
	transition := objectiveContext.Objective.LastPhaseTransition
	if transition == nil || transition.From != work.ObjectiveDiscovery || transition.To != work.ObjectivePlanning ||
		transition.Reason != "No open question changes the plan's shape." || transition.ActorID != "agent:planner" || !transition.At.Equal(testClock{}.Now().UTC()) {
		t.Fatalf("latest transition after reopening = %#v", transition)
	}
	if objectiveContext.Objective.Phase != work.ObjectivePlanning || objectiveContext.Objective.Title != title {
		t.Fatalf("objective after reopening = %#v", objectiveContext.Objective)
	}

	feed, err := service.ListActivity(ctx, app.ActivityFilter{ObjectiveID: objective.ID, Limit: 100})
	if err != nil {
		t.Fatal(err)
	}
	var history []map[string]string
	for _, change := range feed {
		if change.EventType != "objective.phase_changed" {
			continue
		}
		var payload map[string]string
		if err := json.Unmarshal(change.PayloadJSON, &payload); err != nil {
			t.Fatal(err)
		}
		payload["actor"] = change.ActorID
		history = append(history, payload)
	}
	if len(history) != 2 ||
		history[0]["from"] != "idea" || history[0]["to"] != "discovery" || history[0]["reason"] != "The idea survived the first exchange." || history[0]["actor"] != "human:owner" ||
		history[1]["from"] != "discovery" || history[1]["to"] != "planning" || history[1]["actor"] != "agent:planner" {
		t.Fatalf("phase history in the feed = %#v, want both transitions and not the refused one", history)
	}
}

// TestRecordingWorkNeverAdvancesAnObjective is REP-10's second criterion: the
// automatic idea-to-discovery advance was dropped, so questions, decisions and
// context recorded against an objective in idea leave its phase alone.
func TestRecordingWorkNeverAdvancesAnObjective(t *testing.T) {
	ctx := context.Background()
	database, err := Open(ctx, filepath.Join(t.TempDir(), "no-advance.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	if err := database.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	service := app.NewService(database.Store(), &testIDs{}, testClock{})
	objective, err := app.UnwrapMutation(service.CreateObjective(ctx, app.CreateObjectiveCommand{
		ActorID: "human:owner", IdempotencyKey: "objective", Key: "OBJ-IDLE", Title: "Stays an idea", DesiredOutcome: "Phase moves only on purpose.", Phase: work.ObjectiveIdea,
	}))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.AskQuestion(ctx, app.AskQuestionCommand{ObjectiveID: objective.ID, ActorID: "human:owner", Question: "Is this worth doing?", IdempotencyKey: "question"}); err != nil {
		t.Fatal(err)
	}
	if _, err := service.RecordDecision(ctx, app.RecordDecisionCommand{ObjectiveID: objective.ID, ActorID: "human:owner", Title: "Explore", Decision: "Look into it.", IdempotencyKey: "decision"}); err != nil {
		t.Fatal(err)
	}
	if _, err := service.RecordContext(ctx, app.RecordContextCommand{ObjectiveID: objective.ID, ActorID: "human:owner", Kind: work.ContextRequirement, Status: work.ContextProposed, Title: "Keep it small", IdempotencyKey: "context"}); err != nil {
		t.Fatal(err)
	}
	objectiveContext, err := service.GetObjectiveContext(ctx, objective.ID)
	if err != nil {
		t.Fatal(err)
	}
	if objectiveContext.Objective.Phase != work.ObjectiveIdea || objectiveContext.Objective.Version != objective.Version || objectiveContext.Objective.LastPhaseTransition != nil {
		t.Fatalf("objective after recording work = %#v, want it untouched in idea", objectiveContext.Objective)
	}
}

// TestMigration0017LeavesEarlierObjectivesWithoutATransition keeps an
// objective transitioned before reasons were stored without an invented one.
func TestMigration0017LeavesEarlierObjectivesWithoutATransition(t *testing.T) {
	ctx := context.Background()
	migrations, err := loadMigrations()
	if err != nil {
		t.Fatal(err)
	}
	before := -1
	for index, migration := range migrations {
		if migration.version == 17 {
			before = index
		}
	}
	if before < 0 {
		t.Fatal("the objective phase transition migration is missing")
	}
	database, err := Open(ctx, filepath.Join(t.TempDir(), "transition-upgrade.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	if err := database.ensureMigrationTable(ctx); err != nil {
		t.Fatal(err)
	}
	for _, migration := range migrations[:before] {
		if err := database.applyMigration(ctx, migration); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := database.db.ExecContext(ctx, `INSERT INTO objectives (id, key, title, description, desired_outcome, phase, updated_by, version, created_at, updated_at)
		VALUES ('legacy', 'OBJ-LEGACY', 'Legacy', '', 'Transitioned before reasons were kept.', 'planning', 'human:owner', 3, '`+questionFixtureTime+`', '`+questionFixtureTime+`')`); err != nil {
		t.Fatal(err)
	}
	if err := database.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	service := app.NewService(database.Store(), &testIDs{}, testClock{})
	objectiveContext, err := service.GetObjectiveContext(ctx, "legacy")
	if err != nil {
		t.Fatal(err)
	}
	if objectiveContext.Objective.LastPhaseTransition != nil || objectiveContext.Objective.Phase != work.ObjectivePlanning || objectiveContext.Objective.Version != 3 {
		t.Fatalf("legacy objective after migrating = %#v", objectiveContext.Objective)
	}
	resumed, err := app.UnwrapMutation(service.TransitionObjective(ctx, app.TransitionObjectiveCommand{ObjectiveID: "legacy", TargetPhase: work.ObjectivePaused, ActorID: "human:owner", Reason: "Waiting on budget.", ExpectedVersion: 3, IdempotencyKey: "pause"}))
	if err != nil {
		t.Fatal(err)
	}
	if resumed.LastPhaseTransition == nil || resumed.LastPhaseTransition.From != work.ObjectivePlanning || resumed.LastPhaseTransition.Reason != "Waiting on budget." {
		t.Fatalf("first transition after migrating = %#v", resumed.LastPhaseTransition)
	}
}
