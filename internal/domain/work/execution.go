package work

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

type AcceptanceCriterionStatus string

const (
	AcceptancePending   AcceptanceCriterionStatus = "pending"
	AcceptanceSatisfied AcceptanceCriterionStatus = "satisfied"
	AcceptanceWaived    AcceptanceCriterionStatus = "waived"
)

type AcceptanceCriterion struct {
	ID                  string
	WorkItemID          string
	Ordinal             int
	Text                string
	Required            bool
	Status              AcceptanceCriterionStatus
	ResolvedBy          string
	ResolvedAt          time.Time
	ResolutionRationale string
}

func NewAcceptanceCriterion(criterion AcceptanceCriterion) (AcceptanceCriterion, error) {
	criterion.ID = strings.TrimSpace(criterion.ID)
	criterion.WorkItemID = strings.TrimSpace(criterion.WorkItemID)
	criterion.Text = strings.TrimSpace(criterion.Text)
	if criterion.ID == "" || criterion.WorkItemID == "" || criterion.Text == "" {
		return AcceptanceCriterion{}, errors.New("acceptance criterion requires id, work item id, and text")
	}
	if criterion.Ordinal < 1 {
		return AcceptanceCriterion{}, errors.New("acceptance criterion ordinal must be positive")
	}
	criterion.Status = AcceptancePending
	criterion.ResolvedBy = ""
	criterion.ResolvedAt = time.Time{}
	criterion.ResolutionRationale = ""
	return criterion, nil
}

func ResolveAcceptanceCriterion(criterion AcceptanceCriterion, target AcceptanceCriterionStatus, actor, rationale string, now time.Time) (AcceptanceCriterion, error) {
	actor = strings.TrimSpace(actor)
	rationale = strings.TrimSpace(rationale)
	if criterion.Status != AcceptancePending {
		return AcceptanceCriterion{}, errors.New("only pending acceptance criteria can be resolved")
	}
	if target != AcceptanceSatisfied && target != AcceptanceWaived {
		return AcceptanceCriterion{}, fmt.Errorf("acceptance criterion cannot resolve to %q", target)
	}
	if actor == "" || rationale == "" {
		return AcceptanceCriterion{}, errors.New("acceptance criterion resolution requires actor and rationale")
	}
	criterion.Status = target
	criterion.ResolvedBy = actor
	criterion.ResolvedAt = now.UTC()
	criterion.ResolutionRationale = rationale
	return criterion, nil
}

type DependencyKind string

const (
	DependencyHard    DependencyKind = "hard"
	DependencySoft    DependencyKind = "soft"
	DependencyRelated DependencyKind = "related"
)

type Dependency struct {
	ID              string
	WorkItemID      string
	DependsOnItemID string
	Kind            DependencyKind
	Note            string
	CreatedBy       string
	CreatedAt       time.Time
}

func NewDependency(dependency Dependency, now time.Time) (Dependency, error) {
	dependency.ID = strings.TrimSpace(dependency.ID)
	dependency.WorkItemID = strings.TrimSpace(dependency.WorkItemID)
	dependency.DependsOnItemID = strings.TrimSpace(dependency.DependsOnItemID)
	dependency.Note = strings.TrimSpace(dependency.Note)
	dependency.CreatedBy = strings.TrimSpace(dependency.CreatedBy)
	if dependency.ID == "" || dependency.WorkItemID == "" || dependency.DependsOnItemID == "" || dependency.CreatedBy == "" {
		return Dependency{}, errors.New("dependency requires id, work item id, prerequisite item id, and creator")
	}
	if dependency.WorkItemID == dependency.DependsOnItemID {
		return Dependency{}, errors.New("work item cannot depend on itself")
	}
	if !oneOf(dependency.Kind, DependencyHard, DependencySoft, DependencyRelated) {
		return Dependency{}, fmt.Errorf("invalid dependency kind %q", dependency.Kind)
	}
	dependency.CreatedAt = now.UTC()
	return dependency, nil
}

type Activity struct {
	Sequence    int64
	ID          string
	EntityKind  string
	EntityID    string
	WorkItemID  string
	ActorID     string
	EventType   string
	Summary     string
	PayloadJSON json.RawMessage
	CreatedAt   time.Time
}

func NewActivity(activity Activity, now time.Time) (Activity, error) {
	activity.ID = strings.TrimSpace(activity.ID)
	activity.EntityKind = strings.TrimSpace(activity.EntityKind)
	activity.EntityID = strings.TrimSpace(activity.EntityID)
	activity.WorkItemID = strings.TrimSpace(activity.WorkItemID)
	activity.ActorID = strings.TrimSpace(activity.ActorID)
	activity.EventType = strings.TrimSpace(activity.EventType)
	activity.Summary = strings.TrimSpace(activity.Summary)
	if activity.Sequence < 0 {
		return Activity{}, errors.New("activity sequence cannot be negative")
	}
	if activity.ID == "" || activity.EntityKind == "" || activity.EntityID == "" || activity.ActorID == "" || activity.EventType == "" || activity.Summary == "" {
		return Activity{}, errors.New("activity requires id, entity kind, entity id, actor, event type, and summary")
	}
	payload, err := normalizeJSONObject(activity.PayloadJSON)
	if err != nil {
		return Activity{}, fmt.Errorf("activity payload: %w", err)
	}
	activity.PayloadJSON = payload
	activity.CreatedAt = now.UTC()
	return activity, nil
}

func normalizeJSONObject(value json.RawMessage) (json.RawMessage, error) {
	if len(bytes.TrimSpace(value)) == 0 {
		return json.RawMessage("{}"), nil
	}
	var object map[string]json.RawMessage
	if err := json.Unmarshal(value, &object); err != nil {
		return nil, errors.New("must be a valid JSON object")
	}
	if object == nil {
		return nil, errors.New("must be a JSON object")
	}
	var compact bytes.Buffer
	if err := json.Compact(&compact, value); err != nil {
		return nil, errors.New("must be a valid JSON object")
	}
	return json.RawMessage(compact.Bytes()), nil
}

func TransitionWorkItem(item WorkItem, target ExecutionStatus, actor, reason string, now time.Time) (WorkItem, error) {
	actor = strings.TrimSpace(actor)
	reason = strings.TrimSpace(reason)
	if actor == "" {
		return WorkItem{}, errors.New("work item transition requires an actor")
	}
	if !validExecutionStatus(item.ExecutionStatus) || !validExecutionStatus(target) {
		return WorkItem{}, fmt.Errorf("work item cannot transition from %q to %q", item.ExecutionStatus, target)
	}
	if !validWorkItemTransition(item.ExecutionStatus, target) {
		return WorkItem{}, fmt.Errorf("work item cannot transition from %q to %q", item.ExecutionStatus, target)
	}
	if workItemTransitionRequiresReason(item.ExecutionStatus, target) && reason == "" {
		return WorkItem{}, errors.New("work item transition requires a reason")
	}
	item.ExecutionStatus = target
	item.Version++
	item.UpdatedAt = now.UTC()
	return item, nil
}

func validWorkItemTransition(current, target ExecutionStatus) bool {
	if target == StatusCancelled {
		return current == StatusBacklog || current == StatusReady || current == StatusInProgress || current == StatusReview
	}
	switch current {
	case StatusBacklog:
		return target == StatusReady
	case StatusReady:
		return target == StatusInProgress
	case StatusInProgress:
		return target == StatusReady || target == StatusReview
	case StatusReview:
		return target == StatusInProgress || target == StatusDone
	default:
		return false
	}
}

func workItemTransitionRequiresReason(current, target ExecutionStatus) bool {
	return target == StatusCancelled ||
		(current == StatusInProgress && target == StatusReady) ||
		(current == StatusReview && target == StatusInProgress)
}
