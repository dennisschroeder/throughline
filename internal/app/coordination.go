package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/dennisschroeder/throughline/internal/domain/output"
	"github.com/dennisschroeder/throughline/internal/domain/work"
	"github.com/dennisschroeder/throughline/internal/ports"
)

type RegisterActorCommand struct {
	Actor          work.Actor
	IdempotencyKey string
}

func (s *Service) RegisterActor(ctx context.Context, command RegisterActorCommand) (work.Actor, error) {
	actor, err := work.NewActor(command.Actor, s.clock.Now())
	if err != nil {
		return work.Actor{}, err
	}
	var registered work.Actor
	err = s.store.WithinTransaction(ctx, func(repository ports.Repository) error {
		registered, err = executeIdempotently(ctx, s, repository, actor.ID, command.IdempotencyKey, "register_actor", command, func() (work.Actor, error) {
			if err := repository.CreateActor(ctx, actor); err != nil {
				return work.Actor{}, err
			}
			if err := s.recordActivity(ctx, repository, work.Activity{
				EntityKind: "actor", EntityID: actor.ID, ActorID: actor.ID,
				EventType: "actor.registered", Summary: fmt.Sprintf("Actor %s registered", actor.DisplayName),
			}); err != nil {
				return work.Actor{}, err
			}
			return actor, nil
		})
		return err
	})
	if err != nil {
		return work.Actor{}, fmt.Errorf("register actor: %w", err)
	}
	return registered, nil
}

type AssignActorCapabilityCommand struct {
	ActorID        string
	Capability     string
	Description    string
	GrantedBy      string
	IdempotencyKey string
}

type ActorCapability struct {
	ActorID    string
	Capability work.Capability
}

type ApproveWorkItemExecutionCommand struct {
	WorkItemID      string
	ActorID         string
	ApprovedForID   string
	ExpectedVersion int
	IdempotencyKey  string
	Request         string
	Rationale       string
	ExpiresAt       *time.Time
}

func (s *Service) ApproveWorkItemExecution(ctx context.Context, command ApproveWorkItemExecutionCommand) (work.ExecutionApproval, error) {
	if replay, found, err := replayIdempotently[work.ExecutionApproval](ctx, s, command.ActorID, command.IdempotencyKey, "approve_work_item_execution", command); err != nil {
		return work.ExecutionApproval{}, err
	} else if found {
		return replay, nil
	}
	id, err := s.ids.New()
	if err != nil {
		return work.ExecutionApproval{}, fmt.Errorf("generate work item approval id: %w", err)
	}
	var result work.ExecutionApproval
	err = s.store.WithinTransaction(ctx, func(repository ports.Repository) error {
		result, err = executeIdempotently(ctx, s, repository, command.ActorID, command.IdempotencyKey, "approve_work_item_execution", command, func() (work.ExecutionApproval, error) {
			if _, err := repository.Actor(ctx, command.ActorID); err != nil {
				return work.ExecutionApproval{}, err
			}
			if _, err := repository.Actor(ctx, command.ApprovedForID); err != nil {
				return work.ExecutionApproval{}, err
			}
			item, err := repository.WorkItem(ctx, command.WorkItemID)
			if err != nil {
				return work.ExecutionApproval{}, err
			}
			if item.Version != command.ExpectedVersion {
				return work.ExecutionApproval{}, ports.ErrVersionConflict
			}
			approval, err := work.NewExecutionApproval(work.ExecutionApproval{ID: id, WorkItemID: item.ID, ApprovedForActorID: command.ApprovedForID, Request: command.Request, RequestedBy: command.ActorID, ResolvedBy: command.ActorID, Rationale: command.Rationale, ExpiresAt: command.ExpiresAt}, s.clock.Now())
			if err != nil {
				return work.ExecutionApproval{}, err
			}
			if err := repository.CreateWorkItemExecutionApproval(ctx, approval); err != nil {
				return work.ExecutionApproval{}, err
			}
			item.Version++
			item.UpdatedAt = s.clock.Now().UTC()
			if err := repository.UpdateWorkItem(ctx, item, command.ExpectedVersion); err != nil {
				return work.ExecutionApproval{}, err
			}
			if err := s.recordActivity(ctx, repository, work.Activity{EntityKind: "approval", EntityID: approval.ID, WorkItemID: item.ID, ActorID: command.ActorID, EventType: "work_item.execution_approved", Summary: "Work item execution approved"}); err != nil {
				return work.ExecutionApproval{}, err
			}
			return approval, nil
		})
		return err
	})
	if err != nil {
		return work.ExecutionApproval{}, fmt.Errorf("approve work item execution: %w", err)
	}
	return result, nil
}

func (s *Service) AssignActorCapability(ctx context.Context, command AssignActorCapabilityCommand) (ActorCapability, error) {
	capability, err := work.NewCapability(command.Capability, command.Description)
	if err != nil {
		return ActorCapability{}, err
	}
	command.ActorID = strings.TrimSpace(command.ActorID)
	command.GrantedBy = strings.TrimSpace(command.GrantedBy)
	if command.ActorID == "" || command.GrantedBy == "" {
		return ActorCapability{}, errors.New("capability assignment requires actor and granter")
	}
	var assigned ActorCapability
	err = s.store.WithinTransaction(ctx, func(repository ports.Repository) error {
		assigned, err = executeIdempotently(ctx, s, repository, command.GrantedBy, command.IdempotencyKey, "assign_actor_capability", command, func() (ActorCapability, error) {
			if _, err := repository.Actor(ctx, command.ActorID); err != nil {
				return ActorCapability{}, err
			}
			if _, err := repository.Actor(ctx, command.GrantedBy); err != nil {
				return ActorCapability{}, err
			}
			if err := repository.CreateCapability(ctx, capability); err != nil {
				return ActorCapability{}, err
			}
			if err := repository.AssignActorCapability(ctx, command.ActorID, capability.Slug); err != nil {
				return ActorCapability{}, err
			}
			result := ActorCapability{ActorID: command.ActorID, Capability: capability}
			if err := s.recordActivity(ctx, repository, work.Activity{
				EntityKind: "actor", EntityID: command.ActorID, ActorID: command.GrantedBy,
				EventType: "actor.capability_assigned", Summary: fmt.Sprintf("Capability %s assigned", capability.Slug),
			}); err != nil {
				return ActorCapability{}, err
			}
			return result, nil
		})
		return err
	})
	if err != nil {
		return ActorCapability{}, fmt.Errorf("assign actor capability: %w", err)
	}
	return assigned, nil
}

type ClaimWorkItemCommand struct {
	WorkItemID             string
	ActorID                string
	ExpectedVersion        int
	IdempotencyKey         string
	LeaseDuration          time.Duration
	TransitionToInProgress bool
}

type ClaimResult struct {
	Claim    work.Claim
	WorkItem work.WorkItem
}

type ClaimGateError struct {
	Requirements []work.ClaimRequirement
}

func (e ClaimGateError) Error() string { return "work item cannot be claimed" }

func (s *Service) ClaimWorkItem(ctx context.Context, command ClaimWorkItemCommand) (ClaimResult, error) {
	if replay, found, err := replayIdempotently[ClaimResult](ctx, s, command.ActorID, command.IdempotencyKey, "claim_work_item", command); err != nil {
		return ClaimResult{}, err
	} else if found {
		return replay, nil
	}
	claimID, err := s.ids.New()
	if err != nil {
		return ClaimResult{}, fmt.Errorf("generate claim id: %w", err)
	}
	var result ClaimResult
	err = s.store.WithinTransaction(ctx, func(repository ports.Repository) error {
		result, err = executeIdempotently(ctx, s, repository, command.ActorID, command.IdempotencyKey, "claim_work_item", command, func() (ClaimResult, error) {
			actor, err := repository.Actor(ctx, command.ActorID)
			if err != nil {
				return ClaimResult{}, err
			}
			item, err := repository.WorkItem(ctx, command.WorkItemID)
			if err != nil {
				return ClaimResult{}, err
			}
			if item.Version != command.ExpectedVersion {
				return ClaimResult{}, ports.ErrVersionConflict
			}
			now := s.clock.Now()
			expired, err := repository.ExpireClaims(ctx, item.ID, now)
			if err != nil {
				return ClaimResult{}, err
			}
			for _, claim := range expired {
				if err := s.recordActivity(ctx, repository, work.Activity{
					EntityKind: "claim", EntityID: claim.ID, WorkItemID: item.ID, ActorID: claim.ActorID,
					EventType: "claim.expired", Summary: "Claim lease expired",
				}); err != nil {
					return ClaimResult{}, err
				}
			}
			activeClaim, err := repository.ActiveClaim(ctx, item.ID, now)
			if err != nil {
				return ClaimResult{}, err
			}
			requirements, err := claimRequirements(ctx, repository, item, actor, item.ExecutionStatus, activeClaim, now)
			if err != nil {
				return ClaimResult{}, err
			}
			if len(requirements) != 0 {
				return ClaimResult{}, ClaimGateError{Requirements: requirements}
			}
			claim, err := work.NewClaim(claimID, item.ID, actor.ID, command.LeaseDuration, now)
			if err != nil {
				return ClaimResult{}, err
			}
			if err := repository.CreateClaim(ctx, claim); err != nil {
				return ClaimResult{}, err
			}
			if command.TransitionToInProgress {
				item, err = work.TransitionWorkItem(item, work.StatusInProgress, actor.ID, "", now)
				if err != nil {
					return ClaimResult{}, err
				}
			} else {
				item.Version++
				item.UpdatedAt = now.UTC()
			}
			if err := repository.UpdateWorkItem(ctx, item, command.ExpectedVersion); err != nil {
				return ClaimResult{}, err
			}
			if err := s.recordActivity(ctx, repository, work.Activity{
				EntityKind: "claim", EntityID: claim.ID, WorkItemID: item.ID, ActorID: actor.ID,
				EventType: "claim.acquired", Summary: "Work item claimed",
			}); err != nil {
				return ClaimResult{}, err
			}
			if command.TransitionToInProgress {
				if err := s.recordActivity(ctx, repository, work.Activity{
					EntityKind: "work_item", EntityID: item.ID, WorkItemID: item.ID, ActorID: actor.ID,
					EventType: "work_item.status_changed", Summary: "Work item moved from ready to in_progress",
				}); err != nil {
					return ClaimResult{}, err
				}
			}
			return ClaimResult{Claim: claim, WorkItem: item}, nil
		})
		return err
	})
	if err != nil {
		return ClaimResult{}, fmt.Errorf("claim work item: %w", err)
	}
	return result, nil
}

type RenewClaimCommand struct {
	WorkItemID      string
	ClaimID         string
	ActorID         string
	ExpectedVersion int
	IdempotencyKey  string
	Extension       time.Duration
}

func (s *Service) RenewClaim(ctx context.Context, command RenewClaimCommand) (ClaimResult, error) {
	var result ClaimResult
	err := s.store.WithinTransaction(ctx, func(repository ports.Repository) error {
		var err error
		result, err = executeIdempotently(ctx, s, repository, command.ActorID, command.IdempotencyKey, "renew_claim", command, func() (ClaimResult, error) {
			item, err := repository.WorkItem(ctx, command.WorkItemID)
			if err != nil {
				return ClaimResult{}, err
			}
			if item.Version != command.ExpectedVersion {
				return ClaimResult{}, ports.ErrVersionConflict
			}
			claim, err := repository.Claim(ctx, command.ClaimID)
			if err != nil {
				return ClaimResult{}, err
			}
			if claim.WorkItemID != item.ID {
				return ClaimResult{}, errors.New("claim belongs to another work item")
			}
			now := s.clock.Now()
			actor, err := repository.Actor(ctx, command.ActorID)
			if err != nil {
				return ClaimResult{}, err
			}
			requirements, err := claimRequirements(ctx, repository, item, actor, work.StatusReady, nil, now)
			if err != nil {
				return ClaimResult{}, err
			}
			if len(requirements) != 0 {
				return ClaimResult{}, ClaimGateError{Requirements: requirements}
			}
			claim, err = work.RenewClaim(claim, command.ActorID, command.Extension, now)
			if err != nil {
				return ClaimResult{}, err
			}
			if err := repository.RenewClaim(ctx, claim, now); err != nil {
				return ClaimResult{}, err
			}
			item.Version++
			item.UpdatedAt = now.UTC()
			if err := repository.UpdateWorkItem(ctx, item, command.ExpectedVersion); err != nil {
				return ClaimResult{}, err
			}
			if err := s.recordActivity(ctx, repository, work.Activity{
				EntityKind: "claim", EntityID: claim.ID, WorkItemID: item.ID, ActorID: command.ActorID,
				EventType: "claim.renewed", Summary: "Claim lease renewed",
			}); err != nil {
				return ClaimResult{}, err
			}
			return ClaimResult{Claim: claim, WorkItem: item}, nil
		})
		return err
	})
	if err != nil {
		return ClaimResult{}, fmt.Errorf("renew claim: %w", err)
	}
	return result, nil
}

type ReleaseClaimCommand struct {
	WorkItemID      string
	ClaimID         string
	ActorID         string
	ExpectedVersion int
	IdempotencyKey  string
	Reason          string
	ReturnToReady   bool
}

func (s *Service) ReleaseClaim(ctx context.Context, command ReleaseClaimCommand) (ClaimResult, error) {
	var result ClaimResult
	err := s.store.WithinTransaction(ctx, func(repository ports.Repository) error {
		var err error
		result, err = executeIdempotently(ctx, s, repository, command.ActorID, command.IdempotencyKey, "release_claim", command, func() (ClaimResult, error) {
			item, err := repository.WorkItem(ctx, command.WorkItemID)
			if err != nil {
				return ClaimResult{}, err
			}
			if item.Version != command.ExpectedVersion {
				return ClaimResult{}, ports.ErrVersionConflict
			}
			claim, err := repository.Claim(ctx, command.ClaimID)
			if err != nil {
				return ClaimResult{}, err
			}
			if claim.WorkItemID != item.ID {
				return ClaimResult{}, errors.New("claim belongs to another work item")
			}
			now := s.clock.Now()
			claim, err = work.ReleaseClaim(claim, command.ActorID, command.Reason, now)
			if err != nil {
				return ClaimResult{}, err
			}
			if err := repository.ReleaseClaim(ctx, claim); err != nil {
				return ClaimResult{}, err
			}
			if command.ReturnToReady {
				actor, err := repository.Actor(ctx, command.ActorID)
				if err != nil {
					return ClaimResult{}, err
				}
				requirements, err := claimRequirements(ctx, repository, item, actor, work.StatusReady, nil, now)
				if err != nil {
					return ClaimResult{}, err
				}
				if len(requirements) != 0 {
					return ClaimResult{}, ClaimGateError{Requirements: requirements}
				}
				item, err = work.TransitionWorkItem(item, work.StatusReady, command.ActorID, command.Reason, now)
				if err != nil {
					return ClaimResult{}, err
				}
			} else {
				item.Version++
				item.UpdatedAt = now.UTC()
			}
			if err := repository.UpdateWorkItem(ctx, item, command.ExpectedVersion); err != nil {
				return ClaimResult{}, err
			}
			if err := s.recordActivity(ctx, repository, work.Activity{
				EntityKind: "claim", EntityID: claim.ID, WorkItemID: item.ID, ActorID: command.ActorID,
				EventType: "claim.released", Summary: "Claim released",
			}); err != nil {
				return ClaimResult{}, err
			}
			return ClaimResult{Claim: claim, WorkItem: item}, nil
		})
		return err
	})
	if err != nil {
		return ClaimResult{}, fmt.Errorf("release claim: %w", err)
	}
	return result, nil
}

func claimRequirements(ctx context.Context, repository ports.Repository, item work.WorkItem, actor work.Actor, executionStatus work.ExecutionStatus, activeClaim *work.Claim, now time.Time) ([]work.ClaimRequirement, error) {
	objective, err := repository.Objective(ctx, item.ObjectiveID)
	if err != nil {
		return nil, err
	}
	planApproved, err := repository.PlanApprovedForWorkItem(ctx, item.ID)
	if err != nil {
		return nil, err
	}
	dependenciesSatisfied, err := repository.HardDependenciesSatisfied(ctx, item.ID)
	if err != nil {
		return nil, err
	}
	hasOpenBlocker, err := repository.HasOpenBlocker(ctx, item.ID)
	if err != nil {
		return nil, err
	}
	outputRequirementsSatisfied, err := repository.OutputRequirementsSatisfied(ctx, item.ID)
	if err != nil {
		return nil, err
	}
	capabilities, err := repository.RequiredCapabilities(ctx, item.ID)
	if err != nil {
		return nil, err
	}
	capabilitiesSatisfied, err := repository.ActorHasCapabilities(ctx, actor.ID, capabilities)
	if err != nil {
		return nil, err
	}
	approvalSatisfied, err := repository.WorkItemApprovalSatisfied(ctx, item.ID, actor.ID, now)
	if err != nil {
		return nil, err
	}
	return work.EvaluateClaimGate(work.ClaimGateFacts{
		ObjectivePhase: objective.Phase, PlanApproved: planApproved, ItemCommitment: item.CommitmentState,
		ExecutionStatus: executionStatus, ExecutionPolicy: item.ExecutionPolicy, RequiredActorKind: item.RequiredActorKind,
		Actor: actor, HardDependenciesSatisfied: dependenciesSatisfied, HasOpenBlocker: hasOpenBlocker,
		OutputRequirementsSatisfied: outputRequirementsSatisfied, CapabilitiesSatisfied: capabilitiesSatisfied,
		ApprovalSatisfied: approvalSatisfied, ActiveClaim: activeClaim, Now: now,
	}), nil
}

type AppendProgressCommand struct {
	WorkItemID      string
	ActorID         string
	ExpectedVersion int
	IdempotencyKey  string
	Summary         string
	Completed       []string
	Remaining       []string
	Discovered      []string
	Blocker         string
}

type ProgressResult struct {
	Entry    work.ProgressEntry
	WorkItem work.WorkItem
}

func (s *Service) AppendProgress(ctx context.Context, command AppendProgressCommand) (ProgressResult, error) {
	if replay, found, err := replayIdempotently[ProgressResult](ctx, s, command.ActorID, command.IdempotencyKey, "append_progress", command); err != nil {
		return ProgressResult{}, err
	} else if found {
		return replay, nil
	}
	id, err := s.ids.New()
	if err != nil {
		return ProgressResult{}, fmt.Errorf("generate progress id: %w", err)
	}
	var result ProgressResult
	err = s.store.WithinTransaction(ctx, func(repository ports.Repository) error {
		result, err = executeIdempotently(ctx, s, repository, command.ActorID, command.IdempotencyKey, "append_progress", command, func() (ProgressResult, error) {
			if _, err := repository.Actor(ctx, command.ActorID); err != nil {
				return ProgressResult{}, err
			}
			item, err := repository.WorkItem(ctx, command.WorkItemID)
			if err != nil {
				return ProgressResult{}, err
			}
			if item.Version != command.ExpectedVersion {
				return ProgressResult{}, ports.ErrVersionConflict
			}
			entry, err := work.NewProgressEntry(work.ProgressEntry{
				ID: id, WorkItemID: item.ID, ActorID: command.ActorID, Summary: command.Summary,
				Completed: command.Completed, Remaining: command.Remaining, Discovered: command.Discovered, Blocker: command.Blocker,
			}, s.clock.Now())
			if err != nil {
				return ProgressResult{}, err
			}
			if err := repository.CreateProgressEntry(ctx, entry); err != nil {
				return ProgressResult{}, err
			}
			item.Version++
			item.UpdatedAt = s.clock.Now().UTC()
			if err := repository.UpdateWorkItem(ctx, item, command.ExpectedVersion); err != nil {
				return ProgressResult{}, err
			}
			if err := s.recordActivity(ctx, repository, work.Activity{
				EntityKind: "progress", EntityID: entry.ID, WorkItemID: item.ID, ActorID: command.ActorID,
				EventType: "progress.appended", Summary: entry.Summary,
			}); err != nil {
				return ProgressResult{}, err
			}
			return ProgressResult{Entry: entry, WorkItem: item}, nil
		})
		return err
	})
	if err != nil {
		return ProgressResult{}, fmt.Errorf("append progress: %w", err)
	}
	return result, nil
}

type AttachArtifactCommand struct {
	WorkItemID      string
	ActorID         string
	ExpectedVersion int
	IdempotencyKey  string
	Kind            string
	URI             string
	Title           string
	Metadata        json.RawMessage
}

type ArtifactResult struct {
	Artifact output.Artifact
	WorkItem work.WorkItem
}

func (s *Service) AttachArtifact(ctx context.Context, command AttachArtifactCommand) (ArtifactResult, error) {
	if replay, found, err := replayIdempotently[ArtifactResult](ctx, s, command.ActorID, command.IdempotencyKey, "attach_artifact", command); err != nil {
		return ArtifactResult{}, err
	} else if found {
		return replay, nil
	}
	id, err := s.ids.New()
	if err != nil {
		return ArtifactResult{}, fmt.Errorf("generate artifact id: %w", err)
	}
	var result ArtifactResult
	err = s.store.WithinTransaction(ctx, func(repository ports.Repository) error {
		result, err = executeIdempotently(ctx, s, repository, command.ActorID, command.IdempotencyKey, "attach_artifact", command, func() (ArtifactResult, error) {
			if _, err := repository.Actor(ctx, command.ActorID); err != nil {
				return ArtifactResult{}, err
			}
			item, err := repository.WorkItem(ctx, command.WorkItemID)
			if err != nil {
				return ArtifactResult{}, err
			}
			if item.Version != command.ExpectedVersion {
				return ArtifactResult{}, ports.ErrVersionConflict
			}
			artifact, err := output.NewArtifact(output.Artifact{
				ID: id, WorkItemID: item.ID, Kind: command.Kind, URI: command.URI, Title: command.Title,
				Metadata: command.Metadata, AttachedBy: command.ActorID,
			}, s.clock.Now())
			if err != nil {
				return ArtifactResult{}, err
			}
			existing, lookupErr := repository.ArtifactByURI(ctx, item.ID, artifact.URI)
			if lookupErr == nil {
				if existing.Kind != artifact.Kind {
					return ArtifactResult{}, errors.New("an artifact URI cannot be reused with a different kind")
				}
				return ArtifactResult{Artifact: existing, WorkItem: item}, nil
			}
			if !errors.Is(lookupErr, ports.ErrNotFound) {
				return ArtifactResult{}, lookupErr
			}
			if err := repository.CreateArtifact(ctx, artifact); err != nil {
				return ArtifactResult{}, err
			}
			item.Version++
			item.UpdatedAt = s.clock.Now().UTC()
			if err := repository.UpdateWorkItem(ctx, item, command.ExpectedVersion); err != nil {
				return ArtifactResult{}, err
			}
			if err := s.recordActivity(ctx, repository, work.Activity{
				EntityKind: "artifact", EntityID: artifact.ID, WorkItemID: item.ID, ActorID: command.ActorID,
				EventType: "artifact.attached", Summary: fmt.Sprintf("Artifact %s attached", artifact.Kind),
			}); err != nil {
				return ArtifactResult{}, err
			}
			return ArtifactResult{Artifact: artifact, WorkItem: item}, nil
		})
		return err
	})
	if err != nil {
		return ArtifactResult{}, fmt.Errorf("attach artifact: %w", err)
	}
	return result, nil
}
