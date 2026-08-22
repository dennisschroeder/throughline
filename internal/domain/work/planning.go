package work

import (
	"errors"
	"fmt"
	"strings"
	"time"
)

type ContextKind string

const (
	ContextRequirement   ContextKind = "requirement"
	ContextConstraint    ContextKind = "constraint"
	ContextAssumption    ContextKind = "assumption"
	ContextFinding       ContextKind = "finding"
	ContextRisk          ContextKind = "risk"
	ContextSuccessMetric ContextKind = "success_metric"
)

type ContextStatus string

const (
	ContextProposed    ContextStatus = "proposed"
	ContextAccepted    ContextStatus = "accepted"
	ContextSuperseded  ContextStatus = "superseded"
	ContextWaived      ContextStatus = "waived"
	ContextUntested    ContextStatus = "untested"
	ContextValidating  ContextStatus = "validating"
	ContextValidated   ContextStatus = "validated"
	ContextInvalidated ContextStatus = "invalidated"
	ContextRecorded    ContextStatus = "recorded"
)

type ContextRecord struct {
	ID           string
	ObjectiveID  string
	WorkItemID   string
	Kind         ContextKind
	Title        string
	Body         string
	Status       ContextStatus
	Confidence   string
	SourceURI    string
	SupersedesID string
	Version      int
	CreatedAt    time.Time
	UpdatedAt    time.Time
	CreatedBy    string
	UpdatedBy    string
}

func TransitionContextRecord(record ContextRecord, target ContextStatus, actor string, now time.Time) (ContextRecord, error) {
	actor = strings.TrimSpace(actor)
	if actor == "" {
		return ContextRecord{}, errors.New("context transition requires an actor")
	}
	if !validContextTransition(record.Kind, record.Status, target) {
		return ContextRecord{}, fmt.Errorf("%s context cannot transition from %q to %q", record.Kind, record.Status, target)
	}
	record.Status = target
	record.Version++
	record.UpdatedAt = now.UTC()
	record.UpdatedBy = actor
	return record, nil
}

func SupersedeContextRecord(record ContextRecord, actor string, now time.Time) (ContextRecord, error) {
	actor = strings.TrimSpace(actor)
	if actor == "" || record.Status == ContextSuperseded {
		return ContextRecord{}, errors.New("context supersession requires an actor and current record")
	}
	record.Status = ContextSuperseded
	record.Version++
	record.UpdatedAt = now.UTC()
	record.UpdatedBy = actor
	return record, nil
}

func validContextTransition(kind ContextKind, current, target ContextStatus) bool {
	switch kind {
	case ContextAssumption:
		return (current == ContextUntested && target == ContextValidating) ||
			(current == ContextValidating && (target == ContextValidated || target == ContextInvalidated))
	case ContextRequirement, ContextConstraint, ContextRisk, ContextSuccessMetric:
		return (current == ContextProposed && target == ContextAccepted) ||
			(current == ContextAccepted && target == ContextWaived)
	default:
		return false
	}
}

func NewContextRecord(record ContextRecord, now time.Time) (ContextRecord, error) {
	record.ID = strings.TrimSpace(record.ID)
	record.ObjectiveID = strings.TrimSpace(record.ObjectiveID)
	record.WorkItemID = strings.TrimSpace(record.WorkItemID)
	record.Title = strings.TrimSpace(record.Title)
	record.Body = strings.TrimSpace(record.Body)
	record.Confidence = strings.TrimSpace(record.Confidence)
	record.SourceURI = strings.TrimSpace(record.SourceURI)
	record.SupersedesID = strings.TrimSpace(record.SupersedesID)
	record.CreatedBy = strings.TrimSpace(record.CreatedBy)
	record.Version = 1
	record.CreatedAt = now.UTC()
	record.UpdatedAt = now.UTC()
	if record.ID == "" || record.ObjectiveID == "" || record.Title == "" || record.CreatedBy == "" {
		return ContextRecord{}, errors.New("context record requires id, objective id, title, and creator")
	}
	if !validContextStatus(record.Kind, record.Status) {
		return ContextRecord{}, fmt.Errorf("invalid %s context status %q", record.Kind, record.Status)
	}
	return record, nil
}

func validContextStatus(kind ContextKind, status ContextStatus) bool {
	switch kind {
	case ContextAssumption:
		return status == ContextUntested || status == ContextValidating || status == ContextValidated || status == ContextInvalidated || status == ContextSuperseded
	case ContextFinding:
		return status == ContextRecorded || status == ContextSuperseded
	case ContextRequirement, ContextConstraint, ContextRisk, ContextSuccessMetric:
		return status == ContextProposed || status == ContextAccepted || status == ContextSuperseded || status == ContextWaived
	default:
		return false
	}
}

func TransitionObjective(objective Objective, target ObjectivePhase, reason string, now time.Time) (Objective, error) {
	if strings.TrimSpace(reason) == "" {
		return Objective{}, errors.New("objective transition requires a reason")
	}
	if !validObjectiveTransition(objective, target) {
		return Objective{}, fmt.Errorf("objective cannot transition from %q to %q", objective.Phase, target)
	}
	if target == ObjectivePaused {
		objective.PriorPhase = objective.Phase
	} else if objective.Phase == ObjectivePaused {
		objective.PriorPhase = ""
	}
	objective.Phase = target
	objective.Version++
	objective.UpdatedAt = now.UTC()
	return objective, nil
}

func validObjectiveTransition(objective Objective, target ObjectivePhase) bool {
	if target == ObjectiveCancelled {
		return objective.Phase != ObjectiveCompleted && objective.Phase != ObjectiveCancelled
	}
	if target == ObjectivePaused {
		return objective.Phase != ObjectivePaused && objective.Phase != ObjectiveCompleted && objective.Phase != ObjectiveCancelled
	}
	if objective.Phase == ObjectivePaused {
		return target == objective.PriorPhase
	}
	switch objective.Phase {
	case ObjectiveIdea:
		return target == ObjectiveDiscovery
	case ObjectiveDiscovery:
		return target == ObjectivePlanning
	case ObjectivePlanning:
		return target == ObjectiveExecution
	case ObjectiveExecution:
		return target == ObjectiveEvaluation || target == ObjectivePlanning
	case ObjectiveEvaluation:
		return target == ObjectiveCompleted || target == ObjectivePlanning || target == ObjectiveExecution
	default:
		return false
	}
}

func ReviewPlan(plan Plan, decision PlanCommitment, reviewer, reason string, now time.Time) (Plan, error) {
	reviewer = strings.TrimSpace(reviewer)
	reason = strings.TrimSpace(reason)
	if plan.CommitmentState != PlanProposed {
		return Plan{}, errors.New("only proposed plans can be reviewed")
	}
	if decision != PlanApproved && decision != PlanRejected {
		return Plan{}, errors.New("plan review must approve or reject")
	}
	if reviewer == "" || reason == "" {
		return Plan{}, errors.New("plan review requires reviewer and reason")
	}
	plan.CommitmentState = decision
	plan.ResolvedBy = reviewer
	plan.ResolvedAt = now.UTC()
	plan.ResolutionReason = reason
	plan.Version++
	plan.UpdatedAt = now.UTC()
	return plan, nil
}

type QuestionStatus string

const (
	QuestionOpen     QuestionStatus = "open"
	QuestionAnswered QuestionStatus = "answered"
	QuestionWaived   QuestionStatus = "waived"
)

type Question struct {
	ID                     string
	ObjectiveID            string
	WorkItemID             string
	Text                   string
	Status                 QuestionStatus
	Answer                 string
	RequiresHumanAttention bool
	Version                int
	CreatedBy              string
	ResolvedBy             string
	CreatedAt              time.Time
	ResolvedAt             time.Time
}

func NewQuestion(question Question, now time.Time) (Question, error) {
	question.ID = strings.TrimSpace(question.ID)
	question.ObjectiveID = strings.TrimSpace(question.ObjectiveID)
	question.WorkItemID = strings.TrimSpace(question.WorkItemID)
	question.Text = strings.TrimSpace(question.Text)
	question.CreatedBy = strings.TrimSpace(question.CreatedBy)
	if question.ID == "" || question.ObjectiveID == "" || question.Text == "" || question.CreatedBy == "" {
		return Question{}, errors.New("question requires id, objective id, text, and creator")
	}
	question.Status = QuestionOpen
	question.Version = 1
	question.CreatedAt = now.UTC()
	return question, nil
}

func AnswerQuestion(question Question, answer, actor string, now time.Time) (Question, error) {
	answer = strings.TrimSpace(answer)
	actor = strings.TrimSpace(actor)
	if question.Status != QuestionOpen {
		return Question{}, errors.New("only open questions can be answered")
	}
	if answer == "" || actor == "" {
		return Question{}, errors.New("question answer requires text and actor")
	}
	question.Status = QuestionAnswered
	question.Answer = answer
	question.ResolvedBy = actor
	question.ResolvedAt = now.UTC()
	question.Version++
	return question, nil
}

func WaiveQuestion(question Question, reason, actor string, now time.Time) (Question, error) {
	reason = strings.TrimSpace(reason)
	actor = strings.TrimSpace(actor)
	if question.Status != QuestionOpen {
		return Question{}, errors.New("only open questions can be waived")
	}
	if reason == "" || actor == "" {
		return Question{}, errors.New("question waiver requires reason and actor")
	}
	question.Status = QuestionWaived
	question.Answer = reason
	question.ResolvedBy = actor
	question.ResolvedAt = now.UTC()
	question.Version++
	return question, nil
}

type DecisionStatus string

const (
	DecisionProposed   DecisionStatus = "proposed"
	DecisionAccepted   DecisionStatus = "accepted"
	DecisionSuperseded DecisionStatus = "superseded"
)

type Decision struct {
	ID           string
	ObjectiveID  string
	WorkItemID   string
	Title        string
	Outcome      string
	Rationale    string
	Alternatives []string
	Status       DecisionStatus
	SupersedesID string
	DecidedBy    string
	DecidedAt    time.Time
	CreatedAt    time.Time
}

func NewAcceptedDecision(decision Decision, now time.Time) (Decision, error) {
	decision.ID = strings.TrimSpace(decision.ID)
	decision.ObjectiveID = strings.TrimSpace(decision.ObjectiveID)
	decision.WorkItemID = strings.TrimSpace(decision.WorkItemID)
	decision.Title = strings.TrimSpace(decision.Title)
	decision.Outcome = strings.TrimSpace(decision.Outcome)
	decision.Rationale = strings.TrimSpace(decision.Rationale)
	decision.SupersedesID = strings.TrimSpace(decision.SupersedesID)
	decision.DecidedBy = strings.TrimSpace(decision.DecidedBy)
	if decision.ID == "" || decision.ObjectiveID == "" || decision.Title == "" || decision.Outcome == "" || decision.DecidedBy == "" {
		return Decision{}, errors.New("decision requires id, objective id, title, outcome, and deciding actor")
	}
	decision.Status = DecisionAccepted
	decision.DecidedAt = now.UTC()
	decision.CreatedAt = now.UTC()
	return decision, nil
}

func SupersedeDecision(decision Decision) (Decision, error) {
	if decision.Status != DecisionAccepted {
		return Decision{}, errors.New("only accepted decisions can be superseded")
	}
	decision.Status = DecisionSuperseded
	return decision, nil
}

type ApprovalStatus string

const (
	ApprovalApproved ApprovalStatus = "approved"
	ApprovalRejected ApprovalStatus = "rejected"
)

type Approval struct {
	ID          string
	ObjectiveID string
	PlanID      string
	Request     string
	Status      ApprovalStatus
	RequestedBy string
	RequestedAt time.Time
	ResolvedBy  string
	ResolvedAt  time.Time
	Rationale   string
}

func NewPlanApproval(id string, plan Plan) (Approval, error) {
	id = strings.TrimSpace(id)
	if id == "" {
		return Approval{}, errors.New("plan approval requires an id")
	}
	if strings.TrimSpace(plan.ProposedBy) == "" || plan.ProposedAt.IsZero() || strings.TrimSpace(plan.ResolvedBy) == "" || plan.ResolvedAt.IsZero() {
		return Approval{}, errors.New("plan approval requires proposal and resolution audit fields")
	}
	status := ApprovalRejected
	if plan.CommitmentState == PlanApproved {
		status = ApprovalApproved
	} else if plan.CommitmentState != PlanRejected {
		return Approval{}, errors.New("plan approval requires a resolved plan")
	}
	return Approval{
		ID:          id,
		ObjectiveID: plan.ObjectiveID,
		PlanID:      plan.ID,
		Request:     "Review plan revision",
		Status:      status,
		RequestedBy: plan.ProposedBy,
		RequestedAt: plan.ProposedAt,
		ResolvedBy:  plan.ResolvedBy,
		ResolvedAt:  plan.ResolvedAt,
		Rationale:   plan.ResolutionReason,
	}, nil
}
