package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/dennisschroeder/workgraph/internal/domain/output"
	"github.com/dennisschroeder/workgraph/internal/domain/work"
	"github.com/dennisschroeder/workgraph/internal/ports"
)

type RecordContextCommand struct {
	ObjectiveID  string
	WorkItemID   string
	ActorID      string
	Kind         work.ContextKind
	Title        string
	Body         string
	Status       work.ContextStatus
	Confidence   string
	SourceURI    string
	SupersedesID string
}

type TransitionObjectiveCommand struct {
	ObjectiveID     string
	TargetPhase     work.ObjectivePhase
	ActorID         string
	Reason          string
	ExpectedVersion int
}

func (s *Service) TransitionObjective(ctx context.Context, command TransitionObjectiveCommand) (work.Objective, error) {
	if strings.TrimSpace(command.ActorID) == "" {
		return work.Objective{}, errors.New("objective transition requires an actor")
	}
	var transitioned work.Objective
	if err := s.store.WithinTransaction(ctx, func(repository ports.Repository) error {
		objective, err := repository.Objective(ctx, command.ObjectiveID)
		if err != nil {
			return err
		}
		if objective.Version != command.ExpectedVersion {
			return ports.ErrVersionConflict
		}
		if command.TargetPhase == work.ObjectiveExecution {
			approved, err := repository.HasApprovedPlan(ctx, objective.ID)
			if err != nil {
				return err
			}
			if !approved {
				return errors.New("objective cannot enter execution without an approved plan")
			}
		}
		transitioned, err = work.TransitionObjective(objective, command.TargetPhase, command.Reason, s.clock.Now())
		if err != nil {
			return err
		}
		transitioned.UpdatedBy = strings.TrimSpace(command.ActorID)
		if err := repository.UpdateObjective(ctx, transitioned, command.ExpectedVersion); err != nil {
			return err
		}
		return s.recordActivity(ctx, repository, work.Activity{
			EntityKind: "objective", EntityID: transitioned.ID, ActorID: command.ActorID,
			EventType: "objective.phase_changed", Summary: fmt.Sprintf("Objective moved from %s to %s", objective.Phase, transitioned.Phase),
		})
	}); err != nil {
		return work.Objective{}, fmt.Errorf("transition objective: %w", err)
	}
	return transitioned, nil
}

func (s *Service) RecordContext(ctx context.Context, command RecordContextCommand) (work.ContextRecord, error) {
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
		if _, err := repository.Objective(ctx, record.ObjectiveID); err != nil {
			return fmt.Errorf("load objective: %w", err)
		}
		if err := ensureWorkItemScope(ctx, repository, record.ObjectiveID, record.WorkItemID); err != nil {
			return err
		}
		if record.SupersedesID == "" {
			if err := repository.CreateContextRecord(ctx, record); err != nil {
				return err
			}
			return s.recordActivity(ctx, repository, work.Activity{
				EntityKind: "context_record", EntityID: record.ID, WorkItemID: record.WorkItemID, ActorID: command.ActorID,
				EventType: "context_record.recorded", Summary: fmt.Sprintf("Context %s recorded", record.Kind),
			})
		}
		previous, err := repository.ContextRecord(ctx, record.SupersedesID)
		if err != nil {
			return fmt.Errorf("load superseded context record: %w", err)
		}
		if previous.ObjectiveID != record.ObjectiveID || previous.Kind != record.Kind {
			return errors.New("context record can only supersede the same kind in the same objective")
		}
		superseded, err := work.SupersedeContextRecord(previous, record.CreatedBy, record.CreatedAt)
		if err != nil {
			return err
		}
		if err := repository.CreateContextRecord(ctx, record); err != nil {
			return err
		}
		if err := repository.UpdateContextRecord(ctx, superseded, previous.Version); err != nil {
			return err
		}
		return s.recordActivity(ctx, repository, work.Activity{
			EntityKind: "context_record", EntityID: record.ID, WorkItemID: record.WorkItemID, ActorID: command.ActorID,
			EventType: "context_record.recorded", Summary: fmt.Sprintf("Context %s recorded and predecessor superseded", record.Kind),
		})
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
}

func (s *Service) AskQuestion(ctx context.Context, command AskQuestionCommand) (work.Question, error) {
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
		if _, err := repository.Objective(ctx, question.ObjectiveID); err != nil {
			return fmt.Errorf("load objective: %w", err)
		}
		if err := ensureWorkItemScope(ctx, repository, question.ObjectiveID, question.WorkItemID); err != nil {
			return err
		}
		if err := repository.CreateQuestion(ctx, question); err != nil {
			return err
		}
		return s.recordActivity(ctx, repository, work.Activity{
			EntityKind: "question", EntityID: question.ID, WorkItemID: question.WorkItemID, ActorID: command.ActorID,
			EventType: "question.asked", Summary: "Question asked",
		})
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
}

type WaiveQuestionCommand struct {
	QuestionID      string
	ActorID         string
	Reason          string
	ExpectedVersion int
}

func (s *Service) AnswerQuestion(ctx context.Context, command AnswerQuestionCommand) (work.Question, error) {
	var answered work.Question
	if err := s.store.WithinTransaction(ctx, func(repository ports.Repository) error {
		question, err := repository.Question(ctx, command.QuestionID)
		if err != nil {
			return err
		}
		if question.Version != command.ExpectedVersion {
			return ports.ErrVersionConflict
		}
		answered, err = work.AnswerQuestion(question, command.Answer, command.ActorID, s.clock.Now())
		if err != nil {
			return err
		}
		if err := repository.UpdateQuestion(ctx, answered, command.ExpectedVersion); err != nil {
			return err
		}
		return s.recordActivity(ctx, repository, work.Activity{
			EntityKind: "question", EntityID: answered.ID, WorkItemID: answered.WorkItemID, ActorID: command.ActorID,
			EventType: "question.answered", Summary: "Question answered",
		})
	}); err != nil {
		return work.Question{}, fmt.Errorf("answer question: %w", err)
	}
	return answered, nil
}

func (s *Service) WaiveQuestion(ctx context.Context, command WaiveQuestionCommand) (work.Question, error) {
	var waived work.Question
	if err := s.store.WithinTransaction(ctx, func(repository ports.Repository) error {
		question, err := repository.Question(ctx, command.QuestionID)
		if err != nil {
			return err
		}
		if question.Version != command.ExpectedVersion {
			return ports.ErrVersionConflict
		}
		waived, err = work.WaiveQuestion(question, command.Reason, command.ActorID, s.clock.Now())
		if err != nil {
			return err
		}
		if err := repository.UpdateQuestion(ctx, waived, command.ExpectedVersion); err != nil {
			return err
		}
		return s.recordActivity(ctx, repository, work.Activity{
			EntityKind: "question", EntityID: waived.ID, WorkItemID: waived.WorkItemID, ActorID: command.ActorID,
			EventType: "question.waived", Summary: "Question waived",
		})
	}); err != nil {
		return work.Question{}, fmt.Errorf("waive question: %w", err)
	}
	return waived, nil
}

type RecordDecisionCommand struct {
	ObjectiveID  string
	WorkItemID   string
	ActorID      string
	Title        string
	Decision     string
	Rationale    string
	Alternatives []string
	SupersedesID string
}

func (s *Service) RecordDecision(ctx context.Context, command RecordDecisionCommand) (work.Decision, error) {
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
		if _, err := repository.Objective(ctx, decision.ObjectiveID); err != nil {
			return fmt.Errorf("load objective: %w", err)
		}
		if err := ensureWorkItemScope(ctx, repository, decision.ObjectiveID, decision.WorkItemID); err != nil {
			return err
		}
		if decision.SupersedesID == "" {
			if err := repository.CreateDecision(ctx, decision); err != nil {
				return err
			}
			return s.recordActivity(ctx, repository, work.Activity{
				EntityKind: "decision", EntityID: decision.ID, WorkItemID: decision.WorkItemID, ActorID: command.ActorID,
				EventType: "decision.recorded", Summary: "Decision recorded",
			})
		}
		previous, err := repository.Decision(ctx, decision.SupersedesID)
		if err != nil {
			return fmt.Errorf("load superseded decision: %w", err)
		}
		if previous.ObjectiveID != decision.ObjectiveID {
			return errors.New("decision can only supersede another decision in the same objective")
		}
		superseded, err := work.SupersedeDecision(previous)
		if err != nil {
			return err
		}
		if err := repository.CreateDecision(ctx, decision); err != nil {
			return err
		}
		if err := repository.UpdateDecision(ctx, superseded); err != nil {
			return err
		}
		return s.recordActivity(ctx, repository, work.Activity{
			EntityKind: "decision", EntityID: decision.ID, WorkItemID: decision.WorkItemID, ActorID: command.ActorID,
			EventType: "decision.recorded", Summary: "Decision recorded and predecessor superseded",
		})
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
	AcceptanceCriteria   []ProposedAcceptanceCriterion
	ExpectedOutputs      []ProposedExpectedOutput
}

type ProposedAcceptanceCriterion struct {
	Text     string
	Required bool
	Ordinal  int
}

type ProposePlanCommand struct {
	ObjectiveID string
	ActorID     string
	Title       string
	Summary     string
	Revision    int
	Items       []ProposedWorkItem
}

type generatedPlanItem struct {
	workItem       work.WorkItem
	capabilities   []string
	criteria       []work.AcceptanceCriterion
	expectedInputs []generatedExpectedOutput
}

type generatedExpectedOutput struct {
	id      string
	command ProposedExpectedOutput
}

func (s *Service) ProposePlan(ctx context.Context, command ProposePlanCommand) (ports.PlanContext, error) {
	if len(command.Items) == 0 {
		return ports.PlanContext{}, errors.New("proposed plan requires at least one work item")
	}
	planID, err := s.ids.New()
	if err != nil {
		return ports.PlanContext{}, fmt.Errorf("generate plan id: %w", err)
	}
	now := s.clock.Now()
	plan, err := work.NewPlan(planID, command.ObjectiveID, command.Title, command.Summary, command.Revision, work.PlanProposed, now)
	if err != nil {
		return ports.PlanContext{}, err
	}
	plan.ProposedBy = strings.TrimSpace(command.ActorID)
	plan.ProposedAt = now.UTC()
	if plan.ProposedBy == "" {
		return ports.PlanContext{}, errors.New("proposed plan requires an actor")
	}

	items, err := s.generatePlanItems(command.Items, plan, now)
	if err != nil {
		return ports.PlanContext{}, err
	}
	result := ports.PlanContext{Plan: plan}
	if err := s.store.WithinTransaction(ctx, func(repository ports.Repository) error {
		if _, err := repository.Objective(ctx, plan.ObjectiveID); err != nil {
			return fmt.Errorf("load objective: %w", err)
		}
		if err := repository.CreatePlan(ctx, plan); err != nil {
			return err
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
					return err
				}
				inserted[item.workItem.ID] = true
			}
			if len(remaining) == len(pending) {
				return errors.New("could not order recursive work items")
			}
			pending = remaining
		}
		for _, item := range items {
			planned := ports.PlannedWorkItem{WorkItem: item.workItem, RequiredCapabilities: append([]string(nil), item.capabilities...)}
			for _, capability := range item.capabilities {
				if err := repository.AddWorkItemCapability(ctx, item.workItem.ID, capability); err != nil {
					return err
				}
			}
			for _, criterion := range item.criteria {
				if err := repository.CreateAcceptanceCriterion(ctx, criterion); err != nil {
					return err
				}
			}
			for _, candidate := range item.expectedInputs {
				profile, err := repository.OutputProfile(ctx, candidate.command.ProfileName, candidate.command.ProfileVersion)
				if err != nil {
					return fmt.Errorf("load output profile: %w", err)
				}
				expected, err := output.NewExpectedOutput(candidate.id, item.workItem.ID, candidate.command.Name, profile, candidate.command.Contract, candidate.command.DestinationHint, candidate.command.Required, candidate.command.Ordinal)
				if err != nil {
					return err
				}
				if err := repository.CreateExpectedOutput(ctx, expected); err != nil {
					return err
				}
				planned.ExpectedOutputs = append(planned.ExpectedOutputs, output.ExpectedOutputDetail{ExpectedOutput: expected, Profile: profile})
			}
			if err := s.recordActivity(ctx, repository, work.Activity{
				EntityKind: "work_item", EntityID: item.workItem.ID, WorkItemID: item.workItem.ID, ActorID: command.ActorID,
				EventType: "work_item.proposed", Summary: fmt.Sprintf("Work item %s proposed with plan revision %d", item.workItem.Key, plan.Revision),
			}); err != nil {
				return err
			}
			result.Items = append(result.Items, planned)
		}
		return s.recordActivity(ctx, repository, work.Activity{
			EntityKind: "plan", EntityID: plan.ID, ActorID: command.ActorID,
			EventType: "plan.proposed", Summary: fmt.Sprintf("Plan revision %d proposed with %d work items", plan.Revision, len(items)),
		})
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
		generated := generatedPlanItem{workItem: item}
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
}

func (s *Service) ReviewPlan(ctx context.Context, command ReviewPlanCommand) (work.Plan, error) {
	approvalID, err := s.ids.New()
	if err != nil {
		return work.Plan{}, fmt.Errorf("generate approval id: %w", err)
	}
	var reviewed work.Plan
	if err := s.store.WithinTransaction(ctx, func(repository ports.Repository) error {
		plan, err := repository.Plan(ctx, command.PlanID)
		if err != nil {
			return err
		}
		if plan.Version != command.ExpectedVersion {
			return ports.ErrVersionConflict
		}
		reviewed, err = work.ReviewPlan(plan, command.Decision, command.ReviewerActorID, command.Reason, s.clock.Now())
		if err != nil {
			return err
		}
		if reviewed.CommitmentState == work.PlanApproved {
			latestRevision, err := repository.LatestApprovedPlanRevision(ctx, reviewed.ObjectiveID)
			if err != nil {
				return err
			}
			if latestRevision >= reviewed.Revision {
				return errors.New("a plan with the same or newer revision is already approved")
			}
			if err := repository.SupersedeEarlierPlans(ctx, reviewed.ObjectiveID, reviewed.Revision, reviewed.ResolvedAt); err != nil {
				return err
			}
		}
		if err := repository.UpdatePlan(ctx, reviewed, command.ExpectedVersion); err != nil {
			return err
		}
		itemState := work.ItemRejected
		if reviewed.CommitmentState == work.PlanApproved {
			itemState = work.ItemAccepted
		}
		if err := repository.SetPlanItemsCommitment(ctx, reviewed.ID, itemState, reviewed.ResolvedAt); err != nil {
			return err
		}
		approval, err := work.NewPlanApproval(approvalID, reviewed)
		if err != nil {
			return err
		}
		if err := repository.CreateApproval(ctx, approval); err != nil {
			return err
		}
		return s.recordActivity(ctx, repository, work.Activity{
			EntityKind: "plan", EntityID: reviewed.ID, ActorID: command.ReviewerActorID,
			EventType: "plan.reviewed", Summary: fmt.Sprintf("Plan revision %d marked %s", reviewed.Revision, reviewed.CommitmentState),
		})
	}); err != nil {
		return work.Plan{}, fmt.Errorf("review plan: %w", err)
	}
	return reviewed, nil
}

type ProposeOutputProfileCommand struct {
	ActorID      string
	Name         string
	Version      int
	Description  string
	Structure    json.RawMessage
	Semantics    json.RawMessage
	Validation   json.RawMessage
	SupersedesID string
}

func (s *Service) ProposeOutputProfile(ctx context.Context, command ProposeOutputProfileCommand) (output.Profile, error) {
	if command.Version > 1 && strings.TrimSpace(command.SupersedesID) == "" {
		return output.Profile{}, errors.New("later output profile versions require a predecessor")
	}
	if command.Version == 1 && strings.TrimSpace(command.SupersedesID) != "" {
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
		latestVersion, err := repository.LatestOutputProfileVersion(ctx, profile.Name)
		if err != nil {
			return err
		}
		if profile.Version != latestVersion+1 {
			return fmt.Errorf("output profile version must be %d", latestVersion+1)
		}
		if profile.SupersedesID != "" {
			predecessor, err := repository.OutputProfileByID(ctx, profile.SupersedesID)
			if err != nil {
				return fmt.Errorf("load superseded output profile: %w", err)
			}
			if predecessor.Name != profile.Name || predecessor.Version >= profile.Version || predecessor.LifecycleState != output.ProfileActive {
				return errors.New("output profile successor must reference an earlier active version with the same name")
			}
		}
		if err := repository.CreateOutputProfile(ctx, profile); err != nil {
			return err
		}
		return s.recordActivity(ctx, repository, work.Activity{
			EntityKind: "output_profile", EntityID: profile.ID, ActorID: command.ActorID,
			EventType: "output_profile.proposed", Summary: fmt.Sprintf("Output profile %s/v%d proposed", profile.Name, profile.Version),
		})
	}); err != nil {
		return output.Profile{}, fmt.Errorf("propose output profile: %w", err)
	}
	return profile, nil
}

type ReviewOutputProfileCommand struct {
	ProfileID       string
	ReviewerActorID string
	Decision        output.ProfileState
	Reason          string
}

func (s *Service) ReviewOutputProfile(ctx context.Context, command ReviewOutputProfileCommand) (output.Profile, error) {
	var reviewed output.Profile
	if err := s.store.WithinTransaction(ctx, func(repository ports.Repository) error {
		profile, err := repository.OutputProfileByID(ctx, command.ProfileID)
		if err != nil {
			return err
		}
		reviewed, err = output.ReviewProfile(profile, command.Decision, command.ReviewerActorID, command.Reason, s.clock.Now())
		if err != nil {
			return err
		}
		if reviewed.LifecycleState == output.ProfileActive && reviewed.SupersedesID != "" {
			if err := repository.SupersedeOutputProfile(ctx, reviewed.SupersedesID); err != nil {
				return err
			}
		}
		if err := repository.UpdateOutputProfile(ctx, reviewed); err != nil {
			return err
		}
		return s.recordActivity(ctx, repository, work.Activity{
			EntityKind: "output_profile", EntityID: reviewed.ID, ActorID: command.ReviewerActorID,
			EventType: "output_profile.reviewed", Summary: fmt.Sprintf("Output profile %s/v%d marked %s", reviewed.Name, reviewed.Version, reviewed.LifecycleState),
		})
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

func (s *Service) ListOutputProfiles(ctx context.Context) ([]output.Profile, error) {
	profiles, err := s.store.ListOutputProfiles(ctx)
	if err != nil {
		return nil, fmt.Errorf("list output profiles: %w", err)
	}
	return profiles, nil
}
