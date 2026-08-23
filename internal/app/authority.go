package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/dennisschroeder/workgraph/internal/domain/authority"
	"github.com/dennisschroeder/workgraph/internal/domain/work"
	"github.com/dennisschroeder/workgraph/internal/ports"
)

type ExternalActionResult struct {
	Action   authority.ExternalAction
	Revision authority.ExternalActionRevision
}

func (s *Service) GetExternalAction(ctx context.Context, id string) (authority.ExternalAction, error) {
	var action authority.ExternalAction
	err := s.store.WithinTransaction(ctx, func(repository ports.Repository) error {
		var err error
		action, err = repository.ExternalAction(ctx, id)
		return err
	})
	if err != nil {
		return authority.ExternalAction{}, fmt.Errorf("get external action: %w", err)
	}
	return action, nil
}

func (s *Service) GetCurrentExternalActionRevision(ctx context.Context, actionID string) (authority.ExternalActionRevision, error) {
	var revision authority.ExternalActionRevision
	err := s.store.WithinTransaction(ctx, func(repository ports.Repository) error {
		var err error
		revision, err = repository.CurrentExternalActionRevision(ctx, actionID)
		return err
	})
	if err != nil {
		return authority.ExternalActionRevision{}, fmt.Errorf("get current external action revision: %w", err)
	}
	return revision, nil
}

func (s *Service) GetExternalActionExecution(ctx context.Context, id string) (authority.ExternalActionExecution, error) {
	var execution authority.ExternalActionExecution
	err := s.store.WithinTransaction(ctx, func(repository ports.Repository) error {
		var err error
		execution, err = repository.ExternalActionExecution(ctx, id)
		return err
	})
	return execution, err
}

func (s *Service) GetActionApproval(ctx context.Context, id string) (authority.ActionApproval, error) {
	var approval authority.ActionApproval
	err := s.store.WithinTransaction(ctx, func(repository ports.Repository) error {
		var err error
		approval, err = repository.ActionApproval(ctx, id)
		return err
	})
	return approval, err
}

type ProposeExternalActionCommand struct {
	WorkItemID      string
	ActorID         string
	ExpectedVersion int
	IdempotencyKey  string
	Required        bool
	Title           string
	Rationale       string
	Subject         json.RawMessage
}

func (s *Service) ProposeExternalAction(ctx context.Context, command ProposeExternalActionCommand) (ExternalActionResult, error) {
	if replay, found, err := replayIdempotently[ExternalActionResult](ctx, s, command.ActorID, command.IdempotencyKey, "propose_external_action", command); err != nil {
		return ExternalActionResult{}, err
	} else if found {
		return replay, nil
	}
	id, err := s.ids.New()
	if err != nil {
		return ExternalActionResult{}, fmt.Errorf("generate external action id: %w", err)
	}
	var result ExternalActionResult
	err = s.store.WithinTransaction(ctx, func(repository ports.Repository) error {
		result, err = executeIdempotently(ctx, s, repository, command.ActorID, command.IdempotencyKey, "propose_external_action", command, func() (ExternalActionResult, error) {
			if _, err := repository.Actor(ctx, command.ActorID); err != nil {
				return ExternalActionResult{}, err
			}
			item, err := repository.WorkItem(ctx, command.WorkItemID)
			if err != nil {
				return ExternalActionResult{}, err
			}
			if item.Version != command.ExpectedVersion {
				return ExternalActionResult{}, ports.ErrVersionConflict
			}
			action, revision, err := authority.NewExternalAction(authority.ExternalAction{
				ID: id, WorkItemID: item.ID, Required: command.Required, Title: command.Title, Rationale: command.Rationale,
			}, command.Subject, command.ActorID, s.clock.Now())
			if err != nil {
				return ExternalActionResult{}, err
			}
			if err := repository.CreateExternalAction(ctx, action); err != nil {
				return ExternalActionResult{}, err
			}
			if err := repository.CreateExternalActionRevision(ctx, revision); err != nil {
				return ExternalActionResult{}, err
			}
			item.Version++
			item.UpdatedAt = s.clock.Now().UTC()
			if err := repository.UpdateWorkItem(ctx, item, command.ExpectedVersion); err != nil {
				return ExternalActionResult{}, err
			}
			if err := s.recordActivity(ctx, repository, work.Activity{
				EntityKind: "external_action", EntityID: action.ID, WorkItemID: item.ID, ActorID: command.ActorID,
				EventType: "external_action.proposed", Summary: fmt.Sprintf("External action %s proposed", action.ActionType),
			}); err != nil {
				return ExternalActionResult{}, err
			}
			return ExternalActionResult{Action: action, Revision: revision}, nil
		})
		return err
	})
	if err != nil {
		return ExternalActionResult{}, fmt.Errorf("propose external action: %w", err)
	}
	return result, nil
}

type ReviseExternalActionCommand struct {
	ActionID                string
	ActorID                 string
	ExpectedActionVersion   int
	ExpectedWorkItemVersion int
	IdempotencyKey          string
	Subject                 json.RawMessage
}

func (s *Service) ReviseExternalAction(ctx context.Context, command ReviseExternalActionCommand) (ExternalActionResult, error) {
	var result ExternalActionResult
	err := s.store.WithinTransaction(ctx, func(repository ports.Repository) error {
		var err error
		result, err = executeIdempotently(ctx, s, repository, command.ActorID, command.IdempotencyKey, "revise_external_action", command, func() (ExternalActionResult, error) {
			if _, err := repository.Actor(ctx, command.ActorID); err != nil {
				return ExternalActionResult{}, err
			}
			action, err := repository.ExternalAction(ctx, command.ActionID)
			if err != nil {
				return ExternalActionResult{}, err
			}
			if action.Version != command.ExpectedActionVersion {
				return ExternalActionResult{}, ports.ErrVersionConflict
			}
			item, err := repository.WorkItem(ctx, action.WorkItemID)
			if err != nil {
				return ExternalActionResult{}, err
			}
			if item.Version != command.ExpectedWorkItemVersion {
				return ExternalActionResult{}, ports.ErrVersionConflict
			}
			current, err := repository.CurrentExternalActionRevision(ctx, action.ID)
			if err != nil {
				return ExternalActionResult{}, err
			}
			action, revision, err := authority.ReviseExternalAction(action, current, command.Subject, command.ActorID, s.clock.Now())
			if err != nil {
				return ExternalActionResult{}, err
			}
			if err := repository.CreateExternalActionRevision(ctx, revision); err != nil {
				return ExternalActionResult{}, err
			}
			if err := repository.UpdateExternalAction(ctx, action, command.ExpectedActionVersion); err != nil {
				return ExternalActionResult{}, err
			}
			item.Version++
			item.UpdatedAt = s.clock.Now().UTC()
			if err := repository.UpdateWorkItem(ctx, item, command.ExpectedWorkItemVersion); err != nil {
				return ExternalActionResult{}, err
			}
			if err := s.recordActivity(ctx, repository, work.Activity{
				EntityKind: "external_action", EntityID: action.ID, WorkItemID: item.ID, ActorID: command.ActorID,
				EventType: "external_action.revised", Summary: fmt.Sprintf("External action revised to %d", revision.Revision),
			}); err != nil {
				return ExternalActionResult{}, err
			}
			return ExternalActionResult{Action: action, Revision: revision}, nil
		})
		return err
	})
	if err != nil {
		return ExternalActionResult{}, fmt.Errorf("revise external action: %w", err)
	}
	return result, nil
}

type PatchExternalActionMetadataCommand struct {
	ActionID              string
	ActorID               string
	ExpectedActionVersion int
	IdempotencyKey        string
	Title                 *string
	Rationale             *string
}

func (s *Service) PatchExternalActionMetadata(ctx context.Context, command PatchExternalActionMetadataCommand) (authority.ExternalAction, error) {
	var result authority.ExternalAction
	err := s.store.WithinTransaction(ctx, func(repository ports.Repository) error {
		var err error
		result, err = executeIdempotently(ctx, s, repository, command.ActorID, command.IdempotencyKey, "patch_external_action_metadata", command, func() (authority.ExternalAction, error) {
			if _, err := repository.Actor(ctx, command.ActorID); err != nil {
				return authority.ExternalAction{}, err
			}
			action, err := repository.ExternalAction(ctx, command.ActionID)
			if err != nil {
				return authority.ExternalAction{}, err
			}
			if action.Version != command.ExpectedActionVersion {
				return authority.ExternalAction{}, ports.ErrVersionConflict
			}
			title, rationale := action.Title, action.Rationale
			if command.Title != nil {
				title = *command.Title
			}
			if command.Rationale != nil {
				rationale = *command.Rationale
			}
			action, err = authority.UpdateExternalActionMetadata(action, title, rationale, command.ActorID, s.clock.Now())
			if err != nil {
				return authority.ExternalAction{}, err
			}
			if err := repository.UpdateExternalAction(ctx, action, command.ExpectedActionVersion); err != nil {
				return authority.ExternalAction{}, err
			}
			if err := s.recordActivity(ctx, repository, work.Activity{
				EntityKind: "external_action", EntityID: action.ID, WorkItemID: action.WorkItemID, ActorID: command.ActorID,
				EventType: "external_action.metadata_changed", Summary: "External action metadata changed",
			}); err != nil {
				return authority.ExternalAction{}, err
			}
			return action, nil
		})
		return err
	})
	if err != nil {
		return authority.ExternalAction{}, fmt.Errorf("patch external action metadata: %w", err)
	}
	return result, nil
}

type RequestExternalActionApprovalCommand struct {
	ActionID              string
	ActorID               string
	ExpectedActionVersion int
	ExpectedSubjectHash   string
	IdempotencyKey        string
	ApprovedForActorID    string
	Constraints           json.RawMessage
	ExpiresAt             *time.Time
	Request               string
}

func (s *Service) RequestExternalActionApproval(ctx context.Context, command RequestExternalActionApprovalCommand) (authority.ActionApproval, error) {
	if replay, found, err := replayIdempotently[authority.ActionApproval](ctx, s, command.ActorID, command.IdempotencyKey, "request_external_action_approval", command); err != nil {
		return authority.ActionApproval{}, err
	} else if found {
		return replay, nil
	}
	id, err := s.ids.New()
	if err != nil {
		return authority.ActionApproval{}, fmt.Errorf("generate action approval id: %w", err)
	}
	var result authority.ActionApproval
	err = s.store.WithinTransaction(ctx, func(repository ports.Repository) error {
		result, err = executeIdempotently(ctx, s, repository, command.ActorID, command.IdempotencyKey, "request_external_action_approval", command, func() (authority.ActionApproval, error) {
			if _, err := repository.Actor(ctx, command.ActorID); err != nil {
				return authority.ActionApproval{}, err
			}
			if _, err := repository.Actor(ctx, command.ApprovedForActorID); err != nil {
				return authority.ActionApproval{}, err
			}
			action, err := repository.ExternalAction(ctx, command.ActionID)
			if err != nil {
				return authority.ActionApproval{}, err
			}
			if action.Version != command.ExpectedActionVersion {
				return authority.ActionApproval{}, ports.ErrVersionConflict
			}
			revision, err := repository.CurrentExternalActionRevision(ctx, action.ID)
			if err != nil {
				return authority.ActionApproval{}, err
			}
			if strings.TrimSpace(command.ExpectedSubjectHash) == "" || revision.AuthorizationSubjectHash != command.ExpectedSubjectHash {
				return authority.ActionApproval{}, errors.New("approval_stale")
			}
			approval, err := authority.NewActionApproval(authority.ActionApproval{
				ID: id, ApprovedForActorID: command.ApprovedForActorID, Constraints: command.Constraints,
				ExpiresAt: command.ExpiresAt, Request: command.Request, RequestedBy: command.ActorID,
			}, revision, s.clock.Now())
			if err != nil {
				return authority.ActionApproval{}, err
			}
			if err := repository.CreateActionApproval(ctx, approval); err != nil {
				return authority.ActionApproval{}, err
			}
			if err := s.recordActivity(ctx, repository, work.Activity{
				EntityKind: "approval", EntityID: approval.ID, WorkItemID: action.WorkItemID, ActorID: command.ActorID,
				EventType: "approval.requested", Summary: "External action approval requested",
			}); err != nil {
				return authority.ActionApproval{}, err
			}
			return approval, nil
		})
		return err
	})
	if err != nil {
		return authority.ActionApproval{}, fmt.Errorf("request external action approval: %w", err)
	}
	return result, nil
}

type ResolveExternalActionApprovalCommand struct {
	ApprovalID            string
	ActorID               string
	ExpectedActionVersion int
	IdempotencyKey        string
	Decision              authority.ApprovalStatus
	Rationale             string
}

type ApprovalResolutionResult struct {
	Approval authority.ActionApproval
	Grant    *authority.AuthorityGrant
	Action   authority.ExternalAction
}

func (s *Service) ResolveExternalActionApproval(ctx context.Context, command ResolveExternalActionApprovalCommand) (ApprovalResolutionResult, error) {
	if replay, found, err := replayIdempotently[ApprovalResolutionResult](ctx, s, command.ActorID, command.IdempotencyKey, "resolve_external_action_approval", command); err != nil {
		return ApprovalResolutionResult{}, err
	} else if found {
		return replay, nil
	}
	grantID, err := s.ids.New()
	if err != nil {
		return ApprovalResolutionResult{}, fmt.Errorf("generate authority grant id: %w", err)
	}
	var result ApprovalResolutionResult
	err = s.store.WithinTransaction(ctx, func(repository ports.Repository) error {
		result, err = executeIdempotently(ctx, s, repository, command.ActorID, command.IdempotencyKey, "resolve_external_action_approval", command, func() (ApprovalResolutionResult, error) {
			if _, err := repository.Actor(ctx, command.ActorID); err != nil {
				return ApprovalResolutionResult{}, err
			}
			approval, err := repository.ActionApproval(ctx, command.ApprovalID)
			if err != nil {
				return ApprovalResolutionResult{}, err
			}
			action, err := repository.ExternalAction(ctx, approval.ExternalActionID)
			if err != nil {
				return ApprovalResolutionResult{}, err
			}
			if action.Version != command.ExpectedActionVersion {
				return ApprovalResolutionResult{}, ports.ErrVersionConflict
			}
			revision, err := repository.CurrentExternalActionRevision(ctx, action.ID)
			if err != nil {
				return ApprovalResolutionResult{}, err
			}
			if revision.Revision != approval.ExternalActionRevision || revision.AuthorizationSubjectHash != approval.AuthorizationSubjectHash {
				return ApprovalResolutionResult{}, errors.New("approval_stale")
			}
			approval, err = authority.ResolveActionApproval(approval, command.Decision, command.ActorID, command.Rationale, s.clock.Now())
			if err != nil {
				return ApprovalResolutionResult{}, err
			}
			if err := repository.UpdateActionApproval(ctx, approval); err != nil {
				return ApprovalResolutionResult{}, err
			}
			result := ApprovalResolutionResult{Approval: approval}
			if command.Decision == authority.ApprovalApproved {
				grant, err := authority.NewAuthorityGrant(authority.AuthorityGrant{
					ID: grantID, PrincipalActorID: approval.ApprovedForActorID, Constraints: approval.Constraints,
					SourceApprovalID: approval.ID, GrantedBy: command.ActorID, ExpiresAt: approval.ExpiresAt,
				}, revision, s.clock.Now())
				if err != nil {
					return ApprovalResolutionResult{}, err
				}
				if err := repository.CreateAuthorityGrant(ctx, grant); err != nil {
					return ApprovalResolutionResult{}, err
				}
				action, err = authority.TransitionExternalAction(action, authority.ActionAuthorized, s.clock.Now())
				if err != nil {
					return ApprovalResolutionResult{}, err
				}
				action.UpdatedBy = command.ActorID
				result.Grant = &grant
			} else {
				action, err = authority.TransitionExternalAction(action, authority.ActionRejected, s.clock.Now())
				if err != nil {
					return ApprovalResolutionResult{}, err
				}
				action.UpdatedBy = command.ActorID
			}
			if err := repository.UpdateExternalAction(ctx, action, command.ExpectedActionVersion); err != nil {
				return ApprovalResolutionResult{}, err
			}
			if err := s.recordActivity(ctx, repository, work.Activity{
				EntityKind: "approval", EntityID: approval.ID, WorkItemID: action.WorkItemID, ActorID: command.ActorID,
				EventType: "approval.resolved", Summary: fmt.Sprintf("External action approval %s", approval.Status),
			}); err != nil {
				return ApprovalResolutionResult{}, err
			}
			if result.Grant != nil {
				if err := s.recordActivity(ctx, repository, work.Activity{
					EntityKind: "authority_grant", EntityID: result.Grant.ID, WorkItemID: action.WorkItemID, ActorID: command.ActorID,
					EventType: "authority_grant.created", Summary: "Authority grant created",
				}); err != nil {
					return ApprovalResolutionResult{}, err
				}
			}
			result.Action = action
			return result, nil
		})
		return err
	})
	if err != nil {
		return ApprovalResolutionResult{}, fmt.Errorf("resolve external action approval: %w", err)
	}
	return result, nil
}

type RevokeExternalActionApprovalCommand struct {
	ApprovalID            string
	ActorID               string
	ExpectedActionVersion int
	IdempotencyKey        string
	Rationale             string
}

func (s *Service) RevokeExternalActionApproval(ctx context.Context, command RevokeExternalActionApprovalCommand) (ApprovalResolutionResult, error) {
	var result ApprovalResolutionResult
	err := s.store.WithinTransaction(ctx, func(repository ports.Repository) error {
		var err error
		result, err = executeIdempotently(ctx, s, repository, command.ActorID, command.IdempotencyKey, "revoke_external_action_approval", command, func() (ApprovalResolutionResult, error) {
			if _, err := repository.Actor(ctx, command.ActorID); err != nil {
				return ApprovalResolutionResult{}, err
			}
			approval, err := repository.ActionApproval(ctx, command.ApprovalID)
			if err != nil {
				return ApprovalResolutionResult{}, err
			}
			action, err := repository.ExternalAction(ctx, approval.ExternalActionID)
			if err != nil {
				return ApprovalResolutionResult{}, err
			}
			if action.Version != command.ExpectedActionVersion {
				return ApprovalResolutionResult{}, ports.ErrVersionConflict
			}
			grant, err := repository.AuthorityGrantByApproval(ctx, approval.ID)
			if err != nil {
				return ApprovalResolutionResult{}, err
			}
			approval, err = authority.RevokeActionApproval(approval, command.ActorID, command.Rationale, s.clock.Now())
			if err != nil {
				return ApprovalResolutionResult{}, err
			}
			grant, err = authority.RevokeAuthorityGrant(grant, command.ActorID, s.clock.Now())
			if err != nil {
				return ApprovalResolutionResult{}, err
			}
			if err := repository.UpdateActionApproval(ctx, approval); err != nil {
				return ApprovalResolutionResult{}, err
			}
			if err := repository.UpdateAuthorityGrant(ctx, grant); err != nil {
				return ApprovalResolutionResult{}, err
			}
			if err := s.recordActivity(ctx, repository, work.Activity{
				EntityKind: "authority_grant", EntityID: grant.ID, WorkItemID: action.WorkItemID, ActorID: command.ActorID,
				EventType: "authority_grant.revoked", Summary: "Authority grant revoked",
			}); err != nil {
				return ApprovalResolutionResult{}, err
			}
			return ApprovalResolutionResult{Approval: approval, Grant: &grant, Action: action}, nil
		})
		return err
	})
	if err != nil {
		return ApprovalResolutionResult{}, fmt.Errorf("revoke external action approval: %w", err)
	}
	return result, nil
}

type CheckActionAuthorizationQuery struct {
	ActionID    string
	ActorID     string
	SubjectHash string
}

func (s *Service) CheckActionAuthorization(ctx context.Context, query CheckActionAuthorizationQuery) (authority.AuthorizationDecision, error) {
	var decision authority.AuthorizationDecision
	err := s.store.WithinTransaction(ctx, func(repository ports.Repository) error {
		action, err := repository.ExternalAction(ctx, query.ActionID)
		if err != nil {
			return err
		}
		revision, err := repository.CurrentExternalActionRevision(ctx, action.ID)
		if err != nil {
			return err
		}
		grant, err := repository.AuthorityGrantForPrincipal(ctx, action.ID, revision.Revision, query.ActorID)
		if err != nil {
			return err
		}
		decision = authority.CheckAuthorization(action, revision, grant, query.ActorID, query.SubjectHash, s.clock.Now())
		if !decision.Authorized {
			return nil
		}
		capabilities, err := repository.RequiredCapabilities(ctx, action.WorkItemID)
		if err != nil {
			return err
		}
		matched, err := repository.ActorHasCapabilities(ctx, query.ActorID, capabilities)
		if err != nil {
			return err
		}
		if !matched {
			decision = authority.AuthorizationDecision{Denial: &authority.AuthorizationDenial{Reason: authority.DenialCapabilityMismatch}}
		}
		return nil
	})
	if err != nil {
		return authority.AuthorizationDecision{}, fmt.Errorf("check action authorization: %w", err)
	}
	return decision, nil
}

type StartExternalActionExecutionCommand struct {
	ActionID              string
	ActorID               string
	ExpectedActionVersion int
	IdempotencyKey        string
	SubjectHash           string
	AuthorityGrantID      string
}

type ExternalActionExecutionResult struct {
	Action    authority.ExternalAction
	Execution authority.ExternalActionExecution
}

func (s *Service) StartExternalActionExecution(ctx context.Context, command StartExternalActionExecutionCommand) (ExternalActionExecutionResult, error) {
	if replay, found, err := replayIdempotently[ExternalActionExecutionResult](ctx, s, command.ActorID, command.IdempotencyKey, "start_external_action_execution", command); err != nil {
		return ExternalActionExecutionResult{}, err
	} else if found {
		return replay, nil
	}
	id, err := s.ids.New()
	if err != nil {
		return ExternalActionExecutionResult{}, fmt.Errorf("generate external action execution id: %w", err)
	}
	var result ExternalActionExecutionResult
	err = s.store.WithinTransaction(ctx, func(repository ports.Repository) error {
		result, err = executeIdempotently(ctx, s, repository, command.ActorID, command.IdempotencyKey, "start_external_action_execution", command, func() (ExternalActionExecutionResult, error) {
			action, err := repository.ExternalAction(ctx, command.ActionID)
			if err != nil {
				return ExternalActionExecutionResult{}, err
			}
			if action.Version != command.ExpectedActionVersion {
				return ExternalActionExecutionResult{}, ports.ErrVersionConflict
			}
			if action.State != authority.ActionAuthorized {
				return ExternalActionExecutionResult{}, AuthorizationError{Decision: authority.AuthorizationDecision{Denial: &authority.AuthorizationDenial{Reason: authority.DenialActionNotAuthorized}}}
			}
			revision, err := repository.CurrentExternalActionRevision(ctx, action.ID)
			if err != nil {
				return ExternalActionExecutionResult{}, err
			}
			grant, err := repository.AuthorityGrant(ctx, command.AuthorityGrantID)
			if err != nil {
				return ExternalActionExecutionResult{}, err
			}
			decision := authority.CheckAuthorization(action, revision, &grant, command.ActorID, command.SubjectHash, s.clock.Now())
			if !decision.Authorized {
				return ExternalActionExecutionResult{}, AuthorizationError{Decision: decision}
			}
			capabilities, err := repository.RequiredCapabilities(ctx, action.WorkItemID)
			if err != nil {
				return ExternalActionExecutionResult{}, err
			}
			matched, err := repository.ActorHasCapabilities(ctx, command.ActorID, capabilities)
			if err != nil {
				return ExternalActionExecutionResult{}, err
			}
			if !matched {
				return ExternalActionExecutionResult{}, AuthorizationError{Decision: authority.AuthorizationDecision{Denial: &authority.AuthorizationDenial{Reason: authority.DenialCapabilityMismatch}}}
			}
			execution, err := authority.NewExternalActionExecution(id, action, revision, command.ActorID, grant.ID, s.clock.Now())
			if err != nil {
				return ExternalActionExecutionResult{}, err
			}
			if err := repository.CreateExternalActionExecution(ctx, execution, ""); err != nil {
				return ExternalActionExecutionResult{}, err
			}
			action, err = authority.TransitionExternalAction(action, authority.ActionExecuting, s.clock.Now())
			if err != nil {
				return ExternalActionExecutionResult{}, err
			}
			action.UpdatedBy = command.ActorID
			if err := repository.UpdateExternalAction(ctx, action, command.ExpectedActionVersion); err != nil {
				return ExternalActionExecutionResult{}, err
			}
			if err := s.recordActivity(ctx, repository, work.Activity{
				EntityKind: "external_action_execution", EntityID: execution.ID, WorkItemID: action.WorkItemID, ActorID: command.ActorID,
				EventType: "external_action.execution_started", Summary: "External action execution recorded as started",
			}); err != nil {
				return ExternalActionExecutionResult{}, err
			}
			return ExternalActionExecutionResult{Action: action, Execution: execution}, nil
		})
		return err
	})
	if err != nil {
		return ExternalActionExecutionResult{}, fmt.Errorf("start external action execution: %w", err)
	}
	return result, nil
}

type CompleteExternalActionExecutionCommand struct {
	ExecutionID           string
	ActorID               string
	ExpectedActionVersion int
	IdempotencyKey        string
	State                 authority.ExecutionState
	Result                json.RawMessage
	EvidenceArtifactID    string
}

func (s *Service) CompleteExternalActionExecution(ctx context.Context, command CompleteExternalActionExecutionCommand) (ExternalActionExecutionResult, error) {
	var result ExternalActionExecutionResult
	err := s.store.WithinTransaction(ctx, func(repository ports.Repository) error {
		var err error
		result, err = executeIdempotently(ctx, s, repository, command.ActorID, command.IdempotencyKey, "complete_external_action_execution", command, func() (ExternalActionExecutionResult, error) {
			execution, err := repository.ExternalActionExecution(ctx, command.ExecutionID)
			if err != nil {
				return ExternalActionExecutionResult{}, err
			}
			if execution.PrincipalActorID != command.ActorID {
				return ExternalActionExecutionResult{}, errors.New("execution belongs to another principal")
			}
			action, err := repository.ExternalAction(ctx, execution.ExternalActionID)
			if err != nil {
				return ExternalActionExecutionResult{}, err
			}
			if action.Version != command.ExpectedActionVersion {
				return ExternalActionExecutionResult{}, ports.ErrVersionConflict
			}
			if command.EvidenceArtifactID != "" {
				artifacts, err := repository.Artifacts(ctx, action.WorkItemID)
				if err != nil {
					return ExternalActionExecutionResult{}, err
				}
				found := false
				for _, artifact := range artifacts {
					if artifact.ID == command.EvidenceArtifactID {
						found = true
						break
					}
				}
				if !found {
					return ExternalActionExecutionResult{}, errors.New("execution evidence must belong to the action work item")
				}
			}
			evidence := []string(nil)
			if command.EvidenceArtifactID != "" {
				evidence = []string{command.EvidenceArtifactID}
			}
			execution, err = authority.CompleteExternalActionExecution(execution, command.State, command.Result, evidence, s.clock.Now())
			if err != nil {
				return ExternalActionExecutionResult{}, err
			}
			if err := repository.UpdateExternalActionExecution(ctx, execution, command.EvidenceArtifactID); err != nil {
				return ExternalActionExecutionResult{}, err
			}
			target := authority.ActionFailed
			if command.State == authority.ExecutionSucceeded {
				target = authority.ActionSucceeded
			}
			action, err = authority.TransitionExternalAction(action, target, s.clock.Now())
			if err != nil {
				return ExternalActionExecutionResult{}, err
			}
			action.UpdatedBy = command.ActorID
			if err := repository.UpdateExternalAction(ctx, action, command.ExpectedActionVersion); err != nil {
				return ExternalActionExecutionResult{}, err
			}
			if err := s.recordActivity(ctx, repository, work.Activity{
				EntityKind: "external_action_execution", EntityID: execution.ID, WorkItemID: action.WorkItemID, ActorID: command.ActorID,
				EventType: "external_action.execution_recorded", Summary: fmt.Sprintf("External action execution %s recorded", command.State),
			}); err != nil {
				return ExternalActionExecutionResult{}, err
			}
			return ExternalActionExecutionResult{Action: action, Execution: execution}, nil
		})
		return err
	})
	if err != nil {
		return ExternalActionExecutionResult{}, fmt.Errorf("complete external action execution: %w", err)
	}
	return result, nil
}

type AuthorizationError struct {
	Decision authority.AuthorizationDecision
}

func (e AuthorizationError) Error() string {
	if e.Decision.Denial == nil {
		return "external action is not authorized"
	}
	return string(e.Decision.Denial.Reason)
}
