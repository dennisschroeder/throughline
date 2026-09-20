package work

import (
	"errors"
	"fmt"
	"strings"
	"time"
)

type ObjectivePhase string

const (
	ObjectiveIdea       ObjectivePhase = "idea"
	ObjectiveDiscovery  ObjectivePhase = "discovery"
	ObjectivePlanning   ObjectivePhase = "planning"
	ObjectiveExecution  ObjectivePhase = "execution"
	ObjectiveEvaluation ObjectivePhase = "evaluation"
	ObjectiveCompleted  ObjectivePhase = "completed"
	ObjectivePaused     ObjectivePhase = "paused"
	ObjectiveCancelled  ObjectivePhase = "cancelled"
)

type PlanCommitment string

const (
	PlanDraft      PlanCommitment = "draft"
	PlanProposed   PlanCommitment = "proposed"
	PlanApproved   PlanCommitment = "approved"
	PlanRejected   PlanCommitment = "rejected"
	PlanSuperseded PlanCommitment = "superseded"
)

type ItemCommitment string

const (
	ItemProposed   ItemCommitment = "proposed"
	ItemAccepted   ItemCommitment = "accepted"
	ItemRejected   ItemCommitment = "rejected"
	ItemSuperseded ItemCommitment = "superseded"
)

type ExecutionStatus string

const (
	StatusBacklog    ExecutionStatus = "backlog"
	StatusReady      ExecutionStatus = "ready"
	StatusInProgress ExecutionStatus = "in_progress"
	StatusReview     ExecutionStatus = "review"
	StatusDone       ExecutionStatus = "done"
	StatusCancelled  ExecutionStatus = "cancelled"
)

type Priority string

const (
	PriorityLow    Priority = "low"
	PriorityMedium Priority = "medium"
	PriorityHigh   Priority = "high"
	PriorityUrgent Priority = "urgent"
)

// MeasureBasis says whether a Measure's value was measured after the fact or
// estimated in advance; it does not itself distinguish scope from appetite.
type MeasureBasis string

const (
	MeasureEstimated MeasureBasis = "estimated"
	MeasureMeasured  MeasureBasis = "measured"
)

// Measure is a quantity the model stores and never interprets: Unit is an
// opaque string (tokens, euros, days, pages, ...), never compared across
// units and never gated on. The zero value means "no measure recorded" — a
// WorkItem's coarse EstimatedScope hint and an Objective's Appetite (what the
// work is worth spending, set before it starts) both use this same shape,
// answering different questions rather than replacing one another.
type Measure struct {
	Value float64
	Unit  string
	Basis MeasureBasis
}

func (m Measure) isZero() bool {
	return m == Measure{}
}

func validMeasure(m Measure) bool {
	if m.isZero() {
		return true
	}
	if strings.TrimSpace(m.Unit) == "" {
		return false
	}
	if m.Value < 0 {
		return false
	}
	return m.Basis == MeasureEstimated || m.Basis == MeasureMeasured
}

type EstimatedScope string

const (
	ScopeXS      EstimatedScope = "xs"
	ScopeSmall   EstimatedScope = "small"
	ScopeMedium  EstimatedScope = "medium"
	ScopeLarge   EstimatedScope = "large"
	ScopeUnknown EstimatedScope = "unknown"
)

type ExecutionPolicy string

const (
	PolicyHumanOnly            ExecutionPolicy = "human_only"
	PolicyAgentMayPropose      ExecutionPolicy = "agent_may_propose"
	PolicyApprovalRequired     ExecutionPolicy = "approval_required"
	PolicyAutonomousWithReport ExecutionPolicy = "autonomous_with_report"
)

type ActorKind string

const (
	ActorAny   ActorKind = "any"
	ActorHuman ActorKind = "human"
	ActorAgent ActorKind = "agent"
)

type AttentionState string

const (
	AttentionNone                 AttentionState = "none"
	AttentionNeedsHumanDecision   AttentionState = "needs_human_decision"
	AttentionNeedsHumanReview     AttentionState = "needs_human_review"
	AttentionNeedsClarification   AttentionState = "needs_clarification"
	AttentionInterventionRequired AttentionState = "intervention_required"
)

type Objective struct {
	ID             string
	Key            string
	Title          string
	Description    string
	DesiredOutcome string
	Phase          ObjectivePhase
	PriorPhase     ObjectivePhase
	Priority       Priority
	Appetite       Measure
	// Mode says how the objective ends and is fixed at creation; see
	// ObjectiveMode. It is independent of Phase.
	Mode ObjectiveMode
	// MaxConcurrentRuns caps how many of this objective's plan runs may be
	// active at once, across every plan revision. One — serial execution — is
	// the safe default a caller has to raise deliberately.
	MaxConcurrentRuns int
	// LastPhaseTransition is why the objective is in its phase: the most recent
	// transition's edge, reason, actor and time. Nil for an objective never
	// transitioned since reasons began to be stored.
	LastPhaseTransition *PhaseTransition
	UpdatedBy           string
	Version             int
	CreatedAt           time.Time
	UpdatedAt           time.Time
}

type PhaseTransition struct {
	From    ObjectivePhase
	To      ObjectivePhase
	Reason  string
	ActorID string
	At      time.Time
}

type Plan struct {
	ID          string
	ObjectiveID string
	Title       string
	Summary     string
	Revision    int
	// DerivedFromPlanRunID names the run whose experience produced this
	// revision, when one did. It is provenance and nothing else: no
	// observation of a run ever changes a plan, and a revision still only
	// becomes runnable by being approved on its own.
	DerivedFromPlanRunID string
	CommitmentState      PlanCommitment
	ProposedBy           string
	ProposedAt           time.Time
	ResolvedBy           string
	ResolvedAt           time.Time
	ResolutionReason     string
	Version              int
	CreatedAt            time.Time
	UpdatedAt            time.Time
}

type WorkItem struct {
	ID                string
	Key               string
	ObjectiveID       string
	PlanID            string
	ParentID          string
	Title             string
	Description       string
	Kind              string
	CommitmentState   ItemCommitment
	ExecutionStatus   ExecutionStatus
	Priority          Priority
	EstimatedScope    EstimatedScope
	Measure           Measure
	ExecutionPolicy   ExecutionPolicy
	RequiredActorKind ActorKind
	AttentionState    AttentionState
	// ReviewRequirements are the reviews done waits for; none means the gate
	// has nothing to check.
	ReviewRequirements []ReviewRequirement
	// Origin says where the item came from and therefore whether it may
	// execute at all; PlanRunID and OriginPlanStepID carry its provenance when
	// a run materialized it.
	Origin           WorkItemOrigin
	PlanRunID        string
	OriginPlanStepID string
	Version          int
	CreatedAt        time.Time
	UpdatedAt        time.Time
}

// NewObjective normalizes and validates a new objective. Mode and
// MaxConcurrentRuns default to the safe values — a finite objective running one
// plan run at a time — when the caller leaves them unset.
func NewObjective(objective Objective, now time.Time) (Objective, error) {
	objective.ID = strings.TrimSpace(objective.ID)
	objective.Key = strings.TrimSpace(objective.Key)
	objective.Title = strings.TrimSpace(objective.Title)
	objective.Description = strings.TrimSpace(objective.Description)
	objective.DesiredOutcome = strings.TrimSpace(objective.DesiredOutcome)
	if objective.Mode == "" {
		objective.Mode = ObjectiveFinite
	}
	if objective.MaxConcurrentRuns == 0 {
		objective.MaxConcurrentRuns = 1
	}
	objective.Version = 1
	objective.CreatedAt = now.UTC()
	objective.UpdatedAt = now.UTC()
	if err := objective.Validate(); err != nil {
		return Objective{}, err
	}
	return objective, nil
}

func (o Objective) Validate() error {
	if err := requireIdentity(o.ID, o.Key, o.Title); err != nil {
		return fmt.Errorf("objective: %w", err)
	}
	if !validObjectivePhase(o.Phase) {
		return fmt.Errorf("objective: invalid phase %q", o.Phase)
	}
	if !validPriority(o.Priority) {
		return fmt.Errorf("objective: invalid priority %q", o.Priority)
	}
	if !validMeasure(o.Appetite) {
		return fmt.Errorf("objective: invalid appetite %+v", o.Appetite)
	}
	if !ValidObjectiveMode(o.Mode) {
		return fmt.Errorf("objective: invalid mode %q", o.Mode)
	}
	if o.MaxConcurrentRuns < 1 {
		return fmt.Errorf("objective: max concurrent runs must be positive, not %d", o.MaxConcurrentRuns)
	}
	return nil
}

func NewPlan(id, objectiveID, title, summary string, revision int, state PlanCommitment, now time.Time) (Plan, error) {
	plan := Plan{
		ID:              strings.TrimSpace(id),
		ObjectiveID:     strings.TrimSpace(objectiveID),
		Title:           strings.TrimSpace(title),
		Summary:         strings.TrimSpace(summary),
		Revision:        revision,
		CommitmentState: state,
		Version:         1,
		CreatedAt:       now.UTC(),
		UpdatedAt:       now.UTC(),
	}
	if err := plan.Validate(); err != nil {
		return Plan{}, err
	}
	return plan, nil
}

func (p Plan) Validate() error {
	if p.ID == "" || p.ObjectiveID == "" || p.Title == "" {
		return errors.New("plan requires id, objective id, and title")
	}
	if p.Revision < 1 {
		return errors.New("plan revision must be positive")
	}
	if !validPlanCommitment(p.CommitmentState) {
		return fmt.Errorf("invalid plan commitment state %q", p.CommitmentState)
	}
	return nil
}

func NewWorkItem(item WorkItem, now time.Time) (WorkItem, error) {
	item.ID = strings.TrimSpace(item.ID)
	item.Key = strings.TrimSpace(item.Key)
	item.ObjectiveID = strings.TrimSpace(item.ObjectiveID)
	item.PlanID = strings.TrimSpace(item.PlanID)
	item.ParentID = strings.TrimSpace(item.ParentID)
	item.Title = strings.TrimSpace(item.Title)
	item.Description = strings.TrimSpace(item.Description)
	item.Kind = strings.TrimSpace(item.Kind)
	item.PlanRunID = strings.TrimSpace(item.PlanRunID)
	item.OriginPlanStepID = strings.TrimSpace(item.OriginPlanStepID)
	if item.Origin == "" {
		item.Origin = OriginUnplanned
	}
	item.Version = 1
	item.CreatedAt = now.UTC()
	item.UpdatedAt = now.UTC()
	if err := item.Validate(); err != nil {
		return WorkItem{}, err
	}
	return item, nil
}

func (w WorkItem) Validate() error {
	if err := requireIdentity(w.ID, w.Key, w.Title); err != nil {
		return fmt.Errorf("work item: %w", err)
	}
	if w.ObjectiveID == "" {
		return errors.New("work item requires objective id")
	}
	if w.Kind == "" {
		return errors.New("work item kind is required")
	}
	if !validItemCommitment(w.CommitmentState) {
		return fmt.Errorf("work item: invalid commitment state %q", w.CommitmentState)
	}
	if !validExecutionStatus(w.ExecutionStatus) {
		return fmt.Errorf("work item: invalid execution status %q", w.ExecutionStatus)
	}
	if !validPriority(w.Priority) {
		return fmt.Errorf("work item: invalid priority %q", w.Priority)
	}
	if !validEstimatedScope(w.EstimatedScope) {
		return fmt.Errorf("work item: invalid estimated scope %q", w.EstimatedScope)
	}
	if !validMeasure(w.Measure) {
		return fmt.Errorf("work item: invalid measure %+v", w.Measure)
	}
	if !validExecutionPolicy(w.ExecutionPolicy) {
		return fmt.Errorf("work item: invalid execution policy %q", w.ExecutionPolicy)
	}
	if !validActorKind(w.RequiredActorKind) {
		return fmt.Errorf("work item: invalid required actor kind %q", w.RequiredActorKind)
	}
	if !ValidAttentionState(w.AttentionState) {
		return fmt.Errorf("work item: invalid attention state %q", w.AttentionState)
	}
	if !ValidWorkItemOrigin(w.Origin) {
		return fmt.Errorf("work item: invalid origin %q", w.Origin)
	}
	if w.Origin.BelongsToRun() != (w.PlanRunID != "") {
		return errors.New("work item: work belonging to a plan run requires that run, and work that does not must not name one")
	}
	if (w.Origin == OriginPlanStep) != (w.OriginPlanStepID != "") {
		return errors.New("work item: only work materialized from a plan step names an origin plan step, and it always does")
	}
	return nil
}

func requireIdentity(id, key, title string) error {
	if id == "" || key == "" || title == "" {
		return errors.New("id, key, and title are required")
	}
	return nil
}

func validObjectivePhase(value ObjectivePhase) bool {
	return oneOf(value, ObjectiveIdea, ObjectiveDiscovery, ObjectivePlanning, ObjectiveExecution, ObjectiveEvaluation, ObjectiveCompleted, ObjectivePaused, ObjectiveCancelled)
}

func validPlanCommitment(value PlanCommitment) bool {
	return oneOf(value, PlanDraft, PlanProposed, PlanApproved, PlanRejected, PlanSuperseded)
}

func validItemCommitment(value ItemCommitment) bool {
	return oneOf(value, ItemProposed, ItemAccepted, ItemRejected, ItemSuperseded)
}

func validExecutionStatus(value ExecutionStatus) bool {
	return oneOf(value, StatusBacklog, StatusReady, StatusInProgress, StatusReview, StatusDone, StatusCancelled)
}

func validPriority(value Priority) bool {
	return oneOf(value, PriorityLow, PriorityMedium, PriorityHigh, PriorityUrgent)
}

func validEstimatedScope(value EstimatedScope) bool {
	return oneOf(value, ScopeXS, ScopeSmall, ScopeMedium, ScopeLarge, ScopeUnknown)
}

func validExecutionPolicy(value ExecutionPolicy) bool {
	return oneOf(value, PolicyHumanOnly, PolicyAgentMayPropose, PolicyApprovalRequired, PolicyAutonomousWithReport)
}

func validActorKind(value ActorKind) bool {
	return oneOf(value, ActorAny, ActorHuman, ActorAgent)
}

func ValidAttentionState(value AttentionState) bool {
	return oneOf(value, AttentionNone, AttentionNeedsHumanDecision, AttentionNeedsHumanReview, AttentionNeedsClarification, AttentionInterventionRequired)
}

func oneOf[T comparable](value T, allowed ...T) bool {
	for _, candidate := range allowed {
		if value == candidate {
			return true
		}
	}
	return false
}
