// Package claudecodeconfig reconciles Throughline's entry in Claude Code's global
// ~/.claude.json user-scope MCP configuration, following the accepted "one global MCP
// entry per harness" decision and the compatibility contract recorded in
// docs/research/mcp-transport-compatibility.md (a Streamable HTTP entry with an
// environment-expanded Authorization header, so the token itself never enters the file).
package claudecodeconfig

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/dennisschroeder/throughline/internal/clientconfig"
)

// DefaultPath returns Claude Code's global state file, shared across projects for
// user-scoped settings including MCP servers.
func DefaultPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve user home directory: %w", err)
	}
	return filepath.Join(home, ".claude.json"), nil
}

// Reconcile writes or updates the top-level mcpServers.throughline entry in path so it
// matches entry, preserving every other key in the file (including any other MCP servers
// and Claude Code's own state) untouched. If an entry already exists under that name and
// matches entry exactly, it makes no change. If an entry exists and differs, it returns
// *clientconfig.ErrConflict instead of overwriting it unless force is true.
func Reconcile(path string, entry clientconfig.Entry, force bool) (clientconfig.Result, error) {
	document, err := loadDocument(path)
	if err != nil {
		return clientconfig.Result{}, err
	}

	servers, _ := document["mcpServers"].(map[string]any)
	if servers == nil {
		servers = map[string]any{}
	}

	desired := map[string]any{
		"type": "http",
		"url":  entry.URL,
	}
	if entry.BearerTokenEnvVar != "" {
		desired["headers"] = map[string]any{
			"Authorization": "Bearer ${" + entry.BearerTokenEnvVar + "}",
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
	document["mcpServers"] = servers
	if err := writeDocument(path, document); err != nil {
		return clientconfig.Result{}, err
	}
	return clientconfig.Result{Changed: true}, nil
}

// Remove deletes the mcpServers.throughline entry from path if present, preserving every
// other key. It reports whether an entry was actually present to remove.
func Remove(path string) (bool, error) {
	document, err := loadDocument(path)
	if err != nil {
		return false, err
	}
	servers, _ := document["mcpServers"].(map[string]any)
	if servers == nil {
		return false, nil
	}
	if _, ok := servers[clientconfig.ServerName]; !ok {
		return false, nil
	}
	delete(servers, clientconfig.ServerName)
	document["mcpServers"] = servers
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
		return nil, fmt.Errorf("read claude code config: %w", err)
	}
	document := map[string]any{}
	if err := json.Unmarshal(content, &document); err != nil {
		return nil, fmt.Errorf("parse claude code config: %w", err)
	}
	return document, nil
}

func writeDocument(path string, document map[string]any) error {
	encoded, err := json.MarshalIndent(document, "", "  ")
	if err != nil {
		return fmt.Errorf("encode claude code config: %w", err)
	}
	encoded = append(encoded, '\n')
	directory := filepath.Dir(path)
	if err := os.MkdirAll(directory, 0o755); err != nil {
		return fmt.Errorf("create claude code config directory: %w", err)
	}
	temporary, err := os.CreateTemp(directory, ".claude-*.json.tmp")
	if err != nil {
		return fmt.Errorf("create temporary claude code config: %w", err)
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)
	if _, err := temporary.Write(encoded); err != nil {
		_ = temporary.Close()
		return fmt.Errorf("write temporary claude code config: %w", err)
	}
	if err := temporary.Close(); err != nil {
		return fmt.Errorf("close temporary claude code config: %w", err)
	}
	if err := os.Rename(temporaryPath, path); err != nil {
		return fmt.Errorf("install claude code config: %w", err)
	}
	return nil
}

// valuesEqual compares two decoded JSON values by their marshaled representation rather
// than reflect.DeepEqual, since a value round-tripped through Unmarshal (existing) and a
// hand-built map (desired) can differ in concrete numeric/interface wrapper types while
// still describing the same configuration; encoding/json normalizes both consistently.
func valuesEqual(a, b any) bool {
	aEncoded, aErr := json.Marshal(a)
	bEncoded, bErr := json.Marshal(b)
	return aErr == nil && bErr == nil && string(aEncoded) == string(bEncoded)
}
