package dashboard

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/dennisschroeder/throughline/internal/app"
	"github.com/dennisschroeder/throughline/internal/domain/work"
	"github.com/dennisschroeder/throughline/internal/ports"
)

// ItemDetailHandler serves the read-only detail view for a single work item, fetched lazily
// when a board card with no open gate is clicked. It reuses service.GetWorkItem — the same
// app-layer read the MCP get_item tool calls — so this endpoint cannot drift from that
// tool's notion of a work item's full context, and reuses service.ListWorkItems (already
// called for the loop snapshot) to resolve dependency neighbours' titles and to compute the
// reverse "required_by" edge, which WorkItemContext does not carry directly.
func (h *Handlers) ItemDetailHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !h.hostAllowed(r) {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		session, err := h.sessionFromRequest(r)
		if err != nil {
			http.Error(w, err.Error(), http.StatusUnauthorized)
			return
		}
		id := strings.TrimSpace(r.URL.Query().Get("id"))
		if id == "" {
			http.Error(w, "missing id", http.StatusBadRequest)
			return
		}

		service, err := h.router.Service(r.Context(), session.WorkspaceID)
		if err != nil {
			h.logError("resolve service for item detail", err)
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}

		detail, err := buildItemDetail(r.Context(), service, id, h.now())
		if err != nil {
			if errors.Is(err, ports.ErrNotFound) {
				http.Error(w, "not found", http.StatusNotFound)
				return
			}
			h.logError("build item detail", err)
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}
		writeJSON(w, http.StatusOK, detail)
	})
}

// buildItemDetail projects one work item's full ports.WorkItemContext (the same read
// service.GetWorkItem — and therefore the MCP get_item tool — returns) into the dashboard's
// read-only detail-drawer wire shape. It also lists every work item in the workspace once
// (already the loop snapshot's own read path) purely to label dependency neighbours with
// their key/title and to compute the reverse "required_by" edge, which WorkItemContext does
// not carry itself.
func buildItemDetail(ctx context.Context, service *app.Service, id string, now time.Time) (itemDetail, error) {
	item, err := service.GetWorkItem(ctx, id)
	if err != nil {
		return itemDetail{}, fmt.Errorf("get work item: %w", err)
	}

	all, err := service.ListWorkItems(ctx)
	if err != nil {
		return itemDetail{}, fmt.Errorf("list work items: %w", err)
	}
	byID := make(map[string]ports.WorkItemContext, len(all))
	for _, other := range all {
		byID[other.WorkItem.ID] = other
	}

	hasGate := item.WorkItem.AttentionState != work.AttentionNone

	detail := itemDetail{
		WorkItem: itemWorkItemView{
			ID:                item.WorkItem.ID,
			Key:               item.WorkItem.Key,
			Title:             item.WorkItem.Title,
			Description:       item.WorkItem.Description,
			Kind:              item.WorkItem.Kind,
			CommitmentState:   string(item.WorkItem.CommitmentState),
			ExecutionStatus:   string(item.WorkItem.ExecutionStatus),
			Priority:          string(item.WorkItem.Priority),
			AttentionState:    string(item.WorkItem.AttentionState),
			ExecutionPolicy:   string(item.WorkItem.ExecutionPolicy),
			RequiredActorKind: string(item.WorkItem.RequiredActorKind),
			ObjectiveID:       item.WorkItem.ObjectiveID,
			Version:           item.WorkItem.Version,
		},
		ReadOnly: !hasGate,
	}

	for _, ac := range item.AcceptanceCriteria {
		detail.AcceptanceCriteria = append(detail.AcceptanceCriteria, acceptanceCriterionView{
			Text:                ac.Text,
			Required:            ac.Required,
			Status:              string(ac.Status),
			ResolvedBy:          ac.ResolvedBy,
			ResolvedAt:          formatOptionalTime(ac.ResolvedAt),
			ResolutionRationale: ac.ResolutionRationale,
		})
	}

	for _, dep := range item.Dependencies {
		dv := dependencyView{
			ItemID: dep.DependsOnItemID,
			Kind:   string(dep.Kind),
			Note:   dep.Note,
		}
		if target, ok := byID[dep.DependsOnItemID]; ok {
			dv.Key = target.WorkItem.Key
			dv.Title = target.WorkItem.Title
			dv.ExecutionStatus = string(target.WorkItem.ExecutionStatus)
			if dep.Kind == work.DependencyHard {
				satisfied := target.WorkItem.ExecutionStatus == work.StatusDone
				dv.Satisfied = &satisfied
			}
		}
		detail.DependsOn = append(detail.DependsOn, dv)
	}
	for _, other := range all {
		for _, dep := range other.Dependencies {
			if dep.DependsOnItemID != id {
				continue
			}
			detail.RequiredBy = append(detail.RequiredBy, dependencyView{
				ItemID:          other.WorkItem.ID,
				Key:             other.WorkItem.Key,
				Title:           other.WorkItem.Title,
				Kind:            string(dep.Kind),
				Note:            dep.Note,
				ExecutionStatus: string(other.WorkItem.ExecutionStatus),
			})
		}
	}

	// Newest first: Progress arrives oldest-first from the store (append order), the drawer
	// reads top-to-bottom as a log, most recent entry on top.
	for i := len(item.Progress) - 1; i >= 0; i-- {
		p := item.Progress[i]
		detail.Progress = append(detail.Progress, progressView{
			ActorID:    p.ActorID,
			Summary:    p.Summary,
			Completed:  p.Completed,
			Remaining:  p.Remaining,
			Discovered: p.Discovered,
			Blocker:    p.Blocker,
			CreatedAt:  p.CreatedAt.UTC().Format(time.RFC3339),
		})
	}

	for _, eo := range item.ExpectedOutputs {
		detail.ExpectedOutputs = append(detail.ExpectedOutputs, expectedOutputView{
			Name:            eo.ExpectedOutput.Name,
			ProfileName:     eo.Profile.Name,
			ProfileVersion:  eo.Profile.Version,
			Required:        eo.ExpectedOutput.Required,
			DestinationHint: eo.ExpectedOutput.DestinationHint,
		})
	}

	for _, rev := range item.OutputRevisions {
		rv := outputRevisionView{
			Revision:        rev.Revision.Revision,
			AcceptanceState: string(rev.Revision.AcceptanceState),
			ProducedBy:      rev.Revision.ProducedBy,
			ProducedAt:      formatOptionalTime(rev.Revision.ProducedAt),
			AcceptedBy:      rev.Revision.AcceptedBy,
			AcceptedAt:      formatOptionalTime(rev.Revision.AcceptedAt),
		}
		for _, v := range rev.Validations {
			rv.Validations = append(rv.Validations, validationView{
				CriterionRef:  v.CriterionRef,
				ValidatorKind: string(v.ValidatorKind),
				Verdict:       string(v.Verdict),
				VerifierActor: v.VerifierActorID,
				CreatedAt:     v.CreatedAt.UTC().Format(time.RFC3339),
			})
		}
		detail.OutputRevisions = append(detail.OutputRevisions, rv)
	}

	for _, ea := range item.ExternalActions {
		av := externalActionView{
			ActionID:      ea.Action.ID,
			ActionVersion: ea.Action.Version,
			ActionType:    ea.Action.ActionType,
			Title:         ea.Action.Title,
			Rationale:     ea.Action.Rationale,
			State:         string(ea.Action.State),
			Required:      ea.Action.Required,
		}
		for _, g := range ea.Grants {
			av.Grants = append(av.Grants, authorityGrantView{
				ID:               g.ID,
				SourceApprovalID: g.SourceApprovalID,
				PrincipalActorID: g.PrincipalActorID,
				GrantedBy:        g.GrantedBy,
				GrantedAt:        formatOptionalTime(g.GrantedAt),
				Revoked:          g.RevokedAt != nil,
			})
		}
		for _, x := range ea.Executions {
			av.Executions = append(av.Executions, externalActionExecutionView{
				State:      string(x.State),
				StartedAt:  formatOptionalTime(x.StartedAt),
				FinishedAt: formatOptionalTime(x.FinishedAt),
			})
		}
		detail.ExternalActions = append(detail.ExternalActions, av)
	}

	for _, artifact := range item.Artifacts {
		detail.Artifacts = append(detail.Artifacts, artifactView{
			Kind:       artifact.Kind,
			URI:        artifact.URI,
			Title:      artifact.Title,
			AttachedBy: artifact.AttachedBy,
			CreatedAt:  artifact.CreatedAt.UTC().Format(time.RFC3339),
		})
	}

	for _, claim := range item.Claims {
		if !claim.ReleasedAt.IsZero() {
			continue
		}
		detail.Claim = &claimView{
			ActorID:          claim.ActorID,
			AcquiredAt:       claim.AcquiredAt.UTC().Format(time.RFC3339),
			ExpiresAt:        claim.ExpiresAt.UTC().Format(time.RFC3339),
			SecondsRemaining: int64(claim.ExpiresAt.Sub(now).Seconds()),
			Expired:          !claim.ExpiresAt.After(now),
		}
		break
	}

	return detail, nil
}

type itemDetail struct {
	WorkItem           itemWorkItemView          `json:"work_item"`
	AcceptanceCriteria []acceptanceCriterionView `json:"acceptance_criteria"`
	DependsOn          []dependencyView          `json:"depends_on"`
	RequiredBy         []dependencyView          `json:"required_by"`
	Progress           []progressView            `json:"progress"`
	ExpectedOutputs    []expectedOutputView      `json:"expected_outputs"`
	OutputRevisions    []outputRevisionView      `json:"output_revisions"`
	ExternalActions    []externalActionView      `json:"external_actions"`
	Artifacts          []artifactView            `json:"artifacts"`
	Claim              *claimView                `json:"claim,omitempty"`
	// ReadOnly is true when this item has no open gate — the drawer then shows the
	// read-only footer ("No open gate on this item...") instead of decision buttons.
	ReadOnly bool `json:"read_only"`
}

type itemWorkItemView struct {
	ID                string `json:"id"`
	Key               string `json:"key"`
	Title             string `json:"title"`
	Description       string `json:"description"`
	Kind              string `json:"kind"`
	CommitmentState   string `json:"commitment_state"`
	ExecutionStatus   string `json:"execution_status"`
	Priority          string `json:"priority"`
	AttentionState    string `json:"attention_state"`
	ExecutionPolicy   string `json:"execution_policy"`
	RequiredActorKind string `json:"required_actor_kind"`
	ObjectiveID       string `json:"objective_id"`
	Version           int    `json:"version"`
}

type acceptanceCriterionView struct {
	Text                string `json:"text"`
	Required            bool   `json:"required"`
	Status              string `json:"status"`
	ResolvedBy          string `json:"resolved_by,omitempty"`
	ResolvedAt          string `json:"resolved_at,omitempty"`
	ResolutionRationale string `json:"resolution_rationale,omitempty"`
}

type dependencyView struct {
	ItemID          string `json:"item_id"`
	Key             string `json:"key,omitempty"`
	Title           string `json:"title,omitempty"`
	Kind            string `json:"kind"`
	Note            string `json:"note,omitempty"`
	ExecutionStatus string `json:"execution_status,omitempty"`
	Satisfied       *bool  `json:"satisfied,omitempty"`
}

type progressView struct {
	ActorID    string   `json:"actor_id"`
	Summary    string   `json:"summary"`
	Completed  []string `json:"completed,omitempty"`
	Remaining  []string `json:"remaining,omitempty"`
	Discovered []string `json:"discovered,omitempty"`
	Blocker    string   `json:"blocker,omitempty"`
	CreatedAt  string   `json:"created_at"`
}

type expectedOutputView struct {
	Name            string `json:"name"`
	ProfileName     string `json:"profile_name"`
	ProfileVersion  int    `json:"profile_version"`
	Required        bool   `json:"required"`
	DestinationHint string `json:"destination_hint,omitempty"`
}

type outputRevisionView struct {
	Revision        int              `json:"revision"`
	AcceptanceState string           `json:"acceptance_state"`
	ProducedBy      string           `json:"produced_by"`
	ProducedAt      string           `json:"produced_at,omitempty"`
	AcceptedBy      string           `json:"accepted_by,omitempty"`
	AcceptedAt      string           `json:"accepted_at,omitempty"`
	Validations     []validationView `json:"validations,omitempty"`
}

type validationView struct {
	CriterionRef  string `json:"criterion_ref"`
	ValidatorKind string `json:"validator_kind"`
	Verdict       string `json:"verdict"`
	VerifierActor string `json:"verifier_actor_id,omitempty"`
	CreatedAt     string `json:"created_at"`
}

type externalActionView struct {
	ActionID      string                        `json:"action_id"`
	ActionVersion int                           `json:"action_version"`
	ActionType    string                        `json:"action_type"`
	Title         string                        `json:"title"`
	Rationale     string                        `json:"rationale,omitempty"`
	State         string                        `json:"state"`
	Required      bool                          `json:"required"`
	Grants        []authorityGrantView          `json:"grants,omitempty"`
	Executions    []externalActionExecutionView `json:"executions,omitempty"`
}

// authorityGrantView carries enough of authority.AuthorityGrant for the drawer's "Revoke"
// affordance to build a resolve_action_approval copy-prompt (decision="revoked") without a
// second round-trip: SourceApprovalID is the approval_id that tool call targets.
type authorityGrantView struct {
	ID               string `json:"id"`
	SourceApprovalID string `json:"source_approval_id"`
	PrincipalActorID string `json:"principal_actor_id"`
	GrantedBy        string `json:"granted_by"`
	GrantedAt        string `json:"granted_at,omitempty"`
	Revoked          bool   `json:"revoked"`
}

type externalActionExecutionView struct {
	State      string `json:"state"`
	StartedAt  string `json:"started_at,omitempty"`
	FinishedAt string `json:"finished_at,omitempty"`
}

type artifactView struct {
	Kind       string `json:"kind"`
	URI        string `json:"uri"`
	Title      string `json:"title,omitempty"`
	AttachedBy string `json:"attached_by"`
	CreatedAt  string `json:"created_at"`
}

type claimView struct {
	ActorID          string `json:"actor_id"`
	AcquiredAt       string `json:"acquired_at"`
	ExpiresAt        string `json:"expires_at"`
	SecondsRemaining int64  `json:"seconds_remaining"`
	Expired          bool   `json:"expired"`
}
