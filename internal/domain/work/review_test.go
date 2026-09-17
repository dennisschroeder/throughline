package work

import "testing"

func TestEvaluateReviewEvidence(t *testing.T) {
	requirement := ReviewRequirement{CriterionRef: "code-review", ValidatorKind: "human_review"}
	record := func(id, verdict string, stale bool) ReviewRecordFact {
		return ReviewRecordFact{ID: id, CriterionRef: "code-review", ValidatorKind: "human_review", Verdict: verdict, Stale: stale}
	}
	for _, testCase := range []struct {
		name    string
		records []ReviewRecordFact
		state   ReviewEvidenceState
		record  string
	}{
		{"no record", nil, ReviewEvidenceMissing, ""},
		{"another criterion", []ReviewRecordFact{{ID: "r", CriterionRef: "design-review", ValidatorKind: "human_review", Verdict: "passed"}}, ReviewEvidenceMissing, ""},
		{"another kind", []ReviewRecordFact{{ID: "r", CriterionRef: "code-review", ValidatorKind: "evaluation", Verdict: "passed"}}, ReviewEvidenceMissing, ""},
		{"passed", []ReviewRecordFact{record("r", "passed", false)}, ReviewEvidenceSatisfied, "r"},
		{"degraded pass", []ReviewRecordFact{{ID: "r", CriterionRef: "code-review", ValidatorKind: "human_review", Verdict: "passed", Degraded: true}}, ReviewEvidenceSatisfied, "r"},
		{"waived", []ReviewRecordFact{record("r", "waived", false)}, ReviewEvidenceSatisfied, "r"},
		{"failed", []ReviewRecordFact{record("r", "failed", false)}, ReviewEvidenceFailed, "r"},
		{"passed then work recorded", []ReviewRecordFact{record("r", "passed", true)}, ReviewEvidenceStale, "r"},
		{"failed then work recorded stays failed", []ReviewRecordFact{record("r", "failed", true)}, ReviewEvidenceFailed, "r"},
		{"a later failure is not masked by an earlier pass", []ReviewRecordFact{record("r1", "passed", false), record("r2", "failed", false)}, ReviewEvidenceFailed, "r2"},
		{"a later pass supersedes an earlier failure", []ReviewRecordFact{record("r1", "failed", false), record("r2", "passed", false)}, ReviewEvidenceSatisfied, "r2"},
		{"a fresh pass after a stale one", []ReviewRecordFact{record("r1", "passed", true), record("r2", "passed", false)}, ReviewEvidenceSatisfied, "r2"},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			evidence := EvaluateReviewEvidence([]ReviewRequirement{requirement}, testCase.records)
			if len(evidence) != 1 || evidence[0].State != testCase.state || evidence[0].ValidationRecordID != testCase.record || evidence[0].Requirement != requirement {
				t.Fatalf("evidence = %#v, want state %s from record %q", evidence, testCase.state, testCase.record)
			}
			if ReviewRequirementsSatisfied(evidence) != (testCase.state == ReviewEvidenceSatisfied) {
				t.Fatalf("satisfied = %v for state %s", ReviewRequirementsSatisfied(evidence), testCase.state)
			}
		})
	}
	if !ReviewRequirementsSatisfied(EvaluateReviewEvidence(nil, []ReviewRecordFact{record("r", "failed", false)})) {
		t.Fatal("an item that declares no review requirement did not pass")
	}
}

func TestNormalizeReviewRequirements(t *testing.T) {
	normalized, err := NormalizeReviewRequirements([]ReviewRequirement{{CriterionRef: " code-review ", ValidatorKind: " human_review "}, {CriterionRef: "suite", ValidatorKind: "probe"}})
	if err != nil {
		t.Fatal(err)
	}
	if normalized[0] != (ReviewRequirement{CriterionRef: "code-review", ValidatorKind: "human_review"}) || len(normalized) != 2 {
		t.Fatalf("normalized = %#v", normalized)
	}
	for _, invalid := range [][]ReviewRequirement{
		{{CriterionRef: "", ValidatorKind: "human_review"}},
		{{CriterionRef: "code-review", ValidatorKind: " "}},
		{{CriterionRef: "code-review", ValidatorKind: "human_review"}, {CriterionRef: " code-review", ValidatorKind: "human_review"}},
	} {
		if _, err := NormalizeReviewRequirements(invalid); err == nil {
			t.Fatalf("accepted %#v", invalid)
		}
	}
}
