package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/dennisschroeder/workgraph/internal/domain/authority"
	"github.com/dennisschroeder/workgraph/internal/domain/output"
	"github.com/dennisschroeder/workgraph/internal/domain/work"
	"github.com/dennisschroeder/workgraph/internal/ports"
)

type Service struct {
	store ports.Store
	ids   ports.IDGenerator
	clock ports.Clock
}

func NewService(store ports.Store, ids ports.IDGenerator, clock ports.Clock) *Service {
	return &Service{store: store, ids: ids, clock: clock}
}

type CreateObjectiveCommand struct {
	ActorID        string
	IdempotencyKey string
	Key            string
	Title          string
	Description    string
	DesiredOutcome string
	Phase          work.ObjectivePhase
}

func (s *Service) CreateObjective(ctx context.Context, command CreateObjectiveCommand) (work.Objective, error) {
	if replay, found, err := replayIdempotently[work.Objective](ctx, s, command.ActorID, command.IdempotencyKey, "create_objective", command); err != nil {
		return work.Objective{}, err
	} else if found {
		return replay, nil
	}
	if strings.TrimSpace(command.ActorID) == "" {
		return work.Objective{}, errors.New("objective creation requires an actor")
	}
	if command.Phase != work.ObjectiveIdea && command.Phase != work.ObjectiveDiscovery && command.Phase != work.ObjectivePlanning {
		return work.Objective{}, errors.New("new objectives must start in idea, discovery, or planning")
	}
	id, err := s.ids.New()
	if err != nil {
		return work.Objective{}, fmt.Errorf("generate objective id: %w", err)
	}
	objective, err := work.NewObjective(id, command.Key, command.Title, command.Description, command.DesiredOutcome, command.Phase, s.clock.Now())
	if err != nil {
		return work.Objective{}, err
	}
	if err := s.store.WithinTransaction(ctx, func(repository ports.Repository) error {
		create := func() (work.Objective, error) {
			if err := repository.CreateObjective(ctx, objective); err != nil {
				return work.Objective{}, err
			}
			if err := s.recordActivity(ctx, repository, work.Activity{EntityKind: "objective", EntityID: objective.ID, ActorID: command.ActorID, EventType: "objective.created", Summary: fmt.Sprintf("Objective %s created", objective.Key)}); err != nil {
				return work.Objective{}, err
			}
			return objective, nil
		}
		created, err := executeIdempotently(ctx, s, repository, command.ActorID, command.IdempotencyKey, "create_objective", command, create)
		objective = created
		return err
	}); err != nil {
		return work.Objective{}, fmt.Errorf("create objective: %w", err)
	}
	return objective, nil
}

type CreatePlanCommand struct {
	ActorID         string
	IdempotencyKey  string
	ObjectiveID     string
	Title           string
	Summary         string
	Revision        int
	CommitmentState work.PlanCommitment
}

type PatchObjectiveCommand struct {
	ObjectiveID     string
	ActorID         string
	IdempotencyKey  string
	ExpectedVersion int
	Title           *string
	Description     *string
	DesiredOutcome  *string
}

func (s *Service) PatchObjective(ctx context.Context, command PatchObjectiveCommand) (work.Objective, error) {
	var patched work.Objective
	err := s.store.WithinTransaction(ctx, func(repository ports.Repository) error {
		result, err := executeIdempotently(ctx, s, repository, command.ActorID, command.IdempotencyKey, "patch_objective", command, func() (work.Objective, error) {
			objective, err := repository.Objective(ctx, command.ObjectiveID)
			if err != nil {
				return work.Objective{}, err
			}
			if objective.Version != command.ExpectedVersion {
				return work.Objective{}, ports.ErrVersionConflict
			}
			if command.Title != nil {
				objective.Title = strings.TrimSpace(*command.Title)
			}
			if command.Description != nil {
				objective.Description = strings.TrimSpace(*command.Description)
			}
			if command.DesiredOutcome != nil {
				objective.DesiredOutcome = strings.TrimSpace(*command.DesiredOutcome)
			}
			if err := objective.Validate(); err != nil {
				return work.Objective{}, err
			}
			objective.Version++
			objective.UpdatedBy = command.ActorID
			objective.UpdatedAt = s.clock.Now().UTC()
			if err := repository.UpdateObjective(ctx, objective, command.ExpectedVersion); err != nil {
				return work.Objective{}, err
			}
			if err := s.recordActivity(ctx, repository, work.Activity{EntityKind: "objective", EntityID: objective.ID, ActorID: command.ActorID, EventType: "objective.patched", Summary: "Objective details updated"}); err != nil {
				return work.Objective{}, err
			}
			return objective, nil
		})
		patched = result
		return err
	})
	if err != nil {
		return work.Objective{}, fmt.Errorf("patch objective: %w", err)
	}
	return patched, nil
}

type PatchWorkItemCommand struct {
	WorkItemID                     string
	ActorID                        string
	IdempotencyKey                 string
	ExpectedVersion                int
	Title                          *string
	Description                    *string
	ParentID                       *string
	Priority                       *work.Priority
	EstimatedScope                 *work.EstimatedScope
	ExecutionPolicy                *work.ExecutionPolicy
	AttentionState                 *work.AttentionState
	RequiredCapabilities           *[]string
	AcceptanceCriterionResolutions []PatchAcceptanceCriterionResolution
	ExpectedOutputsToAdd           []ProposedExpectedOutput
}

type PatchAcceptanceCriterionResolution struct {
	CriterionID string
	Status      work.AcceptanceCriterionStatus
	Rationale   string
}

func (s *Service) PatchWorkItem(ctx context.Context, command PatchWorkItemCommand) (work.WorkItem, error) {
	var patched work.WorkItem
	err := s.store.WithinTransaction(ctx, func(repository ports.Repository) error {
		result, err := executeIdempotently(ctx, s, repository, command.ActorID, command.IdempotencyKey, "patch_work_item", command, func() (work.WorkItem, error) {
			if !patchWorkItemHasChanges(command) {
				return work.WorkItem{}, errors.New("patch work item requires at least one mutable field")
			}
			item, err := repository.WorkItem(ctx, command.WorkItemID)
			if err != nil {
				return work.WorkItem{}, err
			}
			if item.Version != command.ExpectedVersion {
				return work.WorkItem{}, ports.ErrVersionConflict
			}
			changes := make([]string, 0, 6)
			if command.Title != nil {
				item.Title = strings.TrimSpace(*command.Title)
				changes = append(changes, "title")
			}
			if command.Description != nil {
				item.Description = strings.TrimSpace(*command.Description)
				changes = append(changes, "description")
			}
			if command.ParentID != nil {
				parentID := strings.TrimSpace(*command.ParentID)
				if parentID != "" {
					parent, err := repository.WorkItem(ctx, parentID)
					if err != nil {
						return work.WorkItem{}, fmt.Errorf("load parent work item: %w", err)
					}
					if parent.ObjectiveID != item.ObjectiveID {
						return work.WorkItem{}, errors.New("work item parent belongs to another objective")
					}
					if parent.ID == item.ID {
						return work.WorkItem{}, errors.New("work item cannot be its own parent")
					}
					cycle, err := repository.WorkItemParentCreatesCycle(ctx, item.ID, parent.ID)
					if err != nil {
						return work.WorkItem{}, err
					}
					if cycle {
						return work.WorkItem{}, errors.New("work item parent cycle")
					}
				}
				item.ParentID = parentID
				changes = append(changes, "parent")
			}
			if command.Priority != nil {
				item.Priority = *command.Priority
				changes = append(changes, "priority")
			}
			if command.EstimatedScope != nil {
				item.EstimatedScope = *command.EstimatedScope
				changes = append(changes, "estimated scope")
			}
			if command.ExecutionPolicy != nil {
				item.ExecutionPolicy = *command.ExecutionPolicy
				changes = append(changes, "execution policy")
			}
			if command.AttentionState != nil {
				item.AttentionState = *command.AttentionState
				changes = append(changes, "attention state")
			}
			if command.RequiredCapabilities != nil {
				capabilities, err := normalizedCapabilities(*command.RequiredCapabilities)
				if err != nil {
					return work.WorkItem{}, err
				}
				if err := repository.ReplaceWorkItemCapabilities(ctx, item.ID, capabilities); err != nil {
					return work.WorkItem{}, err
				}
				changes = append(changes, "required capabilities")
			}
			seenCriteria := make(map[string]bool, len(command.AcceptanceCriterionResolutions))
			waivedRequired := false
			for _, resolution := range command.AcceptanceCriterionResolutions {
				criterionID := strings.TrimSpace(resolution.CriterionID)
				if criterionID == "" {
					return work.WorkItem{}, errors.New("acceptance criterion id cannot be empty")
				}
				if seenCriteria[criterionID] {
					return work.WorkItem{}, fmt.Errorf("duplicate acceptance criterion %q", criterionID)
				}
				seenCriteria[criterionID] = true
				criterion, err := repository.AcceptanceCriterion(ctx, criterionID)
				if err != nil {
					return work.WorkItem{}, err
				}
				if criterion.WorkItemID != item.ID {
					return work.WorkItem{}, errors.New("acceptance criterion belongs to another work item")
				}
				resolved, err := work.ResolveAcceptanceCriterion(criterion, resolution.Status, command.ActorID, resolution.Rationale, s.clock.Now())
				if err != nil {
					return work.WorkItem{}, err
				}
				if err := repository.UpdateAcceptanceCriterion(ctx, resolved); err != nil {
					return work.WorkItem{}, err
				}
				waivedRequired = waivedRequired || (resolved.Status == work.AcceptanceWaived && resolved.Required)
			}
			if len(command.AcceptanceCriterionResolutions) > 0 {
				changes = append(changes, "acceptance criteria")
			}
			if waivedRequired && command.AttentionState == nil && item.AttentionState == work.AttentionNone {
				item.AttentionState = work.AttentionNeedsHumanReview
				changes = append(changes, "attention state")
			}
			for _, proposed := range command.ExpectedOutputsToAdd {
				profile, err := repository.OutputProfile(ctx, proposed.ProfileName, proposed.ProfileVersion)
				if err != nil {
					return work.WorkItem{}, fmt.Errorf("load output profile: %w", err)
				}
				id, err := s.ids.New()
				if err != nil {
					return work.WorkItem{}, fmt.Errorf("generate expected output id: %w", err)
				}
				expected, err := output.NewExpectedOutput(id, item.ID, proposed.Name, profile, proposed.Contract, proposed.DestinationHint, proposed.Required, proposed.Ordinal)
				if err != nil {
					return work.WorkItem{}, err
				}
				if err := repository.CreateExpectedOutput(ctx, expected); err != nil {
					return work.WorkItem{}, err
				}
			}
			if len(command.ExpectedOutputsToAdd) > 0 {
				changes = append(changes, "expected outputs")
			}
			if err := item.Validate(); err != nil {
				return work.WorkItem{}, err
			}
			item.Version++
			item.UpdatedAt = s.clock.Now().UTC()
			if err := repository.UpdateWorkItem(ctx, item, command.ExpectedVersion); err != nil {
				return work.WorkItem{}, err
			}
			if err := s.recordActivity(ctx, repository, work.Activity{EntityKind: "work_item", EntityID: item.ID, WorkItemID: item.ID, ActorID: command.ActorID, EventType: "work_item.patched", Summary: "Work item updated: " + strings.Join(changes, ", ")}); err != nil {
				return work.WorkItem{}, err
			}
			return item, nil
		})
		patched = result
		return err
	})
	if err != nil {
		return work.WorkItem{}, fmt.Errorf("patch work item: %w", err)
	}
	return patched, nil
}

func patchWorkItemHasChanges(command PatchWorkItemCommand) bool {
	return command.Title != nil || command.Description != nil || command.ParentID != nil || command.Priority != nil || command.EstimatedScope != nil || command.ExecutionPolicy != nil || command.AttentionState != nil || command.RequiredCapabilities != nil || len(command.AcceptanceCriterionResolutions) > 0 || len(command.ExpectedOutputsToAdd) > 0
}

func normalizedCapabilities(capabilities []string) ([]string, error) {
	result := make([]string, 0, len(capabilities))
	seen := make(map[string]bool, len(capabilities))
	for _, capability := range capabilities {
		capability = strings.TrimSpace(capability)
		if capability == "" {
			return nil, errors.New("required capability cannot be empty")
		}
		if !seen[capability] {
			seen[capability] = true
			result = append(result, capability)
		}
	}
	return result, nil
}

type RequestAttentionCommand struct {
	TargetKind      string
	TargetID        string
	WorkItemID      string
	ActorID         string
	IdempotencyKey  string
	ExpectedVersion int
	AttentionState  work.AttentionState
}

type AttentionRequestResult struct {
	TargetKind     string              `json:"target_kind"`
	TargetID       string              `json:"target_id"`
	AttentionState work.AttentionState `json:"attention_state"`
	WorkItem       *work.WorkItem      `json:"work_item,omitempty"`
	Question       *work.Question      `json:"question,omitempty"`
	Decision       *work.Decision      `json:"decision,omitempty"`
}

func (s *Service) RequestAttention(ctx context.Context, command RequestAttentionCommand) (AttentionRequestResult, error) {
	var result AttentionRequestResult
	err := s.store.WithinTransaction(ctx, func(repository ports.Repository) error {
		requested, err := executeIdempotently(ctx, s, repository, command.ActorID, command.IdempotencyKey, "request_attention", command, func() (AttentionRequestResult, error) {
			if !work.ValidAttentionState(command.AttentionState) {
				return AttentionRequestResult{}, fmt.Errorf("invalid attention state %q", command.AttentionState)
			}
			targetKind, targetID, err := attentionTarget(command)
			if err != nil {
				return AttentionRequestResult{}, err
			}
			result := AttentionRequestResult{TargetKind: targetKind, TargetID: targetID, AttentionState: command.AttentionState}
			activity := work.Activity{EntityKind: targetKind, EntityID: targetID, ActorID: command.ActorID, EventType: "attention.requested", Summary: "Human attention requested", PayloadJSON: attentionPayload(targetKind, targetID, command.AttentionState)}
			switch targetKind {
			case "work_item":
				item, err := repository.WorkItem(ctx, targetID)
				if err != nil {
					return AttentionRequestResult{}, err
				}
				if item.Version != command.ExpectedVersion {
					return AttentionRequestResult{}, ports.ErrVersionConflict
				}
				item.AttentionState = command.AttentionState
				if err := item.Validate(); err != nil {
					return AttentionRequestResult{}, err
				}
				item.Version++
				item.UpdatedAt = s.clock.Now().UTC()
				if err := repository.UpdateWorkItem(ctx, item, command.ExpectedVersion); err != nil {
					return AttentionRequestResult{}, err
				}
				result.WorkItem = &item
				activity.WorkItemID = item.ID
			case "question":
				question, err := repository.Question(ctx, targetID)
				if err != nil {
					return AttentionRequestResult{}, err
				}
				if question.Version != command.ExpectedVersion {
					return AttentionRequestResult{}, ports.ErrVersionConflict
				}
				question.RequiresHumanAttention = command.AttentionState != work.AttentionNone
				question.Version++
				if err := repository.UpdateQuestion(ctx, question, command.ExpectedVersion); err != nil {
					return AttentionRequestResult{}, err
				}
				result.Question = &question
				activity.WorkItemID = question.WorkItemID
			case "decision":
				decision, err := repository.Decision(ctx, targetID)
				if err != nil {
					return AttentionRequestResult{}, err
				}
				result.Decision = &decision
				activity.WorkItemID = decision.WorkItemID
			}
			if err := s.recordActivity(ctx, repository, activity); err != nil {
				return AttentionRequestResult{}, err
			}
			return result, nil
		})
		result = requested
		return err
	})
	if err != nil {
		return AttentionRequestResult{}, fmt.Errorf("request attention: %w", err)
	}
	return result, nil
}

func attentionTarget(command RequestAttentionCommand) (string, string, error) {
	targetKind := strings.TrimSpace(command.TargetKind)
	targetID := strings.TrimSpace(command.TargetID)
	workItemID := strings.TrimSpace(command.WorkItemID)
	if targetKind == "" {
		targetKind = "work_item"
	}
	switch targetKind {
	case "work_item":
		if targetID == "" {
			targetID = workItemID
		}
		if targetID == "" || (workItemID != "" && workItemID != targetID) {
			return "", "", errors.New("attention requires exactly one work item target")
		}
	case "question", "decision", "review", "clarification", "intervention":
		if targetID == "" || workItemID != "" {
			return "", "", errors.New("attention requires exactly one non-work target")
		}
	default:
		return "", "", errors.New("attention target kind is not supported")
	}
	return targetKind, targetID, nil
}

func attentionPayload(targetKind, targetID string, attentionState work.AttentionState) json.RawMessage {
	payload, _ := json.Marshal(struct {
		TargetKind     string              `json:"target_kind"`
		TargetID       string              `json:"target_id"`
		AttentionState work.AttentionState `json:"attention_state"`
	}{TargetKind: targetKind, TargetID: targetID, AttentionState: attentionState})
	return payload
}

func (s *Service) CreatePlan(ctx context.Context, command CreatePlanCommand) (work.Plan, error) {
	if replay, found, err := replayIdempotently[work.Plan](ctx, s, command.ActorID, command.IdempotencyKey, "create_plan", command); err != nil {
		return work.Plan{}, err
	} else if found {
		return replay, nil
	}
	if strings.TrimSpace(command.ActorID) == "" {
		return work.Plan{}, errors.New("plan creation requires an actor")
	}
	if command.CommitmentState != work.PlanDraft {
		return work.Plan{}, errors.New("create plan only supports drafts; use propose plan for reviewable work")
	}
	id, err := s.ids.New()
	if err != nil {
		return work.Plan{}, fmt.Errorf("generate plan id: %w", err)
	}
	plan, err := work.NewPlan(id, command.ObjectiveID, command.Title, command.Summary, command.Revision, command.CommitmentState, s.clock.Now())
	if err != nil {
		return work.Plan{}, err
	}
	if err := s.store.WithinTransaction(ctx, func(repository ports.Repository) error {
		created, err := executeIdempotently(ctx, s, repository, command.ActorID, command.IdempotencyKey, "create_plan", command, func() (work.Plan, error) {
			if _, err := repository.Objective(ctx, plan.ObjectiveID); err != nil {
				return work.Plan{}, fmt.Errorf("load objective: %w", err)
			}
			if err := repository.CreatePlan(ctx, plan); err != nil {
				return work.Plan{}, err
			}
			if err := s.recordActivity(ctx, repository, work.Activity{
				EntityKind: "plan", EntityID: plan.ID, ActorID: command.ActorID,
				EventType: "plan.created", Summary: fmt.Sprintf("Draft plan revision %d created", plan.Revision),
			}); err != nil {
				return work.Plan{}, err
			}
			return plan, nil
		})
		plan = created
		return err
	}); err != nil {
		return work.Plan{}, fmt.Errorf("create plan: %w", err)
	}
	return plan, nil
}

type CreateWorkItemCommand struct {
	ActorID              string
	IdempotencyKey       string
	Key                  string
	ObjectiveID          string
	PlanID               string
	ParentID             string
	Title                string
	Description          string
	Kind                 string
	CommitmentState      work.ItemCommitment
	ExecutionStatus      work.ExecutionStatus
	Priority             work.Priority
	EstimatedScope       work.EstimatedScope
	ExecutionPolicy      work.ExecutionPolicy
	RequiredActorKind    work.ActorKind
	AttentionState       work.AttentionState
	RequiredCapabilities []string
	AcceptanceCriteria   []ProposedAcceptanceCriterion
	ExpectedOutputs      []ProposedExpectedOutput
	OutputRequirements   []ProposedOutputRequirement
	ExternalActions      []ProposedExternalAction
	Dependencies         []CreateWorkItemDependency
}

// CreateWorkItemDependency links the new item to an existing work item by ID.
// Plan proposals use client references; direct creation deliberately does not.
type CreateWorkItemDependency struct {
	DependsOnWorkItemID string
	Kind                work.DependencyKind
	Note                string
}

func (s *Service) CreateWorkItem(ctx context.Context, command CreateWorkItemCommand) (work.WorkItem, error) {
	if replay, found, err := replayIdempotently[work.WorkItem](ctx, s, command.ActorID, command.IdempotencyKey, "create_work_item", command); err != nil {
		return work.WorkItem{}, err
	} else if found {
		return replay, nil
	}
	if strings.TrimSpace(command.ActorID) == "" {
		return work.WorkItem{}, errors.New("work item creation requires an actor")
	}
	advanced := command.CommitmentState == work.ItemAccepted || command.ExecutionStatus == work.StatusReady
	if advanced && (command.CommitmentState != work.ItemAccepted || command.ExecutionStatus != work.StatusReady) {
		return work.WorkItem{}, errors.New("new accepted work items must start ready")
	}
	if !advanced && (command.CommitmentState != work.ItemProposed || command.ExecutionStatus != work.StatusBacklog) {
		return work.WorkItem{}, errors.New("new work items must be proposed and in backlog, or accepted and ready")
	}
	if command.Priority == "" {
		command.Priority = work.PriorityMedium
	}
	if command.EstimatedScope == "" {
		command.EstimatedScope = work.ScopeUnknown
	}
	if command.ExecutionPolicy == "" {
		command.ExecutionPolicy = work.PolicyApprovalRequired
	}
	if command.RequiredActorKind == "" {
		command.RequiredActorKind = work.ActorAny
	}
	id, err := s.ids.New()
	if err != nil {
		return work.WorkItem{}, fmt.Errorf("generate work item id: %w", err)
	}
	item, err := work.NewWorkItem(work.WorkItem{
		ID:                id,
		Key:               command.Key,
		ObjectiveID:       command.ObjectiveID,
		PlanID:            command.PlanID,
		ParentID:          command.ParentID,
		Title:             command.Title,
		Description:       command.Description,
		Kind:              command.Kind,
		CommitmentState:   command.CommitmentState,
		ExecutionStatus:   command.ExecutionStatus,
		Priority:          command.Priority,
		EstimatedScope:    command.EstimatedScope,
		ExecutionPolicy:   command.ExecutionPolicy,
		RequiredActorKind: command.RequiredActorKind,
		AttentionState:    command.AttentionState,
	}, s.clock.Now())
	if err != nil {
		return work.WorkItem{}, err
	}
	initial, err := s.generateInitialWorkItemDetails(item.ID, command)
	if err != nil {
		return work.WorkItem{}, err
	}
	if err := s.store.WithinTransaction(ctx, func(repository ports.Repository) error {
		created, err := executeIdempotently(ctx, s, repository, command.ActorID, command.IdempotencyKey, "create_work_item", command, func() (work.WorkItem, error) {
			objective, err := repository.Objective(ctx, item.ObjectiveID)
			if err != nil {
				return work.WorkItem{}, fmt.Errorf("load objective: %w", err)
			}
			var plan work.Plan
			if item.PlanID != "" {
				plan, err = repository.Plan(ctx, item.PlanID)
				if err != nil {
					return work.WorkItem{}, fmt.Errorf("load plan: %w", err)
				}
				if plan.ObjectiveID != item.ObjectiveID {
					return work.WorkItem{}, errors.New("work item plan belongs to another objective")
				}
			}
			if advanced && (item.PlanID == "" || objective.Phase != work.ObjectiveExecution || plan.CommitmentState != work.PlanApproved) {
				return work.WorkItem{}, errors.New("accepted ready work items require an approved plan in an execution objective")
			}
			if item.ParentID != "" {
				parent, err := repository.WorkItem(ctx, item.ParentID)
				if err != nil {
					return work.WorkItem{}, fmt.Errorf("load parent work item: %w", err)
				}
				if parent.ObjectiveID != item.ObjectiveID {
					return work.WorkItem{}, errors.New("work item parent belongs to another objective")
				}
			}
			if err := repository.CreateWorkItem(ctx, item); err != nil {
				return work.WorkItem{}, err
			}
			for _, capability := range initial.capabilities {
				if err := repository.AddWorkItemCapability(ctx, item.ID, capability); err != nil {
					return work.WorkItem{}, err
				}
			}
			for _, criterion := range initial.criteria {
				if err := repository.CreateAcceptanceCriterion(ctx, criterion); err != nil {
					return work.WorkItem{}, err
				}
			}
			for _, candidate := range initial.expectedOutputs {
				profile, err := repository.OutputProfile(ctx, candidate.command.ProfileName, candidate.command.ProfileVersion)
				if err != nil {
					return work.WorkItem{}, fmt.Errorf("load output profile: %w", err)
				}
				expected, err := output.NewExpectedOutput(candidate.id, item.ID, candidate.command.Name, profile, candidate.command.Contract, candidate.command.DestinationHint, candidate.command.Required, candidate.command.Ordinal)
				if err != nil {
					return work.WorkItem{}, err
				}
				if err := repository.CreateExpectedOutput(ctx, expected); err != nil {
					return work.WorkItem{}, err
				}
			}
			for _, candidate := range initial.requirements {
				requirement, err := createInitialOutputRequirement(ctx, repository, candidate.id, item.ID, candidate.command)
				if err != nil {
					return work.WorkItem{}, err
				}
				if err := repository.CreateOutputRequirement(ctx, requirement); err != nil {
					return work.WorkItem{}, err
				}
			}
			for _, candidate := range initial.actions {
				action, revision, err := authority.NewExternalAction(authority.ExternalAction{ID: candidate.id, WorkItemID: item.ID, Required: candidate.command.Required, Title: candidate.command.Title, Rationale: candidate.command.Rationale}, candidate.command.AuthorizationSubject, command.ActorID, s.clock.Now())
				if err != nil {
					return work.WorkItem{}, err
				}
				if err := repository.CreateExternalAction(ctx, action); err != nil {
					return work.WorkItem{}, err
				}
				if err := repository.CreateExternalActionRevision(ctx, revision); err != nil {
					return work.WorkItem{}, err
				}
			}
			for _, candidate := range initial.dependencies {
				dependencyItem, err := repository.WorkItem(ctx, candidate.command.DependsOnWorkItemID)
				if err != nil {
					return work.WorkItem{}, fmt.Errorf("load dependency work item: %w", err)
				}
				if dependencyItem.ObjectiveID != item.ObjectiveID {
					return work.WorkItem{}, errors.New("dependency work item belongs to another objective")
				}
				cycle, err := repository.DependencyCreatesCycle(ctx, item.ID, dependencyItem.ID)
				if err != nil {
					return work.WorkItem{}, err
				}
				if cycle {
					return work.WorkItem{}, errors.New("dependency cycle")
				}
				dependency, err := work.NewDependency(work.Dependency{ID: candidate.id, WorkItemID: item.ID, DependsOnItemID: dependencyItem.ID, Kind: candidate.command.Kind, Note: candidate.command.Note, CreatedBy: command.ActorID}, s.clock.Now())
				if err != nil {
					return work.WorkItem{}, err
				}
				if err := repository.CreateDependency(ctx, dependency); err != nil {
					return work.WorkItem{}, err
				}
			}
			if err := s.recordActivity(ctx, repository, work.Activity{
				EntityKind: "work_item", EntityID: item.ID, WorkItemID: item.ID, ActorID: command.ActorID,
				EventType: "work_item.created", Summary: fmt.Sprintf("Work item %s created", item.Key),
			}); err != nil {
				return work.WorkItem{}, err
			}
			return item, nil
		})
		item = created
		return err
	}); err != nil {
		return work.WorkItem{}, fmt.Errorf("create work item: %w", err)
	}
	return item, nil
}

type initialWorkItemDetails struct {
	capabilities    []string
	criteria        []work.AcceptanceCriterion
	expectedOutputs []generatedExpectedOutput
	requirements    []generatedOutputRequirement
	actions         []generatedExternalAction
	dependencies    []generatedCreateWorkItemDependency
}

type generatedCreateWorkItemDependency struct {
	id      string
	command CreateWorkItemDependency
}

func (s *Service) generateInitialWorkItemDetails(workItemID string, command CreateWorkItemCommand) (initialWorkItemDetails, error) {
	var details initialWorkItemDetails
	seenCapabilities := make(map[string]bool, len(command.RequiredCapabilities))
	for _, capability := range command.RequiredCapabilities {
		capability = strings.TrimSpace(capability)
		if capability == "" {
			return initialWorkItemDetails{}, errors.New("required capability cannot be empty")
		}
		if !seenCapabilities[capability] {
			seenCapabilities[capability] = true
			details.capabilities = append(details.capabilities, capability)
		}
	}
	seenCriteria := make(map[int]bool, len(command.AcceptanceCriteria))
	for _, proposed := range command.AcceptanceCriteria {
		if seenCriteria[proposed.Ordinal] {
			return initialWorkItemDetails{}, fmt.Errorf("duplicate acceptance criterion ordinal %d", proposed.Ordinal)
		}
		seenCriteria[proposed.Ordinal] = true
		id, err := s.ids.New()
		if err != nil {
			return initialWorkItemDetails{}, fmt.Errorf("generate acceptance criterion id: %w", err)
		}
		criterion, err := work.NewAcceptanceCriterion(work.AcceptanceCriterion{ID: id, WorkItemID: workItemID, Text: proposed.Text, Required: proposed.Required, Ordinal: proposed.Ordinal})
		if err != nil {
			return initialWorkItemDetails{}, err
		}
		details.criteria = append(details.criteria, criterion)
	}
	for _, proposed := range command.ExpectedOutputs {
		id, err := s.ids.New()
		if err != nil {
			return initialWorkItemDetails{}, fmt.Errorf("generate expected output id: %w", err)
		}
		details.expectedOutputs = append(details.expectedOutputs, generatedExpectedOutput{id: id, command: proposed})
	}
	for _, proposed := range command.OutputRequirements {
		id, err := s.ids.New()
		if err != nil {
			return initialWorkItemDetails{}, fmt.Errorf("generate output requirement id: %w", err)
		}
		details.requirements = append(details.requirements, generatedOutputRequirement{id: id, command: proposed})
	}
	for _, proposed := range command.ExternalActions {
		id, err := s.ids.New()
		if err != nil {
			return initialWorkItemDetails{}, fmt.Errorf("generate external action id: %w", err)
		}
		details.actions = append(details.actions, generatedExternalAction{id: id, command: proposed})
	}
	seenDependencies := make(map[string]bool, len(command.Dependencies))
	for _, proposed := range command.Dependencies {
		key := strings.TrimSpace(proposed.DependsOnWorkItemID) + "\x00" + string(proposed.Kind)
		if strings.TrimSpace(proposed.DependsOnWorkItemID) == "" {
			return initialWorkItemDetails{}, errors.New("dependency work item id cannot be empty")
		}
		if seenDependencies[key] {
			continue
		}
		seenDependencies[key] = true
		id, err := s.ids.New()
		if err != nil {
			return initialWorkItemDetails{}, fmt.Errorf("generate dependency id: %w", err)
		}
		details.dependencies = append(details.dependencies, generatedCreateWorkItemDependency{id: id, command: proposed})
	}
	return details, nil
}

func createInitialOutputRequirement(ctx context.Context, repository ports.Repository, id, workItemID string, command ProposedOutputRequirement) (output.OutputRequirement, error) {
	hasRevision := strings.TrimSpace(command.RequiredOutputRevisionID) != ""
	hasProfile := strings.TrimSpace(command.RequiredProfileName) != "" || strings.TrimSpace(command.VersionConstraint) != ""
	if hasRevision == hasProfile {
		return output.OutputRequirement{}, errors.New("output requirement must select exactly one revision or profile constraint")
	}
	if hasRevision {
		revision, err := repository.OutputRevision(ctx, command.RequiredOutputRevisionID)
		if err != nil {
			return output.OutputRequirement{}, fmt.Errorf("load required output revision: %w", err)
		}
		return output.NewExactOutputRequirement(id, workItemID, revision, command.Required, command.Note)
	}
	requirement, err := output.NewProfileOutputRequirement(id, workItemID, command.RequiredProfileName, command.VersionConstraint, command.Required, command.Note)
	if err != nil {
		return output.OutputRequirement{}, err
	}
	version := strings.TrimPrefix(requirement.VersionConstraint, "=")
	profileVersion := 0
	if _, err := fmt.Sscanf(version, "%d", &profileVersion); err != nil {
		return output.OutputRequirement{}, err
	}
	profile, err := repository.OutputProfile(ctx, requirement.RequiredProfileName, profileVersion)
	if err != nil {
		return output.OutputRequirement{}, fmt.Errorf("load required output profile: %w", err)
	}
	if profile.LifecycleState != output.ProfileActive {
		return output.OutputRequirement{}, fmt.Errorf("required output profile %s/v%d is not active", profile.Name, profile.Version)
	}
	return requirement, nil
}

type DefineExpectedOutputCommand struct {
	ActorID         string
	WorkItemID      string
	Name            string
	ProfileName     string
	ProfileVersion  int
	Contract        json.RawMessage
	DestinationHint string
	Required        bool
	Ordinal         int
	ExpectedVersion int
	IdempotencyKey  string
}

func (s *Service) DefineExpectedOutput(ctx context.Context, command DefineExpectedOutputCommand) (output.ExpectedOutput, error) {
	if replay, found, err := replayIdempotently[output.ExpectedOutput](ctx, s, command.ActorID, command.IdempotencyKey, "define_expected_output", command); err != nil {
		return output.ExpectedOutput{}, err
	} else if found {
		return replay, nil
	}
	if strings.TrimSpace(command.ActorID) == "" {
		return output.ExpectedOutput{}, errors.New("expected output definition requires an actor")
	}
	id, err := s.ids.New()
	if err != nil {
		return output.ExpectedOutput{}, fmt.Errorf("generate expected output id: %w", err)
	}
	var expected output.ExpectedOutput
	if err := s.store.WithinTransaction(ctx, func(repository ports.Repository) error {
		var err error
		expected, err = executeIdempotently(ctx, s, repository, command.ActorID, command.IdempotencyKey, "define_expected_output", command, func() (output.ExpectedOutput, error) {
			item, err := repository.WorkItem(ctx, command.WorkItemID)
			if err != nil {
				return output.ExpectedOutput{}, fmt.Errorf("load work item: %w", err)
			}
			if item.Version != command.ExpectedVersion {
				return output.ExpectedOutput{}, ports.ErrVersionConflict
			}
			profile, err := repository.OutputProfile(ctx, command.ProfileName, command.ProfileVersion)
			if err != nil {
				return output.ExpectedOutput{}, fmt.Errorf("load output profile: %w", err)
			}
			result, err := output.NewExpectedOutput(id, command.WorkItemID, command.Name, profile, command.Contract, command.DestinationHint, command.Required, command.Ordinal)
			if err != nil {
				return output.ExpectedOutput{}, err
			}
			if err := repository.CreateExpectedOutput(ctx, result); err != nil {
				return output.ExpectedOutput{}, err
			}
			item.Version++
			item.UpdatedAt = s.clock.Now().UTC()
			if err := repository.UpdateWorkItem(ctx, item, command.ExpectedVersion); err != nil {
				return output.ExpectedOutput{}, err
			}
			if err := s.recordActivity(ctx, repository, work.Activity{EntityKind: "expected_output", EntityID: result.ID, WorkItemID: result.WorkItemID, ActorID: command.ActorID, EventType: "expected_output.defined", Summary: fmt.Sprintf("Expected output %s defined", result.Name)}); err != nil {
				return output.ExpectedOutput{}, err
			}
			return result, nil
		})
		return err
	}); err != nil {
		return output.ExpectedOutput{}, fmt.Errorf("define expected output: %w", err)
	}
	return expected, nil
}

func (s *Service) GetWorkItem(ctx context.Context, id string) (ports.WorkItemContext, error) {
	result, err := s.store.GetWorkItemContext(ctx, id)
	if err != nil {
		return ports.WorkItemContext{}, fmt.Errorf("get work item: %w", err)
	}
	return result, nil
}

func (s *Service) GetPlan(ctx context.Context, id string) (work.Plan, error) {
	var plan work.Plan
	err := s.store.WithinTransaction(ctx, func(repository ports.Repository) error {
		var err error
		plan, err = repository.Plan(ctx, id)
		return err
	})
	return plan, err
}

func (s *Service) GetOutputProfileByID(ctx context.Context, id string) (output.Profile, error) {
	var profile output.Profile
	err := s.store.WithinTransaction(ctx, func(repository ports.Repository) error {
		var err error
		profile, err = repository.OutputProfileByID(ctx, id)
		return err
	})
	return profile, err
}

func (s *Service) GetOutputRevision(ctx context.Context, id string) (output.OutputRevision, error) {
	var revision output.OutputRevision
	err := s.store.WithinTransaction(ctx, func(repository ports.Repository) error {
		var err error
		revision, err = repository.OutputRevision(ctx, id)
		return err
	})
	return revision, err
}

func (s *Service) ListWorkItems(ctx context.Context) ([]ports.WorkItemContext, error) {
	items, err := s.store.ListWorkItemContexts(ctx)
	if err != nil {
		return nil, fmt.Errorf("list work items: %w", err)
	}
	return items, nil
}

type UUIDv7Generator struct{}

func (UUIDv7Generator) New() (string, error) {
	id, err := uuid.NewV7()
	if err != nil {
		return "", err
	}
	return id.String(), nil
}

type SystemClock struct{}

func (SystemClock) Now() time.Time {
	return time.Now().UTC()
}
