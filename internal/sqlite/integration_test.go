package sqlite

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/dennisschroeder/workgraph/internal/app"
	"github.com/dennisschroeder/workgraph/internal/domain/output"
	"github.com/dennisschroeder/workgraph/internal/domain/work"
)

func TestInitializationAndDomainNeutralVerticalSlice(t *testing.T) {
	ctx := context.Background()
	databasePath := filepath.Join(t.TempDir(), "workgraph.db")
	database, err := Open(ctx, databasePath)
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	if err := database.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	if err := database.Migrate(ctx); err != nil {
		t.Fatalf("second migration run failed: %v", err)
	}

	assertInitializationState(t, database)

	service := app.NewService(database.Store(), &testIDs{}, testClock{})
	if _, err := service.RegisterActor(ctx, app.RegisterActorCommand{Actor: work.Actor{ID: "human:owner", Kind: work.ActorTypeHuman, DisplayName: "Owner"}, IdempotencyKey: "register-owner"}); err != nil {
		t.Fatal(err)
	}
	objective, err := service.CreateObjective(ctx, app.CreateObjectiveCommand{
		ActorID:        "human:owner",
		Key:            "OBJ-DOSSIER",
		Title:          "Produce a renewable-energy policy research dossier",
		Description:    "Synthesize source-linked policy evidence for a human decision.",
		DesiredOutcome: "A reviewed dossier with findings, sources, and explicit uncertainty.",
		Phase:          work.ObjectivePlanning,
	})
	if err != nil {
		t.Fatal(err)
	}
	plan, err := service.CreatePlan(ctx, app.CreatePlanCommand{
		ActorID:         "human:owner",
		ObjectiveID:     objective.ID,
		Title:           "Research dossier plan",
		Summary:         "Collect sources, synthesize findings, and submit the dossier for review.",
		Revision:        1,
		CommitmentState: work.PlanDraft,
	})
	if err != nil {
		t.Fatal(err)
	}
	item, err := service.CreateWorkItem(ctx, app.CreateWorkItemCommand{
		ActorID:           "human:owner",
		Key:               "WG-1",
		ObjectiveID:       objective.ID,
		PlanID:            plan.ID,
		Title:             "Synthesize the policy evidence",
		Description:       "Produce the dossier without performing any external action.",
		Kind:              "research",
		CommitmentState:   work.ItemProposed,
		ExecutionStatus:   work.StatusBacklog,
		Priority:          work.PriorityHigh,
		EstimatedScope:    work.ScopeMedium,
		ExecutionPolicy:   work.PolicyAgentMayPropose,
		RequiredActorKind: work.ActorAny,
		AttentionState:    work.AttentionNeedsHumanReview,
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = service.DefineExpectedOutput(ctx, app.DefineExpectedOutputCommand{
		ActorID:         "human:owner",
		WorkItemID:      item.ID,
		Name:            "Policy research dossier",
		ProfileName:     "research_dossier",
		ProfileVersion:  1,
		Contract:        json.RawMessage(`{"jurisdiction":"EU","source_recency":"five_years","validation":{"required":[{"kind":"evaluation","criterion_ref":"instance_contract"}]}}`),
		DestinationHint: "dossiers/renewable-energy-policy.md",
		Required:        true,
		Ordinal:         1,
		ExpectedVersion: item.Version,
		IdempotencyKey:  "define-policy-output",
	})
	if err != nil {
		t.Fatal(err)
	}

	result, err := service.GetWorkItem(ctx, item.ID)
	if err != nil {
		t.Fatal(err)
	}
	if result.Objective.ID != objective.ID || result.Plan == nil || result.Plan.ID != plan.ID || result.WorkItem.ID != item.ID {
		t.Fatalf("broken structured context chain: %#v", result)
	}
	if len(result.ExpectedOutputs) != 1 {
		t.Fatalf("expected one output, got %d", len(result.ExpectedOutputs))
	}
	profile := result.ExpectedOutputs[0].Profile
	if profile.Name != "research_dossier" || profile.Version != 1 || profile.LifecycleState != output.ProfileActive {
		t.Fatalf("unexpected profile binding: %#v", profile)
	}
}

func assertInitializationState(t *testing.T, database *Database) {
	t.Helper()
	queries := []struct {
		query string
		want  int
	}{
		{"SELECT COUNT(*) FROM schema_migrations", 5},
		{"SELECT COUNT(*) FROM output_profiles", 8},
		{"SELECT COUNT(*) FROM output_profiles WHERE lifecycle_state = 'active' AND built_in = 1", 8},
		{"PRAGMA foreign_keys", 1},
		{"PRAGMA busy_timeout", 5000},
	}
	for _, query := range queries {
		var got int
		if err := database.db.QueryRow(query.query).Scan(&got); err != nil {
			t.Fatalf("query %q: %v", query.query, err)
		}
		if got != query.want {
			t.Fatalf("query %q returned %d, want %d", query.query, got, query.want)
		}
	}
	rows, err := database.db.Query("SELECT name, version FROM output_profiles ORDER BY name")
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	wantProfiles := []string{
		"agent_definition/v1",
		"decision_record/v1",
		"generic_artifact/v1",
		"research_dossier/v1",
		"skill_package/v1",
		"structured_document/v1",
		"tool_installation/v1",
		"workflow_definition/v1",
	}
	var profiles []string
	for rows.Next() {
		var name string
		var version int
		if err := rows.Scan(&name, &version); err != nil {
			t.Fatal(err)
		}
		profiles = append(profiles, name+"/v"+fmt.Sprint(version))
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(profiles, wantProfiles) {
		t.Fatalf("seeded profiles = %v, want %v", profiles, wantProfiles)
	}
	var journalMode string
	if err := database.db.QueryRow("PRAGMA journal_mode").Scan(&journalMode); err != nil {
		t.Fatal(err)
	}
	if journalMode != "wal" {
		t.Fatalf("journal mode %q, want wal", journalMode)
	}
}

type testIDs struct {
	next int
}

func (ids *testIDs) New() (string, error) {
	ids.next++
	return time.Date(2026, 8, 21, 0, 0, ids.next, 0, time.UTC).Format("20060102T150405.000000000"), nil
}

type testClock struct{}

func (testClock) Now() time.Time {
	return time.Date(2026, 8, 21, 12, 30, 0, 0, time.UTC)
}
