// Package mcp exposes Workgraph's application services over MCP stdio.
package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/dennisschroeder/workgraph/internal/app"
	"github.com/dennisschroeder/workgraph/internal/domain/authority"
	"github.com/dennisschroeder/workgraph/internal/domain/output"
	"github.com/dennisschroeder/workgraph/internal/domain/work"
	"github.com/dennisschroeder/workgraph/internal/ports"
)

const workspaceID = "local"

// Run serves one already-resolved workspace over MCP's stdio transport.
func Run(ctx context.Context, service *app.Service) error {
	return NewServer(service).Run(ctx, &mcp.StdioTransport{})
}

// NewServer constructs the single-workspace MCP adapter for embedding and tests.
func NewServer(service *app.Service) *mcp.Server {
	server := mcp.NewServer(&mcp.Implementation{
		Name:        "workgraph",
		Title:       "Workgraph",
		Version:     "v1",
		Description: "Authoritative local coordination state. Start with board_overview, then list_ready_items and get_item before claiming work.",
	}, &mcp.ServerOptions{Instructions: serverInstructions})
	adapter := &adapter{service: service}
	adapter.addTools(server)
	return server
}

const serverInstructions = `Use Workgraph as durable shared coordination state, not as an execution harness. Start with board_overview, list_ready_items, and get_item. Claim an item before shared work and pass the returned version to every mutation. Inspect output contracts and external actions before acting. Workgraph records external action proposals, grants, starts, results, and evidence; it never performs external effects. Use get_changes and get_objective_context to resume without hidden session state.`

type adapter struct{ service *app.Service }

func (a *adapter) addTools(server *mcp.Server) {
	a.add(server, "board_overview", "Compact orientation summary.", true, schema(), a.boardOverview)
	a.add(server, "list_ready_items", "List executable candidate work without claiming it.", true, schema("actor_id"), a.listReady)
	a.add(server, "get_item", "Retrieve structured work-item context.", true, schema("id"), a.getItem)
	a.add(server, "get_objective_context", "Retrieve deterministic, bounded objective continuation context.", true, schema("objective_id"), a.getObjectiveContext)
	a.add(server, "get_changes", "Read cursor-based activity deltas.", true, schema(), a.getChanges)
	a.add(server, "list_output_profiles", "List governed persisted output profiles.", true, schema(), a.listProfiles)
	a.add(server, "list_outputs", "Discover accepted reusable outputs.", true, schema(), a.listOutputs)
	a.add(server, "register_actor", "Register a trusted-local actor.", false, schema("actor_id", "kind", "display_name", "idempotency_key"), a.registerActor)
	a.add(server, "create_objective", "Create durable intent.", false, schema("actor_id", "key", "title", "desired_outcome", "phase"), a.createObjective)
	a.add(server, "transition_objective", "Move an objective through its governed phase lifecycle.", false, schema("objective_id", "actor_id", "target_phase", "expected_version"), a.transitionObjective)
	a.add(server, "propose_plan", "Create a proposed plan with domain-neutral work.", false, schema("objective_id", "actor_id", "title", "revision", "items"), a.proposePlan)
	a.add(server, "review_plan", "Approve or reject a proposed plan.", false, schema("plan_id", "actor_id", "decision", "reason", "expected_version"), a.reviewPlan)
	a.add(server, "claim_item", "Acquire an exclusive, expiring work lease.", false, schema("id", "actor_id", "expected_version", "idempotency_key", "lease_seconds"), a.claimItem)
	a.add(server, "append_progress", "Append a concise durable progress checkpoint.", false, schema("id", "actor_id", "expected_version", "idempotency_key", "summary"), a.appendProgress)
	a.add(server, "transition_item", "Transition a claimed work item through execution.", false, schema("id", "actor_id", "target_status", "expected_version", "idempotency_key"), a.transitionItem)
	a.add(server, "define_expected_output", "Bind work to an exact active output profile.", false, schema("work_item_id", "actor_id", "name", "profile_name", "profile_version", "expected_version", "idempotency_key"), a.defineExpectedOutput)
	a.add(server, "create_output_revision", "Create an immutable output revision with artifact references.", false, schema("expected_output_id", "actor_id", "artifacts"), a.createOutputRevision)
	a.add(server, "record_validation", "Record validation evidence and re-evaluate acceptance.", false, schema("output_revision_id", "actor_id", "criterion_ref", "validator_kind", "verdict"), a.recordValidation)
	a.add(server, "propose_external_action", "Record an external action proposal; this never executes it.", false, schema("work_item_id", "actor_id", "expected_version", "idempotency_key", "title", "authorization_subject"), a.proposeAction)
	a.add(server, "request_action_approval", "Request a principal-bound grant for the current action revision.", false, schema("action_id", "actor_id", "approved_for_actor_id", "expected_action_version", "idempotency_key", "constraints", "request"), a.requestActionApproval)
	a.add(server, "resolve_action_approval", "Approve or reject a requested external action grant.", false, schema("approval_id", "actor_id", "expected_action_version", "idempotency_key", "decision", "rationale"), a.resolveActionApproval)
	a.add(server, "check_action_authorization", "Deterministically check a principal-bound external-action grant.", true, schema("action_id", "actor_id", "subject_hash"), a.checkAuthorization)
	a.add(server, "record_external_action_execution", "Record an observed start, success, or failure; no effect is executed.", false, schema("action_id", "actor_id", "expected_action_version", "idempotency_key", "subject_hash", "authority_grant_id", "state"), a.recordExecution)
}

func (a *adapter) add(server *mcp.Server, name, description string, readOnly bool, inputSchema map[string]any, handler func(context.Context, json.RawMessage) (any, error)) {
	server.AddTool(&mcp.Tool{Name: name, Description: description, InputSchema: inputSchema, OutputSchema: outputSchema(), Annotations: &mcp.ToolAnnotations{ReadOnlyHint: readOnly}}, func(ctx context.Context, request *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		result, err := handler(ctx, request.Params.Arguments)
		cursor := int64(0)
		if err == nil {
			cursor, err = a.service.LatestActivitySequence(ctx)
		}
		return toolResult(result, err, cursor), nil
	})
}

func schema(required ...string) map[string]any {
	properties := map[string]any{}
	for _, field := range []string{
		"workspace_id", "id", "objective_id", "work_item_id", "plan_id", "approval_id", "action_id", "execution_id", "expected_output_id", "output_revision_id", "actor_id", "approved_for_actor_id", "key", "title", "description", "desired_outcome", "display_name", "kind", "phase", "target_phase", "target_status", "reason", "summary", "revision", "items", "decision", "expected_version", "expected_action_version", "idempotency_key", "lease_seconds", "transition_to_in_progress", "completed", "remaining", "discovered", "blocker", "name", "profile_name", "profile_version", "destination_hint", "ordinal", "contract", "required", "artifacts", "content_digest", "criterion_ref", "validator_kind", "verdict", "evidence_artifact_id", "details", "authorization_subject", "rationale", "constraints", "request", "subject_hash", "authority_grant_id", "state", "result", "since", "limit", "max_items_per_section", "version_constraint", "produced_by", "include", "include_attention",
	} {
		properties[field] = map[string]any{}
	}
	properties["workspace_id"] = map[string]any{"type": "string", "description": "Optional in the single-workspace server; only local is accepted."}
	return map[string]any{"type": "object", "properties": properties, "required": required, "additionalProperties": false}
}

func outputSchema() map[string]any {
	return map[string]any{"type": "object", "properties": map[string]any{
		"workspace": map[string]any{"type": "object", "properties": map[string]any{"id": map[string]any{"type": "string"}, "change_cursor": map[string]any{"type": "string"}}, "required": []string{"id", "change_cursor"}},
		"result":    map[string]any{},
	}, "required": []string{"workspace", "result"}}
}

func toolResult(result any, err error, cursor int64) *mcp.CallToolResult {
	if err != nil {
		payload := map[string]any{"error": errorPayload(err)}
		encoded, _ := json.Marshal(payload)
		return &mcp.CallToolResult{IsError: true, Content: []mcp.Content{&mcp.TextContent{Text: string(encoded)}}, StructuredContent: payload}
	}
	payload := map[string]any{"workspace": map[string]string{"id": workspaceID, "change_cursor": fmt.Sprint(cursor)}, "result": result}
	encoded, _ := json.Marshal(payload)
	return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: string(encoded)}}, StructuredContent: payload}
}

func errorPayload(err error) map[string]any {
	code := "validation_failed"
	switch {
	case errors.Is(err, ports.ErrNotFound):
		code = "not_found"
	case errors.Is(err, ports.ErrVersionConflict):
		code = "version_conflict"
	case errors.Is(err, ports.ErrClaimConflict):
		code = "claim_conflict"
	case errors.Is(err, ports.ErrIdempotencyMismatch):
		code = "idempotency_key_reused_with_different_request"
	}
	var claim app.ClaimGateError
	if errors.As(err, &claim) {
		return map[string]any{"code": "blocked", "message": err.Error(), "requirements": claim.Requirements}
	}
	var transition app.TransitionGateError
	if errors.As(err, &transition) {
		return map[string]any{"code": "transition_not_allowed", "message": err.Error(), "requirements": transition.Requirements}
	}
	var authorization app.AuthorizationError
	if errors.As(err, &authorization) {
		if authorization.Decision.Denial != nil {
			return map[string]any{"code": string(authorization.Decision.Denial.Reason), "message": err.Error(), "requirements": []any{}}
		}
	}
	if strings.Contains(err.Error(), "output profile") && strings.Contains(err.Error(), "not active") {
		code = "output_profile_inactive"
	}
	if err.Error() == "workspace_not_found" {
		code = "workspace_not_found"
	}
	return map[string]any{"code": code, "message": err.Error(), "requirements": []any{}}
}

func decode(raw json.RawMessage, target any) error {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	return validateWorkspace(workspaceFrom(target))
}

func validateWorkspace(id string) error {
	if id != "" && id != workspaceID {
		return errors.New("workspace_not_found")
	}
	return nil
}

type workspaceInput struct {
	WorkspaceID string `json:"workspace_id"`
}

func workspaceFrom(value any) string {
	if input, ok := value.(interface{ workspace() string }); ok {
		return input.workspace()
	}
	return ""
}
func (i workspaceInput) workspace() string { return i.WorkspaceID }

type listReadyInput struct {
	workspaceInput
	ActorID string `json:"actor_id"`
}

func (a *adapter) listReady(ctx context.Context, raw json.RawMessage) (any, error) {
	var in listReadyInput
	if err := decode(raw, &in); err != nil {
		return nil, err
	}
	if in.ActorID == "" {
		return a.service.ListReadyWork(ctx)
	}
	return a.service.ListReadyWorkForActor(ctx, in.ActorID)
}
func (a *adapter) boardOverview(ctx context.Context, raw json.RawMessage) (any, error) {
	var in workspaceInput
	if err := decode(raw, &in); err != nil {
		return nil, err
	}
	ready, err := a.service.ListReadyWork(ctx)
	if err != nil {
		return nil, err
	}
	cursor, err := a.service.LatestActivitySequence(ctx)
	if err != nil {
		return nil, err
	}
	return map[string]any{"change_cursor": fmt.Sprint(cursor), "counts": map[string]int{"ready": len(ready)}, "ready_high_priority": ready}, nil
}

type getItemInput struct {
	workspaceInput
	ID string `json:"id"`
}

func (a *adapter) getItem(ctx context.Context, raw json.RawMessage) (any, error) {
	var in getItemInput
	if err := decode(raw, &in); err != nil {
		return nil, err
	}
	return a.service.GetWorkItem(ctx, in.ID)
}

type objectiveContextInput struct {
	workspaceInput
	ObjectiveID string `json:"objective_id"`
	MaxItems    int    `json:"max_items_per_section"`
}

func (a *adapter) getObjectiveContext(ctx context.Context, raw json.RawMessage) (any, error) {
	var in objectiveContextInput
	if err := decode(raw, &in); err != nil {
		return nil, err
	}
	result, err := a.service.GetObjectiveContext(ctx, in.ObjectiveID)
	if err != nil {
		return nil, err
	}
	limit := in.MaxItems
	if limit <= 0 || limit > 100 {
		limit = 20
	}
	result.ContextRecords = limitTo(result.ContextRecords, limit)
	result.Plans = limitTo(result.Plans, limit)
	result.Questions = limitTo(result.Questions, limit)
	result.Decisions = limitTo(result.Decisions, limit)
	result.Approvals = limitTo(result.Approvals, limit)
	return result, nil
}

func limitTo[T any](items []T, limit int) []T {
	if len(items) > limit {
		return items[:limit]
	}
	return items
}

type changesInput struct {
	workspaceInput
	Since      string `json:"since"`
	Limit      int    `json:"limit"`
	WorkItemID string `json:"work_item_id"`
}

func (a *adapter) getChanges(ctx context.Context, raw json.RawMessage) (any, error) {
	var in changesInput
	if err := decode(raw, &in); err != nil {
		return nil, err
	}
	since := int64(0)
	if in.Since != "" {
		var err error
		since, err = strconv.ParseInt(in.Since, 10, 64)
		if err != nil || since < 0 {
			return nil, errors.New("since must be a non-negative cursor")
		}
	}
	limit := in.Limit
	if limit <= 0 || limit > 100 {
		limit = 100
	}
	changes, err := a.service.ListActivity(ctx, app.ActivityFilter{Since: since, Limit: limit + 1, WorkItemID: in.WorkItemID})
	if err != nil {
		return nil, err
	}
	hasMore := len(changes) > limit
	if hasMore {
		changes = changes[:limit]
	}
	next := since
	for _, c := range changes {
		next = c.Sequence
	}
	return map[string]any{"changes": changes, "next_cursor": fmt.Sprint(next), "has_more": hasMore}, nil
}
func (a *adapter) listProfiles(ctx context.Context, raw json.RawMessage) (any, error) {
	var in workspaceInput
	if err := decode(raw, &in); err != nil {
		return nil, err
	}
	return a.service.ListOutputProfiles(ctx)
}

type outputsInput struct {
	workspaceInput
	ProfileName string `json:"profile_name"`
	Version     string `json:"version_constraint"`
	ObjectiveID string `json:"objective_id"`
	ProducedBy  string `json:"produced_by"`
	Limit       int    `json:"limit"`
}

func (a *adapter) listOutputs(ctx context.Context, raw json.RawMessage) (any, error) {
	var in outputsInput
	if err := decode(raw, &in); err != nil {
		return nil, err
	}
	limit := in.Limit
	if limit <= 0 || limit > 100 {
		limit = 100
	}
	return a.service.ListAcceptedOutputs(ctx, app.AcceptedOutputFilter{ProfileName: in.ProfileName, VersionConstraint: in.Version, ObjectiveID: in.ObjectiveID, ProducedBy: in.ProducedBy, Limit: limit})
}

type registerActorInput struct {
	workspaceInput
	ActorID        string         `json:"actor_id"`
	Kind           work.ActorType `json:"kind"`
	DisplayName    string         `json:"display_name"`
	IdempotencyKey string         `json:"idempotency_key"`
}

func (a *adapter) registerActor(ctx context.Context, raw json.RawMessage) (any, error) {
	var in registerActorInput
	if err := decode(raw, &in); err != nil {
		return nil, err
	}
	return a.service.RegisterActor(ctx, app.RegisterActorCommand{Actor: work.Actor{ID: in.ActorID, Kind: in.Kind, DisplayName: in.DisplayName}, IdempotencyKey: in.IdempotencyKey})
}

type createObjectiveInput struct {
	workspaceInput
	ActorID        string              `json:"actor_id"`
	Key            string              `json:"key"`
	Title          string              `json:"title"`
	Description    string              `json:"description"`
	DesiredOutcome string              `json:"desired_outcome"`
	Phase          work.ObjectivePhase `json:"phase"`
}

func (a *adapter) createObjective(ctx context.Context, raw json.RawMessage) (any, error) {
	var in createObjectiveInput
	if err := decode(raw, &in); err != nil {
		return nil, err
	}
	return a.service.CreateObjective(ctx, app.CreateObjectiveCommand{ActorID: in.ActorID, Key: in.Key, Title: in.Title, Description: in.Description, DesiredOutcome: in.DesiredOutcome, Phase: in.Phase})
}

type transitionObjectiveInput struct {
	workspaceInput
	ObjectiveID     string              `json:"objective_id"`
	ActorID         string              `json:"actor_id"`
	Target          work.ObjectivePhase `json:"target_phase"`
	Reason          string              `json:"reason"`
	ExpectedVersion int                 `json:"expected_version"`
}

func (a *adapter) transitionObjective(ctx context.Context, raw json.RawMessage) (any, error) {
	var in transitionObjectiveInput
	if err := decode(raw, &in); err != nil {
		return nil, err
	}
	return a.service.TransitionObjective(ctx, app.TransitionObjectiveCommand{ObjectiveID: in.ObjectiveID, ActorID: in.ActorID, TargetPhase: in.Target, Reason: in.Reason, ExpectedVersion: in.ExpectedVersion})
}

type planInput struct {
	workspaceInput
	ObjectiveID string                 `json:"objective_id"`
	ActorID     string                 `json:"actor_id"`
	Title       string                 `json:"title"`
	Summary     string                 `json:"summary"`
	Revision    int                    `json:"revision"`
	Items       []app.ProposedWorkItem `json:"items"`
}

func (a *adapter) proposePlan(ctx context.Context, raw json.RawMessage) (any, error) {
	var in planInput
	if err := decode(raw, &in); err != nil {
		return nil, err
	}
	return a.service.ProposePlan(ctx, app.ProposePlanCommand{ObjectiveID: in.ObjectiveID, ActorID: in.ActorID, Title: in.Title, Summary: in.Summary, Revision: in.Revision, Items: in.Items})
}

type reviewPlanInput struct {
	workspaceInput
	PlanID          string              `json:"plan_id"`
	ActorID         string              `json:"actor_id"`
	Decision        work.PlanCommitment `json:"decision"`
	Reason          string              `json:"reason"`
	ExpectedVersion int                 `json:"expected_version"`
}

func (a *adapter) reviewPlan(ctx context.Context, raw json.RawMessage) (any, error) {
	var in reviewPlanInput
	if err := decode(raw, &in); err != nil {
		return nil, err
	}
	return a.service.ReviewPlan(ctx, app.ReviewPlanCommand{PlanID: in.PlanID, ReviewerActorID: in.ActorID, Decision: in.Decision, Reason: in.Reason, ExpectedVersion: in.ExpectedVersion})
}

type claimInput struct {
	workspaceInput
	ID              string `json:"id"`
	ActorID         string `json:"actor_id"`
	ExpectedVersion int    `json:"expected_version"`
	IdempotencyKey  string `json:"idempotency_key"`
	LeaseSeconds    int    `json:"lease_seconds"`
	Transition      bool   `json:"transition_to_in_progress"`
}

func (a *adapter) claimItem(ctx context.Context, raw json.RawMessage) (any, error) {
	var in claimInput
	if err := decode(raw, &in); err != nil {
		return nil, err
	}
	return a.service.ClaimWorkItem(ctx, app.ClaimWorkItemCommand{WorkItemID: in.ID, ActorID: in.ActorID, ExpectedVersion: in.ExpectedVersion, IdempotencyKey: in.IdempotencyKey, LeaseDuration: time.Duration(in.LeaseSeconds) * time.Second, TransitionToInProgress: in.Transition})
}

type progressInput struct {
	workspaceInput
	ID              string   `json:"id"`
	ActorID         string   `json:"actor_id"`
	ExpectedVersion int      `json:"expected_version"`
	IdempotencyKey  string   `json:"idempotency_key"`
	Summary         string   `json:"summary"`
	Blocker         string   `json:"blocker"`
	Completed       []string `json:"completed"`
	Remaining       []string `json:"remaining"`
	Discovered      []string `json:"discovered"`
}

func (a *adapter) appendProgress(ctx context.Context, raw json.RawMessage) (any, error) {
	var in progressInput
	if err := decode(raw, &in); err != nil {
		return nil, err
	}
	return a.service.AppendProgress(ctx, app.AppendProgressCommand{WorkItemID: in.ID, ActorID: in.ActorID, ExpectedVersion: in.ExpectedVersion, IdempotencyKey: in.IdempotencyKey, Summary: in.Summary, Completed: in.Completed, Remaining: in.Remaining, Discovered: in.Discovered, Blocker: in.Blocker})
}

type transitionItemInput struct {
	workspaceInput
	ID              string               `json:"id"`
	ActorID         string               `json:"actor_id"`
	Target          work.ExecutionStatus `json:"target_status"`
	Reason          string               `json:"reason"`
	ExpectedVersion int                  `json:"expected_version"`
	IdempotencyKey  string               `json:"idempotency_key"`
}

func (a *adapter) transitionItem(ctx context.Context, raw json.RawMessage) (any, error) {
	var in transitionItemInput
	if err := decode(raw, &in); err != nil {
		return nil, err
	}
	return a.service.TransitionWorkItem(ctx, app.TransitionWorkItemCommand{WorkItemID: in.ID, ActorID: in.ActorID, TargetStatus: in.Target, Reason: in.Reason, ExpectedVersion: in.ExpectedVersion, IdempotencyKey: in.IdempotencyKey})
}

type expectedOutputInput struct {
	workspaceInput
	WorkItemID      string          `json:"work_item_id"`
	ActorID         string          `json:"actor_id"`
	Name            string          `json:"name"`
	ProfileName     string          `json:"profile_name"`
	DestinationHint string          `json:"destination_hint"`
	IdempotencyKey  string          `json:"idempotency_key"`
	ProfileVersion  int             `json:"profile_version"`
	ExpectedVersion int             `json:"expected_version"`
	Ordinal         int             `json:"ordinal"`
	Contract        json.RawMessage `json:"contract"`
	Required        bool            `json:"required"`
}

func (a *adapter) defineExpectedOutput(ctx context.Context, raw json.RawMessage) (any, error) {
	var in expectedOutputInput
	if err := decode(raw, &in); err != nil {
		return nil, err
	}
	return a.service.DefineExpectedOutput(ctx, app.DefineExpectedOutputCommand{WorkItemID: in.WorkItemID, ActorID: in.ActorID, Name: in.Name, ProfileName: in.ProfileName, ProfileVersion: in.ProfileVersion, Contract: in.Contract, DestinationHint: in.DestinationHint, Required: in.Required, Ordinal: in.Ordinal, ExpectedVersion: in.ExpectedVersion, IdempotencyKey: in.IdempotencyKey})
}

type outputRevisionInput struct {
	workspaceInput
	ExpectedOutputID string                    `json:"expected_output_id"`
	ActorID          string                    `json:"actor_id"`
	ContentDigest    string                    `json:"content_digest"`
	Artifacts        []app.OutputArtifactInput `json:"artifacts"`
}

func (a *adapter) createOutputRevision(ctx context.Context, raw json.RawMessage) (any, error) {
	var in outputRevisionInput
	if err := decode(raw, &in); err != nil {
		return nil, err
	}
	return a.service.CreateOutputRevision(ctx, app.CreateOutputRevisionCommand{ExpectedOutputID: in.ExpectedOutputID, ActorID: in.ActorID, ContentDigest: in.ContentDigest, Artifacts: in.Artifacts})
}

type validationInput struct {
	workspaceInput
	OutputRevisionID   string                   `json:"output_revision_id"`
	ActorID            string                   `json:"actor_id"`
	CriterionRef       string                   `json:"criterion_ref"`
	ValidatorKind      output.ValidatorKind     `json:"validator_kind"`
	Verdict            output.ValidationVerdict `json:"verdict"`
	EvidenceArtifactID string                   `json:"evidence_artifact_id"`
	Details            json.RawMessage          `json:"details"`
}

func (a *adapter) recordValidation(ctx context.Context, raw json.RawMessage) (any, error) {
	var in validationInput
	if err := decode(raw, &in); err != nil {
		return nil, err
	}
	return a.service.RecordValidation(ctx, app.RecordValidationCommand{OutputRevisionID: in.OutputRevisionID, CriterionRef: in.CriterionRef, ValidatorKind: in.ValidatorKind, Verdict: in.Verdict, VerifierActorID: in.ActorID, EvidenceArtifactID: in.EvidenceArtifactID, Details: in.Details})
}

type actionInput struct {
	workspaceInput
	WorkItemID      string          `json:"work_item_id"`
	ActorID         string          `json:"actor_id"`
	IdempotencyKey  string          `json:"idempotency_key"`
	Title           string          `json:"title"`
	Rationale       string          `json:"rationale"`
	ExpectedVersion int             `json:"expected_version"`
	Required        bool            `json:"required"`
	Subject         json.RawMessage `json:"authorization_subject"`
}

func (a *adapter) proposeAction(ctx context.Context, raw json.RawMessage) (any, error) {
	var in actionInput
	if err := decode(raw, &in); err != nil {
		return nil, err
	}
	return a.service.ProposeExternalAction(ctx, app.ProposeExternalActionCommand{WorkItemID: in.WorkItemID, ActorID: in.ActorID, ExpectedVersion: in.ExpectedVersion, IdempotencyKey: in.IdempotencyKey, Required: in.Required, Title: in.Title, Rationale: in.Rationale, Subject: in.Subject})
}

type requestActionApprovalInput struct {
	workspaceInput
	ActionID              string          `json:"action_id"`
	ActorID               string          `json:"actor_id"`
	ApprovedForActorID    string          `json:"approved_for_actor_id"`
	ExpectedActionVersion int             `json:"expected_action_version"`
	IdempotencyKey        string          `json:"idempotency_key"`
	Constraints           json.RawMessage `json:"constraints"`
	Request               string          `json:"request"`
}

func (a *adapter) requestActionApproval(ctx context.Context, raw json.RawMessage) (any, error) {
	var in requestActionApprovalInput
	if err := decode(raw, &in); err != nil {
		return nil, err
	}
	return a.service.RequestExternalActionApproval(ctx, app.RequestExternalActionApprovalCommand{ActionID: in.ActionID, ActorID: in.ActorID, ApprovedForActorID: in.ApprovedForActorID, ExpectedActionVersion: in.ExpectedActionVersion, IdempotencyKey: in.IdempotencyKey, Constraints: in.Constraints, Request: in.Request})
}

type resolveActionApprovalInput struct {
	workspaceInput
	ApprovalID            string                   `json:"approval_id"`
	ActorID               string                   `json:"actor_id"`
	ExpectedActionVersion int                      `json:"expected_action_version"`
	IdempotencyKey        string                   `json:"idempotency_key"`
	Decision              authority.ApprovalStatus `json:"decision"`
	Rationale             string                   `json:"rationale"`
}

func (a *adapter) resolveActionApproval(ctx context.Context, raw json.RawMessage) (any, error) {
	var in resolveActionApprovalInput
	if err := decode(raw, &in); err != nil {
		return nil, err
	}
	return a.service.ResolveExternalActionApproval(ctx, app.ResolveExternalActionApprovalCommand{ApprovalID: in.ApprovalID, ActorID: in.ActorID, ExpectedActionVersion: in.ExpectedActionVersion, IdempotencyKey: in.IdempotencyKey, Decision: in.Decision, Rationale: in.Rationale})
}

type authorizationInput struct {
	workspaceInput
	ActionID    string `json:"action_id"`
	ActorID     string `json:"actor_id"`
	SubjectHash string `json:"subject_hash"`
}

func (a *adapter) checkAuthorization(ctx context.Context, raw json.RawMessage) (any, error) {
	var in authorizationInput
	if err := decode(raw, &in); err != nil {
		return nil, err
	}
	return a.service.CheckActionAuthorization(ctx, app.CheckActionAuthorizationQuery{ActionID: in.ActionID, ActorID: in.ActorID, SubjectHash: in.SubjectHash})
}

type executionInput struct {
	workspaceInput
	ActionID              string                   `json:"action_id"`
	ExecutionID           string                   `json:"execution_id"`
	ActorID               string                   `json:"actor_id"`
	IdempotencyKey        string                   `json:"idempotency_key"`
	SubjectHash           string                   `json:"subject_hash"`
	AuthorityGrantID      string                   `json:"authority_grant_id"`
	ExpectedActionVersion int                      `json:"expected_action_version"`
	State                 authority.ExecutionState `json:"state"`
	Result                json.RawMessage          `json:"result"`
	EvidenceArtifactID    string                   `json:"evidence_artifact_id"`
}

func (a *adapter) recordExecution(ctx context.Context, raw json.RawMessage) (any, error) {
	var in executionInput
	if err := decode(raw, &in); err != nil {
		return nil, err
	}
	if in.State == authority.ExecutionStarted {
		return a.service.StartExternalActionExecution(ctx, app.StartExternalActionExecutionCommand{ActionID: in.ActionID, ActorID: in.ActorID, ExpectedActionVersion: in.ExpectedActionVersion, IdempotencyKey: in.IdempotencyKey, SubjectHash: in.SubjectHash, AuthorityGrantID: in.AuthorityGrantID})
	}
	return a.service.CompleteExternalActionExecution(ctx, app.CompleteExternalActionExecutionCommand{ExecutionID: in.ExecutionID, ActorID: in.ActorID, ExpectedActionVersion: in.ExpectedActionVersion, IdempotencyKey: in.IdempotencyKey, State: in.State, Result: in.Result, EvidenceArtifactID: in.EvidenceArtifactID})
}
