package app

import (
	"context"

	"github.com/dennisschroeder/throughline/internal/domain/authority"
	"github.com/dennisschroeder/throughline/internal/domain/output"
	"github.com/dennisschroeder/throughline/internal/domain/work"
	"github.com/dennisschroeder/throughline/internal/ports"
)

func (s *Service) RegisterActor(ctx context.Context, command RegisterActorCommand) (Mutation[work.Actor], error) {
	ctx, capture := beginMutation(ctx)
	result, err := s.registerActorMutation(ctx, command)
	return finishMutation(capture, result, err)
}

func (s *Service) ApproveWorkItemExecution(ctx context.Context, command ApproveWorkItemExecutionCommand) (Mutation[work.ExecutionApproval], error) {
	ctx, capture := beginMutation(ctx)
	result, err := s.approveWorkItemExecutionMutation(ctx, command)
	return finishMutation(capture, result, err)
}

func (s *Service) AssignActorCapability(ctx context.Context, command AssignActorCapabilityCommand) (Mutation[ActorCapability], error) {
	ctx, capture := beginMutation(ctx)
	result, err := s.assignActorCapabilityMutation(ctx, command)
	return finishMutation(capture, result, err)
}

func (s *Service) ClaimWorkItem(ctx context.Context, command ClaimWorkItemCommand) (Mutation[ClaimResult], error) {
	ctx, capture := beginMutation(ctx)
	result, err := s.claimWorkItemMutation(ctx, command)
	return finishMutation(capture, result, err)
}

func (s *Service) RenewClaim(ctx context.Context, command RenewClaimCommand) (Mutation[ClaimResult], error) {
	ctx, capture := beginMutation(ctx)
	result, err := s.renewClaimMutation(ctx, command)
	return finishMutation(capture, result, err)
}

func (s *Service) ReleaseClaim(ctx context.Context, command ReleaseClaimCommand) (Mutation[ClaimResult], error) {
	ctx, capture := beginMutation(ctx)
	result, err := s.releaseClaimMutation(ctx, command)
	return finishMutation(capture, result, err)
}

func (s *Service) AppendProgress(ctx context.Context, command AppendProgressCommand) (Mutation[ProgressResult], error) {
	ctx, capture := beginMutation(ctx)
	result, err := s.appendProgressMutation(ctx, command)
	return finishMutation(capture, result, err)
}

func (s *Service) AttachArtifact(ctx context.Context, command AttachArtifactCommand) (Mutation[ArtifactResult], error) {
	ctx, capture := beginMutation(ctx)
	result, err := s.attachArtifactMutation(ctx, command)
	return finishMutation(capture, result, err)
}

func (s *Service) ProposeExternalAction(ctx context.Context, command ProposeExternalActionCommand) (Mutation[ExternalActionResult], error) {
	ctx, capture := beginMutation(ctx)
	result, err := s.proposeExternalActionMutation(ctx, command)
	return finishMutation(capture, result, err)
}

func (s *Service) ReviseExternalAction(ctx context.Context, command ReviseExternalActionCommand) (Mutation[ExternalActionResult], error) {
	ctx, capture := beginMutation(ctx)
	result, err := s.reviseExternalActionMutation(ctx, command)
	return finishMutation(capture, result, err)
}

func (s *Service) PatchExternalActionMetadata(ctx context.Context, command PatchExternalActionMetadataCommand) (Mutation[authority.ExternalAction], error) {
	ctx, capture := beginMutation(ctx)
	result, err := s.patchExternalActionMetadataMutation(ctx, command)
	return finishMutation(capture, result, err)
}

func (s *Service) RequestExternalActionApproval(ctx context.Context, command RequestExternalActionApprovalCommand) (Mutation[authority.ActionApproval], error) {
	ctx, capture := beginMutation(ctx)
	result, err := s.requestExternalActionApprovalMutation(ctx, command)
	return finishMutation(capture, result, err)
}

func (s *Service) ResolveExternalActionApproval(ctx context.Context, command ResolveExternalActionApprovalCommand) (Mutation[ApprovalResolutionResult], error) {
	ctx, capture := beginMutation(ctx)
	result, err := s.resolveExternalActionApprovalMutation(ctx, command)
	return finishMutation(capture, result, err)
}

func (s *Service) RevokeExternalActionApproval(ctx context.Context, command RevokeExternalActionApprovalCommand) (Mutation[ApprovalResolutionResult], error) {
	ctx, capture := beginMutation(ctx)
	result, err := s.revokeExternalActionApprovalMutation(ctx, command)
	return finishMutation(capture, result, err)
}

func (s *Service) StartExternalActionExecution(ctx context.Context, command StartExternalActionExecutionCommand) (Mutation[ExternalActionExecutionResult], error) {
	ctx, capture := beginMutation(ctx)
	result, err := s.startExternalActionExecutionMutation(ctx, command)
	return finishMutation(capture, result, err)
}

func (s *Service) CompleteExternalActionExecution(ctx context.Context, command CompleteExternalActionExecutionCommand) (Mutation[ExternalActionExecutionResult], error) {
	ctx, capture := beginMutation(ctx)
	result, err := s.completeExternalActionExecutionMutation(ctx, command)
	return finishMutation(capture, result, err)
}

func (s *Service) TransitionObjective(ctx context.Context, command TransitionObjectiveCommand) (Mutation[work.Objective], error) {
	ctx, capture := beginMutation(ctx)
	result, err := s.transitionObjectiveMutation(ctx, command)
	return finishMutation(capture, result, err)
}

func (s *Service) RecordContext(ctx context.Context, command RecordContextCommand) (Mutation[work.ContextRecord], error) {
	ctx, capture := beginMutation(ctx)
	result, err := s.recordContextMutation(ctx, command)
	return finishMutation(capture, result, err)
}

func (s *Service) TransitionContext(ctx context.Context, command TransitionContextCommand) (Mutation[work.ContextRecord], error) {
	ctx, capture := beginMutation(ctx)
	result, err := s.transitionContextMutation(ctx, command)
	return finishMutation(capture, result, err)
}

func (s *Service) AskQuestion(ctx context.Context, command AskQuestionCommand) (Mutation[work.Question], error) {
	ctx, capture := beginMutation(ctx)
	result, err := s.askQuestionMutation(ctx, command)
	return finishMutation(capture, result, err)
}

func (s *Service) AnswerQuestion(ctx context.Context, command AnswerQuestionCommand) (Mutation[work.Question], error) {
	ctx, capture := beginMutation(ctx)
	result, err := s.answerQuestionMutation(ctx, command)
	return finishMutation(capture, result, err)
}

func (s *Service) WaiveQuestion(ctx context.Context, command WaiveQuestionCommand) (Mutation[work.Question], error) {
	ctx, capture := beginMutation(ctx)
	result, err := s.waiveQuestionMutation(ctx, command)
	return finishMutation(capture, result, err)
}

func (s *Service) RecordDecision(ctx context.Context, command RecordDecisionCommand) (Mutation[work.Decision], error) {
	ctx, capture := beginMutation(ctx)
	result, err := s.recordDecisionMutation(ctx, command)
	return finishMutation(capture, result, err)
}

func (s *Service) ProposePlan(ctx context.Context, command ProposePlanCommand) (Mutation[ports.PlanContext], error) {
	ctx, capture := beginMutation(ctx)
	result, err := s.proposePlanMutation(ctx, command)
	return finishMutation(capture, result, err)
}

func (s *Service) RequestApproval(ctx context.Context, command RequestApprovalCommand) (Mutation[work.Approval], error) {
	ctx, capture := beginMutation(ctx)
	result, err := s.requestApprovalMutation(ctx, command)
	return finishMutation(capture, result, err)
}

func (s *Service) ResolveApproval(ctx context.Context, command ResolveApprovalCommand) (Mutation[work.Approval], error) {
	ctx, capture := beginMutation(ctx)
	result, err := s.resolveApprovalMutation(ctx, command)
	return finishMutation(capture, result, err)
}

func (s *Service) ReviewPlan(ctx context.Context, command ReviewPlanCommand) (Mutation[work.Plan], error) {
	ctx, capture := beginMutation(ctx)
	result, err := s.reviewPlanMutation(ctx, command)
	return finishMutation(capture, result, err)
}

func (s *Service) ProposeOutputProfile(ctx context.Context, command ProposeOutputProfileCommand) (Mutation[output.Profile], error) {
	ctx, capture := beginMutation(ctx)
	result, err := s.proposeOutputProfileMutation(ctx, command)
	return finishMutation(capture, result, err)
}

func (s *Service) ReviewOutputProfile(ctx context.Context, command ReviewOutputProfileCommand) (Mutation[output.Profile], error) {
	ctx, capture := beginMutation(ctx)
	result, err := s.reviewOutputProfileMutation(ctx, command)
	return finishMutation(capture, result, err)
}

func (s *Service) CreateObjective(ctx context.Context, command CreateObjectiveCommand) (Mutation[work.Objective], error) {
	ctx, capture := beginMutation(ctx)
	result, err := s.createObjectiveMutation(ctx, command)
	return finishMutation(capture, result, err)
}

func (s *Service) PatchObjective(ctx context.Context, command PatchObjectiveCommand) (Mutation[work.Objective], error) {
	ctx, capture := beginMutation(ctx)
	result, err := s.patchObjectiveMutation(ctx, command)
	return finishMutation(capture, result, err)
}

func (s *Service) PatchWorkItem(ctx context.Context, command PatchWorkItemCommand) (Mutation[work.WorkItem], error) {
	ctx, capture := beginMutation(ctx)
	result, err := s.patchWorkItemMutation(ctx, command)
	return finishMutation(capture, result, err)
}

func (s *Service) RequestAttention(ctx context.Context, command RequestAttentionCommand) (Mutation[AttentionRequestResult], error) {
	ctx, capture := beginMutation(ctx)
	result, err := s.requestAttentionMutation(ctx, command)
	return finishMutation(capture, result, err)
}

func (s *Service) CreatePlan(ctx context.Context, command CreatePlanCommand) (Mutation[work.Plan], error) {
	ctx, capture := beginMutation(ctx)
	result, err := s.createPlanMutation(ctx, command)
	return finishMutation(capture, result, err)
}

func (s *Service) CreateWorkItem(ctx context.Context, command CreateWorkItemCommand) (Mutation[work.WorkItem], error) {
	ctx, capture := beginMutation(ctx)
	result, err := s.createWorkItemMutation(ctx, command)
	return finishMutation(capture, result, err)
}

func (s *Service) DefineExpectedOutput(ctx context.Context, command DefineExpectedOutputCommand) (Mutation[output.ExpectedOutput], error) {
	ctx, capture := beginMutation(ctx)
	result, err := s.defineExpectedOutputMutation(ctx, command)
	return finishMutation(capture, result, err)
}

func (s *Service) TransitionWorkItem(ctx context.Context, command TransitionWorkItemCommand) (Mutation[work.WorkItem], error) {
	ctx, capture := beginMutation(ctx)
	result, err := s.transitionWorkItemMutation(ctx, command)
	return finishMutation(capture, result, err)
}

func (s *Service) ResolveAcceptanceCriterion(ctx context.Context, command ResolveAcceptanceCriterionCommand) (Mutation[work.AcceptanceCriterion], error) {
	ctx, capture := beginMutation(ctx)
	result, err := s.resolveAcceptanceCriterionMutation(ctx, command)
	return finishMutation(capture, result, err)
}

func (s *Service) BlockWorkItem(ctx context.Context, command BlockWorkItemCommand) (Mutation[work.ManualBlocker], error) {
	ctx, capture := beginMutation(ctx)
	result, err := s.blockWorkItemMutation(ctx, command)
	return finishMutation(capture, result, err)
}

func (s *Service) UnblockWorkItem(ctx context.Context, command UnblockWorkItemCommand) (Mutation[work.ManualBlocker], error) {
	ctx, capture := beginMutation(ctx)
	result, err := s.unblockWorkItemMutation(ctx, command)
	return finishMutation(capture, result, err)
}

func (s *Service) LinkDependency(ctx context.Context, command LinkDependencyCommand) (Mutation[work.Dependency], error) {
	ctx, capture := beginMutation(ctx)
	result, err := s.linkDependencyMutation(ctx, command)
	return finishMutation(capture, result, err)
}

func (s *Service) UnlinkDependency(ctx context.Context, command UnlinkDependencyCommand) (Mutation[work.WorkItem], error) {
	ctx, capture := beginMutation(ctx)
	result, err := s.unlinkDependencyMutation(ctx, command)
	return finishMutation(capture, result, err)
}

func (s *Service) CreateOutputRevision(ctx context.Context, command CreateOutputRevisionCommand) (Mutation[output.OutputRevision], error) {
	ctx, capture := beginMutation(ctx)
	result, err := s.createOutputRevisionMutation(ctx, command)
	return finishMutation(capture, result, err)
}

func (s *Service) RecordValidation(ctx context.Context, command RecordValidationCommand) (Mutation[output.OutputRevision], error) {
	ctx, capture := beginMutation(ctx)
	result, err := s.recordValidationMutation(ctx, command)
	return finishMutation(capture, result, err)
}

func (s *Service) AddOutputRequirement(ctx context.Context, command AddOutputRequirementCommand) (Mutation[output.OutputRequirement], error) {
	ctx, capture := beginMutation(ctx)
	result, err := s.addOutputRequirementMutation(ctx, command)
	return finishMutation(capture, result, err)
}
