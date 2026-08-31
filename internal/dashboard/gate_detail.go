package dashboard

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/dennisschroeder/throughline/internal/app"
	"github.com/dennisschroeder/throughline/internal/domain/work"
	"github.com/dennisschroeder/throughline/internal/ports"
)

// GateDetailHandler serves the drawer's per-gate detail (ask text, evidence, facts,
// dependencies, activity), fetched lazily when a gate row or a gated board card is opened —
// never folded into /loop, exactly like the item detail endpoint it sits next to.
func (h *Handlers) GateDetailHandler() http.Handler {
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
		kind := strings.TrimSpace(r.URL.Query().Get("kind"))
		id := strings.TrimSpace(r.URL.Query().Get("id"))
		objectiveID := strings.TrimSpace(r.URL.Query().Get("objective_id"))
		if kind == "" || id == "" || objectiveID == "" {
			http.Error(w, "missing kind, id, or objective_id", http.StatusBadRequest)
			return
		}
		service, err := h.router.Service(r.Context(), session.WorkspaceID)
		if err != nil {
			h.logError("resolve service for gate detail", err)
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}
		detail, err := buildGateDetail(r.Context(), service, kind, id, objectiveID, h.now())
		if err != nil {
			h.logError("build gate detail", err)
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		writeJSON(w, http.StatusOK, detail)
	})
}

// buildGateDetail re-derives the one gate identified by (kind, id) fresh — rather than
// caching the queue's Gate — and layers on the drawer-only sections. Re-deriving from the
// same buildGates path the queue uses guarantees the drawer can never show a decision the
// queue would disagree about.
func buildGateDetail(ctx context.Context, service *app.Service, kind, id, objectiveID string, now time.Time) (GateDetail, error) {
	objCtx, err := service.GetObjectiveContext(ctx, objectiveID)
	if err != nil {
		return GateDetail{}, fmt.Errorf("get objective context: %w", err)
	}
	items, err := service.ListWorkItems(ctx)
	if err != nil {
		return GateDetail{}, fmt.Errorf("list work items: %w", err)
	}
	activity, err := service.ListActivity(ctx, app.ActivityFilter{ObjectiveID: objectiveID, Limit: 500})
	if err != nil {
		return GateDetail{}, fmt.Errorf("list activity: %w", err)
	}
	al := buildActorLiveness(activity, now)
	gates, err := buildGates(ctx, service, objCtx, items, al, now)
	if err != nil {
		return GateDetail{}, fmt.Errorf("build gates: %w", err)
	}
	var gate *Gate
	for i := range gates {
		if gates[i].Kind == kind && gates[i].ID == id {
			gate = &gates[i]
			break
		}
	}
	if gate == nil {
		return GateDetail{}, fmt.Errorf("gate %s/%s not found (may have just been decided)", kind, id)
	}

	detail := GateDetail{Gate: *gate}
	switch kind {
	case "plan":
		detail.Ask, detail.Evidence, detail.Facts, detail.Activity = planGateSections(objCtx, *gate, activity, now)
	case "approval":
		detail.Ask, detail.Evidence, detail.Facts, detail.Activity = approvalGateSections(objCtx, items, *gate, activity, now)
	case "question":
		detail.Ask, detail.Evidence, detail.Facts, detail.Activity = questionGateSections(objCtx, *gate, activity, now)
	case "attention":
		detail.Ask, detail.Evidence, detail.Facts, detail.Activity = attentionGateSections(items, *gate, activity, now)
	case "action_approval":
		detail.Ask, detail.Evidence, detail.Facts, detail.Activity, err = actionApprovalGateSections(ctx, service, items, *gate, activity, now)
	case "output_profile":
		detail.Ask, detail.Evidence, detail.Facts, detail.Activity, err = outputProfileGateSections(ctx, service, *gate, activity, now)
	default:
		detail.Ask = gate.Title
		detail.Evidence = Evidence{Label: "Evidence", Kind: "text", Text: gate.EvidenceHint}
	}
	if err != nil {
		return GateDetail{}, err
	}
	return detail, nil
}

func activityRows(activity []work.Activity, entityID string, now time.Time, limit int) []ActivityRow {
	var rows []ActivityRow
	for i := len(activity) - 1; i >= 0 && len(rows) < limit; i-- {
		a := activity[i]
		if entityID != "" && a.EntityID != entityID {
			continue
		}
		rows = append(rows, ActivityRow{Age: ageLabel(a.CreatedAt, now), Event: a.EventType, Actor: a.ActorID, At: formatOptionalTime(a.CreatedAt)})
	}
	return rows
}

func planGateSections(objCtx ports.ObjectiveContext, gate Gate, activity []work.Activity, now time.Time) (string, Evidence, []Fact, []ActivityRow) {
	var plan *work.Plan
	var previous *work.Plan
	for i, pc := range objCtx.Plans {
		if pc.Plan.ID == gate.TargetID {
			plan = &objCtx.Plans[i].Plan
		}
	}
	for i, pc := range objCtx.Plans {
		if pc.Plan.CommitmentState == work.PlanApproved && (previous == nil || pc.Plan.Revision > previous.Revision) {
			previous = &objCtx.Plans[i].Plan
		}
	}
	ask := fmt.Sprintf("Approve or reject the proposed plan %q for objective %q. Approving commits its items; rejecting requires a rationale and leaves the objective without an approved plan.", gate.Title, objCtx.Objective.Title)
	newText := plan.Title + "\n" + plan.Summary
	oldText := ""
	supersedes := "-"
	if previous != nil {
		oldText = previous.Title + "\n" + previous.Summary
		supersedes = previous.ID
	}
	evidence := Evidence{Label: "Evidence", Meta: fmt.Sprintf("revision %d", plan.Revision), Kind: "diff", Diff: lineDiff(oldText, newText)}
	facts := []Fact{
		{"target", plan.ID},
		{"version", fmt.Sprint(plan.Version)},
		{"requester", plan.ProposedBy + " · " + ageLabel(plan.ProposedAt, now)},
		{"attention state", "-"},
		{"profile", "-"},
		{"objective", objCtx.Objective.Title},
		{"supersedes", supersedes},
		{"claim", "-"},
	}
	return ask, evidence, facts, activityRows(activity, plan.ID, now, 4)
}

func approvalGateSections(objCtx ports.ObjectiveContext, items []ports.WorkItemContext, gate Gate, activity []work.Activity, now time.Time) (string, Evidence, []Fact, []ActivityRow) {
	var approval *work.Approval
	for i := range objCtx.Approvals {
		if objCtx.Approvals[i].ID == gate.ID {
			approval = &objCtx.Approvals[i]
		}
	}
	ask := fmt.Sprintf("Approve or reject: %q, targeting %s %s.", gate.Title, gate.TargetKind, gate.TargetID)
	text := gate.Title
	if approval != nil && approval.Request != "" {
		text = approval.Request
	}
	evidence := Evidence{Label: "Evidence", Meta: gate.TargetKind, Kind: "text", Text: text}
	facts := []Fact{
		{"target", gate.TargetID},
		{"version", fmt.Sprint(gate.ExpectedVersion)},
		{"requester", gate.Requester + " · " + ageLabel(parseTime(gate.RequestedAt), now)},
		{"attention state", "-"},
		{"profile", "-"},
		{"objective", objCtx.Objective.Title},
		{"supersedes", "-"},
		{"claim", claimFact(items, gate.WorkItemID, now)},
	}
	return ask, evidence, facts, activityRows(activity, gate.ID, now, 4)
}

func questionGateSections(objCtx ports.ObjectiveContext, gate Gate, activity []work.Activity, now time.Time) (string, Evidence, []Fact, []ActivityRow) {
	var question *work.Question
	for i := range objCtx.Questions {
		if objCtx.Questions[i].ID == gate.ID {
			question = &objCtx.Questions[i]
		}
	}
	ask := fmt.Sprintf("Answer or waive: %q. Waiving requires a rationale and leaves the question open-ended for whatever proceeds without it.", gate.Title)
	meta := "open"
	if question != nil && question.RequiresHumanAttention {
		meta = "flagged for human attention"
	}
	evidence := Evidence{Label: "Evidence", Meta: meta, Kind: "text", Text: gate.Title}
	facts := []Fact{
		{"target", gate.TargetID},
		{"version", fmt.Sprint(gate.ExpectedVersion)},
		{"requester", gate.Requester + " · " + ageLabel(parseTime(gate.RequestedAt), now)},
		{"attention state", meta},
		{"profile", "-"},
		{"objective", objCtx.Objective.Title},
		{"supersedes", "-"},
		{"claim", "-"},
	}
	return ask, evidence, facts, activityRows(activity, gate.ID, now, 4)
}

func attentionGateSections(items []ports.WorkItemContext, gate Gate, activity []work.Activity, now time.Time) (string, Evidence, []Fact, []ActivityRow) {
	var item *ports.WorkItemContext
	for i := range items {
		if items[i].WorkItem.ID == gate.TargetID {
			item = &items[i]
		}
	}
	ask := fmt.Sprintf("This item %s. Acknowledge to clear the attention flag once you've looked.", gate.EvidenceHint)
	var criteria []CriterionRow
	if item != nil {
		for _, ac := range item.AcceptanceCriteria {
			criteria = append(criteria, CriterionRow{Text: ac.Text, Passed: ac.Status == work.AcceptanceSatisfied, Status: string(ac.Status)})
		}
	}
	evidence := Evidence{Label: "Evidence", Meta: gate.EvidenceHint, Kind: "criteria", Criteria: criteria}
	claim := "-"
	if item != nil {
		claim = claimFact(items, item.WorkItem.ID, now)
	}
	facts := []Fact{
		{"target", gate.TargetID},
		{"version", fmt.Sprint(gate.ExpectedVersion)},
		{"requester", gate.Requester + " · " + ageLabel(parseTime(gate.RequestedAt), now)},
		{"attention state", gate.EvidenceHint},
		{"profile", "-"},
		{"objective", gate.ObjectiveID},
		{"supersedes", "-"},
		{"claim", claim},
	}
	return ask, evidence, facts, activityRows(activity, gate.TargetID, now, 4)
}

func actionApprovalGateSections(ctx context.Context, service *app.Service, items []ports.WorkItemContext, gate Gate, activity []work.Activity, now time.Time) (string, Evidence, []Fact, []ActivityRow, error) {
	revision, err := service.GetCurrentExternalActionRevision(ctx, gate.TargetID)
	if err != nil {
		return "", Evidence{}, nil, nil, fmt.Errorf("get current external action revision: %w", err)
	}
	pretty := prettyJSON(revision.AuthorizationSubject)
	ask := fmt.Sprintf("Approve or reject the external action %q for the current authorization subject. Approving grants the requested actor authority to execute it; rejecting requires a rationale.", gate.Title)
	evidence := Evidence{Label: "Evidence", Meta: fmt.Sprintf("revision %d · subject hash %s", revision.Revision, truncate(revision.AuthorizationSubjectHash, 16)), Kind: "text", Text: pretty}
	facts := []Fact{
		{"target", gate.TargetID},
		{"version", fmt.Sprint(gate.ExpectedActionVersion)},
		{"requester", gate.Requester + " · " + ageLabel(parseTime(gate.RequestedAt), now)},
		{"attention state", "-"},
		{"profile", "-"},
		{"objective", gate.ObjectiveID},
		{"supersedes", "-"},
		{"claim", claimFact(items, gate.WorkItemID, now)},
	}
	return ask, evidence, facts, activityRows(activity, gate.ID, now, 4), nil
}

func outputProfileGateSections(ctx context.Context, service *app.Service, gate Gate, activity []work.Activity, now time.Time) (string, Evidence, []Fact, []ActivityRow, error) {
	profile, err := service.GetOutputProfileByID(ctx, gate.TargetID)
	if err != nil {
		return "", Evidence{}, nil, nil, fmt.Errorf("get output profile: %w", err)
	}
	ask := fmt.Sprintf("Activate or reject the proposed output profile %q v%d. Activating makes it the governing contract new expected outputs bind to; rejecting requires a rationale.", profile.Name, profile.Version)
	newText := prettyJSON(profile.Structure) + "\n" + prettyJSON(profile.Semantics)
	oldText := ""
	if profile.SupersedesID != "" {
		if prev, err := service.GetOutputProfileByID(ctx, profile.SupersedesID); err == nil {
			oldText = prettyJSON(prev.Structure) + "\n" + prettyJSON(prev.Semantics)
		}
	}
	evidence := Evidence{Label: "Evidence", Meta: fmt.Sprintf("version %d", profile.Version), Kind: "diff", Diff: lineDiff(oldText, newText)}
	facts := []Fact{
		{"target", profile.ID},
		{"version", fmt.Sprint(profile.StateVersion)},
		{"requester", profile.ProposedBy + " · " + ageLabel(profile.ProposedAt, now)},
		{"attention state", "-"},
		{"profile", fmt.Sprintf("%s v%d", profile.Name, profile.Version)},
		{"objective", gate.ObjectiveID},
		{"supersedes", profile.SupersedesID},
		{"claim", "-"},
	}
	return ask, evidence, facts, activityRows(activity, profile.ID, now, 4), nil
}

func claimFact(items []ports.WorkItemContext, workItemID string, now time.Time) string {
	for _, item := range items {
		if item.WorkItem.ID != workItemID {
			continue
		}
		for _, claim := range item.Claims {
			if !claim.ReleasedAt.IsZero() {
				continue
			}
			if claim.ExpiresAt.After(now) {
				return fmt.Sprintf("%s · ttl %s", claim.ActorID, ageLabel(now, claim.ExpiresAt))
			}
			return fmt.Sprintf("expired %s ago", ageLabel(claim.ExpiresAt, now))
		}
	}
	return "-"
}

func parseTime(s string) time.Time {
	t, _ := time.Parse(time.RFC3339, s)
	return t
}

func prettyJSON(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		return string(raw)
	}
	out, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return string(raw)
	}
	return string(out)
}
