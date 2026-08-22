package app

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/dennisschroeder/workgraph/internal/domain/authority"
	"github.com/dennisschroeder/workgraph/internal/domain/output"
	"github.com/dennisschroeder/workgraph/internal/domain/work"
	"github.com/dennisschroeder/workgraph/internal/ports"
)

func TestRepositoryIndependentSkillDesignSlice(t *testing.T) {
	clock := fixedClock{value: time.Date(2026, 8, 21, 12, 30, 0, 0, time.UTC)}
	store := newMemoryStore(clock.value)
	service := NewService(store, &sequenceIDs{}, clock)
	ctx := context.Background()

	objective, err := service.CreateObjective(ctx, CreateObjectiveCommand{
		ActorID:        "human:owner",
		Key:            "OBJ-SKILL",
		Title:          "Design a reusable interview-synthesis skill",
		DesiredOutcome: "A reviewed skill package that turns interview notes into a source-linked synthesis.",
		Phase:          work.ObjectivePlanning,
	})
	if err != nil {
		t.Fatal(err)
	}
	plan, err := service.CreatePlan(ctx, CreatePlanCommand{
		ActorID:         "human:owner",
		ObjectiveID:     objective.ID,
		Title:           "Interview-synthesis skill plan",
		Summary:         "Define the inputs, synthesis method, output contract, and evaluation rubric.",
		Revision:        1,
		CommitmentState: work.PlanDraft,
	})
	if err != nil {
		t.Fatal(err)
	}
	item, err := service.CreateWorkItem(ctx, CreateWorkItemCommand{
		ActorID:           "human:owner",
		Key:               "WG-1",
		ObjectiveID:       objective.ID,
		PlanID:            plan.ID,
		Title:             "Design the interview-synthesis skill",
		Description:       "Describe behavior, constraints, and evaluation without invoking an agent runtime.",
		Kind:              "skill_design",
		CommitmentState:   work.ItemProposed,
		ExecutionStatus:   work.StatusBacklog,
		Priority:          work.PriorityHigh,
		EstimatedScope:    work.ScopeSmall,
		ExecutionPolicy:   work.PolicyAgentMayPropose,
		RequiredActorKind: work.ActorAny,
		AttentionState:    work.AttentionNeedsHumanReview,
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = service.DefineExpectedOutput(ctx, DefineExpectedOutputCommand{
		ActorID:         "human:owner",
		WorkItemID:      item.ID,
		Name:            "Installable interview-synthesis skill",
		ProfileName:     "skill_package",
		ProfileVersion:  1,
		Contract:        json.RawMessage(`{"evaluation_cases":3,"validation":{"required":[{"kind":"evaluation","criterion_ref":"evaluation_cases"}]}}`),
		DestinationHint: "skills/interview-synthesis",
		Required:        true,
		Ordinal:         1,
		ExpectedVersion: item.Version,
		IdempotencyKey:  "define-skill-output",
	})
	if err != nil {
		t.Fatal(err)
	}

	contextResult, err := service.GetWorkItem(ctx, item.ID)
	if err != nil {
		t.Fatal(err)
	}
	if contextResult.Objective.ID != objective.ID || contextResult.Plan == nil || contextResult.Plan.ID != plan.ID || contextResult.WorkItem.ID != item.ID {
		t.Fatalf("retrieved context does not preserve the objective-plan-item chain: %#v", contextResult)
	}
	if len(contextResult.ExpectedOutputs) != 1 {
		t.Fatalf("expected one output, got %d", len(contextResult.ExpectedOutputs))
	}
	detail := contextResult.ExpectedOutputs[0]
	if detail.Profile.Name != "skill_package" || detail.Profile.Version != 1 || detail.Profile.LifecycleState != output.ProfileActive {
		t.Fatalf("unexpected exact profile binding: %#v", detail.Profile)
	}
}

func TestCreatePlanOnlyCreatesDrafts(t *testing.T) {
	clock := fixedClock{value: time.Date(2026, 8, 21, 12, 30, 0, 0, time.UTC)}
	store := newMemoryStore(clock.value)
	store.objectives["objective"] = work.Objective{ID: "objective"}
	service := NewService(store, &sequenceIDs{}, clock)
	_, err := service.CreatePlan(context.Background(), CreatePlanCommand{
		ActorID:         "human:owner",
		ObjectiveID:     "objective",
		Title:           "Premature plan",
		Revision:        1,
		CommitmentState: work.PlanProposed,
	})
	if err == nil {
		t.Fatal("expected proposed plan creation without proposal audit to be rejected")
	}
}

func TestCreateObjectiveCannotStartInExecution(t *testing.T) {
	clock := fixedClock{value: time.Date(2026, 8, 21, 12, 30, 0, 0, time.UTC)}
	store := newMemoryStore(clock.value)
	service := NewService(store, &sequenceIDs{}, clock)

	_, err := service.CreateObjective(context.Background(), CreateObjectiveCommand{
		ActorID:        "human:owner",
		Key:            "OBJ-PREMATURE",
		Title:          "Premature objective",
		DesiredOutcome: "Execution without an approved handoff",
		Phase:          work.ObjectiveExecution,
	})
	if err == nil {
		t.Fatal("expected objective creation in execution to be rejected")
	}
	if len(store.objectives) != 0 {
		t.Fatal("rejected objective was persisted")
	}
}

type fixedClock struct {
	value time.Time
}

func (clock fixedClock) Now() time.Time { return clock.value }

type sequenceIDs struct {
	next int
}

func (ids *sequenceIDs) New() (string, error) {
	ids.next++
	return fmt.Sprintf("00000000-0000-7000-8000-%012d", ids.next), nil
}

type memoryStore struct {
	objectives map[string]work.Objective
	plans      map[string]work.Plan
	items      map[string]work.WorkItem
	contexts   map[string]work.ContextRecord
	questions  map[string]work.Question
	decisions  map[string]work.Decision
	profiles   map[string]output.Profile
	expected   []output.ExpectedOutput
}

func newMemoryStore(createdAt time.Time) *memoryStore {
	profile := output.Profile{
		ID:             "0198ce12-1800-7000-8000-000000000004",
		Name:           "skill_package",
		Version:        1,
		LifecycleState: output.ProfileActive,
		Structure:      json.RawMessage(`{"required":["instructions"]}`),
		Semantics:      json.RawMessage(`{}`),
		Validation:     json.RawMessage(`{}`),
		BuiltIn:        true,
		CreatedAt:      createdAt,
	}
	return &memoryStore{
		objectives: make(map[string]work.Objective),
		plans:      make(map[string]work.Plan),
		items:      make(map[string]work.WorkItem),
		contexts:   make(map[string]work.ContextRecord),
		questions:  make(map[string]work.Question),
		decisions:  make(map[string]work.Decision),
		profiles:   map[string]output.Profile{"skill_package/1": profile},
	}
}

func (s *memoryStore) CreateContextRecord(_ context.Context, record work.ContextRecord) error {
	s.contexts[record.ID] = record
	return nil
}

func (s *memoryStore) ContextRecord(_ context.Context, id string) (work.ContextRecord, error) {
	record, ok := s.contexts[id]
	if !ok {
		return work.ContextRecord{}, ports.ErrNotFound
	}
	return record, nil
}

func (s *memoryStore) UpdateContextRecord(_ context.Context, record work.ContextRecord, _ int) error {
	s.contexts[record.ID] = record
	return nil
}

func (s *memoryStore) CreateQuestion(_ context.Context, question work.Question) error {
	s.questions[question.ID] = question
	return nil
}

func (s *memoryStore) Question(_ context.Context, id string) (work.Question, error) {
	question, ok := s.questions[id]
	if !ok {
		return work.Question{}, ports.ErrNotFound
	}
	return question, nil
}

func (s *memoryStore) UpdateQuestion(_ context.Context, question work.Question, _ int) error {
	s.questions[question.ID] = question
	return nil
}

func (s *memoryStore) CreateDecision(_ context.Context, decision work.Decision) error {
	s.decisions[decision.ID] = decision
	return nil
}

func (s *memoryStore) Decision(_ context.Context, id string) (work.Decision, error) {
	decision, ok := s.decisions[id]
	if !ok {
		return work.Decision{}, ports.ErrNotFound
	}
	return decision, nil
}

func (s *memoryStore) UpdateDecision(_ context.Context, decision work.Decision) error {
	s.decisions[decision.ID] = decision
	return nil
}

func (s *memoryStore) WithinTransaction(ctx context.Context, operation func(ports.Repository) error) error {
	return operation(s)
}

func (s *memoryStore) CreateObjective(_ context.Context, objective work.Objective) error {
	s.objectives[objective.ID] = objective
	return nil
}

func (s *memoryStore) Objective(_ context.Context, id string) (work.Objective, error) {
	value, ok := s.objectives[id]
	if !ok {
		return work.Objective{}, ports.ErrNotFound
	}
	return value, nil
}

func (s *memoryStore) HasApprovedPlan(_ context.Context, objectiveID string) (bool, error) {
	for _, plan := range s.plans {
		if plan.ObjectiveID == objectiveID && plan.CommitmentState == work.PlanApproved {
			return true, nil
		}
	}
	return false, nil
}

func (s *memoryStore) UpdateObjective(_ context.Context, objective work.Objective, _ int) error {
	s.objectives[objective.ID] = objective
	return nil
}

func (s *memoryStore) CreatePlan(_ context.Context, plan work.Plan) error {
	s.plans[plan.ID] = plan
	return nil
}

func (s *memoryStore) Plan(_ context.Context, id string) (work.Plan, error) {
	value, ok := s.plans[id]
	if !ok {
		return work.Plan{}, ports.ErrNotFound
	}
	return value, nil
}

func (s *memoryStore) LatestApprovedPlanRevision(_ context.Context, objectiveID string) (int, error) {
	latest := 0
	for _, plan := range s.plans {
		if plan.ObjectiveID == objectiveID && plan.CommitmentState == work.PlanApproved && plan.Revision > latest {
			latest = plan.Revision
		}
	}
	return latest, nil
}

func (s *memoryStore) SupersedeEarlierPlans(_ context.Context, objectiveID string, revision int, updatedAt time.Time) error {
	for id, plan := range s.plans {
		if plan.ObjectiveID == objectiveID && plan.Revision < revision && plan.CommitmentState == work.PlanApproved {
			plan.CommitmentState = work.PlanSuperseded
			plan.Version++
			plan.UpdatedAt = updatedAt
			s.plans[id] = plan
		}
	}
	for id, item := range s.items {
		plan := s.plans[item.PlanID]
		if plan.ObjectiveID == objectiveID && plan.Revision < revision && item.CommitmentState == work.ItemAccepted {
			item.CommitmentState = work.ItemSuperseded
			item.Version++
			item.UpdatedAt = updatedAt
			s.items[id] = item
		}
	}
	return nil
}

func (s *memoryStore) UpdatePlan(_ context.Context, plan work.Plan, _ int) error {
	s.plans[plan.ID] = plan
	return nil
}

func (s *memoryStore) SetPlanItemsCommitment(_ context.Context, planID string, state work.ItemCommitment, updatedAt time.Time) error {
	for id, item := range s.items {
		if item.PlanID == planID {
			item.CommitmentState = state
			item.UpdatedAt = updatedAt
			s.items[id] = item
		}
	}
	return nil
}

func (s *memoryStore) CreateApproval(context.Context, work.Approval) error { return nil }

func (s *memoryStore) CreateWorkItem(_ context.Context, item work.WorkItem) error {
	s.items[item.ID] = item
	return nil
}

func (s *memoryStore) WorkItem(_ context.Context, id string) (work.WorkItem, error) {
	value, ok := s.items[id]
	if !ok {
		return work.WorkItem{}, ports.ErrNotFound
	}
	return value, nil
}

func (s *memoryStore) AddWorkItemCapability(context.Context, string, string) error { return nil }

func (s *memoryStore) CreateActor(context.Context, work.Actor) error { return nil }
func (s *memoryStore) Actor(context.Context, string) (work.Actor, error) {
	return work.Actor{}, ports.ErrNotFound
}
func (s *memoryStore) CreateCapability(context.Context, work.Capability) error { return nil }
func (s *memoryStore) AssignActorCapability(context.Context, string, string) error {
	return nil
}
func (s *memoryStore) ActorHasCapabilities(context.Context, string, []string) (bool, error) {
	return true, nil
}
func (s *memoryStore) RequiredCapabilities(context.Context, string) ([]string, error) {
	return nil, nil
}
func (s *memoryStore) PlanApprovedForWorkItem(context.Context, string) (bool, error) {
	return true, nil
}
func (s *memoryStore) HasOpenBlocker(context.Context, string) (bool, error) { return false, nil }
func (s *memoryStore) WorkItemApprovalSatisfied(context.Context, string, string, time.Time) (bool, error) {
	return true, nil
}
func (s *memoryStore) CreateWorkItemExecutionApproval(context.Context, work.ExecutionApproval) error {
	return nil
}
func (s *memoryStore) ActiveClaim(context.Context, string, time.Time) (*work.Claim, error) {
	return nil, nil
}
func (s *memoryStore) ExpireClaims(context.Context, string, time.Time) ([]work.Claim, error) {
	return nil, nil
}
func (s *memoryStore) CreateClaim(context.Context, work.Claim) error { return nil }
func (s *memoryStore) Claim(context.Context, string) (work.Claim, error) {
	return work.Claim{}, ports.ErrNotFound
}
func (s *memoryStore) RenewClaim(context.Context, work.Claim, time.Time) error { return nil }
func (s *memoryStore) ReleaseClaim(context.Context, work.Claim) error          { return nil }
func (s *memoryStore) CreateProgressEntry(context.Context, work.ProgressEntry) error {
	return nil
}
func (s *memoryStore) Artifacts(context.Context, string) ([]output.Artifact, error) { return nil, nil }
func (s *memoryStore) RequiredExternalActionsSatisfied(context.Context, string) (bool, error) {
	return true, nil
}
func (s *memoryStore) CreateExternalAction(context.Context, authority.ExternalAction) error {
	return nil
}
func (s *memoryStore) ExternalAction(context.Context, string) (authority.ExternalAction, error) {
	return authority.ExternalAction{}, ports.ErrNotFound
}
func (s *memoryStore) UpdateExternalAction(context.Context, authority.ExternalAction, int) error {
	return nil
}
func (s *memoryStore) CreateExternalActionRevision(context.Context, authority.ExternalActionRevision) error {
	return nil
}
func (s *memoryStore) ExternalActionRevision(context.Context, string, int) (authority.ExternalActionRevision, error) {
	return authority.ExternalActionRevision{}, ports.ErrNotFound
}
func (s *memoryStore) CurrentExternalActionRevision(context.Context, string) (authority.ExternalActionRevision, error) {
	return authority.ExternalActionRevision{}, ports.ErrNotFound
}
func (s *memoryStore) CreateActionApproval(context.Context, authority.ActionApproval) error {
	return nil
}
func (s *memoryStore) ActionApproval(context.Context, string) (authority.ActionApproval, error) {
	return authority.ActionApproval{}, ports.ErrNotFound
}
func (s *memoryStore) UpdateActionApproval(context.Context, authority.ActionApproval) error {
	return nil
}
func (s *memoryStore) CreateAuthorityGrant(context.Context, authority.AuthorityGrant) error {
	return nil
}
func (s *memoryStore) AuthorityGrant(context.Context, string) (authority.AuthorityGrant, error) {
	return authority.AuthorityGrant{}, ports.ErrNotFound
}
func (s *memoryStore) AuthorityGrantByApproval(context.Context, string) (authority.AuthorityGrant, error) {
	return authority.AuthorityGrant{}, ports.ErrNotFound
}
func (s *memoryStore) UpdateAuthorityGrant(context.Context, authority.AuthorityGrant) error {
	return nil
}
func (s *memoryStore) AuthorityGrantForPrincipal(context.Context, string, int, string) (*authority.AuthorityGrant, error) {
	return nil, nil
}
func (s *memoryStore) CreateExternalActionExecution(context.Context, authority.ExternalActionExecution, string) error {
	return nil
}
func (s *memoryStore) ExternalActionExecution(context.Context, string) (authority.ExternalActionExecution, error) {
	return authority.ExternalActionExecution{}, ports.ErrNotFound
}
func (s *memoryStore) UpdateExternalActionExecution(context.Context, authority.ExternalActionExecution, string) error {
	return nil
}
func (s *memoryStore) IdempotencyRecord(context.Context, string, string) (ports.IdempotencyRecord, error) {
	return ports.IdempotencyRecord{}, ports.ErrNotFound
}
func (s *memoryStore) CreateIdempotencyRecord(context.Context, ports.IdempotencyRecord) error {
	return nil
}

func (s *memoryStore) OutputProfile(_ context.Context, name string, version int) (output.Profile, error) {
	value, ok := s.profiles[fmt.Sprintf("%s/%d", name, version)]
	if !ok {
		return output.Profile{}, ports.ErrNotFound
	}
	return value, nil
}

func (s *memoryStore) OutputProfileByID(_ context.Context, id string) (output.Profile, error) {
	for _, profile := range s.profiles {
		if profile.ID == id {
			return profile, nil
		}
	}
	return output.Profile{}, ports.ErrNotFound
}

func (s *memoryStore) LatestOutputProfileVersion(_ context.Context, name string) (int, error) {
	latest := 0
	for _, profile := range s.profiles {
		if profile.Name == name && profile.Version > latest {
			latest = profile.Version
		}
	}
	return latest, nil
}

func (s *memoryStore) CreateOutputProfile(_ context.Context, profile output.Profile) error {
	s.profiles[fmt.Sprintf("%s/%d", profile.Name, profile.Version)] = profile
	return nil
}

func (s *memoryStore) UpdateOutputProfile(ctx context.Context, profile output.Profile) error {
	return s.CreateOutputProfile(ctx, profile)
}

func (s *memoryStore) SupersedeOutputProfile(_ context.Context, id string) error {
	for key, profile := range s.profiles {
		if profile.ID == id && profile.LifecycleState == output.ProfileActive {
			profile.LifecycleState = output.ProfileSuperseded
			s.profiles[key] = profile
			return nil
		}
	}
	return ports.ErrVersionConflict
}

func (s *memoryStore) CreateExpectedOutput(_ context.Context, expected output.ExpectedOutput) error {
	s.expected = append(s.expected, expected)
	return nil
}

func (s *memoryStore) UpdateWorkItem(_ context.Context, item work.WorkItem, _ int) error {
	s.items[item.ID] = item
	return nil
}

func (s *memoryStore) CreateAcceptanceCriterion(context.Context, work.AcceptanceCriterion) error {
	return nil
}

func (s *memoryStore) AcceptanceCriterion(context.Context, string) (work.AcceptanceCriterion, error) {
	return work.AcceptanceCriterion{}, ports.ErrNotFound
}

func (s *memoryStore) UpdateAcceptanceCriterion(context.Context, work.AcceptanceCriterion) error {
	return nil
}

func (s *memoryStore) AcceptanceCriteriaSatisfied(context.Context, string) (bool, error) {
	return true, nil
}

func (s *memoryStore) CreateDependency(context.Context, work.Dependency) error { return nil }

func (s *memoryStore) DependencyCreatesCycle(context.Context, string, string) (bool, error) {
	return false, nil
}

func (s *memoryStore) HardDependenciesSatisfied(context.Context, string) (bool, error) {
	return true, nil
}

func (s *memoryStore) CreateActivity(context.Context, work.Activity) error { return nil }

func (s *memoryStore) ExpectedOutput(_ context.Context, id string) (output.ExpectedOutput, error) {
	for _, expected := range s.expected {
		if expected.ID == id {
			return expected, nil
		}
	}
	return output.ExpectedOutput{}, ports.ErrNotFound
}

func (s *memoryStore) NextOutputRevision(context.Context, string) (int, error) { return 1, nil }
func (s *memoryStore) CreateArtifact(context.Context, output.Artifact) error   { return nil }
func (s *memoryStore) ArtifactByURI(context.Context, string, string) (output.Artifact, error) {
	return output.Artifact{}, ports.ErrNotFound
}
func (s *memoryStore) CreateOutputRevision(context.Context, output.OutputRevision) error {
	return nil
}
func (s *memoryStore) OutputRevision(context.Context, string) (output.OutputRevision, error) {
	return output.OutputRevision{}, ports.ErrNotFound
}
func (s *memoryStore) UpdateOutputRevisionAcceptance(context.Context, output.OutputRevision) error {
	return nil
}
func (s *memoryStore) CreateValidationRecord(context.Context, output.ValidationRecord) error {
	return nil
}
func (s *memoryStore) ValidationRecords(context.Context, string) ([]output.ValidationRecord, error) {
	return nil, nil
}
func (s *memoryStore) CreateOutputRequirement(context.Context, output.OutputRequirement) error {
	return nil
}
func (s *memoryStore) OutputRequirementsSatisfied(context.Context, string) (bool, error) {
	return true, nil
}
func (s *memoryStore) ExpectedOutputsSatisfied(context.Context, string) (bool, error) {
	return true, nil
}

func (s *memoryStore) GetWorkItemContext(ctx context.Context, id string) (ports.WorkItemContext, error) {
	item, err := s.WorkItem(ctx, id)
	if err != nil {
		return ports.WorkItemContext{}, err
	}
	objective, err := s.Objective(ctx, item.ObjectiveID)
	if err != nil {
		return ports.WorkItemContext{}, err
	}
	var plan *work.Plan
	if item.PlanID != "" {
		loadedPlan, err := s.Plan(ctx, item.PlanID)
		if err != nil {
			return ports.WorkItemContext{}, err
		}
		plan = &loadedPlan
	}
	result := ports.WorkItemContext{Objective: objective, Plan: plan, WorkItem: item}
	for _, expected := range s.expected {
		if expected.WorkItemID != id {
			continue
		}
		for _, profile := range s.profiles {
			if profile.ID == expected.OutputProfileID {
				result.ExpectedOutputs = append(result.ExpectedOutputs, output.ExpectedOutputDetail{ExpectedOutput: expected, Profile: profile})
			}
		}
	}
	return result, nil
}

func (s *memoryStore) GetObjectiveContext(_ context.Context, id string) (ports.ObjectiveContext, error) {
	objective, ok := s.objectives[id]
	if !ok {
		return ports.ObjectiveContext{}, ports.ErrNotFound
	}
	return ports.ObjectiveContext{Objective: objective}, nil
}

func (s *memoryStore) ListOutputProfiles(context.Context) ([]output.Profile, error) {
	profiles := make([]output.Profile, 0, len(s.profiles))
	for _, profile := range s.profiles {
		profiles = append(profiles, profile)
	}
	return profiles, nil
}

func (s *memoryStore) ListReadyWork(context.Context) ([]ports.ReadyWorkItem, error) {
	return nil, nil
}

func (s *memoryStore) ListReadyWorkForActor(context.Context, string) ([]ports.ReadyWorkItem, error) {
	return nil, nil
}

func (s *memoryStore) ListActivity(context.Context, ports.ActivityFilter) ([]work.Activity, error) {
	return nil, nil
}

func (s *memoryStore) LatestActivitySequence(context.Context) (int64, error) {
	return 0, nil
}

func (s *memoryStore) ListAcceptedOutputs(context.Context, ports.AcceptedOutputFilter) ([]ports.AcceptedOutput, error) {
	return nil, nil
}

var _ ports.Store = (*memoryStore)(nil)
var _ ports.Repository = (*memoryStore)(nil)
