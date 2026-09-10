package work

import (
	"testing"
	"time"
)

func TestWorkItemKindIsDomainNeutralAndExtensible(t *testing.T) {
	item, err := NewWorkItem(WorkItem{
		ID:                "018f0000-0000-7000-8000-000000000001",
		Key:               "TH-1",
		ObjectiveID:       "objective",
		PlanID:            "plan",
		Title:             "Synthesize interview evidence",
		Kind:              "acme:qualitative_synthesis",
		CommitmentState:   ItemProposed,
		ExecutionStatus:   StatusBacklog,
		Priority:          PriorityMedium,
		EstimatedScope:    ScopeSmall,
		ExecutionPolicy:   PolicyAgentMayPropose,
		RequiredActorKind: ActorAny,
		AttentionState:    AttentionNone,
	}, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if item.Kind != "acme:qualitative_synthesis" {
		t.Fatalf("kind was changed: %q", item.Kind)
	}
}

// TestMeasureZeroValueMeansUnset is REP-06's Measure convention: the zero
// value is a valid, meaningful "no measure recorded" state, not an invalid one.
func TestMeasureZeroValueMeansUnset(t *testing.T) {
	if !validMeasure(Measure{}) {
		t.Fatal("the zero-value Measure should be valid: it means unset")
	}
}

// TestMeasureRequiresUnitAndBasisOnceAnyFieldIsSet covers the other half of
// the same convention: as soon as a caller states a value, the measure must
// be complete, or it silently discards half of what was recorded.
func TestMeasureRequiresUnitAndBasisOnceAnyFieldIsSet(t *testing.T) {
	for _, invalid := range []Measure{
		{Value: 200},                 // unit and basis missing
		{Value: 200, Unit: "tokens"}, // basis missing
		{Value: 200, Unit: "tokens", Basis: "guessed"},       // basis not a recognized value
		{Value: -1, Unit: "tokens", Basis: MeasureEstimated}, // negative value
	} {
		if validMeasure(invalid) {
			t.Fatalf("%+v accepted as a valid measure", invalid)
		}
	}
	if !validMeasure(Measure{Value: 200, Unit: "tokens", Basis: MeasureEstimated}) {
		t.Fatal("a complete measure with a recognized basis was rejected")
	}
}

// TestObjectiveRequiresAValidPriority is REP-06's first criterion at the
// domain constructor: Objective.Priority reuses WorkItem's exact vocabulary,
// not a parallel one that could silently drift from it.
func TestObjectiveRequiresAValidPriority(t *testing.T) {
	now := time.Now()
	if _, err := NewObjective("id", "OBJ-1", "Title", "", "Outcome", ObjectiveIdea, "", now); err == nil {
		t.Fatal("an objective with no priority was accepted")
	}
	if _, err := NewObjective("id", "OBJ-1", "Title", "", "Outcome", ObjectiveIdea, "urgent-ish", now); err == nil {
		t.Fatal("an objective with an unrecognized priority was accepted")
	}
	objective, err := NewObjective("id", "OBJ-1", "Title", "", "Outcome", ObjectiveIdea, PriorityUrgent, now)
	if err != nil {
		t.Fatal(err)
	}
	if objective.Priority != PriorityUrgent {
		t.Fatalf("priority = %q, want urgent", objective.Priority)
	}
}

// TestObjectiveRejectsAnInvalidAppetite ensures Appetite is validated the same
// way any other Measure is, not exempted because it lives on Objective.
func TestObjectiveRejectsAnInvalidAppetite(t *testing.T) {
	objective := Objective{ID: "id", Key: "OBJ-1", Title: "Title", DesiredOutcome: "Outcome", Phase: ObjectiveIdea, Priority: PriorityMedium}
	objective.Appetite = Measure{Value: 100, Unit: "days"}
	if err := objective.Validate(); err == nil {
		t.Fatal("an objective with an incomplete appetite was accepted")
	}
	objective.Appetite = Measure{Value: 100, Unit: "days", Basis: MeasureEstimated}
	if err := objective.Validate(); err != nil {
		t.Fatalf("a complete appetite was rejected: %v", err)
	}
}
