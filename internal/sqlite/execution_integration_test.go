package sqlite

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"

	"github.com/dennisschroeder/throughline/internal/app"
	"github.com/dennisschroeder/throughline/internal/domain/output"
	"github.com/dennisschroeder/throughline/internal/domain/work"
	"github.com/dennisschroeder/throughline/internal/ports"
)

func TestDurableExecutionGraphVerticalSlice(t *testing.T) {
	ctx := context.Background()
	database, err := Open(ctx, filepath.Join(t.TempDir(), "execution.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	if err := database.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	service := app.NewService(database.Store(), &planningIDs{}, &planningClock{})
	for _, command := range []app.RegisterActorCommand{
		{Actor: work.Actor{ID: "human:sponsor", Kind: work.ActorTypeHuman, DisplayName: "Sponsor"}, IdempotencyKey: "register-sponsor"},
		{Actor: work.Actor{ID: "agent:researcher", Kind: work.ActorTypeAgent, DisplayName: "Researcher"}, IdempotencyKey: "register-researcher"},
		{Actor: work.Actor{ID: "agent:planner", Kind: work.ActorTypeAgent, DisplayName: "Planner"}, IdempotencyKey: "register-planner"},
		{Actor: work.Actor{ID: "human:reviewer", Kind: work.ActorTypeHuman, DisplayName: "Reviewer"}, IdempotencyKey: "register-reviewer"},
	} {
		if _, err := service.RegisterActor(ctx, command); err != nil {
			t.Fatal(err)
		}
	}

	producerObjective, err := service.CreateObjective(ctx, app.CreateObjectiveCommand{
		ActorID: "human:sponsor", IdempotencyKey: "create-producer-objective",
		Key: "OBJ-DOSSIER", Title: "Produce a reusable research dossier",
		DesiredOutcome: "A validated source-auditing dossier can be reused exactly.", Phase: work.ObjectivePlanning,
	})
	if err != nil {
		t.Fatal(err)
	}
	plan, err := service.ProposePlan(ctx, app.ProposePlanCommand{
		ObjectiveID: producerObjective.ID, ActorID: "agent:planner", IdempotencyKey: "propose-producer-plan", Title: "Research and synthesize", Revision: 1,
		Items: []app.ProposedWorkItem{
			{
				ClientRef: "research", Key: "TH-DOSSIER", Title: "Research source-auditing methods", Kind: "research",
				Priority: work.PriorityHigh, EstimatedScope: work.ScopeMedium, ExecutionPolicy: work.PolicyAgentMayPropose, RequiredActorKind: work.ActorAgent,
				AcceptanceCriteria: []app.ProposedAcceptanceCriterion{{Text: "The dossier distinguishes evidence from uncertainty.", Required: true, Ordinal: 1}},
				ExpectedOutputs: []app.ProposedExpectedOutput{{
					Name: "Source-auditing dossier", ProfileName: "research_dossier", ProfileVersion: 1, Required: true, Ordinal: 1,
					Contract: json.RawMessage(`{"minimum_sources":3,"validation":{"required":[{"kind":"evaluation","criterion_ref":"minimum_sources"}]}}`),
				}},
			},
			{
				ClientRef: "skill", Key: "TH-SKILL", Title: "Design a skill from the accepted dossier", Kind: "skill_design",
				Priority: work.PriorityMedium, EstimatedScope: work.ScopeSmall, ExecutionPolicy: work.PolicyAgentMayPropose, RequiredActorKind: work.ActorAgent,
			},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.ReviewPlan(ctx, app.ReviewPlanCommand{PlanID: plan.Plan.ID, ReviewerActorID: "human:sponsor", IdempotencyKey: "review-producer-plan", Decision: work.PlanApproved, Reason: "The contracts are explicit.", ExpectedVersion: 1}); err != nil {
		t.Fatal(err)
	}
	if _, err := service.TransitionObjective(ctx, app.TransitionObjectiveCommand{ObjectiveID: producerObjective.ID, TargetPhase: work.ObjectiveExecution, ActorID: "human:sponsor", IdempotencyKey: "transition-producer-objective", Reason: "Begin approved work.", ExpectedVersion: 1}); err != nil {
		t.Fatal(err)
	}

	items := itemsByKey(plan.Items)
	research := items["TH-DOSSIER"]
	skill := items["TH-SKILL"]
	research, err = service.TransitionWorkItem(ctx, app.TransitionWorkItemCommand{WorkItemID: research.ID, TargetStatus: work.StatusReady, ActorID: "human:sponsor", Reason: "Approved for execution.", ExpectedVersion: 2, IdempotencyKey: "ready-research"})
	if err != nil {
		t.Fatal(err)
	}
	skill, err = service.TransitionWorkItem(ctx, app.TransitionWorkItemCommand{WorkItemID: skill.ID, TargetStatus: work.StatusReady, ActorID: "human:sponsor", Reason: "Queue after research.", ExpectedVersion: 2, IdempotencyKey: "ready-skill"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.LinkDependency(ctx, app.LinkDependencyCommand{WorkItemID: skill.ID, DependsOnWorkItemID: research.ID, Kind: work.DependencyHard, ActorID: "agent:planner", ExpectedVersion: skill.Version, IdempotencyKey: "link-skill-research"}); err != nil {
		t.Fatal(err)
	}
	if _, err := service.LinkDependency(ctx, app.LinkDependencyCommand{WorkItemID: research.ID, DependsOnWorkItemID: skill.ID, Kind: work.DependencyHard, ActorID: "agent:planner", ExpectedVersion: research.Version, IdempotencyKey: "link-research-skill"}); err == nil {
		t.Fatal("expected a hard dependency cycle to be rejected")
	}
	ready, err := service.ListReadyWork(ctx)
	if err != nil {
		t.Fatal(err)
	}
	assertReadyKeys(t, ready, "TH-DOSSIER")

	producerContext, err := service.GetWorkItem(ctx, research.ID)
	if err != nil {
		t.Fatal(err)
	}
	expected := producerContext.ExpectedOutputs[0].ExpectedOutput
	revision, err := service.CreateOutputRevision(ctx, app.CreateOutputRevisionCommand{
		ExpectedOutputID: expected.ID, ActorID: "agent:researcher", IdempotencyKey: "create-producer-revision", ContentDigest: "sha256:dossier-v1",
		Artifacts: []app.OutputArtifactInput{{Kind: "document", URI: "file:///tmp/source-auditing-dossier.md", Title: "Source-auditing dossier", Role: "primary"}},
	})
	if err != nil {
		t.Fatal(err)
	}

	consumerObjective, err := service.CreateObjective(ctx, app.CreateObjectiveCommand{
		ActorID: "human:sponsor", IdempotencyKey: "create-consumer-objective",
		Key: "OBJ-REUSE", Title: "Reuse the accepted dossier", DesiredOutcome: "A second objective consumes the exact reviewed result.", Phase: work.ObjectivePlanning,
	})
	if err != nil {
		t.Fatal(err)
	}
	consumerPlan, err := service.ProposePlan(ctx, app.ProposePlanCommand{
		ObjectiveID: consumerObjective.ID, ActorID: "agent:planner", IdempotencyKey: "propose-consumer-plan", Title: "Apply the dossier", Revision: 1,
		Items: []app.ProposedWorkItem{{
			ClientRef: "apply", Key: "TH-APPLY", Title: "Apply the reviewed source-auditing method", Kind: "workflow_design",
			Priority: work.PriorityMedium, EstimatedScope: work.ScopeSmall, ExecutionPolicy: work.PolicyAgentMayPropose, RequiredActorKind: work.ActorAgent,
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.ReviewPlan(ctx, app.ReviewPlanCommand{PlanID: consumerPlan.Plan.ID, ReviewerActorID: "human:sponsor", IdempotencyKey: "review-consumer-plan", Decision: work.PlanApproved, Reason: "Reuse is explicit.", ExpectedVersion: 1}); err != nil {
		t.Fatal(err)
	}
	if _, err := service.TransitionObjective(ctx, app.TransitionObjectiveCommand{ObjectiveID: consumerObjective.ID, TargetPhase: work.ObjectiveExecution, ActorID: "human:sponsor", IdempotencyKey: "transition-consumer-objective", Reason: "Begin reuse.", ExpectedVersion: 1}); err != nil {
		t.Fatal(err)
	}
	consumer := consumerPlan.Items[0].WorkItem
	consumer, err = service.TransitionWorkItem(ctx, app.TransitionWorkItemCommand{WorkItemID: consumer.ID, TargetStatus: work.StatusReady, ActorID: "human:sponsor", Reason: "Queue when the dossier is accepted.", ExpectedVersion: 2, IdempotencyKey: "ready-consumer"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.AddOutputRequirement(ctx, app.AddOutputRequirementCommand{
		WorkItemID: consumer.ID, RequiredOutputRevisionID: revision.ID, RequiredProfileName: "research_dossier",
		VersionConstraint: "=1", Required: true, ActorID: "agent:planner", ExpectedVersion: consumer.Version, IdempotencyKey: "invalid-requirement-ambiguous",
	}); err == nil {
		t.Fatal("expected an ambiguous output requirement target to be rejected")
	}
	if _, err := service.AddOutputRequirement(ctx, app.AddOutputRequirementCommand{
		WorkItemID: consumer.ID, Required: true, ActorID: "agent:planner", ExpectedVersion: consumer.Version, IdempotencyKey: "invalid-requirement-missing",
	}); err == nil {
		t.Fatal("expected a missing output requirement target to be rejected")
	}
	if _, err := service.AddOutputRequirement(ctx, app.AddOutputRequirementCommand{WorkItemID: consumer.ID, RequiredOutputRevisionID: revision.ID, Required: true, Note: "Reuse this exact reviewed dossier.", ActorID: "agent:planner", ExpectedVersion: consumer.Version, IdempotencyKey: "require-exact-revision"}); err != nil {
		t.Fatal(err)
	}
	ready, err = service.ListReadyWork(ctx)
	if err != nil {
		t.Fatal(err)
	}
	assertReadyKeys(t, ready, "TH-DOSSIER")

	validations := []app.RecordValidationCommand{
		{OutputRevisionID: revision.ID, CriterionRef: "structure", ValidatorKind: output.ValidatorStructure, Verdict: output.VerdictPassed, VerifierActorID: "agent:validator", Details: json.RawMessage(`{"summary":"Required dossier sections are present."}`)},
		{OutputRevisionID: revision.ID, CriterionRef: "provenance", ValidatorKind: output.ValidatorProvenance, Verdict: output.VerdictPassed, VerifierActorID: "agent:validator", Details: json.RawMessage(`{"summary":"Claims link to sources."}`)},
		{OutputRevisionID: revision.ID, CriterionRef: "human_review", ValidatorKind: output.ValidatorHumanReview, Verdict: output.VerdictPassed, VerifierActorID: "human:reviewer", Details: json.RawMessage(`{"rationale":"Evidence and uncertainty are clearly separated."}`)},
		{OutputRevisionID: revision.ID, CriterionRef: "minimum_sources", ValidatorKind: output.ValidatorEvaluation, Verdict: output.VerdictPassed, VerifierActorID: "human:reviewer", Details: json.RawMessage(`{"summary":"At least three independent sources are present."}`)},
	}
	for index, command := range validations {
		command.IdempotencyKey = "record-validation-" + string(rune('a'+index))
		revision, err = service.RecordValidation(ctx, command)
		if err != nil {
			t.Fatal(err)
		}
	}
	if revision.AcceptanceState != output.RevisionAccepted {
		t.Fatalf("revision state = %q", revision.AcceptanceState)
	}
	revision, err = service.RecordValidation(ctx, app.RecordValidationCommand{
		OutputRevisionID: revision.ID, CriterionRef: "consumer-readiness", ValidatorKind: output.ValidatorSuccessorUse,
		Verdict: output.VerdictPassed, VerifierActorID: "agent:consumer", IdempotencyKey: "record-consumer-readiness", Details: json.RawMessage(`{"summary":"The accepted dossier is usable by its declared consumer."}`),
	})
	if err != nil {
		t.Fatal(err)
	}
	if revision.AcceptanceState != output.RevisionAccepted {
		t.Fatal("additional append-only evidence changed accepted state")
	}
	if _, err := service.RecordValidation(ctx, app.RecordValidationCommand{
		OutputRevisionID: revision.ID, CriterionRef: "structure", ValidatorKind: output.ValidatorStructure,
		Verdict: output.VerdictFailed, VerifierActorID: "agent:validator", IdempotencyKey: "record-late-contradiction", Details: json.RawMessage(`{"summary":"Late contradictory verdict."}`),
	}); err == nil {
		t.Fatal("expected contract validation to be closed after acceptance")
	}
	if _, err := service.RecordValidation(ctx, app.RecordValidationCommand{
		OutputRevisionID: revision.ID, CriterionRef: "structure", ValidatorKind: output.ValidatorSuccessorUse,
		Verdict: output.VerdictPassed, VerifierActorID: "agent:consumer", IdempotencyKey: "record-shadowed-criterion", Details: json.RawMessage(`{"summary":"Attempted criterion shadowing."}`),
	}); err == nil {
		t.Fatal("expected successor-use evidence to reject an acceptance criterion reference")
	}

	criterion := producerContext.AcceptanceCriteria[0]
	if _, err := service.ResolveAcceptanceCriterion(ctx, app.ResolveAcceptanceCriterionCommand{CriterionID: criterion.ID, Status: work.AcceptanceSatisfied, ActorID: "human:reviewer", Rationale: "The accepted dossier meets the criterion.", ExpectedWorkItemVersion: research.Version, IdempotencyKey: "resolve-research-criterion"}); err != nil {
		t.Fatal(err)
	}
	research.Version++
	for _, target := range []work.ExecutionStatus{work.StatusInProgress, work.StatusReview, work.StatusDone} {
		research, err = service.TransitionWorkItem(ctx, app.TransitionWorkItemCommand{WorkItemID: research.ID, TargetStatus: target, ActorID: "agent:researcher", Reason: "Advance validated dossier work.", ExpectedVersion: research.Version, IdempotencyKey: "advance-research-" + string(target)})
		if err != nil {
			t.Fatal(err)
		}
	}

	ready, err = service.ListReadyWork(ctx)
	if err != nil {
		t.Fatal(err)
	}
	assertReadyKeys(t, ready, "TH-APPLY", "TH-SKILL")
	accepted, err := service.ListAcceptedOutputs(ctx, app.AcceptedOutputFilter{
		ProfileName: "research_dossier", VersionConstraint: "=1", ObjectiveID: producerObjective.ID,
		ProducedBy: "agent:researcher", AcceptedSince: revision.ProducedAt, Limit: 10,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(accepted) != 1 || accepted[0].Revision.ID != revision.ID || accepted[0].Revision.ContentDigest != "sha256:dossier-v1" {
		t.Fatalf("accepted outputs = %#v", accepted)
	}
	if _, err := service.ListAcceptedOutputs(ctx, app.AcceptedOutputFilter{Limit: 501}); err == nil {
		t.Fatal("expected an unbounded accepted-output query to be rejected")
	}
	otherProducerOutputs, err := service.ListAcceptedOutputs(ctx, app.AcceptedOutputFilter{ProducedBy: "agent:other", Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(otherProducerOutputs) != 0 {
		t.Fatalf("producer filter returned %#v", otherProducerOutputs)
	}
	reloadedProducer, err := service.GetWorkItem(ctx, research.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(reloadedProducer.OutputRevisions) != 1 || len(reloadedProducer.OutputRevisions[0].Artifacts) != 1 || len(reloadedProducer.OutputRevisions[0].Validations) != 5 || reloadedProducer.OutputRevisions[0].Revision.AcceptanceState != output.RevisionAccepted {
		t.Fatalf("structured producer output context = %#v", reloadedProducer.OutputRevisions)
	}
	revision2, err := service.CreateOutputRevision(ctx, app.CreateOutputRevisionCommand{
		ExpectedOutputID: expected.ID, ActorID: "agent:researcher", IdempotencyKey: "create-producer-revision-v2", ContentDigest: "sha256:dossier-v2",
		Artifacts: []app.OutputArtifactInput{{Kind: "document", URI: "file:///tmp/source-auditing-dossier.md", Title: "Updated source-auditing dossier", Role: "primary"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if revision2.Revision != 2 || revision2.AcceptanceState != output.RevisionProduced {
		t.Fatalf("second revision = %#v", revision2)
	}
	reloadedProducer, err = service.GetWorkItem(ctx, research.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(reloadedProducer.OutputRevisions) != 2 || reloadedProducer.OutputRevisions[0].Revision.Artifacts[0].ArtifactID != reloadedProducer.OutputRevisions[1].Revision.Artifacts[0].ArtifactID {
		t.Fatalf("stable artifact reuse = %#v", reloadedProducer.OutputRevisions)
	}
	consumerContext, err := service.GetWorkItem(ctx, consumer.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(consumerContext.OutputRequirements) != 1 || consumerContext.OutputRequirements[0].RequiredOutputRevisionID != revision.ID {
		t.Fatalf("consumer requirements = %#v", consumerContext.OutputRequirements)
	}
	activities, err := service.ListActivity(ctx, app.ActivityFilter{WorkItemID: research.ID, Limit: 100})
	if err != nil {
		t.Fatal(err)
	}
	foundDone := false
	for _, activity := range activities {
		if activity.EventType == "work_item.status_changed" && activity.Summary == "Work item moved from review to done" {
			foundDone = true
		}
	}
	if len(activities) < 6 || !foundDone {
		t.Fatalf("research activity = %#v", activities)
	}
	cancelled, err := service.TransitionWorkItem(ctx, app.TransitionWorkItemCommand{
		WorkItemID: consumer.ID, TargetStatus: work.StatusCancelled, ActorID: "human:sponsor",
		Reason: "The second objective was intentionally stopped.", ExpectedVersion: consumerContext.WorkItem.Version, IdempotencyKey: "cancel-consumer",
	})
	if err != nil {
		t.Fatal(err)
	}
	consumerActivities, err := service.ListActivity(ctx, app.ActivityFilter{WorkItemID: cancelled.ID, Limit: 100})
	if err != nil {
		t.Fatal(err)
	}
	var cancellationPayload map[string]string
	if err := json.Unmarshal(consumerActivities[len(consumerActivities)-1].PayloadJSON, &cancellationPayload); err != nil {
		t.Fatal(err)
	}
	if cancellationPayload["reason"] != "The second objective was intentionally stopped." {
		t.Fatalf("cancellation reason was not durable: %#v", cancellationPayload)
	}
	allActivities, err := service.ListActivity(ctx, app.ActivityFilter{Limit: 500})
	if err != nil {
		t.Fatal(err)
	}
	events := make(map[string]bool)
	for _, activity := range allActivities {
		events[activity.EventType] = true
	}
	for _, event := range []string{"objective.created", "plan.proposed", "work_item.proposed", "plan.reviewed", "objective.phase_changed", "output_revision.accepted"} {
		if !events[event] {
			t.Fatalf("activity timeline is missing %s: %#v", event, events)
		}
	}
	for name, statement := range map[string]string{
		"revision content": "UPDATE output_revisions SET content_digest = 'changed' WHERE id = '" + revision.ID + "'",
		"artifact binding": "INSERT INTO output_revision_artifacts(output_revision_id, artifact_id, role) SELECT '" + revision.ID + "', artifact_id, 'secondary' FROM output_revision_artifacts WHERE output_revision_id = '" + revision.ID + "' LIMIT 1",
		"validation":       "UPDATE output_validations SET verdict = 'failed' WHERE output_revision_id = '" + revision.ID + "'",
		"activity":         "UPDATE activity SET summary = 'changed' WHERE work_item_id = '" + research.ID + "'",
	} {
		t.Run(name+" is immutable", func(t *testing.T) {
			if _, err := database.db.ExecContext(ctx, statement); err == nil {
				t.Fatal("expected authoritative SQLite immutability guard")
			}
		})
	}
}

func itemsByKey(items []ports.PlannedWorkItem) map[string]work.WorkItem {
	result := make(map[string]work.WorkItem, len(items))
	for _, item := range items {
		result[item.WorkItem.Key] = item.WorkItem
	}
	return result
}

func assertReadyKeys(t *testing.T, items []ports.ReadyWorkItem, want ...string) {
	t.Helper()
	if len(items) != len(want) {
		t.Fatalf("ready count = %d, want %d: %#v", len(items), len(want), items)
	}
	got := make(map[string]bool, len(items))
	for _, item := range items {
		got[item.WorkItem.Key] = true
	}
	for _, key := range want {
		if !got[key] {
			t.Fatalf("ready work missing %s: %#v", key, items)
		}
	}
}
