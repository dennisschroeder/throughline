// Package hermesconfig reconciles Throughline's entry in Hermes Agent's global
// ~/.hermes/config.yaml, following the accepted "one global MCP entry per harness" decision.
// Because Hermes does not read MCP server instructions into the agent context (see
// docs/research/mcp-transport-compatibility.md), this adapter's entry — url plus an
// environment-expanded Authorization header — must be sufficient on its own, with no
// server-instruction dependency.
package hermesconfig

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"gopkg.in/yaml.v3"

	"github.com/dennisschroeder/throughline/internal/clientconfig"
)

// DefaultPath returns Hermes's global configuration file for the active profile's home.
func DefaultPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve user home directory: %w", err)
	}
	return filepath.Join(home, ".hermes", "config.yaml"), nil
}

// Reconcile writes or updates the top-level mcp_servers.throughline entry in path so it
// matches entry, preserving every other key in the file — including profiles and any other
// mcp_servers entry — untouched. If an entry already exists under that name and matches
// entry exactly, it makes no change. If an entry exists and differs, it returns
// *clientconfig.ErrConflict instead of overwriting it unless force is true.
func Reconcile(path string, entry clientconfig.Entry, force bool) (clientconfig.Result, error) {
	document, err := loadDocument(path)
	if err != nil {
		return clientconfig.Result{}, err
	}

	servers, _ := document["mcp_servers"].(map[string]any)
	if servers == nil {
		servers = map[string]any{}
	}

	desired := map[string]any{"url": entry.URL}
	if entry.BearerTokenEnvVar != "" {
		desired["headers"] = map[string]any{
			"Authorization": "Bearer ${env:" + entry.BearerTokenEnvVar + "}",
		}
	}

	if existing, ok := servers[clientconfig.ServerName]; ok {
		if valuesEqual(existing, desired) {
			return clientconfig.Result{Changed: false}, nil
		}
		if !force {
			return clientconfig.Result{}, &clientconfig.ErrConflict{
				Path:   path,
				Reason: fmt.Sprintf("existing entry %#v does not match the expected %#v", existing, desired),
			}
		}
	}

	servers[clientconfig.ServerName] = desired
	document["mcp_servers"] = servers
	if err := writeDocument(path, document); err != nil {
		return clientconfig.Result{}, err
	}
	return clientconfig.Result{Changed: true}, nil
}

// Remove deletes the mcp_servers.throughline entry from path if present, preserving every
// other key. It reports whether an entry was actually present to remove.
func Remove(path string) (bool, error) {
	document, err := loadDocument(path)
	if err != nil {
		return false, err
	}
	servers, _ := document["mcp_servers"].(map[string]any)
	if servers == nil {
		return false, nil
	}
	if _, ok := servers[clientconfig.ServerName]; !ok {
		return false, nil
	}
	delete(servers, clientconfig.ServerName)
	document["mcp_servers"] = servers
	if err := writeDocument(path, document); err != nil {
		return false, err
	}
	return true, nil
}

func loadDocument(path string) (map[string]any, error) {
	content, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return map[string]any{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read hermes config: %w", err)
	}
	document := map[string]any{}
	if err := yaml.Unmarshal(content, &document); err != nil {
		return nil, fmt.Errorf("parse hermes config: %w", err)
	}
	return document, nil
}

func writeDocument(path string, document map[string]any) error {
	node, err := orderedNode(document)
	if err != nil {
		return fmt.Errorf("encode hermes config: %w", err)
	}
	encoded, err := yaml.Marshal(node)
	if err != nil {
		return fmt.Errorf("encode hermes config: %w", err)
	}
	directory := filepath.Dir(path)
	if err := os.MkdirAll(directory, 0o755); err != nil {
		return fmt.Errorf("create hermes config directory: %w", err)
	}
	temporary, err := os.CreateTemp(directory, ".hermes-*.yaml.tmp")
	if err != nil {
		return fmt.Errorf("create temporary hermes config: %w", err)
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)
	if _, err := temporary.Write(encoded); err != nil {
		_ = temporary.Close()
		return fmt.Errorf("write temporary hermes config: %w", err)
	}
	if err := temporary.Close(); err != nil {
		return fmt.Errorf("close temporary hermes config: %w", err)
	}
	if err := os.Rename(temporaryPath, path); err != nil {
		return fmt.Errorf("install hermes config: %w", err)
	}
	return nil
}

// orderedNode converts a decoded-YAML value into a yaml.Node tree with every map's keys
// sorted, so repeated writes of the same logical document are byte-identical: Go's
// map[string]any iteration order is randomized, and yaml.v3 does not sort it for us.
func orderedNode(value any) (*yaml.Node, error) {
	switch typed := value.(type) {
	case map[string]any:
		node := &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
		keys := make([]string, 0, len(typed))
		for key := range typed {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		for _, key := range keys {
			valueNode, err := orderedNode(typed[key])
			if err != nil {
				return nil, err
			}
			node.Content = append(node.Content, &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: key}, valueNode)
		}
		return node, nil
	case []any:
		node := &yaml.Node{Kind: yaml.SequenceNode, Tag: "!!seq"}
		for _, item := range typed {
			itemNode, err := orderedNode(item)
			if err != nil {
				return nil, err
			}
			node.Content = append(node.Content, itemNode)
		}
		return node, nil
	default:
		var node yaml.Node
		if err := node.Encode(value); err != nil {
			return nil, err
		}
		return &node, nil
	}
}

// valuesEqual compares two decoded YAML values by their canonical YAML representation
// rather than reflect.DeepEqual, since a value round-tripped through Unmarshal (existing)
// and a hand-built map (desired) can differ in concrete numeric/interface wrapper type
// while describing the same configuration.
func valuesEqual(a, b any) bool {
	aNode, aErr := orderedNode(a)
	bNode, bErr := orderedNode(b)
	if aErr != nil || bErr != nil {
		return false
	}
	aEncoded, aErr := yaml.Marshal(aNode)
	bEncoded, bErr := yaml.Marshal(bNode)
	return aErr == nil && bErr == nil && string(aEncoded) == string(bEncoded)
}
