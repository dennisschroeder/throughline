package output

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"
)

type ProfileState string

const (
	ProfileDraft      ProfileState = "draft"
	ProfileProposed   ProfileState = "proposed"
	ProfileActive     ProfileState = "active"
	ProfileRejected   ProfileState = "rejected"
	ProfileSuperseded ProfileState = "superseded"
)

type Profile struct {
	ID               string
	Name             string
	Version          int
	Description      string
	LifecycleState   ProfileState
	Structure        json.RawMessage
	Semantics        json.RawMessage
	Validation       json.RawMessage
	BuiltIn          bool
	SupersedesID     string
	ProposedBy       string
	ProposedAt       time.Time
	ResolvedBy       string
	ResolvedAt       time.Time
	ResolutionReason string
	CreatedAt        time.Time
}

func NewProfileProposal(profile Profile, now time.Time) (Profile, error) {
	profile.ID = strings.TrimSpace(profile.ID)
	profile.Name = strings.TrimSpace(profile.Name)
	profile.Description = strings.TrimSpace(profile.Description)
	profile.SupersedesID = strings.TrimSpace(profile.SupersedesID)
	profile.ProposedBy = strings.TrimSpace(profile.ProposedBy)
	if profile.ID == "" || profile.Name == "" || profile.ProposedBy == "" || profile.Version < 1 {
		return Profile{}, errors.New("output profile proposal requires id, name, positive version, and proposer")
	}
	for field, value := range map[string]json.RawMessage{
		"structure":  profile.Structure,
		"semantics":  profile.Semantics,
		"validation": profile.Validation,
	} {
		if err := validateJSONObject(value); err != nil {
			return Profile{}, fmt.Errorf("output profile %s: %w", field, err)
		}
	}
	profile.LifecycleState = ProfileProposed
	profile.BuiltIn = false
	profile.ProposedAt = now.UTC()
	profile.CreatedAt = now.UTC()
	return profile, nil
}

func ReviewProfile(profile Profile, decision ProfileState, reviewer, reason string, now time.Time) (Profile, error) {
	reviewer = strings.TrimSpace(reviewer)
	reason = strings.TrimSpace(reason)
	if profile.LifecycleState != ProfileProposed {
		return Profile{}, errors.New("only proposed output profiles can be reviewed")
	}
	if decision != ProfileActive && decision != ProfileRejected {
		return Profile{}, errors.New("output profile review must activate or reject")
	}
	if reviewer == "" || reason == "" {
		return Profile{}, errors.New("output profile review requires reviewer and reason")
	}
	profile.LifecycleState = decision
	profile.ResolvedBy = reviewer
	profile.ResolvedAt = now.UTC()
	profile.ResolutionReason = reason
	return profile, nil
}

type ExpectedOutput struct {
	ID              string
	WorkItemID      string
	Name            string
	OutputProfileID string
	Contract        json.RawMessage
	DestinationHint string
	Required        bool
	Ordinal         int
}

type ExpectedOutputDetail struct {
	ExpectedOutput ExpectedOutput
	Profile        Profile
}

func NewExpectedOutput(id, workItemID, name string, profile Profile, contract json.RawMessage, destinationHint string, required bool, ordinal int) (ExpectedOutput, error) {
	if profile.LifecycleState != ProfileActive {
		return ExpectedOutput{}, fmt.Errorf("output profile %s/v%d is not active", profile.Name, profile.Version)
	}
	if len(contract) == 0 {
		contract = json.RawMessage(`{}`)
	}
	if err := validateJSONObject(contract); err != nil {
		return ExpectedOutput{}, fmt.Errorf("expected output contract: %w", err)
	}
	if _, err := combinedRequiredValidations(profile, contract); err != nil {
		return ExpectedOutput{}, err
	}
	expected := ExpectedOutput{
		ID:              strings.TrimSpace(id),
		WorkItemID:      strings.TrimSpace(workItemID),
		Name:            strings.TrimSpace(name),
		OutputProfileID: profile.ID,
		Contract:        append(json.RawMessage(nil), contract...),
		DestinationHint: strings.TrimSpace(destinationHint),
		Required:        required,
		Ordinal:         ordinal,
	}
	if expected.ID == "" || expected.WorkItemID == "" || expected.Name == "" || expected.OutputProfileID == "" {
		return ExpectedOutput{}, errors.New("expected output requires id, work item id, name, and profile id")
	}
	if expected.Ordinal < 1 {
		return ExpectedOutput{}, errors.New("expected output ordinal must be positive")
	}
	return expected, nil
}

func validateJSONObject(value json.RawMessage) error {
	decoder := json.NewDecoder(bytes.NewReader(value))
	var object map[string]any
	if err := decoder.Decode(&object); err != nil {
		return err
	}
	if object == nil {
		return errors.New("must be a JSON object")
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		if err == nil {
			return errors.New("contains trailing JSON")
		}
		return err
	}
	return nil
}
