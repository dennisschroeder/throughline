package dashboard

// This file defines the wire shapes the frontend consumes, matching
// plans/dashboard/wireframes/loop-dashboard/README.md's "GET /loop view model" section
// field-for-field. The server computes every derived fact here (queue order, blocker
// reasons, liveness, allowed_decisions, rationale_required_for) so the browser has nothing
// to compute — it is a pure view over this payload.

// dormantThreshold is the liveness window: an actor whose most recent activity row is older
// than this is "dormant" (README: "Dormant threshold: 60s since the actor's last tool call").
const dormantThresholdSeconds = 60

// ActorRef is the actor summary embedded in a gate and in the glance rail.
type ActorRef struct {
	ID         string `json:"id"`
	LastCallAt string `json:"last_call_at,omitempty"`
	Live       bool   `json:"live"`
}

// Gate is one open item blocking a human decision: a plan review, a pending approval
// (work-item/output-profile/output-revision), an open question, a pending external-action
// approval, a work item carrying a non-none attention state, an output-profile activation, a
// live authority grant offered for revocation, a question offered for waiver, or a paused
// objective. See gates.go for how each kind is derived without inventing new store queries.
type Gate struct {
	ID                    string   `json:"id"`
	Kind                  string   `json:"kind"`
	Title                 string   `json:"title"`
	TargetID              string   `json:"target_id"`
	TargetKind            string   `json:"target_kind"`
	Requester             string   `json:"requester"`
	RequestedAt           string   `json:"requested_at"`
	WaitingLabel          string   `json:"waiting_label"`
	ExpectedVersion       int      `json:"expected_version"`
	ExpectedActionVersion int      `json:"expected_action_version,omitempty"`
	AllowedDecisions      []string `json:"allowed_decisions"`
	RationaleRequiredFor  []string `json:"rationale_required_for"`
	EvidenceHint          string   `json:"evidence_hint"`
	Actor                 ActorRef `json:"actor"`
	WorkItemID            string   `json:"work_item_id,omitempty"`
	ObjectiveID           string   `json:"objective_id"`
}

// LoopCard is one board card, already carrying the server-computed blocker reason and
// progress fraction — the client renders it verbatim.
type LoopCard struct {
	ID string `json:"id"`
	// ItemID is the work item's internal id — what GET /api/v1/item?id= actually takes.
	// ID above is the human-readable key (e.g. "TH-VIZ-05-DASHBOARD-UX") the spec draws on
	// the card face; the two are different strings, so the client must not fetch on ID.
	ItemID      string        `json:"item_id"`
	Kind        string        `json:"kind"`
	Priority    string        `json:"priority"`
	Title       string        `json:"title"`
	MetaLabel   string        `json:"meta_label"`
	Blocker     *CardBlocker  `json:"blocker"`
	Progress    *CardProgress `json:"progress"`
	GateID      string        `json:"gate_id,omitempty"`
	ObjectiveID string        `json:"objective_id"`
	Dimmed      bool          `json:"dimmed"`
}

type CardBlocker struct {
	Code  string `json:"code"`
	Label string `json:"label"`
}

type CardProgress struct {
	Passed int `json:"passed"`
	Total  int `json:"total"`
}

// Column is one board column (Backlog/Ready/In progress/Review/Done).
type Column struct {
	Key   string     `json:"key"`
	Label string     `json:"label"`
	Count int        `json:"count"`
	Cards []LoopCard `json:"cards"`
}

// Counts backs the shell's seven-cell count row.
type Counts struct {
	NeedsYou      int `json:"needs_you"`
	Proposed      int `json:"proposed"`
	Ready         int `json:"ready"`
	InProgress    int `json:"in_progress"`
	InReview      int `json:"in_review"`
	Accepted      int `json:"accepted"`
	DormantActors int `json:"dormant_actors"`
}

// GlanceActor is one row in the "Objective at a glance" actors section.
type GlanceActor struct {
	ID         string `json:"id"`
	LastCallAt string `json:"last_call_at,omitempty"`
	Live       bool   `json:"live"`
}

type GlanceOutputs struct {
	Accepted int `json:"accepted"`
	InReview int `json:"in_review"`
	Profiles int `json:"profiles"`
}

type GlanceDecision struct {
	At   string `json:"at"`
	Text string `json:"text"`
}

// Glance is the "Objective at a glance" rail.
type Glance struct {
	Phase           string           `json:"phase"`
	PhaseNote       string           `json:"phase_note"`
	Actors          []GlanceActor    `json:"actors"`
	Outputs         GlanceOutputs    `json:"outputs"`
	RecentDecisions []GlanceDecision `json:"recent_decisions"`
}

// ObjectiveHeader is the top-bar switcher's current-objective summary.
type ObjectiveHeader struct {
	ID            string `json:"id"`
	Title         string `json:"title"`
	Phase         string `json:"phase"`
	Version       int    `json:"version"`
	WorkspacePath string `json:"workspace_path"`
}

// LoopSnapshot is the entire GET /api/v1/loop payload — one-to-one with the spec's regions.
type LoopSnapshot struct {
	Objective ObjectiveHeader `json:"objective"`
	Counts    Counts          `json:"counts"`
	Gates     []Gate          `json:"gates"`
	Columns   []Column        `json:"columns"`
	Glance    Glance          `json:"glance"`
	Cursor    int64           `json:"cursor"`
}

// ObjectiveSummary is one row in the GET /api/v1/objectives switcher payload.
type ObjectiveSummary struct {
	ID        string `json:"id"`
	Title     string `json:"title"`
	Phase     string `json:"phase"`
	ItemCount int    `json:"item_count"`
	GateCount int    `json:"gate_count"`
	Current   bool   `json:"current"`
}

// ObjectivesResponse is the GET /api/v1/objectives payload.
type ObjectivesResponse struct {
	WorkspacePath string             `json:"workspace_path"`
	ActorID       string             `json:"actor_id"`
	Objectives    []ObjectiveSummary `json:"objectives"`
}

// ChangesResponse is the GET /api/v1/changes payload.
type ChangesResponse struct {
	Cursor  int64 `json:"cursor"`
	Changed bool  `json:"changed"`
}

// DiffLine is one line of a plan/spec-revision diff evidence block.
type DiffLine struct {
	Kind string `json:"kind"` // "added" | "removed" | "context"
	Text string `json:"text"`
}

// Evidence is the drawer's Evidence section: for a plan/spec revision it is a diff; for an
// external action it is the authorization subject rendered as text; for a question it is the
// established options/observations; for an output it is validation criteria with verdicts;
// for a plain work item it is acceptance criteria with passed ones marked.
type Evidence struct {
	Label    string         `json:"label"`
	Meta     string         `json:"meta"`
	Kind     string         `json:"kind"` // "diff" | "text" | "criteria"
	Diff     []DiffLine     `json:"diff,omitempty"`
	Text     string         `json:"text,omitempty"`
	Criteria []CriterionRow `json:"criteria,omitempty"`
}

type CriterionRow struct {
	Text   string `json:"text"`
	Passed bool   `json:"passed"`
	Status string `json:"status"`
}

// Fact is one row of the drawer's 8-pair key/value Facts grid.
type Fact struct {
	Label string `json:"label"`
	Value string `json:"value"`
}

// DependencyRow is one row of the drawer's Dependencies section.
type DependencyRow struct {
	Relation string `json:"relation"`
	Target   string `json:"target"`
	State    string `json:"state"`
}

// ActivityRow is one row of the drawer's Activity section.
type ActivityRow struct {
	Age   string `json:"age"`
	Event string `json:"event"`
	Actor string `json:"actor"`
	At    string `json:"at"`
}

// GateDetail is the GET /api/v1/gate/{id} payload: the queue's Gate plus everything the
// drawer needs (ask text, evidence, facts, dependencies, activity).
type GateDetail struct {
	Gate         Gate            `json:"gate"`
	Ask          string          `json:"ask"`
	Evidence     Evidence        `json:"evidence"`
	Facts        []Fact          `json:"facts"`
	Dependencies []DependencyRow `json:"dependencies"`
	Activity     []ActivityRow   `json:"activity"`
}
