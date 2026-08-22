package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

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
		if strings.TrimSpace(command.IdempotencyKey) == "" {
			_, err := create()
			return err
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
	ObjectiveID     string
	Title           string
	Summary         string
	Revision        int
	CommitmentState work.PlanCommitment
}

func (s *Service) CreatePlan(ctx context.Context, command CreatePlanCommand) (work.Plan, error) {
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
		if _, err := repository.Objective(ctx, plan.ObjectiveID); err != nil {
			return fmt.Errorf("load objective: %w", err)
		}
		if err := repository.CreatePlan(ctx, plan); err != nil {
			return err
		}
		return s.recordActivity(ctx, repository, work.Activity{
			EntityKind: "plan", EntityID: plan.ID, ActorID: command.ActorID,
			EventType: "plan.created", Summary: fmt.Sprintf("Draft plan revision %d created", plan.Revision),
		})
	}); err != nil {
		return work.Plan{}, fmt.Errorf("create plan: %w", err)
	}
	return plan, nil
}

type CreateWorkItemCommand struct {
	ActorID           string
	Key               string
	ObjectiveID       string
	PlanID            string
	Title             string
	Description       string
	Kind              string
	CommitmentState   work.ItemCommitment
	ExecutionStatus   work.ExecutionStatus
	Priority          work.Priority
	EstimatedScope    work.EstimatedScope
	ExecutionPolicy   work.ExecutionPolicy
	RequiredActorKind work.ActorKind
	AttentionState    work.AttentionState
}

func (s *Service) CreateWorkItem(ctx context.Context, command CreateWorkItemCommand) (work.WorkItem, error) {
	if strings.TrimSpace(command.ActorID) == "" {
		return work.WorkItem{}, errors.New("work item creation requires an actor")
	}
	if command.CommitmentState != work.ItemProposed || command.ExecutionStatus != work.StatusBacklog {
		return work.WorkItem{}, errors.New("new work items must be proposed and in backlog until commitment review is implemented")
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
	if err := s.store.WithinTransaction(ctx, func(repository ports.Repository) error {
		if _, err := repository.Objective(ctx, item.ObjectiveID); err != nil {
			return fmt.Errorf("load objective: %w", err)
		}
		if item.PlanID != "" {
			plan, err := repository.Plan(ctx, item.PlanID)
			if err != nil {
				return fmt.Errorf("load plan: %w", err)
			}
			if plan.ObjectiveID != item.ObjectiveID {
				return errors.New("work item plan belongs to another objective")
			}
		}
		if err := repository.CreateWorkItem(ctx, item); err != nil {
			return err
		}
		return s.recordActivity(ctx, repository, work.Activity{
			EntityKind: "work_item", EntityID: item.ID, WorkItemID: item.ID, ActorID: command.ActorID,
			EventType: "work_item.created", Summary: fmt.Sprintf("Work item %s created", item.Key),
		})
	}); err != nil {
		return work.WorkItem{}, fmt.Errorf("create work item: %w", err)
	}
	return item, nil
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
