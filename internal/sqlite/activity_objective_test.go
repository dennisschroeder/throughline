package sqlite

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dennisschroeder/throughline/internal/app"
	"github.com/dennisschroeder/throughline/internal/domain/work"
)

// TestObjectiveFeedIncludesPlanningRecords reproduces the defect REP-07
// repairs: questions, decisions, context records and plans recorded against
// an objective without a work item used to be absent from the
// objective-filtered feed, which then looked exactly like an objective where
// nothing had happened.
func TestObjectiveFeedIncludesPlanningRecords(t *testing.T) {
	ctx := context.Background()
	database, err := Open(ctx, filepath.Join(t.TempDir(), "objective-feed.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	if err := database.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	service := app.NewService(database.Store(), &testIDs{}, testClock{})
	if _, err := app.UnwrapMutation(service.RegisterActor(ctx, app.RegisterActorCommand{Actor: work.Actor{ID: "human:owner", Kind: work.ActorTypeHuman, DisplayName: "Owner"}, IdempotencyKey: "feed-owner"})); err != nil {
		t.Fatal(err)
	}
	createObjective := func(key string) work.Objective {
		t.Helper()
		objective, err := app.UnwrapMutation(service.CreateObjective(ctx, app.CreateObjectiveCommand{
			ActorID: "human:owner", IdempotencyKey: "feed-" + key, Key: key,
			Title: "Objective " + key, DesiredOutcome: "Its history is visible.", Phase: work.ObjectiveDiscovery,
		}))
		if err != nil {
			t.Fatal(err)
		}
		return objective
	}
	objective := createObjective("OBJ-FEED")
	other := createObjective("OBJ-OTHER")

	record, err := app.UnwrapMutation(service.RecordContext(ctx, app.RecordContextCommand{
		ObjectiveID: objective.ID, ActorID: "human:owner", Kind: work.ContextRequirement, Status: work.ContextProposed, Title: "Users resume from the feed", IdempotencyKey: "feed-context",
	}))
	if err != nil {
		t.Fatal(err)
	}
	question, err := app.UnwrapMutation(service.AskQuestion(ctx, app.AskQuestionCommand{
		ObjectiveID: objective.ID, ActorID: "human:owner", Question: "Is the feed complete?", IdempotencyKey: "feed-question",
	}))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := app.UnwrapMutation(service.AnswerQuestion(ctx, app.AnswerQuestionCommand{
		QuestionID: question.ID, ActorID: "human:owner", Answer: "It is now.", ExpectedVersion: question.Version, IdempotencyKey: "feed-answer",
	})); err != nil {
		t.Fatal(err)
	}
	decision, err := app.UnwrapMutation(service.RecordDecision(ctx, app.RecordDecisionCommand{
		ObjectiveID: objective.ID, ActorID: "human:owner", Title: "Bind activity", Decision: "Activity carries its objective.", IdempotencyKey: "feed-decision",
	}))
	if err != nil {
		t.Fatal(err)
	}
	plan, err := app.UnwrapMutation(service.CreatePlan(ctx, app.CreatePlanCommand{
		ActorID: "human:owner", IdempotencyKey: "feed-plan", ObjectiveID: objective.ID, Title: "Draft", Revision: 1, CommitmentState: work.PlanDraft,
	}))
	if err != nil {
		t.Fatal(err)
	}
	approval, err := app.UnwrapMutation(service.RequestApproval(ctx, app.RequestApprovalCommand{
		ActorID: "human:owner", IdempotencyKey: "feed-approval", Request: "Approve the draft?", PlanID: plan.ID,
	}))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := app.UnwrapMutation(service.ResolveApproval(ctx, app.ResolveApprovalCommand{
		ApprovalID: approval.ID, ActorID: "human:owner", IdempotencyKey: "feed-approval-resolved",
		ExpectedVersion: approval.Version, Decision: work.ApprovalRejected, Rationale: "Not yet.",
	})); err != nil {
		t.Fatal(err)
	}
	if _, err := app.UnwrapMutation(service.RecordDecision(ctx, app.RecordDecisionCommand{
		ObjectiveID: other.ID, ActorID: "human:owner", Title: "Elsewhere", Decision: "Belongs to the other objective.", IdempotencyKey: "feed-other-decision",
	})); err != nil {
		t.Fatal(err)
	}

	changes, err := service.ListActivity(ctx, app.ActivityFilter{ObjectiveID: objective.ID, Limit: 100})
	if err != nil {
		t.Fatal(err)
	}
	events := map[string]string{}
	var previous int64
	for _, change := range changes {
		if change.ObjectiveID != objective.ID {
			t.Fatalf("objective feed returned %s bound to %q", change.EventType, change.ObjectiveID)
		}
		if change.Sequence <= previous {
			t.Fatalf("objective feed is out of cursor order: %d after %d", change.Sequence, previous)
		}
		previous = change.Sequence
		events[change.EventType+":"+change.EntityID] = change.EntityKind
	}
	for _, want := range []string{
		"objective.created:" + objective.ID,
		"context_record.recorded:" + record.ID,
		"question.asked:" + question.ID,
		"question.answered:" + question.ID,
		"decision.recorded:" + decision.ID,
		"plan.created:" + plan.ID,
		"approval.requested:" + approval.ID,
		"approval.resolved:" + approval.ID,
	} {
		if _, ok := events[want]; !ok {
			t.Fatalf("objective feed omits %s; got %v", want, events)
		}
	}
	if len(events) != 8 {
		t.Fatalf("objective feed = %v, want exactly the eight events of this objective", events)
	}

	snapshot, err := service.SelectObjectiveContext(ctx, app.ObjectiveContextQuery{ObjectiveID: objective.ID})
	if err != nil {
		t.Fatal(err)
	}
	var sawDecision bool
	for _, change := range snapshot.RecentChanges {
		if change.EntityID == decision.ID {
			sawDecision = true
		}
		if change.ObjectiveID != objective.ID {
			t.Fatalf("recent changes include %s of objective %q", change.EventType, change.ObjectiveID)
		}
	}
	if !sawDecision {
		t.Fatalf("objective context recent changes omit the objective's decision: %#v", snapshot.RecentChanges)
	}
}

// TestMigration0014BackfillsActivityObjectiveOnAPopulatedWorkspace seeds
// history written before activity carried an objective, then migrates. Every
// objective-scoped row must be bound, workspace-level rows must stay unbound,
// no row may move in cursor order, the cursor must keep advancing past the
// old high water mark, and the append-only protection the backfill had to
// lift must be back in force.
func TestMigration0014BackfillsActivityObjectiveOnAPopulatedWorkspace(t *testing.T) {
	ctx := context.Background()
	migrations, err := loadMigrations()
	if err != nil {
		t.Fatal(err)
	}
	beforeBinding := -1
	for index, migration := range migrations {
		if migration.version == 14 {
			beforeBinding = index
		}
	}
	if beforeBinding < 0 {
		t.Fatal("the activity objective binding migration is missing")
	}
	database, err := Open(ctx, filepath.Join(t.TempDir(), "activity-backfill.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	if err := database.ensureMigrationTable(ctx); err != nil {
		t.Fatal(err)
	}
	for _, migration := range migrations[:beforeBinding] {
		if err := database.applyMigration(ctx, migration); err != nil {
			t.Fatal(err)
		}
	}

	const timestamp = "2026-09-01T00:00:00.000000000Z"
	for _, statement := range []string{
		`INSERT INTO actors (id, kind, display_name, created_at) VALUES ('human:legacy', 'human', 'Legacy owner', '` + timestamp + `')`,
		`INSERT INTO objectives (id, key, title, description, desired_outcome, phase, version, created_at, updated_at)
		 VALUES ('objective-a', 'OBJ-A', 'A', '', 'History is found.', 'discovery', 1, '` + timestamp + `', '` + timestamp + `')`,
		`INSERT INTO objectives (id, key, title, description, desired_outcome, phase, version, created_at, updated_at)
		 VALUES ('objective-b', 'OBJ-B', 'B', '', 'History stays apart.', 'discovery', 1, '` + timestamp + `', '` + timestamp + `')`,
		`INSERT INTO work_items (id, key, objective_id, title, kind, commitment_state, execution_status, priority, estimated_scope,
		   execution_policy, required_actor_kind, attention_state, created_at, updated_at)
		 VALUES ('item-a', 'A-1', 'objective-a', 'Item', 'task', 'accepted', 'backlog', 'medium', 'small',
		   'autonomous_with_report', 'any', 'none', '` + timestamp + `', '` + timestamp + `')`,
		`INSERT INTO plans (id, objective_id, title, revision, commitment_state, created_at, updated_at)
		 VALUES ('plan-a', 'objective-a', 'Plan', 1, 'proposed', '` + timestamp + `', '` + timestamp + `')`,
		`INSERT INTO questions (id, objective_id, question, status, created_by, created_at)
		 VALUES ('question-a', 'objective-a', 'Open?', 'open', 'human:legacy', '` + timestamp + `')`,
		`INSERT INTO decisions (id, objective_id, title, decision, status, decided_by, decided_at, created_at)
		 VALUES ('decision-b', 'objective-b', 'Other', 'Other objective.', 'accepted', 'human:legacy', '` + timestamp + `', '` + timestamp + `')`,
		`INSERT INTO context_records (id, objective_id, kind, title, status, version, created_at, updated_at, created_by)
		 VALUES ('context-a', 'objective-a', 'requirement', 'Requirement', 'proposed', 1, '` + timestamp + `', '` + timestamp + `', 'human:legacy')`,
		`INSERT INTO approvals (id, objective_id, plan_id, request, status, requested_by, requested_at)
		 VALUES ('approval-a', 'objective-a', 'plan-a', 'Approve?', 'requested', 'human:legacy', '` + timestamp + `')`,
		// Execution approvals are stored without an objective, so their
		// activity must be bound through the work item, never the approval.
		`INSERT INTO approvals (id, work_item_id, approved_for_actor_id, request, status, requested_by, requested_at)
		 VALUES ('approval-execution', 'item-a', 'human:legacy', 'Execute?', 'approved', 'human:legacy', '` + timestamp + `')`,
	} {
		if _, err := database.db.ExecContext(ctx, statement); err != nil {
			t.Fatalf("seed pre-upgrade row: %v\n%s", err, statement)
		}
	}
	// Each row: activity id, entity kind, entity id, work item id, the
	// objective the backfill must bind it to ("" for workspace-level).
	history := [][5]string{
		{"act-actor", "actor", "human:legacy", "", ""},
		{"act-objective", "objective", "objective-a", "", "objective-a"},
		{"act-plan", "plan", "plan-a", "", "objective-a"},
		{"act-question", "question", "question-a", "", "objective-a"},
		{"act-decision-b", "decision", "decision-b", "", "objective-b"},
		{"act-context", "context_record", "context-a", "", "objective-a"},
		{"act-approval", "approval", "approval-a", "", "objective-a"},
		{"act-item", "work_item", "item-a", "item-a", "objective-a"},
		{"act-execution-approval", "approval", "approval-execution", "item-a", "objective-a"},
		{"act-profile", "output_profile", "profile-x", "", ""},
	}
	sequences := map[string]int64{}
	for _, row := range history {
		var workItemID any
		if row[3] != "" {
			workItemID = row[3]
		}
		result, err := database.db.ExecContext(ctx, `
INSERT INTO activity (id, entity_kind, entity_id, work_item_id, actor_id, event_type, summary, payload_json, created_at)
VALUES (?, ?, ?, ?, 'human:legacy', 'legacy.event', 'Legacy event', '{}', ?)`, row[0], row[1], row[2], workItemID, timestamp)
		if err != nil {
			t.Fatal(err)
		}
		sequence, err := result.LastInsertId()
		if err != nil {
			t.Fatal(err)
		}
		sequences[row[0]] = sequence
	}
	highWaterMark := sequences["act-profile"]
	sequenceCounter := func() int64 {
		t.Helper()
		var counter int64
		// Qualified: the effects collector's TEMP table has AUTOINCREMENT too,
		// so after Migrate an unqualified name resolves to temp.sqlite_sequence.
		if err := database.db.QueryRowContext(ctx, "SELECT seq FROM main.sqlite_sequence WHERE name = 'activity'").Scan(&counter); err != nil {
			t.Fatal(err)
		}
		return counter
	}
	counterBefore := sequenceCounter()
	triggerSQL := func() string {
		t.Helper()
		var definition string
		if err := database.db.QueryRowContext(ctx, "SELECT sql FROM main.sqlite_master WHERE type = 'trigger' AND name = 'activity_is_append_only_update'").Scan(&definition); err != nil {
			t.Fatal(err)
		}
		return definition
	}
	triggerBefore := triggerSQL()
	var indexes int
	if err := database.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM main.sqlite_master WHERE type = 'index' AND name = 'activity_by_objective_sequence'").Scan(&indexes); err != nil {
		t.Fatal(err)
	}
	if indexes != 0 {
		t.Fatal("activity_by_objective_sequence exists before the migration that creates it")
	}

	if err := database.Migrate(ctx); err != nil {
		t.Fatalf("migrate a populated activity table: %v", err)
	}
	// A rebuilt table would reset the AUTOINCREMENT counter to the highest
	// surviving sequence; the cursor high water mark must survive exactly.
	if counterAfter := sequenceCounter(); counterAfter != counterBefore {
		t.Fatalf("activity sequence counter moved from %d to %d", counterBefore, counterAfter)
	}
	// The trigger the backfill lifts must come back exactly as 0003 defined
	// it: a column-restricted UPDATE OF trigger would leave objective_id and
	// sequence rewritable while still rejecting the summary update below.
	if err := database.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM main.sqlite_master WHERE type = 'index' AND name = 'activity_by_objective_sequence' AND sql LIKE '%(objective_id, sequence)%'").Scan(&indexes); err != nil {
		t.Fatal(err)
	}
	if indexes != 1 {
		t.Fatal("the objective feed's activity_by_objective_sequence index is missing after the migration")
	}
	if triggerAfter := triggerSQL(); triggerAfter != triggerBefore {
		t.Fatalf("append-only update trigger after backfill =\n%s\nwant\n%s", triggerAfter, triggerBefore)
	}

	for _, row := range history {
		var sequence int64
		var objectiveID *string
		if err := database.db.QueryRowContext(ctx, "SELECT sequence, objective_id FROM activity WHERE id = ?", row[0]).Scan(&sequence, &objectiveID); err != nil {
			t.Fatal(err)
		}
		if sequence != sequences[row[0]] {
			t.Fatalf("%s moved from sequence %d to %d", row[0], sequences[row[0]], sequence)
		}
		got := ""
		if objectiveID != nil {
			got = *objectiveID
		}
		if got != row[4] {
			t.Fatalf("%s backfilled to objective %q, want %q", row[0], got, row[4])
		}
	}

	service := app.NewService(database.Store(), &testIDs{}, testClock{})
	feed, err := service.ListActivity(ctx, app.ActivityFilter{ObjectiveID: "objective-a", Limit: 100})
	if err != nil {
		t.Fatal(err)
	}
	var feedIDs []string
	for _, change := range feed {
		feedIDs = append(feedIDs, change.ID)
	}
	if got, want := strings.Join(feedIDs, ","), "act-objective,act-plan,act-question,act-context,act-approval,act-item,act-execution-approval"; got != want {
		t.Fatalf("objective feed after backfill = %s, want %s", got, want)
	}

	recorded, err := app.UnwrapMutation(service.RecordDecision(ctx, app.RecordDecisionCommand{
		ObjectiveID: "objective-a", ActorID: "human:legacy", Title: "After upgrade", Decision: "Recorded after the backfill.", IdempotencyKey: "post-upgrade-decision",
	}))
	if err != nil {
		t.Fatal(err)
	}
	after, err := service.ListActivity(ctx, app.ActivityFilter{ObjectiveID: "objective-a", Since: highWaterMark, Limit: 100})
	if err != nil {
		t.Fatal(err)
	}
	if len(after) != 1 || after[0].EntityID != recorded.ID || after[0].Sequence <= highWaterMark {
		t.Fatalf("post-upgrade feed since %d = %#v, want only the new decision above the old high water mark", highWaterMark, after)
	}

	if _, err := database.db.ExecContext(ctx, "UPDATE activity SET summary = 'rewritten' WHERE id = 'act-objective'"); err == nil || !strings.Contains(err.Error(), "append-only") {
		t.Fatalf("update after backfill = %v, want the append-only trigger to reject it", err)
	}
	if _, err := database.db.ExecContext(ctx, "UPDATE activity SET objective_id = NULL WHERE id = 'act-objective'"); err == nil || !strings.Contains(err.Error(), "append-only") {
		t.Fatalf("rebinding after backfill = %v, want the append-only trigger to reject it", err)
	}
	if _, err := database.db.ExecContext(ctx, "DELETE FROM activity WHERE id = 'act-objective'"); err == nil || !strings.Contains(err.Error(), "append-only") {
		t.Fatalf("delete after backfill = %v, want the append-only trigger to reject it", err)
	}
}

// TestObjectiveContextRecentChangesKeepObjectiveLevelAndSelectedItemHistory
// pins the selection get_objective_context applies to recent changes:
// objective-level history always, history of the work items it selected, and
// nothing from items it did not select.
func TestObjectiveContextRecentChangesKeepObjectiveLevelAndSelectedItemHistory(t *testing.T) {
	ctx := context.Background()
	database, err := Open(ctx, filepath.Join(t.TempDir(), "recent-changes.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	if err := database.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	const timestamp = "2026-09-01T00:00:00.000000000Z"
	item := func(id, status string) string {
		return `INSERT INTO work_items (id, key, objective_id, plan_id, title, kind, commitment_state, execution_status, priority, estimated_scope,
		   execution_policy, required_actor_kind, attention_state, created_at, updated_at)
		 VALUES ('` + id + `', '` + id + `', 'objective-a', 'plan-a', 'Item', 'task', 'accepted', '` + status + `', 'medium', 'small',
		   'autonomous_with_report', 'any', 'none', '` + timestamp + `', '` + timestamp + `')`
	}
	for _, statement := range []string{
		`INSERT INTO objectives (id, key, title, description, desired_outcome, phase, version, created_at, updated_at)
		 VALUES ('objective-a', 'OBJ-A', 'A', '', 'Recent history is selected.', 'execution', 1, '` + timestamp + `', '` + timestamp + `')`,
		`INSERT INTO plans (id, objective_id, title, revision, commitment_state, created_at, updated_at)
		 VALUES ('plan-a', 'objective-a', 'Plan', 1, 'approved', '` + timestamp + `', '` + timestamp + `')`,
		item("item-selected", "ready"),
		item("item-unselected", "backlog"),
	} {
		if _, err := database.db.ExecContext(ctx, statement); err != nil {
			t.Fatalf("seed: %v\n%s", err, statement)
		}
	}
	for _, row := range [][3]string{
		{"act-question", "question", ""},
		{"act-selected", "work_item", "item-selected"},
		{"act-unselected", "work_item", "item-unselected"},
	} {
		var workItemID any
		if row[2] != "" {
			workItemID = row[2]
		}
		if _, err := database.db.ExecContext(ctx, `
INSERT INTO activity (id, entity_kind, entity_id, work_item_id, objective_id, actor_id, event_type, summary, payload_json, created_at)
VALUES (?, ?, ?, ?, 'objective-a', 'human:owner', 'test.event', 'Test event', '{}', ?)`, row[0], row[1], row[0], workItemID, timestamp); err != nil {
			t.Fatal(err)
		}
	}

	service := app.NewService(database.Store(), &testIDs{}, testClock{})
	snapshot, err := service.SelectObjectiveContext(ctx, app.ObjectiveContextQuery{ObjectiveID: "objective-a"})
	if err != nil {
		t.Fatal(err)
	}
	var recent []string
	for _, change := range snapshot.RecentChanges {
		recent = append(recent, change.ID)
	}
	if got, want := strings.Join(recent, ","), "act-selected,act-question"; got != want {
		t.Fatalf("recent changes = %s, want %s (newest first, unselected item excluded)", got, want)
	}
}
