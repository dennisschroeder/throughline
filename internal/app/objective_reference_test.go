package app

import (
	"errors"
	"testing"

	"github.com/dennisschroeder/throughline/internal/domain/work"
	"github.com/dennisschroeder/throughline/internal/ports"
)

// TestResolveObjectiveInMatchesTheDocumentedRules pins the one statement of what
// an objective_id may be. Every tool that takes the field runs through here, so
// each rule below is a rule about twelve tools at once, and each was previously
// only stated in a comment.
func TestResolveObjectiveInMatchesTheDocumentedRules(t *testing.T) {
	objectives := []work.Objective{
		// A key spelled like another objective's identifier, listed first so that
		// slice order favours the key. Nothing in the domain forbids the
		// collision: a key is validated only as non-empty, and ListObjectives
		// orders by key, so the colliding objective really can come first.
		// Without this ordering the precedence rule cannot be told apart from
		// "whichever field matches first wins".
		{ID: "id-gamma", Key: "id-alpha"},
		{ID: "id-alpha", Key: "OBJ-ALPHA"},
		{ID: "id-beta", Key: "OBJ-BETA"},
	}
	for _, testCase := range []struct {
		name      string
		reference string
		want      string
		wantErr   bool
	}{
		{"identifier", "id-beta", "id-beta", false},
		{"key", "OBJ-BETA", "id-beta", false},
		{"surrounding whitespace is trimmed", "  OBJ-BETA  ", "id-beta", false},
		{"an empty reference is no filter, not an error", "", "", false},
		{"whitespace alone is no filter either", "   ", "", false},
		{"an unknown reference is not found", "OBJ-NOPE", "", true},
		{"keys are matched exactly, not case-insensitively", "obj-beta", "", true},
		{"an identifier wins over a key spelled the same way", "id-alpha", "id-alpha", false},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			got, err := ResolveObjectiveIn(objectives, testCase.reference)
			if testCase.wantErr {
				if !errors.Is(err, ports.ErrNotFound) {
					t.Fatalf("error = %v, want not found", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got.ID != testCase.want {
				t.Fatalf("resolved %q to %q, want %q", testCase.reference, got.ID, testCase.want)
			}
		})
	}
}

// TestResolveObjectiveRequiresAReference separates the two contracts: an absent
// objective_id means "the whole workspace" to a filter and "you addressed
// nothing" to a caller that needs one objective.
func TestResolveObjectiveRequiresAReference(t *testing.T) {
	service := &Service{}
	for _, reference := range []string{"", "   "} {
		if _, err := service.ResolveObjective(t.Context(), reference); !errors.Is(err, ports.ErrNotFound) {
			t.Fatalf("ResolveObjective(%q) error = %v, want not found", reference, err)
		}
	}
}
