package sqlite

import (
	"errors"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/dennisschroeder/throughline/internal/app"
	"github.com/dennisschroeder/throughline/internal/domain/authority"
	"github.com/dennisschroeder/throughline/internal/domain/work"
	"github.com/dennisschroeder/throughline/internal/ports"
)

// reopen returns a second service over the same database file, the way a later
// process reads a workspace the first one wrote. It shares the id generator, so
// the two do not mint colliding identities.
func (h *runHarness) reopen(t *testing.T) *app.Service {
	t.Helper()
	database, err := Open(h.ctx, h.path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	if err := database.Migrate(h.ctx); err != nil {
		t.Fatal(err)
	}
	return app.NewService(database.Store(), h.ids, &planningClock{})
}

// TestARunKeyReplaysAcrossAProcessRestart is the durability half of the
// deduplication contract. A harness that crashes and comes back must find the
// run it created, not start a second one — and the run key is the only thing it
// has to find it with.
func TestARunKeyReplaysAcrossAProcessRestart(t *testing.T) {
	h := newRunHarness(t, "replay-restart.db", []app.ProposedPlanInput{{Name: "window", Required: true, Ordinal: 1}}, 0)
	binding := app.RunInputBindingCommand{Name: "window", Value: "2026-Q1"}
	first, err := h.createRun("weekly", "create-before-restart", "agent:one", binding)
	if err != nil {
		t.Fatal(err)
	}

	restarted := h.reopen(t)
	replayed, err := app.UnwrapMutation(restarted.CreatePlanRun(h.ctx, app.CreatePlanRunCommand{
		ObjectiveID: h.objective.ID, PlanID: h.plan.ID, ActorID: "agent:two",
		IdempotencyKey: "create-after-restart", RunKey: "weekly",
		Bindings: []app.RunInputBindingCommand{binding},
	}))
	if err != nil {
		t.Fatalf("a restarted process could not find the run its key names: %v", err)
	}
	if replayed.Run.ID != first.Run.ID || replayed.Run.Sequence != first.Run.Sequence {
		t.Fatalf("restart produced a different run: %#v", replayed.Run)
	}
	if len(replayed.WorkItems) != len(first.WorkItems) {
		t.Fatalf("restart returned %d work items, want %d", len(replayed.WorkItems), len(first.WorkItems))
	}
	for index, item := range replayed.WorkItems {
		if item.ID != first.WorkItems[index].ID {
			t.Fatalf("restart returned different work items: %s and %s", item.ID, first.WorkItems[index].ID)
		}
	}

	// And the key still refuses to mean something else, without mutating
	// anything it refused.
	var conflict app.RunConflictError
	if _, err := app.UnwrapMutation(restarted.CreatePlanRun(h.ctx, app.CreatePlanRunCommand{
		ObjectiveID: h.objective.ID, PlanID: h.plan.ID, ActorID: "agent:two",
		IdempotencyKey: "create-conflicting-after-restart", RunKey: "weekly",
		Bindings: []app.RunInputBindingCommand{{Name: "window", Value: "2026-Q2"}},
	})); !errors.As(err, &conflict) {
		t.Fatalf("the same key with different bindings after restart = %v, want a run conflict", err)
	}
	page, err := restarted.ListPlanRuns(h.ctx, ports.PlanRunFilter{ObjectiveID: h.objective.ID})
	if err != nil {
		t.Fatal(err)
	}
	if page.Total != 1 {
		t.Fatalf("the objective holds %d runs after a refused conflict, want 1", page.Total)
	}
}

// TestCapacityCountsActiveRunsAcrossRevisions is the objective-wide half of the
// limit: it counts executions, not executions of one definition, so an active
// run of an earlier revision blocks a run of a later one.
func TestCapacityCountsActiveRunsAcrossRevisions(t *testing.T) {
	h := newRunHarness(t, "capacity-revisions.db", nil, 0)
	first, err := h.createRun("run-1", "create-1", "agent:one")
	if err != nil {
		t.Fatal(err)
	}
	second, err := app.UnwrapMutation(h.service.ProposePlan(h.ctx, app.ProposePlanCommand{
		ObjectiveID: h.objective.ID, ActorID: "agent:one", IdempotencyKey: "propose-revision-2",
		Title: "A second revision", Revision: 2,
		Steps: []app.ProposedPlanStep{{
			ClientRef: "only", Key: "RUN-SECOND-REVISION", Title: "A step of the second revision", Kind: "research", Required: true,
			Priority: work.PriorityMedium, EstimatedScope: work.ScopeSmall,
			ExecutionPolicy: work.PolicyAutonomousWithReport, RequiredActorKind: work.ActorAgent,
		}},
	}))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := app.UnwrapMutation(h.service.ReviewPlan(h.ctx, app.ReviewPlanCommand{
		PlanID: second.Plan.ID, ReviewerActorID: "human:owner", IdempotencyKey: "approve-revision-2",
		Decision: work.PlanApproved, Reason: "The second definition is complete.", ExpectedVersion: 1,
	})); err != nil {
		t.Fatal(err)
	}
	var capacity app.RunCapacityError
	if _, err := app.UnwrapMutation(h.service.CreatePlanRun(h.ctx, app.CreatePlanRunCommand{
		ObjectiveID: h.objective.ID, PlanID: second.Plan.ID, ActorID: "agent:one",
		IdempotencyKey: "run-revision-2", RunKey: "run-2",
	})); !errors.As(err, &capacity) {
		t.Fatalf("a run of another revision under an occupied limit = %v, want a capacity refusal", err)
	}
	// Closing the first run frees the slot for the other revision, which is
	// what makes the limit a capacity rather than a lock on one definition.
	if _, err := app.UnwrapMutation(h.service.ClosePlanRun(h.ctx, app.ClosePlanRunCommand{
		PlanRunID: first.Run.ID, ActorID: "agent:one", IdempotencyKey: "cancel-1",
		ExpectedVersion: first.Run.Version, TargetStatus: work.PlanRunCancelled, Reason: "Making room deliberately.",
	})); err != nil {
		t.Fatal(err)
	}
	if _, err := app.UnwrapMutation(h.service.CreatePlanRun(h.ctx, app.CreatePlanRunCommand{
		ObjectiveID: h.objective.ID, PlanID: second.Plan.ID, ActorID: "agent:one",
		IdempotencyKey: "run-revision-2-again", RunKey: "run-2",
	})); err != nil {
		t.Fatalf("the freed slot did not admit a run of the other revision: %v", err)
	}
}

// TestConcurrentCreationCannotExceedTheLimit drives two independent database
// handles at the same objective at once, which is how two harnesses on one
// machine actually meet. Exactly as many runs as the limit permits may exist
// afterwards, whatever order the two attempts land in.
//
// It also guards the transaction mode. Under deferred transactions this passed
// for the wrong reason: both writers read a count of zero and SQLite aborted
// one on the lock upgrade, so the limit held by accident and the caller got
// "database is locked" instead of being told it was at capacity. The
// assertions below refuse that outcome.
func TestConcurrentCreationCannotExceedTheLimit(t *testing.T) {
	h := newRunHarness(t, "capacity-race.db", nil, 0)
	first := h.reopen(t)
	second := h.reopen(t)

	start := make(chan struct{})
	var group sync.WaitGroup
	results := make([]error, 2)
	for index, service := range []*app.Service{first, second} {
		group.Add(1)
		go func(index int, service *app.Service) {
			defer group.Done()
			<-start
			_, err := app.UnwrapMutation(service.CreatePlanRun(h.ctx, app.CreatePlanRunCommand{
				ObjectiveID: h.objective.ID, PlanID: h.plan.ID, ActorID: "agent:one",
				IdempotencyKey: "race-" + string(rune('a'+index)), RunKey: "race-" + string(rune('a'+index)),
			}))
			results[index] = err
		}(index, service)
	}
	close(start)
	group.Wait()

	succeeded := 0
	for index, err := range results {
		if err == nil {
			succeeded++
			continue
		}
		// The loser must be refused by one of the two mechanisms that actually
		// enforce the limit: the capacity check, or the uniqueness of the
		// objective's run sequence, which is what makes two creations unable
		// to agree on a number. A lock or busy error is neither — it would
		// mean the limit held by accident, and a test that accepts it passes
		// against an implementation that does not enforce anything.
		var capacity app.RunCapacityError
		if !errors.As(err, &capacity) && !isSequenceCollision(err) {
			t.Fatalf("attempt %d was not refused by the capacity check or the run sequence constraint: %v", index, err)
		}
	}
	if succeeded != 1 {
		t.Fatalf("%d of two concurrent creations succeeded under a limit of one: %v", succeeded, results)
	}
	page, err := h.service.ListPlanRuns(h.ctx, ports.PlanRunFilter{
		ObjectiveID: h.objective.ID, Statuses: []work.PlanRunStatus{work.PlanRunActive},
	})
	if err != nil {
		t.Fatal(err)
	}
	if page.Total != 1 {
		t.Fatalf("the objective holds %d active runs after the race, want 1", page.Total)
	}
}

// isSequenceCollision recognizes the other shape a losing concurrent creation
// can legitimately take: UNIQUE(objective_id, sequence) rejecting the second
// writer that read the same highest sequence. That constraint, not the count,
// is what makes the capacity limit atomic rather than advisory.
func isSequenceCollision(err error) bool {
	return err != nil && strings.Contains(err.Error(), "UNIQUE constraint failed: plan_runs.objective_id, plan_runs.sequence")
}

// TestASecondRunSharesNoIdentityWithTheFirst is the separateness invariant
// checked across every kind of record a run accumulates, not only its work
// items — and checked again through a second process, so it is a property of
// what was stored rather than of what one session happened to hold.
func TestASecondRunSharesNoIdentityWithTheFirst(t *testing.T) {
	h := newRunHarness(t, "disjoint-identities.db", nil, 2)
	first, err := h.createRun("run-1", "create-1", "agent:one")
	if err != nil {
		t.Fatal(err)
	}
	// Give the first run some history of every kind: a claim, an output, an
	// external action, and its own activity.
	firstItem := accumulateRunHistory(t, h, first.WorkItems[0], "first")
	before, err := h.service.GetPlanRun(h.ctx, first.Run.ID)
	if err != nil {
		t.Fatal(err)
	}
	beforeContext, err := h.service.GetWorkItem(h.ctx, firstItem.ID)
	if err != nil {
		t.Fatal(err)
	}

	second, err := h.createRun("run-2", "create-2", "agent:one")
	if err != nil {
		t.Fatal(err)
	}
	secondItem := accumulateRunHistory(t, h, second.WorkItems[0], "second")
	secondContext, err := h.service.GetWorkItem(h.ctx, secondItem.ID)
	if err != nil {
		t.Fatal(err)
	}

	// Each run carried its action to a recorded terminal result, so there is
	// execution evidence on both sides to compare rather than only a proposal.
	for _, side := range []ports.WorkItemContext{beforeContext, secondContext} {
		if len(side.ExternalActions) != 1 || len(side.ExternalActions[0].Executions) != 1 || len(side.ExternalActions[0].Grants) != 1 {
			t.Fatalf("a run is missing its authority or execution evidence: %#v", side.ExternalActions)
		}
		if side.ExternalActions[0].Executions[0].State != authority.ExecutionSucceeded {
			t.Fatalf("a run's execution did not reach a terminal result: %#v", side.ExternalActions[0].Executions[0])
		}
	}
	for name, pair := range map[string][2]string{
		"work item":       {beforeContext.WorkItem.ID, secondContext.WorkItem.ID},
		"claim":           {beforeContext.Claims[0].ID, secondContext.Claims[0].ID},
		"expected output": {beforeContext.ExpectedOutputs[0].ExpectedOutput.ID, secondContext.ExpectedOutputs[0].ExpectedOutput.ID},
		"output revision": {beforeContext.OutputRevisions[0].Revision.ID, secondContext.OutputRevisions[0].Revision.ID},
		"external action": {beforeContext.ExternalActions[0].Action.ID, secondContext.ExternalActions[0].Action.ID},
		"authority grant": {beforeContext.ExternalActions[0].Grants[0].ID, secondContext.ExternalActions[0].Grants[0].ID},
		"execution evidence": {
			beforeContext.ExternalActions[0].Executions[0].ID,
			secondContext.ExternalActions[0].Executions[0].ID,
		},
		"artifact": {beforeContext.Artifacts[0].ID, secondContext.Artifacts[0].ID},
	} {
		if pair[0] == pair[1] {
			t.Fatalf("the two runs share a %s: %s", name, pair[0])
		}
		if pair[0] == "" || pair[1] == "" {
			t.Fatalf("a run is missing its %s: %q and %q", name, pair[0], pair[1])
		}
	}

	// Activity is per run too: each run's work has its own trail, and neither
	// appears in the other's.
	firstActivity, err := h.service.ListActivity(h.ctx, ports.ActivityFilter{WorkItemID: firstItem.ID})
	if err != nil {
		t.Fatal(err)
	}
	secondActivity, err := h.service.ListActivity(h.ctx, ports.ActivityFilter{WorkItemID: secondItem.ID})
	if err != nil {
		t.Fatal(err)
	}
	if len(firstActivity) == 0 || len(secondActivity) == 0 {
		t.Fatalf("a run's work has no activity: %d and %d", len(firstActivity), len(secondActivity))
	}
	seen := map[string]bool{}
	for _, entry := range firstActivity {
		seen[entry.ID] = true
	}
	for _, entry := range secondActivity {
		if seen[entry.ID] {
			t.Fatalf("the two runs share activity %s", entry.ID)
		}
	}

	// The first run is byte-identical afterwards, read back through a second
	// process rather than from this one's memory.
	restarted := h.reopen(t)
	after, err := restarted.GetPlanRun(h.ctx, first.Run.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(after.Run, before.Run) {
		t.Fatalf("the first run changed:\n before %#v\n after  %#v", before.Run, after.Run)
	}
	if !reflect.DeepEqual(after.WorkItems, before.WorkItems) {
		t.Fatalf("the first run's work items changed after a second run and a reopen")
	}
	afterContext, err := restarted.GetWorkItem(h.ctx, firstItem.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(afterContext.OutputRevisions, beforeContext.OutputRevisions) {
		t.Fatalf("the first run's outputs changed after a second run and a reopen")
	}
}

// accumulateRunHistory gives one run's work item a claim, an artifact, an
// expected output with a produced revision, and an external action proposal, so
// a separateness check has something of every kind to compare.
func accumulateRunHistory(t *testing.T, h *runHarness, item work.WorkItem, label string) work.WorkItem {
	t.Helper()
	ready, err := app.UnwrapMutation(h.service.TransitionWorkItem(h.ctx, app.TransitionWorkItemCommand{
		WorkItemID: item.ID, TargetStatus: work.StatusReady, ActorID: "agent:one",
		Reason: "Queue " + label + ".", ExpectedVersion: item.Version, IdempotencyKey: "ready-" + label,
	}))
	if err != nil {
		t.Fatal(err)
	}
	claimed, err := app.UnwrapMutation(h.service.ClaimWorkItem(h.ctx, app.ClaimWorkItemCommand{
		WorkItemID: item.ID, ActorID: "agent:one", ExpectedVersion: ready.Version,
		LeaseDuration: time.Hour, IdempotencyKey: "claim-" + label, TransitionToInProgress: true,
	}))
	if err != nil {
		t.Fatal(err)
	}
	current := claimed.WorkItem
	artifact, err := app.UnwrapMutation(h.service.AttachArtifact(h.ctx, app.AttachArtifactCommand{
		WorkItemID: item.ID, ActorID: "agent:one", ExpectedVersion: current.Version, IdempotencyKey: "artifact-" + label,
		Kind: "document", URI: "throughline://runs/" + label, Title: "Evidence for " + label,
	}))
	if err != nil {
		t.Fatal(err)
	}
	current = artifact.WorkItem
	defined, err := app.UnwrapMutation(h.service.DefineExpectedOutput(h.ctx, app.DefineExpectedOutputCommand{
		WorkItemID: item.ID, ActorID: "agent:one", Name: "Dossier for " + label, ProfileName: "research_dossier",
		ProfileVersion: 1, Ordinal: 1, Required: true, ExpectedVersion: current.Version, IdempotencyKey: "define-" + label,
	}))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := app.UnwrapMutation(h.service.CreateOutputRevision(h.ctx, app.CreateOutputRevisionCommand{
		ExpectedOutputID: defined.ID, ActorID: "agent:one", IdempotencyKey: "revision-" + label,
		ContentDigest: "sha256:" + label,
		Artifacts:     []app.OutputArtifactInput{{Kind: "document", URI: "file:///" + label + ".md", Title: "Dossier", Role: "primary"}},
	})); err != nil {
		t.Fatal(err)
	}
	reread, err := h.service.GetWorkItem(h.ctx, item.ID)
	if err != nil {
		t.Fatal(err)
	}
	// The action is carried all the way to a recorded terminal result, so the
	// run accumulates execution evidence and not only a proposal — which is
	// one of the identities the separateness claim names.
	proposed, err := app.UnwrapMutation(h.service.ProposeExternalAction(h.ctx, app.ProposeExternalActionCommand{
		WorkItemID: item.ID, ActorID: "agent:one", ExpectedVersion: reread.WorkItem.Version, IdempotencyKey: "action-" + label,
		Required: false, Title: "Publish " + label, Rationale: "Gives the run an action of its own.",
		Subject: []byte(`{"action_type":"knowledge.publish","target":{"collection":"runs"},"arguments":[],"scope":{},"permissions":["knowledge.write"],"credential_requirements":[],"constraints":{}}`),
	}))
	if err != nil {
		t.Fatal(err)
	}
	requested, err := app.UnwrapMutation(h.service.RequestExternalActionApproval(h.ctx, app.RequestExternalActionApprovalCommand{
		ActionID: proposed.Action.ID, ActorID: "agent:one", ExpectedActionVersion: proposed.Action.Version,
		ExpectedSubjectHash: proposed.Revision.AuthorizationSubjectHash, IdempotencyKey: "request-" + label,
		ApprovedForActorID: "agent:one", Constraints: []byte(`{}`), Request: "Authorize this publication.",
	}))
	if err != nil {
		t.Fatal(err)
	}
	resolved, err := app.UnwrapMutation(h.service.ResolveExternalActionApproval(h.ctx, app.ResolveExternalActionApprovalCommand{
		ApprovalID: requested.ID, ActorID: "human:owner", ExpectedActionVersion: proposed.Action.Version,
		IdempotencyKey: "resolve-" + label, Decision: authority.ApprovalApproved, Rationale: "The scope is appropriate.",
	}))
	if err != nil {
		t.Fatal(err)
	}
	started, err := app.UnwrapMutation(h.service.StartExternalActionExecution(h.ctx, app.StartExternalActionExecutionCommand{
		ActionID: resolved.Action.ID, ActorID: "agent:one", ExpectedActionVersion: resolved.Action.Version,
		IdempotencyKey: "start-" + label, SubjectHash: proposed.Revision.AuthorizationSubjectHash, AuthorityGrantID: resolved.Grant.ID,
	}))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := app.UnwrapMutation(h.service.CompleteExternalActionExecution(h.ctx, app.CompleteExternalActionExecutionCommand{
		ExecutionID: started.Execution.ID, ActorID: "agent:one", ExpectedActionVersion: started.Action.Version,
		IdempotencyKey: "complete-" + label, State: authority.ExecutionSucceeded,
		Result: []byte(`{"status":"published"}`), EvidenceArtifactID: artifact.Artifact.ID,
	})); err != nil {
		t.Fatal(err)
	}
	final, err := h.service.GetWorkItem(h.ctx, item.ID)
	if err != nil {
		t.Fatal(err)
	}
	return final.WorkItem
}

// TestListPlanRunsIsBoundedAndFiltered covers the read a resuming session
// depends on: it must be able to narrow to one objective, one revision or one
// status, page through the result, and be told when there is more — without
// anything picking a run on its behalf.
func TestListPlanRunsIsBoundedAndFiltered(t *testing.T) {
	h := newRunHarness(t, "list-runs.db", nil, 3)
	var created []string
	for index, key := range []string{"run-1", "run-2", "run-3"} {
		run, err := h.createRun(key, "create-"+key, "agent:one")
		if err != nil {
			t.Fatal(err)
		}
		created = append(created, run.Run.ID)
		if index == 0 {
			if _, err := app.UnwrapMutation(h.service.ClosePlanRun(h.ctx, app.ClosePlanRunCommand{
				PlanRunID: run.Run.ID, ActorID: "agent:one", IdempotencyKey: "cancel-" + key,
				ExpectedVersion: run.Run.Version, TargetStatus: work.PlanRunCancelled, Reason: "One terminal run to filter out.",
			})); err != nil {
				t.Fatal(err)
			}
		}
	}

	all, err := h.service.ListPlanRuns(h.ctx, ports.PlanRunFilter{ObjectiveID: h.objective.ID})
	if err != nil {
		t.Fatal(err)
	}
	if all.Total != 3 || len(all.Runs) != 3 || all.HasMore {
		t.Fatalf("unfiltered listing = total %d, %d runs, has more %v", all.Total, len(all.Runs), all.HasMore)
	}
	for _, summary := range all.Runs {
		if summary.PlanRevision != 1 || summary.WorkItems != 2 {
			t.Fatalf("run summary = %#v, want revision 1 and two work items", summary)
		}
	}
	// The cancelled run's work is cancelled with it, which is the number a
	// resuming session reads to see there is nothing left in it.
	for _, summary := range all.Runs {
		if summary.Run.Status == work.PlanRunCancelled && summary.Cancelled != summary.WorkItems {
			t.Fatalf("a cancelled run reports %d of %d work items cancelled", summary.Cancelled, summary.WorkItems)
		}
	}

	active, err := h.service.ListPlanRuns(h.ctx, ports.PlanRunFilter{
		ObjectiveID: h.objective.ID, Statuses: []work.PlanRunStatus{work.PlanRunActive},
	})
	if err != nil {
		t.Fatal(err)
	}
	if active.Total != 2 {
		t.Fatalf("active listing = %d runs, want 2", active.Total)
	}
	for _, summary := range active.Runs {
		if summary.Run.Status != work.PlanRunActive {
			t.Fatalf("status filter returned a %s run", summary.Run.Status)
		}
	}

	page, err := h.service.ListPlanRuns(h.ctx, ports.PlanRunFilter{ObjectiveID: h.objective.ID, Limit: 2})
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Runs) != 2 || !page.HasMore || page.Total != 3 {
		t.Fatalf("first page = %d runs, has more %v, total %d", len(page.Runs), page.HasMore, page.Total)
	}
	rest, err := h.service.ListPlanRuns(h.ctx, ports.PlanRunFilter{ObjectiveID: h.objective.ID, Offset: 2, Limit: 2})
	if err != nil {
		t.Fatal(err)
	}
	if len(rest.Runs) != 1 || rest.HasMore {
		t.Fatalf("second page = %d runs, has more %v", len(rest.Runs), rest.HasMore)
	}
	seen := map[string]bool{}
	for _, summary := range append(append([]ports.PlanRunSummary{}, page.Runs...), rest.Runs...) {
		if seen[summary.Run.ID] {
			t.Fatalf("paging returned run %s twice", summary.Run.ID)
		}
		seen[summary.Run.ID] = true
	}
	for _, id := range created {
		if !seen[id] {
			t.Fatalf("paging lost run %s", id)
		}
	}
	if _, err := h.service.ListPlanRuns(h.ctx, ports.PlanRunFilter{ObjectiveID: h.objective.ID, Offset: 99}); err == nil {
		t.Fatal("an offset past the end was accepted")
	}
	if _, err := h.service.ListPlanRuns(h.ctx, ports.PlanRunFilter{
		ObjectiveID: h.objective.ID, Statuses: []work.PlanRunStatus{"finished"},
	}); err == nil {
		t.Fatal("an unknown run status was accepted as a filter")
	}
}

// TestObjectiveContextCarriesItsRuns is what makes resuming possible without a
// second question: the objective a session reads to orient itself already says
// which executions exist and how much of each is left.
func TestObjectiveContextCarriesItsRuns(t *testing.T) {
	h := newRunHarness(t, "objective-runs.db", nil, 2)
	first, err := h.createRun("run-1", "create-1", "agent:one")
	if err != nil {
		t.Fatal(err)
	}
	second, err := h.createRun("run-2", "create-2", "agent:one")
	if err != nil {
		t.Fatal(err)
	}
	carryWorkItemToDone(t, h, first.WorkItems[0], "first-step")

	recovered, err := h.service.GetObjectiveContext(h.ctx, h.objective.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(recovered.PlanRuns) != 2 {
		t.Fatalf("objective context carries %d runs, want 2", len(recovered.PlanRuns))
	}
	byID := map[string]ports.PlanRunSummary{}
	for _, summary := range recovered.PlanRuns {
		byID[summary.Run.ID] = summary
	}
	if got := byID[first.Run.ID]; got.Done != 1 || got.WorkItems != 2 {
		t.Fatalf("the run with one finished step summarizes as %#v", got)
	}
	if got := byID[second.Run.ID]; got.Done != 0 || got.WorkItems != 2 {
		t.Fatalf("the untouched run summarizes as %#v", got)
	}
	if byID[first.Run.ID].PlanRevision != 1 || byID[second.Run.ID].PlanRevision != 1 {
		t.Fatalf("run summaries lost their plan revision: %#v", recovered.PlanRuns)
	}
}

// TestItemsCanBeScopedToOneRun is the other half of continuing the right run: a
// session that has chosen a run must be able to list that run's work, and
// plan_id cannot do it because every run of a revision answers to the same one.
func TestItemsCanBeScopedToOneRun(t *testing.T) {
	h := newRunHarness(t, "item-run-scope.db", nil, 2)
	first, err := h.createRun("run-1", "create-1", "agent:one")
	if err != nil {
		t.Fatal(err)
	}
	second, err := h.createRun("run-2", "create-2", "agent:one")
	if err != nil {
		t.Fatal(err)
	}
	items, err := h.service.ListWorkItems(h.ctx)
	if err != nil {
		t.Fatal(err)
	}
	perRun := map[string]int{}
	sharedPlan := map[string]bool{}
	for _, item := range items {
		if item.WorkItem.ObjectiveID != h.objective.ID || item.WorkItem.PlanRunID == "" {
			continue
		}
		perRun[item.WorkItem.PlanRunID]++
		if item.Plan != nil {
			sharedPlan[item.Plan.ID] = true
		}
		if item.WorkItem.OriginPlanStepID == "" || item.WorkItem.Origin != work.OriginPlanStep {
			t.Fatalf("a materialized item lost its provenance: %#v", item.WorkItem)
		}
	}
	if perRun[first.Run.ID] != 2 || perRun[second.Run.ID] != 2 {
		t.Fatalf("work per run = %#v, want two each", perRun)
	}
	if len(sharedPlan) != 1 {
		t.Fatalf("the two runs' items span %d plan revisions, want 1 — which is why plan_id cannot scope a run", len(sharedPlan))
	}
}

// TestReadyWorkIsScopedToActiveRuns pins that the queue a harness pulls from
// never offers work whose run has ended, and that closing one run does not
// touch what another run is offering.
func TestReadyWorkIsScopedToActiveRuns(t *testing.T) {
	h := newRunHarness(t, "ready-run-scope.db", nil, 2)
	first, err := h.createRun("run-1", "create-1", "agent:one")
	if err != nil {
		t.Fatal(err)
	}
	second, err := h.createRun("run-2", "create-2", "agent:one")
	if err != nil {
		t.Fatal(err)
	}
	for label, run := range map[string]ports.PlanRunContext{"first": first, "second": second} {
		item := run.WorkItems[0]
		if _, err := app.UnwrapMutation(h.service.TransitionWorkItem(h.ctx, app.TransitionWorkItemCommand{
			WorkItemID: item.ID, TargetStatus: work.StatusReady, ActorID: "agent:one",
			Reason: "Queue " + label + ".", ExpectedVersion: item.Version, IdempotencyKey: "ready-" + label,
		})); err != nil {
			t.Fatal(err)
		}
	}
	ready, err := h.service.ListReadyWork(h.ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(ready) != 2 {
		t.Fatalf("ready work = %d items, want one from each active run", len(ready))
	}
	if _, err := app.UnwrapMutation(h.service.ClosePlanRun(h.ctx, app.ClosePlanRunCommand{
		PlanRunID: first.Run.ID, ActorID: "agent:one", IdempotencyKey: "cancel-first",
		ExpectedVersion: first.Run.Version, TargetStatus: work.PlanRunCancelled, Reason: "Abandoned.",
	})); err != nil {
		t.Fatal(err)
	}
	ready, err = h.service.ListReadyWork(h.ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(ready) != 1 {
		t.Fatalf("ready work after closing one run = %d items, want 1", len(ready))
	}
	if ready[0].WorkItem.PlanRunID != second.Run.ID {
		t.Fatalf("ready work belongs to %s, want the still-active run %s", ready[0].WorkItem.PlanRunID, second.Run.ID)
	}
}
