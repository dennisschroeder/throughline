package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/dennisschroeder/throughline/internal/domain/authority"
	"github.com/dennisschroeder/throughline/internal/domain/output"
	"github.com/dennisschroeder/throughline/internal/domain/work"
	"github.com/dennisschroeder/throughline/internal/ports"
)

type RecordContextCommand struct {
	ObjectiveID    string
	WorkItemID     string
	ActorID        string
	Kind           work.ContextKind
	Title          string
	Body           string
	Status         work.ContextStatus
	Confidence     string
	SourceURI      string
	SupersedesID   string
	IdempotencyKey string
}

type TransitionObjectiveCommand struct {
	ObjectiveID     string
	TargetPhase     work.ObjectivePhase
	ActorID         string
	Reason          string
	ExpectedVersion int
	IdempotencyKey  string
}

func (s *Service) GetQuestion(ctx context.Context, id string) (work.Question, error) {
	var question work.Question
	err := s.store.WithinTransaction(ctx, func(repository ports.Repository) error {
		var err error
		question, err = repository.Question(ctx, id)
		return err
	})
	return question, err
}

func (s *Service) GetDecision(ctx context.Context, id string) (work.Decision, error) {
	var decision work.Decision
	err := s.store.WithinTransaction(ctx, func(repository ports.Repository) error {
		var err error
		decision, err = repository.Decision(ctx, id)
		return err
	})
	return decision, err
}

func (s *Service) TransitionObjective(ctx context.Context, command TransitionObjectiveCommand) (work.Objective, error) {
	if strings.TrimSpace(command.ActorID) == "" {
		return work.Objective{}, errors.New("objective transition requires an actor")
	}
	var transitioned work.Objective
	if err := s.store.WithinTransaction(ctx, func(repository ports.Repository) error {
		transition := func() (work.Objective, error) {
			objective, err := repository.Objective(ctx, command.ObjectiveID)
			if err != nil {
				return work.Objective{}, err
			}
			if objective.Version != command.ExpectedVersion {
				return work.Objective{}, ports.ErrVersionConflict
			}
			if command.TargetPhase == work.ObjectiveExecution {
				approved, err := repository.HasApprovedPlan(ctx, objective.ID)
				if err != nil {
					return work.Objective{}, err
				}
				if !approved {
					return work.Objective{}, errors.New("objective cannot enter execution without an approved plan")
				}
			}
			transitioned, err = work.TransitionObjective(objective, command.TargetPhase, command.Reason, s.clock.Now())
			if err != nil {
				return work.Objective{}, err
			}
			transitioned.UpdatedBy = strings.TrimSpace(command.ActorID)
			if err := repository.UpdateObjective(ctx, transitioned, command.ExpectedVersion); err != nil {
				return work.Objective{}, err
			}
			if err := s.recordActivity(ctx, repository, work.Activity{
				EntityKind: "objective", EntityID: transitioned.ID, ActorID: command.ActorID,
				EventType: "objective.phase_changed", Summary: fmt.Sprintf("Objective moved from %s to %s", objective.Phase, transitioned.Phase),
			}); err != nil {
				return work.Objective{}, err
			}
			return transitioned, nil
		}
		var err error
		transitioned, err = executeIdempotently(ctx, s, repository, command.ActorID, command.IdempotencyKey, "transition_objective", command, transition)
		return err
	}); err != nil {
		return work.Objective{}, fmt.Errorf("transition objective: %w", err)
	}
	return transitioned, nil
}

func (s *Service) RecordContext(ctx context.Context, command RecordContextCommand) (work.ContextRecord, error) {
	if replay, found, err := replayIdempotently[work.ContextRecord](ctx, s, command.ActorID, command.IdempotencyKey, "record_context", command); err != nil {
		return work.ContextRecord{}, err
	} else if found {
		return replay, nil
	}
	id, err := s.ids.New()
	if err != nil {
		return work.ContextRecord{}, fmt.Errorf("generate context record id: %w", err)
	}
	record, err := work.NewContextRecord(work.ContextRecord{
		ID:           id,
		ObjectiveID:  command.ObjectiveID,
		WorkItemID:   command.WorkItemID,
		Kind:         command.Kind,
		Title:        command.Title,
		Body:         command.Body,
		Status:       command.Status,
		Confidence:   command.Confidence,
		SourceURI:    command.SourceURI,
		SupersedesID: command.SupersedesID,
		CreatedBy:    command.ActorID,
	}, s.clock.Now())
	if err != nil {
		return work.ContextRecord{}, err
	}
	if err := s.store.WithinTransaction(ctx, func(repository ports.Repository) error {
		created, err := executeIdempotently(ctx, s, repository, command.ActorID, command.IdempotencyKey, "record_context", command, func() (work.ContextRecord, error) {
			if _, err := repository.Objective(ctx, record.ObjectiveID); err != nil {
				return work.ContextRecord{}, fmt.Errorf("load objective: %w", err)
			}
			if err := ensureWorkItemScope(ctx, repository, record.ObjectiveID, record.WorkItemID); err != nil {
				return work.ContextRecord{}, err
			}
			if record.SupersedesID == "" {
				if err := repository.CreateContextRecord(ctx, record); err != nil {
					return work.ContextRecord{}, err
				}
				if err := s.recordActivity(ctx, repository, work.Activity{EntityKind: "context_record", EntityID: record.ID, WorkItemID: record.WorkItemID, ActorID: command.ActorID, EventType: "context_record.recorded", Summary: fmt.Sprintf("Context %s recorded", record.Kind)}); err != nil {
					return work.ContextRecord{}, err
				}
				return record, nil
			}
			previous, err := repository.ContextRecord(ctx, record.SupersedesID)
			if err != nil {
				return work.ContextRecord{}, fmt.Errorf("load superseded context record: %w", err)
			}
			if previous.ObjectiveID != record.ObjectiveID || previous.Kind != record.Kind {
				return work.ContextRecord{}, errors.New("context record can only supersede the same kind in the same objective")
			}
			superseded, err := work.SupersedeContextRecord(previous, record.CreatedBy, record.CreatedAt)
			if err != nil {
				return work.ContextRecord{}, err
			}
			if err := repository.CreateContextRecord(ctx, record); err != nil {
				return work.ContextRecord{}, err
			}
			if err := repository.UpdateContextRecord(ctx, superseded, previous.Version); err != nil {
				return work.ContextRecord{}, err
			}
			if err := s.recordActivity(ctx, repository, work.Activity{EntityKind: "context_record", EntityID: record.ID, WorkItemID: record.WorkItemID, ActorID: command.ActorID, EventType: "context_record.recorded", Summary: fmt.Sprintf("Context %s recorded and predecessor superseded", record.Kind)}); err != nil {
				return work.ContextRecord{}, err
			}
			return record, nil
		})
		record = created
		return err
	}); err != nil {
		return work.ContextRecord{}, fmt.Errorf("record context: %w", err)
	}
	return record, nil
}

type TransitionContextCommand struct {
	ContextRecordID string
	ActorID         string
	TargetStatus    work.ContextStatus
	ExpectedVersion int
}

func (s *Service) TransitionContext(ctx context.Context, command TransitionContextCommand) (work.ContextRecord, error) {
	var transitioned work.ContextRecord
	if err := s.store.WithinTransaction(ctx, func(repository ports.Repository) error {
		record, err := repository.ContextRecord(ctx, command.ContextRecordID)
		if err != nil {
			return err
		}
		if record.Version != command.ExpectedVersion {
			return ports.ErrVersionConflict
		}
		transitioned, err = work.TransitionContextRecord(record, command.TargetStatus, command.ActorID, s.clock.Now())
		if err != nil {
			return err
		}
		if err := repository.UpdateContextRecord(ctx, transitioned, command.ExpectedVersion); err != nil {
			return err
		}
		return s.recordActivity(ctx, repository, work.Activity{
			EntityKind: "context_record", EntityID: transitioned.ID, WorkItemID: transitioned.WorkItemID, ActorID: command.ActorID,
			EventType: "context_record.status_changed", Summary: fmt.Sprintf("Context marked %s", transitioned.Status),
		})
	}); err != nil {
		return work.ContextRecord{}, fmt.Errorf("transition context: %w", err)
	}
	return transitioned, nil
}

type AskQuestionCommand struct {
	ObjectiveID            string
	WorkItemID             string
	ActorID                string
	Question               string
	RequiresHumanAttention bool
	IdempotencyKey         string
}

func (s *Service) AskQuestion(ctx context.Context, command AskQuestionCommand) (work.Question, error) {
	if replay, found, err := replayIdempotently[work.Question](ctx, s, command.ActorID, command.IdempotencyKey, "ask_question", command); err != nil {
		return work.Question{}, err
	} else if found {
		return replay, nil
	}
	id, err := s.ids.New()
	if err != nil {
		return work.Question{}, fmt.Errorf("generate question id: %w", err)
	}
	question, err := work.NewQuestion(work.Question{
		ID:                     id,
		ObjectiveID:            command.ObjectiveID,
		WorkItemID:             command.WorkItemID,
		Text:                   command.Question,
		RequiresHumanAttention: command.RequiresHumanAttention,
		CreatedBy:              command.ActorID,
	}, s.clock.Now())
	if err != nil {
		return work.Question{}, err
	}
	if err := s.store.WithinTransaction(ctx, func(repository ports.Repository) error {
		created, err := executeIdempotently(ctx, s, repository, command.ActorID, command.IdempotencyKey, "ask_question", command, func() (work.Question, error) {
			if _, err := repository.Objective(ctx, question.ObjectiveID); err != nil {
				return work.Question{}, fmt.Errorf("load objective: %w", err)
			}
			if err := ensureWorkItemScope(ctx, repository, question.ObjectiveID, question.WorkItemID); err != nil {
				return work.Question{}, err
			}
			if err := repository.CreateQuestion(ctx, question); err != nil {
				return work.Question{}, err
			}
			if err := s.recordActivity(ctx, repository, work.Activity{EntityKind: "question", EntityID: question.ID, WorkItemID: question.WorkItemID, ActorID: command.ActorID, EventType: "question.asked", Summary: "Question asked"}); err != nil {
				return work.Question{}, err
			}
			return question, nil
		})
		question = created
		return err
	}); err != nil {
		return work.Question{}, fmt.Errorf("ask question: %w", err)
	}
	return question, nil
}

type AnswerQuestionCommand struct {
	QuestionID      string
	ActorID         string
	Answer          string
	ExpectedVersion int
	IdempotencyKey  string
}

type WaiveQuestionCommand struct {
	QuestionID      string
	ActorID         string
	Reason          string
	ExpectedVersion int
	IdempotencyKey  string
}

func (s *Service) AnswerQuestion(ctx context.Context, command AnswerQuestionCommand) (work.Question, error) {
	var answered work.Question
	if err := s.store.WithinTransaction(ctx, func(repository ports.Repository) error {
		result, err := executeIdempotently(ctx, s, repository, command.ActorID, command.IdempotencyKey, "answer_question", command, func() (work.Question, error) {
			question, err := repository.Question(ctx, command.QuestionID)
			if err != nil {
				return work.Question{}, err
			}
			if question.Version != command.ExpectedVersion {
				return work.Question{}, ports.ErrVersionConflict
			}
			updated, err := work.AnswerQuestion(question, command.Answer, command.ActorID, s.clock.Now())
			if err != nil {
				return work.Question{}, err
			}
			if err := repository.UpdateQuestion(ctx, updated, command.ExpectedVersion); err != nil {
				return work.Question{}, err
			}
			if err := s.recordActivity(ctx, repository, work.Activity{EntityKind: "question", EntityID: updated.ID, WorkItemID: updated.WorkItemID, ActorID: command.ActorID, EventType: "question.answered", Summary: "Question answered"}); err != nil {
				return work.Question{}, err
			}
			return updated, nil
		})
		answered = result
		return err
	}); err != nil {
		return work.Question{}, fmt.Errorf("answer question: %w", err)
	}
	return answered, nil
}

func (s *Service) WaiveQuestion(ctx context.Context, command WaiveQuestionCommand) (work.Question, error) {
	var waived work.Question
	if err := s.store.WithinTransaction(ctx, func(repository ports.Repository) error {
		result, err := executeIdempotently(ctx, s, repository, command.ActorID, command.IdempotencyKey, "waive_question", command, func() (work.Question, error) {
			question, err := repository.Question(ctx, command.QuestionID)
			if err != nil {
				return work.Question{}, err
			}
			if question.Version != command.ExpectedVersion {
				return work.Question{}, ports.ErrVersionConflict
			}
			updated, err := work.WaiveQuestion(question, command.Reason, command.ActorID, s.clock.Now())
			if err != nil {
				return work.Question{}, err
			}
			if err := repository.UpdateQuestion(ctx, updated, command.ExpectedVersion); err != nil {
				return work.Question{}, err
			}
			if err := s.recordActivity(ctx, repository, work.Activity{EntityKind: "question", EntityID: updated.ID, WorkItemID: updated.WorkItemID, ActorID: command.ActorID, EventType: "question.waived", Summary: "Question waived"}); err != nil {
				return work.Question{}, err
			}
			return updated, nil
		})
		waived = result
		return err
	}); err != nil {
		return work.Question{}, fmt.Errorf("waive question: %w", err)
	}
	return waived, nil
}

type RecordDecisionCommand struct {
	ObjectiveID    string
	WorkItemID     string
	ActorID        string
	Title          string
	Decision       string
	Rationale      string
	Alternatives   []string
	SupersedesID   string
	IdempotencyKey string
}

func (s *Service) RecordDecision(ctx context.Context, command RecordDecisionCommand) (work.Decision, error) {
	if replay, found, err := replayIdempotently[work.Decision](ctx, s, command.ActorID, command.IdempotencyKey, "record_decision", command); err != nil {
		return work.Decision{}, err
	} else if found {
		return replay, nil
	}
	id, err := s.ids.New()
	if err != nil {
		return work.Decision{}, fmt.Errorf("generate decision id: %w", err)
	}
	decision, err := work.NewAcceptedDecision(work.Decision{
		ID:           id,
		ObjectiveID:  command.ObjectiveID,
		WorkItemID:   command.WorkItemID,
		Title:        command.Title,
		Outcome:      command.Decision,
		Rationale:    command.Rationale,
		Alternatives: append([]string(nil), command.Alternatives...),
		SupersedesID: command.SupersedesID,
		DecidedBy:    command.ActorID,
	}, s.clock.Now())
	if err != nil {
		return work.Decision{}, err
	}
	if err := s.store.WithinTransaction(ctx, func(repository ports.Repository) error {
		created, err := executeIdempotently(ctx, s, repository, command.ActorID, command.IdempotencyKey, "record_decision", command, func() (work.Decision, error) {
			if _, err := repository.Objective(ctx, decision.ObjectiveID); err != nil {
				return work.Decision{}, fmt.Errorf("load objective: %w", err)
			}
			if err := ensureWorkItemScope(ctx, repository, decision.ObjectiveID, decision.WorkItemID); err != nil {
				return work.Decision{}, err
			}
			if decision.SupersedesID == "" {
				if err := repository.CreateDecision(ctx, decision); err != nil {
					return work.Decision{}, err
				}
				if err := s.recordActivity(ctx, repository, work.Activity{EntityKind: "decision", EntityID: decision.ID, WorkItemID: decision.WorkItemID, ActorID: command.ActorID, EventType: "decision.recorded", Summary: "Decision recorded"}); err != nil {
					return work.Decision{}, err
				}
				return decision, nil
			}
			previous, err := repository.Decision(ctx, decision.SupersedesID)
			if err != nil {
				return work.Decision{}, fmt.Errorf("load superseded decision: %w", err)
			}
			if previous.ObjectiveID != decision.ObjectiveID {
				return work.Decision{}, errors.New("decision can only supersede another decision in the same objective")
			}
			superseded, err := work.SupersedeDecision(previous)
			if err != nil {
				return work.Decision{}, err
			}
			if err := repository.CreateDecision(ctx, decision); err != nil {
				return work.Decision{}, err
			}
			if err := repository.UpdateDecision(ctx, superseded); err != nil {
				return work.Decision{}, err
			}
			if err := s.recordActivity(ctx, repository, work.Activity{EntityKind: "decision", EntityID: decision.ID, WorkItemID: decision.WorkItemID, ActorID: command.ActorID, EventType: "decision.recorded", Summary: "Decision recorded and predecessor superseded"}); err != nil {
				return work.Decision{}, err
			}
			return decision, nil
		})
		decision = created
		return err
	}); err != nil {
		return work.Decision{}, fmt.Errorf("record decision: %w", err)
	}
	return decision, nil
}

func ensureWorkItemScope(ctx context.Context, repository ports.Repository, objectiveID, workItemID string) error {
	if strings.TrimSpace(workItemID) == "" {
		return nil
	}
	item, err := repository.WorkItem(ctx, workItemID)
	if err != nil {
		return fmt.Errorf("load scoped work item: %w", err)
	}
	if item.ObjectiveID != objectiveID {
		return errors.New("work item belongs to another objective")
	}
	return nil
}

type ProposedExpectedOutput struct {
	Name            string
	ProfileName     string
	ProfileVersion  int
	Contract        json.RawMessage
	DestinationHint string
	Required        bool
	Ordinal         int
}

type ProposedWorkItem struct {
	ClientRef            string
	ParentRef            string
	Key                  string
	Title                string
	Description          string
	Kind                 string
	Priority             work.Priority
	EstimatedScope       work.EstimatedScope
	ExecutionPolicy      work.ExecutionPolicy
	RequiredActorKind    work.ActorKind
	RequiredCapabilities []string
	DependsOn            []string
	AcceptanceCriteria   []ProposedAcceptanceCriterion
	ExpectedOutputs      []ProposedExpectedOutput
	OutputRequirements   []ProposedOutputRequirement
	ExternalActions      []ProposedExternalAction
}

type ProposedOutputRequirement struct {
	RequiredOutputRevisionID string
	RequiredProfileName      string
	VersionConstraint        string
	Required                 bool
	Note                     string
}

type ProposedExternalAction struct {
	Required             bool
	Title                string
	Rationale            string
	AuthorizationSubject json.RawMessage
}

type ProposedAcceptanceCriterion struct {
	Text     string
	Required bool
	Ordinal  int
}

type ProposePlanCommand struct {
	ObjectiveID    string
	ActorID        string
	IdempotencyKey string
	Title          string
	Summary        string
	Revision       int
	Items          []ProposedWorkItem
}

type generatedPlanItem struct {
	clientRef      string
	workItem       work.WorkItem
	capabilities   []string
	dependsOn      []string
	criteria       []work.AcceptanceCriterion
	expectedInputs []generatedExpectedOutput
	requirements   []generatedOutputRequirement
	actions        []generatedExternalAction
}

type generatedExpectedOutput struct {
	id      string
	command ProposedExpectedOutput
}

type generatedOutputRequirement struct {
	id      string
	command ProposedOutputRequirement
}

type generatedExternalAction struct {
	id      string
	command ProposedExternalAction
}

func (s *Service) ProposePlan(ctx context.Context, command ProposePlanCommand) (ports.PlanContext, error) {
	if replay, found, err := replayIdempotently[ports.PlanContext](ctx, s, command.ActorID, command.IdempotencyKey, "propose_plan", command); err != nil {
		return ports.PlanContext{}, err
	} else if found {
		return replay, nil
	}
	if len(command.Items) == 0 {
		return ports.PlanContext{}, errors.New("proposed plan requires at least one work item")
	}
	now := s.clock.Now()
	var plan work.Plan
	var items []generatedPlanItem
	var result ports.PlanContext
	if err := s.store.WithinTransaction(ctx, func(repository ports.Repository) error {
		proposed, err := executeIdempotently(ctx, s, repository, command.ActorID, command.IdempotencyKey, "propose_plan", command, func() (ports.PlanContext, error) {
			revision := command.Revision
			if revision == 0 {
				latest, err := repository.LatestPlanRevision(ctx, command.ObjectiveID)
				if err != nil {
					return ports.PlanContext{}, err
				}
				revision = latest + 1
			}
			if revision < 1 {
				return ports.PlanContext{}, errors.New("plan revision must be positive")
			}
			planID, err := s.ids.New()
			if err != nil {
				return ports.PlanContext{}, fmt.Errorf("generate plan id: %w", err)
			}
			plan, err = work.NewPlan(planID, command.ObjectiveID, command.Title, command.Summary, revision, work.PlanProposed, now)
			if err != nil {
				return ports.PlanContext{}, err
			}
			plan.ProposedBy = strings.TrimSpace(command.ActorID)
			plan.ProposedAt = now.UTC()
			if plan.ProposedBy == "" {
				return ports.PlanContext{}, errors.New("proposed plan requires an actor")
			}
			items, err = s.generatePlanItems(command.Items, plan, now)
			if err != nil {
				return ports.PlanContext{}, err
			}
			result = ports.PlanContext{Plan: plan}
			if _, err := repository.Objective(ctx, plan.ObjectiveID); err != nil {
				return ports.PlanContext{}, fmt.Errorf("load objective: %w", err)
			}
			if err := repository.CreatePlan(ctx, plan); err != nil {
				return ports.PlanContext{}, err
			}
			pending := append([]generatedPlanItem(nil), items...)
			inserted := make(map[string]bool, len(items))
			for len(pending) > 0 {
				remaining := make([]generatedPlanItem, 0, len(pending))
				for _, item := range pending {
					if item.workItem.ParentID != "" && !inserted[item.workItem.ParentID] {
						remaining = append(remaining, item)
						continue
					}
					if err := repository.CreateWorkItem(ctx, item.workItem); err != nil {
						return ports.PlanContext{}, err
					}
					inserted[item.workItem.ID] = true
				}
				if len(remaining) == len(pending) {
					return ports.PlanContext{}, errors.New("could not order recursive work items")
				}
				pending = remaining
			}
			for _, item := range items {
				planned := ports.PlannedWorkItem{WorkItem: item.workItem, RequiredCapabilities: append([]string(nil), item.capabilities...)}
				for _, capability := range item.capabilities {
					if err := repository.AddWorkItemCapability(ctx, item.workItem.ID, capability); err != nil {
						return ports.PlanContext{}, err
					}
				}
				for _, criterion := range item.criteria {
					if err := repository.CreateAcceptanceCriterion(ctx, criterion); err != nil {
						return ports.PlanContext{}, err
					}
				}
				for _, candidate := range item.expectedInputs {
					profile, err := repository.OutputProfile(ctx, candidate.command.ProfileName, candidate.command.ProfileVersion)
					if err != nil {
						return ports.PlanContext{}, fmt.Errorf("load output profile: %w", err)
					}
					expected, err := output.NewExpectedOutput(candidate.id, item.workItem.ID, candidate.command.Name, profile, candidate.command.Contract, candidate.command.DestinationHint, candidate.command.Required, candidate.command.Ordinal)
					if err != nil {
						return ports.PlanContext{}, err
					}
					if err := repository.CreateExpectedOutput(ctx, expected); err != nil {
						return ports.PlanContext{}, err
					}
					planned.ExpectedOutputs = append(planned.ExpectedOutputs, output.ExpectedOutputDetail{ExpectedOutput: expected, Profile: profile})
				}
				for _, candidate := range item.requirements {
					hasRevision := strings.TrimSpace(candidate.command.RequiredOutputRevisionID) != ""
					hasProfile := strings.TrimSpace(candidate.command.RequiredProfileName) != "" || strings.TrimSpace(candidate.command.VersionConstraint) != ""
					if hasRevision == hasProfile {
						return ports.PlanContext{}, errors.New("planned output requirement must select exactly one revision or profile constraint")
					}
					var requirement output.OutputRequirement
					if hasRevision {
						revision, err := repository.OutputRevision(ctx, candidate.command.RequiredOutputRevisionID)
						if err != nil {
							return ports.PlanContext{}, fmt.Errorf("load required output revision: %w", err)
						}
						requirement, err = output.NewExactOutputRequirement(candidate.id, item.workItem.ID, revision, candidate.command.Required, candidate.command.Note)
						if err != nil {
							return ports.PlanContext{}, err
						}
					} else {
						var err error
						requirement, err = output.NewProfileOutputRequirement(candidate.id, item.workItem.ID, candidate.command.RequiredProfileName, candidate.command.VersionConstraint, candidate.command.Required, candidate.command.Note)
						if err != nil {
							return ports.PlanContext{}, err
						}
					}
					if err := repository.CreateOutputRequirement(ctx, requirement); err != nil {
						return ports.PlanContext{}, err
					}
					planned.OutputRequirements = append(planned.OutputRequirements, requirement)
				}
				for _, candidate := range item.actions {
					action, revision, err := authority.NewExternalAction(authority.ExternalAction{ID: candidate.id, WorkItemID: item.workItem.ID, Required: candidate.command.Required, Title: candidate.command.Title, Rationale: candidate.command.Rationale}, candidate.command.AuthorizationSubject, command.ActorID, now)
					if err != nil {
						return ports.PlanContext{}, err
					}
					if err := repository.CreateExternalAction(ctx, action); err != nil {
						return ports.PlanContext{}, err
					}
					if err := repository.CreateExternalActionRevision(ctx, revision); err != nil {
						return ports.PlanContext{}, err
					}
					planned.ExternalActions = append(planned.ExternalActions, action)
				}
				if err := s.recordActivity(ctx, repository, work.Activity{
					EntityKind: "work_item", EntityID: item.workItem.ID, WorkItemID: item.workItem.ID, ActorID: command.ActorID,
					EventType: "work_item.proposed", Summary: fmt.Sprintf("Work item %s proposed with plan revision %d", item.workItem.Key, plan.Revision),
				}); err != nil {
					return ports.PlanContext{}, err
				}
				result.Items = append(result.Items, planned)
			}
			for _, item := range items {
				for _, dependencyRef := range item.dependsOn {
					dependsOnID, exists := idsByClientRef(items, dependencyRef)
					if !exists {
						return ports.PlanContext{}, fmt.Errorf("unknown dependency work item reference %q", dependencyRef)
					}
					if dependsOnID == item.workItem.ID {
						return ports.PlanContext{}, errors.New("work item cannot depend on itself")
					}
					cycle, err := repository.DependencyCreatesCycle(ctx, item.workItem.ID, dependsOnID)
					if err != nil {
						return ports.PlanContext{}, err
					}
					if cycle {
						return ports.PlanContext{}, errors.New("dependency cycle")
					}
					dependencyID, err := s.ids.New()
					if err != nil {
						return ports.PlanContext{}, fmt.Errorf("generate dependency id: %w", err)
					}
					dependency, err := work.NewDependency(work.Dependency{ID: dependencyID, WorkItemID: item.workItem.ID, DependsOnItemID: dependsOnID, Kind: work.DependencyHard, CreatedBy: command.ActorID}, now)
					if err != nil {
						return ports.PlanContext{}, err
					}
					if err := repository.CreateDependency(ctx, dependency); err != nil {
						return ports.PlanContext{}, err
					}
				}
			}
			if err := s.recordActivity(ctx, repository, work.Activity{
				EntityKind: "plan", EntityID: plan.ID, ActorID: command.ActorID,
				EventType: "plan.proposed", Summary: fmt.Sprintf("Plan revision %d proposed with %d work items", plan.Revision, len(items)),
			}); err != nil {
				return ports.PlanContext{}, err
			}
			return result, nil
		})
		result = proposed
		return err
	}); err != nil {
		return ports.PlanContext{}, fmt.Errorf("propose plan: %w", err)
	}
	return result, nil
}

func (s *Service) generatePlanItems(commands []ProposedWorkItem, plan work.Plan, now time.Time) ([]generatedPlanItem, error) {
	idsByRef := make(map[string]string, len(commands))
	commandsByRef := make(map[string]ProposedWorkItem, len(commands))
	for _, command := range commands {
		ref := strings.TrimSpace(command.ClientRef)
		if ref == "" {
			return nil, errors.New("proposed work item requires a client reference")
		}
		if _, exists := idsByRef[ref]; exists {
			return nil, fmt.Errorf("duplicate work item client reference %q", ref)
		}
		id, err := s.ids.New()
		if err != nil {
			return nil, fmt.Errorf("generate work item id: %w", err)
		}
		idsByRef[ref] = id
		commandsByRef[ref] = command
	}
	if err := validateParentReferences(commandsByRef); err != nil {
		return nil, err
	}

	items := make([]generatedPlanItem, 0, len(commands))
	for _, command := range commands {
		item, err := work.NewWorkItem(work.WorkItem{
			ID:                idsByRef[strings.TrimSpace(command.ClientRef)],
			Key:               command.Key,
			ObjectiveID:       plan.ObjectiveID,
			PlanID:            plan.ID,
			ParentID:          idsByRef[strings.TrimSpace(command.ParentRef)],
			Title:             command.Title,
			Description:       command.Description,
			Kind:              command.Kind,
			CommitmentState:   work.ItemProposed,
			ExecutionStatus:   work.StatusBacklog,
			Priority:          command.Priority,
			EstimatedScope:    command.EstimatedScope,
			ExecutionPolicy:   command.ExecutionPolicy,
			RequiredActorKind: command.RequiredActorKind,
			AttentionState:    work.AttentionNone,
		}, now)
		if err != nil {
			return nil, err
		}
		generated := generatedPlanItem{clientRef: strings.TrimSpace(command.ClientRef), workItem: item}
		seenDependencies := make(map[string]bool)
		for _, dependencyRef := range command.DependsOn {
			dependencyRef = strings.TrimSpace(dependencyRef)
			if dependencyRef == "" {
				return nil, errors.New("dependency reference cannot be empty")
			}
			if !seenDependencies[dependencyRef] {
				seenDependencies[dependencyRef] = true
				generated.dependsOn = append(generated.dependsOn, dependencyRef)
			}
		}
		seenCapabilities := make(map[string]bool)
		for _, capability := range command.RequiredCapabilities {
			capability = strings.TrimSpace(capability)
			if capability == "" {
				return nil, errors.New("required capability cannot be empty")
			}
			if !seenCapabilities[capability] {
				seenCapabilities[capability] = true
				generated.capabilities = append(generated.capabilities, capability)
			}
		}
		for _, expected := range command.ExpectedOutputs {
			id, err := s.ids.New()
			if err != nil {
				return nil, fmt.Errorf("generate expected output id: %w", err)
			}
			generated.expectedInputs = append(generated.expectedInputs, generatedExpectedOutput{id: id, command: expected})
		}
		for _, requirement := range command.OutputRequirements {
			id, err := s.ids.New()
			if err != nil {
				return nil, fmt.Errorf("generate output requirement id: %w", err)
			}
			generated.requirements = append(generated.requirements, generatedOutputRequirement{id: id, command: requirement})
		}
		for _, action := range command.ExternalActions {
			id, err := s.ids.New()
			if err != nil {
				return nil, fmt.Errorf("generate external action id: %w", err)
			}
			generated.actions = append(generated.actions, generatedExternalAction{id: id, command: action})
		}
		for _, proposed := range command.AcceptanceCriteria {
			id, err := s.ids.New()
			if err != nil {
				return nil, fmt.Errorf("generate acceptance criterion id: %w", err)
			}
			criterion, err := work.NewAcceptanceCriterion(work.AcceptanceCriterion{
				ID: id, WorkItemID: item.ID, Text: proposed.Text, Required: proposed.Required, Ordinal: proposed.Ordinal,
			})
			if err != nil {
				return nil, err
			}
			generated.criteria = append(generated.criteria, criterion)
		}
		items = append(items, generated)
	}
	return items, nil
}

func idsByClientRef(items []generatedPlanItem, clientRef string) (string, bool) {
	for _, item := range items {
		if item.clientRef == clientRef {
			return item.workItem.ID, true
		}
	}
	return "", false
}

func validateParentReferences(commands map[string]ProposedWorkItem) error {
	for ref, command := range commands {
		seen := map[string]bool{ref: true}
		parent := strings.TrimSpace(command.ParentRef)
		for parent != "" {
			candidate, exists := commands[parent]
			if !exists {
				return fmt.Errorf("unknown parent work item reference %q", parent)
			}
			if seen[parent] {
				return fmt.Errorf("recursive work item parent cycle at %q", parent)
			}
			seen[parent] = true
			parent = strings.TrimSpace(candidate.ParentRef)
		}
	}
	return nil
}

type ReviewPlanCommand struct {
	PlanID          string
	ReviewerActorID string
	Decision        work.PlanCommitment
	Reason          string
	ExpectedVersion int
	IdempotencyKey  string
}

type RequestApprovalCommand struct {
	ActorID               string
	IdempotencyKey        string
	Request               string
	PlanID                string
	WorkItemID            string
	OutputProfileID       string
	OutputRevisionID      string
	ExpectedTargetVersion int
}

func (s *Service) RequestApproval(ctx context.Context, command RequestApprovalCommand) (work.Approval, error) {
	if replay, found, err := replayIdempotently[work.Approval](ctx, s, command.ActorID, command.IdempotencyKey, "request_approval", command); err != nil {
		return work.Approval{}, err
	} else if found {
		return replay, nil
	}
	id, err := s.ids.New()
	if err != nil {
		return work.Approval{}, fmt.Errorf("generate approval id: %w", err)
	}
	var result work.Approval
	err = s.store.WithinTransaction(ctx, func(repository ports.Repository) error {
		created, err := executeIdempotently(ctx, s, repository, command.ActorID, command.IdempotencyKey, "request_approval", command, func() (work.Approval, error) {
			if _, err := repository.Actor(ctx, command.ActorID); err != nil {
				return work.Approval{}, err
			}
			approval := work.Approval{ID: id, PlanID: command.PlanID, WorkItemID: command.WorkItemID, OutputProfileID: command.OutputProfileID, OutputRevisionID: command.OutputRevisionID, Request: command.Request, RequestedBy: command.ActorID}
			if approval.PlanID != "" {
				plan, err := repository.Plan(ctx, approval.PlanID)
				if err != nil {
					return work.Approval{}, err
				}
				if command.ExpectedTargetVersion > 0 && plan.Version != command.ExpectedTargetVersion {
					return work.Approval{}, ports.ErrVersionConflict
				}
				approval.ObjectiveID = plan.ObjectiveID
			}
			if approval.WorkItemID != "" {
				item, err := repository.WorkItem(ctx, approval.WorkItemID)
				if err != nil {
					return work.Approval{}, err
				}
				if command.ExpectedTargetVersion > 0 && item.Version != command.ExpectedTargetVersion {
					return work.Approval{}, ports.ErrVersionConflict
				}
				approval.ObjectiveID = item.ObjectiveID
			}
			if approval.OutputProfileID != "" {
				profile, err := repository.OutputProfileByID(ctx, approval.OutputProfileID)
				if err != nil {
					return work.Approval{}, err
				}
				if command.ExpectedTargetVersion > 0 && profile.StateVersion != command.ExpectedTargetVersion {
					return work.Approval{}, ports.ErrVersionConflict
				}
			}
			if approval.OutputRevisionID != "" {
				revision, err := repository.OutputRevision(ctx, approval.OutputRevisionID)
				if err != nil {
					return work.Approval{}, err
				}
				if command.ExpectedTargetVersion > 0 && revision.Revision != command.ExpectedTargetVersion {
					return work.Approval{}, ports.ErrVersionConflict
				}
				expected, err := repository.ExpectedOutput(ctx, revision.ExpectedOutputID)
				if err != nil {
					return work.Approval{}, err
				}
				item, err := repository.WorkItem(ctx, expected.WorkItemID)
				if err != nil {
					return work.Approval{}, err
				}
				approval.ObjectiveID = item.ObjectiveID
			}
			approval, err := work.NewApprovalRequest(approval, s.clock.Now())
			if err != nil {
				return work.Approval{}, err
			}
			if err := repository.CreateApproval(ctx, approval); err != nil {
				return work.Approval{}, err
			}
			if err := s.recordActivity(ctx, repository, work.Activity{EntityKind: "approval", EntityID: approval.ID, WorkItemID: approval.WorkItemID, ActorID: command.ActorID, EventType: "approval.requested", Summary: "Approval requested"}); err != nil {
				return work.Approval{}, err
			}
			return approval, nil
		})
		result = created
		return err
	})
	if err != nil {
		return work.Approval{}, fmt.Errorf("request approval: %w", err)
	}
	return result, nil
}

type ResolveApprovalCommand struct {
	ApprovalID      string
	ActorID         string
	IdempotencyKey  string
	ExpectedVersion int
	Decision        work.ApprovalStatus
	Rationale       string
}

func (s *Service) GetApproval(ctx context.Context, id string) (work.Approval, error) {
	var approval work.Approval
	err := s.store.WithinTransaction(ctx, func(repository ports.Repository) error {
		var err error
		approval, err = repository.Approval(ctx, id)
		return err
	})
	return approval, err
}

func (s *Service) ResolveApproval(ctx context.Context, command ResolveApprovalCommand) (work.Approval, error) {
	var result work.Approval
	err := s.store.WithinTransaction(ctx, func(repository ports.Repository) error {
		resolved, err := executeIdempotently(ctx, s, repository, command.ActorID, command.IdempotencyKey, "resolve_approval", command, func() (work.Approval, error) {
			if _, err := repository.Actor(ctx, command.ActorID); err != nil {
				return work.Approval{}, err
			}
			approval, err := repository.Approval(ctx, command.ApprovalID)
			if err != nil {
				return work.Approval{}, err
			}
			if approval.Version != command.ExpectedVersion {
				return work.Approval{}, ports.ErrVersionConflict
			}
			approval, err = work.ResolveApproval(approval, command.Decision, command.ActorID, command.Rationale, s.clock.Now())
			if err != nil {
				return work.Approval{}, err
			}
			if err := repository.UpdateApproval(ctx, approval, command.ExpectedVersion); err != nil {
				return work.Approval{}, err
			}
			if err := s.recordActivity(ctx, repository, work.Activity{EntityKind: "approval", EntityID: approval.ID, WorkItemID: approval.WorkItemID, ActorID: command.ActorID, EventType: "approval.resolved", Summary: fmt.Sprintf("Approval marked %s", approval.Status)}); err != nil {
				return work.Approval{}, err
			}
			return approval, nil
		})
		result = resolved
		return err
	})
	if err != nil {
		return work.Approval{}, fmt.Errorf("resolve approval: %w", err)
	}
	return result, nil
}

func (s *Service) ReviewPlan(ctx context.Context, command ReviewPlanCommand) (work.Plan, error) {
	if replay, found, err := replayIdempotently[work.Plan](ctx, s, command.ReviewerActorID, command.IdempotencyKey, "review_plan", command); err != nil {
		return work.Plan{}, err
	} else if found {
		return replay, nil
	}
	approvalID, err := s.ids.New()
	if err != nil {
		return work.Plan{}, fmt.Errorf("generate approval id: %w", err)
	}
	var reviewed work.Plan
	if err := s.store.WithinTransaction(ctx, func(repository ports.Repository) error {
		result, err := executeIdempotently(ctx, s, repository, command.ReviewerActorID, command.IdempotencyKey, "review_plan", command, func() (work.Plan, error) {
			plan, err := repository.Plan(ctx, command.PlanID)
			if err != nil {
				return work.Plan{}, err
			}
			if plan.Version != command.ExpectedVersion {
				return work.Plan{}, ports.ErrVersionConflict
			}
			reviewed, err = work.ReviewPlan(plan, command.Decision, command.ReviewerActorID, command.Reason, s.clock.Now())
			if err != nil {
				return work.Plan{}, err
			}
			if reviewed.CommitmentState == work.PlanApproved {
				latestRevision, err := repository.LatestApprovedPlanRevision(ctx, reviewed.ObjectiveID)
				if err != nil {
					return work.Plan{}, err
				}
				if latestRevision >= reviewed.Revision {
					return work.Plan{}, errors.New("a plan with the same or newer revision is already approved")
				}
				if err := repository.SupersedeEarlierPlans(ctx, reviewed.ObjectiveID, reviewed.Revision, reviewed.ResolvedAt); err != nil {
					return work.Plan{}, err
				}
			}
			if err := repository.UpdatePlan(ctx, reviewed, command.ExpectedVersion); err != nil {
				return work.Plan{}, err
			}
			itemState := work.ItemRejected
			if reviewed.CommitmentState == work.PlanApproved {
				itemState = work.ItemAccepted
			}
			if err := repository.SetPlanItemsCommitment(ctx, reviewed.ID, itemState, reviewed.ResolvedAt); err != nil {
				return work.Plan{}, err
			}
			approval, err := work.NewPlanApproval(approvalID, reviewed)
			if err != nil {
				return work.Plan{}, err
			}
			if err := repository.CreateApproval(ctx, approval); err != nil {
				return work.Plan{}, err
			}
			if err := s.recordActivity(ctx, repository, work.Activity{
				EntityKind: "plan", EntityID: reviewed.ID, ActorID: command.ReviewerActorID,
				EventType: "plan.reviewed", Summary: fmt.Sprintf("Plan revision %d marked %s", reviewed.Revision, reviewed.CommitmentState),
			}); err != nil {
				return work.Plan{}, err
			}
			return reviewed, nil
		})
		reviewed = result
		return err
	}); err != nil {
		return work.Plan{}, fmt.Errorf("review plan: %w", err)
	}
	return reviewed, nil
}

type ProposeOutputProfileCommand struct {
	ActorID        string
	IdempotencyKey string
	Name           string
	Version        int
	Description    string
	Structure      json.RawMessage
	Semantics      json.RawMessage
	Validation     json.RawMessage
	SupersedesID   string
	Supersedes     string
}

func (s *Service) ProposeOutputProfile(ctx context.Context, command ProposeOutputProfileCommand) (output.Profile, error) {
	if replay, found, err := replayIdempotently[output.Profile](ctx, s, command.ActorID, command.IdempotencyKey, "propose_output_profile", command); err != nil {
		return output.Profile{}, err
	} else if found {
		return replay, nil
	}
	if strings.TrimSpace(command.SupersedesID) != "" && strings.TrimSpace(command.Supersedes) != "" {
		return output.Profile{}, errors.New("output profile proposal accepts either supersedes or supersedes_id")
	}
	hasPredecessor := strings.TrimSpace(command.SupersedesID) != "" || strings.TrimSpace(command.Supersedes) != ""
	if command.Version > 1 && !hasPredecessor {
		return output.Profile{}, errors.New("later output profile versions require a predecessor")
	}
	if command.Version == 1 && hasPredecessor {
		return output.Profile{}, errors.New("first output profile version cannot supersede another profile")
	}
	id, err := s.ids.New()
	if err != nil {
		return output.Profile{}, fmt.Errorf("generate output profile id: %w", err)
	}
	profile, err := output.NewProfileProposal(output.Profile{
		ID:           id,
		Name:         command.Name,
		Version:      command.Version,
		Description:  command.Description,
		Structure:    append(json.RawMessage(nil), command.Structure...),
		Semantics:    append(json.RawMessage(nil), command.Semantics...),
		Validation:   append(json.RawMessage(nil), command.Validation...),
		SupersedesID: command.SupersedesID,
		ProposedBy:   command.ActorID,
	}, s.clock.Now())
	if err != nil {
		return output.Profile{}, err
	}
	if err := s.store.WithinTransaction(ctx, func(repository ports.Repository) error {
		proposed, err := executeIdempotently(ctx, s, repository, command.ActorID, command.IdempotencyKey, "propose_output_profile", command, func() (output.Profile, error) {
			if strings.TrimSpace(command.Supersedes) != "" {
				name, version, err := parseOutputProfileSelector(command.Supersedes)
				if err != nil {
					return output.Profile{}, err
				}
				predecessor, err := repository.OutputProfile(ctx, name, version)
				if err != nil {
					return output.Profile{}, fmt.Errorf("load superseded output profile: %w", err)
				}
				profile.SupersedesID = predecessor.ID
			}
			latestVersion, err := repository.LatestOutputProfileVersion(ctx, profile.Name)
			if err != nil {
				return output.Profile{}, err
			}
			if profile.Version != latestVersion+1 {
				return output.Profile{}, fmt.Errorf("output profile version must be %d", latestVersion+1)
			}
			if profile.SupersedesID != "" {
				predecessor, err := repository.OutputProfileByID(ctx, profile.SupersedesID)
				if err != nil {
					return output.Profile{}, fmt.Errorf("load superseded output profile: %w", err)
				}
				if predecessor.Name != profile.Name || predecessor.Version >= profile.Version || predecessor.LifecycleState != output.ProfileActive {
					return output.Profile{}, errors.New("output profile successor must reference an earlier active version with the same name")
				}
			}
			if err := repository.CreateOutputProfile(ctx, profile); err != nil {
				return output.Profile{}, err
			}
			if err := s.recordActivity(ctx, repository, work.Activity{
				EntityKind: "output_profile", EntityID: profile.ID, ActorID: command.ActorID,
				EventType: "output_profile.proposed", Summary: fmt.Sprintf("Output profile %s/v%d proposed", profile.Name, profile.Version),
			}); err != nil {
				return output.Profile{}, err
			}
			return profile, nil
		})
		profile = proposed
		return err
	}); err != nil {
		return output.Profile{}, fmt.Errorf("propose output profile: %w", err)
	}
	return profile, nil
}

func parseOutputProfileSelector(selector string) (string, int, error) {
	name, versionText, found := strings.Cut(strings.TrimSpace(selector), "/v")
	if !found || strings.TrimSpace(name) == "" || strings.Contains(versionText, "/") {
		return "", 0, errors.New("supersedes must be formatted as name/v<version>")
	}
	version, err := strconv.Atoi(versionText)
	if err != nil || version < 1 {
		return "", 0, errors.New("supersedes must be formatted as name/v<version>")
	}
	return name, version, nil
}

type ReviewOutputProfileCommand struct {
	ProfileID       string
	ReviewerActorID string
	IdempotencyKey  string
	ExpectedVersion int
	Decision        output.ProfileState
	Reason          string
}

func (s *Service) ReviewOutputProfile(ctx context.Context, command ReviewOutputProfileCommand) (output.Profile, error) {
	var reviewed output.Profile
	if err := s.store.WithinTransaction(ctx, func(repository ports.Repository) error {
		result, err := executeIdempotently(ctx, s, repository, command.ReviewerActorID, command.IdempotencyKey, "review_output_profile", command, func() (output.Profile, error) {
			profile, err := repository.OutputProfileByID(ctx, command.ProfileID)
			if err != nil {
				return output.Profile{}, err
			}
			if command.ExpectedVersion > 0 && profile.StateVersion != command.ExpectedVersion {
				return output.Profile{}, ports.ErrVersionConflict
			}
			reviewed, err = output.ReviewProfile(profile, command.Decision, command.ReviewerActorID, command.Reason, s.clock.Now())
			if err != nil {
				return output.Profile{}, err
			}
			reviewed.StateVersion++
			if reviewed.LifecycleState == output.ProfileActive && reviewed.SupersedesID != "" {
				if err := repository.SupersedeOutputProfile(ctx, reviewed.SupersedesID); err != nil {
					return output.Profile{}, err
				}
			}
			if err := repository.UpdateOutputProfile(ctx, reviewed); err != nil {
				return output.Profile{}, err
			}
			if err := s.recordActivity(ctx, repository, work.Activity{
				EntityKind: "output_profile", EntityID: reviewed.ID, ActorID: command.ReviewerActorID,
				EventType: "output_profile.reviewed", Summary: fmt.Sprintf("Output profile %s/v%d marked %s", reviewed.Name, reviewed.Version, reviewed.LifecycleState),
			}); err != nil {
				return output.Profile{}, err
			}
			return reviewed, nil
		})
		reviewed = result
		return err
	}); err != nil {
		return output.Profile{}, fmt.Errorf("review output profile: %w", err)
	}
	return reviewed, nil
}

func (s *Service) GetObjectiveContext(ctx context.Context, id string) (ports.ObjectiveContext, error) {
	result, err := s.store.GetObjectiveContext(ctx, id)
	if err != nil {
		return ports.ObjectiveContext{}, fmt.Errorf("get objective context: %w", err)
	}
	return result, nil
}

type ObjectiveContextQuery struct {
	ObjectiveID        string
	ActorID            string
	Include            []string
	MaxItemsPerSection int
}

type ObjectiveContextSnapshot struct {
	Objective            work.Objective               `json:"objective"`
	SelectedContext      []work.ContextRecord         `json:"selected_context"`
	Plans                []ports.PlanContext          `json:"plans"`
	Questions            []work.Question              `json:"questions"`
	Decisions            []work.Decision              `json:"decisions"`
	Approvals            []work.Approval              `json:"approvals"`
	ActorRelevantWork    []ports.WorkItemContext      `json:"actor_relevant_work"`
	AcceptedOutputs      []ports.OutputRevisionDetail `json:"accepted_outputs"`
	AuthorityAndEvidence []ports.ExternalActionDetail `json:"authority_and_evidence"`
	Artifacts            []output.Artifact            `json:"artifacts"`
	RecentChanges        []work.Activity              `json:"recent_changes"`
}

// SelectObjectiveContext applies the documented actor-aware continuation
// selection in the application layer, rather than in a transport adapter.
func (s *Service) SelectObjectiveContext(ctx context.Context, query ObjectiveContextQuery) (ObjectiveContextSnapshot, error) {
	limit := query.MaxItemsPerSection
	if limit <= 0 || limit > 100 {
		limit = 20
	}
	selection, err := s.store.SelectObjectiveContext(ctx, ports.ObjectiveContextSelectionQuery{
		ObjectiveID: query.ObjectiveID,
		ActorID:     strings.TrimSpace(query.ActorID),
		Limit:       limit,
	})
	if err != nil {
		return ObjectiveContextSnapshot{}, err
	}
	context := selection.Context
	snapshot := ObjectiveContextSnapshot{Objective: context.Objective, SelectedContext: limitSlice(context.ContextRecords, limit), Plans: limitSlice(approvedPlans(context.Plans), limit), Questions: limitSlice(openQuestions(context.Questions), limit), Decisions: limitSlice(context.Decisions, limit), Approvals: limitSlice(context.Approvals, limit), RecentChanges: selection.RecentChanges}
	for _, item := range selection.WorkItems {
		snapshot.ActorRelevantWork = append(snapshot.ActorRelevantWork, item)
		for _, revision := range item.OutputRevisions {
			if revision.Revision.AcceptanceState == output.RevisionAccepted {
				snapshot.AcceptedOutputs = append(snapshot.AcceptedOutputs, revision)
			}
		}
		snapshot.AuthorityAndEvidence = append(snapshot.AuthorityAndEvidence, item.ExternalActions...)
		snapshot.Artifacts = append(snapshot.Artifacts, item.Artifacts...)
	}
	if len(query.Include) > 0 {
		include := make(map[string]bool, len(query.Include))
		for _, section := range query.Include {
			include[strings.TrimSpace(section)] = true
		}
		if !include["intent"] {
			snapshot.SelectedContext = nil
		}
		if !include["approved_plan"] {
			snapshot.Plans = nil
		}
		if !include["open_questions"] {
			snapshot.Questions = nil
		}
		if !include["decisions"] {
			snapshot.Decisions = nil
		}
		if !include["approvals"] {
			snapshot.Approvals = nil
		}
		if !include["ready_work"] {
			snapshot.ActorRelevantWork = nil
		}
		if !include["accepted_outputs"] {
			snapshot.AcceptedOutputs = nil
		}
		if !include["external_actions"] && !include["authority"] {
			snapshot.AuthorityAndEvidence = nil
		}
		if !include["artifacts"] {
			snapshot.Artifacts = nil
		}
		if !include["recent_changes"] {
			snapshot.RecentChanges = nil
		}
	}
	return snapshot, nil
}

func approvedPlans(plans []ports.PlanContext) []ports.PlanContext {
	result := make([]ports.PlanContext, 0, len(plans))
	for _, plan := range plans {
		if plan.Plan.CommitmentState == work.PlanApproved {
			result = append(result, plan)
		}
	}
	return result
}

func openQuestions(questions []work.Question) []work.Question {
	result := make([]work.Question, 0, len(questions))
	for _, question := range questions {
		if question.Status == work.QuestionOpen {
			result = append(result, question)
		}
	}
	return result
}

func limitSlice[T any](values []T, limit int) []T {
	if len(values) <= limit {
		return values
	}
	return values[:limit]
}

func (s *Service) ListOutputProfiles(ctx context.Context) ([]output.Profile, error) {
	profiles, err := s.store.ListOutputProfiles(ctx)
	if err != nil {
		return nil, fmt.Errorf("list output profiles: %w", err)
	}
	return profiles, nil
}
