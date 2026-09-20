package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/dennisschroeder/throughline/internal/domain/authority"
	"github.com/dennisschroeder/throughline/internal/domain/output"
	"github.com/dennisschroeder/throughline/internal/domain/work"
	"github.com/dennisschroeder/throughline/internal/ports"
)

// RunInputBindingCommand is one value the harness supplies for one of the
// plan's declared inputs. Exactly one shape must be filled, and every binding
// is written once when the run is created.
type RunInputBindingCommand struct {
	Name             string
	Value            string
	OutputRevisionID string
	Locator          string
	SourceVersion    string
	Digest           string
}

type CreatePlanRunCommand struct {
	ObjectiveID    string
	PlanID         string
	ActorID        string
	IdempotencyKey string
	// RunKey is opaque and unique within the objective. Throughline reads no
	// time, cadence or schedule out of it.
	RunKey   string
	Bindings []RunInputBindingCommand
}

// RunConflictError reports a run key already used in this objective for a
// different plan revision or different bindings. It is deliberately not the
// same as ordinary idempotency: the key names the business execution instance
// across actors, and reusing it with changed content must not quietly
// reinterpret the run that already exists.
type RunConflictError struct {
	RunKey    string
	ExistingR string
	Reason    string
}

func (e RunConflictError) Error() string {
	return fmt.Sprintf("plan run key %q already names run %s with different content: %s", e.RunKey, e.ExistingR, e.Reason)
}

// RunCapacityError reports that the objective already has as many active runs
// as it permits.
type RunCapacityError struct {
	ObjectiveID string
	Active      int
	Limit       int
}

func (e RunCapacityError) Error() string {
	return fmt.Sprintf("objective already has %d active plan runs and permits %d", e.Active, e.Limit)
}

func (s *Service) CreatePlanRun(ctx context.Context, command CreatePlanRunCommand) (Mutation[ports.PlanRunContext], error) {
	ctx, capture := beginMutation(ctx)
	result, err := s.createPlanRunMutation(ctx, command)
	return finishMutation(capture, result, err)
}

// createPlanRunMutation writes the whole run or nothing. Everything it checks
// — the objective's phase, the exact approved revision, every required
// binding, external reference identity, objective-wide capacity and the
// complete materialization of every step — happens inside one transaction, so
// a rejected creation leaves neither a run nor a work item behind.
func (s *Service) createPlanRunMutation(ctx context.Context, command CreatePlanRunCommand) (ports.PlanRunContext, error) {
	// The run key is normalized before the request is hashed anywhere, not
	// after. Normalizing between the replay lookup and the executed write
	// would hash the same logical request two different ways, so the retry
	// carrying the very key that succeeded would come back as a reused
	// idempotency key with a different request.
	command.RunKey = strings.TrimSpace(command.RunKey)
	if replay, found, err := replayIdempotently[ports.PlanRunContext](ctx, s, command.ActorID, command.IdempotencyKey, "create_plan_run", command); err != nil {
		return ports.PlanRunContext{}, err
	} else if found {
		return replay, nil
	}
	if strings.TrimSpace(command.ActorID) == "" {
		return ports.PlanRunContext{}, errors.New("creating a plan run requires an actor")
	}
	if command.RunKey == "" {
		return ports.PlanRunContext{}, errors.New("creating a plan run requires a run key")
	}
	if strings.TrimSpace(command.PlanID) == "" {
		return ports.PlanRunContext{}, errors.New("creating a plan run requires one exact approved plan revision")
	}
	now := s.clock.Now()
	var result ports.PlanRunContext
	if err := s.store.WithinTransaction(ctx, func(repository ports.Repository) error {
		created, err := executeIdempotently(ctx, s, repository, command.ActorID, command.IdempotencyKey, "create_plan_run", command, func() (ports.PlanRunContext, error) {
			return s.materializePlanRun(ctx, repository, command, now)
		})
		result = created
		return err
	}); err != nil {
		return ports.PlanRunContext{}, fmt.Errorf("create plan run: %w", err)
	}
	return result, nil
}

func (s *Service) materializePlanRun(ctx context.Context, repository ports.Repository, command CreatePlanRunCommand, now time.Time) (ports.PlanRunContext, error) {
	objective, err := repository.Objective(ctx, command.ObjectiveID)
	if err != nil {
		return ports.PlanRunContext{}, fmt.Errorf("load objective: %w", err)
	}
	plan, err := repository.Plan(ctx, command.PlanID)
	if err != nil {
		return ports.PlanRunContext{}, fmt.Errorf("load plan revision: %w", err)
	}
	if plan.ObjectiveID != objective.ID {
		return ports.PlanRunContext{}, fmt.Errorf("plan revision %d belongs to another objective", plan.Revision)
	}
	if plan.CommitmentState != work.PlanApproved {
		return ports.PlanRunContext{}, fmt.Errorf("plan revision %d is %s; only an approved revision can be run", plan.Revision, plan.CommitmentState)
	}
	inputs, err := repository.PlanInputs(ctx, plan.ID)
	if err != nil {
		return ports.PlanRunContext{}, err
	}
	// The run's identity is generated before its bindings so each binding is
	// built once, already pointing at the run it belongs to. A replayed run
	// key discards this identifier unused, which costs nothing.
	runID, err := s.ids.New()
	if err != nil {
		return ports.PlanRunContext{}, fmt.Errorf("generate plan run id: %w", err)
	}
	bindings, err := s.buildRunInputBindings(command, inputs, runID, now)
	if err != nil {
		return ports.PlanRunContext{}, err
	}
	fingerprint := work.RunCreationFingerprint(plan.ID, bindings)

	// The run key is resolved across the whole objective and independently of
	// the actor, and before capacity: a retry of a creation that already
	// succeeded must return that run rather than be rejected for capacity the
	// run itself is occupying.
	existing, err := repository.PlanRunByKey(ctx, objective.ID, command.RunKey)
	if err == nil {
		if existing.PlanID != plan.ID {
			return ports.PlanRunContext{}, RunConflictError{RunKey: command.RunKey, ExistingR: existing.ID, Reason: "it was created from a different plan revision"}
		}
		if existing.BindingFingerprint != fingerprint {
			return ports.PlanRunContext{}, RunConflictError{RunKey: command.RunKey, ExistingR: existing.ID, Reason: "it was created with different input bindings"}
		}
		return s.planRunContext(ctx, repository, existing)
	}
	if !errors.Is(err, ports.ErrNotFound) {
		return ports.PlanRunContext{}, err
	}

	if objective.Phase != work.ObjectiveExecution {
		return ports.PlanRunContext{}, fmt.Errorf("creating a plan run requires an objective in execution phase, not %s", objective.Phase)
	}
	active, err := repository.ActivePlanRunCount(ctx, objective.ID)
	if err != nil {
		return ports.PlanRunContext{}, err
	}
	if active >= objective.MaxConcurrentRuns {
		return ports.PlanRunContext{}, RunCapacityError{ObjectiveID: objective.ID, Active: active, Limit: objective.MaxConcurrentRuns}
	}

	for _, binding := range bindings {
		if err := s.validateBindingIdentity(ctx, repository, binding); err != nil {
			return ports.PlanRunContext{}, err
		}
	}

	steps, err := repository.PlanSteps(ctx, plan.ID)
	if err != nil {
		return ports.PlanRunContext{}, err
	}
	if len(steps) == 0 {
		return ports.PlanRunContext{}, fmt.Errorf("plan revision %d declares no plan steps and cannot be run", plan.Revision)
	}
	sequence, err := repository.NextPlanRunSequence(ctx, objective.ID)
	if err != nil {
		return ports.PlanRunContext{}, err
	}
	run, err := work.NewPlanRun(work.PlanRun{
		ID: runID, ObjectiveID: objective.ID, PlanID: plan.ID, RunKey: command.RunKey,
		Sequence: sequence, BindingFingerprint: fingerprint, StartedBy: command.ActorID,
	}, now)
	if err != nil {
		return ports.PlanRunContext{}, err
	}
	if err := repository.CreatePlanRun(ctx, run); err != nil {
		return ports.PlanRunContext{}, err
	}
	for _, binding := range bindings {
		if err := repository.CreateRunInputBinding(ctx, binding); err != nil {
			return ports.PlanRunContext{}, err
		}
	}
	items, err := s.materializeSteps(ctx, repository, run, steps, command.ActorID, now)
	if err != nil {
		return ports.PlanRunContext{}, err
	}
	if err := s.materializeStepDependencies(ctx, repository, plan.ID, items, command.ActorID, now); err != nil {
		return ports.PlanRunContext{}, err
	}
	payload, err := json.Marshal(map[string]any{"run_key": run.RunKey, "plan_revision": plan.Revision, "work_items": len(items)})
	if err != nil {
		return ports.PlanRunContext{}, err
	}
	if err := s.recordActivity(ctx, repository, work.Activity{
		EntityKind: "plan_run", EntityID: run.ID, ObjectiveID: objective.ID, ActorID: command.ActorID,
		EventType: "plan_run.created", PayloadJSON: payload,
		Summary: fmt.Sprintf("Plan run %d of revision %d created with %d work items", run.Sequence, plan.Revision, len(items)),
	}); err != nil {
		return ports.PlanRunContext{}, err
	}
	materialized := make([]work.WorkItem, 0, len(items))
	for _, step := range steps {
		materialized = append(materialized, items[step.ID])
	}
	return ports.PlanRunContext{Run: run, Plan: plan, Bindings: bindings, WorkItems: materialized}, nil
}

// buildRunInputBindings matches the supplied bindings against what the plan
// declares. Every required input must be bound at creation; an optional one
// left out simply stays unbound, and nothing can be added afterwards.
func (s *Service) buildRunInputBindings(command CreatePlanRunCommand, inputs []work.PlanInput, runID string, now time.Time) ([]work.RunInputBinding, error) {
	byName := make(map[string]work.PlanInput, len(inputs))
	for _, input := range inputs {
		byName[input.Name] = input
	}
	bound := make(map[string]bool, len(command.Bindings))
	bindings := make([]work.RunInputBinding, 0, len(command.Bindings))
	for _, supplied := range command.Bindings {
		name := strings.TrimSpace(supplied.Name)
		input, declared := byName[name]
		if !declared {
			return nil, fmt.Errorf("plan run binds %q, which the plan revision does not declare as an input", name)
		}
		if bound[name] {
			return nil, fmt.Errorf("plan input %q is bound twice", name)
		}
		bound[name] = true
		id, err := s.ids.New()
		if err != nil {
			return nil, fmt.Errorf("generate run input binding id: %w", err)
		}
		kind, err := bindingKind(supplied)
		if err != nil {
			return nil, err
		}
		binding, err := work.NewRunInputBinding(work.RunInputBinding{
			// Name is taken from the declared input rather than from the
			// request, so a binding can never name something the plan does
			// not declare.
			ID: id, PlanRunID: runID, PlanInputID: input.ID, Name: input.Name,
			Kind: kind, Value: supplied.Value, OutputRevisionID: supplied.OutputRevisionID,
			Locator: supplied.Locator, SourceVersion: supplied.SourceVersion, Digest: supplied.Digest,
			CreatedBy: command.ActorID,
		}, now)
		if err != nil {
			return nil, err
		}
		bindings = append(bindings, binding)
	}
	for _, input := range inputs {
		if input.Required && !bound[input.Name] {
			return nil, fmt.Errorf("plan run is missing the required input %q", input.Name)
		}
	}
	return bindings, nil
}

// bindingKind reads the shape off the command and refuses an ambiguous one.
// Picking a winner by precedence would silently discard the rest of what the
// caller sent, and a run key replayed with only the discarded part changed
// would then resolve to the existing run as if nothing had changed.
func bindingKind(supplied RunInputBindingCommand) (work.RunInputBindingKind, error) {
	value := strings.TrimSpace(supplied.Value) != ""
	revision := strings.TrimSpace(supplied.OutputRevisionID) != ""
	external := strings.TrimSpace(supplied.Locator) != "" ||
		strings.TrimSpace(supplied.SourceVersion) != "" ||
		strings.TrimSpace(supplied.Digest) != ""
	filled := 0
	for _, shape := range []bool{value, revision, external} {
		if shape {
			filled++
		}
	}
	if filled != 1 {
		return "", fmt.Errorf("binding for plan input %q must fill exactly one shape — a value, one exact accepted output revision, or an external reference — but fills %d", strings.TrimSpace(supplied.Name), filled)
	}
	switch {
	case revision:
		return work.BindingOutputRevision, nil
	case external:
		return work.BindingExternal, nil
	default:
		return work.BindingValue, nil
	}
}

// validateBindingIdentity checks that a binding names something that exists
// and is usable. An external reference is validated for shape only:
// Throughline never fetches the source and never computes its digest, so the
// caller supplies the identity and is trusted for its content.
func (s *Service) validateBindingIdentity(ctx context.Context, repository ports.Repository, binding work.RunInputBinding) error {
	if binding.Kind != work.BindingOutputRevision {
		return nil
	}
	revision, err := repository.OutputRevision(ctx, binding.OutputRevisionID)
	if err != nil {
		return fmt.Errorf("plan input %q: load bound output revision: %w", binding.Name, err)
	}
	if revision.AcceptanceState != output.RevisionAccepted {
		return fmt.Errorf("plan input %q binds output revision %s, which is %s; only an accepted revision can be bound", binding.Name, revision.ID, revision.AcceptanceState)
	}
	return nil
}

// materializeSteps writes one fresh work item per step, with its own copy of
// the step's criteria, expected outputs, output requirements, capabilities and
// external action proposals. Nothing is shared with another run: identities
// are new, so a later run leaves every earlier one untouched.
func (s *Service) materializeSteps(ctx context.Context, repository ports.Repository, run work.PlanRun, steps []work.PlanStep, actorID string, now time.Time) (map[string]work.WorkItem, error) {
	items := make(map[string]work.WorkItem, len(steps))
	pending := append([]work.PlanStep(nil), steps...)
	for len(pending) > 0 {
		remaining := make([]work.PlanStep, 0, len(pending))
		for _, step := range pending {
			if step.ParentStepID != "" {
				if _, ready := items[step.ParentStepID]; !ready {
					remaining = append(remaining, step)
					continue
				}
			}
			item, err := s.materializeStep(ctx, repository, run, step, items, actorID, now)
			if err != nil {
				return nil, err
			}
			items[step.ID] = item
		}
		if len(remaining) == len(pending) {
			return nil, errors.New("could not order recursive plan steps")
		}
		pending = remaining
	}
	return items, nil
}

func (s *Service) materializeStep(ctx context.Context, repository ports.Repository, run work.PlanRun, step work.PlanStep, materialized map[string]work.WorkItem, actorID string, now time.Time) (work.WorkItem, error) {
	itemID, err := s.ids.New()
	if err != nil {
		return work.WorkItem{}, fmt.Errorf("generate work item id: %w", err)
	}
	parentID := ""
	if step.ParentStepID != "" {
		parentID = materialized[step.ParentStepID].ID
	}
	item, err := work.NewWorkItem(work.WorkItem{
		ID:                 itemID,
		Key:                work.MaterializedWorkItemKey(step.Key, run.Sequence),
		ObjectiveID:        run.ObjectiveID,
		PlanID:             run.PlanID,
		ParentID:           parentID,
		Title:              step.Title,
		Description:        step.Description,
		Kind:               step.Kind,
		CommitmentState:    work.ItemAccepted,
		ExecutionStatus:    work.StatusBacklog,
		Priority:           step.Priority,
		EstimatedScope:     step.EstimatedScope,
		ExecutionPolicy:    step.ExecutionPolicy,
		RequiredActorKind:  step.RequiredActorKind,
		AttentionState:     work.AttentionNone,
		ReviewRequirements: step.ReviewRequirements,
		Origin:             work.OriginPlanStep,
		PlanRunID:          run.ID,
		OriginPlanStepID:   step.ID,
	}, now)
	if err != nil {
		return work.WorkItem{}, err
	}
	if err := repository.CreateWorkItem(ctx, item); err != nil {
		return work.WorkItem{}, fmt.Errorf("materialize plan step %q as work item %q: %w", step.Key, item.Key, err)
	}
	for _, capability := range step.Definition.RequiredCapabilities {
		if err := repository.AddWorkItemCapability(ctx, item.ID, capability); err != nil {
			return work.WorkItem{}, err
		}
	}
	for _, criterion := range step.Definition.AcceptanceCriteria {
		id, err := s.ids.New()
		if err != nil {
			return work.WorkItem{}, fmt.Errorf("generate acceptance criterion id: %w", err)
		}
		created, err := work.NewAcceptanceCriterion(work.AcceptanceCriterion{
			ID: id, WorkItemID: item.ID, Text: criterion.Text, Required: criterion.Required, Ordinal: criterion.Ordinal,
		})
		if err != nil {
			return work.WorkItem{}, err
		}
		if err := repository.CreateAcceptanceCriterion(ctx, created); err != nil {
			return work.WorkItem{}, err
		}
	}
	for _, expected := range step.Definition.ExpectedOutputs {
		profile, err := repository.OutputProfile(ctx, expected.ProfileName, expected.ProfileVersion)
		if err != nil {
			return work.WorkItem{}, fmt.Errorf("plan step %q expected output %q: %w", step.Key, expected.Name, err)
		}
		id, err := s.ids.New()
		if err != nil {
			return work.WorkItem{}, fmt.Errorf("generate expected output id: %w", err)
		}
		created, err := output.NewExpectedOutput(id, item.ID, expected.Name, profile, json.RawMessage(expected.Contract), expected.DestinationHint, expected.Required, expected.Ordinal)
		if err != nil {
			return work.WorkItem{}, err
		}
		if err := repository.CreateExpectedOutput(ctx, created); err != nil {
			return work.WorkItem{}, err
		}
	}
	for _, requirement := range step.Definition.OutputRequirements {
		revision, err := repository.OutputRevision(ctx, requirement.RequiredOutputRevisionID)
		if err != nil {
			return work.WorkItem{}, fmt.Errorf("plan step %q output requirement: %w", step.Key, err)
		}
		id, err := s.ids.New()
		if err != nil {
			return work.WorkItem{}, fmt.Errorf("generate output requirement id: %w", err)
		}
		created, err := output.NewExactOutputRequirement(id, item.ID, revision, requirement.Required, requirement.Note)
		if err != nil {
			return work.WorkItem{}, err
		}
		if err := repository.CreateOutputRequirement(ctx, created); err != nil {
			return work.WorkItem{}, err
		}
	}
	for _, proposed := range step.Definition.ExternalActions {
		id, err := s.ids.New()
		if err != nil {
			return work.WorkItem{}, fmt.Errorf("generate external action id: %w", err)
		}
		// The authorization subject is copied byte for byte from the frozen
		// definition. Throughline substitutes nothing into it, so what the run
		// asks to be authorized is exactly what the plan recorded.
		action, revision, err := authority.NewExternalAction(authority.ExternalAction{
			ID: id, WorkItemID: item.ID, Required: proposed.Required, Title: proposed.Title, Rationale: proposed.Rationale,
		}, []byte(proposed.AuthorizationSubject), actorID, now)
		if err != nil {
			return work.WorkItem{}, fmt.Errorf("plan step %q external action %q: %w", step.Key, proposed.Title, err)
		}
		if err := repository.CreateExternalAction(ctx, action); err != nil {
			return work.WorkItem{}, err
		}
		if err := repository.CreateExternalActionRevision(ctx, revision); err != nil {
			return work.WorkItem{}, err
		}
	}
	if err := s.recordActivity(ctx, repository, work.Activity{
		EntityKind: "work_item", EntityID: item.ID, WorkItemID: item.ID, ActorID: actorID,
		EventType: "work_item.materialized",
		Summary:   fmt.Sprintf("Work item %s materialized from plan step %s for run %d", item.Key, step.Key, run.Sequence),
	}); err != nil {
		return work.WorkItem{}, err
	}
	return item, nil
}

// materializeStepDependencies copies the plan's step prerequisites onto this
// run's own work items, so a run's ordering never reaches into another run.
func (s *Service) materializeStepDependencies(ctx context.Context, repository ports.Repository, planID string, items map[string]work.WorkItem, actorID string, now time.Time) error {
	dependencies, err := repository.PlanStepDependencies(ctx, planID)
	if err != nil {
		return err
	}
	for _, dependency := range dependencies {
		item, ok := items[dependency.PlanStepID]
		prerequisite, prerequisiteOK := items[dependency.DependsOnStepID]
		if !ok || !prerequisiteOK {
			return errors.New("plan step dependency refers to a step this run did not materialize")
		}
		id, err := s.ids.New()
		if err != nil {
			return fmt.Errorf("generate dependency id: %w", err)
		}
		created, err := work.NewDependency(work.Dependency{
			ID: id, WorkItemID: item.ID, DependsOnItemID: prerequisite.ID, Kind: work.DependencyHard, CreatedBy: actorID,
		}, now)
		if err != nil {
			return err
		}
		if err := repository.CreateDependency(ctx, created); err != nil {
			return err
		}
	}
	return nil
}

type ClosePlanRunCommand struct {
	PlanRunID       string
	ActorID         string
	IdempotencyKey  string
	ExpectedVersion int
	TargetStatus    work.PlanRunStatus
	Reason          string
}

func (s *Service) ClosePlanRun(ctx context.Context, command ClosePlanRunCommand) (Mutation[ports.PlanRunContext], error) {
	ctx, capture := beginMutation(ctx)
	result, err := s.closePlanRunMutation(ctx, command)
	return finishMutation(capture, result, err)
}

// closePlanRunMutation is the one explicit ending. Throughline never decides
// that a run is over; it only validates the ending a caller asks for, and then
// leaves no apparently-executable remainder behind: every work item that is
// not already terminal is cancelled with the run's rationale and every open
// claim on the run's work is released, all in this transaction.
func (s *Service) closePlanRunMutation(ctx context.Context, command ClosePlanRunCommand) (ports.PlanRunContext, error) {
	if replay, found, err := replayIdempotently[ports.PlanRunContext](ctx, s, command.ActorID, command.IdempotencyKey, "close_plan_run", command); err != nil {
		return ports.PlanRunContext{}, err
	} else if found {
		return replay, nil
	}
	now := s.clock.Now()
	var result ports.PlanRunContext
	if err := s.store.WithinTransaction(ctx, func(repository ports.Repository) error {
		closed, err := executeIdempotently(ctx, s, repository, command.ActorID, command.IdempotencyKey, "close_plan_run", command, func() (ports.PlanRunContext, error) {
			run, err := repository.PlanRun(ctx, command.PlanRunID)
			if err != nil {
				return ports.PlanRunContext{}, err
			}
			if run.Version != command.ExpectedVersion {
				return ports.PlanRunContext{}, ports.ErrVersionConflict
			}
			// Cancelling is an administrative closure: it releases capacity
			// and open leases, and stays available whatever phase the
			// objective is in. Declaring success or failure is a judgement
			// about work, and that needs the objective to be in execution.
			if command.TargetStatus != work.PlanRunCancelled {
				objective, err := repository.Objective(ctx, run.ObjectiveID)
				if err != nil {
					return ports.PlanRunContext{}, err
				}
				if objective.Phase != work.ObjectiveExecution {
					return ports.PlanRunContext{}, fmt.Errorf("closing a plan run as %s requires an objective in execution phase, not %s", command.TargetStatus, objective.Phase)
				}
			}
			facts := work.RunClosureFacts{}
			if command.TargetStatus == work.PlanRunSucceeded {
				facts, err = repository.PlanRunClosureFacts(ctx, run.ID)
				if err != nil {
					return ports.PlanRunContext{}, err
				}
			}
			ended, err := work.ClosePlanRun(run, command.TargetStatus, command.ActorID, command.Reason, facts, now)
			if err != nil {
				return ports.PlanRunContext{}, err
			}
			if err := repository.UpdatePlanRun(ctx, ended, command.ExpectedVersion); err != nil {
				return ports.PlanRunContext{}, err
			}
			if err := s.settleRunWork(ctx, repository, ended, command, now); err != nil {
				return ports.PlanRunContext{}, err
			}
			payload, err := json.Marshal(map[string]string{"status": string(ended.Status), "reason": strings.TrimSpace(command.Reason)})
			if err != nil {
				return ports.PlanRunContext{}, err
			}
			if err := s.recordActivity(ctx, repository, work.Activity{
				EntityKind: "plan_run", EntityID: ended.ID, ObjectiveID: ended.ObjectiveID, ActorID: command.ActorID,
				EventType: "plan_run.closed", PayloadJSON: payload,
				Summary: fmt.Sprintf("Plan run %d closed as %s", ended.Sequence, ended.Status),
			}); err != nil {
				return ports.PlanRunContext{}, err
			}
			return s.planRunContext(ctx, repository, ended)
		})
		result = closed
		return err
	}); err != nil {
		return ports.PlanRunContext{}, fmt.Errorf("close plan run: %w", err)
	}
	return result, nil
}

// settleRunWork cancels whatever the run leaves unfinished and ends every open
// claim on its work. A successful run has nothing left to cancel — its gate
// already required every item to be terminal — so this only ever acts on a
// failed or cancelled run's remainder.
func (s *Service) settleRunWork(ctx context.Context, repository ports.Repository, run work.PlanRun, command ClosePlanRunCommand, now time.Time) error {
	reason := strings.TrimSpace(command.Reason)
	if reason == "" {
		reason = fmt.Sprintf("plan run closed as %s", run.Status)
	}
	items, err := repository.PlanRunWorkItems(ctx, run.ID)
	if err != nil {
		return err
	}
	for _, item := range items {
		// An expired lease already holds nothing, but its row is still open.
		// Sweeping it through the ordinary expiry path first means the release
		// below only ever sees live claims, which is the only kind their owner
		// may release — otherwise one lapsed lease would make the run
		// impossible to close at all.
		if _, err := repository.ExpireClaims(ctx, item.ID, now); err != nil {
			return err
		}
		if item.ExecutionStatus == work.StatusDone || item.ExecutionStatus == work.StatusCancelled {
			continue
		}
		cancelled, err := work.TransitionWorkItem(item, work.StatusCancelled, command.ActorID, reason, now)
		if err != nil {
			return err
		}
		if err := repository.UpdateWorkItem(ctx, cancelled, item.Version); err != nil {
			return err
		}
		payload, err := json.Marshal(map[string]string{"from": string(item.ExecutionStatus), "to": string(work.StatusCancelled), "reason": reason})
		if err != nil {
			return err
		}
		if err := s.recordActivity(ctx, repository, work.Activity{
			EntityKind: "work_item", EntityID: item.ID, WorkItemID: item.ID, ActorID: command.ActorID,
			EventType: "work_item.status_changed", PayloadJSON: payload,
			Summary: fmt.Sprintf("Work item cancelled because plan run %d closed as %s", run.Sequence, run.Status),
		}); err != nil {
			return err
		}
	}
	claims, err := repository.OpenClaimsForRun(ctx, run.ID)
	if err != nil {
		return err
	}
	for _, claim := range claims {
		released, err := work.ReleaseClaim(claim, claim.ActorID, reason, now)
		if err != nil {
			return err
		}
		if err := repository.ReleaseClaim(ctx, released); err != nil {
			return err
		}
	}
	return nil
}

func (s *Service) GetPlanRun(ctx context.Context, id string) (ports.PlanRunContext, error) {
	var result ports.PlanRunContext
	err := s.store.WithinTransaction(ctx, func(repository ports.Repository) error {
		run, err := repository.PlanRun(ctx, id)
		if err != nil {
			return err
		}
		result, err = s.planRunContext(ctx, repository, run)
		return err
	})
	return result, err
}

func (s *Service) planRunContext(ctx context.Context, repository ports.Repository, run work.PlanRun) (ports.PlanRunContext, error) {
	plan, err := repository.Plan(ctx, run.PlanID)
	if err != nil {
		return ports.PlanRunContext{}, err
	}
	bindings, err := repository.RunInputBindings(ctx, run.ID)
	if err != nil {
		return ports.PlanRunContext{}, err
	}
	items, err := repository.PlanRunWorkItems(ctx, run.ID)
	if err != nil {
		return ports.PlanRunContext{}, err
	}
	return ports.PlanRunContext{Run: run, Plan: plan, Bindings: bindings, WorkItems: items}, nil
}
