package dashboard

import (
	"testing"
	"time"

	"github.com/dennisschroeder/throughline/internal/domain/work"
)

// TestChooseObjectiveNamesAnObjectiveEvenWhenNoneCouldBeCounted covers the case
// the surrounding loop cannot reach from a test: every candidate is dropped when
// a store read for it fails, and the choice must still name a real objective.
// Returning the zero objective hands the caller an empty id with no error, and
// the handler's 404 branch only fires on an error — so the empty id travels on
// and fails later as an internal error about an objective nobody asked for.
func TestChooseObjectiveNamesAnObjectiveEvenWhenNoneCouldBeCounted(t *testing.T) {
	objectives := []work.Objective{{ID: "id-first", Key: "OBJ-FIRST"}, {ID: "id-second", Key: "OBJ-SECOND"}}
	if got := chooseObjective(objectives, nil); got.ID != "id-first" {
		t.Fatalf("with no countable candidate the choice was %q, want the first objective", got.ID)
	}
	if got := chooseObjective(nil, nil); got.ID != "" {
		t.Fatalf("with no objectives at all the choice was %q, want the zero objective", got.ID)
	}
}

func TestChooseObjectivePrefersGatesThenWorkThenRecency(t *testing.T) {
	early := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	late := time.Date(2026, 9, 9, 0, 0, 0, 0, time.UTC)
	objective := func(id string, updated time.Time) work.Objective {
		return work.Objective{ID: id, Key: "OBJ-" + id, UpdatedAt: updated}
	}
	for _, testCase := range []struct {
		name       string
		candidates []objectiveCandidate
		want       string
	}{
		{
			name: "an objective that needs you beats one that merely holds work",
			candidates: []objectiveCandidate{
				{objective: objective("busy", late), gates: 0, items: 40},
				{objective: objective("gated", early), gates: 1, items: 0},
			},
			want: "gated",
		},
		{
			name: "with nothing gated, the one holding work beats the one just created",
			candidates: []objectiveCandidate{
				{objective: objective("working", early), gates: 0, items: 3},
				{objective: objective("brand-new", late), gates: 0, items: 0},
			},
			want: "working",
		},
		{
			name: "with gates and work equal, the most recently touched wins",
			candidates: []objectiveCandidate{
				{objective: objective("stale", early), gates: 2, items: 5},
				{objective: objective("fresh", late), gates: 2, items: 5},
			},
			want: "fresh",
		},
		{
			name:       "a single candidate is the choice",
			candidates: []objectiveCandidate{{objective: objective("only", early), gates: 0, items: 0}},
			want:       "only",
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			if got := chooseObjective(nil, testCase.candidates); got.ID != testCase.want {
				t.Fatalf("chose %q, want %q", got.ID, testCase.want)
			}
		})
	}
}
