package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/dennisschroeder/throughline/internal/domain/output"
	"github.com/dennisschroeder/throughline/internal/domain/work"
	"github.com/dennisschroeder/throughline/internal/ports"
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
					return work.WorkItem{}, ClaimGateError{Requirements: []work.ClaimRequirement{{Code: work.ClaimRequirementClaimAvailable, Message: "only the active claim owner can advance work"}}}
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

type BlockWorkItemCommand struct {
	WorkItemID      string
	ActorID         string
	IdempotencyKey  string
	ExpectedVersion int
	Reason          string
}

func (s *Service) BlockWorkItem(ctx context.Context, command BlockWorkItemCommand) (work.ManualBlocker, error) {
	if replay, found, err := replayIdempotently[work.ManualBlocker](ctx, s, command.ActorID, command.IdempotencyKey, "block_work_item", command); err != nil {
		return work.ManualBlocker{}, err
	} else if found {
		return replay, nil
	}
	id, err := s.ids.New()
	if err != nil {
		return work.ManualBlocker{}, fmt.Errorf("generate blocker id: %w", err)
	}
	blocker, err := work.NewManualBlocker(id, command.WorkItemID, command.Reason, command.ActorID, s.clock.Now())
	if err != nil {
		return work.ManualBlocker{}, err
	}
	err = s.store.WithinTransaction(ctx, func(repository ports.Repository) error {
		created, err := executeIdempotently(ctx, s, repository, command.ActorID, command.IdempotencyKey, "block_work_item", command, func() (work.ManualBlocker, error) {
			item, err := repository.WorkItem(ctx, command.WorkItemID)
			if err != nil {
				return work.ManualBlocker{}, err
			}
			if item.Version != command.ExpectedVersion {
				return work.ManualBlocker{}, ports.ErrVersionConflict
			}
			if err := repository.CreateManualBlocker(ctx, blocker); err != nil {
				return work.ManualBlocker{}, err
			}
			item.Version++
			item.UpdatedAt = s.clock.Now().UTC()
			if err := repository.UpdateWorkItem(ctx, item, command.ExpectedVersion); err != nil {
				return work.ManualBlocker{}, err
			}
			if err := s.recordActivity(ctx, repository, work.Activity{EntityKind: "manual_blocker", EntityID: blocker.ID, WorkItemID: item.ID, ActorID: command.ActorID, EventType: "work_item.blocked", Summary: blocker.Reason}); err != nil {
				return work.ManualBlocker{}, err
			}
			return blocker, nil
		})
		blocker = created
		return err
	})
	if err != nil {
		return work.ManualBlocker{}, fmt.Errorf("block work item: %w", err)
	}
	return blocker, nil
}

type UnblockWorkItemCommand struct {
	BlockerID       string
	ActorID         string
	IdempotencyKey  string
	ExpectedVersion int
	Resolution      string
}

func (s *Service) UnblockWorkItem(ctx context.Context, command UnblockWorkItemCommand) (work.ManualBlocker, error) {
	var blocker work.ManualBlocker
	err := s.store.WithinTransaction(ctx, func(repository ports.Repository) error {
		resolved, err := executeIdempotently(ctx, s, repository, command.ActorID, command.IdempotencyKey, "unblock_work_item", command, func() (work.ManualBlocker, error) {
			current, err := repository.ManualBlocker(ctx, command.BlockerID)
			if err != nil {
				return work.ManualBlocker{}, err
			}
			item, err := repository.WorkItem(ctx, current.WorkItemID)
			if err != nil {
				return work.ManualBlocker{}, err
			}
			if item.Version != command.ExpectedVersion {
				return work.ManualBlocker{}, ports.ErrVersionConflict
			}
			updated, err := work.ResolveManualBlocker(current, command.ActorID, command.Resolution, s.clock.Now())
			if err != nil {
				return work.ManualBlocker{}, err
			}
			if err := repository.UpdateManualBlocker(ctx, updated); err != nil {
				return work.ManualBlocker{}, err
			}
			item.Version++
			item.UpdatedAt = s.clock.Now().UTC()
			if err := repository.UpdateWorkItem(ctx, item, command.ExpectedVersion); err != nil {
				return work.ManualBlocker{}, err
			}
			if err := s.recordActivity(ctx, repository, work.Activity{EntityKind: "manual_blocker", EntityID: updated.ID, WorkItemID: item.ID, ActorID: command.ActorID, EventType: "work_item.unblocked", Summary: updated.Resolution}); err != nil {
				return work.ManualBlocker{}, err
			}
			return updated, nil
		})
		blocker = resolved
		return err
	})
	if err != nil {
		return work.ManualBlocker{}, fmt.Errorf("unblock work item: %w", err)
	}
	return blocker, nil
}

func (s *Service) LinkDependency(ctx context.Context, command LinkDependencyCommand) (work.Dependency, error) {
	if replay, found, err := replayIdempotently[work.Dependency](ctx, s, command.ActorID, command.IdempotencyKey, "link_dependency", command); err != nil {
		return work.Dependency{}, err
	} else if found {
		return replay, nil
	}
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

type UnlinkDependencyCommand struct {
	WorkItemID          string
	DependsOnWorkItemID string
	Kind                work.DependencyKind
	ActorID             string
	ExpectedVersion     int
	IdempotencyKey      string
}

func (s *Service) UnlinkDependency(ctx context.Context, command UnlinkDependencyCommand) (work.WorkItem, error) {
	var item work.WorkItem
	err := s.store.WithinTransaction(ctx, func(repository ports.Repository) error {
		result, err := executeIdempotently(ctx, s, repository, command.ActorID, command.IdempotencyKey, "unlink_dependency", command, func() (work.WorkItem, error) {
			current, err := repository.WorkItem(ctx, command.WorkItemID)
			if err != nil {
				return work.WorkItem{}, err
			}
			if current.Version != command.ExpectedVersion {
				return work.WorkItem{}, ports.ErrVersionConflict
			}
			if err := repository.DeleteDependency(ctx, command.WorkItemID, command.DependsOnWorkItemID, command.Kind); err != nil {
				return work.WorkItem{}, err
			}
			current.Version++
			current.UpdatedAt = s.clock.Now().UTC()
			if err := repository.UpdateWorkItem(ctx, current, command.ExpectedVersion); err != nil {
				return work.WorkItem{}, err
			}
			if err := s.recordActivity(ctx, repository, work.Activity{EntityKind: "dependency", EntityID: command.WorkItemID + ":" + command.DependsOnWorkItemID + ":" + string(command.Kind), WorkItemID: current.ID, ActorID: command.ActorID, EventType: "dependency.unlinked", Summary: fmt.Sprintf("%s dependency unlinked", command.Kind)}); err != nil {
				return work.WorkItem{}, err
			}
			return current, nil
		})
		item = result
		return err
	})
	if err != nil {
		return work.WorkItem{}, fmt.Errorf("unlink dependency: %w", err)
	}
	return item, nil
}

type OutputArtifactInput struct {
	Kind     string          `json:"kind"`
	URI      string          `json:"uri"`
	Title    string          `json:"title,omitempty"`
	Metadata json.RawMessage `json:"metadata,omitempty"`
	Role     string          `json:"role,omitempty"`
}

type CreateOutputRevisionCommand struct {
	ExpectedOutputID string
	ActorID          string
	IdempotencyKey   string
	ContentDigest    string
	Artifacts        []OutputArtifactInput
}

func (s *Service) CreateOutputRevision(ctx context.Context, command CreateOutputRevisionCommand) (output.OutputRevision, error) {
	if replay, found, err := replayIdempotently[output.OutputRevision](ctx, s, command.ActorID, command.IdempotencyKey, "create_output_revision", command); err != nil {
		return output.OutputRevision{}, err
	} else if found {
		return replay, nil
	}
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
		created, err := executeIdempotently(ctx, s, repository, command.ActorID, command.IdempotencyKey, "create_output_revision", command, func() (output.OutputRevision, error) {
			expected, err := repository.ExpectedOutput(ctx, command.ExpectedOutputID)
			if err != nil {
				return output.OutputRevision{}, err
			}
			profile, err := repository.OutputProfileByID(ctx, expected.OutputProfileID)
			if err != nil {
				return output.OutputRevision{}, err
			}
			revisionNumber, err := repository.NextOutputRevision(ctx, expected.ID)
			if err != nil {
				return output.OutputRevision{}, err
			}
			bindings := make([]output.RevisionArtifact, 0, len(command.Artifacts))
			for index, input := range command.Artifacts {
				artifact, err := output.NewArtifact(output.Artifact{
					ID: artifactIDs[index], WorkItemID: expected.WorkItemID, Kind: input.Kind, URI: input.URI,
					Title: input.Title, Metadata: input.Metadata, AttachedBy: command.ActorID,
				}, s.clock.Now())
				if err != nil {
					return output.OutputRevision{}, err
				}
				existing, lookupErr := repository.ArtifactByURI(ctx, expected.WorkItemID, artifact.URI)
				if lookupErr == nil {
					if existing.Kind != artifact.Kind {
						return output.OutputRevision{}, errors.New("an artifact URI cannot be reused with a different kind")
					}
					artifact = existing
				} else if errors.Is(lookupErr, ports.ErrNotFound) {
					if err := repository.CreateArtifact(ctx, artifact); err != nil {
						return output.OutputRevision{}, err
					}
				} else {
					return output.OutputRevision{}, lookupErr
				}
				bindings = append(bindings, output.RevisionArtifact{ArtifactID: artifact.ID, Role: input.Role})
			}
			revision, err = output.NewOutputRevision(revisionID, expected, profile, revisionNumber, bindings, command.ContentDigest, command.ActorID, s.clock.Now())
			if err != nil {
				return output.OutputRevision{}, err
			}
			accepted, acceptanceErr := output.AcceptOutputRevision(revision, expected, profile, nil, command.ActorID, "No validation records are required by the output contract.", s.clock.Now())
			if acceptanceErr == nil {
				revision = accepted
			} else if !errors.Is(acceptanceErr, output.ErrAcceptanceIncomplete) {
				return output.OutputRevision{}, acceptanceErr
			}
			if err := repository.CreateOutputRevision(ctx, revision); err != nil {
				return output.OutputRevision{}, err
			}
			if err := s.recordActivity(ctx, repository, work.Activity{
				EntityKind: "output_revision", EntityID: revision.ID, WorkItemID: expected.WorkItemID, ActorID: command.ActorID,
				EventType: "output_revision.created", Summary: fmt.Sprintf("Output revision %d produced", revision.Revision),
			}); err != nil {
				return output.OutputRevision{}, err
			}
			if revision.AcceptanceState == output.RevisionAccepted {
				if err := s.recordActivity(ctx, repository, work.Activity{
					EntityKind: "output_revision", EntityID: revision.ID, WorkItemID: expected.WorkItemID, ActorID: command.ActorID,
					EventType: "output_revision.accepted", Summary: fmt.Sprintf("Output revision %d accepted", revision.Revision),
				}); err != nil {
					return output.OutputRevision{}, err
				}
			}
			return revision, nil
		})
		revision = created
		return err
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
	IdempotencyKey     string
}

func (s *Service) RecordValidation(ctx context.Context, command RecordValidationCommand) (output.OutputRevision, error) {
	if replay, found, err := replayIdempotently[output.OutputRevision](ctx, s, command.VerifierActorID, command.IdempotencyKey, "record_validation", command); err != nil {
		return output.OutputRevision{}, err
	} else if found {
		return replay, nil
	}
	id, err := s.ids.New()
	if err != nil {
		return output.OutputRevision{}, fmt.Errorf("generate validation id: %w", err)
	}
	var revision output.OutputRevision
	err = s.store.WithinTransaction(ctx, func(repository ports.Repository) error {
		recorded, err := executeIdempotently(ctx, s, repository, command.VerifierActorID, command.IdempotencyKey, "record_validation", command, func() (output.OutputRevision, error) {
			var err error
			revision, err = repository.OutputRevision(ctx, command.OutputRevisionID)
			if err != nil {
				return output.OutputRevision{}, err
			}
			if revision.AcceptanceState != output.RevisionProduced && revision.AcceptanceState != output.RevisionAccepted {
				return output.OutputRevision{}, errors.New("only produced or accepted output revisions can receive validation")
			}
			expected, err := repository.ExpectedOutput(ctx, revision.ExpectedOutputID)
			if err != nil {
				return output.OutputRevision{}, err
			}
			profile, err := repository.OutputProfileByID(ctx, revision.OutputProfileID)
			if err != nil {
				return output.OutputRevision{}, err
			}
			if revision.AcceptanceState == output.RevisionAccepted &&
				(command.ValidatorKind != output.ValidatorSuccessorUse || command.Verdict != output.VerdictPassed) {
				return output.OutputRevision{}, errors.New("accepted output revisions only accept passed successor-use evidence")
			}
			if revision.AcceptanceState == output.RevisionAccepted {
				required, err := output.IsRequiredValidationCriterion(expected, profile, command.CriterionRef)
				if err != nil {
					return output.OutputRevision{}, err
				}
				if required {
					return output.OutputRevision{}, errors.New("successor-use evidence cannot reuse an acceptance criterion reference")
				}
			}
			record, err := output.NewValidationRecord(id, revision, command.CriterionRef, command.ValidatorKind, command.Verdict, command.Score, command.VerifierActorID, command.EvidenceArtifactID, command.Details, s.clock.Now())
			if err != nil {
				return output.OutputRevision{}, err
			}
			if err := repository.CreateValidationRecord(ctx, record); err != nil {
				return output.OutputRevision{}, err
			}
			if err := s.recordActivity(ctx, repository, work.Activity{
				EntityKind: "validation_record", EntityID: record.ID, WorkItemID: expected.WorkItemID, ActorID: command.VerifierActorID,
				EventType: "output_validation.recorded", Summary: fmt.Sprintf("%s validation recorded as %s", record.ValidatorKind, record.Verdict),
			}); err != nil {
				return output.OutputRevision{}, err
			}
			if revision.AcceptanceState == output.RevisionAccepted {
				return revision, nil
			}
			validations, err := repository.ValidationRecords(ctx, revision.ID)
			if err != nil {
				return output.OutputRevision{}, err
			}
			accepted, acceptanceErr := output.AcceptOutputRevision(revision, expected, profile, validations, command.VerifierActorID, "Output contract validation requirements satisfied.", s.clock.Now())
			if acceptanceErr != nil {
				if errors.Is(acceptanceErr, output.ErrAcceptanceIncomplete) {
					return revision, nil
				}
				return output.OutputRevision{}, acceptanceErr
			}
			if err := repository.UpdateOutputRevisionAcceptance(ctx, accepted); err != nil {
				return output.OutputRevision{}, err
			}
			revision = accepted
			if err := s.recordActivity(ctx, repository, work.Activity{
				EntityKind: "output_revision", EntityID: revision.ID, WorkItemID: expected.WorkItemID, ActorID: command.VerifierActorID,
				EventType: "output_revision.accepted", Summary: fmt.Sprintf("Output revision %d accepted", revision.Revision),
			}); err != nil {
				return output.OutputRevision{}, err
			}
			return revision, nil
		})
		revision = recorded
		return err
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
	if replay, found, err := replayIdempotently[output.OutputRequirement](ctx, s, command.ActorID, command.IdempotencyKey, "add_output_requirement", command); err != nil {
		return output.OutputRequirement{}, err
	} else if found {
		return replay, nil
	}
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

func (s *Service) LatestActivitySequence(ctx context.Context) (int64, error) {
	sequence, err := s.store.LatestActivitySequence(ctx)
	if err != nil {
		return 0, fmt.Errorf("latest activity sequence: %w", err)
	}
	return sequence, nil
}

func (s *Service) IdempotencyCursor(ctx context.Context, actorID, key string) (int64, error) {
	record, err := s.store.IdempotencyRecord(ctx, actorID, key)
	if err != nil {
		return 0, fmt.Errorf("read idempotency record: %w", err)
	}
	var response struct {
		ChangeCursor int64 `json:"change_cursor"`
	}
	if err := json.Unmarshal(record.Response, &response); err != nil {
		return 0, fmt.Errorf("decode idempotency response: %w", err)
	}
	return response.ChangeCursor, nil
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
