package work

import (
	"errors"
	"fmt"
	"strings"
)

// ReviewRequirement is a review a work item declares it needs before done:
// a validation of the item itself with this criterion reference and
// validator kind. How the review is performed is the workflow's business.
type ReviewRequirement struct {
	CriterionRef  string
	ValidatorKind string
}

type ReviewEvidenceState string

const (
	ReviewEvidenceSatisfied ReviewEvidenceState = "satisfied"
	ReviewEvidenceMissing   ReviewEvidenceState = "missing"
	ReviewEvidenceFailed    ReviewEvidenceState = "failed"
	ReviewEvidenceStale     ReviewEvidenceState = "stale"
)

// ReviewRecordFact is what the gate needs to know about one work-item
// validation. Records are passed oldest first.
type ReviewRecordFact struct {
	ID            string
	CriterionRef  string
	ValidatorKind string
	Verdict       string
	Degraded      bool
	Stale         bool
}

// ReviewEvidence is how one declared requirement currently stands, and which
// record decided it.
type ReviewEvidence struct {
	Requirement        ReviewRequirement
	State              ReviewEvidenceState
	ValidationRecordID string
	// Degraded repeats the deciding record's flag, so a reader sees a
	// satisfied requirement that rests on a weaker pass.
	Degraded bool
}

// NormalizeReviewRequirements trims each requirement and rejects empty or
// repeated ones, so a requirement can be matched by exact value.
func NormalizeReviewRequirements(requirements []ReviewRequirement) ([]ReviewRequirement, error) {
	seen := make(map[ReviewRequirement]bool, len(requirements))
	var result []ReviewRequirement
	for _, requirement := range requirements {
		requirement.CriterionRef = strings.TrimSpace(requirement.CriterionRef)
		requirement.ValidatorKind = strings.TrimSpace(requirement.ValidatorKind)
		if requirement.CriterionRef == "" || requirement.ValidatorKind == "" {
			return nil, errors.New("review requirement requires a criterion reference and a validator kind")
		}
		if seen[requirement] {
			return nil, fmt.Errorf("review requirement %s/%s is declared twice", requirement.CriterionRef, requirement.ValidatorKind)
		}
		seen[requirement] = true
		result = append(result, requirement)
	}
	return result, nil
}

// EvaluateReviewEvidence decides each requirement from the latest record that
// matches it exactly. A later failed review is not masked by an earlier pass,
// and a pass followed by recorded work no longer vouches for that work.
func EvaluateReviewEvidence(requirements []ReviewRequirement, records []ReviewRecordFact) []ReviewEvidence {
	result := make([]ReviewEvidence, 0, len(requirements))
	for _, requirement := range requirements {
		evidence := ReviewEvidence{Requirement: requirement, State: ReviewEvidenceMissing}
		for _, record := range records {
			if record.CriterionRef != requirement.CriterionRef || record.ValidatorKind != requirement.ValidatorKind {
				continue
			}
			evidence.ValidationRecordID = record.ID
			evidence.Degraded = record.Degraded
			switch {
			case record.Verdict != "passed" && record.Verdict != "waived":
				evidence.State = ReviewEvidenceFailed
			case record.Stale:
				evidence.State = ReviewEvidenceStale
			default:
				evidence.State = ReviewEvidenceSatisfied
			}
		}
		result = append(result, evidence)
	}
	return result
}

// ReviewRequirementsSatisfied reports whether every declared requirement is
// satisfied; an item that declares none has nothing outstanding.
func ReviewRequirementsSatisfied(evidence []ReviewEvidence) bool {
	for _, item := range evidence {
		if item.State != ReviewEvidenceSatisfied {
			return false
		}
	}
	return true
}
