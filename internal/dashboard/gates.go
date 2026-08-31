package dashboard

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/dennisschroeder/throughline/internal/app"
	"github.com/dennisschroeder/throughline/internal/domain/authority"
	"github.com/dennisschroeder/throughline/internal/domain/output"
	"github.com/dennisschroeder/throughline/internal/domain/work"
	"github.com/dennisschroeder/throughline/internal/ports"
)

// buildGates derives every open gate for one objective from data the application layer
// already exposes — ports.ObjectiveContext (raw, unfiltered: see the investigation this
// package's design rests on, GetObjectiveContext must be used here rather than
// SelectObjectiveContext, which pre-filters away exactly the proposed/pending records a
// gate list needs) and ports.WorkItemContext (from ListWorkItems). No new store method or
// SQL is introduced; where the domain genuinely has no queryable "pending" record for a gate
// kind the README documents (grant revocation, objective pause — see the two doc comments
// below), this simply emits none for that kind rather than inventing one.
//
// Queue order (server-owned per the spec): oldest-requested gate first, so the head gate is
// whichever has been waiting longest.
func buildGates(ctx context.Context, service *app.Service, objCtx ports.ObjectiveContext, items []ports.WorkItemContext, al *actorLiveness, now time.Time) ([]Gate, error) {
	var gates []Gate

	for _, pc := range objCtx.Plans {
		if pc.Plan.CommitmentState != work.PlanProposed {
			continue
		}
		gates = append(gates, Gate{
			ID:                   pc.Plan.ID,
			Kind:                 "plan",
			Title:                pc.Plan.Title,
			TargetID:             pc.Plan.ID,
			TargetKind:           "plan",
			Requester:            pc.Plan.ProposedBy,
			RequestedAt:          formatOptionalTime(pc.Plan.ProposedAt),
			WaitingLabel:         ageLabel(pc.Plan.ProposedAt, now),
			ExpectedVersion:      pc.Plan.Version,
			AllowedDecisions:     []string{"Approve", "Reject"},
			RationaleRequiredFor: []string{"Reject"},
			EvidenceHint:         planSummaryHint(pc),
			Actor:                al.ref(pc.Plan.ProposedBy),
			ObjectiveID:          objCtx.Objective.ID,
		})
	}

	for _, a := range objCtx.Approvals {
		if a.Status != work.ApprovalRequested {
			continue
		}
		targetID, targetKind := approvalTarget(a)
		title := a.Request
		if title == "" {
			title = fmt.Sprintf("Approval requested (%s)", targetKind)
		}
		gates = append(gates, Gate{
			ID:                   a.ID,
			Kind:                 "approval",
			Title:                title,
			TargetID:             targetID,
			TargetKind:           targetKind,
			Requester:            a.RequestedBy,
			RequestedAt:          formatOptionalTime(a.RequestedAt),
			WaitingLabel:         ageLabel(a.RequestedAt, now),
			ExpectedVersion:      a.Version,
			AllowedDecisions:     []string{"Approve", "Reject"},
			RationaleRequiredFor: []string{"Reject"},
			EvidenceHint:         truncate(a.Request, 120),
			Actor:                al.ref(a.RequestedBy),
			WorkItemID:           a.WorkItemID,
			ObjectiveID:          objCtx.Objective.ID,
		})
	}

	for _, q := range objCtx.Questions {
		if q.Status != work.QuestionOpen {
			continue
		}
		gates = append(gates, Gate{
			ID:                   q.ID,
			Kind:                 "question",
			Title:                q.Text,
			TargetID:             q.ID,
			TargetKind:           "question",
			Requester:            q.CreatedBy,
			RequestedAt:          formatOptionalTime(q.CreatedAt),
			WaitingLabel:         ageLabel(q.CreatedAt, now),
			ExpectedVersion:      q.Version,
			AllowedDecisions:     []string{"Answer", "Waive"},
			RationaleRequiredFor: []string{"Waive"},
			EvidenceHint:         truncate(q.Text, 120),
			Actor:                al.ref(q.CreatedBy),
			WorkItemID:           q.WorkItemID,
			ObjectiveID:          objCtx.Objective.ID,
		})
	}

	for _, item := range items {
		if item.Objective.ID != objCtx.Objective.ID {
			continue
		}
		if item.WorkItem.AttentionState == work.AttentionNone {
			continue
		}
		requestedAt := item.WorkItem.UpdatedAt
		requester := ""
		if entry := latestAttentionActivity(item); entry != nil {
			requestedAt = entry.CreatedAt
			requester = entry.ActorID
		}
		gates = append(gates, Gate{
			ID:                   "attn-" + item.WorkItem.ID,
			Kind:                 "attention",
			Title:                item.WorkItem.Title,
			TargetID:             item.WorkItem.ID,
			TargetKind:           "work_item",
			Requester:            requester,
			RequestedAt:          formatOptionalTime(requestedAt),
			WaitingLabel:         ageLabel(requestedAt, now),
			ExpectedVersion:      item.WorkItem.Version,
			AllowedDecisions:     []string{"Acknowledge"},
			RationaleRequiredFor: nil,
			EvidenceHint:         humanizeAttention(item.WorkItem.AttentionState),
			Actor:                al.ref(requester),
			WorkItemID:           item.WorkItem.ID,
			ObjectiveID:          objCtx.Objective.ID,
		})
	}

	actionGates, err := buildActionApprovalGates(ctx, service, objCtx.Objective.ID, items, al, now)
	if err != nil {
		return nil, fmt.Errorf("build action approval gates: %w", err)
	}
	gates = append(gates, actionGates...)

	profileGates, err := buildOutputProfileGates(ctx, service, objCtx.Objective.ID, al, now)
	if err != nil {
		return nil, fmt.Errorf("build output profile gates: %w", err)
	}
	gates = append(gates, profileGates...)

	// Grant revocation and objective pause are documented gate kinds (see the drawer/copy-
	// prompt support in static/index.html) but the domain has no "pending" record for
	// either: RevokeAuthorityGrant applies immediately (no revocation-request workflow),
	// and TransitionObjective moves an objective to `paused` immediately (no pause-request
	// workflow) — see the gates.go investigation. There is nothing to enumerate here; a
	// live grant or a paused objective is not itself "waiting on a human decision", so
	// neither produces a queue entry. Only a future two-phase revoke/pause workflow would
	// change that.

	sort.SliceStable(gates, func(i, j int) bool {
		return gateSortKey(gates[i]).Before(gateSortKey(gates[j]))
	})
	return gates, nil
}

func gateSortKey(g Gate) time.Time {
	t, err := time.Parse(time.RFC3339, g.RequestedAt)
	if err != nil {
		return time.Time{}
	}
	return t
}

func planSummaryHint(pc ports.PlanContext) string {
	if pc.Plan.Summary != "" {
		return truncate(pc.Plan.Summary, 120)
	}
	return fmt.Sprintf("%d item(s) proposed", len(pc.Items))
}

// approvalTarget reads which single target field work.Approval carries (the domain
// guarantees exactly one is set) and returns its id and a target_kind label.
func approvalTarget(a work.Approval) (id, kind string) {
	switch {
	case a.PlanID != "":
		return a.PlanID, "plan"
	case a.OutputProfileID != "":
		return a.OutputProfileID, "output_profile"
	case a.OutputRevisionID != "":
		return a.OutputRevisionID, "output_revision"
	case a.WorkItemID != "":
		return a.WorkItemID, "work_item"
	default:
		return "", "unknown"
	}
}

func latestAttentionActivity(item ports.WorkItemContext) *work.ProgressEntry {
	// Progress entries are the closest thing to a per-item activity log already embedded in
	// WorkItemContext; used only as a best-effort "who asked" hint when no dedicated
	// attention-request record is fetched (see buildActionApprovalGates for the pattern
	// that does fetch dedicated activity rows when it matters more).
	if len(item.Progress) == 0 {
		return nil
	}
	return &item.Progress[len(item.Progress)-1]
}

func humanizeAttention(s work.AttentionState) string {
	switch s {
	case work.AttentionNeedsHumanDecision:
		return "needs a decision"
	case work.AttentionNeedsHumanReview:
		return "needs review"
	case work.AttentionNeedsClarification:
		return "needs clarification"
	case work.AttentionInterventionRequired:
		return "intervention required"
	default:
		return string(s)
	}
}

// buildActionApprovalGates finds pending authority.ActionApproval records by scanning this
// objective's recent activity for "approval.requested" events whose id does not belong to
// the work.Approval set already returned by GetObjectiveContext (that EntityKind is reused
// for both approval systems — see coordination.go/authority.go), then resolves each
// candidate id with the same GetActionApproval read the check_action_authorization/
// resolve_action_approval tools use. This is the store-layer gap the investigation flagged:
// there is no ListActionApprovals; activity is the only enumerable trail, so this is bounded
// to recent activity rather than being a complete historical scan.
func buildActionApprovalGates(ctx context.Context, service *app.Service, objectiveID string, items []ports.WorkItemContext, al *actorLiveness, now time.Time) ([]Gate, error) {
	activity, err := service.ListActivity(ctx, app.ActivityFilter{ObjectiveID: objectiveID, Limit: 500})
	if err != nil {
		return nil, err
	}
	seen := make(map[string]bool)
	var gates []Gate
	for _, a := range activity {
		if a.EntityKind != "approval" || a.EventType != "approval.requested" || a.EntityID == "" || seen[a.EntityID] {
			continue
		}
		seen[a.EntityID] = true
		approval, err := service.GetActionApproval(ctx, a.EntityID)
		if err != nil {
			continue // not an action approval id (likely a work.Approval id sharing the entity_kind string) — skip
		}
		if approval.Status != authority.ApprovalRequested {
			continue
		}
		action, workItemID, title := resolveExternalAction(items, approval.ExternalActionID)
		if title == "" {
			title = fmt.Sprintf("External action approval (%s)", approval.ExternalActionID)
		}
		gates = append(gates, Gate{
			ID:                    approval.ID,
			Kind:                  "action_approval",
			Title:                 title,
			TargetID:              approval.ExternalActionID,
			TargetKind:            "external_action",
			Requester:             approval.RequestedBy,
			RequestedAt:           formatOptionalTime(approval.RequestedAt),
			WaitingLabel:          ageLabel(approval.RequestedAt, now),
			ExpectedActionVersion: action.Version,
			AllowedDecisions:      []string{"Approve", "Reject"},
			RationaleRequiredFor:  []string{"Reject"},
			EvidenceHint:          truncate(approval.Request, 120),
			Actor:                 al.ref(approval.RequestedBy),
			WorkItemID:            workItemID,
			ObjectiveID:           objectiveID,
		})
	}
	return gates, nil
}

func resolveExternalAction(items []ports.WorkItemContext, actionID string) (authority.ExternalAction, string, string) {
	for _, item := range items {
		for _, ea := range item.ExternalActions {
			if ea.Action.ID == actionID {
				return ea.Action, item.WorkItem.ID, ea.Action.Title
			}
		}
	}
	return authority.ExternalAction{}, "", ""
}

// buildOutputProfileGates lists governed output profiles awaiting activation review.
// output.Profile is not objective-scoped in the domain (ListOutputProfiles is workspace
// wide), so these gates are attached to whichever objective is currently loaded — the only
// objective the human is looking at — rather than being silently invisible.
func buildOutputProfileGates(ctx context.Context, service *app.Service, objectiveID string, al *actorLiveness, now time.Time) ([]Gate, error) {
	profiles, err := service.ListOutputProfiles(ctx)
	if err != nil {
		return nil, err
	}
	var gates []Gate
	for _, p := range profiles {
		if p.LifecycleState != output.ProfileProposed {
			continue
		}
		gates = append(gates, Gate{
			ID:                   p.ID,
			Kind:                 "output_profile",
			Title:                fmt.Sprintf("%s v%d", p.Name, p.Version),
			TargetID:             p.ID,
			TargetKind:           "output_profile",
			Requester:            p.ProposedBy,
			RequestedAt:          formatOptionalTime(p.ProposedAt),
			WaitingLabel:         ageLabel(p.ProposedAt, now),
			ExpectedVersion:      p.StateVersion,
			AllowedDecisions:     []string{"Activate", "Reject"},
			RationaleRequiredFor: []string{"Reject"},
			EvidenceHint:         truncate(p.Description, 120),
			Actor:                al.ref(p.ProposedBy),
			ObjectiveID:          objectiveID,
		})
	}
	return gates, nil
}

func truncate(s string, n int) string {
	s = strings.TrimSpace(s)
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
