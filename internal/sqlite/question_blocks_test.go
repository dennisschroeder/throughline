package sqlite

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/dennisschroeder/throughline/internal/app"
	"github.com/dennisschroeder/throughline/internal/domain/work"
)

const questionFixtureTime = "2026-09-01T00:00:00.000000000Z"

// seedExecutableItems prepares an objective in execution with an approved
// plan and accepted, ready items, so readiness and claiming depend on nothing
// but the question blocks under test.
func seedExecutableItems(t *testing.T, ctx context.Context, database *Database, ids ...string) {
	t.Helper()
	statements := []string{
		`INSERT INTO actors (id, kind, display_name, created_at) VALUES ('human:owner', 'human', 'Owner', '` + questionFixtureTime + `')`,
		`INSERT INTO objectives (id, key, title, description, desired_outcome, phase, version, created_at, updated_at)
		 VALUES ('objective-a', 'OBJ-A', 'A', '', 'Questions hold work.', 'execution', 1, '` + questionFixtureTime + `', '` + questionFixtureTime + `')`,
		`INSERT INTO objectives (id, key, title, description, desired_outcome, phase, version, created_at, updated_at)
		 VALUES ('objective-b', 'OBJ-B', 'B', '', 'Elsewhere.', 'execution', 1, '` + questionFixtureTime + `', '` + questionFixtureTime + `')`,
		`INSERT INTO plans (id, objective_id, title, revision, commitment_state, created_at, updated_at)
		 VALUES ('plan-a', 'objective-a', 'Plan', 1, 'approved', '` + questionFixtureTime + `', '` + questionFixtureTime + `')`,
		`INSERT INTO plans (id, objective_id, title, revision, commitment_state, created_at, updated_at)
		 VALUES ('plan-b', 'objective-b', 'Plan', 1, 'approved', '` + questionFixtureTime + `', '` + questionFixtureTime + `')`,
		`INSERT INTO work_items (id, key, objective_id, plan_id, title, kind, commitment_state, execution_status, priority, estimated_scope,
		   execution_policy, required_actor_kind, attention_state, created_at, updated_at)
		 VALUES ('item-elsewhere', 'item-elsewhere', 'objective-b', 'plan-b', 'Item', 'task', 'accepted', 'ready', 'medium', 'small',
		   'autonomous_with_report', 'any', 'none', '` + questionFixtureTime + `', '` + questionFixtureTime + `')`,
	}
	for _, id := range ids {
		statements = append(statements, `INSERT INTO work_items (id, key, objective_id, plan_id, title, kind, commitment_state, execution_status, priority, estimated_scope,
		   execution_policy, required_actor_kind, attention_state, created_at, updated_at)
		 VALUES ('`+id+`', '`+id+`', 'objective-a', 'plan-a', 'Item', 'task', 'accepted', 'ready', 'medium', 'small',
		   'autonomous_with_report', 'any', 'none', '`+questionFixtureTime+`', '`+questionFixtureTime+`')`)
	}
	for _, statement := range statements {
		if _, err := database.db.ExecContext(ctx, statement); err != nil {
			t.Fatalf("seed: %v\n%s", err, statement)
		}
	}
}

func readyItemIDs(t *testing.T, ctx context.Context, service *app.Service) []string {
	t.Helper()
	ready, err := service.ListReadyWork(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var ids []string
	for _, item := range ready {
		if item.Objective.ID == "objective-a" {
			ids = append(ids, item.WorkItem.ID)
		}
	}
	slices.Sort(ids)
	return ids
}

// TestUnresolvedQuestionsBlockEveryLinkedItemUntilResolved is REP-08's first
// criterion: an unsharp or open question holds every item it is linked to,
// whether linked when asked, by being asked on the item, or linked later, and
// only answering or waiving releases them.
func TestUnresolvedQuestionsBlockEveryLinkedItemUntilResolved(t *testing.T) {
	ctx := context.Background()
	database, err := Open(ctx, filepath.Join(t.TempDir(), "question-blocks.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	if err := database.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	seedExecutableItems(t, ctx, database, "item-a", "item-b", "item-c", "item-d")
	if _, err := database.db.ExecContext(ctx, `INSERT INTO work_items (id, key, objective_id, plan_id, title, kind, commitment_state, execution_status, priority, estimated_scope,
		   execution_policy, required_actor_kind, attention_state, created_at, updated_at)
		 VALUES ('item-done', 'item-done', 'objective-a', 'plan-a', 'Item', 'task', 'accepted', 'done', 'medium', 'small',
		   'autonomous_with_report', 'any', 'none', '`+questionFixtureTime+`', '`+questionFixtureTime+`')`); err != nil {
		t.Fatal(err)
	}
	if _, err := database.db.ExecContext(ctx, `INSERT INTO work_items (id, key, objective_id, plan_id, title, kind, commitment_state, execution_status, priority, estimated_scope,
		   execution_policy, required_actor_kind, attention_state, created_at, updated_at)
		 VALUES ('item-rejected', 'item-rejected', 'objective-a', 'plan-a', 'Item', 'task', 'rejected', 'backlog', 'medium', 'small',
		   'autonomous_with_report', 'any', 'none', '`+questionFixtureTime+`', '`+questionFixtureTime+`')`); err != nil {
		t.Fatal(err)
	}
	service := app.NewService(database.Store(), &testIDs{}, testClock{})
	claim := func(id, key string) error {
		item, err := service.GetWorkItem(ctx, id)
		if err != nil {
			t.Fatal(err)
		}
		_, err = service.ClaimWorkItem(ctx, app.ClaimWorkItemCommand{WorkItemID: id, ActorID: "human:owner", ExpectedVersion: item.WorkItem.Version, IdempotencyKey: key, LeaseDuration: time.Hour})
		return err
	}
	if got := readyItemIDs(t, ctx, service); strings.Join(got, ",") != "item-a,item-b,item-c,item-d" {
		t.Fatalf("ready before any question = %v", got)
	}

	fog, err := app.UnwrapMutation(service.AskQuestion(ctx, app.AskQuestionCommand{
		ObjectiveID: "objective-a", ActorID: "human:owner", IdempotencyKey: "fog", Question: "How the export interacts with retention",
		Status: work.QuestionUnsharp, BlocksWorkItems: []string{"item-a", "item-b"},
	}))
	if err != nil {
		t.Fatal(err)
	}
	if fog.Status != work.QuestionUnsharp || strings.Join(fog.BlocksWorkItems, ",") != "item-a,item-b" {
		t.Fatalf("unsharp question = %#v", fog)
	}
	if got := readyItemIDs(t, ctx, service); strings.Join(got, ",") != "item-c,item-d" {
		t.Fatalf("ready while unsharp question holds a and b = %v", got)
	}
	var gateError app.ClaimGateError
	if err := claim("item-a", "claim-a-held"); !errors.As(err, &gateError) || len(gateError.Requirements) != 1 || gateError.Requirements[0].Code != work.ClaimRequirementNoBlockers {
		t.Fatalf("claim of an item held by an unsharp question = %v (%#v), want only the no-blockers requirement", err, gateError)
	}
	if _, err := service.AnswerQuestion(ctx, app.AnswerQuestionCommand{QuestionID: fog.ID, ActorID: "human:owner", Answer: "Premature.", ExpectedVersion: fog.Version, IdempotencyKey: "answer-unsharp"}); err == nil {
		t.Fatal("an unsharp question was answered before being phrased")
	}

	sharpened, err := app.UnwrapMutation(service.SharpenQuestion(ctx, app.SharpenQuestionCommand{
		QuestionID: fog.ID, ActorID: "human:owner", Question: "Does exporting a record extend its retention period?", ExpectedVersion: fog.Version, IdempotencyKey: "sharpen",
	}))
	if err != nil {
		t.Fatal(err)
	}
	if sharpened.Status != work.QuestionOpen || sharpened.Text != "Does exporting a record extend its retention period?" {
		t.Fatalf("sharpened question = %#v", sharpened)
	}
	stored, err := service.GetObjectiveContext(ctx, "objective-a")
	if err != nil {
		t.Fatal(err)
	}
	if len(stored.Questions) != 1 || stored.Questions[0].Text != sharpened.Text || stored.Questions[0].Status != work.QuestionOpen {
		t.Fatalf("stored question after sharpening = %#v, want the phrased text persisted", stored.Questions)
	}
	linked, err := app.UnwrapMutation(service.LinkQuestionBlocker(ctx, app.LinkQuestionBlockerCommand{
		QuestionID: fog.ID, WorkItemID: "item-c", ActorID: "human:owner", ExpectedVersion: sharpened.Version, IdempotencyKey: "link-c",
	}))
	if err != nil {
		t.Fatal(err)
	}
	if got := readyItemIDs(t, ctx, service); strings.Join(got, ",") != "item-d" {
		t.Fatalf("ready while the open question holds a, b and c = %v", got)
	}
	// The unsharp wording survives only in the graduation event, and the link
	// event belongs to the item it now holds, so that item's feed shows it.
	var sawSharpened, sawLinked bool
	objectiveFeed, err := service.ListActivity(ctx, app.ActivityFilter{ObjectiveID: "objective-a", Limit: 100})
	if err != nil {
		t.Fatal(err)
	}
	for _, change := range objectiveFeed {
		if change.EventType == "question.sharpened" && change.EntityID == fog.ID {
			var payload struct {
				PreviousText string `json:"previous_text"`
			}
			if err := json.Unmarshal(change.PayloadJSON, &payload); err != nil {
				t.Fatal(err)
			}
			sawSharpened = payload.PreviousText == "How the export interacts with retention"
		}
	}
	itemFeed, err := service.ListActivity(ctx, app.ActivityFilter{WorkItemID: "item-c", Limit: 100})
	if err != nil {
		t.Fatal(err)
	}
	for _, change := range itemFeed {
		if change.EventType == "question.blocks_linked" && change.EntityID == fog.ID {
			sawLinked = true
		}
	}
	if !sawSharpened || !sawLinked {
		t.Fatalf("graduation event with previous text recorded = %v, link event in item-c's feed = %v", sawSharpened, sawLinked)
	}
	if _, err := service.LinkQuestionBlocker(ctx, app.LinkQuestionBlockerCommand{
		QuestionID: fog.ID, WorkItemID: "item-done", ActorID: "human:owner", ExpectedVersion: linked.Version, IdempotencyKey: "link-done",
	}); err == nil {
		t.Fatal("a question was linked as a blocker of a done item, which it could never hold")
	}
	if _, err := service.LinkQuestionBlocker(ctx, app.LinkQuestionBlockerCommand{
		QuestionID: fog.ID, WorkItemID: "item-rejected", ActorID: "human:owner", ExpectedVersion: linked.Version, IdempotencyKey: "link-rejected",
	}); err == nil {
		t.Fatal("a question was linked as a blocker of a rejected item, which can never be claimed")
	}
	// Asking about a finished item is still a question about it, and its own
	// link is recorded even though it holds nothing.
	aboutDone, err := app.UnwrapMutation(service.AskQuestion(ctx, app.AskQuestionCommand{
		ObjectiveID: "objective-a", WorkItemID: "item-done", ActorID: "human:owner", IdempotencyKey: "about-done", Question: "Was the output archived?",
	}))
	if err != nil {
		t.Fatalf("asking a question on a done item: %v", err)
	}
	if strings.Join(aboutDone.BlocksWorkItems, ",") != "item-done" {
		t.Fatalf("question on a done item blocks %v, want its own item", aboutDone.BlocksWorkItems)
	}
	if _, err := service.LinkQuestionBlocker(ctx, app.LinkQuestionBlockerCommand{
		QuestionID: fog.ID, WorkItemID: "item-elsewhere", ActorID: "human:owner", ExpectedVersion: linked.Version, IdempotencyKey: "link-elsewhere",
	}); err == nil {
		t.Fatal("a question blocked a work item of another objective")
	}
	detail, err := service.GetWorkItem(ctx, "item-c")
	if err != nil {
		t.Fatal(err)
	}
	if len(detail.BlockingQuestions) != 1 || detail.BlockingQuestions[0].ID != fog.ID {
		t.Fatalf("item-c blocking questions = %#v", detail.BlockingQuestions)
	}

	onItem, err := app.UnwrapMutation(service.AskQuestion(ctx, app.AskQuestionCommand{
		ObjectiveID: "objective-a", WorkItemID: "item-d", ActorID: "human:owner", IdempotencyKey: "on-item", Question: "Which format does the consumer read?",
	}))
	if err != nil {
		t.Fatal(err)
	}
	if got := readyItemIDs(t, ctx, service); len(got) != 0 {
		t.Fatalf("ready while a question asked on item-d is open = %v, want none", got)
	}

	answered, err := app.UnwrapMutation(service.AnswerQuestion(ctx, app.AnswerQuestionCommand{
		QuestionID: fog.ID, ActorID: "human:owner", Answer: "No; retention runs from creation.", ExpectedVersion: linked.Version, IdempotencyKey: "answer",
	}))
	if err != nil {
		t.Fatal(err)
	}
	if got := readyItemIDs(t, ctx, service); strings.Join(got, ",") != "item-a,item-b,item-c" {
		t.Fatalf("ready after answering = %v, want every item the answered question held", got)
	}
	if released, err := service.GetWorkItem(ctx, "item-c"); err != nil || len(released.BlockingQuestions) != 0 {
		t.Fatalf("item-c blocking questions after answering = %#v, %v, want none", released.BlockingQuestions, err)
	}
	if _, err := service.LinkQuestionBlocker(ctx, app.LinkQuestionBlockerCommand{
		QuestionID: fog.ID, WorkItemID: "item-d", ActorID: "human:owner", ExpectedVersion: answered.Version, IdempotencyKey: "link-after-answer",
	}); err == nil {
		t.Fatal("an answered question was linked as a blocker")
	}
	if err := claim("item-a", "claim-a-free"); err != nil {
		t.Fatalf("claim after the question was answered: %v", err)
	}

	if _, err := app.UnwrapMutation(service.WaiveQuestion(ctx, app.WaiveQuestionCommand{
		QuestionID: onItem.ID, ActorID: "human:owner", Reason: "The consumer reads both.", ExpectedVersion: onItem.Version, IdempotencyKey: "waive",
	})); err != nil {
		t.Fatal(err)
	}
	if got := readyItemIDs(t, ctx, service); strings.Join(got, ",") != "item-a,item-b,item-c,item-d" {
		t.Fatalf("ready after waiving the question on item-d = %v", got)
	}
}

// TestMigration0015CarriesQuestionsAndTheirExistingBlocksForward migrates
// questions written before REP-08. A question asked on a work item already
// blocked that item, so the migration must record that as a link; the
// attention state has to be recovered from the last request for it, since the
// old update never wrote the flag; and nothing else about a question moves.
func TestMigration0015CarriesQuestionsAndTheirExistingBlocksForward(t *testing.T) {
	ctx := context.Background()
	migrations, err := loadMigrations()
	if err != nil {
		t.Fatal(err)
	}
	beforeQuestions := -1
	for index, migration := range migrations {
		if migration.version == 15 {
			beforeQuestions = index
		}
	}
	if beforeQuestions < 0 {
		t.Fatal("the question frontier migration is missing")
	}
	database, err := Open(ctx, filepath.Join(t.TempDir(), "questions-upgrade.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	if err := database.ensureMigrationTable(ctx); err != nil {
		t.Fatal(err)
	}
	for _, migration := range migrations[:beforeQuestions] {
		if err := database.applyMigration(ctx, migration); err != nil {
			t.Fatal(err)
		}
	}
	seedExecutableItems(t, ctx, database, "item-a", "item-b")
	question := func(id, workItemID, status string, flag int) string {
		item := "NULL"
		if workItemID != "" {
			item = "'" + workItemID + "'"
		}
		return `INSERT INTO questions (id, objective_id, work_item_id, question, status, answer, requires_human_attention, version, created_by, created_at)
		 VALUES ('` + id + `', 'objective-a', ` + item + `, 'Question ` + id + `', '` + status + `', '', ` + string(rune('0'+flag)) + `, 3, 'human:owner', '` + questionFixtureTime + `')`
	}
	for _, statement := range []string{
		question("q-open-on-item", "item-a", "open", 1),
		question("q-answered-on-item", "item-b", "answered", 0),
		question("q-objective", "", "open", 1),
		question("q-reviewed", "", "open", 1),
		`INSERT INTO activity (id, entity_kind, entity_id, objective_id, actor_id, event_type, summary, payload_json, created_at)
		 VALUES ('act-1', 'question', 'q-reviewed', 'objective-a', 'human:owner', 'attention.requested', 'Attention', '{"target_kind":"question","target_id":"q-reviewed","attention_state":"needs_clarification"}', '` + questionFixtureTime + `')`,
		`INSERT INTO activity (id, entity_kind, entity_id, objective_id, actor_id, event_type, summary, payload_json, created_at)
		 VALUES ('act-2', 'question', 'q-reviewed', 'objective-a', 'human:owner', 'attention.requested', 'Attention', '{"target_kind":"question","target_id":"q-reviewed","attention_state":"intervention_required"}', '` + questionFixtureTime + `')`,
	} {
		if _, err := database.db.ExecContext(ctx, statement); err != nil {
			t.Fatalf("seed pre-upgrade row: %v\n%s", err, statement)
		}
	}

	if err := database.Migrate(ctx); err != nil {
		t.Fatalf("migrate populated questions: %v", err)
	}

	service := app.NewService(database.Store(), &testIDs{}, testClock{})
	objectiveContext, err := service.GetObjectiveContext(ctx, "objective-a")
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]struct {
		status    work.QuestionStatus
		attention work.AttentionState
		blocks    string
	}{
		"q-open-on-item":     {work.QuestionOpen, work.AttentionNeedsHumanDecision, "item-a"},
		"q-answered-on-item": {work.QuestionAnswered, work.AttentionNone, "item-b"},
		"q-objective":        {work.QuestionOpen, work.AttentionNeedsHumanDecision, ""},
		"q-reviewed":         {work.QuestionOpen, work.AttentionInterventionRequired, ""},
	}
	if len(objectiveContext.Questions) != len(want) {
		t.Fatalf("questions after migrating = %#v", objectiveContext.Questions)
	}
	for _, got := range objectiveContext.Questions {
		expected := want[got.ID]
		if got.Status != expected.status || got.AttentionState != expected.attention || strings.Join(got.BlocksWorkItems, ",") != expected.blocks || got.Version != 3 || got.Text != "Question "+got.ID {
			t.Fatalf("%s after migrating = %#v, want status %s, attention %s, blocks %q, version 3", got.ID, got, expected.status, expected.attention, expected.blocks)
		}
	}
	if got := readyItemIDs(t, ctx, service); strings.Join(got, ",") != "item-b" {
		t.Fatalf("ready after migrating = %v, want item-a still held by its open question", got)
	}

	var schema string
	if err := database.db.QueryRowContext(ctx, "SELECT sql FROM main.sqlite_master WHERE type = 'table' AND name = 'questions'").Scan(&schema); err != nil {
		t.Fatal(err)
	}
	for _, fragment := range []string{"'unsharp', 'open', 'answered', 'waived'", "attention_state IN ('none', 'needs_human_decision', 'needs_human_review', 'needs_clarification', 'intervention_required')", "CHECK (version > 0)"} {
		if !strings.Contains(schema, fragment) {
			t.Fatalf("questions schema after migrating lacks %q:\n%s", fragment, schema)
		}
	}
	var indexes int
	if err := database.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM main.sqlite_master WHERE type = 'index' AND name IN ('questions_by_objective_status', 'question_blocks_by_item')").Scan(&indexes); err != nil {
		t.Fatal(err)
	}
	if indexes != 2 {
		t.Fatalf("question indexes after migrating = %d, want both", indexes)
	}
}
