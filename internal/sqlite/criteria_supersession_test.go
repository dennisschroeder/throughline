package sqlite

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dennisschroeder/throughline/internal/app"
	"github.com/dennisschroeder/throughline/internal/domain/work"
	"github.com/dennisschroeder/throughline/internal/ports"
)

// criteriaFixture is one accepted work item carrying two required criteria.
func criteriaFixture(t *testing.T, name string) (context.Context, string, *Database, *app.Service, work.WorkItem, []work.AcceptanceCriterion) {
	t.Helper()
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), name)
	database, err := Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	if err := database.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	service := app.NewService(database.Store(), &testIDs{}, testClock{})
	if _, err := app.UnwrapMutation(service.RegisterActor(ctx, app.RegisterActorCommand{Actor: work.Actor{ID: "human:owner", Kind: work.ActorTypeHuman, DisplayName: "Owner"}, IdempotencyKey: "criteria-owner"})); err != nil {
		t.Fatal(err)
	}
	objective, err := app.UnwrapMutation(service.CreateObjective(ctx, app.CreateObjectiveCommand{
		ActorID: "human:owner", IdempotencyKey: "criteria-objective", Key: "OBJ-CRITERIA",
		Title: "Correct a wrong condition", DesiredOutcome: "History survives the correction.", Phase: work.ObjectivePlanning,
	}))
	if err != nil {
		t.Fatal(err)
	}
	item, err := app.UnwrapMutation(service.CreateWorkItem(ctx, app.CreateWorkItemCommand{
		ActorID: "human:owner", IdempotencyKey: "criteria-item", Key: "TH-CRITERIA", ObjectiveID: objective.ID,
		Title: "Carries the criteria", Kind: "research", CommitmentState: work.ItemProposed, ExecutionStatus: work.StatusBacklog,
		Priority: work.PriorityMedium, EstimatedScope: work.ScopeSmall, ExecutionPolicy: work.PolicyAgentMayPropose,
		RequiredActorKind: work.ActorAny, AttentionState: work.AttentionNone,
		AcceptanceCriteria: []app.ProposedAcceptanceCriterion{
			{Text: "The wrong condition, written before anyone understood the work.", Required: true, Ordinal: 1},
			{Text: "A condition that stays right.", Required: true, Ordinal: 2},
		},
	}))
	if err != nil {
		t.Fatal(err)
	}
	itemContext, err := service.GetWorkItem(ctx, item.ID)
	if err != nil {
		t.Fatal(err)
	}
	return ctx, path, database, service, item, itemContext.AcceptanceCriteria
}

// TestSupersedingACriterionKeepsItsPredecessorIntact is the first criterion.
// Before this, a wrong condition could only be waived — which records that the
// condition was excused rather than that it was mistaken — or left in place to
// block completion for a reason nobody stood behind.
func TestSupersedingACriterionKeepsItsPredecessorIntact(t *testing.T) {
	ctx, path, database, service, item, criteria := criteriaFixture(t, "supersede.db")
	wrong := criteria[0]

	patched, err := service.PatchWorkItem(ctx, app.PatchWorkItemCommand{
		WorkItemID: item.ID, ActorID: "human:owner", IdempotencyKey: "supersede-wrong", ExpectedVersion: item.Version,
		AcceptanceCriteriaToAdd: []app.PatchAcceptanceCriterionAddition{{
			Text: "The condition the work actually has to meet.", Required: true, Ordinal: wrong.Ordinal,
			SupersedesID: wrong.ID, SupersessionReason: "The original named the wrong artefact.",
		}},
	})
	if err != nil {
		t.Fatal(err)
	}

	after, err := service.GetWorkItem(ctx, item.ID)
	if err != nil {
		t.Fatal(err)
	}
	var predecessor, replacement *work.AcceptanceCriterion
	for index := range after.AcceptanceCriteria {
		switch after.AcceptanceCriteria[index].ID {
		case wrong.ID:
			predecessor = &after.AcceptanceCriteria[index]
		default:
			if after.AcceptanceCriteria[index].SupersedesID == wrong.ID {
				replacement = &after.AcceptanceCriteria[index]
			}
		}
	}
	if predecessor == nil || replacement == nil {
		t.Fatalf("criteria after supersession = %#v", after.AcceptanceCriteria)
	}

	// The predecessor is history: readable, unchanged apart from its status.
	if predecessor.Status != work.AcceptanceSuperseded {
		t.Fatalf("predecessor status = %q, want superseded", predecessor.Status)
	}
	if predecessor.Text != wrong.Text || predecessor.Ordinal != wrong.Ordinal || predecessor.Required != wrong.Required {
		t.Fatalf("superseding rewrote the predecessor: %#v, was %#v", *predecessor, wrong)
	}
	// The replacement carries the link and the reason, and may reuse the ordinal.
	if replacement.SupersessionReason != "The original named the wrong artefact." {
		t.Fatalf("replacement reason = %q", replacement.SupersessionReason)
	}
	if replacement.Ordinal != wrong.Ordinal {
		t.Fatalf("replacement ordinal = %d, want the predecessor's %d", replacement.Ordinal, wrong.Ordinal)
	}
	if replacement.Status != work.AcceptancePending {
		t.Fatalf("replacement status = %q, want pending", replacement.Status)
	}

	// All of it survives a restart: this is durable state, not a view.
	if err := database.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	if err := reopened.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	restarted, err := app.NewService(reopened.Store(), &testIDs{}, testClock{}).GetWorkItem(ctx, item.ID)
	if err != nil {
		t.Fatal(err)
	}
	found := 0
	for _, criterion := range restarted.AcceptanceCriteria {
		if criterion.ID == wrong.ID && criterion.Status == work.AcceptanceSuperseded && criterion.Text == wrong.Text {
			found++
		}
		if criterion.SupersedesID == wrong.ID && criterion.SupersessionReason != "" {
			found++
		}
	}
	if found != 2 {
		t.Fatalf("after reopening, the superseded pair did not survive: %#v", restarted.AcceptanceCriteria)
	}
	_ = patched
}

// TestOnlyActiveCriteriaBlockCompletion is the second criterion, checked at the
// gate that actually decides. A superseded condition must stop gating the work;
// otherwise correcting a criterion would leave the item permanently incomplete.
func TestOnlyActiveCriteriaBlockCompletion(t *testing.T) {
	ctx, _, database, service, item, criteria := criteriaFixture(t, "blocking.db")
	wrong := criteria[0]

	pendingRequired := func() int {
		t.Helper()
		var count int
		if err := database.db.QueryRowContext(ctx,
			"SELECT count(*) FROM acceptance_criteria WHERE work_item_id = ? AND required = 1 AND status = 'pending'",
			item.ID).Scan(&count); err != nil {
			t.Fatal(err)
		}
		return count
	}
	satisfied := func() bool {
		t.Helper()
		var ok bool
		if err := database.Store().WithinTransaction(ctx, func(repository ports.Repository) error {
			var err error
			ok, err = repository.AcceptanceCriteriaSatisfied(ctx, item.ID)
			return err
		}); err != nil {
			t.Fatal(err)
		}
		return ok
	}

	if pendingRequired() != 2 || satisfied() {
		t.Fatal("two pending required criteria did not block completion")
	}

	if _, err := service.PatchWorkItem(ctx, app.PatchWorkItemCommand{
		WorkItemID: item.ID, ActorID: "human:owner", IdempotencyKey: "blocking-supersede", ExpectedVersion: item.Version,
		AcceptanceCriteriaToAdd: []app.PatchAcceptanceCriterionAddition{{
			Text: "The condition that replaced it.", Required: true, Ordinal: wrong.Ordinal,
			SupersedesID: wrong.ID, SupersessionReason: "Replaced rather than excused.",
		}},
	}); err != nil {
		t.Fatal(err)
	}
	// Superseding swapped one pending condition for another rather than removing
	// the gate: still two, and still blocked.
	if pendingRequired() != 2 || satisfied() {
		t.Fatal("superseding a criterion changed how many conditions are outstanding")
	}

	after, err := service.GetWorkItem(ctx, item.ID)
	if err != nil {
		t.Fatal(err)
	}
	resolutions := []app.PatchAcceptanceCriterionResolution{}
	for _, criterion := range after.AcceptanceCriteria {
		if criterion.Status == work.AcceptancePending {
			resolutions = append(resolutions, app.PatchAcceptanceCriterionResolution{
				CriterionID: criterion.ID, Status: work.AcceptanceSatisfied, Rationale: "Met.",
			})
		}
	}
	if len(resolutions) != 2 {
		t.Fatalf("pending criteria = %d, want the replacement and the untouched one", len(resolutions))
	}
	if _, err := service.PatchWorkItem(ctx, app.PatchWorkItemCommand{
		WorkItemID: item.ID, ActorID: "human:owner", IdempotencyKey: "blocking-resolve",
		ExpectedVersion: after.WorkItem.Version, AcceptanceCriterionResolutions: resolutions,
	}); err != nil {
		t.Fatal(err)
	}

	// Every active condition is met. The superseded one is history and must not
	// hold the item back.
	if !satisfied() {
		t.Fatal("a superseded criterion still blocked completion")
	}
}

// TestAddingACriterionReportsAnOrdinalCollisionAsADomainError covers the common
// mistake with the new addition path: a caller adding a condition has no reason
// to know which ordinals are taken, and used to receive the driver's unique
// constraint error with a SQLite error number.
func TestAddingACriterionReportsAnOrdinalCollisionAsADomainError(t *testing.T) {
	ctx, _, _, service, item, criteria := criteriaFixture(t, "ordinals.db")
	taken := criteria[0]

	_, err := service.PatchWorkItem(ctx, app.PatchWorkItemCommand{
		WorkItemID: item.ID, ActorID: "human:owner", IdempotencyKey: "ordinal-collision", ExpectedVersion: item.Version,
		AcceptanceCriteriaToAdd: []app.PatchAcceptanceCriterionAddition{{Text: "Collides.", Required: true, Ordinal: taken.Ordinal}},
	})
	if err == nil {
		t.Fatal("adding a criterion on a taken ordinal succeeded")
	}
	if !strings.Contains(err.Error(), "already in use") || strings.Contains(err.Error(), "UNIQUE constraint") {
		t.Fatalf("error = %v, want a domain message rather than the driver's", err)
	}

	// A free ordinal is accepted, and superseding is the way to reuse a taken one.
	if _, err := service.PatchWorkItem(ctx, app.PatchWorkItemCommand{
		WorkItemID: item.ID, ActorID: "human:owner", IdempotencyKey: "ordinal-free", ExpectedVersion: item.Version,
		AcceptanceCriteriaToAdd: []app.PatchAcceptanceCriterionAddition{{Text: "Appended.", Required: true, Ordinal: 9}},
	}); err != nil {
		t.Fatalf("adding a criterion on a free ordinal failed: %v", err)
	}
}

// TestAddingARequiredCriterionToFinishedWorkAsksForReview covers the state that
// would otherwise pass unnoticed: an item still marked done whose completion
// gate no longer holds.
func TestAddingARequiredCriterionToFinishedWorkAsksForReview(t *testing.T) {
	ctx, _, database, service, item, criteria := criteriaFixture(t, "reopened.db")

	// Satisfy everything and mark the item done directly, which is the state a
	// completed item is in when someone realises a condition was missing.
	resolutions := make([]app.PatchAcceptanceCriterionResolution, 0, len(criteria))
	for _, criterion := range criteria {
		resolutions = append(resolutions, app.PatchAcceptanceCriterionResolution{CriterionID: criterion.ID, Status: work.AcceptanceSatisfied, Rationale: "Met."})
	}
	patched, err := service.PatchWorkItem(ctx, app.PatchWorkItemCommand{
		WorkItemID: item.ID, ActorID: "human:owner", IdempotencyKey: "reopened-resolve",
		ExpectedVersion: item.Version, AcceptanceCriterionResolutions: resolutions,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := database.db.ExecContext(ctx, "UPDATE work_items SET execution_status = 'done' WHERE id = ?", item.ID); err != nil {
		t.Fatal(err)
	}

	reopened, err := service.PatchWorkItem(ctx, app.PatchWorkItemCommand{
		WorkItemID: item.ID, ActorID: "human:owner", IdempotencyKey: "reopened-add",
		ExpectedVersion: patched.Result.Version,
		AcceptanceCriteriaToAdd: []app.PatchAcceptanceCriterionAddition{{
			Text: "The condition nobody wrote down until afterwards.", Required: true, Ordinal: 7,
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if reopened.Result.AttentionState != work.AttentionNeedsHumanReview {
		t.Fatalf("attention state = %q, want a review request: a done item now has an unmet gate", reopened.Result.AttentionState)
	}
}

// TestSupersedingAtTheStoreRefusesAStaleOrRepeatedWrite pins the guards on the
// update itself. The domain rejects a second supersession first, so without
// this the store's own conditions could be removed unnoticed — and they are
// what makes the write safe under a concurrent one.
func TestSupersedingAtTheStoreRefusesAStaleOrRepeatedWrite(t *testing.T) {
	ctx, _, database, _, item, criteria := criteriaFixture(t, "store-guard.db")
	target := criteria[0]
	_ = item

	supersede := func(criterion work.AcceptanceCriterion) error {
		return database.Store().WithinTransaction(ctx, func(repository ports.Repository) error {
			return repository.SupersedeAcceptanceCriterion(ctx, criterion)
		})
	}

	// A write carrying the wrong expected version must not land.
	stale := target
	stale.Version = target.Version + 5
	if err := supersede(stale); err == nil {
		t.Fatal("a supersession carrying a stale version succeeded")
	}

	first := target
	first.Version = target.Version + 1
	if err := supersede(first); err != nil {
		t.Fatalf("the first supersession failed: %v", err)
	}

	// Superseding it again must not land either, whatever version it carries.
	again := first
	again.Version = first.Version + 1
	if err := supersede(again); err == nil {
		t.Fatal("a criterion was superseded twice at the store")
	}
}
