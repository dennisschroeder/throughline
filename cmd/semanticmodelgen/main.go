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
	input, err := os.ReadFile(filepath.Join(root, "ontology", "throughline.json"))
	if err != nil {
		return err
	}
	var raw map[string]any
	if err := json.Unmarshal(input, &raw); err != nil {
		return fmt.Errorf("decode ontology: %w", err)
	}
	for _, section := range []string{"entities", "relations", "lifecycles", "invariants", "source_mappings"} {
		if _, ok := raw[section]; !ok {
			return fmt.Errorf("ontology missing %s", section)
		}
	}
	canonical, err := canonicalJSON(raw)
	if err != nil {
		return err
	}
	contentDigest := digest(canonical)
	var result model
	if err := json.Unmarshal(canonical, &result); err != nil {
		return err
	}
	result.ContentDigest = contentDigest
	result.SourceDigest, err = sourceDigest(root, result.SourceMapping)
	if err != nil {
		return err
	}
	result.Manifest = manifest{SchemaVersion: result.SchemaVersion, ModelVersion: result.ModelVersion, ContentDigest: result.ContentDigest, SourceDigest: result.SourceDigest, Bootstrap: result.Bootstrap, AvailableSections: []string{"manifest", "entities", "relations", "lifecycles", "invariants", "source_mappings", "full"}, SectionCounts: map[string]int{"entities": len(result.Entities), "relations": len(result.Relations), "lifecycles": len(result.Lifecycles), "invariants": len(result.Invariants), "source_mappings": len(result.SourceMapping)}}
	for _, values := range [][]record{result.Entities, result.Invariants} {
		sort.Slice(values, func(i, j int) bool { return values[i].ID < values[j].ID })
	}
	sort.Slice(result.Relations, func(i, j int) bool { return result.Relations[i].ID < result.Relations[j].ID })
	sort.Slice(result.Lifecycles, func(i, j int) bool { return result.Lifecycles[i].ID < result.Lifecycles[j].ID })
	sort.Slice(result.SourceMapping, func(i, j int) bool { return result.SourceMapping[i].ID < result.SourceMapping[j].ID })
	output, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		return err
	}
	output = append(output, '\n')
	path := filepath.Join(root, "internal", "semanticmodel", "model.generated.json")
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	return os.WriteFile(path, output, 0644)
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
			return fmt.Sprint(values[i].(map[string]any)["id"]) < fmt.Sprint(values[j].(map[string]any)["id"])
		})
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
		if mapping.Path == "" || seen[mapping.Path] {
			continue
		}
		normalized := filepath.ToSlash(filepath.Clean(mapping.Path))
		if filepath.IsAbs(mapping.Path) || normalized == ".." || strings.HasPrefix(normalized, "../") {
			return "", fmt.Errorf("source mapping %s escapes repository root", mapping.Path)
		}
		seen[mapping.Path] = true
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
		files := []string{path}
		if info.IsDir() {
			files = nil
			err = filepath.Walk(absolute, func(candidate string, entry os.FileInfo, walkErr error) error {
				if walkErr != nil {
					return walkErr
				}
				if entry.IsDir() {
					return nil
				}
				relative, relErr := filepath.Rel(root, candidate)
				if relErr != nil {
					return relErr
				}
				files = append(files, filepath.ToSlash(relative))
				return nil
			})
			if err != nil {
				return "", fmt.Errorf("source mapping %s: %w", path, err)
			}
			sort.Strings(files)
		}
		for _, file := range files {
			realFile, resolveErr := filepath.EvalSymlinks(filepath.Join(root, file))
			if resolveErr != nil {
				return "", resolveErr
			}
			relative, relErr := filepath.Rel(realRoot, realFile)
			if relErr != nil || relative == ".." || strings.HasPrefix(filepath.ToSlash(relative), "../") {
				return "", fmt.Errorf("source mapping %s resolves outside repository", file)
			}
			content, readErr := os.ReadFile(filepath.Join(root, file))
			if readErr != nil {
				return "", fmt.Errorf("source mapping %s: %w", file, readErr)
			}
			combined.WriteString(file)
			combined.WriteByte(0)
			combined.Write(content)
			combined.WriteByte(0)
		}
	}
	return digest(combined.Bytes()), nil
}

func digest(value []byte) string { sum := sha256.Sum256(value); return hex.EncodeToString(sum[:]) }
