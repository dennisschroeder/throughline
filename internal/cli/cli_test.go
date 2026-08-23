package cli

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/dennisschroeder/throughline/internal/app"
	"github.com/dennisschroeder/throughline/internal/config"
	"github.com/dennisschroeder/throughline/internal/domain/work"
	throughlinesqlite "github.com/dennisschroeder/throughline/internal/sqlite"
)

func TestInitCanRunTwice(t *testing.T) {
	root := t.TempDir()
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	if code := Run(context.Background(), []string{"init", root}, &stdout, &stderr); code != 0 {
		t.Fatalf("first init exited %d: %s", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "initialized Throughline workspace") {
		t.Fatalf("first init output = %q", stdout.String())
	}
	stdout.Reset()
	stderr.Reset()
	if code := Run(context.Background(), []string{"init", root}, &stdout, &stderr); code != 0 {
		t.Fatalf("second init exited %d: %s", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "reopened Throughline workspace") {
		t.Fatalf("second init output = %q", stdout.String())
	}

	for _, path := range []string{
		filepath.Join(root, config.DirectoryName, config.ConfigFileName),
		filepath.Join(root, config.DirectoryName, config.DefaultDatabasePath),
	} {
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("expected initialized file %q: %v", path, err)
		}
	}
}

func TestReadyAndShowInspectExecutionGraph(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	if code := Run(ctx, []string{"init", root}, &stdout, &stderr); code != 0 {
		t.Fatalf("init exited %d: %s", code, stderr.String())
	}
	workspace, err := config.Load(root)
	if err != nil {
		t.Fatal(err)
	}
	database, err := throughlinesqlite.Open(ctx, workspace.DatabasePath)
	if err != nil {
		t.Fatal(err)
	}
	service := app.NewService(database.Store(), &cliIDs{}, cliClock{})
	if _, err := service.RegisterActor(ctx, app.RegisterActorCommand{Actor: work.Actor{ID: "human:reviewer", Kind: work.ActorTypeHuman, DisplayName: "Reviewer"}, IdempotencyKey: "register-reviewer"}); err != nil {
		t.Fatal(err)
	}
	objective, err := service.CreateObjective(ctx, app.CreateObjectiveCommand{
		ActorID: "human:owner", IdempotencyKey: "create-objective-cli",
		Key: "OBJ-CLI", Title: "Inspect research work", DesiredOutcome: "Ready work is visible.", Phase: work.ObjectivePlanning,
	})
	if err != nil {
		t.Fatal(err)
	}
	plan, err := service.ProposePlan(ctx, app.ProposePlanCommand{
		ObjectiveID: objective.ID, ActorID: "agent:planner", IdempotencyKey: "propose-plan-cli", Title: "Research plan", Revision: 1,
		Items: []app.ProposedWorkItem{{
			ClientRef: "research", Key: "TH-CLI", Title: "Prepare the research dossier", Kind: "research",
			Priority: work.PriorityHigh, EstimatedScope: work.ScopeSmall, ExecutionPolicy: work.PolicyAgentMayPropose, RequiredActorKind: work.ActorAgent,
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.ReviewPlan(ctx, app.ReviewPlanCommand{PlanID: plan.Plan.ID, ReviewerActorID: "human:reviewer", IdempotencyKey: "review-plan-cli", Decision: work.PlanApproved, Reason: "Ready for execution.", ExpectedVersion: 1}); err != nil {
		t.Fatal(err)
	}
	if _, err := service.TransitionObjective(ctx, app.TransitionObjectiveCommand{ObjectiveID: objective.ID, TargetPhase: work.ObjectiveExecution, ActorID: "human:reviewer", IdempotencyKey: "transition-objective-cli", Reason: "Begin work.", ExpectedVersion: 1}); err != nil {
		t.Fatal(err)
	}
	itemContext, err := service.GetWorkItem(ctx, plan.Items[0].WorkItem.ID)
	if err != nil {
		t.Fatal(err)
	}
	item, err := service.TransitionWorkItem(ctx, app.TransitionWorkItemCommand{
		WorkItemID: itemContext.WorkItem.ID, TargetStatus: work.StatusReady, ActorID: "human:reviewer", Reason: "Queue work.", ExpectedVersion: itemContext.WorkItem.Version, IdempotencyKey: "ready-cli-item",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := database.Close(); err != nil {
		t.Fatal(err)
	}

	stdout.Reset()
	stderr.Reset()
	if code := Run(ctx, []string{"ready", root}, &stdout, &stderr); code != 0 {
		t.Fatalf("ready exited %d: %s", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "TH-CLI\tPrepare the research dossier") {
		t.Fatalf("ready output = %q", stdout.String())
	}
	stdout.Reset()
	stderr.Reset()
	if code := Run(ctx, []string{"show", item.ID, root}, &stdout, &stderr); code != 0 {
		t.Fatalf("show exited %d: %s", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), `"Key": "TH-CLI"`) || !strings.Contains(stdout.String(), `"ExecutionStatus": "ready"`) {
		t.Fatalf("show output = %q", stdout.String())
	}
}

type cliIDs struct{ next int }

func (ids *cliIDs) New() (string, error) {
	ids.next++
	return time.Date(2026, 8, 21, 18, 0, ids.next, 0, time.UTC).Format("20060102T150405.000000000"), nil
}

type cliClock struct{}

func (cliClock) Now() time.Time { return time.Date(2026, 8, 21, 18, 0, 0, 0, time.UTC) }
