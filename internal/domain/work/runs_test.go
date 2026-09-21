package work

import (
	"strings"
	"testing"
	"time"
)

var runFixtureTime = time.Date(2026, 8, 21, 15, 0, 0, 0, time.UTC)

func validPlanRun() PlanRun {
	return PlanRun{
		ID: "run-1", ObjectiveID: "objective-1", PlanID: "plan-1", RunKey: "weekly-2026-W34",
		Sequence: 1, BindingFingerprint: "fingerprint", StartedBy: "agent:one",
	}
}

// TestNewPlanRunIsActiveImmediately pins the decision that runs have no
// preparatory state: everything a run needs is supplied when it is created, so
// there is nothing a created run could still be waiting for.
func TestNewPlanRunIsActiveImmediately(t *testing.T) {
	run, err := NewPlanRun(validPlanRun(), runFixtureTime)
	if err != nil {
		t.Fatal(err)
	}
	if run.Status != PlanRunActive || run.Version != 1 || run.StartedAt.IsZero() {
		t.Fatalf("new plan run = %#v", run)
	}
	if !run.ClosedAt.IsZero() || run.ClosedBy != "" {
		t.Fatalf("new plan run carries closure fields: %#v", run)
	}
}

func TestNewPlanRunRequiresItsIdentity(t *testing.T) {
	for name, mutate := range map[string]func(*PlanRun){
		"no run key":     func(r *PlanRun) { r.RunKey = "" },
		"no objective":   func(r *PlanRun) { r.ObjectiveID = "" },
		"no plan":        func(r *PlanRun) { r.PlanID = "" },
		"no actor":       func(r *PlanRun) { r.StartedBy = "" },
		"no sequence":    func(r *PlanRun) { r.Sequence = 0 },
		"no fingerprint": func(r *PlanRun) { r.BindingFingerprint = "" },
	} {
		t.Run(name, func(t *testing.T) {
			run := validPlanRun()
			mutate(&run)
			if _, err := NewPlanRun(run, runFixtureTime); err == nil {
				t.Fatalf("a plan run with %s was accepted", name)
			}
		})
	}
}

// TestClosePlanRunEnforcesItsTargets covers the whole closure contract in one
// place: success is gated on the run's obligations, failure and cancellation
// need a rationale, and a terminal run is irreversible.
func TestClosePlanRunEnforcesItsTargets(t *testing.T) {
	run, err := NewPlanRun(validPlanRun(), runFixtureTime)
	if err != nil {
		t.Fatal(err)
	}
	met := RunClosureFacts{RequiredStepItemsDone: true, RemainingItemsTerminal: true, OutputObligationsSatisfied: true, ActionObligationsSatisfied: true}

	if _, err := ClosePlanRun(run, PlanRunSucceeded, "agent:one", "", RunClosureFacts{}, runFixtureTime); err == nil {
		t.Fatal("a run with unmet obligations was allowed to succeed")
	}
	if _, err := ClosePlanRun(run, PlanRunFailed, "agent:one", "", met, runFixtureTime); err == nil {
		t.Fatal("a run was failed without a rationale")
	}
	if _, err := ClosePlanRun(run, PlanRunCancelled, "agent:one", "", met, runFixtureTime); err == nil {
		t.Fatal("a run was cancelled without a rationale")
	}
	if _, err := ClosePlanRun(run, PlanRunActive, "agent:one", "", met, runFixtureTime); err == nil {
		t.Fatal("a run was closed as active")
	}
	if _, err := ClosePlanRun(run, PlanRunSucceeded, "", "", met, runFixtureTime); err == nil {
		t.Fatal("a run was closed without an actor")
	}

	succeeded, err := ClosePlanRun(run, PlanRunSucceeded, "agent:one", "", met, runFixtureTime)
	if err != nil {
		t.Fatal(err)
	}
	if !succeeded.Status.Terminal() || succeeded.ClosedBy != "agent:one" || succeeded.ClosedAt.IsZero() || succeeded.Version != 2 {
		t.Fatalf("succeeded run = %#v", succeeded)
	}
	if _, err := ClosePlanRun(succeeded, PlanRunFailed, "agent:one", "Reconsidered.", met, runFixtureTime); err == nil {
		t.Fatal("a terminal run was closed again")
	}
}

// TestClosePlanRunNamesEveryUnmetObligation matters because a refusal that
// says only "not ready" leaves the caller guessing what to finish.
func TestClosePlanRunNamesEveryUnmetObligation(t *testing.T) {
	run, err := NewPlanRun(validPlanRun(), runFixtureTime)
	if err != nil {
		t.Fatal(err)
	}
	_, err = ClosePlanRun(run, PlanRunSucceeded, "agent:one", "", RunClosureFacts{}, runFixtureTime)
	if err == nil {
		t.Fatal("expected a refusal")
	}
	for _, fragment := range []string{"required plan step", "terminal", "outputs", "external actions"} {
		if !strings.Contains(err.Error(), fragment) {
			t.Fatalf("closure refusal %q does not mention %q", err, fragment)
		}
	}
}

// TestRunGateSatisfiedFollowsOrigin is the single rule that decides whether
// anything may execute, so each branch is pinned rather than inferred from the
// call sites.
func TestRunGateSatisfiedFollowsOrigin(t *testing.T) {
	for name, probe := range map[string]struct {
		origin    WorkItemOrigin
		runActive bool
		want      bool
	}{
		"plan step in an active run": {OriginPlanStep, true, true},
		"plan step in a closed run":  {OriginPlanStep, false, false},
		"unplanned work":             {OriginUnplanned, false, false},
		"unplanned work, run active": {OriginUnplanned, true, false},
		"legacy work":                {OriginLegacy, false, true},
	} {
		t.Run(name, func(t *testing.T) {
			satisfied, message := RunGateSatisfied(probe.origin, probe.runActive)
			if satisfied != probe.want {
				t.Fatalf("RunGateSatisfied(%q, %v) = %v, want %v", probe.origin, probe.runActive, satisfied, probe.want)
			}
			if !satisfied && message == "" {
				t.Fatal("a refusal carries no explanation")
			}
		})
	}
}

// TestRunCreationFingerprintIdentifiesContentNotOrder pins what a replayed run
// key is compared against: the same revision and the same bindings, whatever
// order the caller sent them in.
func TestRunCreationFingerprintIdentifiesContentNotOrder(t *testing.T) {
	window := RunInputBinding{Name: "window", Kind: BindingValue, Value: "2026-Q1"}
	source := RunInputBinding{Name: "source", Kind: BindingExternal, Locator: "file:///a", Digest: "sha256:abc"}
	forward := RunCreationFingerprint("plan-1", []RunInputBinding{window, source})
	reversed := RunCreationFingerprint("plan-1", []RunInputBinding{source, window})
	if forward != reversed {
		t.Fatal("binding order changed the fingerprint")
	}
	if RunCreationFingerprint("plan-2", []RunInputBinding{window, source}) == forward {
		t.Fatal("a different plan revision produced the same fingerprint")
	}
	changed := window
	changed.Value = "2026-Q2"
	if RunCreationFingerprint("plan-1", []RunInputBinding{changed, source}) == forward {
		t.Fatal("a changed binding value produced the same fingerprint")
	}
}

// TestRunInputBindingRequiresExactlyOneShape covers the identity rule: a run
// must be able to say precisely what it worked from.
func TestRunInputBindingRequiresExactlyOneShape(t *testing.T) {
	base := RunInputBinding{ID: "binding-1", PlanRunID: "run-1", PlanInputID: "input-1", Name: "window", CreatedBy: "agent:one"}
	for name, probe := range map[string]struct {
		mutate func(*RunInputBinding)
		valid  bool
	}{
		"a literal value":               {func(b *RunInputBinding) { b.Kind, b.Value = BindingValue, "2026-Q1" }, true},
		"an empty value":                {func(b *RunInputBinding) { b.Kind = BindingValue }, false},
		"an exact output revision":      {func(b *RunInputBinding) { b.Kind, b.OutputRevisionID = BindingOutputRevision, "revision-1" }, true},
		"no output revision":            {func(b *RunInputBinding) { b.Kind = BindingOutputRevision }, false},
		"a locator with a version":      {func(b *RunInputBinding) { b.Kind, b.Locator, b.SourceVersion = BindingExternal, "file:///a", "v7" }, true},
		"a locator with a digest":       {func(b *RunInputBinding) { b.Kind, b.Locator, b.Digest = BindingExternal, "file:///a", "sha256:abc" }, true},
		"a bare locator":                {func(b *RunInputBinding) { b.Kind, b.Locator = BindingExternal, "file:///a" }, false},
		"an external without a locator": {func(b *RunInputBinding) { b.Kind, b.Digest = BindingExternal, "sha256:abc" }, false},
		"an unknown kind":               {func(b *RunInputBinding) { b.Kind = "guess" }, false},
	} {
		t.Run(name, func(t *testing.T) {
			binding := base
			probe.mutate(&binding)
			result, err := NewRunInputBinding(binding, runFixtureTime)
			if probe.valid != (err == nil) {
				t.Fatalf("NewRunInputBinding(%s) error = %v, want valid = %v", name, err, probe.valid)
			}
			if err != nil {
				return
			}
			// The shapes are mutually exclusive: a stored binding carries the
			// fields of its own kind and nothing else.
			switch result.Kind {
			case BindingValue:
				if result.OutputRevisionID != "" || result.Locator != "" {
					t.Fatalf("value binding kept other fields: %#v", result)
				}
			case BindingOutputRevision:
				if result.Value != "" || result.Locator != "" {
					t.Fatalf("output revision binding kept other fields: %#v", result)
				}
			case BindingExternal:
				if result.Value != "" || result.OutputRevisionID != "" {
					t.Fatalf("external binding kept other fields: %#v", result)
				}
			}
		})
	}
}

// TestMaterializedKeysAreDistinctPerRun is why a plan step's key may not carry
// the separator: two runs of the same revision must not collide.
func TestMaterializedKeysAreDistinctPerRun(t *testing.T) {
	if MaterializedWorkItemKey("TH-STEP", 1) == MaterializedWorkItemKey("TH-STEP", 2) {
		t.Fatal("two runs produced the same work item key")
	}
	step := PlanStep{
		ID: "step-1", PlanID: "plan-1", ClientRef: "only", Key: "TH-STEP/1", Title: "Ambiguous key", Kind: "research",
		Priority: PriorityMedium, EstimatedScope: ScopeSmall, ExecutionPolicy: PolicyAutonomousWithReport,
		RequiredActorKind: ActorAgent, Ordinal: 1,
	}
	if _, err := NewPlanStep(step, runFixtureTime); err == nil {
		t.Fatal("a step key carrying the run separator was accepted")
	}
}

// TestPlanStepRejectsAProfileOutputRequirement pins the rule that keeps a
// reusable definition from reaching implicitly into another run's results.
func TestPlanStepRejectsAProfileOutputRequirement(t *testing.T) {
	step := PlanStep{
		ID: "step-1", PlanID: "plan-1", ClientRef: "only", Key: "TH-STEP", Title: "Needs an input", Kind: "research",
		Priority: PriorityMedium, EstimatedScope: ScopeSmall, ExecutionPolicy: PolicyAutonomousWithReport,
		RequiredActorKind: ActorAgent, Ordinal: 1,
		Definition: StepDefinition{OutputRequirements: []StepOutputRequirement{{Required: true, Note: "Any accepted dossier."}}},
	}
	if _, err := NewPlanStep(step, runFixtureTime); err == nil {
		t.Fatal("a plan step requirement without an exact output revision was accepted")
	}
	step.Definition.OutputRequirements[0].RequiredOutputRevisionID = "revision-1"
	if _, err := NewPlanStep(step, runFixtureTime); err != nil {
		t.Fatalf("a plan step requiring one exact revision was rejected: %v", err)
	}
}

// TestObjectiveModeAndCapacityDefaultSafely covers the two fields that decide
// how an objective ends and how much may run at once.
func TestObjectiveModeAndCapacityDefaultSafely(t *testing.T) {
	objective, err := NewObjective(Objective{
		ID: "objective-1", Key: "OBJ-1", Title: "Defaults", DesiredOutcome: "Safe by default",
		Phase: ObjectiveIdea, Priority: PriorityMedium,
	}, runFixtureTime)
	if err != nil {
		t.Fatal(err)
	}
	if objective.Mode != ObjectiveFinite || objective.MaxConcurrentRuns != 1 {
		t.Fatalf("objective defaults = %#v", objective)
	}
	objective.MaxConcurrentRuns = 0
	if err := objective.Validate(); err == nil {
		t.Fatal("an objective permitting no runs at all was accepted")
	}
	objective.MaxConcurrentRuns = 1
	objective.Mode = "occasional"
	if err := objective.Validate(); err == nil {
		t.Fatal("an unknown objective mode was accepted")
	}
}

// TestWorkItemProvenanceIsConsistent stops a work item from claiming a plan
// step origin without saying which run and step it came from, or carrying run
// provenance it did not get from a step.
func TestWorkItemProvenanceIsConsistent(t *testing.T) {
	base := WorkItem{
		ID: "item-1", Key: "TH-1", ObjectiveID: "objective-1", Title: "Work", Kind: "research",
		CommitmentState: ItemAccepted, ExecutionStatus: StatusBacklog, Priority: PriorityMedium,
		EstimatedScope: ScopeSmall, ExecutionPolicy: PolicyAutonomousWithReport, RequiredActorKind: ActorAgent,
		AttentionState: AttentionNone,
	}
	for name, probe := range map[string]struct {
		mutate func(*WorkItem)
		valid  bool
	}{
		"a complete plan step origin":   {func(i *WorkItem) { i.Origin, i.PlanRunID, i.OriginPlanStepID = OriginPlanStep, "run-1", "step-1" }, true},
		"a plan step without a run":     {func(i *WorkItem) { i.Origin, i.OriginPlanStepID = OriginPlanStep, "step-1" }, false},
		"a plan step without a step":    {func(i *WorkItem) { i.Origin, i.PlanRunID = OriginPlanStep, "run-1" }, false},
		"unplanned work with a run":     {func(i *WorkItem) { i.Origin, i.PlanRunID, i.OriginPlanStepID = OriginUnplanned, "run-1", "step-1" }, false},
		"unplanned work without a run":  {func(i *WorkItem) { i.Origin = OriginUnplanned }, true},
		"legacy work without a run":     {func(i *WorkItem) { i.Origin = OriginLegacy }, true},
		"an unknown origin":             {func(i *WorkItem) { i.Origin = "somewhere" }, false},
		"no origin, defaulting to none": {func(i *WorkItem) {}, true},
	} {
		t.Run(name, func(t *testing.T) {
			item := base
			probe.mutate(&item)
			if _, err := NewWorkItem(item, runFixtureTime); probe.valid != (err == nil) {
				t.Fatalf("NewWorkItem(%s) error = %v, want valid = %v", name, err, probe.valid)
			}
		})
	}
}

// TestBindingFingerprintsAreUnambiguous is a regression: the fields were once
// joined with a separator, so two bindings whose values contained that
// separator could serialize identically and a changed run would look replayed.
func TestBindingFingerprintsAreUnambiguous(t *testing.T) {
	left := RunInputBinding{Name: "window", Kind: BindingExternal, Locator: "a\x00b", SourceVersion: "c"}
	right := RunInputBinding{Name: "window", Kind: BindingExternal, Locator: "a", SourceVersion: "b\x00c"}
	if left.Fingerprint() == right.Fingerprint() {
		t.Fatal("two different bindings produced the same fingerprint")
	}
	shifted := RunInputBinding{Name: "win", Kind: BindingValue, Value: "dowvalue"}
	unshifted := RunInputBinding{Name: "window", Kind: BindingValue, Value: "value"}
	if shifted.Fingerprint() == unshifted.Fingerprint() {
		t.Fatal("a field boundary shift produced the same fingerprint")
	}
}
