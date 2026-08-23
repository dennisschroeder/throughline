package ports

import (
	"context"
	"errors"
	"time"

	"github.com/dennisschroeder/workgraph/internal/domain/authority"
	"github.com/dennisschroeder/workgraph/internal/domain/output"
	"github.com/dennisschroeder/workgraph/internal/domain/work"
)

var ErrNotFound = errors.New("not found")
var ErrVersionConflict = errors.New("version conflict")
var ErrClaimConflict = errors.New("claim conflict")
var ErrIdempotencyMismatch = errors.New("idempotency key reused with a different request")

type IDGenerator interface {
	New() (string, error)
}

type Clock interface {
	Now() time.Time
}

type Repository interface {
	CreateObjective(context.Context, work.Objective) error
	Objective(ctx context.Context, id string) (work.Objective, error)
	HasApprovedPlan(ctx context.Context, objectiveID string) (bool, error)
	UpdateObjective(ctx context.Context, objective work.Objective, expectedVersion int) error
	CreateContextRecord(context.Context, work.ContextRecord) error
	ContextRecord(ctx context.Context, id string) (work.ContextRecord, error)
	UpdateContextRecord(ctx context.Context, record work.ContextRecord, expectedVersion int) error
	CreateQuestion(context.Context, work.Question) error
	Question(ctx context.Context, id string) (work.Question, error)
	UpdateQuestion(ctx context.Context, question work.Question, expectedVersion int) error
	CreateDecision(context.Context, work.Decision) error
	Decision(ctx context.Context, id string) (work.Decision, error)
	UpdateDecision(context.Context, work.Decision) error
	CreatePlan(context.Context, work.Plan) error
	Plan(ctx context.Context, id string) (work.Plan, error)
	LatestPlanRevision(ctx context.Context, objectiveID string) (int, error)
	LatestApprovedPlanRevision(ctx context.Context, objectiveID string) (int, error)
	SupersedeEarlierPlans(ctx context.Context, objectiveID string, revision int, updatedAt time.Time) error
	UpdatePlan(ctx context.Context, plan work.Plan, expectedVersion int) error
	SetPlanItemsCommitment(ctx context.Context, planID string, state work.ItemCommitment, updatedAt time.Time) error
	CreateApproval(context.Context, work.Approval) error
	Approval(ctx context.Context, id string) (work.Approval, error)
	UpdateApproval(ctx context.Context, approval work.Approval, expectedVersion int) error
	CreateWorkItem(context.Context, work.WorkItem) error
	WorkItem(ctx context.Context, id string) (work.WorkItem, error)
	UpdateWorkItem(ctx context.Context, item work.WorkItem, expectedVersion int) error
	WorkItemParentCreatesCycle(ctx context.Context, workItemID, parentID string) (bool, error)
	CreateAcceptanceCriterion(context.Context, work.AcceptanceCriterion) error
	AcceptanceCriterion(ctx context.Context, id string) (work.AcceptanceCriterion, error)
	UpdateAcceptanceCriterion(context.Context, work.AcceptanceCriterion) error
	AcceptanceCriteriaSatisfied(ctx context.Context, workItemID string) (bool, error)
	CreateDependency(context.Context, work.Dependency) error
	DeleteDependency(ctx context.Context, workItemID, dependsOnItemID string, kind work.DependencyKind) error
	DependencyCreatesCycle(ctx context.Context, workItemID, dependsOnItemID string) (bool, error)
	HardDependenciesSatisfied(ctx context.Context, workItemID string) (bool, error)
	CreateActivity(context.Context, work.Activity) error
	AddWorkItemCapability(ctx context.Context, workItemID, capability string) error
	ReplaceWorkItemCapabilities(ctx context.Context, workItemID string, capabilities []string) error
	CreateActor(context.Context, work.Actor) error
	Actor(ctx context.Context, id string) (work.Actor, error)
	CreateCapability(context.Context, work.Capability) error
	AssignActorCapability(ctx context.Context, actorID, capability string) error
	ActorHasCapabilities(ctx context.Context, actorID string, capabilities []string) (bool, error)
	RequiredCapabilities(ctx context.Context, workItemID string) ([]string, error)
	PlanApprovedForWorkItem(ctx context.Context, workItemID string) (bool, error)
	HasOpenBlocker(ctx context.Context, workItemID string) (bool, error)
	CreateManualBlocker(context.Context, work.ManualBlocker) error
	ManualBlocker(ctx context.Context, id string) (work.ManualBlocker, error)
	UpdateManualBlocker(context.Context, work.ManualBlocker) error
	WorkItemApprovalSatisfied(ctx context.Context, workItemID, actorID string, now time.Time) (bool, error)
	CreateWorkItemExecutionApproval(context.Context, work.ExecutionApproval) error
	ActiveClaim(ctx context.Context, workItemID string, now time.Time) (*work.Claim, error)
	ExpireClaims(ctx context.Context, workItemID string, now time.Time) ([]work.Claim, error)
	CreateClaim(context.Context, work.Claim) error
	Claim(ctx context.Context, id string) (work.Claim, error)
	RenewClaim(ctx context.Context, claim work.Claim, now time.Time) error
	ReleaseClaim(context.Context, work.Claim) error
	CreateProgressEntry(context.Context, work.ProgressEntry) error
	Artifacts(ctx context.Context, workItemID string) ([]output.Artifact, error)
	RequiredExternalActionsSatisfied(ctx context.Context, workItemID string) (bool, error)
	CreateExternalAction(context.Context, authority.ExternalAction) error
	ExternalAction(ctx context.Context, id string) (authority.ExternalAction, error)
	UpdateExternalAction(ctx context.Context, action authority.ExternalAction, expectedVersion int) error
	CreateExternalActionRevision(context.Context, authority.ExternalActionRevision) error
	ExternalActionRevision(ctx context.Context, actionID string, revision int) (authority.ExternalActionRevision, error)
	CurrentExternalActionRevision(ctx context.Context, actionID string) (authority.ExternalActionRevision, error)
	CreateActionApproval(context.Context, authority.ActionApproval) error
	ActionApproval(ctx context.Context, id string) (authority.ActionApproval, error)
	UpdateActionApproval(context.Context, authority.ActionApproval) error
	CreateAuthorityGrant(context.Context, authority.AuthorityGrant) error
	AuthorityGrant(ctx context.Context, id string) (authority.AuthorityGrant, error)
	AuthorityGrantByApproval(ctx context.Context, approvalID string) (authority.AuthorityGrant, error)
	UpdateAuthorityGrant(context.Context, authority.AuthorityGrant) error
	AuthorityGrantForPrincipal(ctx context.Context, actionID string, revision int, principalID string) (*authority.AuthorityGrant, error)
	CreateExternalActionExecution(context.Context, authority.ExternalActionExecution, string) error
	ExternalActionExecution(ctx context.Context, id string) (authority.ExternalActionExecution, error)
	UpdateExternalActionExecution(context.Context, authority.ExternalActionExecution, string) error
	IdempotencyRecord(ctx context.Context, actorID, key string) (IdempotencyRecord, error)
	CreateIdempotencyRecord(context.Context, IdempotencyRecord) error
	LatestActivitySequence(context.Context) (int64, error)
	OutputProfile(ctx context.Context, name string, version int) (output.Profile, error)
	OutputProfileByID(ctx context.Context, id string) (output.Profile, error)
	LatestOutputProfileVersion(ctx context.Context, name string) (int, error)
	CreateOutputProfile(context.Context, output.Profile) error
	UpdateOutputProfile(context.Context, output.Profile) error
	SupersedeOutputProfile(ctx context.Context, id string) error
	CreateExpectedOutput(context.Context, output.ExpectedOutput) error
	ExpectedOutput(ctx context.Context, id string) (output.ExpectedOutput, error)
	NextOutputRevision(ctx context.Context, expectedOutputID string) (int, error)
	CreateArtifact(context.Context, output.Artifact) error
	ArtifactByURI(ctx context.Context, workItemID, uri string) (output.Artifact, error)
	CreateOutputRevision(context.Context, output.OutputRevision) error
	OutputRevision(ctx context.Context, id string) (output.OutputRevision, error)
	UpdateOutputRevisionAcceptance(context.Context, output.OutputRevision) error
	CreateValidationRecord(context.Context, output.ValidationRecord) error
	ValidationRecords(ctx context.Context, outputRevisionID string) ([]output.ValidationRecord, error)
	CreateOutputRequirement(context.Context, output.OutputRequirement) error
	OutputRequirementsSatisfied(ctx context.Context, workItemID string) (bool, error)
	ExpectedOutputsSatisfied(ctx context.Context, workItemID string) (bool, error)
}

type Store interface {
	WithinTransaction(context.Context, func(Repository) error) error
	GetWorkItemContext(ctx context.Context, id string) (WorkItemContext, error)
	ListWorkItemContexts(ctx context.Context) ([]WorkItemContext, error)
	GetObjectiveContext(ctx context.Context, id string) (ObjectiveContext, error)
	SelectObjectiveContext(ctx context.Context, query ObjectiveContextSelectionQuery) (ObjectiveContextSelection, error)
	ListOutputProfiles(ctx context.Context) ([]output.Profile, error)
	ListReadyWork(ctx context.Context) ([]ReadyWorkItem, error)
	ListReadyWorkForActor(ctx context.Context, actorID string) ([]ReadyWorkItem, error)
	ListActivity(ctx context.Context, filter ActivityFilter) ([]work.Activity, error)
	LatestActivitySequence(ctx context.Context) (int64, error)
	ListAcceptedOutputs(ctx context.Context, filter AcceptedOutputFilter) ([]AcceptedOutput, error)
	IdempotencyRecord(ctx context.Context, actorID, key string) (IdempotencyRecord, error)
}

type WorkItemContext struct {
	Objective            work.Objective
	Plan                 *work.Plan
	WorkItem             work.WorkItem
	RequiredCapabilities []string
	ExpectedOutputs      []output.ExpectedOutputDetail
	AcceptanceCriteria   []work.AcceptanceCriterion
	Dependencies         []work.Dependency
	OutputRequirements   []output.OutputRequirement
	OutputRevisions      []OutputRevisionDetail
	Claims               []work.Claim
	Progress             []work.ProgressEntry
	Artifacts            []output.Artifact
	ExternalActions      []ExternalActionDetail
}

type OutputRevisionDetail struct {
	Revision    output.OutputRevision
	Artifacts   []output.Artifact
	Validations []output.ValidationRecord
}

type AcceptedOutput struct {
	Revision       output.OutputRevision
	ExpectedOutput output.ExpectedOutput
	Profile        output.Profile
	Artifacts      []output.Artifact
}

type AcceptedOutputFilter struct {
	ProfileName       string
	VersionConstraint string
	ObjectiveID       string
	ProducedBy        string
	AcceptedSince     time.Time
	Limit             int
}

type ReadyWorkItem struct {
	Objective work.Objective
	WorkItem  work.WorkItem
}

type ActivityFilter struct {
	WorkItemID  string
	ObjectiveID string
	Since       int64
	Limit       int
}

type IdempotencyRecord struct {
	ActorID     string
	Key         string
	Operation   string
	RequestHash string
	Response    []byte
	CreatedAt   time.Time
}

type ExternalActionDetail struct {
	Action     authority.ExternalAction
	Revision   authority.ExternalActionRevision
	Grants     []authority.AuthorityGrant
	Executions []authority.ExternalActionExecution
}

type PlannedWorkItem struct {
	WorkItem             work.WorkItem
	RequiredCapabilities []string
	ExpectedOutputs      []output.ExpectedOutputDetail
	OutputRequirements   []output.OutputRequirement
	ExternalActions      []authority.ExternalAction
}

type PlanContext struct {
	Plan  work.Plan
	Items []PlannedWorkItem
}

type ObjectiveContext struct {
	Objective      work.Objective
	ContextRecords []work.ContextRecord
	Plans          []PlanContext
	Questions      []work.Question
	Decisions      []work.Decision
	Approvals      []work.Approval
}

// ObjectiveContextSelectionQuery identifies the bounded actor-aware continuation view.
type ObjectiveContextSelectionQuery struct {
	ObjectiveID string
	ActorID     string
	Limit       int
}

// ObjectiveContextSelection is read from one database snapshot so a continuation
// view cannot combine an objective from one revision with work from another.
type ObjectiveContextSelection struct {
	Context       ObjectiveContext
	WorkItems     []WorkItemContext
	RecentChanges []work.Activity
}
