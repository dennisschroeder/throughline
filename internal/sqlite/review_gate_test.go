package sqlite

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/dennisschroeder/throughline/internal/app"
	"github.com/dennisschroeder/throughline/internal/domain/output"
	"github.com/dennisschroeder/throughline/internal/domain/work"
)

// TestDeclaredReviewRequirementsGateDone is REP-09's two criteria end to end:
// done waits for every declared review; a missing, wrong, failed or stale
// record does not satisfy it; and the item reports each requirement's state.
func TestDeclaredReviewRequirementsGateDone(t *testing.T) {
	ctx := context.Background()
	database, err := Open(ctx, filepath.Join(t.TempDir(), "review-gate.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	if err := database.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	seedExecutableItems(t, ctx, database, "item-reviewed", "item-plain")
	service := app.NewService(database.Store(), &testIDs{}, testClock{})
	step := 0
	key := func(name string) string {
		step++
		return fmt.Sprintf("%s-%d", name, step)
	}
	version := func(id string) int {
		t.Helper()
		item, err := service.GetWorkItem(ctx, id)
		if err != nil {
			t.Fatal(err)
		}
		return item.WorkItem.Version
	}
	transition := func(id string, target work.ExecutionStatus) error {
		t.Helper()
		_, err := service.TransitionWorkItem(ctx, app.TransitionWorkItemCommand{WorkItemID: id, TargetStatus: target, ActorID: "human:owner", Reason: "Test.", ExpectedVersion: version(id), IdempotencyKey: key("transition")})
		return err
	}
	blockedOnReview := func(id string) bool {
		t.Helper()
		err := transition(id, work.StatusDone)
		var gate app.TransitionGateError
		if err == nil {
			t.Fatalf("%s reached done", id)
		}
		if !errors.As(err, &gate) {
			t.Fatalf("done of %s failed for another reason: %v", id, err)
		}
		for _, requirement := range gate.Requirements {
			if requirement.Code != work.TransitionRequirementReview {
				t.Fatalf("done of %s blocked by %s as well: %#v", id, requirement.Code, gate.Requirements)
			}
		}
		return len(gate.Requirements) == 1
	}
	review := func(ref string, kind output.ValidatorKind, verdict output.ValidationVerdict, degraded bool) output.ValidationRecord {
		t.Helper()
		record, err := app.UnwrapMutation(service.RecordWorkItemValidation(ctx, app.RecordWorkItemValidationCommand{
			WorkItemID: "item-reviewed", CriterionRef: ref, ValidatorKind: kind, Verdict: verdict, VerifierActorID: "human:owner",
			Details: json.RawMessage(`{"rationale":"Checked."}`), Degraded: degraded, IdempotencyKey: key("review"),
		}))
		if err != nil {
			t.Fatal(err)
		}
		return record
	}
	states := func() string {
		t.Helper()
		item, err := service.GetWorkItem(ctx, "item-reviewed")
		if err != nil {
			t.Fatal(err)
		}
		var parts []string
		for _, evidence := range item.ReviewEvidence {
			parts = append(parts, evidence.Requirement.CriterionRef+"="+string(evidence.State))
		}
		return strings.Join(parts, ",")
	}

	requirements := []work.ReviewRequirement{{CriterionRef: "code-review", ValidatorKind: "human_review"}, {CriterionRef: "suite", ValidatorKind: "probe"}}
	if _, err := service.PatchWorkItem(ctx, app.PatchWorkItemCommand{WorkItemID: "item-reviewed", ActorID: "human:owner", ExpectedVersion: version("item-reviewed"), IdempotencyKey: key("declare"), ReviewRequirements: &requirements}); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"item-reviewed", "item-plain"} {
		if _, err := service.ClaimWorkItem(ctx, app.ClaimWorkItemCommand{WorkItemID: id, ActorID: "human:owner", ExpectedVersion: version(id), IdempotencyKey: key("claim"), LeaseDuration: time.Hour, TransitionToInProgress: true}); err != nil {
			t.Fatal(err)
		}
		if err := transition(id, work.StatusReview); err != nil {
			t.Fatal(err)
		}
	}
	if err := transition("item-plain", work.StatusDone); err != nil {
		t.Fatalf("an item that declares no review requirement could not reach done: %v", err)
	}

	if !blockedOnReview("item-reviewed") || states() != "code-review=missing,suite=missing" {
		t.Fatalf("undeclared evidence: states %s", states())
	}
	review("design-review", output.ValidatorHumanReview, output.VerdictPassed, false)
	review("code-review", output.ValidatorEvaluation, output.VerdictPassed, false)
	review("suite", output.ValidatorProbe, output.VerdictPassed, false)
	if !blockedOnReview("item-reviewed") || states() != "code-review=missing,suite=satisfied" {
		t.Fatalf("wrong criterion or kind satisfied a requirement: states %s", states())
	}
	review("code-review", output.ValidatorHumanReview, output.VerdictPassed, true)
	review("code-review", output.ValidatorHumanReview, output.VerdictFailed, false)
	if !blockedOnReview("item-reviewed") || states() != "code-review=failed,suite=satisfied" {
		t.Fatalf("a later failed review was masked: states %s", states())
	}
	degraded := review("code-review", output.ValidatorHumanReview, output.VerdictPassed, true)
	if states() != "code-review=satisfied,suite=satisfied" || !degraded.Degraded {
		t.Fatalf("a degraded pass after the failure: states %s, record %#v", states(), degraded)
	}

	if _, err := service.AppendProgress(ctx, app.AppendProgressCommand{WorkItemID: "item-reviewed", ActorID: "human:owner", ExpectedVersion: version("item-reviewed"), IdempotencyKey: key("progress"), Summary: "Fixed a finding."}); err != nil {
		t.Fatal(err)
	}
	if !blockedOnReview("item-reviewed") || states() != "code-review=stale,suite=stale" {
		t.Fatalf("progress after the reviews did not stale them: states %s", states())
	}
	review("code-review", output.ValidatorHumanReview, output.VerdictPassed, false)
	review("suite", output.ValidatorProbe, output.VerdictPassed, false)
	if err := transition("item-reviewed", work.StatusInProgress); err != nil {
		t.Fatal(err)
	}
	if err := transition("item-reviewed", work.StatusReview); err != nil {
		t.Fatal(err)
	}
	if !blockedOnReview("item-reviewed") || states() != "code-review=stale,suite=stale" {
		t.Fatalf("returning to in_progress after the reviews did not stale them: states %s", states())
	}

	review("code-review", output.ValidatorHumanReview, output.VerdictPassed, false)
	review("suite", output.ValidatorProbe, output.VerdictPassed, false)
	added, err := app.UnwrapMutation(service.PatchWorkItem(ctx, app.PatchWorkItemCommand{WorkItemID: "item-reviewed", ActorID: "human:owner", ExpectedVersion: version("item-reviewed"), IdempotencyKey: key("criterion"),
		AcceptanceCriteriaToAdd: []app.PatchAcceptanceCriterionAddition{{Text: "The suite passes.", Required: true, Ordinal: 1}}}))
	if err != nil {
		t.Fatal(err)
	}
	if states() != "code-review=stale,suite=stale" {
		t.Fatalf("adding a criterion after the reviews did not stale them: states %s", states())
	}
	review("code-review", output.ValidatorHumanReview, output.VerdictPassed, false)
	review("suite", output.ValidatorProbe, output.VerdictPassed, false)
	detail, err := service.GetWorkItem(ctx, "item-reviewed")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.PatchWorkItem(ctx, app.PatchWorkItemCommand{WorkItemID: "item-reviewed", ActorID: "human:owner", ExpectedVersion: added.Version, IdempotencyKey: key("resolve"),
		AcceptanceCriterionResolutions: []app.PatchAcceptanceCriterionResolution{{CriterionID: detail.AcceptanceCriteria[0].ID, Status: work.AcceptanceSatisfied, Rationale: "Green."}}}); err != nil {
		t.Fatal(err)
	}
	if _, err := service.RenewClaim(ctx, app.RenewClaimCommand{WorkItemID: "item-reviewed", ClaimID: detail.Claims[0].ID, ActorID: "human:owner", ExpectedVersion: version("item-reviewed"), IdempotencyKey: key("renew"), Extension: time.Hour}); err != nil {
		t.Fatal(err)
	}
	if states() != "code-review=satisfied,suite=satisfied" {
		t.Fatalf("resolving a criterion or renewing the claim staled the reviews: states %s", states())
	}
	if err := transition("item-reviewed", work.StatusDone); err != nil {
		t.Fatalf("done with every declared review satisfied: %v", err)
	}
}

// TestWorkItemReviewEvidenceMustBelongToTheItem refuses a review that points
// at another item's artifact as its evidence.
func TestWorkItemReviewEvidenceMustBelongToTheItem(t *testing.T) {
	ctx := context.Background()
	database, err := Open(ctx, filepath.Join(t.TempDir(), "review-evidence.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	if err := database.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	seedExecutableItems(t, ctx, database, "item-a", "item-b")
	service := app.NewService(database.Store(), &testIDs{}, testClock{})
	item, err := service.GetWorkItem(ctx, "item-b")
	if err != nil {
		t.Fatal(err)
	}
	attached, err := app.UnwrapMutation(service.AttachArtifact(ctx, app.AttachArtifactCommand{WorkItemID: "item-b", ActorID: "human:owner", ExpectedVersion: item.WorkItem.Version, IdempotencyKey: "attach", Kind: "report", URI: "workspace:reports/b.md"}))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.RecordWorkItemValidation(ctx, app.RecordWorkItemValidationCommand{
		WorkItemID: "item-a", CriterionRef: "code-review", ValidatorKind: output.ValidatorProbe, Verdict: output.VerdictPassed,
		VerifierActorID: "human:owner", EvidenceArtifactID: attached.Artifact.ID, IdempotencyKey: "foreign-evidence",
	}); err == nil {
		t.Fatal("a review of item-a accepted item-b's artifact as its evidence")
	}
}

// TestClaimingIntoInProgressStalesAnEarlierReview covers the second path into
// in_progress: claim_item's own transition, which used to write no payload
// saying where the item moved.
func TestClaimingIntoInProgressStalesAnEarlierReview(t *testing.T) {
	ctx := context.Background()
	database, err := Open(ctx, filepath.Join(t.TempDir(), "review-claim.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	if err := database.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	seedExecutableItems(t, ctx, database, "item-a")
	service := app.NewService(database.Store(), &testIDs{}, testClock{})
	requirements := []work.ReviewRequirement{{CriterionRef: "design", ValidatorKind: "human_review"}}
	declared, err := app.UnwrapMutation(service.PatchWorkItem(ctx, app.PatchWorkItemCommand{WorkItemID: "item-a", ActorID: "human:owner", ExpectedVersion: 1, IdempotencyKey: "declare", ReviewRequirements: &requirements}))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.RecordWorkItemValidation(ctx, app.RecordWorkItemValidationCommand{
		WorkItemID: "item-a", CriterionRef: "design", ValidatorKind: output.ValidatorHumanReview, Verdict: output.VerdictPassed,
		VerifierActorID: "human:owner", Details: json.RawMessage(`{"rationale":"Design read before work started."}`), IdempotencyKey: "early-review",
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := service.ClaimWorkItem(ctx, app.ClaimWorkItemCommand{WorkItemID: "item-a", ActorID: "human:owner", ExpectedVersion: declared.Version, IdempotencyKey: "claim", LeaseDuration: time.Hour, TransitionToInProgress: true}); err != nil {
		t.Fatal(err)
	}
	item, err := service.GetWorkItem(ctx, "item-a")
	if err != nil {
		t.Fatal(err)
	}
	if len(item.ReviewEvidence) != 1 || item.ReviewEvidence[0].State != work.ReviewEvidenceStale {
		t.Fatalf("review recorded before the claim moved the item into in_progress = %#v, want stale", item.ReviewEvidence)
	}
}

// TestMigration0016KeepsOutputValidationsAndTheirProtection rebuilds
// output_validations over existing records: every record keeps its fields and
// insertion order, the append-only triggers the rebuild drops come back, and
// the table now demands exactly one subject.
func TestMigration0016KeepsOutputValidationsAndTheirProtection(t *testing.T) {
	ctx := context.Background()
	migrations, err := loadMigrations()
	if err != nil {
		t.Fatal(err)
	}
	beforeReviews := -1
	for index, migration := range migrations {
		if migration.version == 16 {
			beforeReviews = index
		}
	}
	if beforeReviews < 0 {
		t.Fatal("the work item review migration is missing")
	}
	database, err := Open(ctx, filepath.Join(t.TempDir(), "validations-upgrade.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	if err := database.ensureMigrationTable(ctx); err != nil {
		t.Fatal(err)
	}
	for _, migration := range migrations[:beforeReviews] {
		if err := database.applyMigration(ctx, migration); err != nil {
			t.Fatal(err)
		}
	}
	seedExecutableItems(t, ctx, database, "item-a")
	triggerSQL := func(name string) string {
		t.Helper()
		var definition string
		if err := database.db.QueryRowContext(ctx, "SELECT sql FROM main.sqlite_master WHERE type = 'trigger' AND name = ?", name).Scan(&definition); err != nil {
			t.Fatalf("trigger %s: %v", name, err)
		}
		return definition
	}
	updateTrigger, deleteTrigger := triggerSQL("output_validation_is_append_only_update"), triggerSQL("output_validation_is_append_only_delete")
	for _, statement := range []string{
		`INSERT INTO expected_outputs (id, work_item_id, name, output_profile_id, ordinal) VALUES ('expected-a', 'item-a', 'Doc', '0198ce12-1800-7000-8000-000000000001', 1)`,
		`INSERT INTO output_revisions (id, expected_output_id, output_profile_id, revision, produced_by, produced_at) VALUES ('revision-a', 'expected-a', '0198ce12-1800-7000-8000-000000000001', 1, 'human:owner', '` + questionFixtureTime + `')`,
		// Inserted out of id order so a rebuild that sorted by id would show.
		`INSERT INTO output_validations (id, output_revision_id, criterion_ref, validator_kind, verdict, score, verifier_actor_id, details_json, created_at, version)
		 VALUES ('validation-z', 'revision-a', 'structure', 'structure', 'failed', 0.25, 'agent:validator', '{"note":"first"}', '` + questionFixtureTime + `', 2)`,
		`INSERT INTO output_validations (id, output_revision_id, criterion_ref, validator_kind, verdict, verifier_actor_id, created_at)
		 VALUES ('validation-a', 'revision-a', 'structure', 'structure', 'passed', 'agent:validator', '` + questionFixtureTime + `')`,
	} {
		if _, err := database.db.ExecContext(ctx, statement); err != nil {
			t.Fatalf("seed pre-upgrade row: %v\n%s", err, statement)
		}
	}

	if err := database.Migrate(ctx); err != nil {
		t.Fatalf("migrate populated output validations: %v", err)
	}

	rows, err := database.db.QueryContext(ctx, "SELECT id, output_revision_id, work_item_id IS NULL, verdict, COALESCE(score, -1), details_json, version, subject_sequence, degraded FROM output_validations ORDER BY rowid")
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for rows.Next() {
		var id, revision, verdict, details string
		var noItem bool
		var score float64
		var recordVersion, sequence, degraded int
		if err := rows.Scan(&id, &revision, &noItem, &verdict, &score, &details, &recordVersion, &sequence, &degraded); err != nil {
			t.Fatal(err)
		}
		got = append(got, fmt.Sprintf("%s|%s|%v|%s|%v|%s|%d|%d|%d", id, revision, noItem, verdict, score, details, recordVersion, sequence, degraded))
	}
	rows.Close()
	want := []string{
		`validation-z|revision-a|true|failed|0.25|{"note":"first"}|2|0|0`,
		`validation-a|revision-a|true|passed|-1|{}|1|0|0`,
	}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("validations after migrating =\n%s\nwant\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
	if triggerSQL("output_validation_is_append_only_update") != updateTrigger || triggerSQL("output_validation_is_append_only_delete") != deleteTrigger {
		t.Fatal("the append-only triggers did not come back unchanged after the rebuild")
	}
	if _, err := database.db.ExecContext(ctx, "UPDATE output_validations SET verdict = 'passed' WHERE id = 'validation-z'"); err == nil || !strings.Contains(err.Error(), "append-only") {
		t.Fatalf("update after the rebuild = %v, want the append-only trigger to reject it", err)
	}
	var schema string
	if err := database.db.QueryRowContext(ctx, "SELECT sql FROM main.sqlite_master WHERE type = 'table' AND name = 'output_validations'").Scan(&schema); err != nil {
		t.Fatal(err)
	}
	for _, fragment := range []string{"(output_revision_id IS NOT NULL) + (work_item_id IS NOT NULL) = 1", "CHECK (version > 0)", "CHECK (json_valid(details_json))", "work_item_id TEXT REFERENCES work_items(id) ON DELETE CASCADE"} {
		if !strings.Contains(schema, fragment) {
			t.Fatalf("output_validations schema lacks %q:\n%s", fragment, schema)
		}
	}
	var indexes int
	if err := database.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM main.sqlite_master WHERE type = 'index' AND name IN ('output_validations_by_revision', 'output_validations_by_work_item')").Scan(&indexes); err != nil {
		t.Fatal(err)
	}
	if indexes != 2 {
		t.Fatalf("validation indexes after migrating = %d, want both", indexes)
	}
	item, err := app.NewService(database.Store(), &testIDs{}, testClock{}).GetWorkItem(ctx, "item-a")
	if err != nil {
		t.Fatal(err)
	}
	if item.WorkItem.ReviewRequirements != nil || item.ReviewEvidence != nil || len(item.OutputRevisions) != 1 || len(item.OutputRevisions[0].Validations) != 2 {
		t.Fatalf("pre-existing item after migrating = requirements %#v, evidence %#v, revisions %#v", item.WorkItem.ReviewRequirements, item.ReviewEvidence, item.OutputRevisions)
	}
}

// TestEveryKindOfRecordedWorkStalesAReview runs each work step the staleness
// rule names, on an item whose declared review passed just before it, and
// requires the review to be stale afterwards. A review recorded straight after
// such a step is not stale: the step is what it reviewed.
func TestEveryKindOfRecordedWorkStalesAReview(t *testing.T) {
	steps := map[string]func(t *testing.T, ctx context.Context, service *app.Service, itemID string, version int){
		"attach_artifact": func(t *testing.T, ctx context.Context, service *app.Service, itemID string, version int) {
			if _, err := service.AttachArtifact(ctx, app.AttachArtifactCommand{WorkItemID: itemID, ActorID: "human:owner", ExpectedVersion: version, IdempotencyKey: "step", Kind: "report", URI: "workspace:report.md"}); err != nil {
				t.Fatal(err)
			}
		},
		"define_expected_output": func(t *testing.T, ctx context.Context, service *app.Service, itemID string, version int) {
			if _, err := service.DefineExpectedOutput(ctx, app.DefineExpectedOutputCommand{WorkItemID: itemID, ActorID: "human:owner", ExpectedVersion: version, IdempotencyKey: "step", Name: "Doc", ProfileName: "structured_document", ProfileVersion: 1, Required: true, Ordinal: 2}); err != nil {
				t.Fatal(err)
			}
		},
		"patch_item expected output": func(t *testing.T, ctx context.Context, service *app.Service, itemID string, version int) {
			if _, err := service.PatchWorkItem(ctx, app.PatchWorkItemCommand{WorkItemID: itemID, ActorID: "human:owner", ExpectedVersion: version, IdempotencyKey: "step",
				ExpectedOutputsToAdd: []app.ProposedExpectedOutput{{Name: "Doc", ProfileName: "structured_document", ProfileVersion: 1, Required: true, Ordinal: 2}}}); err != nil {
				t.Fatal(err)
			}
		},
		"add_output_requirement": func(t *testing.T, ctx context.Context, service *app.Service, itemID string, version int) {
			if _, err := service.AddOutputRequirement(ctx, app.AddOutputRequirementCommand{WorkItemID: itemID, ActorID: "human:owner", ExpectedVersion: version, IdempotencyKey: "step", RequiredProfileName: "structured_document", VersionConstraint: "=1", Required: false}); err != nil {
				t.Fatal(err)
			}
		},
		"create_output_revision": func(t *testing.T, ctx context.Context, service *app.Service, itemID string, version int) {
			item, err := service.GetWorkItem(ctx, itemID)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := service.CreateOutputRevision(ctx, app.CreateOutputRevisionCommand{ExpectedOutputID: item.ExpectedOutputs[0].ExpectedOutput.ID, ActorID: "human:owner", IdempotencyKey: "step",
				Artifacts: []app.OutputArtifactInput{{Kind: "document", URI: "workspace:existing.md", Role: "primary"}}}); err != nil {
				t.Fatal(err)
			}
		},
		"superseding a criterion": func(t *testing.T, ctx context.Context, service *app.Service, itemID string, version int) {
			item, err := service.GetWorkItem(ctx, itemID)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := service.PatchWorkItem(ctx, app.PatchWorkItemCommand{WorkItemID: itemID, ActorID: "human:owner", ExpectedVersion: version, IdempotencyKey: "step",
				AcceptanceCriteriaToAdd: []app.PatchAcceptanceCriterionAddition{{Text: "Corrected.", Required: true, Ordinal: 1, SupersedesID: item.AcceptanceCriteria[0].ID, SupersessionReason: "Wrong condition."}}}); err != nil {
				t.Fatal(err)
			}
		},
	}
	for name, step := range steps {
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()
			database, err := Open(ctx, filepath.Join(t.TempDir(), "stale.db"))
			if err != nil {
				t.Fatal(err)
			}
			defer database.Close()
			if err := database.Migrate(ctx); err != nil {
				t.Fatal(err)
			}
			seedExecutableItems(t, ctx, database, "item-a")
			service := app.NewService(database.Store(), &testIDs{}, testClock{})
			requirements := []work.ReviewRequirement{{CriterionRef: "design", ValidatorKind: "probe"}}
			declared, err := app.UnwrapMutation(service.PatchWorkItem(ctx, app.PatchWorkItemCommand{WorkItemID: "item-a", ActorID: "human:owner", ExpectedVersion: 1, IdempotencyKey: "declare", ReviewRequirements: &requirements,
				AcceptanceCriteriaToAdd: []app.PatchAcceptanceCriterionAddition{{Text: "Original.", Required: true, Ordinal: 1}},
				ExpectedOutputsToAdd:    []app.ProposedExpectedOutput{{Name: "Existing", ProfileName: "structured_document", ProfileVersion: 1, Required: false, Ordinal: 1}}}))
			if err != nil {
				t.Fatal(err)
			}
			claimed, err := app.UnwrapMutation(service.ClaimWorkItem(ctx, app.ClaimWorkItemCommand{WorkItemID: "item-a", ActorID: "human:owner", ExpectedVersion: declared.Version, IdempotencyKey: "claim", LeaseDuration: time.Hour, TransitionToInProgress: true}))
			if err != nil {
				t.Fatal(err)
			}
			state := func() work.ReviewEvidenceState {
				t.Helper()
				item, err := service.GetWorkItem(ctx, "item-a")
				if err != nil {
					t.Fatal(err)
				}
				return item.ReviewEvidence[0].State
			}
			record := func(key string) {
				t.Helper()
				if _, err := service.RecordWorkItemValidation(ctx, app.RecordWorkItemValidationCommand{WorkItemID: "item-a", CriterionRef: "design", ValidatorKind: output.ValidatorProbe, Verdict: output.VerdictPassed, VerifierActorID: "human:owner", IdempotencyKey: key}); err != nil {
					t.Fatal(err)
				}
			}
			record("before")
			if got := state(); got != work.ReviewEvidenceSatisfied {
				t.Fatalf("state before the step = %s", got)
			}
			step(t, ctx, service, "item-a", claimed.WorkItem.Version)
			if got := state(); got != work.ReviewEvidenceStale {
				t.Fatalf("state after %s = %s, want stale", name, got)
			}
			record("after")
			if got := state(); got != work.ReviewEvidenceSatisfied {
				t.Fatalf("a review recorded straight after %s = %s, want satisfied", name, got)
			}
		})
	}
}

// TestReadyWorkCarriesEveryObjectiveAndItemField pins the ready-work join to
// the same column lists as the plain selects. It listed its own columns and
// had stopped reading objective priority and appetite and item measure when
// those were added, so list_ready_items reported them as unset.
func TestReadyWorkCarriesEveryObjectiveAndItemField(t *testing.T) {
	ctx := context.Background()
	database, err := Open(ctx, filepath.Join(t.TempDir(), "ready-fields.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	if err := database.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	seedExecutableItems(t, ctx, database, "item-a")
	for _, statement := range []string{
		`UPDATE objectives SET priority = 'urgent', appetite_value = 3, appetite_unit = 'days', appetite_basis = 'estimated' WHERE id = 'objective-a'`,
		`UPDATE work_items SET measure_value = 5, measure_unit = 'points', measure_basis = 'measured',
		   review_requirements_json = '[{"criterion_ref":"design","validator_kind":"probe"}]' WHERE id = 'item-a'`,
	} {
		if _, err := database.db.ExecContext(ctx, statement); err != nil {
			t.Fatal(err)
		}
	}
	ready, err := app.NewService(database.Store(), &testIDs{}, testClock{}).ListReadyWork(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range ready {
		if entry.WorkItem.ID != "item-a" {
			continue
		}
		if entry.Objective.Priority != work.PriorityUrgent || entry.Objective.Appetite != (work.Measure{Value: 3, Unit: "days", Basis: work.MeasureEstimated}) {
			t.Fatalf("ready objective = %#v", entry.Objective)
		}
		if entry.WorkItem.Measure != (work.Measure{Value: 5, Unit: "points", Basis: work.MeasureMeasured}) || len(entry.WorkItem.ReviewRequirements) != 1 {
			t.Fatalf("ready item = %#v", entry.WorkItem)
		}
		return
	}
	t.Fatalf("item-a is not ready: %#v", ready)
}

// TestChangingReviewRequirementsRaisesAttentionWhenItHidesAnUnmetReview
// mirrors the rule for waiving or adding a required acceptance criterion:
// dropping a review that was not satisfied, or declaring one on work already
// done, flags the item for a person instead of passing silently.
func TestChangingReviewRequirementsRaisesAttentionWhenItHidesAnUnmetReview(t *testing.T) {
	ctx := context.Background()
	database, err := Open(ctx, filepath.Join(t.TempDir(), "review-attention.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	if err := database.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	seedExecutableItems(t, ctx, database, "item-failed", "item-passed", "item-done", "item-open")
	service := app.NewService(database.Store(), &testIDs{}, testClock{})
	patch := func(id, key string, requirements []work.ReviewRequirement) work.WorkItem {
		t.Helper()
		item, err := service.GetWorkItem(ctx, id)
		if err != nil {
			t.Fatal(err)
		}
		patched, err := app.UnwrapMutation(service.PatchWorkItem(ctx, app.PatchWorkItemCommand{WorkItemID: id, ActorID: "human:owner", ExpectedVersion: item.WorkItem.Version, IdempotencyKey: key, ReviewRequirements: &requirements}))
		if err != nil {
			t.Fatal(err)
		}
		return patched
	}
	review := func(id string, verdict output.ValidationVerdict) {
		t.Helper()
		if _, err := service.RecordWorkItemValidation(ctx, app.RecordWorkItemValidationCommand{WorkItemID: id, CriterionRef: "design", ValidatorKind: output.ValidatorProbe, Verdict: verdict, VerifierActorID: "human:owner", IdempotencyKey: "review-" + id}); err != nil {
			t.Fatal(err)
		}
	}
	declared := []work.ReviewRequirement{{CriterionRef: "design", ValidatorKind: "probe"}}

	patch("item-failed", "declare-failed", declared)
	review("item-failed", output.VerdictFailed)
	if cleared := patch("item-failed", "clear-failed", nil); cleared.AttentionState != work.AttentionNeedsHumanReview {
		t.Fatalf("dropping a failed review requirement left attention %s", cleared.AttentionState)
	}

	patch("item-passed", "declare-passed", declared)
	review("item-passed", output.VerdictPassed)
	if cleared := patch("item-passed", "clear-passed", nil); cleared.AttentionState != work.AttentionNone {
		t.Fatalf("dropping a satisfied review requirement raised attention %s", cleared.AttentionState)
	}

	if _, err := database.db.ExecContext(ctx, "UPDATE work_items SET execution_status = 'done' WHERE id = 'item-done'"); err != nil {
		t.Fatal(err)
	}
	if added := patch("item-done", "declare-done", declared); added.AttentionState != work.AttentionNeedsHumanReview {
		t.Fatalf("declaring a review on a done item left attention %s", added.AttentionState)
	}
	if added := patch("item-open", "declare-open", declared); added.AttentionState != work.AttentionNone {
		t.Fatalf("declaring a review on open work raised attention %s", added.AttentionState)
	}
}

// TestWorkOnAnotherItemDoesNotStaleAReview keeps staleness to the reviewed
// item: progress recorded elsewhere says nothing about this item's work.
func TestWorkOnAnotherItemDoesNotStaleAReview(t *testing.T) {
	ctx := context.Background()
	database, err := Open(ctx, filepath.Join(t.TempDir(), "review-isolation.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	if err := database.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	seedExecutableItems(t, ctx, database, "item-a", "item-b")
	service := app.NewService(database.Store(), &testIDs{}, testClock{})
	requirements := []work.ReviewRequirement{{CriterionRef: "design", ValidatorKind: "probe"}}
	if _, err := service.PatchWorkItem(ctx, app.PatchWorkItemCommand{WorkItemID: "item-a", ActorID: "human:owner", ExpectedVersion: 1, IdempotencyKey: "declare", ReviewRequirements: &requirements}); err != nil {
		t.Fatal(err)
	}
	if _, err := service.RecordWorkItemValidation(ctx, app.RecordWorkItemValidationCommand{WorkItemID: "item-a", CriterionRef: "design", ValidatorKind: output.ValidatorProbe, Verdict: output.VerdictPassed, VerifierActorID: "human:owner", IdempotencyKey: "review"}); err != nil {
		t.Fatal(err)
	}
	claimed, err := app.UnwrapMutation(service.ClaimWorkItem(ctx, app.ClaimWorkItemCommand{WorkItemID: "item-b", ActorID: "human:owner", ExpectedVersion: 1, IdempotencyKey: "claim-b", LeaseDuration: time.Hour, TransitionToInProgress: true}))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.AppendProgress(ctx, app.AppendProgressCommand{WorkItemID: "item-b", ActorID: "human:owner", ExpectedVersion: claimed.WorkItem.Version, IdempotencyKey: "progress-b", Summary: "Unrelated work."}); err != nil {
		t.Fatal(err)
	}
	item, err := service.GetWorkItem(ctx, "item-a")
	if err != nil {
		t.Fatal(err)
	}
	if len(item.ReviewEvidence) != 1 || item.ReviewEvidence[0].State != work.ReviewEvidenceSatisfied {
		t.Fatalf("item-a review after work on item-b = %#v, want satisfied", item.ReviewEvidence)
	}
}

// TestOutputRevisionValidationKeepsItsDegradedFlag covers the other subject:
// any validation may be marked degraded, not only a work-item review.
func TestOutputRevisionValidationKeepsItsDegradedFlag(t *testing.T) {
	ctx := context.Background()
	database, err := Open(ctx, filepath.Join(t.TempDir(), "output-degraded.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	if err := database.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	seedExecutableItems(t, ctx, database, "item-a")
	service := app.NewService(database.Store(), &testIDs{}, testClock{})
	if _, err := service.PatchWorkItem(ctx, app.PatchWorkItemCommand{WorkItemID: "item-a", ActorID: "human:owner", ExpectedVersion: 1, IdempotencyKey: "output",
		ExpectedOutputsToAdd: []app.ProposedExpectedOutput{{Name: "Doc", ProfileName: "structured_document", ProfileVersion: 1, Required: true, Ordinal: 1}}}); err != nil {
		t.Fatal(err)
	}
	item, err := service.GetWorkItem(ctx, "item-a")
	if err != nil {
		t.Fatal(err)
	}
	revision, err := app.UnwrapMutation(service.CreateOutputRevision(ctx, app.CreateOutputRevisionCommand{ExpectedOutputID: item.ExpectedOutputs[0].ExpectedOutput.ID, ActorID: "human:owner", IdempotencyKey: "revision",
		Artifacts: []app.OutputArtifactInput{{Kind: "document", URI: "workspace:doc.md", Role: "primary"}}}))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.RecordValidation(ctx, app.RecordValidationCommand{OutputRevisionID: revision.ID, CriterionRef: "structure", ValidatorKind: output.ValidatorStructure, Verdict: output.VerdictPassed, VerifierActorID: "agent:validator", Degraded: true, IdempotencyKey: "validate"}); err != nil {
		t.Fatal(err)
	}
	item, err = service.GetWorkItem(ctx, "item-a")
	if err != nil {
		t.Fatal(err)
	}
	if len(item.OutputRevisions) != 1 || len(item.OutputRevisions[0].Validations) != 1 || !item.OutputRevisions[0].Validations[0].Degraded {
		t.Fatalf("output revision validations = %#v, want one degraded record", item.OutputRevisions)
	}
}
