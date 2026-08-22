package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/dennisschroeder/workgraph/internal/domain/output"
	"github.com/dennisschroeder/workgraph/internal/domain/work"
	"github.com/dennisschroeder/workgraph/internal/ports"
)

type TransitionWorkItemCommand struct {
	WorkItemID      string
	TargetStatus    work.ExecutionStatus
	ActorID         string
	Reason          string
	ExpectedVersion int
	IdempotencyKey  string
}

func (s *Service) TransitionWorkItem(ctx context.Context, command TransitionWorkItemCommand) (work.WorkItem, error) {
	var transitioned work.WorkItem
	err := s.store.WithinTransaction(ctx, func(repository ports.Repository) error {
		var err error
		transitioned, err = executeIdempotently(ctx, s, repository, command.ActorID, command.IdempotencyKey, "transition_work_item", command, func() (work.WorkItem, error) {
			item, err := repository.WorkItem(ctx, command.WorkItemID)
			if err != nil {
				return work.WorkItem{}, err
			}
			if item.Version != command.ExpectedVersion {
				return work.WorkItem{}, ports.ErrVersionConflict
			}
			objective, err := repository.Objective(ctx, item.ObjectiveID)
			if err != nil {
				return work.WorkItem{}, err
			}
			requirements, err := transitionRequirements(ctx, repository, objective, item, command.TargetStatus)
			if err != nil {
				return work.WorkItem{}, err
			}
			if len(requirements) != 0 {
				return work.WorkItem{}, TransitionGateError{Requirements: requirements}
			}
			if command.TargetStatus != work.StatusReady && command.TargetStatus != work.StatusCancelled {
				actor, err := repository.Actor(ctx, command.ActorID)
				if err != nil {
					return work.WorkItem{}, err
				}
				activeClaim, err := repository.ActiveClaim(ctx, item.ID, s.clock.Now())
				if err != nil {
					return work.WorkItem{}, err
				}
				if activeClaim != nil && activeClaim.ActorID != command.ActorID {
					return work.WorkItem{}, errors.New("only the active claim owner can advance work")
				}
				policyRequirements, err := claimRequirements(ctx, repository, item, actor, work.StatusReady, nil, s.clock.Now())
				if err != nil {
					return work.WorkItem{}, err
				}
				if len(policyRequirements) != 0 {
					return work.WorkItem{}, ClaimGateError{Requirements: policyRequirements}
				}
			}
			result, err := work.TransitionWorkItem(item, command.TargetStatus, command.ActorID, command.Reason, s.clock.Now())
			if err != nil {
				return work.WorkItem{}, err
			}
			if err := repository.UpdateWorkItem(ctx, result, command.ExpectedVersion); err != nil {
				return work.WorkItem{}, err
			}
			payload, err := json.Marshal(map[string]string{"from": string(item.ExecutionStatus), "to": string(result.ExecutionStatus), "reason": strings.TrimSpace(command.Reason)})
			if err != nil {
				return work.WorkItem{}, err
			}
			if err := s.recordActivity(ctx, repository, work.Activity{EntityKind: "work_item", EntityID: item.ID, WorkItemID: item.ID, ActorID: command.ActorID, EventType: "work_item.status_changed", Summary: fmt.Sprintf("Work item moved from %s to %s", item.ExecutionStatus, result.ExecutionStatus), PayloadJSON: payload}); err != nil {
				return work.WorkItem{}, err
			}
			return result, nil
		})
		return err
	})
	if err != nil {
		return work.WorkItem{}, fmt.Errorf("transition work item: %w", err)
	}
	return transitioned, nil
}

type TransitionGateError struct {
	Requirements []work.TransitionRequirement
}

func (e TransitionGateError) Error() string { return "work item transition is blocked" }

func transitionRequirements(ctx context.Context, repository ports.Repository, objective work.Objective, item work.WorkItem, target work.ExecutionStatus) ([]work.TransitionRequirement, error) {
	planApproved, err := repository.PlanApprovedForWorkItem(ctx, item.ID)
	if err != nil {
		return nil, err
	}
	criteriaSatisfied, err := repository.AcceptanceCriteriaSatisfied(ctx, item.ID)
	if err != nil {
		return nil, err
	}
	dependenciesSatisfied, err := repository.HardDependenciesSatisfied(ctx, item.ID)
	if err != nil {
		return nil, err
	}
	expectedOutputsSatisfied, err := repository.ExpectedOutputsSatisfied(ctx, item.ID)
	if err != nil {
		return nil, err
	}
	outputRequirementsSatisfied, err := repository.OutputRequirementsSatisfied(ctx, item.ID)
	if err != nil {
		return nil, err
	}
	externalActionsSatisfied, err := repository.RequiredExternalActionsSatisfied(ctx, item.ID)
	if err != nil {
		return nil, err
	}
	return work.EvaluateTransitionGate(work.TransitionGateFacts{
		ObjectivePhase: objective.Phase, PlanApproved: planApproved, ItemCommitment: item.CommitmentState,
		CurrentStatus: item.ExecutionStatus, TargetStatus: target,
		AcceptanceCriteriaSatisfied: criteriaSatisfied, HardDependenciesSatisfied: dependenciesSatisfied,
		ExpectedOutputsSatisfied: expectedOutputsSatisfied, OutputRequirementsSatisfied: outputRequirementsSatisfied,
		ExternalActionsSatisfied:    externalActionsSatisfied,
		ReviewRequirementsSatisfied: true,
	}), nil
}

type ResolveAcceptanceCriterionCommand struct {
	CriterionID             string
	Status                  work.AcceptanceCriterionStatus
	ActorID                 string
	Rationale               string
	ExpectedWorkItemVersion int
	IdempotencyKey          string
}

func (s *Service) ResolveAcceptanceCriterion(ctx context.Context, command ResolveAcceptanceCriterionCommand) (work.AcceptanceCriterion, error) {
	var resolved work.AcceptanceCriterion
	err := s.store.WithinTransaction(ctx, func(repository ports.Repository) error {
		var err error
		resolved, err = executeIdempotently(ctx, s, repository, command.ActorID, command.IdempotencyKey, "resolve_acceptance_criterion", command, func() (work.AcceptanceCriterion, error) {
			criterion, err := repository.AcceptanceCriterion(ctx, command.CriterionID)
			if err != nil {
				return work.AcceptanceCriterion{}, err
			}
			item, err := repository.WorkItem(ctx, criterion.WorkItemID)
			if err != nil {
				return work.AcceptanceCriterion{}, err
			}
			if item.Version != command.ExpectedWorkItemVersion {
				return work.AcceptanceCriterion{}, ports.ErrVersionConflict
			}
			result, err := work.ResolveAcceptanceCriterion(criterion, command.Status, command.ActorID, command.Rationale, s.clock.Now())
			if err != nil {
				return work.AcceptanceCriterion{}, err
			}
			if err := repository.UpdateAcceptanceCriterion(ctx, result); err != nil {
				return work.AcceptanceCriterion{}, err
			}
			item.Version++
			item.UpdatedAt = s.clock.Now().UTC()
			if err := repository.UpdateWorkItem(ctx, item, command.ExpectedWorkItemVersion); err != nil {
				return work.AcceptanceCriterion{}, err
			}
			if err := s.recordActivity(ctx, repository, work.Activity{EntityKind: "acceptance_criterion", EntityID: result.ID, WorkItemID: result.WorkItemID, ActorID: command.ActorID, EventType: "acceptance_criterion.resolved", Summary: fmt.Sprintf("Acceptance criterion marked %s", result.Status)}); err != nil {
				return work.AcceptanceCriterion{}, err
			}
			return result, nil
		})
		return err
	})
	if err != nil {
		return work.AcceptanceCriterion{}, fmt.Errorf("resolve acceptance criterion: %w", err)
	}
	return resolved, nil
}

type LinkDependencyCommand struct {
	WorkItemID          string
	DependsOnWorkItemID string
	Kind                work.DependencyKind
	Note                string
	ActorID             string
	ExpectedVersion     int
	IdempotencyKey      string
}

func (s *Service) LinkDependency(ctx context.Context, command LinkDependencyCommand) (work.Dependency, error) {
	id, err := s.ids.New()
	if err != nil {
		return work.Dependency{}, fmt.Errorf("generate dependency id: %w", err)
	}
	dependency, err := work.NewDependency(work.Dependency{
		ID: id, WorkItemID: command.WorkItemID, DependsOnItemID: command.DependsOnWorkItemID,
		Kind: command.Kind, Note: command.Note, CreatedBy: command.ActorID,
	}, s.clock.Now())
	if err != nil {
		return work.Dependency{}, err
	}
	err = s.store.WithinTransaction(ctx, func(repository ports.Repository) error {
		var executeErr error
		dependency, executeErr = executeIdempotently(ctx, s, repository, command.ActorID, command.IdempotencyKey, "link_dependency", command, func() (work.Dependency, error) {
			item, err := repository.WorkItem(ctx, dependency.WorkItemID)
			if err != nil {
				return work.Dependency{}, err
			}
			if item.Version != command.ExpectedVersion {
				return work.Dependency{}, ports.ErrVersionConflict
			}
			prerequisite, err := repository.WorkItem(ctx, dependency.DependsOnItemID)
			if err != nil {
				return work.Dependency{}, err
			}
			if item.ObjectiveID != prerequisite.ObjectiveID {
				return work.Dependency{}, errors.New("dependencies must stay within one objective")
			}
			if dependency.Kind == work.DependencyHard {
				cycle, err := repository.DependencyCreatesCycle(ctx, dependency.WorkItemID, dependency.DependsOnItemID)
				if err != nil {
					return work.Dependency{}, err
				}
				if cycle {
					return work.Dependency{}, errors.New("dependency_cycle")
				}
			}
			if err := repository.CreateDependency(ctx, dependency); err != nil {
				return work.Dependency{}, err
			}
			item.Version++
			item.UpdatedAt = s.clock.Now().UTC()
			if err := repository.UpdateWorkItem(ctx, item, command.ExpectedVersion); err != nil {
				return work.Dependency{}, err
			}
			if err := s.recordActivity(ctx, repository, work.Activity{EntityKind: "dependency", EntityID: dependency.ID, WorkItemID: item.ID, ActorID: command.ActorID, EventType: "dependency.linked", Summary: fmt.Sprintf("%s dependency linked", dependency.Kind)}); err != nil {
				return work.Dependency{}, err
			}
			return dependency, nil
		})
		return executeErr
	})
	if err != nil {
		return work.Dependency{}, fmt.Errorf("link dependency: %w", err)
	}
	return dependency, nil
}

type OutputArtifactInput struct {
	Kind     string
	URI      string
	Title    string
	Metadata json.RawMessage
	Role     string
}

type CreateOutputRevisionCommand struct {
	ExpectedOutputID string
	ActorID          string
	ContentDigest    string
	Artifacts        []OutputArtifactInput
}

func (s *Service) CreateOutputRevision(ctx context.Context, command CreateOutputRevisionCommand) (output.OutputRevision, error) {
	revisionID, err := s.ids.New()
	if err != nil {
		return output.OutputRevision{}, fmt.Errorf("generate output revision id: %w", err)
	}
	artifactIDs := make([]string, len(command.Artifacts))
	for index := range command.Artifacts {
		artifactIDs[index], err = s.ids.New()
		if err != nil {
			return output.OutputRevision{}, fmt.Errorf("generate artifact id: %w", err)
		}
	}
	var revision output.OutputRevision
	err = s.store.WithinTransaction(ctx, func(repository ports.Repository) error {
		expected, err := repository.ExpectedOutput(ctx, command.ExpectedOutputID)
		if err != nil {
			return err
		}
		profile, err := repository.OutputProfileByID(ctx, expected.OutputProfileID)
		if err != nil {
			return err
		}
		revisionNumber, err := repository.NextOutputRevision(ctx, expected.ID)
		if err != nil {
			return err
		}
		bindings := make([]output.RevisionArtifact, 0, len(command.Artifacts))
		for index, input := range command.Artifacts {
			artifact, err := output.NewArtifact(output.Artifact{
				ID: artifactIDs[index], WorkItemID: expected.WorkItemID, Kind: input.Kind, URI: input.URI,
				Title: input.Title, Metadata: input.Metadata, AttachedBy: command.ActorID,
			}, s.clock.Now())
			if err != nil {
				return err
			}
			existing, lookupErr := repository.ArtifactByURI(ctx, expected.WorkItemID, artifact.URI)
			if lookupErr == nil {
				if existing.Kind != artifact.Kind {
					return errors.New("an artifact URI cannot be reused with a different kind")
				}
				artifact = existing
			} else if errors.Is(lookupErr, ports.ErrNotFound) {
				if err := repository.CreateArtifact(ctx, artifact); err != nil {
					return err
				}
			} else {
				return lookupErr
			}
			bindings = append(bindings, output.RevisionArtifact{ArtifactID: artifact.ID, Role: input.Role})
		}
		revision, err = output.NewOutputRevision(revisionID, expected, profile, revisionNumber, bindings, command.ContentDigest, command.ActorID, s.clock.Now())
		if err != nil {
			return err
		}
		accepted, acceptanceErr := output.AcceptOutputRevision(revision, expected, profile, nil, command.ActorID, "No validation records are required by the output contract.", s.clock.Now())
		if acceptanceErr == nil {
			revision = accepted
		} else if !errors.Is(acceptanceErr, output.ErrAcceptanceIncomplete) {
			return acceptanceErr
		}
		if err := repository.CreateOutputRevision(ctx, revision); err != nil {
			return err
		}
		if err := s.recordActivity(ctx, repository, work.Activity{
			EntityKind: "output_revision", EntityID: revision.ID, WorkItemID: expected.WorkItemID, ActorID: command.ActorID,
			EventType: "output_revision.created", Summary: fmt.Sprintf("Output revision %d produced", revision.Revision),
		}); err != nil {
			return err
		}
		if revision.AcceptanceState == output.RevisionAccepted {
			return s.recordActivity(ctx, repository, work.Activity{
				EntityKind: "output_revision", EntityID: revision.ID, WorkItemID: expected.WorkItemID, ActorID: command.ActorID,
				EventType: "output_revision.accepted", Summary: fmt.Sprintf("Output revision %d accepted", revision.Revision),
			})
		}
		return nil
	})
	if err != nil {
		return output.OutputRevision{}, fmt.Errorf("create output revision: %w", err)
	}
	return revision, nil
}

type RecordValidationCommand struct {
	OutputRevisionID   string
	CriterionRef       string
	ValidatorKind      output.ValidatorKind
	Verdict            output.ValidationVerdict
	Score              *float64
	VerifierActorID    string
	EvidenceArtifactID string
	Details            json.RawMessage
}

func (s *Service) RecordValidation(ctx context.Context, command RecordValidationCommand) (output.OutputRevision, error) {
	id, err := s.ids.New()
	if err != nil {
		return output.OutputRevision{}, fmt.Errorf("generate validation id: %w", err)
	}
	var revision output.OutputRevision
	err = s.store.WithinTransaction(ctx, func(repository ports.Repository) error {
		var err error
		revision, err = repository.OutputRevision(ctx, command.OutputRevisionID)
		if err != nil {
			return err
		}
		if revision.AcceptanceState != output.RevisionProduced && revision.AcceptanceState != output.RevisionAccepted {
			return errors.New("only produced or accepted output revisions can receive validation")
		}
		expected, err := repository.ExpectedOutput(ctx, revision.ExpectedOutputID)
		if err != nil {
			return err
		}
		profile, err := repository.OutputProfileByID(ctx, revision.OutputProfileID)
		if err != nil {
			return err
		}
		if revision.AcceptanceState == output.RevisionAccepted &&
			(command.ValidatorKind != output.ValidatorSuccessorUse || command.Verdict != output.VerdictPassed) {
			return errors.New("accepted output revisions only accept passed successor-use evidence")
		}
		if revision.AcceptanceState == output.RevisionAccepted {
			required, err := output.IsRequiredValidationCriterion(expected, profile, command.CriterionRef)
			if err != nil {
				return err
			}
			if required {
				return errors.New("successor-use evidence cannot reuse an acceptance criterion reference")
			}
		}
		record, err := output.NewValidationRecord(id, revision, command.CriterionRef, command.ValidatorKind, command.Verdict, command.Score, command.VerifierActorID, command.EvidenceArtifactID, command.Details, s.clock.Now())
		if err != nil {
			return err
		}
		if err := repository.CreateValidationRecord(ctx, record); err != nil {
			return err
		}
		if err := s.recordActivity(ctx, repository, work.Activity{
			EntityKind: "validation_record", EntityID: record.ID, WorkItemID: expected.WorkItemID, ActorID: command.VerifierActorID,
			EventType: "output_validation.recorded", Summary: fmt.Sprintf("%s validation recorded as %s", record.ValidatorKind, record.Verdict),
		}); err != nil {
			return err
		}
		if revision.AcceptanceState == output.RevisionAccepted {
			return nil
		}
		validations, err := repository.ValidationRecords(ctx, revision.ID)
		if err != nil {
			return err
		}
		accepted, acceptanceErr := output.AcceptOutputRevision(revision, expected, profile, validations, command.VerifierActorID, "Output contract validation requirements satisfied.", s.clock.Now())
		if acceptanceErr != nil {
			if errors.Is(acceptanceErr, output.ErrAcceptanceIncomplete) {
				return nil
			}
			return acceptanceErr
		}
		if err := repository.UpdateOutputRevisionAcceptance(ctx, accepted); err != nil {
			return err
		}
		revision = accepted
		return s.recordActivity(ctx, repository, work.Activity{
			EntityKind: "output_revision", EntityID: revision.ID, WorkItemID: expected.WorkItemID, ActorID: command.VerifierActorID,
			EventType: "output_revision.accepted", Summary: fmt.Sprintf("Output revision %d accepted", revision.Revision),
		})
	})
	if err != nil {
		return output.OutputRevision{}, fmt.Errorf("record validation: %w", err)
	}
	return revision, nil
}

type AddOutputRequirementCommand struct {
	WorkItemID               string
	RequiredOutputRevisionID string
	RequiredProfileName      string
	VersionConstraint        string
	Required                 bool
	Note                     string
	ActorID                  string
	ExpectedVersion          int
	IdempotencyKey           string
}

func (s *Service) AddOutputRequirement(ctx context.Context, command AddOutputRequirementCommand) (output.OutputRequirement, error) {
	hasRevision := strings.TrimSpace(command.RequiredOutputRevisionID) != ""
	hasProfile := strings.TrimSpace(command.RequiredProfileName) != "" || strings.TrimSpace(command.VersionConstraint) != ""
	if hasRevision == hasProfile {
		return output.OutputRequirement{}, errors.New("output requirement must select exactly one revision or profile constraint")
	}
	id, err := s.ids.New()
	if err != nil {
		return output.OutputRequirement{}, fmt.Errorf("generate output requirement id: %w", err)
	}
	var requirement output.OutputRequirement
	err = s.store.WithinTransaction(ctx, func(repository ports.Repository) error {
		var executeErr error
		requirement, executeErr = executeIdempotently(ctx, s, repository, command.ActorID, command.IdempotencyKey, "add_output_requirement", command, func() (output.OutputRequirement, error) {
			item, err := repository.WorkItem(ctx, command.WorkItemID)
			if err != nil {
				return output.OutputRequirement{}, err
			}
			if item.Version != command.ExpectedVersion {
				return output.OutputRequirement{}, ports.ErrVersionConflict
			}
			if hasRevision {
				revision, err := repository.OutputRevision(ctx, command.RequiredOutputRevisionID)
				if err != nil {
					return output.OutputRequirement{}, err
				}
				requirement, err = output.NewExactOutputRequirement(id, item.ID, revision, command.Required, command.Note)
				if err != nil {
					return output.OutputRequirement{}, err
				}
			} else {
				requirement, err = output.NewProfileOutputRequirement(id, item.ID, command.RequiredProfileName, command.VersionConstraint, command.Required, command.Note)
				if err != nil {
					return output.OutputRequirement{}, err
				}
			}
			if err := repository.CreateOutputRequirement(ctx, requirement); err != nil {
				return output.OutputRequirement{}, err
			}
			item.Version++
			item.UpdatedAt = s.clock.Now().UTC()
			if err := repository.UpdateWorkItem(ctx, item, command.ExpectedVersion); err != nil {
				return output.OutputRequirement{}, err
			}
			if err := s.recordActivity(ctx, repository, work.Activity{
				EntityKind: "output_requirement", EntityID: requirement.ID, WorkItemID: item.ID, ActorID: command.ActorID,
				EventType: "output_requirement.added", Summary: "Reusable output requirement added",
			}); err != nil {
				return output.OutputRequirement{}, err
			}
			return requirement, nil
		})
		return executeErr
	})
	if err != nil {
		return output.OutputRequirement{}, fmt.Errorf("add output requirement: %w", err)
	}
	return requirement, nil
}

type AcceptedOutputFilter = ports.AcceptedOutputFilter
type ActivityFilter = ports.ActivityFilter

func (s *Service) ListReadyWork(ctx context.Context) ([]ports.ReadyWorkItem, error) {
	items, err := s.store.ListReadyWork(ctx)
	if err != nil {
		return nil, fmt.Errorf("list ready work: %w", err)
	}
	return items, nil
}

func (s *Service) ListReadyWorkForActor(ctx context.Context, actorID string) ([]ports.ReadyWorkItem, error) {
	items, err := s.store.ListReadyWorkForActor(ctx, actorID)
	if err != nil {
		return nil, fmt.Errorf("list ready work for actor: %w", err)
	}
	return items, nil
}

func (s *Service) ListAcceptedOutputs(ctx context.Context, filter AcceptedOutputFilter) ([]ports.AcceptedOutput, error) {
	if filter.Limit <= 0 {
		filter.Limit = 100
	}
	if filter.Limit > 500 {
		return nil, errors.New("accepted output limit cannot exceed 500")
	}
	if strings.TrimSpace(filter.VersionConstraint) != "" {
		version := strings.TrimPrefix(strings.TrimSpace(filter.VersionConstraint), "=")
		if parsed, err := strconv.Atoi(version); err != nil || parsed < 1 {
			return nil, errors.New("accepted output filter supports only a positive exact version constraint")
		}
		filter.VersionConstraint = version
	}
	items, err := s.store.ListAcceptedOutputs(ctx, filter)
	if err != nil {
		return nil, fmt.Errorf("list accepted outputs: %w", err)
	}
	return items, nil
}

func (s *Service) ListActivity(ctx context.Context, filter ActivityFilter) ([]work.Activity, error) {
	items, err := s.store.ListActivity(ctx, filter)
	if err != nil {
		return nil, fmt.Errorf("list activity: %w", err)
	}
	return items, nil
}

func (s *Service) recordActivity(ctx context.Context, repository ports.Repository, candidate work.Activity) error {
	id, err := s.ids.New()
	if err != nil {
		return fmt.Errorf("generate activity id: %w", err)
	}
	candidate.ID = id
	activity, err := work.NewActivity(candidate, s.clock.Now())
	if err != nil {
		return err
	}
	return repository.CreateActivity(ctx, activity)
}
