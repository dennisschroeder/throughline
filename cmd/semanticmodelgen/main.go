package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/dennisschroeder/throughline/internal/semanticmodel"
)

type record struct {
	ID         string `json:"id"`
	Name       string `json:"name,omitempty"`
	Definition string `json:"definition,omitempty"`
	Statement  string `json:"statement,omitempty"`
}

type model struct {
	SchemaVersion string          `json:"schema_version"`
	ModelVersion  string          `json:"model_version"`
	Bootstrap     string          `json:"bootstrap"`
	Entities      []record        `json:"entities"`
	Relations     []relation      `json:"relations"`
	Lifecycles    []lifecycle     `json:"lifecycles"`
	Invariants    []record        `json:"invariants"`
	SourceMapping []sourceMapping `json:"source_mappings"`
	Manifest      manifest        `json:"manifest"`
	ContentDigest string          `json:"content_digest"`
	SourceDigest  string          `json:"source_digest"`
}

type relation struct {
	ID          string `json:"id"`
	From        string `json:"from"`
	To          string `json:"to"`
	Cardinality string `json:"cardinality"`
}
type lifecycle struct {
	ID          string      `json:"id"`
	Entity      string      `json:"entity"`
	States      []string    `json:"states"`
	Transitions [][2]string `json:"transitions"`
	ResumeRule  string      `json:"resume_rule,omitempty"`
}
type sourceMapping struct {
	ID      string `json:"id"`
	Path    string `json:"path"`
	Symbol  string `json:"symbol,omitempty"`
	Heading string `json:"heading,omitempty"`
}
type manifest struct {
	SchemaVersion     string         `json:"schema_version"`
	ModelVersion      string         `json:"model_version"`
	ContentDigest     string         `json:"content_digest"`
	SourceDigest      string         `json:"source_digest"`
	Bootstrap         string         `json:"bootstrap"`
	AvailableSections []string       `json:"available_sections"`
	SectionCounts     map[string]int `json:"section_counts"`
}

func main() {
	if err := generate(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func generate() error {
	root, err := repositoryRoot()
	if err != nil {
		return err
	}
	output, err := renderModel(root)
	if err != nil {
		return err
	}
	path := filepath.Join(root, "internal", "semanticmodel", "model.generated.json")
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	return os.WriteFile(path, output, 0644)
}

func renderModel(root string) ([]byte, error) {
	input, err := os.ReadFile(filepath.Join(root, "ontology", "throughline.json"))
	if err != nil {
		return nil, err
	}
	var raw map[string]any
	if err := json.Unmarshal(input, &raw); err != nil {
		return nil, fmt.Errorf("decode ontology: %w", err)
	}
	for _, section := range []string{"entities", "relations", "lifecycles", "invariants", "source_mappings"} {
		if _, ok := raw[section]; !ok {
			return nil, fmt.Errorf("ontology missing %s", section)
		}
	}
	canonical, err := canonicalJSON(raw)
	if err != nil {
		return nil, err
	}
	contentDigest := digest(canonical)
	var result model
	if err := json.Unmarshal(canonical, &result); err != nil {
		return nil, err
	}
	result.ContentDigest = contentDigest
	result.SourceDigest, err = sourceDigest(root, result.SourceMapping)
	if err != nil {
		return nil, err
	}
	result.Manifest = manifest{SchemaVersion: result.SchemaVersion, ModelVersion: result.ModelVersion, ContentDigest: result.ContentDigest, SourceDigest: result.SourceDigest, Bootstrap: result.Bootstrap, AvailableSections: semanticmodel.AvailableSections(), SectionCounts: map[string]int{"entities": len(result.Entities), "relations": len(result.Relations), "lifecycles": len(result.Lifecycles), "invariants": len(result.Invariants), "source_mappings": len(result.SourceMapping)}}
	for _, values := range [][]record{result.Entities, result.Invariants} {
		sort.Slice(values, func(i, j int) bool { return values[i].ID < values[j].ID })
	}
	sort.Slice(result.Relations, func(i, j int) bool { return result.Relations[i].ID < result.Relations[j].ID })
	sort.Slice(result.Lifecycles, func(i, j int) bool { return result.Lifecycles[i].ID < result.Lifecycles[j].ID })
	sort.Slice(result.SourceMapping, func(i, j int) bool { return result.SourceMapping[i].ID < result.SourceMapping[j].ID })
	output, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		return nil, err
	}
	output = append(output, '\n')
	return output, nil
}

func repositoryRoot() (string, error) {
	root, err := os.Getwd()
	if err != nil {
		return "", err
	}
	for {
		if _, statErr := os.Stat(filepath.Join(root, "ontology", "throughline.json")); statErr == nil {
			return root, nil
		}
		parent := filepath.Dir(root)
		if parent == root {
			return "", fmt.Errorf("could not locate ontology/throughline.json")
		}
		root = parent
	}
}

func canonicalJSON(value map[string]any) ([]byte, error) {
	for _, section := range []string{"entities", "relations", "lifecycles", "invariants", "source_mappings"} {
		values, ok := value[section].([]any)
		if !ok {
			return nil, fmt.Errorf("ontology %s must be an array", section)
		}
		sort.SliceStable(values, func(i, j int) bool {
			left, leftOK := values[i].(map[string]any)
			right, rightOK := values[j].(map[string]any)
			if !leftOK || !rightOK {
				return false
			}
			return fmt.Sprint(left["id"]) < fmt.Sprint(right["id"])
		})
		for _, candidate := range values {
			record, ok := candidate.(map[string]any)
			if !ok || strings.TrimSpace(fmt.Sprint(record["id"])) == "" {
				return nil, fmt.Errorf("ontology %s entries must be objects with IDs", section)
			}
		}
	}
	delete(value, "manifest")
	delete(value, "content_digest")
	delete(value, "source_digest")
	return json.Marshal(value)
}

func sourceDigest(root string, mappings []sourceMapping) (string, error) {
	realRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		return "", err
	}
	paths := make([]string, 0, len(mappings))
	seen := map[string]bool{}
	for _, mapping := range mappings {
		if mapping.Path == "" {
			continue
		}
		normalized := filepath.ToSlash(filepath.Clean(mapping.Path))
		if filepath.IsAbs(mapping.Path) || normalized == ".." || strings.HasPrefix(normalized, "../") {
			return "", fmt.Errorf("source mapping %s escapes repository root", mapping.Path)
		}
		if seen[normalized] {
			continue
		}
		seen[normalized] = true
		paths = append(paths, normalized)
	}
	sort.Strings(paths)
	var combined bytes.Buffer
	for _, path := range paths {
		absolute := filepath.Join(root, path)
		info, err := os.Stat(absolute)
		if err != nil {
			return "", fmt.Errorf("source mapping %s: %w", path, err)
		}
		if info.IsDir() {
			return "", fmt.Errorf("source mapping %s must reference a file", path)
		}
		realFile, resolveErr := filepath.EvalSymlinks(filepath.Join(root, path))
		if resolveErr != nil {
			return "", resolveErr
		}
		relative, relErr := filepath.Rel(realRoot, realFile)
		if relErr != nil || relative == ".." || strings.HasPrefix(filepath.ToSlash(relative), "../") {
			return "", fmt.Errorf("source mapping %s resolves outside repository", path)
		}
		content, readErr := os.ReadFile(filepath.Join(root, path))
		if readErr != nil {
			return "", fmt.Errorf("source mapping %s: %w", path, readErr)
		}
		mapping, ok := mappingForPath(mappings, path)
		if !ok {
			return "", fmt.Errorf("source mapping %s is missing", path)
		}
		anchor := mapping.Symbol
		if anchor == "" {
			anchor = mapping.Heading
		}
		if !bytes.Contains(content, []byte(anchor)) {
			return "", fmt.Errorf("source mapping %s anchor %q does not exist", path, anchor)
		}
		combined.WriteString(path)
		combined.WriteByte(0)
		combined.Write(content)
		combined.WriteByte(0)
	}
	return digest(combined.Bytes()), nil
}

func mappingForPath(mappings []sourceMapping, path string) (sourceMapping, bool) {
	for _, mapping := range mappings {
		if filepath.ToSlash(filepath.Clean(mapping.Path)) == path {
			return mapping, true
		}
	}
	return sourceMapping{}, false
}

func digest(value []byte) string { sum := sha256.Sum256(value); return hex.EncodeToString(sum[:]) }
