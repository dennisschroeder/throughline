// Package mcp exposes Throughline's application services over MCP stdio.
package mcp

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/google/jsonschema-go/jsonschema"
	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/dennisschroeder/throughline/internal/app"
	"github.com/dennisschroeder/throughline/internal/domain/authority"
	"github.com/dennisschroeder/throughline/internal/domain/output"
	"github.com/dennisschroeder/throughline/internal/domain/work"
	"github.com/dennisschroeder/throughline/internal/ports"
	"github.com/dennisschroeder/throughline/internal/semanticmodel"
)

const workspaceID = "local"

// Run serves one already-resolved workspace over MCP's stdio transport.
func Run(ctx context.Context, service *app.Service) error {
	return NewServer(service).Run(ctx, &mcp.StdioTransport{})
}

// NewServer constructs the single-workspace MCP adapter for embedding and tests.
func NewServer(service *app.Service) *mcp.Server {
	instructions := serverInstructions
	if model, err := semanticmodel.Load(); err == nil {
		if builtInstructions, instructionErr := semanticInstructions(model); instructionErr == nil {
			instructions = builtInstructions
		}
	}
	server := mcp.NewServer(&mcp.Implementation{
		Name:        "throughline",
		Title:       "Throughline",
		Version:     "v1",
		Description: "Authoritative local coordination state. Start with board_overview, then list_ready_items and get_item before claiming work.",
	}, &mcp.ServerOptions{Instructions: instructions})
	adapter := &adapter{service: service}
	adapter.addTools(server)
	return server
}

const maxServerInstructionsBytes = 2048

const serverInstructions = `Use Throughline as durable shared coordination state, not as an execution harness. Start with board_overview, list_ready_items, and get_item. Claim an item before shared work and pass the returned version to every mutation. Inspect output contracts and external actions before acting. Throughline records external action proposals, grants, starts, results, and evidence; Throughline never performs external effects. Use get_changes and get_objective_context to resume without hidden session state.`

func semanticInstructions(model *semanticmodel.Model) (string, error) {
	instructions := fmt.Sprintf("%s Semantic model %s (%s). Work/output chain: WorkItem -> ExpectedOutput -> OutputRevision -> ValidationRecord -> accepted/reusable output. Authority chain: ExternalAction revision -> AuthorizationSubject -> principal-bound AuthorityGrant -> recorded execution evidence. Call get_semantic_model for details. %s", model.Bootstrap, model.ModelVersion, model.ContentDigest, serverInstructions)
	if len(instructions) > maxServerInstructionsBytes {
		return "", fmt.Errorf("semantic model instructions exceed %d bytes", maxServerInstructionsBytes)
	}
	return instructions, nil
}

type adapter struct{ service *app.Service }

func (a *adapter) addTools(server *mcp.Server) {
	a.add(server, "board_overview", "Compact orientation summary.", true, schemaFor[boardOverviewInput](), a.boardOverview)
	a.add(server, "list_items", "List structured work-item summaries.", true, schemaFor[listItemsInput](), a.listItems)
	a.add(server, "list_ready_items", "List executable candidate work without claiming it.", true, schemaFor[listReadyInput]("actor_id"), a.listReady)
	a.add(server, "get_item", "Retrieve structured work-item context.", true, schemaFor[getItemInput]("id"), a.getItem)
	a.add(server, "get_objective_context", "Retrieve deterministic, bounded objective continuation context.", true, schemaFor[objectiveContextInput]("objective_id"), a.getObjectiveContext)
	a.add(server, "get_changes", "Read cursor-based activity deltas.", true, schemaFor[changesInput](), a.getChanges)
	a.add(server, "get_semantic_model", "Read the embedded Throughline semantic model.", true, semanticModelSchema(), a.getSemanticModel)
	a.add(server, "list_output_profiles", "List governed persisted output profiles.", true, schemaFor[workspaceInput](), a.listProfiles)
	a.add(server, "get_output_profile", "Read one exact governed output profile version.", true, schemaFor[outputProfileInput]("profile_name", "profile_version"), a.getProfile)
	a.add(server, "list_outputs", "Discover accepted reusable outputs.", true, schemaFor[outputsInput](), a.listOutputs)
	a.add(server, "register_actor", "Register a trusted-local actor.", false, schemaFor[registerActorInput]("actor_id", "kind", "display_name", "idempotency_key"), a.registerActor)
	a.add(server, "create_objective", "Create durable intent.", false, schemaFor[createObjectiveInput]("actor_id", "idempotency_key", "key", "title", "desired_outcome", "phase"), a.createObjective)
	a.add(server, "patch_objective", "Update safe objective details with optimistic concurrency.", false, schemaFor[patchObjectiveInput]("objective_id", "actor_id", "idempotency_key", "expected_version"), a.patchObjective)
	a.add(server, "create_item", "Create one proposed domain-neutral work item.", false, schemaFor[createItemInput]("actor_id", "idempotency_key", "key", "objective_id", "title", "kind"), a.createItem)
	a.add(server, "patch_item", "Update safe work-item details with optimistic concurrency.", false, patchItemSchema(), a.patchItem)
	a.add(server, "request_attention", "Request an orthogonal human attention state for a governed target.", false, requestAttentionSchema(), a.requestAttention)
	a.add(server, "request_approval", "Request an approval for a governed target.", false, requestApprovalSchema(), a.requestApproval)
	a.add(server, "resolve_approval", "Resolve or revoke a requested approval.", false, resolveApprovalSchema(), a.resolveApproval)
	a.add(server, "block_item", "Create a persisted manual blocker.", false, schemaFor[blockItemInput]("work_item_id", "actor_id", "idempotency_key", "expected_version", "reason"), a.blockItem)
	a.add(server, "unblock_item", "Resolve a persisted manual blocker.", false, schemaFor[unblockItemInput]("blocker_id", "actor_id", "idempotency_key", "expected_version", "resolution"), a.unblockItem)
	a.add(server, "transition_objective", "Move an objective through its governed phase lifecycle.", false, schemaFor[transitionObjectiveInput]("objective_id", "actor_id", "target_phase", "expected_version", "idempotency_key"), a.transitionObjective)
	a.add(server, "propose_plan", "Create a proposed plan with domain-neutral work.", false, schemaFor[planInput]("objective_id", "actor_id", "idempotency_key", "title", "items"), a.proposePlan)
	a.add(server, "review_plan", "Approve or reject a proposed plan.", false, schemaFor[reviewPlanInput]("plan_id", "actor_id", "idempotency_key", "decision", "reason", "expected_version"), a.reviewPlan)
	a.add(server, "record_context", "Record typed objective or work-item context.", false, schemaFor[recordContextInput]("objective_id", "actor_id", "idempotency_key", "kind", "title", "status"), a.recordContext)
	a.add(server, "record_decision", "Record a durable accepted decision.", false, schemaFor[recordDecisionInput]("objective_id", "actor_id", "idempotency_key", "title", "decision"), a.recordDecision)
	a.add(server, "ask_question", "Record a durable open question.", false, schemaFor[askQuestionInput]("objective_id", "actor_id", "idempotency_key", "question"), a.askQuestion)
	a.add(server, "answer_question", "Answer or waive an open question.", false, schemaFor[answerQuestionInput]("question_id", "actor_id", "idempotency_key", "expected_version"), a.answerQuestion)
	a.add(server, "propose_output_profile", "Propose a governed immutable output profile version.", false, schemaFor[proposeOutputProfileInput]("actor_id", "idempotency_key", "name", "version", "structure", "semantics", "validation"), a.proposeOutputProfile)
	a.add(server, "review_output_profile", "Activate or reject a proposed output profile.", false, schemaFor[reviewOutputProfileInput]("profile_id", "actor_id", "idempotency_key", "expected_version", "decision", "reason"), a.reviewOutputProfile)
	a.add(server, "renew_claim", "Renew an owned work lease.", false, schemaFor[claimRenewInput]("work_item_id", "claim_id", "actor_id", "expected_version", "idempotency_key", "lease_seconds"), a.renewClaim)
	a.add(server, "release_item", "Release an owned work lease.", false, schemaFor[claimReleaseInput]("work_item_id", "claim_id", "actor_id", "expected_version", "idempotency_key", "reason"), a.releaseClaim)
	a.add(server, "claim_item", "Acquire an exclusive, expiring work lease.", false, schemaFor[claimInput]("id", "actor_id", "expected_version", "idempotency_key", "lease_seconds"), a.claimItem)
	a.add(server, "append_progress", "Append a concise durable progress checkpoint.", false, schemaFor[progressInput]("id", "actor_id", "expected_version", "idempotency_key", "summary"), a.appendProgress)
	a.add(server, "transition_item", "Transition a claimed work item through execution.", false, schemaFor[transitionItemInput]("id", "actor_id", "target_status", "expected_version", "idempotency_key"), a.transitionItem)
	a.add(server, "define_expected_output", "Bind work to an exact active output profile.", false, schemaFor[expectedOutputInput]("work_item_id", "actor_id", "name", "profile_name", "profile_version", "expected_version", "idempotency_key"), a.defineExpectedOutput)
	a.add(server, "create_output_revision", "Create an immutable output revision with artifact references.", false, schemaFor[outputRevisionInput]("expected_output_id", "actor_id", "idempotency_key", "artifacts"), a.createOutputRevision)
	a.add(server, "record_validation", "Record validation evidence and re-evaluate acceptance.", false, schemaFor[validationInput]("output_revision_id", "actor_id", "idempotency_key", "criterion_ref", "validator_kind", "verdict"), a.recordValidation)
	a.add(server, "add_output_requirement", "Require an accepted reusable output before work is ready.", false, schemaFor[outputRequirementInput]("work_item_id", "actor_id", "expected_version", "idempotency_key"), a.addOutputRequirement)
	a.add(server, "attach_artifact", "Attach an immutable external reference to work.", false, schemaFor[artifactInput]("work_item_id", "actor_id", "expected_version", "idempotency_key", "kind", "uri"), a.attachArtifact)
	a.add(server, "link_dependency", "Link a typed dependency within one objective.", false, schemaFor[dependencyInput]("work_item_id", "depends_on_work_item_id", "actor_id", "expected_version", "idempotency_key", "kind"), a.linkDependency)
	a.add(server, "unlink_dependency", "Remove a typed dependency within one objective.", false, schemaFor[unlinkDependencyInput]("work_item_id", "depends_on_work_item_id", "actor_id", "expected_version", "idempotency_key", "kind"), a.unlinkDependency)
	a.add(server, "revise_external_action", "Create the next immutable authorization-subject revision.", false, schemaFor[reviseActionInput]("action_id", "actor_id", "expected_action_version", "expected_work_item_version", "idempotency_key", "authorization_subject"), a.reviseAction)
	a.add(server, "propose_external_action", "Record an external action proposal; this never executes it.", false, actionSchema(), a.proposeAction)
	a.add(server, "patch_external_action_metadata", "Update non-authorizing external action metadata without revising its subject.", false, schemaFor[patchActionMetadataInput]("action_id", "actor_id", "expected_action_version", "idempotency_key"), a.patchActionMetadata)
	a.add(server, "request_action_approval", "Request a principal-bound grant for the current action revision.", false, schemaFor[requestActionApprovalInput]("action_id", "actor_id", "approved_for_actor_id", "expected_action_version", "authorization_subject_hash", "idempotency_key", "constraints", "request"), a.requestActionApproval)
	a.add(server, "resolve_action_approval", "Approve or reject a requested external action grant.", false, schemaFor[resolveActionApprovalInput]("approval_id", "actor_id", "expected_action_version", "idempotency_key", "decision", "rationale"), a.resolveActionApproval)
	a.add(server, "check_action_authorization", "Deterministically check a principal-bound external-action grant.", true, schemaFor[authorizationInput]("action_id", "actor_id", "subject_hash"), a.checkAuthorization)
	a.add(server, "record_external_action_execution", "Record an observed start, success, or failure; no effect is executed.", false, executionSchema(), a.recordExecution)
}

func (a *adapter) add(server *mcp.Server, name, description string, readOnly bool, inputSchema map[string]any, handler func(context.Context, json.RawMessage) (any, error)) {
	server.AddTool(&mcp.Tool{Name: name, Description: description, InputSchema: inputSchema, OutputSchema: outputSchema(name), Annotations: &mcp.ToolAnnotations{ReadOnlyHint: readOnly}}, func(ctx context.Context, request *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		if err := validateToolInput(request.Params.Arguments, inputSchema); err != nil {
			return toolErrorResult(a.errorPayload(ctx, err, request.Params.Arguments)), nil
		}
		result, err := handler(ctx, request.Params.Arguments)
		if err != nil {
			return toolErrorResult(a.errorPayload(ctx, err, request.Params.Arguments)), nil
		}
		cursor := int64(0)
		if !readOnly {
			cursor, err = a.idempotencyCursor(ctx, request.Params.Arguments)
		} else {
			cursor, err = a.service.LatestActivitySequence(ctx)
		}
		if err != nil {
			return toolErrorResult(a.errorPayload(ctx, err, request.Params.Arguments)), nil
		}
		normalized := snakeCaseValue(result)
		output := map[string]any{"workspace": map[string]any{"id": workspaceID, "change_cursor": fmt.Sprint(cursor)}, "result": normalized}
		schema := outputSchema(name)
		if err := validateJSONSchema(output, schema, schema, "output"); err != nil {
			return toolErrorResult(map[string]any{"code": "output_validation_failed", "message": err.Error(), "requirements": []any{}}), nil
		}
		return toolResult(normalized, cursor), nil
	})
}

func (a *adapter) idempotencyCursor(ctx context.Context, raw json.RawMessage) (int64, error) {
	var input struct {
		ActorID        string `json:"actor_id"`
		IdempotencyKey string `json:"idempotency_key"`
	}
	if err := json.Unmarshal(raw, &input); err != nil {
		return 0, err
	}
	return a.service.IdempotencyCursor(ctx, input.ActorID, input.IdempotencyKey)
}

func validateToolInput(raw json.RawMessage, schema map[string]any) error {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	var value any
	if err := decoder.Decode(&value); err != nil {
		return errors.New("tool input must be a JSON object")
	}
	if _, ok := value.(map[string]any); !ok {
		return errors.New("tool input must be a JSON object")
	}
	return validateJSONSchema(value, schema, schema, "input")
}

func validateJSONSchema(value any, schema, root map[string]any, path string) error {
	if reference, _ := schema["$ref"].(string); reference != "" {
		const prefix = "#/$defs/"
		if !strings.HasPrefix(reference, prefix) {
			return fmt.Errorf("unsupported schema reference %q", reference)
		}
		definitions, _ := root["$defs"].(map[string]any)
		resolved, ok := definitions[strings.TrimPrefix(reference, prefix)].(map[string]any)
		if !ok {
			return fmt.Errorf("missing schema reference %q", reference)
		}
		return validateJSONSchema(value, resolved, root, path)
	}
	if alternatives, ok := schema["allOf"].([]any); ok {
		for _, alternative := range alternatives {
			if candidate, ok := alternative.(map[string]any); ok {
				if err := validateJSONSchema(value, candidate, root, path); err != nil {
					return err
				}
			}
		}
	}
	if alternatives, ok := schema["oneOf"].([]any); ok && schema["x-runtime-branch"] != true {
		matches := 0
		for _, alternative := range alternatives {
			candidate, ok := alternative.(map[string]any)
			if ok && validateJSONSchema(value, candidate, root, path) == nil {
				matches++
			}
		}
		if matches != 1 {
			return fmt.Errorf("%s must match exactly one schema branch", path)
		}
	}
	if alternatives, ok := schema["anyOf"].([]any); ok {
		for _, alternative := range alternatives {
			candidate, ok := alternative.(map[string]any)
			if ok && validateJSONSchema(value, candidate, root, path) == nil {
				goto anyOfMatched
			}
		}
		return fmt.Errorf("%s must match a schema branch", path)
	anyOfMatched:
	}
	if forbidden, ok := schema["not"].(map[string]any); ok && validateJSONSchema(value, forbidden, root, path) == nil {
		return fmt.Errorf("%s contains a forbidden field combination", path)
	}
	if constant, ok := schema["const"]; ok && fmt.Sprint(value) != fmt.Sprint(constant) {
		return fmt.Errorf("%s must equal %v", path, constant)
	}
	if values, ok := schema["enum"].([]any); ok {
		matched := false
		for _, candidate := range values {
			if fmt.Sprint(value) == fmt.Sprint(candidate) {
				matched = true
				break
			}
		}
		if !matched {
			return fmt.Errorf("%s must be one of the documented values", path)
		}
	}
	if kind, _ := schema["type"].(string); kind != "" && !matchesJSONType(value, kind) {
		return fmt.Errorf("%s must be a %s", path, kind)
	}
	object, isObject := value.(map[string]any)
	if isObject {
		properties, _ := schema["properties"].(map[string]any)
		for _, field := range schemaRequired(schema) {
			candidate, ok := object[field]
			if !ok || (candidate == nil && strings.HasPrefix(path, "input")) {
				return fmt.Errorf("%s is missing required field %q", path, field)
			}
			if strings.HasPrefix(path, "input") {
				text, ok := candidate.(string)
				if ok && strings.TrimSpace(text) == "" {
					return fmt.Errorf("%s required field %q cannot be empty", path, field)
				}
			}
		}
		for field, candidate := range object {
			child, exists := properties[field]
			if !exists {
				if strict, configured := schema["additionalProperties"].(bool); configured && !strict {
					return fmt.Errorf("%s has unknown field %q", path, field)
				}
				continue
			}
			if childSchema, ok := child.(map[string]any); ok {
				if err := validateJSONSchema(candidate, childSchema, root, path+"."+field); err != nil {
					return err
				}
			}
		}
	}
	if values, ok := value.([]any); ok {
		if itemSchema, ok := schema["items"].(map[string]any); ok {
			for index, candidate := range values {
				if err := validateJSONSchema(candidate, itemSchema, root, fmt.Sprintf("%s[%d]", path, index)); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

func schemaRequired(schema map[string]any) []string {
	values, _ := schema["required"].([]any)
	result := make([]string, 0, len(values))
	for _, value := range values {
		if field, ok := value.(string); ok {
			result = append(result, field)
		}
	}
	if strings, ok := schema["required"].([]string); ok {
		return strings
	}
	return result
}

func matchesJSONType(value any, kind string) bool {
	switch kind {
	case "object":
		_, ok := value.(map[string]any)
		return ok
	case "array":
		_, ok := value.([]any)
		return ok
	case "string":
		_, ok := value.(string)
		return ok
	case "boolean":
		_, ok := value.(bool)
		return ok
	case "number":
		if _, ok := value.(json.Number); ok {
			return true
		}
		_, ok := value.(float64)
		return ok
	case "integer":
		if number, ok := value.(json.Number); ok {
			_, err := strconv.ParseInt(number.String(), 10, 64)
			return err == nil
		}
		if number, ok := value.(float64); ok {
			return math.Trunc(number) == number
		}
		return false
	case "null":
		return value == nil
	default:
		return true
	}
}

func schemaFor[T any](required ...string) map[string]any {
	schema, err := jsonschema.For[T](nil)
	if err != nil {
		panic(fmt.Sprintf("derive MCP schema: %v", err))
	}
	schema.Required = required
	encoded, err := json.Marshal(schema)
	if err != nil {
		panic(fmt.Sprintf("encode MCP schema: %v", err))
	}
	var result map[string]any
	if err := json.Unmarshal(encoded, &result); err != nil {
		panic(fmt.Sprintf("decode MCP schema: %v", err))
	}
	return strictGovernedSchemas(result)
}

func outputSchema(name string) map[string]any {
	return map[string]any{"type": "object", "properties": map[string]any{
		"workspace": map[string]any{"type": "object", "properties": map[string]any{"id": map[string]any{"type": "string"}, "change_cursor": map[string]any{"type": "string"}}, "required": []string{"id", "change_cursor"}, "additionalProperties": false},
		"result":    resultSchema(name),
	}, "required": []string{"workspace", "result"}, "additionalProperties": false}
}

func resultSchema(name string) map[string]any {
	switch name {
	case "board_overview":
		return schemaForResult[boardOverviewResult]()
	case "list_items":
		return schemaForResult[listItemsResult]()
	case "list_ready_items":
		return schemaForResult[[]ports.ReadyWorkItem]()
	case "get_item":
		return schemaForResult[getItemResult]()
	case "get_objective_context":
		return schemaForResult[app.ObjectiveContextSnapshot]()
	case "get_changes":
		return schemaForResult[changesResult]()
	case "get_semantic_model":
		return semanticModelResultSchema()
	case "list_output_profiles":
		return schemaForResult[[]output.Profile]()
	case "get_output_profile", "propose_output_profile", "review_output_profile":
		return schemaForResult[output.Profile]()
	case "list_outputs":
		return schemaForResult[[]ports.AcceptedOutput]()
	case "register_actor":
		return schemaForResult[work.Actor]()
	case "create_objective", "patch_objective", "transition_objective":
		return schemaForResult[work.Objective]()
	case "create_item", "patch_item", "transition_item", "unlink_dependency":
		return schemaForResult[work.WorkItem]()
	case "request_attention":
		return schemaForResult[app.AttentionRequestResult]()
	case "request_approval":
		return oneOf(genericApprovalResultSchema(), actionApprovalResultSchema())
	case "resolve_approval":
		return oneOf(schemaForResult[work.Approval](), schemaForResult[app.ApprovalResolutionResult](), schemaForResult[authority.ActionApproval]())
	case "resolve_action_approval":
		return schemaForResult[app.ApprovalResolutionResult]()
	case "block_item", "unblock_item":
		return schemaForResult[work.ManualBlocker]()
	case "propose_plan":
		return schemaForResult[ports.PlanContext]()
	case "review_plan":
		return schemaForResult[work.Plan]()
	case "record_context":
		return schemaForResult[work.ContextRecord]()
	case "record_decision":
		return schemaForResult[work.Decision]()
	case "ask_question", "answer_question":
		return schemaForResult[work.Question]()
	case "renew_claim", "release_item", "claim_item":
		return schemaForResult[app.ClaimResult]()
	case "append_progress":
		return schemaForResult[app.ProgressResult]()
	case "define_expected_output":
		return schemaForResult[output.ExpectedOutput]()
	case "create_output_revision", "record_validation":
		return schemaForResult[output.OutputRevision]()
	case "add_output_requirement":
		return schemaForResult[output.OutputRequirement]()
	case "attach_artifact":
		return schemaForResult[app.ArtifactResult]()
	case "link_dependency":
		return schemaForResult[work.Dependency]()
	case "revise_external_action", "propose_external_action":
		return schemaForResult[app.ExternalActionResult]()
	case "patch_external_action_metadata":
		return schemaForResult[authority.ExternalAction]()
	case "request_action_approval":
		return schemaForResult[authority.ActionApproval]()
	case "check_action_authorization":
		return schemaForResult[authority.AuthorizationDecision]()
	case "record_external_action_execution":
		return schemaForResult[app.ExternalActionExecutionResult]()
	default:
		panic("missing MCP result schema for " + name)
	}
}

func genericApprovalResultSchema() map[string]any {
	return map[string]any{"type": "object", "properties": map[string]any{
		"id":                 map[string]any{"type": "string"},
		"objective_id":       map[string]any{"type": "string"},
		"plan_id":            map[string]any{"type": "string"},
		"work_item_id":       map[string]any{"type": "string"},
		"output_profile_id":  map[string]any{"type": "string"},
		"output_revision_id": map[string]any{"type": "string"},
		"request":            map[string]any{"type": "string"},
		"status":             map[string]any{"type": "string"},
		"version":            map[string]any{"type": "integer"},
		"requested_by":       map[string]any{"type": "string"},
		"requested_at":       map[string]any{"type": "string"},
		"resolved_by":        map[string]any{"type": "string"},
		"resolved_at":        map[string]any{"type": "string"},
		"rationale":          map[string]any{"type": "string"},
	}, "required": []string{"id", "objective_id", "plan_id", "work_item_id", "output_profile_id", "output_revision_id", "request", "status", "version", "requested_by", "requested_at", "resolved_by", "resolved_at", "rationale"}, "additionalProperties": false}
}

func actionApprovalResultSchema() map[string]any {
	return map[string]any{"type": "object", "properties": map[string]any{
		"id":                         map[string]any{"type": "string"},
		"external_action_id":         map[string]any{"type": "string"},
		"external_action_revision":   map[string]any{"type": "integer"},
		"approved_for_actor_id":      map[string]any{"type": "string"},
		"authorization_subject_hash": map[string]any{"type": "string"},
		"constraints":                governedSchemaMust("constraints"),
		"expires_at":                 map[string]any{"type": []string{"string", "null"}},
		"request":                    map[string]any{"type": "string"},
		"status":                     map[string]any{"type": "string"},
		"requested_by":               map[string]any{"type": "string"},
		"requested_at":               map[string]any{"type": "string"},
		"resolved_by":                map[string]any{"type": "string"},
		"resolved_at":                map[string]any{"type": []string{"string", "null"}},
		"rationale":                  map[string]any{"type": "string"},
	}, "required": []string{"id", "external_action_id", "external_action_revision", "approved_for_actor_id", "authorization_subject_hash", "constraints", "expires_at", "request", "status", "requested_by", "requested_at", "resolved_by", "resolved_at", "rationale"}, "additionalProperties": false}
}

func schemaForResult[T any]() map[string]any {
	schema, err := jsonschema.For[T](nil)
	if err != nil {
		panic(fmt.Sprintf("derive MCP result schema: %v", err))
	}
	encoded, err := json.Marshal(schema)
	if err != nil {
		panic(fmt.Sprintf("encode MCP result schema: %v", err))
	}
	var result map[string]any
	if err := json.Unmarshal(encoded, &result); err != nil {
		panic(fmt.Sprintf("decode MCP result schema: %v", err))
	}
	return strictGovernedSchemas(optionalOutputSchema(snakeCaseSchema(result)))
}

func strictGovernedSchemas(schema map[string]any) map[string]any {
	result := make(map[string]any, len(schema))
	for key, value := range schema {
		switch typed := value.(type) {
		case map[string]any:
			if key == "properties" {
				properties := make(map[string]any, len(typed))
				for name, property := range typed {
					if governed, ok := governedSchema(name); ok {
						properties[name] = governed
						continue
					}
					if nested, ok := property.(map[string]any); ok {
						properties[name] = strictGovernedSchemas(nested)
					} else {
						properties[name] = property
					}
				}
				result[key] = properties
			} else {
				result[key] = strictGovernedSchemas(typed)
			}
		case []any:
			values := make([]any, len(typed))
			for index, candidate := range typed {
				if nested, ok := candidate.(map[string]any); ok {
					values[index] = strictGovernedSchemas(nested)
				} else {
					values[index] = candidate
				}
			}
			result[key] = values
		default:
			result[key] = value
		}
	}
	return result
}

func governedSchema(name string) (map[string]any, bool) {
	object := func(properties map[string]any, required ...string) map[string]any {
		result := map[string]any{"type": "object", "properties": properties, "additionalProperties": false}
		if len(required) > 0 {
			result["required"] = required
		}
		return result
	}
	stringArray := map[string]any{"type": "array", "items": map[string]any{"type": "string"}}
	switch name {
	case "authorization_subject":
		return object(map[string]any{"action_type": map[string]any{"type": "string"}, "target": governedTargetSchema(), "arguments": map[string]any{"type": "array", "items": object(map[string]any{"name": map[string]any{"type": "string"}, "value": map[string]any{"type": "string"}}, "name", "value")}, "scope": governedScopeSchema(), "permissions": stringArray, "credential_requirements": stringArray, "constraints": governedConstraintsSchema()}, "action_type", "target", "arguments", "scope", "permissions", "credential_requirements", "constraints"), true
	case "target":
		return governedTargetSchema(), true
	case "scope":
		return governedScopeSchema(), true
	case "constraints":
		return governedConstraintsSchema(), true
	case "structure":
		return object(map[string]any{"required": stringArray}), true
	case "semantics":
		return object(map[string]any{"claims_require_provenance": map[string]any{"type": "boolean"}, "claims_require_sources": map[string]any{"type": "boolean"}, "purpose": map[string]any{"type": "string"}}), true
	case "validation":
		return object(map[string]any{"required": map[string]any{"type": "array", "items": object(map[string]any{"kind": map[string]any{"type": "string"}, "criterion_ref": map[string]any{"type": "string"}, "rubric": map[string]any{"type": "string"}}, "kind")}}), true
	case "contract":
		return object(map[string]any{"jurisdiction": map[string]any{"type": "string"}, "source_recency": map[string]any{"type": "string"}, "evaluation_cases": map[string]any{"type": "integer"}, "minimum_sources": map[string]any{"type": "integer"}, "validation": governedSchemaMust("validation")}), true
	case "metadata":
		return object(map[string]any{"format": map[string]any{"type": "string"}, "title": map[string]any{"type": "string"}, "rationale": map[string]any{"type": "string"}}), true
	case "details":
		return object(map[string]any{"summary": map[string]any{"type": "string"}, "rationale": map[string]any{"type": "string"}}), true
	case "result":
		result := object(map[string]any{"receipt": map[string]any{"type": "string"}, "status": map[string]any{"type": "string"}, "installed_version": map[string]any{"type": "string"}, "message_id": map[string]any{"type": "string"}, "issue_url": map[string]any{"type": "string"}, "deployment_id": map[string]any{"type": "string"}})
		result["type"] = []string{"null", "object"}
		return result, true
	case "payload_json":
		return map[string]any{"type": "object", "additionalProperties": true}, true
	default:
		return nil, false
	}
}

func governedSchemaMust(name string) map[string]any {
	schema, _ := governedSchema(name)
	return schema
}

func governedTargetSchema() map[string]any {
	return map[string]any{"type": "object", "properties": map[string]any{"package": map[string]any{"type": "string"}, "tool": map[string]any{"type": "string"}, "collection": map[string]any{"type": "string"}, "version": map[string]any{"type": "string"}, "uri": map[string]any{"type": "string"}, "id": map[string]any{"type": "string"}, "name": map[string]any{"type": "string"}}, "additionalProperties": false}
}

func governedScopeSchema() map[string]any {
	return map[string]any{"type": "object", "properties": map[string]any{"environment": map[string]any{"type": "string"}, "workspace": map[string]any{"type": "string"}, "project": map[string]any{"type": "string"}}, "additionalProperties": false}
}

func governedConstraintsSchema() map[string]any {
	return map[string]any{"type": "object", "properties": map[string]any{"audience": map[string]any{"type": "string"}, "global_install": map[string]any{"type": "boolean"}}, "additionalProperties": false}
}

func optionalOutputSchema(schema map[string]any) map[string]any {
	result := make(map[string]any, len(schema))
	hasRequiredDataField := false
	for key, value := range schema {
		if key == "required" {
			if _, ok := value.(bool); ok {
				hasRequiredDataField = true
				continue
			}
			result[key] = value
			continue
		}
		switch typed := value.(type) {
		case map[string]any:
			result[key] = optionalOutputSchema(typed)
		case []any:
			converted := make([]any, len(typed))
			for index, candidate := range typed {
				if nested, ok := candidate.(map[string]any); ok {
					converted[index] = optionalOutputSchema(nested)
				} else {
					converted[index] = candidate
				}
			}
			result[key] = converted
		default:
			result[key] = value
		}
	}
	if _, hasProperties := result["properties"]; hasProperties {
		properties := result["properties"].(map[string]any)
		if hasRequiredDataField {
			properties["required"] = map[string]any{"type": "boolean"}
		}
		if _, configured := result["additionalProperties"]; !configured {
			result["additionalProperties"] = false
		}
	}
	return result
}

func snakeCaseSchema(schema map[string]any) map[string]any {
	result := make(map[string]any, len(schema))
	for key, value := range schema {
		switch key {
		case "properties":
			properties, _ := value.(map[string]any)
			converted := make(map[string]any, len(properties))
			for name, property := range properties {
				if nested, ok := property.(map[string]any); ok {
					converted[snakeCase(name)] = snakeCaseSchema(nested)
				} else {
					converted[snakeCase(name)] = property
				}
			}
			result[key] = converted
		case "required":
			result[key] = snakeCaseRequired(value)
		case "items", "not":
			if nested, ok := value.(map[string]any); ok {
				result[key] = snakeCaseSchema(nested)
			} else {
				result[key] = value
			}
		case "oneOf", "anyOf", "allOf":
			values, _ := value.([]any)
			converted := make([]any, len(values))
			for index, candidate := range values {
				if nested, ok := candidate.(map[string]any); ok {
					converted[index] = snakeCaseSchema(nested)
				} else {
					converted[index] = candidate
				}
			}
			result[key] = converted
		case "$defs":
			definitions, _ := value.(map[string]any)
			converted := make(map[string]any, len(definitions))
			for name, candidate := range definitions {
				if nested, ok := candidate.(map[string]any); ok {
					converted[name] = snakeCaseSchema(nested)
				} else {
					converted[name] = candidate
				}
			}
			result[key] = converted
		default:
			result[key] = value
		}
	}
	return result
}

func snakeCaseRequired(value any) any {
	values, ok := value.([]any)
	if !ok {
		return value
	}
	result := make([]any, len(values))
	for index, candidate := range values {
		if name, ok := candidate.(string); ok {
			result[index] = snakeCase(name)
		} else {
			result[index] = candidate
		}
	}
	return result
}

func oneOf(schemas ...map[string]any) map[string]any {
	values := make([]any, len(schemas))
	for index, schema := range schemas {
		values[index] = schema
	}
	return map[string]any{"anyOf": values}
}

func executionSchema() map[string]any {
	result := schemaFor[executionInput]()
	result["required"] = []string{}
	result["additionalProperties"] = false
	result["x-runtime-branch"] = true
	result["oneOf"] = []any{
		executionBranch([]string{"actor_id", "idempotency_key", "expected_action_version", "state", "action_id", "subject_hash", "authority_grant_id"}, "started", []string{"execution_id", "result", "evidence_artifact_id"}),
		executionBranch([]string{"actor_id", "idempotency_key", "expected_action_version", "state", "execution_id", "result", "evidence_artifact_id"}, "succeeded", []string{"action_id", "subject_hash", "authority_grant_id"}),
		executionBranch([]string{"actor_id", "idempotency_key", "expected_action_version", "state", "execution_id", "result", "evidence_artifact_id"}, "failed", []string{"action_id", "subject_hash", "authority_grant_id"}),
	}
	return result
}

func executionBranch(required []string, state string, forbidden []string) map[string]any {
	return map[string]any{
		"required":   required,
		"properties": map[string]any{"state": map[string]any{"const": state}},
		"not":        map[string]any{"anyOf": requiredFields(forbidden)},
	}
}

func requiredFields(fields []string) []any {
	result := make([]any, len(fields))
	for index, field := range fields {
		result[index] = map[string]any{"required": []string{field}}
	}
	return result
}

func toolResult(result any, cursor int64) *mcp.CallToolResult {
	payload := map[string]any{"workspace": map[string]string{"id": workspaceID, "change_cursor": fmt.Sprint(cursor)}, "result": snakeCaseValue(result)}
	encoded, _ := json.Marshal(payload)
	return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: string(encoded)}}, StructuredContent: payload}
}

func toolErrorResult(error map[string]any) *mcp.CallToolResult {
	payload := map[string]any{"error": snakeCaseValue(error)}
	encoded, _ := json.Marshal(payload)
	return &mcp.CallToolResult{IsError: true, Content: []mcp.Content{&mcp.TextContent{Text: string(encoded)}}, StructuredContent: payload}
}

func snakeCaseValue(value any) any {
	encoded, err := json.Marshal(value)
	if err != nil {
		return value
	}
	var decoded any
	if err := json.Unmarshal(encoded, &decoded); err != nil {
		return value
	}
	return snakeCaseJSON(decoded)
}

func snakeCaseJSON(value any) any {
	switch typed := value.(type) {
	case map[string]any:
		result := make(map[string]any, len(typed))
		for key, candidate := range typed {
			if opaqueGovernedJSON(key) {
				result[snakeCase(key)] = candidate
				continue
			}
			result[snakeCase(key)] = snakeCaseJSON(candidate)
		}
		return result
	case []any:
		result := make([]any, len(typed))
		for index, candidate := range typed {
			result[index] = snakeCaseJSON(candidate)
		}
		return result
	default:
		return value
	}
}

func opaqueGovernedJSON(field string) bool {
	switch snakeCase(field) {
	case "authorization_subject", "contract", "structure", "semantics", "validation", "metadata", "details", "constraints", "result":
		return true
	default:
		return false
	}
}

func snakeCase(value string) string {
	var result strings.Builder
	for index, letter := range []rune(value) {
		if unicode.IsUpper(letter) && index > 0 {
			previous := []rune(value)[index-1]
			nextLower := index+1 < len([]rune(value)) && unicode.IsLower([]rune(value)[index+1])
			if unicode.IsLower(previous) || unicode.IsDigit(previous) || (unicode.IsUpper(previous) && nextLower) {
				result.WriteByte('_')
			}
		}
		result.WriteRune(unicode.ToLower(letter))
	}
	return result.String()
}

func (a *adapter) errorPayload(ctx context.Context, err error, raw json.RawMessage) map[string]any {
	code := "validation_failed"
	var invalidModel *semanticmodel.InvalidArtifactError
	switch {
	case errors.As(err, &invalidModel):
		code = "semantic_model_invalid"
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
		code := "blocked"
		for _, requirement := range claim.Requirements {
			if requirement.Code == work.ClaimRequirementClaimAvailable {
				code = "claim_conflict"
				break
			}
		}
		payload := map[string]any{"code": code, "message": err.Error(), "requirements": claim.Requirements}
		var input struct {
			ID         string `json:"id"`
			WorkItemID string `json:"work_item_id"`
		}
		if json.Unmarshal(raw, &input) == nil {
			id := input.ID
			if id == "" {
				id = input.WorkItemID
			}
			if current, getErr := a.service.GetWorkItem(ctx, id); getErr == nil {
				payload["current"] = map[string]any{"id": current.WorkItem.ID, "version": current.WorkItem.Version, "status": current.WorkItem.ExecutionStatus, "claims": current.Claims}
			}
		}
		return payload
	}
	var transition app.TransitionGateError
	if errors.As(err, &transition) {
		payload := map[string]any{"code": "transition_not_allowed", "message": err.Error(), "requirements": transition.Requirements}
		var input struct {
			ID         string `json:"id"`
			WorkItemID string `json:"work_item_id"`
		}
		if json.Unmarshal(raw, &input) == nil {
			id := input.ID
			if id == "" {
				id = input.WorkItemID
			}
			if current, getErr := a.service.GetWorkItem(ctx, id); getErr == nil {
				payload["current"] = map[string]any{"id": current.WorkItem.ID, "version": current.WorkItem.Version, "status": current.WorkItem.ExecutionStatus, "claims": current.Claims}
			}
		}
		return payload
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
	if err.Error() == "approval_stale" {
		code = "approval_stale"
	}
	payload := map[string]any{"code": code, "message": err.Error(), "requirements": []any{}}
	if code == "approval_stale" {
		var input struct {
			ActionID   string `json:"action_id"`
			ApprovalID string `json:"approval_id"`
		}
		if json.Unmarshal(raw, &input) == nil && input.ActionID == "" && input.ApprovalID != "" {
			if approval, getErr := a.service.GetActionApproval(ctx, input.ApprovalID); getErr == nil {
				input.ActionID = approval.ExternalActionID
			}
		}
		if input.ActionID != "" {
			if action, getErr := a.service.GetExternalAction(ctx, input.ActionID); getErr == nil {
				current := map[string]any{"id": action.ID, "version": action.Version, "state": action.State, "current_revision": action.CurrentRevision}
				if revision, revisionErr := a.service.GetCurrentExternalActionRevision(ctx, action.ID); revisionErr == nil {
					current["authorization_subject_hash"] = revision.AuthorizationSubjectHash
				}
				payload["current"] = current
			}
		}
	}
	if code == "version_conflict" {
		var input map[string]json.RawMessage
		if json.Unmarshal(raw, &input) == nil {
			for _, field := range []string{"id", "work_item_id", "objective_id", "plan_id", "profile_id", "action_id", "output_revision_id", "execution_id", "approval_id"} {
				var id string
				if json.Unmarshal(input[field], &id) == nil && id != "" {
					if current, getErr := a.service.GetWorkItem(ctx, id); getErr == nil {
						payload["current"] = map[string]any{"id": current.WorkItem.ID, "key": current.WorkItem.Key, "version": current.WorkItem.Version, "status": current.WorkItem.ExecutionStatus}
						return payload
					}
					context, contextErr := a.service.GetObjectiveContext(ctx, id)
					if contextErr == nil {
						payload["current"] = map[string]any{"id": context.Objective.ID, "key": context.Objective.Key, "version": context.Objective.Version, "phase": context.Objective.Phase}
						return payload
					}
					action, actionErr := a.service.GetExternalAction(ctx, id)
					if actionErr == nil {
						payload["current"] = map[string]any{"id": action.ID, "version": action.Version, "state": action.State, "current_revision": action.CurrentRevision}
						return payload
					}
					plan, planErr := a.service.GetPlan(ctx, id)
					if planErr == nil {
						payload["current"] = map[string]any{"id": plan.ID, "version": plan.Version, "commitment_state": plan.CommitmentState, "revision": plan.Revision}
						return payload
					}
					profile, profileErr := a.service.GetOutputProfileByID(ctx, id)
					if profileErr == nil {
						payload["current"] = map[string]any{"id": profile.ID, "name": profile.Name, "version": profile.Version, "state_version": profile.StateVersion, "lifecycle_state": profile.LifecycleState}
						return payload
					}
					revision, revisionErr := a.service.GetOutputRevision(ctx, id)
					if revisionErr == nil {
						payload["current"] = map[string]any{"id": revision.ID, "revision": revision.Revision, "acceptance_state": revision.AcceptanceState, "expected_output_id": revision.ExpectedOutputID}
						return payload
					}
					execution, executionErr := a.service.GetExternalActionExecution(ctx, id)
					if executionErr == nil {
						payload["current"] = map[string]any{"id": execution.ID, "action_id": execution.ExternalActionID, "action_revision": execution.ActionRevision, "state": execution.State}
						return payload
					}
					approval, approvalErr := a.service.GetActionApproval(ctx, id)
					if approvalErr == nil {
						payload["current"] = map[string]any{"id": approval.ID, "action_id": approval.ExternalActionID, "action_revision": approval.ExternalActionRevision, "status": approval.Status}
						return payload
					}
					genericApproval, genericApprovalErr := a.service.GetApproval(ctx, id)
					if genericApprovalErr == nil {
						payload["current"] = map[string]any{"id": genericApproval.ID, "version": genericApproval.Version, "status": genericApproval.Status, "plan_id": genericApproval.PlanID, "work_item_id": genericApproval.WorkItemID, "output_profile_id": genericApproval.OutputProfileID, "output_revision_id": genericApproval.OutputRevisionID}
						return payload
					}
				}
			}
		}
	}
	return payload
}

func decode(raw json.RawMessage, target any) error {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	if err := requireEOF(decoder); err != nil {
		return err
	}
	return validateWorkspace(workspaceFrom(target))
}

func requireEOF(decoder *json.Decoder) error {
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		if err == nil {
			return errors.New("input must contain exactly one JSON value")
		}
		return err
	}
	return nil
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
	ActorID        string `json:"actor_id"`
	IncludeClaimed bool   `json:"include_claimed"`
}

func (a *adapter) listReady(ctx context.Context, raw json.RawMessage) (any, error) {
	var in listReadyInput
	if err := decode(raw, &in); err != nil {
		return nil, err
	}
	var (
		items []ports.ReadyWorkItem
		err   error
	)
	if in.ActorID == "" {
		items, err = a.service.ListReadyWork(ctx)
	} else {
		items, err = a.service.ListReadyWorkForActor(ctx, in.ActorID)
	}
	if err != nil || in.IncludeClaimed {
		return items, err
	}
	ready := make([]ports.ReadyWorkItem, 0, len(items))
	for _, item := range items {
		if item.WorkItem.ExecutionStatus == work.StatusReady {
			ready = append(ready, item)
		}
	}
	return ready, nil
}

type boardOverviewInput struct {
	workspaceInput
	ObjectiveID      string `json:"objective_id"`
	IncludeAttention bool   `json:"include_attention"`
}

func (a *adapter) boardOverview(ctx context.Context, raw json.RawMessage) (any, error) {
	var in boardOverviewInput
	if err := decode(raw, &in); err != nil {
		return nil, err
	}
	items, err := a.service.ListWorkItems(ctx)
	if err != nil {
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
	result := boardOverviewResult{ChangeCursor: fmt.Sprint(cursor), Objectives: map[string]int{}, Counts: map[string]int{}}
	plans := map[string]bool{}
	for _, item := range items {
		if in.ObjectiveID != "" && item.Objective.ID != in.ObjectiveID {
			continue
		}
		result.Objectives[string(item.Objective.Phase)]++
		result.Counts[string(item.WorkItem.ExecutionStatus)]++
		if item.Plan != nil && item.Plan.CommitmentState == work.PlanProposed && !plans[item.Plan.ID] {
			result.PlansNeedingReview++
			plans[item.Plan.ID] = true
		}
		for _, action := range item.ExternalActions {
			if action.Action.State == authority.ActionProposed {
				result.ExternalActionsNeedingAuthority++
			}
		}
		if in.IncludeAttention && item.WorkItem.AttentionState != work.AttentionNone {
			result.NeedsHumanAttention = append(result.NeedsHumanAttention, item.WorkItem)
		}
	}
	profiles, err := a.service.ListOutputProfiles(ctx)
	if err != nil {
		return nil, err
	}
	for _, profile := range profiles {
		if profile.LifecycleState == output.ProfileProposed {
			result.OutputProfilesNeedingReview++
		}
	}
	for _, item := range ready {
		if in.ObjectiveID == "" || item.Objective.ID == in.ObjectiveID {
			result.ReadyHighPriority = append(result.ReadyHighPriority, item)
		}
	}
	return result, nil
}

type boardOverviewResult struct {
	ChangeCursor                    string                `json:"change_cursor"`
	Objectives                      map[string]int        `json:"objectives"`
	PlansNeedingReview              int                   `json:"plans_needing_review"`
	OutputProfilesNeedingReview     int                   `json:"output_profiles_needing_review"`
	ExternalActionsNeedingAuthority int                   `json:"external_actions_needing_authority"`
	Counts                          map[string]int        `json:"counts"`
	ReadyHighPriority               []ports.ReadyWorkItem `json:"ready_high_priority"`
	NeedsHumanAttention             []work.WorkItem       `json:"needs_human_attention"`
}

type listItemsInput struct {
	workspaceInput
	ObjectiveID            string                 `json:"objective_id"`
	ObjectivePhases        []work.ObjectivePhase  `json:"objective_phase"`
	PlanID                 string                 `json:"plan_id"`
	CommitmentStates       []work.ItemCommitment  `json:"commitment_state"`
	ExecutionStatus        []work.ExecutionStatus `json:"execution_status"`
	Priorities             []work.Priority        `json:"priority"`
	Kinds                  []string               `json:"kind"`
	AttentionStates        []work.AttentionState  `json:"attention_state"`
	ExecutionPolicy        []work.ExecutionPolicy `json:"execution_policy"`
	RequiredCapabilities   []string               `json:"required_capability"`
	RequiredOutputProfiles []string               `json:"required_output_profile"`
	ClaimedBy              string                 `json:"claimed_by"`
	Blocked                *bool                  `json:"blocked"`
	Cursor                 string                 `json:"cursor"`
	Limit                  int                    `json:"limit"`
}

func (a *adapter) listItems(ctx context.Context, raw json.RawMessage) (any, error) {
	var in listItemsInput
	if err := decode(raw, &in); err != nil {
		return nil, err
	}
	items, err := a.service.ListWorkItems(ctx)
	if err != nil {
		return nil, err
	}
	readyIDs := map[string]bool{}
	if in.Blocked != nil {
		ready, err := a.service.ListReadyWork(ctx)
		if err != nil {
			return nil, err
		}
		for _, candidate := range ready {
			readyIDs[candidate.WorkItem.ID] = true
		}
	}
	offset, err := decodePageCursor(in.Cursor)
	if err != nil {
		return nil, err
	}
	filtered := make([]ports.WorkItemContext, 0, len(items))
	for _, item := range items {
		if in.ObjectiveID != "" && item.Objective.ID != in.ObjectiveID {
			continue
		}
		if in.PlanID != "" && (item.Plan == nil || item.Plan.ID != in.PlanID) {
			continue
		}
		if !contains(in.ObjectivePhases, item.Objective.Phase) || !contains(in.CommitmentStates, item.WorkItem.CommitmentState) || !contains(in.ExecutionStatus, item.WorkItem.ExecutionStatus) || !contains(in.Priorities, item.WorkItem.Priority) || !contains(in.Kinds, item.WorkItem.Kind) || !contains(in.AttentionStates, item.WorkItem.AttentionState) || !contains(in.ExecutionPolicy, item.WorkItem.ExecutionPolicy) {
			continue
		}
		if !hasCapabilities(item.RequiredCapabilities, in.RequiredCapabilities) || !hasOutputProfiles(item.ExpectedOutputs, in.RequiredOutputProfiles) {
			continue
		}
		if in.ClaimedBy != "" && !claimedBy(item.Claims, in.ClaimedBy) {
			continue
		}
		blocked := item.Objective.Phase == work.ObjectiveExecution && item.WorkItem.CommitmentState == work.ItemAccepted && item.WorkItem.ExecutionStatus != work.StatusDone && item.WorkItem.ExecutionStatus != work.StatusCancelled && !readyIDs[item.WorkItem.ID]
		if in.Blocked != nil && *in.Blocked != blocked {
			continue
		}
		filtered = append(filtered, item)
	}
	if offset > len(filtered) {
		return nil, errors.New("invalid list_items cursor")
	}
	limit := in.Limit
	if limit <= 0 || limit > 100 {
		limit = 100
	}
	end := offset + limit
	if end > len(filtered) {
		end = len(filtered)
	}
	next := ""
	if end < len(filtered) {
		next = encodePageCursor(end)
	}
	return listItemsResult{Items: filtered[offset:end], NextCursor: next, HasMore: end < len(filtered)}, nil
}

type listItemsResult struct {
	Items      []ports.WorkItemContext `json:"items"`
	NextCursor string                  `json:"next_cursor"`
	HasMore    bool                    `json:"has_more"`
}

func encodePageCursor(offset int) string {
	return base64.RawURLEncoding.EncodeToString([]byte(strconv.Itoa(offset)))
}

func decodePageCursor(cursor string) (int, error) {
	if cursor == "" {
		return 0, nil
	}
	decoded, err := base64.RawURLEncoding.DecodeString(cursor)
	if err != nil {
		return 0, errors.New("invalid list_items cursor")
	}
	offset, err := strconv.Atoi(string(decoded))
	if err != nil || offset < 0 {
		return 0, errors.New("invalid list_items cursor")
	}
	return offset, nil
}

func hasCapabilities(values, required []string) bool {
	for _, value := range required {
		if !containsCapability(values, value) {
			return false
		}
	}
	return true
}

func hasOutputProfiles(values []output.ExpectedOutputDetail, required []string) bool {
	for _, value := range required {
		if !hasOutputProfile(values, value) {
			return false
		}
	}
	return true
}

func containsCapability(values []string, value string) bool {
	for _, candidate := range values {
		if candidate == value {
			return true
		}
	}
	return false
}

func hasOutputProfile(values []output.ExpectedOutputDetail, name string) bool {
	for _, value := range values {
		if value.Profile.Name == name {
			return true
		}
	}
	return false
}

func claimedBy(values []work.Claim, actorID string) bool {
	for _, value := range values {
		if value.ActorID == actorID && value.ReleasedAt.IsZero() {
			return true
		}
	}
	return false
}

func contains[T comparable](values []T, value T) bool {
	return len(values) == 0 || func() bool {
		for _, candidate := range values {
			if candidate == value {
				return true
			}
		}
		return false
	}()
}

type getItemInput struct {
	workspaceInput
	ID            string   `json:"id"`
	Include       []string `json:"include"`
	ActivityLimit int      `json:"activity_limit"`
}

func (a *adapter) getItem(ctx context.Context, raw json.RawMessage) (any, error) {
	var in getItemInput
	if err := decode(raw, &in); err != nil {
		return nil, err
	}
	item, err := a.service.GetWorkItem(ctx, in.ID)
	if err != nil {
		return nil, err
	}
	selected := make(map[string]bool, len(in.Include))
	for _, section := range in.Include {
		section = strings.TrimSpace(section)
		if !containsString([]string{"description", "plan", "context", "acceptance_criteria", "expected_outputs", "output_revisions", "validations", "required_outputs", "capabilities", "external_actions", "authority_grants", "dependencies", "claims", "progress", "decisions", "questions", "approvals", "artifacts", "activity"}, section) {
			return nil, fmt.Errorf("get_item include %q is not supported", section)
		}
		selected[section] = true
	}
	if len(selected) > 0 {
		if !selected["plan"] {
			item.Plan = nil
		}
		if !selected["acceptance_criteria"] {
			item.AcceptanceCriteria = nil
		}
		if !selected["expected_outputs"] {
			item.ExpectedOutputs = nil
		}
		if !selected["output_revisions"] {
			item.OutputRevisions = nil
		}
		if !selected["required_outputs"] {
			item.OutputRequirements = nil
		}
		if !selected["capabilities"] {
			item.RequiredCapabilities = nil
		}
		if !selected["external_actions"] && !selected["authority_grants"] {
			item.ExternalActions = nil
		}
		if !selected["dependencies"] {
			item.Dependencies = nil
		}
		if !selected["claims"] {
			item.Claims = nil
		}
		if !selected["progress"] {
			item.Progress = nil
		}
		if !selected["artifacts"] {
			item.Artifacts = nil
		}
	}
	result := getItemResult{WorkItemContext: item}
	if selected["activity"] {
		limit := in.ActivityLimit
		if limit <= 0 || limit > 100 {
			limit = 20
		}
		result.Activity, err = a.service.ListActivity(ctx, app.ActivityFilter{WorkItemID: in.ID, Limit: limit})
		if err != nil {
			return nil, err
		}
	}
	return result, nil
}

type getItemResult struct {
	ports.WorkItemContext
	Activity []work.Activity `json:"activity"`
}

type objectiveContextInput struct {
	workspaceInput
	ObjectiveID string   `json:"objective_id"`
	ActorID     string   `json:"actor_id"`
	MaxItems    int      `json:"max_items_per_section"`
	Include     []string `json:"include"`
}

func (a *adapter) getObjectiveContext(ctx context.Context, raw json.RawMessage) (any, error) {
	var in objectiveContextInput
	if err := decode(raw, &in); err != nil {
		return nil, err
	}
	for _, section := range in.Include {
		if !containsString([]string{"intent", "decisions", "open_questions", "approved_plan", "ready_work", "accepted_outputs", "external_actions", "authority", "recent_changes", "artifacts", "approvals"}, strings.TrimSpace(section)) {
			return nil, fmt.Errorf("get_objective_context include %q is not supported", section)
		}
	}
	return a.service.SelectObjectiveContext(ctx, app.ObjectiveContextQuery{ObjectiveID: in.ObjectiveID, ActorID: in.ActorID, Include: in.Include, MaxItemsPerSection: in.MaxItems})
}

func containsString(values []string, value string) bool {
	for _, candidate := range values {
		if candidate == value {
			return true
		}
	}
	return false
}

type changesInput struct {
	workspaceInput
	Since       string `json:"since"`
	Limit       int    `json:"limit"`
	WorkItemID  string `json:"work_item_id"`
	ObjectiveID string `json:"objective_id"`
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
	changes, err := a.service.ListActivity(ctx, app.ActivityFilter{Since: since, Limit: limit + 1, WorkItemID: in.WorkItemID, ObjectiveID: in.ObjectiveID})
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
	return changesResult{Changes: changes, NextCursor: fmt.Sprint(next), HasMore: hasMore}, nil
}

type semanticModelInput struct {
	Section string   `json:"section"`
	IDs     []string `json:"ids"`
}

func semanticModelSchema() map[string]any {
	return map[string]any{"type": "object", "properties": map[string]any{
		"section": map[string]any{"type": "string", "enum": semanticmodel.AvailableSections()},
		"ids":     map[string]any{"type": "array", "maxItems": 50, "uniqueItems": true, "items": map[string]any{"type": "string"}},
	}, "additionalProperties": false}
}

func semanticModelResultSchema() map[string]any {
	return map[string]any{"type": "object", "properties": map[string]any{
		"section": map[string]any{"type": "string"}, "model_version": map[string]any{"type": "string"},
		"content_digest": map[string]any{"type": "string"}, "data": map[string]any{"anyOf": []any{map[string]any{"type": "object"}, map[string]any{"type": "array"}}},
		"not_found_ids": map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
	}, "required": []string{"section", "model_version", "content_digest", "data", "not_found_ids"}, "additionalProperties": false}
}

func (a *adapter) getSemanticModel(_ context.Context, raw json.RawMessage) (any, error) {
	var input semanticModelInput
	if err := decode(raw, &input); err != nil {
		return nil, err
	}
	model, err := semanticmodel.Load()
	if err != nil {
		return nil, err
	}
	data, missing, err := model.Section(input.Section, input.IDs)
	if err != nil {
		return nil, err
	}
	section := input.Section
	if section == "" {
		section = "manifest"
	}
	return map[string]any{"section": section, "model_version": model.ModelVersion, "content_digest": model.ContentDigest, "data": data, "not_found_ids": missing}, nil
}

type changesResult struct {
	Changes    []work.Activity `json:"changes"`
	NextCursor string          `json:"next_cursor"`
	HasMore    bool            `json:"has_more"`
}

func (a *adapter) listProfiles(ctx context.Context, raw json.RawMessage) (any, error) {
	var in workspaceInput
	if err := decode(raw, &in); err != nil {
		return nil, err
	}
	return a.service.ListOutputProfiles(ctx)
}

type outputProfileInput struct {
	workspaceInput
	Name    string `json:"profile_name"`
	Version int    `json:"profile_version"`
}

func (a *adapter) getProfile(ctx context.Context, raw json.RawMessage) (any, error) {
	var in outputProfileInput
	if err := decode(raw, &in); err != nil {
		return nil, err
	}
	profiles, err := a.service.ListOutputProfiles(ctx)
	if err != nil {
		return nil, err
	}
	for _, profile := range profiles {
		if profile.Name == in.Name && profile.Version == in.Version {
			return profile, nil
		}
	}
	return nil, ports.ErrNotFound
}

type outputsInput struct {
	workspaceInput
	ProfileName   string `json:"profile_name"`
	Version       string `json:"version_constraint"`
	ObjectiveID   string `json:"objective_id"`
	ProducedBy    string `json:"produced_by"`
	AcceptedSince string `json:"accepted_since"`
	Limit         int    `json:"limit"`
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
	acceptedSince, err := parseOptionalTime(in.AcceptedSince)
	if err != nil {
		return nil, errors.New("accepted_since must be RFC3339")
	}
	filter := app.AcceptedOutputFilter{ProfileName: in.ProfileName, VersionConstraint: in.Version, ObjectiveID: in.ObjectiveID, ProducedBy: in.ProducedBy, Limit: limit}
	if acceptedSince != nil {
		filter.AcceptedSince = *acceptedSince
	}
	return a.service.ListAcceptedOutputs(ctx, filter)
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
	IdempotencyKey string              `json:"idempotency_key"`
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
	return a.service.CreateObjective(ctx, app.CreateObjectiveCommand{ActorID: in.ActorID, IdempotencyKey: in.IdempotencyKey, Key: in.Key, Title: in.Title, Description: in.Description, DesiredOutcome: in.DesiredOutcome, Phase: in.Phase})
}

type patchObjectiveInput struct {
	workspaceInput
	ObjectiveID     string  `json:"objective_id"`
	ActorID         string  `json:"actor_id"`
	IdempotencyKey  string  `json:"idempotency_key"`
	ExpectedVersion int     `json:"expected_version"`
	Title           *string `json:"title"`
	Description     *string `json:"description"`
	DesiredOutcome  *string `json:"desired_outcome"`
}

func (a *adapter) patchObjective(ctx context.Context, raw json.RawMessage) (any, error) {
	var in patchObjectiveInput
	if err := decode(raw, &in); err != nil {
		return nil, err
	}
	return a.service.PatchObjective(ctx, app.PatchObjectiveCommand{ObjectiveID: in.ObjectiveID, ActorID: in.ActorID, IdempotencyKey: in.IdempotencyKey, ExpectedVersion: in.ExpectedVersion, Title: in.Title, Description: in.Description, DesiredOutcome: in.DesiredOutcome})
}

type createItemInput struct {
	workspaceInput
	ActorID              string                         `json:"actor_id"`
	IdempotencyKey       string                         `json:"idempotency_key"`
	Key                  string                         `json:"key"`
	ObjectiveID          string                         `json:"objective_id"`
	PlanID               string                         `json:"plan_id"`
	ParentID             string                         `json:"parent_id"`
	Title                string                         `json:"title"`
	Description          string                         `json:"description"`
	Kind                 string                         `json:"kind"`
	CommitmentState      work.ItemCommitment            `json:"commitment_state"`
	ExecutionStatus      work.ExecutionStatus           `json:"execution_status"`
	Priority             work.Priority                  `json:"priority"`
	EstimatedScope       work.EstimatedScope            `json:"estimated_scope"`
	ExecutionPolicy      work.ExecutionPolicy           `json:"execution_policy"`
	RequiredActorKind    work.ActorKind                 `json:"required_actor_kind"`
	RequiredCapabilities []string                       `json:"required_capabilities,omitempty"`
	AcceptanceCriteria   []planAcceptanceCriterionInput `json:"acceptance_criteria,omitempty"`
	ExpectedOutputs      []planExpectedOutputInput      `json:"expected_outputs,omitempty"`
	OutputRequirements   []planOutputRequirementInput   `json:"output_requirements,omitempty"`
	ExternalActions      []planExternalActionInput      `json:"external_actions,omitempty"`
	Dependencies         []createItemDependencyInput    `json:"dependencies,omitempty"`
}

type createItemDependencyInput struct {
	DependsOnWorkItemID string              `json:"depends_on_work_item_id"`
	Kind                work.DependencyKind `json:"kind"`
	Note                string              `json:"note,omitempty"`
}

func (a *adapter) createItem(ctx context.Context, raw json.RawMessage) (any, error) {
	var in createItemInput
	if err := decode(raw, &in); err != nil {
		return nil, err
	}
	command := app.CreateWorkItemCommand{
		ActorID: in.ActorID, IdempotencyKey: in.IdempotencyKey, Key: in.Key, ObjectiveID: in.ObjectiveID, PlanID: in.PlanID, ParentID: in.ParentID,
		Title: in.Title, Description: in.Description, Kind: in.Kind, CommitmentState: in.CommitmentState, ExecutionStatus: in.ExecutionStatus,
		Priority: in.Priority, EstimatedScope: in.EstimatedScope, ExecutionPolicy: in.ExecutionPolicy, RequiredActorKind: in.RequiredActorKind,
		AttentionState: work.AttentionNone, RequiredCapabilities: in.RequiredCapabilities,
	}
	if command.CommitmentState == "" {
		command.CommitmentState = work.ItemProposed
	}
	if command.ExecutionStatus == "" {
		command.ExecutionStatus = work.StatusBacklog
	}
	for _, criterion := range in.AcceptanceCriteria {
		command.AcceptanceCriteria = append(command.AcceptanceCriteria, app.ProposedAcceptanceCriterion{Text: criterion.Text, Required: criterion.Required, Ordinal: criterion.Ordinal})
	}
	for _, expected := range in.ExpectedOutputs {
		command.ExpectedOutputs = append(command.ExpectedOutputs, app.ProposedExpectedOutput{Name: expected.Name, ProfileName: expected.ProfileName, ProfileVersion: expected.ProfileVersion, Contract: expected.Contract, DestinationHint: expected.DestinationHint, Required: expected.Required, Ordinal: expected.Ordinal})
	}
	for _, requirement := range in.OutputRequirements {
		command.OutputRequirements = append(command.OutputRequirements, app.ProposedOutputRequirement{RequiredOutputRevisionID: requirement.RequiredOutputRevisionID, RequiredProfileName: requirement.RequiredProfileName, VersionConstraint: requirement.VersionConstraint, Required: requirement.Required, Note: requirement.Note})
	}
	for _, action := range in.ExternalActions {
		subject, err := externalActionSubject(action)
		if err != nil {
			return nil, err
		}
		command.ExternalActions = append(command.ExternalActions, app.ProposedExternalAction{Required: action.Required, Title: action.Title, Rationale: action.Rationale, AuthorizationSubject: subject})
	}
	for _, dependency := range in.Dependencies {
		command.Dependencies = append(command.Dependencies, app.CreateWorkItemDependency{DependsOnWorkItemID: dependency.DependsOnWorkItemID, Kind: dependency.Kind, Note: dependency.Note})
	}
	return a.service.CreateWorkItem(ctx, command)
}

type patchItemInput struct {
	workspaceInput
	ID                             string                           `json:"id"`
	ActorID                        string                           `json:"actor_id"`
	IdempotencyKey                 string                           `json:"idempotency_key"`
	ExpectedVersion                int                              `json:"expected_version"`
	Title                          *string                          `json:"title"`
	Description                    *string                          `json:"description"`
	ParentID                       *string                          `json:"parent_id"`
	Priority                       *work.Priority                   `json:"priority"`
	EstimatedScope                 *work.EstimatedScope             `json:"estimated_scope"`
	ExecutionPolicy                *work.ExecutionPolicy            `json:"execution_policy"`
	AttentionState                 *work.AttentionState             `json:"attention_state"`
	RequiredCapabilities           *[]string                        `json:"required_capabilities"`
	AcceptanceCriterionResolutions []patchAcceptanceResolutionInput `json:"acceptance_criterion_resolutions"`
	ExpectedOutputsToAdd           []planExpectedOutputInput        `json:"expected_outputs_to_add"`
}

type patchAcceptanceResolutionInput struct {
	CriterionID string                         `json:"criterion_id"`
	Status      work.AcceptanceCriterionStatus `json:"status"`
	Rationale   string                         `json:"rationale"`
}

func patchItemSchema() map[string]any {
	result := schemaFor[patchItemInput]("id", "actor_id", "idempotency_key", "expected_version")
	properties := result["properties"].(map[string]any)
	properties["parent_id"] = map[string]any{"type": "string"}
	properties["required_capabilities"] = map[string]any{"type": "array", "items": map[string]any{"type": "string"}}
	properties["acceptance_criterion_resolutions"] = map[string]any{"type": "array", "items": map[string]any{
		"type": "object", "properties": map[string]any{
			"criterion_id": map[string]any{"type": "string"},
			"status":       map[string]any{"enum": []string{"satisfied", "waived"}},
			"rationale":    map[string]any{"type": "string"},
		}, "required": []string{"criterion_id", "status", "rationale"}, "additionalProperties": false,
	}}
	properties["expected_outputs_to_add"] = map[string]any{"type": "array", "items": map[string]any{
		"type": "object", "properties": map[string]any{
			"name":             map[string]any{"type": "string"},
			"profile":          map[string]any{"type": "string"},
			"profile_version":  map[string]any{"type": "integer"},
			"contract":         governedSchemaMust("contract"),
			"destination_hint": map[string]any{"type": "string"},
			"required":         map[string]any{"type": "boolean"},
			"ordinal":          map[string]any{"type": "integer"},
		}, "required": []string{"name", "profile", "profile_version", "ordinal"}, "additionalProperties": false,
	}}
	return result
}

func (a *adapter) patchItem(ctx context.Context, raw json.RawMessage) (any, error) {
	var in patchItemInput
	if err := decode(raw, &in); err != nil {
		return nil, err
	}
	command := app.PatchWorkItemCommand{WorkItemID: in.ID, ActorID: in.ActorID, IdempotencyKey: in.IdempotencyKey, ExpectedVersion: in.ExpectedVersion, Title: in.Title, Description: in.Description, ParentID: in.ParentID, Priority: in.Priority, EstimatedScope: in.EstimatedScope, ExecutionPolicy: in.ExecutionPolicy, AttentionState: in.AttentionState, RequiredCapabilities: in.RequiredCapabilities}
	for _, resolution := range in.AcceptanceCriterionResolutions {
		command.AcceptanceCriterionResolutions = append(command.AcceptanceCriterionResolutions, app.PatchAcceptanceCriterionResolution{CriterionID: resolution.CriterionID, Status: resolution.Status, Rationale: resolution.Rationale})
	}
	for _, expected := range in.ExpectedOutputsToAdd {
		command.ExpectedOutputsToAdd = append(command.ExpectedOutputsToAdd, app.ProposedExpectedOutput{Name: expected.Name, ProfileName: expected.ProfileName, ProfileVersion: expected.ProfileVersion, Contract: expected.Contract, DestinationHint: expected.DestinationHint, Required: expected.Required, Ordinal: expected.Ordinal})
	}
	return a.service.PatchWorkItem(ctx, command)
}

type requestAttentionInput struct {
	workspaceInput
	TargetKind      string              `json:"target_kind"`
	TargetID        string              `json:"target_id"`
	WorkItemID      string              `json:"work_item_id"`
	ActorID         string              `json:"actor_id"`
	IdempotencyKey  string              `json:"idempotency_key"`
	ExpectedVersion int                 `json:"expected_version"`
	AttentionState  work.AttentionState `json:"attention_state"`
}

func requestAttentionSchema() map[string]any {
	result := schemaFor[requestAttentionInput]("actor_id", "idempotency_key", "attention_state")
	result["x-runtime-branch"] = true
	result["oneOf"] = []any{
		map[string]any{"required": []string{"work_item_id", "expected_version"}, "properties": map[string]any{"target_kind": map[string]any{"enum": []string{"work_item"}}}, "not": map[string]any{"required": []string{"target_id"}}},
		map[string]any{"required": []string{"target_kind", "target_id", "expected_version"}, "properties": map[string]any{"target_kind": map[string]any{"enum": []string{"question"}}}, "not": map[string]any{"required": []string{"work_item_id"}}},
		map[string]any{"required": []string{"target_kind", "target_id"}, "properties": map[string]any{"target_kind": map[string]any{"enum": []string{"decision", "review", "clarification", "intervention"}}}, "not": map[string]any{"required": []string{"work_item_id"}}},
	}
	return result
}

func (a *adapter) requestAttention(ctx context.Context, raw json.RawMessage) (any, error) {
	var in requestAttentionInput
	if err := decode(raw, &in); err != nil {
		return nil, err
	}
	return a.service.RequestAttention(ctx, app.RequestAttentionCommand{TargetKind: in.TargetKind, TargetID: in.TargetID, WorkItemID: in.WorkItemID, ActorID: in.ActorID, IdempotencyKey: in.IdempotencyKey, ExpectedVersion: in.ExpectedVersion, AttentionState: in.AttentionState})
}

type requestApprovalInput struct {
	workspaceInput
	TargetKind               string          `json:"target_kind"`
	ActorID                  string          `json:"actor_id"`
	IdempotencyKey           string          `json:"idempotency_key"`
	Request                  string          `json:"request"`
	WorkItemID               string          `json:"work_item_id"`
	PlanID                   string          `json:"plan_id"`
	OutputProfileID          string          `json:"output_profile_id"`
	OutputRevisionID         string          `json:"output_revision_id"`
	ActionID                 string          `json:"action_id"`
	ApprovedForActorID       string          `json:"approved_for_actor_id"`
	ExpectedVersion          int             `json:"expected_version"`
	ExpectedActionVersion    int             `json:"expected_action_version"`
	AuthorizationSubjectHash string          `json:"authorization_subject_hash"`
	Constraints              json.RawMessage `json:"constraints"`
	ExpiresAt                string          `json:"expires_at"`
}

func (a *adapter) requestApproval(ctx context.Context, raw json.RawMessage) (any, error) {
	var in requestApprovalInput
	if err := decode(raw, &in); err != nil {
		return nil, err
	}
	switch in.TargetKind {
	case "plan", "work_item", "output_profile", "output_revision":
		if in.ExpectedVersion < 1 {
			return nil, errors.New("generic approval requires expected_version")
		}
		if (in.TargetKind == "plan" && (in.PlanID == "" || in.WorkItemID != "" || in.OutputProfileID != "" || in.OutputRevisionID != "" || in.ActionID != "")) ||
			(in.TargetKind == "work_item" && (in.WorkItemID == "" || in.PlanID != "" || in.OutputProfileID != "" || in.OutputRevisionID != "" || in.ActionID != "")) ||
			(in.TargetKind == "output_profile" && (in.OutputProfileID == "" || in.PlanID != "" || in.WorkItemID != "" || in.OutputRevisionID != "" || in.ActionID != "")) ||
			(in.TargetKind == "output_revision" && (in.OutputRevisionID == "" || in.PlanID != "" || in.WorkItemID != "" || in.OutputProfileID != "" || in.ActionID != "")) {
			return nil, errors.New("generic approval target_kind must match exactly one target id")
		}
		return a.service.RequestApproval(ctx, app.RequestApprovalCommand{ActorID: in.ActorID, IdempotencyKey: in.IdempotencyKey, Request: in.Request, PlanID: in.PlanID, WorkItemID: in.WorkItemID, OutputProfileID: in.OutputProfileID, OutputRevisionID: in.OutputRevisionID, ExpectedTargetVersion: in.ExpectedVersion})
	case "external_action":
		if in.ActionID == "" || in.ApprovedForActorID == "" || in.ExpectedActionVersion < 1 || in.AuthorizationSubjectHash == "" || in.PlanID != "" || in.WorkItemID != "" || in.OutputProfileID != "" || in.OutputRevisionID != "" {
			return nil, errors.New("external action approval requires action_id, approved_for_actor_id, expected_action_version, and authorization_subject_hash")
		}
		expiresAt, err := parseOptionalTime(in.ExpiresAt)
		if err != nil {
			return nil, err
		}
		return a.service.RequestExternalActionApproval(ctx, app.RequestExternalActionApprovalCommand{ActionID: in.ActionID, ActorID: in.ActorID, ApprovedForActorID: in.ApprovedForActorID, ExpectedActionVersion: in.ExpectedActionVersion, ExpectedSubjectHash: in.AuthorizationSubjectHash, IdempotencyKey: in.IdempotencyKey, Constraints: in.Constraints, ExpiresAt: expiresAt, Request: in.Request})
	default:
		return nil, errors.New("request_approval target_kind must be plan, work_item, output_profile, output_revision, or external_action")
	}
}

func requestApprovalSchema() map[string]any {
	result := schemaFor[requestApprovalInput]()
	result["additionalProperties"] = false
	result["x-runtime-branch"] = true
	result["oneOf"] = []any{
		approvalRequestBranch("plan", "plan_id"),
		approvalRequestBranch("work_item", "work_item_id"),
		approvalRequestBranch("output_profile", "output_profile_id"),
		approvalRequestBranch("output_revision", "output_revision_id"),
		approvalRequestBranch("external_action", "action_id", "approved_for_actor_id", "expected_action_version", "authorization_subject_hash", "constraints"),
	}
	return result
}

func approvalRequestBranch(kind string, target string, additional ...string) map[string]any {
	required := append([]string{"target_kind", "actor_id", "idempotency_key", "request", target}, additional...)
	if kind != "external_action" {
		required = append(required, "expected_version")
	}
	forbidden := []string{"plan_id", "work_item_id", "output_profile_id", "output_revision_id", "action_id"}
	forbidden = removeString(forbidden, target)
	return map[string]any{"required": required, "properties": map[string]any{"target_kind": map[string]any{"const": kind}}, "not": map[string]any{"anyOf": requiredFields(forbidden)}}
}

func removeString(values []string, target string) []string {
	result := make([]string, 0, len(values))
	for _, value := range values {
		if value != target {
			result = append(result, value)
		}
	}
	return result
}

func parseOptionalTime(value string) (*time.Time, error) {
	if strings.TrimSpace(value) == "" {
		return nil, nil
	}
	parsed, err := time.Parse(time.RFC3339, value)
	if err != nil {
		return nil, errors.New("expires_at must be RFC3339")
	}
	return &parsed, nil
}

type resolveApprovalInput struct {
	workspaceInput
	TargetKind            string `json:"target_kind"`
	ApprovalID            string `json:"approval_id"`
	ActorID               string `json:"actor_id"`
	IdempotencyKey        string `json:"idempotency_key"`
	Decision              string `json:"decision"`
	ExpectedVersion       int    `json:"expected_version"`
	ExpectedActionVersion int    `json:"expected_action_version"`
	Rationale             string `json:"rationale"`
}

func (a *adapter) resolveApproval(ctx context.Context, raw json.RawMessage) (any, error) {
	var in resolveApprovalInput
	if err := decode(raw, &in); err != nil {
		return nil, err
	}
	if in.TargetKind == "external_action" && authority.ApprovalStatus(in.Decision) == authority.ApprovalRevoked {
		if in.ExpectedActionVersion < 1 {
			return nil, errors.New("external action approval resolution requires expected_action_version")
		}
		return a.service.RevokeExternalActionApproval(ctx, app.RevokeExternalActionApprovalCommand{ApprovalID: in.ApprovalID, ActorID: in.ActorID, ExpectedActionVersion: in.ExpectedActionVersion, IdempotencyKey: in.IdempotencyKey, Rationale: in.Rationale})
	}
	if in.TargetKind == "external_action" {
		if in.ExpectedActionVersion < 1 {
			return nil, errors.New("external action approval resolution requires expected_action_version")
		}
		return a.service.ResolveExternalActionApproval(ctx, app.ResolveExternalActionApprovalCommand{ApprovalID: in.ApprovalID, ActorID: in.ActorID, ExpectedActionVersion: in.ExpectedActionVersion, IdempotencyKey: in.IdempotencyKey, Decision: authority.ApprovalStatus(in.Decision), Rationale: in.Rationale})
	}
	if in.TargetKind != "plan" && in.TargetKind != "work_item" && in.TargetKind != "output_profile" && in.TargetKind != "output_revision" {
		return nil, errors.New("resolve_approval target_kind must identify the approval target")
	}
	if in.ExpectedVersion < 1 {
		return nil, errors.New("generic approval resolution requires expected_version")
	}
	return a.service.ResolveApproval(ctx, app.ResolveApprovalCommand{ApprovalID: in.ApprovalID, ActorID: in.ActorID, IdempotencyKey: in.IdempotencyKey, ExpectedVersion: in.ExpectedVersion, Decision: work.ApprovalStatus(in.Decision), Rationale: in.Rationale})
}

func resolveApprovalSchema() map[string]any {
	result := schemaFor[resolveApprovalInput]()
	result["required"] = []string{}
	result["additionalProperties"] = false
	result["x-runtime-branch"] = true
	result["oneOf"] = []any{
		approvalResolutionBranch("plan"),
		approvalResolutionBranch("work_item"),
		approvalResolutionBranch("output_profile"),
		approvalResolutionBranch("output_revision"),
		map[string]any{"required": []string{"target_kind", "approval_id", "actor_id", "idempotency_key", "decision", "rationale", "expected_action_version"}, "properties": map[string]any{"target_kind": map[string]any{"const": "external_action"}}, "not": map[string]any{"required": []string{"expected_version"}}},
	}
	return result
}

func approvalResolutionBranch(kind string) map[string]any {
	return map[string]any{"required": []string{"target_kind", "approval_id", "actor_id", "idempotency_key", "decision", "rationale", "expected_version"}, "properties": map[string]any{"target_kind": map[string]any{"const": kind}}, "not": map[string]any{"required": []string{"expected_action_version"}}}
}

type blockItemInput struct {
	workspaceInput
	WorkItemID      string `json:"work_item_id"`
	ActorID         string `json:"actor_id"`
	IdempotencyKey  string `json:"idempotency_key"`
	ExpectedVersion int    `json:"expected_version"`
	Reason          string `json:"reason"`
}

func (a *adapter) blockItem(ctx context.Context, raw json.RawMessage) (any, error) {
	var in blockItemInput
	if err := decode(raw, &in); err != nil {
		return nil, err
	}
	return a.service.BlockWorkItem(ctx, app.BlockWorkItemCommand{WorkItemID: in.WorkItemID, ActorID: in.ActorID, IdempotencyKey: in.IdempotencyKey, ExpectedVersion: in.ExpectedVersion, Reason: in.Reason})
}

type unblockItemInput struct {
	workspaceInput
	BlockerID       string `json:"blocker_id"`
	ActorID         string `json:"actor_id"`
	IdempotencyKey  string `json:"idempotency_key"`
	ExpectedVersion int    `json:"expected_version"`
	Resolution      string `json:"resolution"`
}

func (a *adapter) unblockItem(ctx context.Context, raw json.RawMessage) (any, error) {
	var in unblockItemInput
	if err := decode(raw, &in); err != nil {
		return nil, err
	}
	return a.service.UnblockWorkItem(ctx, app.UnblockWorkItemCommand{BlockerID: in.BlockerID, ActorID: in.ActorID, IdempotencyKey: in.IdempotencyKey, ExpectedVersion: in.ExpectedVersion, Resolution: in.Resolution})
}

type transitionObjectiveInput struct {
	workspaceInput
	ObjectiveID     string              `json:"objective_id"`
	ActorID         string              `json:"actor_id"`
	Target          work.ObjectivePhase `json:"target_phase"`
	Reason          string              `json:"reason"`
	ExpectedVersion int                 `json:"expected_version"`
	IdempotencyKey  string              `json:"idempotency_key"`
}

func (a *adapter) transitionObjective(ctx context.Context, raw json.RawMessage) (any, error) {
	var in transitionObjectiveInput
	if err := decode(raw, &in); err != nil {
		return nil, err
	}
	return a.service.TransitionObjective(ctx, app.TransitionObjectiveCommand{ObjectiveID: in.ObjectiveID, ActorID: in.ActorID, TargetPhase: in.Target, Reason: in.Reason, ExpectedVersion: in.ExpectedVersion, IdempotencyKey: in.IdempotencyKey})
}

type planInput struct {
	workspaceInput
	ObjectiveID    string          `json:"objective_id"`
	ActorID        string          `json:"actor_id"`
	IdempotencyKey string          `json:"idempotency_key"`
	Title          string          `json:"title"`
	Summary        string          `json:"summary,omitempty"`
	Revision       int             `json:"revision"`
	Items          []planItemInput `json:"items"`
}

type planItemInput struct {
	ClientRef            string                         `json:"client_ref"`
	ParentRef            string                         `json:"parent_ref,omitempty"`
	Key                  string                         `json:"key"`
	Title                string                         `json:"title"`
	Description          string                         `json:"description,omitempty"`
	Kind                 string                         `json:"kind"`
	Priority             work.Priority                  `json:"priority"`
	EstimatedScope       work.EstimatedScope            `json:"estimated_scope"`
	ExecutionPolicy      work.ExecutionPolicy           `json:"execution_policy"`
	RequiredActorKind    work.ActorKind                 `json:"required_actor_kind"`
	RequiredCapabilities []string                       `json:"required_capabilities,omitempty"`
	DependsOn            []string                       `json:"depends_on,omitempty"`
	AcceptanceCriteria   []planAcceptanceCriterionInput `json:"acceptance_criteria,omitempty"`
	ExpectedOutputs      []planExpectedOutputInput      `json:"expected_outputs,omitempty"`
	OutputRequirements   []planOutputRequirementInput   `json:"output_requirements,omitempty"`
	ExternalActions      []planExternalActionInput      `json:"external_actions,omitempty"`
}

type planAcceptanceCriterionInput struct {
	Text     string `json:"text"`
	Required bool   `json:"required,omitempty"`
	Ordinal  int    `json:"ordinal,omitempty"`
}

type planExpectedOutputInput struct {
	Name            string          `json:"name"`
	ProfileName     string          `json:"profile"`
	ProfileVersion  int             `json:"profile_version"`
	Contract        json.RawMessage `json:"contract,omitempty"`
	DestinationHint string          `json:"destination_hint,omitempty"`
	Required        bool            `json:"required"`
	Ordinal         int             `json:"ordinal"`
}

type planOutputRequirementInput struct {
	RequiredOutputRevisionID string `json:"required_output_revision_id,omitempty"`
	RequiredProfileName      string `json:"required_profile_name,omitempty"`
	VersionConstraint        string `json:"version_constraint,omitempty"`
	Required                 bool   `json:"required,omitempty"`
	Note                     string `json:"note,omitempty"`
}

type planExternalActionInput struct {
	ClientRef              string                       `json:"client_ref,omitempty"`
	Required               bool                         `json:"required,omitempty"`
	Title                  string                       `json:"title"`
	Rationale              string                       `json:"rationale,omitempty"`
	ActionType             string                       `json:"action_type,omitempty"`
	Target                 json.RawMessage              `json:"target,omitempty"`
	Arguments              []authorizationArgumentInput `json:"arguments,omitempty"`
	Scope                  json.RawMessage              `json:"scope,omitempty"`
	Permissions            []string                     `json:"permissions,omitempty"`
	CredentialRequirements []string                     `json:"credential_requirements,omitempty"`
	Constraints            json.RawMessage              `json:"constraints,omitempty"`
	AuthorizationSubject   json.RawMessage              `json:"authorization_subject,omitempty"`
}

type authorizationArgumentInput struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

func externalActionSubject(action planExternalActionInput) (json.RawMessage, error) {
	if len(action.AuthorizationSubject) != 0 {
		return action.AuthorizationSubject, nil
	}
	return json.Marshal(map[string]any{
		"action_type":             action.ActionType,
		"target":                  action.Target,
		"arguments":               action.Arguments,
		"scope":                   action.Scope,
		"permissions":             action.Permissions,
		"credential_requirements": action.CredentialRequirements,
		"constraints":             action.Constraints,
	})
}

func (a *adapter) proposePlan(ctx context.Context, raw json.RawMessage) (any, error) {
	var in planInput
	if err := decode(raw, &in); err != nil {
		return nil, err
	}
	items := make([]app.ProposedWorkItem, 0, len(in.Items))
	for _, item := range in.Items {
		converted := app.ProposedWorkItem{ClientRef: item.ClientRef, ParentRef: item.ParentRef, Key: item.Key, Title: item.Title, Description: item.Description, Kind: item.Kind, Priority: item.Priority, EstimatedScope: item.EstimatedScope, ExecutionPolicy: item.ExecutionPolicy, RequiredActorKind: item.RequiredActorKind, RequiredCapabilities: item.RequiredCapabilities, DependsOn: item.DependsOn}
		for _, criterion := range item.AcceptanceCriteria {
			converted.AcceptanceCriteria = append(converted.AcceptanceCriteria, app.ProposedAcceptanceCriterion{Text: criterion.Text, Required: criterion.Required, Ordinal: criterion.Ordinal})
		}
		for _, expected := range item.ExpectedOutputs {
			converted.ExpectedOutputs = append(converted.ExpectedOutputs, app.ProposedExpectedOutput{Name: expected.Name, ProfileName: expected.ProfileName, ProfileVersion: expected.ProfileVersion, Contract: expected.Contract, DestinationHint: expected.DestinationHint, Required: expected.Required, Ordinal: expected.Ordinal})
		}
		for _, requirement := range item.OutputRequirements {
			converted.OutputRequirements = append(converted.OutputRequirements, app.ProposedOutputRequirement{RequiredOutputRevisionID: requirement.RequiredOutputRevisionID, RequiredProfileName: requirement.RequiredProfileName, VersionConstraint: requirement.VersionConstraint, Required: requirement.Required, Note: requirement.Note})
		}
		for _, action := range item.ExternalActions {
			subject, err := externalActionSubject(action)
			if err != nil {
				return nil, err
			}
			converted.ExternalActions = append(converted.ExternalActions, app.ProposedExternalAction{Required: action.Required, Title: action.Title, Rationale: action.Rationale, AuthorizationSubject: subject})
		}
		items = append(items, converted)
	}
	return a.service.ProposePlan(ctx, app.ProposePlanCommand{ObjectiveID: in.ObjectiveID, ActorID: in.ActorID, IdempotencyKey: in.IdempotencyKey, Title: in.Title, Summary: in.Summary, Revision: in.Revision, Items: items})
}

type reviewPlanInput struct {
	workspaceInput
	PlanID          string              `json:"plan_id"`
	ActorID         string              `json:"actor_id"`
	IdempotencyKey  string              `json:"idempotency_key"`
	Decision        work.PlanCommitment `json:"decision"`
	Reason          string              `json:"reason"`
	ExpectedVersion int                 `json:"expected_version"`
}

type recordContextInput struct {
	workspaceInput
	ObjectiveID    string             `json:"objective_id"`
	WorkItemID     string             `json:"work_item_id"`
	ActorID        string             `json:"actor_id"`
	IdempotencyKey string             `json:"idempotency_key"`
	Kind           work.ContextKind   `json:"kind"`
	Title          string             `json:"title"`
	Body           string             `json:"body"`
	Status         work.ContextStatus `json:"status"`
	Confidence     string             `json:"confidence"`
	SourceURI      string             `json:"source_uri"`
	SupersedesID   string             `json:"supersedes_id"`
}

func (a *adapter) recordContext(ctx context.Context, raw json.RawMessage) (any, error) {
	var in recordContextInput
	if err := decode(raw, &in); err != nil {
		return nil, err
	}
	return a.service.RecordContext(ctx, app.RecordContextCommand{ObjectiveID: in.ObjectiveID, WorkItemID: in.WorkItemID, ActorID: in.ActorID, IdempotencyKey: in.IdempotencyKey, Kind: in.Kind, Title: in.Title, Body: in.Body, Status: in.Status, Confidence: in.Confidence, SourceURI: in.SourceURI, SupersedesID: in.SupersedesID})
}

type recordDecisionInput struct {
	workspaceInput
	ObjectiveID    string   `json:"objective_id"`
	WorkItemID     string   `json:"work_item_id"`
	ActorID        string   `json:"actor_id"`
	IdempotencyKey string   `json:"idempotency_key"`
	Title          string   `json:"title"`
	Decision       string   `json:"decision"`
	Rationale      string   `json:"rationale"`
	Alternatives   []string `json:"alternatives"`
	SupersedesID   string   `json:"supersedes_id"`
}

func (a *adapter) recordDecision(ctx context.Context, raw json.RawMessage) (any, error) {
	var in recordDecisionInput
	if err := decode(raw, &in); err != nil {
		return nil, err
	}
	return a.service.RecordDecision(ctx, app.RecordDecisionCommand{ObjectiveID: in.ObjectiveID, WorkItemID: in.WorkItemID, ActorID: in.ActorID, IdempotencyKey: in.IdempotencyKey, Title: in.Title, Decision: in.Decision, Rationale: in.Rationale, Alternatives: in.Alternatives, SupersedesID: in.SupersedesID})
}

type askQuestionInput struct {
	workspaceInput
	ObjectiveID            string `json:"objective_id"`
	WorkItemID             string `json:"work_item_id"`
	ActorID                string `json:"actor_id"`
	IdempotencyKey         string `json:"idempotency_key"`
	Question               string `json:"question"`
	RequiresHumanAttention bool   `json:"requires_human_attention"`
}

func (a *adapter) askQuestion(ctx context.Context, raw json.RawMessage) (any, error) {
	var in askQuestionInput
	if err := decode(raw, &in); err != nil {
		return nil, err
	}
	return a.service.AskQuestion(ctx, app.AskQuestionCommand{ObjectiveID: in.ObjectiveID, WorkItemID: in.WorkItemID, ActorID: in.ActorID, IdempotencyKey: in.IdempotencyKey, Question: in.Question, RequiresHumanAttention: in.RequiresHumanAttention})
}

type answerQuestionInput struct {
	workspaceInput
	QuestionID      string `json:"question_id"`
	ActorID         string `json:"actor_id"`
	IdempotencyKey  string `json:"idempotency_key"`
	ExpectedVersion int    `json:"expected_version"`
	Answer          string `json:"answer"`
	WaiveReason     string `json:"waive_reason"`
}

func (a *adapter) answerQuestion(ctx context.Context, raw json.RawMessage) (any, error) {
	var in answerQuestionInput
	if err := decode(raw, &in); err != nil {
		return nil, err
	}
	if in.Answer != "" && in.WaiveReason != "" {
		return nil, errors.New("answer_question accepts either answer or waive_reason")
	}
	if in.WaiveReason != "" {
		return a.service.WaiveQuestion(ctx, app.WaiveQuestionCommand{QuestionID: in.QuestionID, ActorID: in.ActorID, IdempotencyKey: in.IdempotencyKey, Reason: in.WaiveReason, ExpectedVersion: in.ExpectedVersion})
	}
	return a.service.AnswerQuestion(ctx, app.AnswerQuestionCommand{QuestionID: in.QuestionID, ActorID: in.ActorID, IdempotencyKey: in.IdempotencyKey, Answer: in.Answer, ExpectedVersion: in.ExpectedVersion})
}

func (a *adapter) reviewPlan(ctx context.Context, raw json.RawMessage) (any, error) {
	var in reviewPlanInput
	if err := decode(raw, &in); err != nil {
		return nil, err
	}
	return a.service.ReviewPlan(ctx, app.ReviewPlanCommand{PlanID: in.PlanID, ReviewerActorID: in.ActorID, IdempotencyKey: in.IdempotencyKey, Decision: in.Decision, Reason: in.Reason, ExpectedVersion: in.ExpectedVersion})
}

type proposeOutputProfileInput struct {
	workspaceInput
	ActorID        string          `json:"actor_id"`
	IdempotencyKey string          `json:"idempotency_key"`
	Name           string          `json:"name"`
	Version        int             `json:"version"`
	Description    string          `json:"description"`
	Structure      json.RawMessage `json:"structure"`
	Semantics      json.RawMessage `json:"semantics"`
	Validation     json.RawMessage `json:"validation"`
	SupersedesID   string          `json:"supersedes_id"`
	Supersedes     string          `json:"supersedes"`
}

func (a *adapter) proposeOutputProfile(ctx context.Context, raw json.RawMessage) (any, error) {
	var in proposeOutputProfileInput
	if err := decode(raw, &in); err != nil {
		return nil, err
	}
	return a.service.ProposeOutputProfile(ctx, app.ProposeOutputProfileCommand{ActorID: in.ActorID, IdempotencyKey: in.IdempotencyKey, Name: in.Name, Version: in.Version, Description: in.Description, Structure: in.Structure, Semantics: in.Semantics, Validation: in.Validation, SupersedesID: in.SupersedesID, Supersedes: in.Supersedes})
}

type reviewOutputProfileInput struct {
	workspaceInput
	ProfileID       string              `json:"profile_id"`
	ActorID         string              `json:"actor_id"`
	IdempotencyKey  string              `json:"idempotency_key"`
	ExpectedVersion int                 `json:"expected_version"`
	Decision        output.ProfileState `json:"decision"`
	Reason          string              `json:"reason"`
}

func (a *adapter) reviewOutputProfile(ctx context.Context, raw json.RawMessage) (any, error) {
	var in reviewOutputProfileInput
	if err := decode(raw, &in); err != nil {
		return nil, err
	}
	return a.service.ReviewOutputProfile(ctx, app.ReviewOutputProfileCommand{ProfileID: in.ProfileID, ReviewerActorID: in.ActorID, IdempotencyKey: in.IdempotencyKey, ExpectedVersion: in.ExpectedVersion, Decision: in.Decision, Reason: in.Reason})
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

type claimRenewInput struct {
	workspaceInput
	WorkItemID      string `json:"work_item_id"`
	ClaimID         string `json:"claim_id"`
	ActorID         string `json:"actor_id"`
	ExpectedVersion int    `json:"expected_version"`
	IdempotencyKey  string `json:"idempotency_key"`
	LeaseSeconds    int    `json:"lease_seconds"`
}

func (a *adapter) renewClaim(ctx context.Context, raw json.RawMessage) (any, error) {
	var in claimRenewInput
	if err := decode(raw, &in); err != nil {
		return nil, err
	}
	return a.service.RenewClaim(ctx, app.RenewClaimCommand{WorkItemID: in.WorkItemID, ClaimID: in.ClaimID, ActorID: in.ActorID, ExpectedVersion: in.ExpectedVersion, IdempotencyKey: in.IdempotencyKey, Extension: time.Duration(in.LeaseSeconds) * time.Second})
}

type claimReleaseInput struct {
	workspaceInput
	WorkItemID      string `json:"work_item_id"`
	ClaimID         string `json:"claim_id"`
	ActorID         string `json:"actor_id"`
	ExpectedVersion int    `json:"expected_version"`
	IdempotencyKey  string `json:"idempotency_key"`
	Reason          string `json:"reason"`
	ReturnToReady   bool   `json:"return_to_ready"`
}

func (a *adapter) releaseClaim(ctx context.Context, raw json.RawMessage) (any, error) {
	var in claimReleaseInput
	if err := decode(raw, &in); err != nil {
		return nil, err
	}
	return a.service.ReleaseClaim(ctx, app.ReleaseClaimCommand{WorkItemID: in.WorkItemID, ClaimID: in.ClaimID, ActorID: in.ActorID, ExpectedVersion: in.ExpectedVersion, IdempotencyKey: in.IdempotencyKey, Reason: in.Reason, ReturnToReady: in.ReturnToReady})
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
	IdempotencyKey   string                    `json:"idempotency_key"`
	ContentDigest    string                    `json:"content_digest"`
	Artifacts        []app.OutputArtifactInput `json:"artifacts"`
}

func (a *adapter) createOutputRevision(ctx context.Context, raw json.RawMessage) (any, error) {
	var in outputRevisionInput
	if err := decode(raw, &in); err != nil {
		return nil, err
	}
	return a.service.CreateOutputRevision(ctx, app.CreateOutputRevisionCommand{ExpectedOutputID: in.ExpectedOutputID, ActorID: in.ActorID, IdempotencyKey: in.IdempotencyKey, ContentDigest: in.ContentDigest, Artifacts: in.Artifacts})
}

type validationInput struct {
	workspaceInput
	OutputRevisionID   string                   `json:"output_revision_id"`
	ActorID            string                   `json:"actor_id"`
	IdempotencyKey     string                   `json:"idempotency_key"`
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
	return a.service.RecordValidation(ctx, app.RecordValidationCommand{OutputRevisionID: in.OutputRevisionID, CriterionRef: in.CriterionRef, ValidatorKind: in.ValidatorKind, Verdict: in.Verdict, VerifierActorID: in.ActorID, IdempotencyKey: in.IdempotencyKey, EvidenceArtifactID: in.EvidenceArtifactID, Details: in.Details})
}

type outputRequirementInput struct {
	workspaceInput
	WorkItemID               string `json:"work_item_id"`
	ActorID                  string `json:"actor_id"`
	ExpectedVersion          int    `json:"expected_version"`
	IdempotencyKey           string `json:"idempotency_key"`
	RequiredOutputRevisionID string `json:"required_output_revision_id"`
	RequiredProfileName      string `json:"required_profile_name"`
	VersionConstraint        string `json:"version_constraint"`
	Required                 bool   `json:"required"`
	Note                     string `json:"note"`
}

func (a *adapter) addOutputRequirement(ctx context.Context, raw json.RawMessage) (any, error) {
	var in outputRequirementInput
	if err := decode(raw, &in); err != nil {
		return nil, err
	}
	return a.service.AddOutputRequirement(ctx, app.AddOutputRequirementCommand{
		WorkItemID: in.WorkItemID, ActorID: in.ActorID, ExpectedVersion: in.ExpectedVersion,
		IdempotencyKey: in.IdempotencyKey, RequiredOutputRevisionID: in.RequiredOutputRevisionID,
		RequiredProfileName: in.RequiredProfileName, VersionConstraint: in.VersionConstraint,
		Required: in.Required, Note: in.Note,
	})
}

type artifactInput struct {
	workspaceInput
	WorkItemID      string          `json:"work_item_id"`
	ActorID         string          `json:"actor_id"`
	ExpectedVersion int             `json:"expected_version"`
	IdempotencyKey  string          `json:"idempotency_key"`
	Kind            string          `json:"kind"`
	URI             string          `json:"uri"`
	Title           string          `json:"title"`
	Metadata        json.RawMessage `json:"metadata"`
}

func (a *adapter) attachArtifact(ctx context.Context, raw json.RawMessage) (any, error) {
	var in artifactInput
	if err := decode(raw, &in); err != nil {
		return nil, err
	}
	return a.service.AttachArtifact(ctx, app.AttachArtifactCommand{WorkItemID: in.WorkItemID, ActorID: in.ActorID, ExpectedVersion: in.ExpectedVersion, IdempotencyKey: in.IdempotencyKey, Kind: in.Kind, URI: in.URI, Title: in.Title, Metadata: in.Metadata})
}

type dependencyInput struct {
	workspaceInput
	WorkItemID          string              `json:"work_item_id"`
	DependsOnWorkItemID string              `json:"depends_on_work_item_id"`
	ActorID             string              `json:"actor_id"`
	ExpectedVersion     int                 `json:"expected_version"`
	IdempotencyKey      string              `json:"idempotency_key"`
	Kind                work.DependencyKind `json:"kind"`
	Note                string              `json:"note"`
}

func (a *adapter) linkDependency(ctx context.Context, raw json.RawMessage) (any, error) {
	var in dependencyInput
	if err := decode(raw, &in); err != nil {
		return nil, err
	}
	return a.service.LinkDependency(ctx, app.LinkDependencyCommand{WorkItemID: in.WorkItemID, DependsOnWorkItemID: in.DependsOnWorkItemID, ActorID: in.ActorID, ExpectedVersion: in.ExpectedVersion, IdempotencyKey: in.IdempotencyKey, Kind: in.Kind, Note: in.Note})
}

type unlinkDependencyInput struct {
	workspaceInput
	WorkItemID          string              `json:"work_item_id"`
	DependsOnWorkItemID string              `json:"depends_on_work_item_id"`
	ActorID             string              `json:"actor_id"`
	ExpectedVersion     int                 `json:"expected_version"`
	IdempotencyKey      string              `json:"idempotency_key"`
	Kind                work.DependencyKind `json:"kind"`
}

func (a *adapter) unlinkDependency(ctx context.Context, raw json.RawMessage) (any, error) {
	var in unlinkDependencyInput
	if err := decode(raw, &in); err != nil {
		return nil, err
	}
	return a.service.UnlinkDependency(ctx, app.UnlinkDependencyCommand{WorkItemID: in.WorkItemID, DependsOnWorkItemID: in.DependsOnWorkItemID, ActorID: in.ActorID, ExpectedVersion: in.ExpectedVersion, IdempotencyKey: in.IdempotencyKey, Kind: in.Kind})
}

type actionInput struct {
	workspaceInput
	WorkItemID     string `json:"work_item_id"`
	ActorID        string `json:"actor_id"`
	IdempotencyKey string `json:"idempotency_key"`
	Title          string `json:"title"`
	Rationale      string `json:"rationale"`
	Metadata       *struct {
		Title     string `json:"title"`
		Rationale string `json:"rationale"`
	} `json:"metadata"`
	ExpectedVersion int             `json:"expected_version"`
	Required        bool            `json:"required"`
	Subject         json.RawMessage `json:"authorization_subject"`
}

func (a *adapter) proposeAction(ctx context.Context, raw json.RawMessage) (any, error) {
	var in actionInput
	if err := decode(raw, &in); err != nil {
		return nil, err
	}
	if in.Metadata != nil {
		if in.Title != "" || in.Rationale != "" {
			return nil, errors.New("propose_external_action accepts metadata or flat title/rationale, not both")
		}
		in.Title, in.Rationale = in.Metadata.Title, in.Metadata.Rationale
	}
	if in.Title == "" {
		return nil, errors.New("propose_external_action requires metadata.title or title")
	}
	return a.service.ProposeExternalAction(ctx, app.ProposeExternalActionCommand{WorkItemID: in.WorkItemID, ActorID: in.ActorID, ExpectedVersion: in.ExpectedVersion, IdempotencyKey: in.IdempotencyKey, Required: in.Required, Title: in.Title, Rationale: in.Rationale, Subject: in.Subject})
}

func actionSchema() map[string]any {
	result := schemaFor[actionInput]("work_item_id", "actor_id", "expected_version", "idempotency_key", "authorization_subject")
	result["x-runtime-branch"] = true
	result["oneOf"] = []any{
		map[string]any{"required": []string{"title"}, "not": map[string]any{"required": []string{"metadata"}}},
		map[string]any{"required": []string{"metadata"}, "not": map[string]any{"required": []string{"title"}}},
	}
	return result
}

type patchActionMetadataInput struct {
	workspaceInput
	ActionID              string `json:"action_id"`
	ActorID               string `json:"actor_id"`
	ExpectedActionVersion int    `json:"expected_action_version"`
	IdempotencyKey        string `json:"idempotency_key"`
	Metadata              *struct {
		Title     *string `json:"title"`
		Rationale *string `json:"rationale"`
	} `json:"metadata"`
}

func (a *adapter) patchActionMetadata(ctx context.Context, raw json.RawMessage) (any, error) {
	var in patchActionMetadataInput
	if err := decode(raw, &in); err != nil {
		return nil, err
	}
	if in.Metadata == nil || (in.Metadata.Title == nil && in.Metadata.Rationale == nil) {
		return nil, errors.New("patch_external_action_metadata requires title or rationale metadata")
	}
	return a.service.PatchExternalActionMetadata(ctx, app.PatchExternalActionMetadataCommand{ActionID: in.ActionID, ActorID: in.ActorID, ExpectedActionVersion: in.ExpectedActionVersion, IdempotencyKey: in.IdempotencyKey, Title: in.Metadata.Title, Rationale: in.Metadata.Rationale})
}

type reviseActionInput struct {
	workspaceInput
	ActionID                string          `json:"action_id"`
	ActorID                 string          `json:"actor_id"`
	ExpectedActionVersion   int             `json:"expected_action_version"`
	ExpectedWorkItemVersion int             `json:"expected_work_item_version"`
	IdempotencyKey          string          `json:"idempotency_key"`
	Subject                 json.RawMessage `json:"authorization_subject"`
}

func (a *adapter) reviseAction(ctx context.Context, raw json.RawMessage) (any, error) {
	var in reviseActionInput
	if err := decode(raw, &in); err != nil {
		return nil, err
	}
	return a.service.ReviseExternalAction(ctx, app.ReviseExternalActionCommand{ActionID: in.ActionID, ActorID: in.ActorID, ExpectedActionVersion: in.ExpectedActionVersion, ExpectedWorkItemVersion: in.ExpectedWorkItemVersion, IdempotencyKey: in.IdempotencyKey, Subject: in.Subject})
}

type requestActionApprovalInput struct {
	workspaceInput
	ActionID                 string          `json:"action_id"`
	ActorID                  string          `json:"actor_id"`
	ApprovedForActorID       string          `json:"approved_for_actor_id"`
	ExpectedActionVersion    int             `json:"expected_action_version"`
	AuthorizationSubjectHash string          `json:"authorization_subject_hash"`
	IdempotencyKey           string          `json:"idempotency_key"`
	Constraints              json.RawMessage `json:"constraints"`
	ExpiresAt                string          `json:"expires_at"`
	Request                  string          `json:"request"`
}

func (a *adapter) requestActionApproval(ctx context.Context, raw json.RawMessage) (any, error) {
	var in requestActionApprovalInput
	if err := decode(raw, &in); err != nil {
		return nil, err
	}
	expiresAt, err := parseOptionalTime(in.ExpiresAt)
	if err != nil {
		return nil, err
	}
	if in.AuthorizationSubjectHash == "" {
		return nil, errors.New("request_action_approval requires authorization_subject_hash")
	}
	return a.service.RequestExternalActionApproval(ctx, app.RequestExternalActionApprovalCommand{ActionID: in.ActionID, ActorID: in.ActorID, ApprovedForActorID: in.ApprovedForActorID, ExpectedActionVersion: in.ExpectedActionVersion, ExpectedSubjectHash: in.AuthorizationSubjectHash, IdempotencyKey: in.IdempotencyKey, Constraints: in.Constraints, ExpiresAt: expiresAt, Request: in.Request})
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
	if err := validateExecutionBranch(raw, in.State); err != nil {
		return nil, err
	}
	if in.State == authority.ExecutionStarted {
		return a.service.StartExternalActionExecution(ctx, app.StartExternalActionExecutionCommand{ActionID: in.ActionID, ActorID: in.ActorID, ExpectedActionVersion: in.ExpectedActionVersion, IdempotencyKey: in.IdempotencyKey, SubjectHash: in.SubjectHash, AuthorityGrantID: in.AuthorityGrantID})
	}
	return a.service.CompleteExternalActionExecution(ctx, app.CompleteExternalActionExecutionCommand{ExecutionID: in.ExecutionID, ActorID: in.ActorID, ExpectedActionVersion: in.ExpectedActionVersion, IdempotencyKey: in.IdempotencyKey, State: in.State, Result: in.Result, EvidenceArtifactID: in.EvidenceArtifactID})
}

func validateExecutionBranch(raw json.RawMessage, state authority.ExecutionState) error {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		return err
	}
	has := func(name string) bool {
		value, ok := fields[name]
		return ok && !bytes.Equal(bytes.TrimSpace(value), []byte("null"))
	}
	for _, name := range []string{"actor_id", "idempotency_key", "expected_action_version", "state"} {
		if !has(name) {
			return fmt.Errorf("execution requires %q", name)
		}
	}
	if state == authority.ExecutionStarted {
		for _, name := range []string{"action_id", "subject_hash", "authority_grant_id"} {
			if !has(name) {
				return fmt.Errorf("start execution requires %q", name)
			}
		}
		for _, name := range []string{"execution_id", "result", "evidence_artifact_id"} {
			if has(name) {
				return fmt.Errorf("start execution forbids %q", name)
			}
		}
		return nil
	}
	if state != authority.ExecutionSucceeded && state != authority.ExecutionFailed {
		return errors.New("execution state must be started, succeeded, or failed")
	}
	for _, name := range []string{"execution_id", "result", "evidence_artifact_id"} {
		if !has(name) {
			return fmt.Errorf("terminal execution requires %q", name)
		}
	}
	var evidenceArtifactID string
	if err := json.Unmarshal(fields["evidence_artifact_id"], &evidenceArtifactID); err != nil || strings.TrimSpace(evidenceArtifactID) == "" {
		return errors.New("terminal execution requires a non-empty evidence_artifact_id")
	}
	for _, name := range []string{"action_id", "subject_hash", "authority_grant_id"} {
		if has(name) {
			return fmt.Errorf("terminal execution forbids %q", name)
		}
	}
	return nil
}
