//go:generate go run ../../cmd/semanticmodelgen

package semanticmodel

import (
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
)

//go:embed model.generated.json
var generated []byte

type Record struct {
	ID         string `json:"id"`
	Name       string `json:"name,omitempty"`
	Definition string `json:"definition,omitempty"`
	Statement  string `json:"statement,omitempty"`
}

type Relation struct {
	ID          string `json:"id"`
	From        string `json:"from"`
	To          string `json:"to"`
	Cardinality string `json:"cardinality"`
}

type Lifecycle struct {
	ID          string      `json:"id"`
	Entity      string      `json:"entity"`
	States      []string    `json:"states"`
	Transitions [][2]string `json:"transitions"`
	ResumeRule  string      `json:"resume_rule,omitempty"`
}

type SourceMapping struct {
	ID      string `json:"id"`
	Path    string `json:"path"`
	Symbol  string `json:"symbol,omitempty"`
	Heading string `json:"heading,omitempty"`
}

type Manifest struct {
	SchemaVersion     string         `json:"schema_version"`
	ModelVersion      string         `json:"model_version"`
	ContentDigest     string         `json:"content_digest"`
	SourceDigest      string         `json:"source_digest"`
	Bootstrap         string         `json:"bootstrap"`
	AvailableSections []string       `json:"available_sections"`
	SectionCounts     map[string]int `json:"section_counts"`
}

type Model struct {
	SchemaVersion  string          `json:"schema_version"`
	ModelVersion   string          `json:"model_version"`
	Bootstrap      string          `json:"bootstrap"`
	Entities       []Record        `json:"entities"`
	Relations      []Relation      `json:"relations"`
	Lifecycles     []Lifecycle     `json:"lifecycles"`
	Invariants     []Record        `json:"invariants"`
	SourceMappings []SourceMapping `json:"source_mappings"`
	Manifest       Manifest        `json:"manifest"`
	ContentDigest  string          `json:"content_digest"`
	SourceDigest   string          `json:"source_digest"`
}

type InvalidArtifactError struct{ Reason string }

func (e *InvalidArtifactError) Error() string {
	return "semantic model artifact is invalid: " + e.Reason
}

var (
	loadOnce sync.Once
	loaded   *Model
	loadErr  error
)

func Load() (*Model, error) {
	loadOnce.Do(func() {
		var model Model
		if err := json.Unmarshal(generated, &model); err != nil {
			loadErr = &InvalidArtifactError{Reason: err.Error()}
			return
		}
		if err := model.Validate(); err != nil {
			loadErr = err
			return
		}
		loaded = &model
	})
	return loaded, loadErr
}

func (model *Model) Validate() error {
	if model.SchemaVersion == "" || model.ModelVersion == "" || model.ContentDigest == "" || model.SourceDigest == "" {
		return &InvalidArtifactError{Reason: "manifest fields are required"}
	}
	if len(model.Bootstrap) > 2048 {
		return &InvalidArtifactError{Reason: "bootstrap exceeds 2 KiB"}
	}
	var raw map[string]any
	if err := json.Unmarshal(generated, &raw); err != nil {
		return &InvalidArtifactError{Reason: err.Error()}
	}
	delete(raw, "manifest")
	delete(raw, "content_digest")
	delete(raw, "source_digest")
	canonical, err := json.Marshal(raw)
	if err != nil {
		return &InvalidArtifactError{Reason: err.Error()}
	}
	contentSum := sha256.Sum256(canonical)
	if hex.EncodeToString(contentSum[:]) != model.ContentDigest {
		return &InvalidArtifactError{Reason: "content digest does not match artifact content"}
	}
	if model.Manifest.SchemaVersion != model.SchemaVersion || model.Manifest.ModelVersion != model.ModelVersion || model.Manifest.ContentDigest != model.ContentDigest || model.Manifest.SourceDigest != model.SourceDigest || model.Manifest.Bootstrap != model.Bootstrap {
		return &InvalidArtifactError{Reason: "manifest does not match model"}
	}
	counts := model.Manifest.SectionCounts
	if counts["entities"] != len(model.Entities) || counts["relations"] != len(model.Relations) || counts["lifecycles"] != len(model.Lifecycles) || counts["invariants"] != len(model.Invariants) || counts["source_mappings"] != len(model.SourceMappings) {
		return &InvalidArtifactError{Reason: "manifest section counts do not match model"}
	}
	if !sameStrings(model.Manifest.AvailableSections, AvailableSections()) {
		return &InvalidArtifactError{Reason: "manifest available sections do not match model"}
	}
	entityIDs := map[string]bool{}
	lifecycleIDs := map[string]bool{}
	invariantIDs := map[string]bool{}
	mappingIDs := map[string]bool{}
	for _, entity := range model.Entities {
		if entity.ID == "" || entityIDs[entity.ID] {
			return &InvalidArtifactError{Reason: "entity IDs must be unique and non-empty"}
		}
		entityIDs[entity.ID] = true
	}
	relationIDs := map[string]bool{}
	for _, relation := range model.Relations {
		if relation.ID == "" || relationIDs[relation.ID] {
			return &InvalidArtifactError{Reason: "relation IDs must be unique and non-empty"}
		}
		relationIDs[relation.ID] = true
		if relation.ID == "" || !entityIDs[relation.From] || !entityIDs[relation.To] {
			return &InvalidArtifactError{Reason: fmt.Sprintf("relation %q has an invalid endpoint", relation.ID)}
		}
	}
	for _, lifecycle := range model.Lifecycles {
		if lifecycle.ID == "" || lifecycleIDs[lifecycle.ID] {
			return &InvalidArtifactError{Reason: "lifecycle IDs must be unique and non-empty"}
		}
		lifecycleIDs[lifecycle.ID] = true
		if !entityIDs[lifecycle.Entity] {
			return &InvalidArtifactError{Reason: fmt.Sprintf("lifecycle %q has an invalid entity", lifecycle.ID)}
		}
		states := map[string]bool{}
		for _, state := range lifecycle.States {
			if state == "" || states[state] {
				return &InvalidArtifactError{Reason: fmt.Sprintf("lifecycle %q has invalid states", lifecycle.ID)}
			}
			states[state] = true
		}
		for _, transition := range lifecycle.Transitions {
			if !states[transition[0]] || !states[transition[1]] {
				return &InvalidArtifactError{Reason: fmt.Sprintf("lifecycle %q has an invalid transition", lifecycle.ID)}
			}
		}
	}
	for _, invariant := range model.Invariants {
		if invariant.ID == "" || invariantIDs[invariant.ID] {
			return &InvalidArtifactError{Reason: "invariant IDs must be unique and non-empty"}
		}
		invariantIDs[invariant.ID] = true
	}
	for _, mapping := range model.SourceMappings {
		if mapping.ID == "" || mappingIDs[mapping.ID] || mapping.Path == "" || (mapping.Symbol == "" && mapping.Heading == "") {
			return &InvalidArtifactError{Reason: "source mappings must have unique IDs, paths, and symbols or headings"}
		}
		mappingIDs[mapping.ID] = true
	}
	return nil
}

func sameStrings(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}

func (model *Model) Section(section string, ids []string) (any, []string, error) {
	if section == "" {
		section = "manifest"
	}
	if !isSupportedSection(section) {
		return nil, nil, fmt.Errorf("unknown semantic model section %q", section)
	}
	if len(ids) > 50 {
		return nil, nil, errors.New("ids cannot contain more than 50 values")
	}
	if section == "manifest" {
		if len(ids) > 0 {
			return nil, nil, errors.New("ids are not supported for manifest")
		}
		return model.Manifest, []string{}, nil
	}
	if section == "full" {
		if len(ids) > 0 {
			return nil, nil, errors.New("ids are not supported for full")
		}
		return model, []string{}, nil
	}
	var values []any
	switch section {
	case "entities":
		for _, value := range model.Entities {
			values = append(values, value)
		}
	case "relations":
		for _, value := range model.Relations {
			values = append(values, value)
		}
	case "lifecycles":
		for _, value := range model.Lifecycles {
			values = append(values, value)
		}
	case "invariants":
		for _, value := range model.Invariants {
			values = append(values, value)
		}
	case "source_mappings":
		for _, value := range model.SourceMappings {
			values = append(values, value)
		}
	default:
		return nil, nil, fmt.Errorf("unknown semantic model section %q", section)
	}
	if len(ids) == 0 {
		return values, []string{}, nil
	}
	wanted := map[string]bool{}
	for _, id := range ids {
		if wanted[id] {
			return nil, nil, fmt.Errorf("ids must be unique")
		}
		wanted[id] = true
	}
	found := map[string]bool{}
	filtered := make([]any, 0, len(ids))
	for _, value := range values {
		encoded, _ := json.Marshal(value)
		var object map[string]any
		_ = json.Unmarshal(encoded, &object)
		if wanted[object["id"].(string)] {
			filtered = append(filtered, value)
			found[object["id"].(string)] = true
		}
	}
	missing := make([]string, 0)
	for _, id := range ids {
		if !found[id] {
			missing = append(missing, id)
		}
	}
	return filtered, missing, nil
}
