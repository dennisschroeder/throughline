package work

import (
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"
)

type ActorType string

const (
	ActorTypeHuman   ActorType = "human"
	ActorTypeAgent   ActorType = "agent"
	ActorTypeService ActorType = "service"
)

type Actor struct {
	ID          string
	Kind        ActorType
	DisplayName string
	CreatedAt   time.Time
}

func NewActor(actor Actor, now time.Time) (Actor, error) {
	actor.ID = strings.TrimSpace(actor.ID)
	actor.DisplayName = strings.TrimSpace(actor.DisplayName)
	actor.CreatedAt = now.UTC()
	if err := actor.Validate(); err != nil {
		return Actor{}, err
	}
	return actor, nil
}

func (a Actor) Validate() error {
	if a.ID == "" {
		return errors.New("actor requires an id")
	}
	if !oneOf(a.Kind, ActorTypeHuman, ActorTypeAgent, ActorTypeService) {
		return fmt.Errorf("actor: invalid kind %q", a.Kind)
	}
	return nil
}

type Capability struct {
	Slug        string
	Description string
}

func NewCapability(slug, description string) (Capability, error) {
	normalized, err := NormalizeCapabilitySlug(slug)
	if err != nil {
		return Capability{}, err
	}
	return Capability{Slug: normalized, Description: strings.TrimSpace(description)}, nil
}

func NormalizeCapabilitySlug(value string) (string, error) {
	value = strings.ToLower(strings.TrimSpace(value))
	value = strings.ReplaceAll(value, "-", "_")
	value = strings.Join(strings.Fields(value), "_")
	if value == "" {
		return "", errors.New("capability slug is required")
	}

	previousSeparator := false
	for index, character := range value {
		isLowercaseLetter := character >= 'a' && character <= 'z'
		isDigit := character >= '0' && character <= '9'
		if !isLowercaseLetter && !isDigit && character != '_' {
			return "", fmt.Errorf("capability slug %q contains invalid character %q", value, character)
		}
		if index == 0 && !isLowercaseLetter {
			return "", fmt.Errorf("capability slug %q must start with a letter", value)
		}
		if character == '_' {
			if previousSeparator {
				return "", fmt.Errorf("capability slug %q cannot contain consecutive separators", value)
			}
			previousSeparator = true
			continue
		}
		previousSeparator = false
	}
	if previousSeparator {
		return "", fmt.Errorf("capability slug %q cannot end with a separator", value)
	}
	return value, nil
}

const (
	MinClaimLeaseDuration = time.Minute
	MaxClaimLeaseDuration = 8 * time.Hour
)

type Claim struct {
	ID            string
	WorkItemID    string
	ActorID       string
	AcquiredAt    time.Time
	ExpiresAt     time.Time
	ReleasedAt    time.Time
	ReleaseReason string
}

type ExecutionApproval struct {
	ID                 string
	WorkItemID         string
	ApprovedForActorID string
	Request            string
	RequestedBy        string
	RequestedAt        time.Time
	ResolvedBy         string
	ResolvedAt         time.Time
	Rationale          string
	ExpiresAt          *time.Time
}

func NewExecutionApproval(approval ExecutionApproval, now time.Time) (ExecutionApproval, error) {
	approval.ID = strings.TrimSpace(approval.ID)
	approval.WorkItemID = strings.TrimSpace(approval.WorkItemID)
	approval.ApprovedForActorID = strings.TrimSpace(approval.ApprovedForActorID)
	approval.Request = strings.TrimSpace(approval.Request)
	approval.RequestedBy = strings.TrimSpace(approval.RequestedBy)
	approval.ResolvedBy = strings.TrimSpace(approval.ResolvedBy)
	approval.Rationale = strings.TrimSpace(approval.Rationale)
	if approval.ID == "" || approval.WorkItemID == "" || approval.ApprovedForActorID == "" || approval.Request == "" || approval.RequestedBy == "" || approval.ResolvedBy == "" || approval.Rationale == "" {
		return ExecutionApproval{}, errors.New("work item execution approval requires id, item, principal, request, requester, approver, and rationale")
	}
	approval.RequestedAt = now.UTC()
	approval.ResolvedAt = now.UTC()
	if approval.ExpiresAt != nil {
		expiresAt := approval.ExpiresAt.UTC()
		if !expiresAt.After(now) {
			return ExecutionApproval{}, errors.New("work item execution approval expiry must be in the future")
		}
		approval.ExpiresAt = &expiresAt
	}
	return approval, nil
}

func NewClaim(id, workItemID, actorID string, leaseDuration time.Duration, now time.Time) (Claim, error) {
	if err := validateClaimLeaseDuration(leaseDuration); err != nil {
		return Claim{}, err
	}
	claim := Claim{
		ID:         strings.TrimSpace(id),
		WorkItemID: strings.TrimSpace(workItemID),
		ActorID:    strings.TrimSpace(actorID),
		AcquiredAt: now.UTC(),
		ExpiresAt:  now.UTC().Add(leaseDuration),
	}
	if err := claim.Validate(); err != nil {
		return Claim{}, err
	}
	return claim, nil
}

func (c Claim) Validate() error {
	if c.ID == "" || c.WorkItemID == "" || c.ActorID == "" {
		return errors.New("claim requires id, work item id, and actor id")
	}
	if c.AcquiredAt.IsZero() || c.ExpiresAt.IsZero() || !c.ExpiresAt.After(c.AcquiredAt) {
		return errors.New("claim expiry must be after acquisition")
	}
	if c.ReleasedAt.IsZero() != (c.ReleaseReason == "") {
		return errors.New("claim release time and reason must be recorded together")
	}
	if !c.ReleasedAt.IsZero() && c.ReleasedAt.Before(c.AcquiredAt) {
		return errors.New("claim cannot be released before acquisition")
	}
	return nil
}

func RenewClaim(claim Claim, actorID string, extension time.Duration, now time.Time) (Claim, error) {
	if err := validateClaimOwnership(claim, actorID, now); err != nil {
		return Claim{}, err
	}
	if err := validateClaimLeaseDuration(extension); err != nil {
		return Claim{}, err
	}
	claim.ExpiresAt = claim.ExpiresAt.Add(extension)
	return claim, nil
}

func ReleaseClaim(claim Claim, actorID, reason string, now time.Time) (Claim, error) {
	if err := validateClaimOwnership(claim, actorID, now); err != nil {
		return Claim{}, err
	}
	reason = strings.TrimSpace(reason)
	if reason == "" {
		return Claim{}, errors.New("claim release requires a reason")
	}
	claim.ReleasedAt = now.UTC()
	claim.ReleaseReason = reason
	return claim, nil
}

func (c Claim) ExpiredAt(now time.Time) bool {
	return !now.UTC().Before(c.ExpiresAt)
}

func validateClaimLeaseDuration(duration time.Duration) error {
	if duration < MinClaimLeaseDuration || duration > MaxClaimLeaseDuration {
		return fmt.Errorf("claim lease duration must be between %s and %s", MinClaimLeaseDuration, MaxClaimLeaseDuration)
	}
	return nil
}

func validateClaimOwnership(claim Claim, actorID string, now time.Time) error {
	if err := claim.Validate(); err != nil {
		return err
	}
	actorID = strings.TrimSpace(actorID)
	if actorID == "" || actorID != claim.ActorID {
		return errors.New("claim operation requires the owning actor")
	}
	if !claim.ReleasedAt.IsZero() {
		return errors.New("claim has already been released")
	}
	if claim.ExpiredAt(now) {
		return errors.New("claim has expired")
	}
	return nil
}

type ProgressEntry struct {
	ID         string
	WorkItemID string
	ActorID    string
	Summary    string
	Completed  []string
	Remaining  []string
	Discovered []string
	Blocker    string
	CreatedAt  time.Time
}

func NewProgressEntry(entry ProgressEntry, now time.Time) (ProgressEntry, error) {
	entry.ID = strings.TrimSpace(entry.ID)
	entry.WorkItemID = strings.TrimSpace(entry.WorkItemID)
	entry.ActorID = strings.TrimSpace(entry.ActorID)
	entry.Summary = strings.TrimSpace(entry.Summary)
	entry.Blocker = strings.TrimSpace(entry.Blocker)
	var err error
	if entry.Completed, err = normalizeProgressPoints(entry.Completed); err != nil {
		return ProgressEntry{}, fmt.Errorf("progress completed: %w", err)
	}
	if entry.Remaining, err = normalizeProgressPoints(entry.Remaining); err != nil {
		return ProgressEntry{}, fmt.Errorf("progress remaining: %w", err)
	}
	if entry.Discovered, err = normalizeProgressPoints(entry.Discovered); err != nil {
		return ProgressEntry{}, fmt.Errorf("progress discovered: %w", err)
	}
	entry.CreatedAt = now.UTC()
	if err := entry.Validate(); err != nil {
		return ProgressEntry{}, err
	}
	return entry, nil
}

func (p ProgressEntry) Validate() error {
	if p.ID == "" || p.WorkItemID == "" || p.ActorID == "" || p.Summary == "" {
		return errors.New("progress entry requires id, work item id, actor id, and summary")
	}
	if utf8.RuneCountInString(p.Summary) > 500 {
		return errors.New("progress summary must be at most 500 characters")
	}
	if utf8.RuneCountInString(p.Blocker) > 500 {
		return errors.New("progress blocker must be at most 500 characters")
	}
	return nil
}

func normalizeProgressPoints(points []string) ([]string, error) {
	if len(points) > 20 {
		return nil, errors.New("must contain at most 20 entries")
	}
	normalized := make([]string, len(points))
	for index, point := range points {
		point = strings.TrimSpace(point)
		if point == "" {
			return nil, errors.New("entries must not be blank")
		}
		if utf8.RuneCountInString(point) > 500 {
			return nil, errors.New("entries must be at most 500 characters")
		}
		normalized[index] = point
	}
	return normalized, nil
}

type TransitionRequirementCode string

const (
	TransitionRequirementLifecycle          TransitionRequirementCode = "lifecycle_transition_allowed"
	TransitionRequirementObjectiveExecution TransitionRequirementCode = "objective_in_execution"
	TransitionRequirementPlanApproved       TransitionRequirementCode = "plan_approved"
	TransitionRequirementItemAccepted       TransitionRequirementCode = "work_item_accepted"
	TransitionRequirementAcceptanceCriteria TransitionRequirementCode = "acceptance_criteria_satisfied"
	TransitionRequirementHardDependencies   TransitionRequirementCode = "hard_dependencies_satisfied"
	TransitionRequirementExpectedOutputs    TransitionRequirementCode = "expected_outputs_satisfied"
	TransitionRequirementOutputRequirements TransitionRequirementCode = "output_requirements_satisfied"
	TransitionRequirementExternalActions    TransitionRequirementCode = "external_actions_satisfied"
	TransitionRequirementReview             TransitionRequirementCode = "review_requirements_satisfied"
)

type TransitionRequirement struct {
	Code    TransitionRequirementCode
	Message string
}

type TransitionGateFacts struct {
	ObjectivePhase              ObjectivePhase
	PlanApproved                bool
	ItemCommitment              ItemCommitment
	CurrentStatus               ExecutionStatus
	TargetStatus                ExecutionStatus
	AcceptanceCriteriaSatisfied bool
	HardDependenciesSatisfied   bool
	ExpectedOutputsSatisfied    bool
	OutputRequirementsSatisfied bool
	ExternalActionsSatisfied    bool
	ReviewRequirementsSatisfied bool
}

func EvaluateTransitionGate(facts TransitionGateFacts) []TransitionRequirement {
	requirements := make([]TransitionRequirement, 0, 10)
	if !validWorkItemTransition(facts.CurrentStatus, facts.TargetStatus) {
		requirements = append(requirements, TransitionRequirement{TransitionRequirementLifecycle, "work item lifecycle transition is not allowed"})
	}
	if facts.ObjectivePhase != ObjectiveExecution {
		requirements = append(requirements, TransitionRequirement{TransitionRequirementObjectiveExecution, "work item execution requires an objective in execution phase"})
	}
	if !facts.PlanApproved {
		requirements = append(requirements, TransitionRequirement{TransitionRequirementPlanApproved, "work item execution requires an approved plan"})
	}
	if facts.ItemCommitment != ItemAccepted {
		requirements = append(requirements, TransitionRequirement{TransitionRequirementItemAccepted, "work item execution requires accepted work"})
	}
	if facts.TargetStatus != StatusDone {
		return requirements
	}
	for _, requirement := range []struct {
		code      TransitionRequirementCode
		satisfied bool
		message   string
	}{
		{TransitionRequirementAcceptanceCriteria, facts.AcceptanceCriteriaSatisfied, "acceptance criteria are not satisfied"},
		{TransitionRequirementHardDependencies, facts.HardDependenciesSatisfied, "hard dependencies are not satisfied"},
		{TransitionRequirementExpectedOutputs, facts.ExpectedOutputsSatisfied, "expected outputs are not satisfied"},
		{TransitionRequirementOutputRequirements, facts.OutputRequirementsSatisfied, "output requirements are not satisfied"},
		{TransitionRequirementExternalActions, facts.ExternalActionsSatisfied, "required external actions are not satisfied"},
		{TransitionRequirementReview, facts.ReviewRequirementsSatisfied, "review requirements are not satisfied"},
	} {
		if !requirement.satisfied {
			requirements = append(requirements, TransitionRequirement{requirement.code, requirement.message})
		}
	}
	return requirements
}

type ClaimRequirementCode string

const (
	ClaimRequirementObjectiveExecution ClaimRequirementCode = "objective_in_execution"
	ClaimRequirementPlanApproved       ClaimRequirementCode = "plan_approved"
	ClaimRequirementItemAccepted       ClaimRequirementCode = "work_item_accepted"
	ClaimRequirementItemReady          ClaimRequirementCode = "work_item_ready"
	ClaimRequirementHardDependencies   ClaimRequirementCode = "hard_dependencies_satisfied"
	ClaimRequirementNoBlockers         ClaimRequirementCode = "no_open_blockers"
	ClaimRequirementOutputRequirements ClaimRequirementCode = "output_requirements_satisfied"
	ClaimRequirementActorKind          ClaimRequirementCode = "actor_kind_eligible"
	ClaimRequirementCapabilities       ClaimRequirementCode = "capabilities_satisfied"
	ClaimRequirementApproval           ClaimRequirementCode = "approval_satisfied"
	ClaimRequirementClaimAvailable     ClaimRequirementCode = "claim_available"
)

type ClaimRequirement struct {
	Code    ClaimRequirementCode
	Message string
}

type ClaimGateFacts struct {
	ObjectivePhase              ObjectivePhase
	PlanApproved                bool
	ItemCommitment              ItemCommitment
	ExecutionStatus             ExecutionStatus
	ExecutionPolicy             ExecutionPolicy
	RequiredActorKind           ActorKind
	Actor                       Actor
	HardDependenciesSatisfied   bool
	HasOpenBlocker              bool
	OutputRequirementsSatisfied bool
	CapabilitiesSatisfied       bool
	ApprovalSatisfied           bool
	ActiveClaim                 *Claim
	Now                         time.Time
}

func EvaluateClaimGate(facts ClaimGateFacts) []ClaimRequirement {
	requirements := make([]ClaimRequirement, 0, 11)
	for _, requirement := range []struct {
		code      ClaimRequirementCode
		satisfied bool
		message   string
	}{
		{ClaimRequirementObjectiveExecution, facts.ObjectivePhase == ObjectiveExecution, "claiming requires an objective in execution phase"},
		{ClaimRequirementPlanApproved, facts.PlanApproved, "claiming requires an approved plan"},
		{ClaimRequirementItemAccepted, facts.ItemCommitment == ItemAccepted, "claiming requires accepted work"},
		{ClaimRequirementItemReady, facts.ExecutionStatus == StatusReady, "claiming requires ready work"},
		{ClaimRequirementHardDependencies, facts.HardDependenciesSatisfied, "hard dependencies are not satisfied"},
		{ClaimRequirementNoBlockers, !facts.HasOpenBlocker, "work item has an open blocker"},
		{ClaimRequirementOutputRequirements, facts.OutputRequirementsSatisfied, "output requirements are not satisfied"},
		{ClaimRequirementActorKind, actorMatchesRequiredKind(facts.Actor.Kind, facts.RequiredActorKind), "actor kind is not eligible"},
		{ClaimRequirementCapabilities, facts.CapabilitiesSatisfied, "actor does not satisfy required capabilities"},
	} {
		if !requirement.satisfied {
			requirements = append(requirements, ClaimRequirement{requirement.code, requirement.message})
		}
	}
	if facts.ExecutionPolicy == PolicyApprovalRequired && !facts.ApprovalSatisfied {
		requirements = append(requirements, ClaimRequirement{ClaimRequirementApproval, "work item requires approval"})
	}
	if facts.ExecutionPolicy == PolicyHumanOnly && facts.Actor.Kind != ActorTypeHuman {
		requirements = append(requirements, ClaimRequirement{ClaimRequirementActorKind, "human-only work requires a human actor"})
	}
	if facts.ActiveClaim != nil && facts.ActiveClaim.ReleasedAt.IsZero() && !facts.ActiveClaim.ExpiredAt(facts.Now) {
		requirements = append(requirements, ClaimRequirement{ClaimRequirementClaimAvailable, "work item already has an active claim"})
	}
	return requirements
}

func actorMatchesRequiredKind(actorKind ActorType, requiredKind ActorKind) bool {
	switch requiredKind {
	case ActorAny:
		return oneOf(actorKind, ActorTypeHuman, ActorTypeAgent, ActorTypeService)
	case ActorHuman:
		return actorKind == ActorTypeHuman
	case ActorAgent:
		return actorKind == ActorTypeAgent
	default:
		return false
	}
}
