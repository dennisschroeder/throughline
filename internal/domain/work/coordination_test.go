package work

import (
	"reflect"
	"testing"
	"time"
)

func TestCoordinationActorAndCapability(t *testing.T) {
	now := time.Date(2026, 8, 22, 10, 0, 0, 0, time.FixedZone("test", 2*60*60))
	actor, err := NewActor(Actor{ID: " agent:writer ", Kind: ActorTypeAgent, DisplayName: " Writer "}, now)
	if err != nil {
		t.Fatal(err)
	}
	if actor.ID != "agent:writer" || actor.DisplayName != "Writer" || actor.CreatedAt.Location() != time.UTC {
		t.Fatalf("unexpected actor: %#v", actor)
	}
	if _, err := NewActor(Actor{ID: "service:sync", Kind: "bot"}, now); err == nil {
		t.Fatal("expected invalid actor kind to be rejected")
	}

	capability, err := NewCapability(" Web-Research ", " Finds sources. ")
	if err != nil {
		t.Fatal(err)
	}
	if capability.Slug != "web_research" || capability.Description != "Finds sources." {
		t.Fatalf("unexpected capability: %#v", capability)
	}
	for _, slug := range []string{"", "1research", "web__research", "web.research", "web_research_"} {
		if _, err := NormalizeCapabilitySlug(slug); err == nil {
			t.Fatalf("expected invalid slug %q to be rejected", slug)
		}
	}
}

func TestClaimLifecycleValidatesOwnerExpiryAndBounds(t *testing.T) {
	now := time.Date(2026, 8, 22, 10, 0, 0, 0, time.UTC)
	claim, err := NewClaim(" claim-1 ", " item-1 ", " agent:writer ", time.Hour, now)
	if err != nil {
		t.Fatal(err)
	}
	if claim.ID != "claim-1" || !claim.ExpiresAt.Equal(now.Add(time.Hour)) {
		t.Fatalf("unexpected claim: %#v", claim)
	}
	renewed, err := RenewClaim(claim, "agent:writer", 2*time.Hour, now.Add(30*time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if !renewed.ExpiresAt.Equal(now.Add(3 * time.Hour)) {
		t.Fatalf("unexpected renewed claim: %#v", renewed)
	}
	released, err := ReleaseClaim(renewed, "agent:writer", "Handed off to reviewer.", now.Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if released.ReleasedAt.IsZero() || released.ReleaseReason != "Handed off to reviewer." {
		t.Fatalf("unexpected released claim: %#v", released)
	}
	for _, test := range []struct {
		name string
		fn   func() error
	}{
		{"short lease", func() error { _, err := NewClaim("claim-2", "item-1", "agent:writer", time.Minute-1, now); return err }},
		{"long lease", func() error {
			_, err := NewClaim("claim-2", "item-1", "agent:writer", 8*time.Hour+time.Second, now)
			return err
		}},
		{"wrong owner", func() error { _, err := RenewClaim(claim, "agent:other", time.Hour, now); return err }},
		{"released", func() error { _, err := RenewClaim(released, "agent:writer", time.Hour, now); return err }},
		{"expired", func() error { _, err := ReleaseClaim(claim, "agent:writer", "Late.", now.Add(time.Hour)); return err }},
	} {
		t.Run(test.name, func(t *testing.T) {
			if err := test.fn(); err == nil {
				t.Fatal("expected claim operation to be rejected")
			}
		})
	}
}

func TestProgressEntryIsConciseAndNormalized(t *testing.T) {
	now := time.Date(2026, 8, 22, 10, 0, 0, 0, time.UTC)
	entry, err := NewProgressEntry(ProgressEntry{
		ID: " progress-1 ", WorkItemID: " item-1 ", ActorID: " agent:writer ", Summary: " Drafted the source outline. ",
		Completed: []string{" Selected sources "}, Remaining: []string{" Write the synthesis "}, Discovered: []string{" One source is stale "}, Blocker: " Awaiting access ",
	}, now)
	if err != nil {
		t.Fatal(err)
	}
	if entry.ID != "progress-1" || entry.Completed[0] != "Selected sources" || entry.CreatedAt.Location() != time.UTC {
		t.Fatalf("unexpected progress entry: %#v", entry)
	}
	if _, err := NewProgressEntry(ProgressEntry{ID: "progress-2", WorkItemID: "item-1", ActorID: "agent:writer", Summary: "ok", Remaining: []string{" "}}, now); err == nil {
		t.Fatal("expected blank progress point to be rejected")
	}
}

func TestGateEvaluatorsReturnStableOrderedFailures(t *testing.T) {
	transitionFailures := EvaluateTransitionGate(TransitionGateFacts{
		ObjectivePhase: ObjectivePlanning, CurrentStatus: StatusReady, TargetStatus: StatusDone,
	})
	if got, want := transitionCodes(transitionFailures), []TransitionRequirementCode{
		TransitionRequirementLifecycle,
		TransitionRequirementObjectiveExecution,
		TransitionRequirementPlanApproved,
		TransitionRequirementItemAccepted,
		TransitionRequirementAcceptanceCriteria,
		TransitionRequirementHardDependencies,
		TransitionRequirementExpectedOutputs,
		TransitionRequirementOutputRequirements,
		TransitionRequirementExternalActions,
		TransitionRequirementReview,
	}; !reflect.DeepEqual(got, want) {
		t.Fatalf("unexpected transition failures: got %v, want %v", got, want)
	}
	if failures := EvaluateTransitionGate(TransitionGateFacts{
		ObjectivePhase: ObjectiveExecution, PlanApproved: true, ItemCommitment: ItemAccepted,
		CurrentStatus: StatusReview, TargetStatus: StatusDone, AcceptanceCriteriaSatisfied: true,
		HardDependenciesSatisfied: true, ExpectedOutputsSatisfied: true, OutputRequirementsSatisfied: true,
		ExternalActionsSatisfied: true, ReviewRequirementsSatisfied: true,
	}); len(failures) != 0 {
		t.Fatalf("expected transition to pass, got %#v", failures)
	}

	now := time.Date(2026, 8, 22, 10, 0, 0, 0, time.UTC)
	activeClaim, err := NewClaim("claim-1", "item-1", "agent:other", time.Hour, now)
	if err != nil {
		t.Fatal(err)
	}
	claimFailures := EvaluateClaimGate(ClaimGateFacts{
		ObjectivePhase: ObjectivePlanning, ExecutionStatus: StatusBacklog, ExecutionPolicy: PolicyApprovalRequired,
		RequiredActorKind: ActorHuman, Actor: Actor{Kind: ActorTypeAgent}, HasOpenBlocker: true,
		ActiveClaim: &activeClaim, Now: now,
	})
	if got, want := claimCodes(claimFailures), []ClaimRequirementCode{
		ClaimRequirementObjectiveExecution,
		ClaimRequirementPlanApproved,
		ClaimRequirementItemAccepted,
		ClaimRequirementItemReady,
		ClaimRequirementHardDependencies,
		ClaimRequirementNoBlockers,
		ClaimRequirementOutputRequirements,
		ClaimRequirementActorKind,
		ClaimRequirementCapabilities,
		ClaimRequirementApproval,
		ClaimRequirementClaimAvailable,
	}; !reflect.DeepEqual(got, want) {
		t.Fatalf("unexpected claim failures: got %v, want %v", got, want)
	}
	if failures := EvaluateClaimGate(ClaimGateFacts{
		ObjectivePhase: ObjectiveExecution, PlanApproved: true, ItemCommitment: ItemAccepted, ExecutionStatus: StatusReady,
		ExecutionPolicy: PolicyAutonomousWithReport, RequiredActorKind: ActorAgent, Actor: Actor{Kind: ActorTypeAgent},
		HardDependenciesSatisfied: true, OutputRequirementsSatisfied: true, CapabilitiesSatisfied: true, Now: now,
	}); len(failures) != 0 {
		t.Fatalf("expected claim to pass, got %#v", failures)
	}
}

func transitionCodes(requirements []TransitionRequirement) []TransitionRequirementCode {
	codes := make([]TransitionRequirementCode, len(requirements))
	for index, requirement := range requirements {
		codes[index] = requirement.Code
	}
	return codes
}

func claimCodes(requirements []ClaimRequirement) []ClaimRequirementCode {
	codes := make([]ClaimRequirementCode, len(requirements))
	for index, requirement := range requirements {
		codes[index] = requirement.Code
	}
	return codes
}
