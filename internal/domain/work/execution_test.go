package work

import (
	"encoding/json"
	"testing"
	"time"
)

func TestAcceptanceCriterionLifecycle(t *testing.T) {
	now := time.Date(2026, 8, 21, 16, 0, 0, 0, time.FixedZone("test", 2*60*60))
	criterion, err := NewAcceptanceCriterion(AcceptanceCriterion{
		ID: " criterion-1 ", WorkItemID: " item-1 ", Ordinal: 1,
		Text: " Sources are cited ", Required: true, Status: AcceptanceSatisfied,
	})
	if err != nil {
		t.Fatal(err)
	}
	if criterion.ID != "criterion-1" || criterion.Text != "Sources are cited" || criterion.Status != AcceptancePending {
		t.Fatalf("unexpected criterion: %#v", criterion)
	}

	satisfied, err := ResolveAcceptanceCriterion(criterion, AcceptanceSatisfied, " agent:reviewer ", " Verified against the source list. ", now)
	if err != nil {
		t.Fatal(err)
	}
	if satisfied.Status != AcceptanceSatisfied || satisfied.ResolvedBy != "agent:reviewer" || satisfied.ResolutionRationale != "Verified against the source list." || satisfied.ResolvedAt.Location() != time.UTC {
		t.Fatalf("unexpected resolution: %#v", satisfied)
	}
	if _, err := ResolveAcceptanceCriterion(satisfied, AcceptanceWaived, "human:owner", "No longer required.", now); err == nil {
		t.Fatal("expected a resolved criterion to reject another resolution")
	}
}

func TestAcceptanceCriterionRejectsInvalidCreationAndResolution(t *testing.T) {
	valid := AcceptanceCriterion{ID: "criterion-1", WorkItemID: "item-1", Ordinal: 1, Text: "Reviewed", Required: true}
	for name, mutate := range map[string]func(*AcceptanceCriterion){
		"missing text":    func(criterion *AcceptanceCriterion) { criterion.Text = " " },
		"invalid ordinal": func(criterion *AcceptanceCriterion) { criterion.Ordinal = 0 },
	} {
		t.Run(name, func(t *testing.T) {
			criterion := valid
			mutate(&criterion)
			if _, err := NewAcceptanceCriterion(criterion); err == nil {
				t.Fatal("expected validation error")
			}
		})
	}
	criterion, err := NewAcceptanceCriterion(valid)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ResolveAcceptanceCriterion(criterion, AcceptanceSatisfied, "agent", " ", time.Now()); err == nil {
		t.Fatal("expected missing rationale to be rejected")
	}
	if _, err := ResolveAcceptanceCriterion(criterion, AcceptancePending, "agent", "not a resolution", time.Now()); err == nil {
		t.Fatal("expected pending target to be rejected")
	}
}

func TestNewDependencyValidatesAndNormalizes(t *testing.T) {
	now := time.Date(2026, 8, 21, 14, 0, 0, 0, time.UTC)
	dependency, err := NewDependency(Dependency{
		ID: " dep-1 ", WorkItemID: " item-2 ", DependsOnItemID: " item-1 ",
		Kind: DependencyHard, Note: " Research precedes synthesis. ", CreatedBy: " agent:planner ",
	}, now)
	if err != nil {
		t.Fatal(err)
	}
	if dependency.Note != "Research precedes synthesis." || dependency.CreatedBy != "agent:planner" || !dependency.CreatedAt.Equal(now) {
		t.Fatalf("unexpected dependency: %#v", dependency)
	}

	dependency.DependsOnItemID = dependency.WorkItemID
	if _, err := NewDependency(dependency, now); err == nil {
		t.Fatal("expected self-dependency to be rejected")
	}
	dependency.DependsOnItemID = "item-1"
	dependency.Kind = "blocks"
	if _, err := NewDependency(dependency, now); err == nil {
		t.Fatal("expected invalid dependency kind to be rejected")
	}
}

func TestNewActivityRequiresJSONObjectPayload(t *testing.T) {
	now := time.Date(2026, 8, 21, 14, 0, 0, 0, time.FixedZone("test", 2*60*60))
	activity, err := NewActivity(Activity{
		ID: " activity-1 ", EntityKind: " work_item ", EntityID: " item-1 ",
		ActorID: " agent:researcher ", EventType: " criterion.satisfied ", Summary: " Criterion satisfied. ",
		PayloadJSON: json.RawMessage(` { "criterion_id": "criterion-1" } `),
	}, now)
	if err != nil {
		t.Fatal(err)
	}
	if string(activity.PayloadJSON) != `{"criterion_id":"criterion-1"}` || activity.ActorID != "agent:researcher" || activity.CreatedAt.Location() != time.UTC {
		t.Fatalf("unexpected activity: %#v", activity)
	}

	emptyPayload, err := NewActivity(Activity{
		ID: "activity-2", EntityKind: "work_item", EntityID: "item-1",
		ActorID: "human:owner", EventType: "item.reviewed", Summary: "Item reviewed.",
	}, now)
	if err != nil {
		t.Fatal(err)
	}
	if string(emptyPayload.PayloadJSON) != "{}" {
		t.Fatalf("unexpected default payload: %s", emptyPayload.PayloadJSON)
	}

	for _, payload := range []json.RawMessage{json.RawMessage(`[]`), json.RawMessage(`null`), json.RawMessage(`{"broken"}`)} {
		activity.PayloadJSON = payload
		if _, err := NewActivity(activity, now); err == nil {
			t.Fatalf("expected payload %q to be rejected", payload)
		}
	}
}

func TestTransitionWorkItemLifecycle(t *testing.T) {
	now := time.Date(2026, 8, 21, 14, 0, 0, 0, time.UTC)
	item := WorkItem{ExecutionStatus: StatusBacklog, Version: 1}
	steps := []struct {
		target ExecutionStatus
		reason string
	}{
		{StatusReady, ""},
		{StatusInProgress, ""},
		{StatusReview, ""},
		{StatusInProgress, "Changes requested."},
		{StatusReview, ""},
		{StatusDone, ""},
	}
	for index, step := range steps {
		var err error
		item, err = TransitionWorkItem(item, step.target, " agent:worker ", step.reason, now.Add(time.Duration(index)*time.Minute))
		if err != nil {
			t.Fatalf("transition to %s: %v", step.target, err)
		}
	}
	if item.ExecutionStatus != StatusDone || item.Version != 7 {
		t.Fatalf("unexpected final item: %#v", item)
	}
	if _, err := TransitionWorkItem(item, StatusReview, "agent:worker", "", now); err == nil {
		t.Fatal("expected done item to reject further transitions")
	}
}

func TestTransitionWorkItemRequiresReasonsForCancellationAndBackwardEdges(t *testing.T) {
	now := time.Date(2026, 8, 21, 14, 0, 0, 0, time.UTC)
	for _, test := range []struct {
		name    string
		current ExecutionStatus
		target  ExecutionStatus
	}{
		{"cancel", StatusReady, StatusCancelled},
		{"release", StatusInProgress, StatusReady},
		{"changes requested", StatusReview, StatusInProgress},
	} {
		t.Run(test.name, func(t *testing.T) {
			item := WorkItem{ExecutionStatus: test.current, Version: 2}
			if _, err := TransitionWorkItem(item, test.target, "actor", " ", now); err == nil {
				t.Fatal("expected missing reason to be rejected")
			}
			transitioned, err := TransitionWorkItem(item, test.target, "actor", "Recorded reason.", now)
			if err != nil {
				t.Fatal(err)
			}
			if transitioned.ExecutionStatus != test.target || transitioned.Version != 3 {
				t.Fatalf("unexpected transitioned item: %#v", transitioned)
			}
		})
	}
}
