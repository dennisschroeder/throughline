package dashboard

import (
	"context"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/dennisschroeder/throughline/internal/app"
	"github.com/dennisschroeder/throughline/internal/domain/work"
	"github.com/dennisschroeder/throughline/internal/ports"
)

// ObjectivesHandler serves the switcher's contents: every objective the workspace's work
// items currently reference (see the ListWorkItems-dedup comment on resolveObjectives below
// for why this, and not a dedicated ListObjectives read, is the source), each with a
// server-computed open-gate count.
func (h *Handlers) ObjectivesHandler() http.Handler {
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
		service, err := h.router.Service(r.Context(), session.WorkspaceID)
		if err != nil {
			h.logError("resolve service for objectives", err)
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}
		current := strings.TrimSpace(r.URL.Query().Get("objective_id"))
		resp, err := buildObjectivesResponse(r.Context(), service, session.WorkspaceID, session.ActorID, current, h.now())
		if err != nil {
			h.logError("build objectives response", err)
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}
		writeJSON(w, http.StatusOK, resp)
	})
}

// LoopHandler serves the whole-screen payload for one objective. objective_id is optional:
// omitted, it auto-resolves per the spec ("--objective if given, else the objective with the
// most blocking gates, else last used" — this HTTP surface has no --objective flag, so a
// present query parameter plays that role; "last used" has no session-scoped storage today,
// so the fallback below is the most-blocked objective, tie-broken by most recently updated).
func (h *Handlers) LoopHandler() http.Handler {
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
		service, err := h.router.Service(r.Context(), session.WorkspaceID)
		if err != nil {
			h.logError("resolve service for loop", err)
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}
		requested := strings.TrimSpace(r.URL.Query().Get("objective_id"))
		objectiveID, err := resolveObjectiveID(r.Context(), service, requested, h.now())
		if err != nil {
			h.logError("resolve objective", err)
			http.Error(w, "no objective found", http.StatusNotFound)
			return
		}
		snapshot, err := buildLoopSnapshot(r.Context(), service, objectiveID, session.WorkspaceID, h.now())
		if err != nil {
			h.logError("build loop snapshot", err)
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}
		writeJSON(w, http.StatusOK, snapshot)
	})
}

// ChangesHandler is the cheap cursor-delta poll: {cursor, changed}. The client re-fetches
// /loop only when changed is true. Cursor is the workspace's latest activity sequence — a
// coarser signal than one scoped to the current objective, but always correct (a poll that
// refetches slightly more often than strictly necessary is harmless; one that misses a
// change is not).
func (h *Handlers) ChangesHandler() http.Handler {
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
		service, err := h.router.Service(r.Context(), session.WorkspaceID)
		if err != nil {
			h.logError("resolve service for changes", err)
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}
		since := int64(0)
		fmt.Sscanf(r.URL.Query().Get("since"), "%d", &since)
		cursor, err := service.LatestActivitySequence(r.Context())
		if err != nil {
			h.logError("latest activity sequence", err)
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}
		writeJSON(w, http.StatusOK, ChangesResponse{Cursor: cursor, Changed: cursor != since})
	})
}

// objectivesFromItems dedups the distinct objectives referenced by this workspace's work
// items. There is no ListObjectives read path (see the original snapshot builder this
// package replaces), so — as before — a workspace with zero work items yields zero
// objectives, and an objective with no work items yet is invisible to the switcher.
func objectivesFromItems(items []ports.WorkItemContext) []work.Objective {
	seen := make(map[string]bool)
	var out []work.Objective
	for _, item := range items {
		id := item.Objective.ID
		if id == "" || seen[id] {
			continue
		}
		seen[id] = true
		out = append(out, item.Objective)
	}
	return out
}

func resolveObjectiveID(ctx context.Context, service *app.Service, requested string, now time.Time) (string, error) {
	if requested != "" {
		return requested, nil
	}
	items, err := service.ListWorkItems(ctx)
	if err != nil {
		return "", err
	}
	objectives := objectivesFromItems(items)
	if len(objectives) == 0 {
		return "", fmt.Errorf("no objectives in this workspace")
	}
	if len(objectives) == 1 {
		return objectives[0].ID, nil
	}
	al := &actorLiveness{lastCallAt: map[string]time.Time{}, now: now}
	best := objectives[0]
	bestGates := -1
	for _, obj := range objectives {
		objCtx, err := service.GetObjectiveContext(ctx, obj.ID)
		if err != nil {
			continue
		}
		gates, err := buildGates(ctx, service, objCtx, items, al, now)
		if err != nil {
			continue
		}
		n := len(gates)
		if n > bestGates || (n == bestGates && obj.UpdatedAt.After(best.UpdatedAt)) {
			bestGates = n
			best = obj
		}
	}
	return best.ID, nil
}

func buildObjectivesResponse(ctx context.Context, service *app.Service, workspaceID, actorID, currentID string, now time.Time) (ObjectivesResponse, error) {
	items, err := service.ListWorkItems(ctx)
	if err != nil {
		return ObjectivesResponse{}, err
	}
	objectives := objectivesFromItems(items)
	al := &actorLiveness{lastCallAt: map[string]time.Time{}, now: now}
	resp := ObjectivesResponse{WorkspacePath: workspaceID, ActorID: actorID}
	for _, obj := range objectives {
		itemCount := 0
		for _, item := range items {
			if item.Objective.ID == obj.ID {
				itemCount++
			}
		}
		objCtx, err := service.GetObjectiveContext(ctx, obj.ID)
		if err != nil {
			return ObjectivesResponse{}, err
		}
		gates, err := buildGates(ctx, service, objCtx, items, al, now)
		if err != nil {
			return ObjectivesResponse{}, err
		}
		resp.Objectives = append(resp.Objectives, ObjectiveSummary{
			ID:        obj.ID,
			Title:     obj.Title,
			Phase:     string(obj.Phase),
			ItemCount: itemCount,
			GateCount: len(gates),
			Current:   obj.ID == currentID,
		})
	}
	sort.SliceStable(resp.Objectives, func(i, j int) bool { return resp.Objectives[i].Title < resp.Objectives[j].Title })
	return resp, nil
}

func buildLoopSnapshot(ctx context.Context, service *app.Service, objectiveID, workspaceID string, now time.Time) (LoopSnapshot, error) {
	objCtx, err := service.GetObjectiveContext(ctx, objectiveID)
	if err != nil {
		return LoopSnapshot{}, fmt.Errorf("get objective context: %w", err)
	}
	allItems, err := service.ListWorkItems(ctx)
	if err != nil {
		return LoopSnapshot{}, fmt.Errorf("list work items: %w", err)
	}
	var items []ports.WorkItemContext
	for _, item := range allItems {
		if item.Objective.ID == objectiveID {
			items = append(items, item)
		}
	}
	ready, err := service.ListReadyWork(ctx)
	if err != nil {
		return LoopSnapshot{}, fmt.Errorf("list ready work: %w", err)
	}
	readyIDs := make(map[string]bool, len(ready))
	for _, r := range ready {
		readyIDs[r.WorkItem.ID] = true
	}
	activity, err := service.ListActivity(ctx, app.ActivityFilter{ObjectiveID: objectiveID, Limit: 500})
	if err != nil {
		return LoopSnapshot{}, fmt.Errorf("list activity: %w", err)
	}
	al := buildActorLiveness(activity, now)

	gates, err := buildGates(ctx, service, objCtx, allItems, al, now)
	if err != nil {
		return LoopSnapshot{}, fmt.Errorf("build gates: %w", err)
	}
	gatedWorkItem := make(map[string]Gate, len(gates))
	for _, g := range gates {
		wid := g.WorkItemID
		if wid == "" && g.TargetKind == "work_item" {
			wid = g.TargetID
		}
		if wid != "" {
			if _, exists := gatedWorkItem[wid]; !exists {
				gatedWorkItem[wid] = g
			}
		}
	}

	cursor, err := service.LatestActivitySequence(ctx)
	if err != nil {
		return LoopSnapshot{}, fmt.Errorf("latest activity sequence: %w", err)
	}

	snapshot := LoopSnapshot{
		Objective: ObjectiveHeader{
			ID:            objCtx.Objective.ID,
			Title:         objCtx.Objective.Title,
			Phase:         string(objCtx.Objective.Phase),
			Version:       objCtx.Objective.Version,
			WorkspacePath: workspaceID,
		},
		Gates:  gates,
		Cursor: cursor,
	}
	snapshot.Counts = buildCounts(items, gates, al)
	snapshot.Columns = buildColumns(items, gatedWorkItem, readyIDs, objCtx.Objective, now)
	snapshot.Glance = buildGlance(objCtx, items, al)
	return snapshot, nil
}

func buildCounts(items []ports.WorkItemContext, gates []Gate, al *actorLiveness) Counts {
	c := Counts{NeedsYou: len(gates)}
	for _, item := range items {
		if item.WorkItem.CommitmentState == work.ItemProposed {
			c.Proposed++
		}
		switch item.WorkItem.ExecutionStatus {
		case work.StatusReady:
			c.Ready++
		case work.StatusInProgress:
			c.InProgress++
		case work.StatusReview:
			c.InReview++
		case work.StatusDone:
			c.Accepted++
		}
	}
	for actorID := range al.lastCallAt {
		if !al.live(actorID) {
			c.DormantActors++
		}
	}
	return c
}

var columnDefs = []struct {
	key    string
	label  string
	status work.ExecutionStatus
}{
	{"backlog", "Backlog", work.StatusBacklog},
	{"ready", "Ready", work.StatusReady},
	{"in_progress", "In progress", work.StatusInProgress},
	{"review", "Review", work.StatusReview},
	{"done", "Done", work.StatusDone},
}

func buildColumns(items []ports.WorkItemContext, gatedWorkItem map[string]Gate, readyIDs map[string]bool, objective work.Objective, now time.Time) []Column {
	columns := make([]Column, len(columnDefs))
	for i, def := range columnDefs {
		columns[i] = Column{Key: def.key, Label: def.label}
	}
	for _, item := range items {
		wi := item.WorkItem
		idx := -1
		for i, def := range columnDefs {
			if def.status == wi.ExecutionStatus {
				idx = i
				break
			}
		}
		if idx == -1 {
			continue // cancelled or otherwise off-board
		}
		card := buildCard(item, gatedWorkItem, readyIDs, objective, columnDefs[idx].key, now)
		columns[idx].Cards = append(columns[idx].Cards, card)
	}
	for i := range columns {
		columns[i].Count = len(columns[i].Cards)
	}
	return columns
}

func buildCard(item ports.WorkItemContext, gatedWorkItem map[string]Gate, readyIDs map[string]bool, objective work.Objective, columnKey string, now time.Time) LoopCard {
	wi := item.WorkItem
	card := LoopCard{
		ID:          wi.Key,
		ItemID:      wi.ID,
		Kind:        wi.Kind,
		Priority:    string(wi.Priority),
		Title:       wi.Title,
		ObjectiveID: item.Objective.ID,
		Dimmed:      columnKey == "done",
	}

	var activeClaim *work.Claim
	for i, claim := range item.Claims {
		if claim.ReleasedAt.IsZero() {
			activeClaim = &item.Claims[i]
			break
		}
	}
	switch {
	case activeClaim != nil && activeClaim.ExpiresAt.After(now):
		card.MetaLabel = activeClaim.ActorID + " · claimed"
	case wi.CommitmentState == work.ItemAccepted && wi.ExecutionStatus == work.StatusDone:
		card.MetaLabel = "accepted " + ageLabel(wi.UpdatedAt, now) + " ago"
	default:
		card.MetaLabel = "unclaimed"
	}

	if gate, ok := gatedWorkItem[wi.ID]; ok {
		card.GateID = gate.ID
		card.Blocker = &CardBlocker{Code: "gated", Label: "gated · " + gate.ID + " needs you"}
	} else if wi.CommitmentState == work.ItemAccepted && objective.Phase == work.ObjectiveExecution &&
		wi.ExecutionStatus != work.StatusDone && wi.ExecutionStatus != work.StatusCancelled && !readyIDs[wi.ID] {
		card.Blocker = &CardBlocker{Code: "blocked_dependency", Label: "blocked · waiting on dependencies"}
	}

	required := 0
	passed := 0
	for _, ac := range item.AcceptanceCriteria {
		if !ac.Required {
			continue
		}
		required++
		if ac.Status == work.AcceptanceSatisfied {
			passed++
		}
	}
	if required > 0 {
		card.Progress = &CardProgress{Passed: passed, Total: required}
	}
	return card
}

func buildGlance(objCtx ports.ObjectiveContext, items []ports.WorkItemContext, al *actorLiveness) Glance {
	g := Glance{
		Phase:     string(objCtx.Objective.Phase),
		PhaseNote: phaseNote(objCtx.Objective.Phase),
	}
	for actorID := range al.lastCallAt {
		g.Actors = append(g.Actors, GlanceActor{ID: actorID, LastCallAt: al.ref(actorID).LastCallAt, Live: al.live(actorID)})
	}
	sort.SliceStable(g.Actors, func(i, j int) bool { return g.Actors[i].ID < g.Actors[j].ID })

	profileSeen := make(map[string]bool)
	for _, item := range items {
		for _, eo := range item.ExpectedOutputs {
			profileSeen[fmt.Sprintf("%s@%d", eo.Profile.Name, eo.Profile.Version)] = true
		}
	}
	g.Outputs.Profiles = len(profileSeen)
	for _, item := range items {
		for _, rev := range item.OutputRevisions {
			if rev.Revision.AcceptanceState == "accepted" {
				g.Outputs.Accepted++
			} else if rev.Revision.AcceptanceState == "produced" {
				g.Outputs.InReview++
			}
		}
	}

	decisions := append([]work.Decision(nil), objCtx.Decisions...)
	sort.SliceStable(decisions, func(i, j int) bool { return decisions[i].DecidedAt.After(decisions[j].DecidedAt) })
	for i, d := range decisions {
		if i >= 3 {
			break
		}
		text := d.Title
		if d.Outcome != "" {
			text = d.Title + " — " + d.Outcome
		}
		g.RecentDecisions = append(g.RecentDecisions, GlanceDecision{At: formatOptionalTime(d.DecidedAt), Text: truncate(text, 140)})
	}
	return g
}

func phaseNote(phase work.ObjectivePhase) string {
	switch phase {
	case work.ObjectiveIdea:
		return "Still being framed; no plan yet."
	case work.ObjectiveDiscovery:
		return "Gathering context before a plan is proposed."
	case work.ObjectivePlanning:
		return "A plan is proposed or under review."
	case work.ObjectiveExecution:
		return "Work is actively being executed."
	case work.ObjectiveEvaluation:
		return "Execution is done; outcomes are being validated."
	case work.ObjectiveCompleted:
		return "Closed out. Nothing further expected."
	case work.ObjectivePaused:
		return "Paused. No work is expected to proceed."
	case work.ObjectiveCancelled:
		return "Cancelled."
	default:
		return ""
	}
}
