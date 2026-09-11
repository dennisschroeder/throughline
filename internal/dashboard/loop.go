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

// ObjectivesHandler serves the switcher's contents: every objective in the workspace, each
// with its item count and a server-computed open-gate count. It reads the objectives
// themselves, so an objective a person has just created is listed and selectable before it
// has any work items.
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
		// The current marker is addressed the same way the loop payload is, so a
		// key in the URL marks the same row the snapshot describes.
		current := strings.TrimSpace(r.URL.Query().Get("objective_id"))
		if current != "" {
			objective, resolveErr := service.ResolveObjective(r.Context(), current)
			if resolveErr != nil {
				h.logError("resolve current objective", resolveErr)
				http.Error(w, "no objective found", http.StatusNotFound)
				return
			}
			current = objective.ID
		}
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

func resolveObjectiveID(ctx context.Context, service *app.Service, requested string, now time.Time) (string, error) {
	if requested != "" {
		// A requested objective is an address, not a promise that it exists, and
		// list_objectives now prints keys prominently enough that pasting one
		// into the URL is the obvious move. Resolving here turns an unknown or
		// mistyped reference into the 404 this handler already has a branch for,
		// instead of letting it reach the snapshot builder and surface as 500.
		objective, err := service.ResolveObjective(ctx, requested)
		if err != nil {
			return "", err
		}
		return objective.ID, nil
	}
	objectives, err := service.ListObjectives(ctx)
	if err != nil {
		return "", err
	}
	if len(objectives) == 0 {
		return "", fmt.Errorf("no objectives in this workspace")
	}
	if len(objectives) == 1 {
		return objectives[0].ID, nil
	}
	items, err := service.ListWorkItems(ctx)
	if err != nil {
		return "", err
	}
	itemCounts := map[string]int{}
	for _, item := range items {
		itemCounts[item.Objective.ID]++
	}
	al := &actorLiveness{lastCallAt: map[string]time.Time{}, now: now}
	candidates := make([]objectiveCandidate, 0, len(objectives))
	for _, obj := range objectives {
		objCtx, err := service.GetObjectiveContext(ctx, obj.ID)
		if err != nil {
			continue
		}
		gates, err := buildGates(ctx, service, objCtx, items, al, now)
		if err != nil {
			continue
		}
		candidates = append(candidates, objectiveCandidate{objective: obj, gates: len(gates), items: itemCounts[obj.ID]})
	}
	return chooseObjective(objectives[0], candidates).ID, nil
}

// objectiveCandidate is one objective whose gates could actually be counted.
type objectiveCandidate struct {
	objective work.Objective
	gates     int
	items     int
}

// chooseObjective picks the objective to open when the caller named none.
//
// Most blocking gates wins, then the objective that actually holds work, then
// most recently updated. Without the middle term an objective that was just
// created and has nothing in it wins every all-zero contest, because it is
// trivially the most recently updated one — so opening the dashboard right after
// creating an objective showed an empty board.
//
// Every candidate can be dropped upstream when a store read fails, so this takes
// a fallback for the run where all of them are. Returning the zero objective
// instead would hand the caller an empty id with no error: the handler's 404
// branch only fires on an error, so the empty id travels on and fails later as
// an internal error about an objective nobody asked for. Naming a real objective
// makes a lasting failure legible, and leaves the reader somewhere they can
// navigate away from while a transient one clears.
//
// The fallback is a single objective rather than the whole list, so a caller
// cannot pass a candidate set and a list that disagree.
func chooseObjective(fallback work.Objective, candidates []objectiveCandidate) work.Objective {
	if len(candidates) == 0 {
		return fallback
	}
	best := candidates[0]
	for _, candidate := range candidates[1:] {
		better := candidate.gates > best.gates ||
			(candidate.gates == best.gates && candidate.items > best.items) ||
			(candidate.gates == best.gates && candidate.items == best.items &&
				candidate.objective.UpdatedAt.After(best.objective.UpdatedAt))
		if better {
			best = candidate
		}
	}
	return best.objective
}

func buildObjectivesResponse(ctx context.Context, service *app.Service, workspaceID, actorID, currentID string, now time.Time) (ObjectivesResponse, error) {
	items, err := service.ListWorkItems(ctx)
	if err != nil {
		return ObjectivesResponse{}, err
	}
	// Read the objectives rather than deriving them from the items: an objective
	// with none is exactly the one a person has just created and wants to switch
	// to, and deriving hid it until its first item existed.
	objectives, err := service.ListObjectives(ctx)
	if err != nil {
		return ObjectivesResponse{}, err
	}
	al := &actorLiveness{lastCallAt: map[string]time.Time{}, now: now}
	resp := ObjectivesResponse{WorkspacePath: workspaceID, ActorID: actorID}
	itemCounts := map[string]int{}
	for _, item := range items {
		itemCounts[item.Objective.ID]++
	}
	for _, obj := range objectives {
		itemCount := itemCounts[obj.ID]
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
	} else if len(item.BlockingQuestions) > 0 && wi.ExecutionStatus != work.StatusDone && wi.ExecutionStatus != work.StatusCancelled {
		// A question holding the item is a different thing to wait on than a
		// prerequisite item, and the person reading the card resolves it
		// differently, so it is named rather than folded into dependencies.
		question := item.BlockingQuestions[0]
		label := "blocked · question: " + truncate(question.Text, 60)
		if more := len(item.BlockingQuestions) - 1; more > 0 {
			label += fmt.Sprintf(" (+%d more)", more)
		}
		card.Blocker = &CardBlocker{Code: "blocked_question", Label: label}
	} else if wi.CommitmentState == work.ItemAccepted && objective.Phase == work.ObjectiveExecution &&
		wi.ExecutionStatus != work.StatusDone && wi.ExecutionStatus != work.StatusCancelled && !readyIDs[wi.ID] {
		card.Blocker = &CardBlocker{Code: "blocked_dependency", Label: "blocked · waiting on dependencies"}
	}

	required := 0
	passed := 0
	for _, ac := range item.AcceptanceCriteria {
		// A superseded criterion is a condition nobody stands behind any more.
		// Counting it made superseding one move the card away from done, which
		// is exactly backwards: describing the work more accurately does not
		// make it less finished.
		if !ac.Required || !ac.Status.Active() {
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
