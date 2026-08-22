package work

import (
	"testing"
	"time"
)

func TestWorkItemKindIsDomainNeutralAndExtensible(t *testing.T) {
	item, err := NewWorkItem(WorkItem{
		ID:                "018f0000-0000-7000-8000-000000000001",
		Key:               "WG-1",
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
