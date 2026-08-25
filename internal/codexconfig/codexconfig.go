// Package codexconfig reconciles Throughline's entry in Codex's global
// ~/.codex/config.toml, following the accepted "one global MCP entry per harness" decision
// and the compatibility contract recorded in docs/research/mcp-transport-compatibility.md
// (bearer_token_env_var over static http_headers, so the token itself never enters the
// file).
package codexconfig

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/pelletier/go-toml/v2"

	"github.com/dennisschroeder/throughline/internal/clientconfig"
)

// DefaultPath returns Codex's global configuration file, shared by the ChatGPT desktop
// app, Codex CLI, and IDE extension on the same host.
func DefaultPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve user home directory: %w", err)
	}
	return filepath.Join(home, ".codex", "config.toml"), nil
}

// Reconcile writes or updates the [mcp_servers.throughline] table in path so it matches
// entry, preserving every other key and table in the file untouched. If an entry already
// exists under that name and matches entry exactly, it makes no change. If an entry exists
// and differs, it returns *clientconfig.ErrConflict instead of overwriting it unless force
// is true.
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
		desired["bearer_token_env_var"] = entry.BearerTokenEnvVar
	}
	if entry.Required {
		desired["required"] = true
	}

	if existing, ok := servers[clientconfig.ServerName]; ok {
		if mapsEqual(existing, desired) {
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

// Remove deletes the [mcp_servers.throughline] entry from path if present, preserving
// every other key. It reports whether an entry was actually present to remove.
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
		return nil, fmt.Errorf("read codex config: %w", err)
	}
	document := map[string]any{}
	if err := toml.Unmarshal(content, &document); err != nil {
		return nil, fmt.Errorf("parse codex config: %w", err)
	}
	return document, nil
}

func writeDocument(path string, document map[string]any) error {
	encoded, err := toml.Marshal(document)
	if err != nil {
		return fmt.Errorf("encode codex config: %w", err)
	}
	directory := filepath.Dir(path)
	if err := os.MkdirAll(directory, 0o755); err != nil {
		return fmt.Errorf("create codex config directory: %w", err)
	}
	temporary, err := os.CreateTemp(directory, ".config-*.toml.tmp")
	if err != nil {
		return fmt.Errorf("create temporary codex config: %w", err)
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)
	if _, err := temporary.Write(encoded); err != nil {
		_ = temporary.Close()
		return fmt.Errorf("write temporary codex config: %w", err)
	}
	if err := temporary.Close(); err != nil {
		return fmt.Errorf("close temporary codex config: %w", err)
	}
	if err := os.Rename(temporaryPath, path); err != nil {
		return fmt.Errorf("install codex config: %w", err)
	}
	return nil
}

// mapsEqual compares two decoded TOML values field by field rather than with
// reflect.DeepEqual, since a value round-tripped through Unmarshal (existing) and a
// hand-built map (desired) can differ in concrete numeric/bool wrapper types while still
// being the same configuration.
func mapsEqual(a, b any) bool {
	am, aok := a.(map[string]any)
	bm, bok := b.(map[string]any)
	if !aok || !bok {
		return false
	}
	if len(am) != len(bm) {
		return false
	}
	for key, avalue := range am {
		bvalue, ok := bm[key]
		if !ok || fmt.Sprint(avalue) != fmt.Sprint(bvalue) {
			return false
		}
	}
	return true
}
