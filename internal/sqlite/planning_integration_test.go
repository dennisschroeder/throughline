package sqlite

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"
	"time"

	"github.com/dennisschroeder/throughline/internal/app"
	"github.com/dennisschroeder/throughline/internal/domain/output"
	"github.com/dennisschroeder/throughline/internal/domain/work"
)

func TestIntentAndPlanningVerticalSlice(t *testing.T) {
	ctx := context.Background()
	database, err := Open(ctx, filepath.Join(t.TempDir(), "planning.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	if err := database.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	service := app.NewService(database.Store(), &planningIDs{}, &planningClock{})
	if _, err := app.UnwrapMutation(service.RegisterActor(ctx, app.RegisterActorCommand{Actor: work.Actor{ID: "agent:planner", Kind: work.ActorTypeAgent, DisplayName: "Planner"}, IdempotencyKey: "register-planner"})); err != nil {
		t.Fatal(err)
	}

	objective, err := app.UnwrapMutation(service.CreateObjective(ctx, app.CreateObjectiveCommand{
		ActorID:        "human:sponsor",
		IdempotencyKey: "create-objective-planning",
		Key:            "OBJ-SKILL-RESEARCH",
		Title:          "Design a source-auditing skill",
		Description:    "Research existing methods and design a reusable skill without executing external actions.",
		DesiredOutcome: "An approved plan with explicit evidence and output contracts.",
		Phase:          work.ObjectivePlanning,
	}))
	if err != nil {
		t.Fatal(err)
	}
	var requirement work.ContextRecord
	var assumption work.ContextRecord
	for _, command := range []app.RecordContextCommand{
		{ObjectiveID: objective.ID, ActorID: "agent:planner", IdempotencyKey: "record-requirement", Kind: work.ContextRequirement, Title: "Source-linked claims", Body: "Every material claim names its source.", Status: work.ContextAccepted},
		{ObjectiveID: objective.ID, ActorID: "human:sponsor", IdempotencyKey: "record-constraint", Kind: work.ContextConstraint, Title: "No external installation", Body: "This milestone records installation work but performs no side effect.", Status: work.ContextAccepted},
		{ObjectiveID: objective.ID, ActorID: "agent:planner", IdempotencyKey: "record-assumption", Kind: work.ContextAssumption, Title: "Three methods are enough", Body: "Validate during research.", Status: work.ContextUntested, Confidence: "medium"},
		{ObjectiveID: objective.ID, ActorID: "agent:researcher", IdempotencyKey: "record-finding", Kind: work.ContextFinding, Title: "Provenance rubrics are reusable", Body: "A rubric can be applied across dossiers.", Status: work.ContextRecorded, SourceURI: "https://example.test/provenance"},
		{ObjectiveID: objective.ID, ActorID: "human:sponsor", IdempotencyKey: "record-success-metric", Kind: work.ContextSuccessMetric, Title: "Independent recovery", Body: "A new agent can recover the approved intent and plan.", Status: work.ContextUntested},
	} {
		record, err := app.UnwrapMutation(service.RecordContext(ctx, command))
		if err != nil {
			t.Fatal(err)
		}
		if record.Kind == work.ContextRequirement {
			requirement = record
		}
		if record.Kind == work.ContextAssumption {
			assumption = record
		}
	}
	validating, err := app.UnwrapMutation(service.TransitionContext(ctx, app.TransitionContextCommand{
		ContextRecordID: assumption.ID, ActorID: "agent:researcher", IdempotencyKey: "transition-assumption-validating", TargetStatus: work.ContextValidating, ExpectedVersion: 1,
	}))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := app.UnwrapMutation(service.TransitionContext(ctx, app.TransitionContextCommand{
		ContextRecordID: validating.ID, ActorID: "agent:researcher", IdempotencyKey: "transition-assumption-invalidated", TargetStatus: work.ContextInvalidated, ExpectedVersion: 2,
	})); err != nil {
		t.Fatal(err)
	}
	if _, err := app.UnwrapMutation(service.RecordContext(ctx, app.RecordContextCommand{
		ObjectiveID: objective.ID, ActorID: "human:sponsor", Kind: work.ContextRequirement,
		IdempotencyKey: "supersede-requirement",
		Title:          "Source-linked claims and uncertainty", Body: "Every material claim names its source and uncertainty.",
		Status: work.ContextAccepted, SupersedesID: requirement.ID,
	})); err != nil {
		t.Fatal(err)
	}
	question, err := app.UnwrapMutation(service.AskQuestion(ctx, app.AskQuestionCommand{
		ObjectiveID: objective.ID, ActorID: "agent:planner", IdempotencyKey: "ask-reviewer-question", Question: "Which audience owns final review?", AttentionState: work.AttentionNeedsHumanDecision,
	}))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := app.UnwrapMutation(service.AnswerQuestion(ctx, app.AnswerQuestionCommand{
		QuestionID: question.ID, ActorID: "human:sponsor", IdempotencyKey: "answer-reviewer-question", Answer: "The research operations lead.", ExpectedVersion: 1,
	})); err != nil {
		t.Fatal(err)
	}
	waivedQuestion, err := app.UnwrapMutation(service.AskQuestion(ctx, app.AskQuestionCommand{
		ObjectiveID: objective.ID, ActorID: "agent:planner", IdempotencyKey: "ask-transcript-question", Question: "Should the skill include raw transcripts?",
	}))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := app.UnwrapMutation(service.WaiveQuestion(ctx, app.WaiveQuestionCommand{
		QuestionID: waivedQuestion.ID, ActorID: "human:sponsor", IdempotencyKey: "waive-transcript-question", Reason: "Raw transcripts are outside the output contract.", ExpectedVersion: 1,
	})); err != nil {
		t.Fatal(err)
	}
	decision, err := app.UnwrapMutation(service.RecordDecision(ctx, app.RecordDecisionCommand{
		ObjectiveID: objective.ID, ActorID: "human:sponsor", IdempotencyKey: "record-rubric-decision", Title: "Use a reusable rubric", Decision: "Ship the provenance rubric with the skill.", Rationale: "It makes later reviews consistent.", Alternatives: []string{"Keep the rubric in chat"},
	}))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := app.UnwrapMutation(service.RecordDecision(ctx, app.RecordDecisionCommand{
		ObjectiveID: objective.ID, ActorID: "human:sponsor", IdempotencyKey: "supersede-rubric-decision", Title: "Use a reusable rubric and uncertainty scale",
		Decision: "Ship the provenance rubric and uncertainty scale with the skill.", Rationale: "This corrects the accepted decision with a more complete review contract.",
		Alternatives: []string{"Rubric only"}, SupersedesID: decision.ID,
	})); err != nil {
		t.Fatal(err)
	}

	proposedProfile, err := app.UnwrapMutation(service.ProposeOutputProfile(ctx, app.ProposeOutputProfileCommand{
		ActorID: "agent:designer", IdempotencyKey: "propose-evidence-map", Name: "evidence_map", Version: 1, Description: "A claim-to-source evidence map.",
		Structure: json.RawMessage(`{"required":["claims","sources"]}`), Semantics: json.RawMessage(`{"claims_require_sources":true}`), Validation: json.RawMessage(`{"required":[{"kind":"human_review"}]}`),
	}))
	if err != nil {
		t.Fatal(err)
	}
	if proposedProfile.LifecycleState != output.ProfileProposed {
		t.Fatalf("profile state = %q", proposedProfile.LifecycleState)
	}
	if _, err := app.UnwrapMutation(service.ProposePlan(ctx, app.ProposePlanCommand{
		ObjectiveID: objective.ID,
		ActorID:     "agent:planner", IdempotencyKey: "propose-premature-plan",
		Title:    "Premature profile use",
		Revision: 1,
		Steps: []app.ProposedPlanStep{{
			ClientRef: "premature", Key: "TH-PREMATURE", Title: "Use an unreviewed vocabulary", Kind: "research",
			Priority: work.PriorityMedium, EstimatedScope: work.ScopeSmall, ExecutionPolicy: work.PolicyAgentMayPropose, RequiredActorKind: work.ActorAgent,
			ExpectedOutputs: []app.ProposedExpectedOutput{{Name: "Evidence map", ProfileName: "evidence_map", ProfileVersion: 1, Required: true, Ordinal: 1}},
		}},
	})); err == nil {
		t.Fatal("expected proposed output profile to be unusable")
	}
	if _, err := app.UnwrapMutation(service.ReviewOutputProfile(ctx, app.ReviewOutputProfileCommand{
		ProfileID: proposedProfile.ID, ReviewerActorID: "human:sponsor", IdempotencyKey: "review-evidence-map-v1", ExpectedVersion: 1, Decision: output.ProfileActive, Reason: "The contract is domain-neutral and reviewable.",
	})); err != nil {
		t.Fatal(err)
	}
	if _, err := app.UnwrapMutation(service.ProposeOutputProfile(ctx, app.ProposeOutputProfileCommand{
		ActorID: "agent:designer", IdempotencyKey: "propose-invalid-evidence-map-v2", Name: "evidence_map", Version: 2, Description: "Missing predecessor.",
		Structure: json.RawMessage(`{"required":["claims"]}`), Semantics: json.RawMessage(`{}`), Validation: json.RawMessage(`{}`),
	})); err == nil {
		t.Fatal("expected a later profile version without a predecessor to be rejected")
	}
	profileV2, err := app.UnwrapMutation(service.ProposeOutputProfile(ctx, app.ProposeOutputProfileCommand{
		ActorID: "agent:designer", IdempotencyKey: "propose-evidence-map-v2", Name: "evidence_map", Version: 2, Description: "An evidence map with explicit uncertainty.", Supersedes: "evidence_map/v1",
		Structure: json.RawMessage(`{"required":["claims","sources","uncertainty"]}`), Semantics: json.RawMessage(`{"claims_require_sources":true}`), Validation: json.RawMessage(`{"required":[{"kind":"human_review"}]}`),
	}))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := app.UnwrapMutation(service.ReviewOutputProfile(ctx, app.ReviewOutputProfileCommand{
		ProfileID: profileV2.ID, ReviewerActorID: "human:sponsor", IdempotencyKey: "review-evidence-map-v2", ExpectedVersion: 1, Decision: output.ProfileActive, Reason: "The successor makes uncertainty explicit.",
	})); err != nil {
		t.Fatal(err)
	}
	baseRubric, err := app.UnwrapMutation(service.ProposeOutputProfile(ctx, app.ProposeOutputProfileCommand{
		ActorID: "agent:designer", IdempotencyKey: "propose-review-rubric-v1", Name: "review_rubric", Version: 1, Description: "A reviewed scoring rubric.",
		Structure: json.RawMessage(`{"required":["criteria"]}`), Semantics: json.RawMessage(`{}`), Validation: json.RawMessage(`{}`),
	}))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := app.UnwrapMutation(service.ReviewOutputProfile(ctx, app.ReviewOutputProfileCommand{
		ProfileID: baseRubric.ID, ReviewerActorID: "human:sponsor", IdempotencyKey: "review-review-rubric-v1", ExpectedVersion: 1, Decision: output.ProfileActive, Reason: "The base rubric is usable.",
	})); err != nil {
		t.Fatal(err)
	}
	rejectedRubric, err := app.UnwrapMutation(service.ProposeOutputProfile(ctx, app.ProposeOutputProfileCommand{
		ActorID: "agent:designer", IdempotencyKey: "propose-review-rubric-v2", Name: "review_rubric", Version: 2, Description: "A rejected rubric revision.", SupersedesID: baseRubric.ID,
		Structure: json.RawMessage(`{"required":["criteria","weights"]}`), Semantics: json.RawMessage(`{}`), Validation: json.RawMessage(`{}`),
	}))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := app.UnwrapMutation(service.ReviewOutputProfile(ctx, app.ReviewOutputProfileCommand{
		ProfileID: rejectedRubric.ID, ReviewerActorID: "human:sponsor", IdempotencyKey: "review-review-rubric-v2", ExpectedVersion: 1, Decision: output.ProfileRejected, Reason: "The weights were underspecified.",
	})); err != nil {
		t.Fatal(err)
	}
	replacementRubric, err := app.UnwrapMutation(service.ProposeOutputProfile(ctx, app.ProposeOutputProfileCommand{
		ActorID: "agent:designer", IdempotencyKey: "propose-review-rubric-v3", Name: "review_rubric", Version: 3, Description: "A corrected rubric revision.", SupersedesID: baseRubric.ID,
		Structure: json.RawMessage(`{"required":["criteria","scoring"]}`), Semantics: json.RawMessage(`{}`), Validation: json.RawMessage(`{}`),
	}))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := app.UnwrapMutation(service.ReviewOutputProfile(ctx, app.ReviewOutputProfileCommand{
		ProfileID: replacementRubric.ID, ReviewerActorID: "human:sponsor", IdempotencyKey: "review-review-rubric-v3", ExpectedVersion: 1, Decision: output.ProfileActive, Reason: "The corrected successor is complete.",
	})); err != nil {
		t.Fatal(err)
	}
	if _, err := app.UnwrapMutation(service.ProposePlan(ctx, app.ProposePlanCommand{
		ObjectiveID: objective.ID, ActorID: "agent:planner", Title: "Stale profile plan", Revision: 1,
		Steps: []app.ProposedPlanStep{{
			ClientRef: "stale", Key: "TH-STALE", Title: "Use the superseded profile", Kind: "research",
			Priority: work.PriorityMedium, EstimatedScope: work.ScopeSmall, ExecutionPolicy: work.PolicyAgentMayPropose, RequiredActorKind: work.ActorAgent,
			ExpectedOutputs: []app.ProposedExpectedOutput{{Name: "Evidence map", ProfileName: "evidence_map", ProfileVersion: 1, Required: true, Ordinal: 1}},
		}},
	})); err == nil {
		t.Fatal("expected superseded output profile to be unusable")
	}

	planContext, err := app.UnwrapMutation(service.ProposePlan(ctx, app.ProposePlanCommand{
		ObjectiveID: objective.ID,
		ActorID:     "agent:planner", IdempotencyKey: "propose-main-plan",
		Title:    "Source-auditing skill plan",
		Summary:  "Research methods, design the skill, and document a separately governed installation step.",
		Revision: 1,
		Steps: []app.ProposedPlanStep{
			{
				ClientRef: "skill", ParentRef: "research", Key: "TH-SKILL", Title: "Design the reusable source-auditing skill", Kind: "skill_design", Required: true,
				Priority: work.PriorityHigh, EstimatedScope: work.ScopeMedium, ExecutionPolicy: work.PolicyAgentMayPropose, RequiredActorKind: work.ActorAgent,
				RequiredCapabilities: []string{"skill_design"},
				ExpectedOutputs:      []app.ProposedExpectedOutput{{Name: "Skill package", ProfileName: "skill_package", ProfileVersion: 1, Required: true, Ordinal: 1}, {Name: "Evidence map", ProfileName: "evidence_map", ProfileVersion: 2, Required: true, Ordinal: 2}},
			},
			{
				ClientRef: "research", Key: "TH-RESEARCH", Title: "Research source-auditing methods", Kind: "research", Required: true,
				Priority: work.PriorityHigh, EstimatedScope: work.ScopeMedium, ExecutionPolicy: work.PolicyAgentMayPropose, RequiredActorKind: work.ActorAgent,
				RequiredCapabilities: []string{"web_research", "document_reading"},
				ExpectedOutputs:      []app.ProposedExpectedOutput{{Name: "Method dossier", ProfileName: "research_dossier", ProfileVersion: 1, Required: true, Ordinal: 1}},
			},
			{
				ClientRef: "installation", Key: "TH-INSTALL", Title: "Prepare the reviewed installation procedure", Kind: "tool_installation", Required: true,
				Priority: work.PriorityMedium, EstimatedScope: work.ScopeSmall, ExecutionPolicy: work.PolicyApprovalRequired, RequiredActorKind: work.ActorHuman,
				RequiredCapabilities: []string{"mcp_installation"},
				ExpectedOutputs:      []app.ProposedExpectedOutput{{Name: "Installation procedure", ProfileName: "tool_installation", ProfileVersion: 1, Required: true, Ordinal: 1}},
				ExternalActions:      []app.ProposedExternalAction{{Required: true, Title: "Install the reviewed skill", Rationale: "Installation is an externally authorized effect.", AuthorizationSubject: json.RawMessage(`{"action_type":"tool.install","target":{"tool":"throughline"},"arguments":[],"scope":{},"permissions":["filesystem.write"],"credential_requirements":[],"constraints":{}}`)}},
			},
		},
	}))
	if err != nil {
		t.Fatal(err)
	}
	// A proposal persists a reusable definition and no executable work: the
	// plan says what a run would do, and only creating a run does it.
	if planContext.Plan.CommitmentState != work.PlanProposed || len(planContext.Steps) != 3 || len(planContext.Items) != 0 {
		t.Fatalf("unexpected proposed plan: %#v", planContext)
	}
	if _, err := app.UnwrapMutation(service.TransitionObjective(ctx, app.TransitionObjectiveCommand{
		ObjectiveID: objective.ID, TargetPhase: work.ObjectiveExecution, ActorID: "human:sponsor", IdempotencyKey: "reject-premature-objective-transition", Reason: "Start approved work.", ExpectedVersion: 1,
	})); err == nil {
		t.Fatal("expected execution transition before plan approval to be rejected")
	}
	if _, err := app.UnwrapMutation(service.ReviewPlan(ctx, app.ReviewPlanCommand{
		PlanID: planContext.Plan.ID, ReviewerActorID: "human:sponsor", IdempotencyKey: "review-main-plan", Decision: work.PlanApproved, Reason: "The scope, capabilities, and outputs are explicit.", ExpectedVersion: 1,
	})); err != nil {
		t.Fatal(err)
	}
	if _, err := app.UnwrapMutation(service.TransitionObjective(ctx, app.TransitionObjectiveCommand{
		ObjectiveID: objective.ID, TargetPhase: work.ObjectiveExecution, ActorID: "human:sponsor", IdempotencyKey: "transition-main-objective", Reason: "Start approved work.", ExpectedVersion: 1,
	})); err != nil {
		t.Fatal(err)
	}

	recovered, err := service.GetObjectiveContext(ctx, objective.ID)
	if err != nil {
		t.Fatal(err)
	}
	if recovered.Objective.Phase != work.ObjectiveExecution || recovered.Objective.Version != 2 {
		t.Fatalf("objective execution transition was not recovered: %#v", recovered.Objective)
	}
	if len(recovered.ContextRecords) != 6 || len(recovered.Questions) != 2 || len(recovered.Decisions) != 2 || len(recovered.Approvals) != 1 {
		t.Fatalf("intent and decision context was not recovered: %#v", recovered)
	}
	contextStatuses := make(map[string]work.ContextStatus)
	for _, record := range recovered.ContextRecords {
		contextStatuses[record.ID] = record.Status
	}
	if contextStatuses[requirement.ID] != work.ContextSuperseded || contextStatuses[assumption.ID] != work.ContextInvalidated {
		t.Fatalf("context lifecycle was not recovered: %#v", contextStatuses)
	}
	questionStatuses := make(map[work.QuestionStatus]bool)
	for _, candidate := range recovered.Questions {
		questionStatuses[candidate.Status] = true
	}
	if !questionStatuses[work.QuestionAnswered] || !questionStatuses[work.QuestionWaived] || recovered.Decisions[0].Status != work.DecisionSuperseded || recovered.Decisions[1].Status != work.DecisionAccepted {
		t.Fatalf("decision/question lifecycle was not recovered: %#v %#v", recovered.Questions, recovered.Decisions)
	}
	if len(recovered.Plans) != 1 || recovered.Plans[0].Plan.CommitmentState != work.PlanApproved {
		t.Fatalf("approved plan was not recovered: %#v", recovered.Plans)
	}
	steps := recovered.Plans[0].Steps
	stepIDs := make(map[string]string, len(steps))
	stepParents := make(map[string]string, len(steps))
	for _, step := range steps {
		stepIDs[step.Key] = step.ID
		stepParents[step.Key] = step.ParentStepID
	}
	if len(steps) != 3 || stepParents["TH-SKILL"] != stepIDs["TH-RESEARCH"] {
		t.Fatalf("recursive step hierarchy was not recovered: %#v", steps)
	}
	for _, step := range steps {
		if len(step.Definition.RequiredCapabilities) == 0 || len(step.Definition.ExpectedOutputs) == 0 {
			t.Fatalf("approved plan step is incomplete: %#v", step)
		}
	}

	// Only now does anything executable exist, and it exists because a run was
	// created — one fresh work item per step, carrying the step's own child
	// data.
	run, err := app.UnwrapMutation(service.CreatePlanRun(ctx, app.CreatePlanRunCommand{
		ObjectiveID: objective.ID, PlanID: planContext.Plan.ID, ActorID: "agent:planner", IdempotencyKey: "run-main-plan", RunKey: "main-1",
	}))
	if err != nil {
		t.Fatal(err)
	}
	if run.Run.Status != work.PlanRunActive || run.Run.Sequence != 1 || len(run.WorkItems) != 3 {
		t.Fatalf("plan run = %#v", run.Run)
	}
	items := runItemsByStepKey(run)
	itemIDs := make(map[string]string, len(items))
	for key, item := range items {
		itemIDs[key] = item.ID
		if item.CommitmentState != work.ItemAccepted || item.Origin != work.OriginPlanStep || item.PlanRunID != run.Run.ID {
			t.Fatalf("materialized item %s is incomplete: %#v", key, item)
		}
	}
	if installation := itemIDs["TH-INSTALL"]; installation == "" {
		t.Fatal("installation work item is missing")
	} else if context, err := service.GetWorkItem(ctx, installation); err != nil || len(context.ExternalActions) != 1 {
		t.Fatalf("copied external action = %#v, %v", context.ExternalActions, err)
	}
	if items["TH-SKILL"].ParentID != itemIDs["TH-RESEARCH"] {
		t.Fatalf("run did not reproduce the step hierarchy: %#v", items)
	}
	profiles, err := service.ListOutputProfiles(ctx)
	if err != nil {
		t.Fatal(err)
	}
	evidenceStates := make(map[int]output.ProfileState)
	rubricStates := make(map[int]output.ProfileState)
	for _, profile := range profiles {
		if profile.Name == "evidence_map" {
			evidenceStates[profile.Version] = profile.LifecycleState
		}
		if profile.Name == "review_rubric" {
			rubricStates[profile.Version] = profile.LifecycleState
		}
	}
	if evidenceStates[1] != output.ProfileSuperseded || evidenceStates[2] != output.ProfileActive {
		t.Fatalf("governed profile history = %#v", evidenceStates)
	}
	if rubricStates[1] != output.ProfileSuperseded || rubricStates[2] != output.ProfileRejected || rubricStates[3] != output.ProfileActive {
		t.Fatalf("rejected profile replacement history = %#v", rubricStates)
	}
	replacement, err := app.UnwrapMutation(service.ProposePlan(ctx, app.ProposePlanCommand{
		ObjectiveID: objective.ID, ActorID: "agent:planner", IdempotencyKey: "propose-replacement-plan", Title: "Source-auditing skill plan v2", Summary: "Refine the approved plan.", Revision: 2,
		Steps: []app.ProposedPlanStep{{
			ClientRef: "refine", Key: "TH-REFINE", Title: "Refine the source-auditing skill", Kind: "skill_design", Required: true,
			Priority: work.PriorityHigh, EstimatedScope: work.ScopeSmall, ExecutionPolicy: work.PolicyAgentMayPropose, RequiredActorKind: work.ActorAgent,
			RequiredCapabilities: []string{"skill_design"},
			ExpectedOutputs:      []app.ProposedExpectedOutput{{Name: "Refined skill", ProfileName: "skill_package", ProfileVersion: 1, Required: true, Ordinal: 1}},
		}},
	}))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := app.UnwrapMutation(service.ReviewPlan(ctx, app.ReviewPlanCommand{
		PlanID: replacement.Plan.ID, ReviewerActorID: "human:sponsor", IdempotencyKey: "review-replacement-plan", Decision: work.PlanApproved, Reason: "The new revision replaces the earlier scope.", ExpectedVersion: 1,
	})); err != nil {
		t.Fatal(err)
	}
	replacedContext, err := service.GetObjectiveContext(ctx, objective.ID)
	if err != nil {
		t.Fatal(err)
	}
	planStates := make(map[int]work.PlanCommitment)
	stepsByRevision := make(map[int]int)
	for _, candidate := range replacedContext.Plans {
		planStates[candidate.Plan.Revision] = candidate.Plan.CommitmentState
		stepsByRevision[candidate.Plan.Revision] = len(candidate.Steps)
	}
	if planStates[1] != work.PlanSuperseded || planStates[2] != work.PlanApproved {
		t.Fatalf("plan replacement states = %#v", planStates)
	}
	// A later revision replaces what a new run would be created from. It
	// rewrites neither revision's definition, and it does not touch the work
	// the first revision's run already materialized.
	if stepsByRevision[1] != 3 || stepsByRevision[2] != 1 {
		t.Fatalf("plan definitions were rewritten by the replacement: %#v", stepsByRevision)
	}
	firstRun, err := service.GetPlanRun(ctx, run.Run.ID)
	if err != nil {
		t.Fatal(err)
	}
	if firstRun.Run.Status != work.PlanRunActive || firstRun.Run.PlanID != planContext.Plan.ID || len(firstRun.WorkItems) != 3 {
		t.Fatalf("the first run changed when a later revision was approved: %#v", firstRun.Run)
	}
	otherObjective, err := app.UnwrapMutation(service.CreateObjective(ctx, app.CreateObjectiveCommand{
		ActorID: "human:sponsor", IdempotencyKey: "create-other-objective",
		Key: "OBJ-OTHER", Title: "Unrelated objective", DesiredOutcome: "Keep graph edges consistent.", Phase: work.ObjectivePlanning,
	}))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := app.UnwrapMutation(service.RecordContext(ctx, app.RecordContextCommand{
		ObjectiveID: otherObjective.ID, WorkItemID: itemIDs["TH-SKILL"], ActorID: "agent:planner", IdempotencyKey: "invalid-cross-objective-context",
		Kind: work.ContextFinding, Title: "Cross-objective context", Status: work.ContextRecorded,
	})); err == nil {
		t.Fatal("expected cross-objective context record to be rejected")
	}
	if _, err := app.UnwrapMutation(service.AskQuestion(ctx, app.AskQuestionCommand{
		ObjectiveID: otherObjective.ID, WorkItemID: itemIDs["TH-SKILL"], ActorID: "agent:planner", IdempotencyKey: "invalid-cross-objective-question", Question: "Cross-objective question?",
	})); err == nil {
		t.Fatal("expected cross-objective question to be rejected")
	}
	if _, err := app.UnwrapMutation(service.RecordDecision(ctx, app.RecordDecisionCommand{
		ObjectiveID: otherObjective.ID, WorkItemID: itemIDs["TH-SKILL"], ActorID: "human:sponsor", IdempotencyKey: "invalid-cross-objective-decision", Title: "Cross-objective decision", Decision: "Reject the invalid edge.",
	})); err == nil {
		t.Fatal("expected cross-objective decision to be rejected")
	}
}

type planningIDs struct{ next int }

func (ids *planningIDs) New() (string, error) {
	ids.next++
	return time.Date(2026, 8, 21, 15, 0, ids.next, 0, time.UTC).Format("20060102T150405.000000000"), nil
}

type planningClock struct{ next int }

func (clock *planningClock) Now() time.Time {
	clock.next++
	return time.Date(2026, 8, 21, 15, 0, 0, 0, time.UTC).Add(time.Duration(clock.next) * time.Second)
}
