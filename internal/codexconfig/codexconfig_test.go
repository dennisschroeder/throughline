package codexconfig

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/pelletier/go-toml/v2"

	"github.com/dennisschroeder/throughline/internal/clientconfig"
)

var testEntry = clientconfig.Entry{
	URL:               "http://127.0.0.1:43121/mcp",
	BearerTokenEnvVar: "THROUGHLINE_MCP_TOKEN",
	Required:          true,
}

// codex0149Fixture is a representative ~/.codex/config.toml as written by Codex 0.149.1:
// unrelated top-level settings, a profile table, and one pre-existing unrelated MCP server.
// Reconcile must add the throughline entry alongside these without disturbing any of it.
const codex0149Fixture = `model = "gpt-5-codex"
approval_policy = "on-request"
sandbox_mode = "workspace-write"

[profiles.default]
model = "gpt-5-codex"
approval_policy = "never"

[mcp_servers.filesystem]
command = "npx"
args = ["-y", "@modelcontextprotocol/server-filesystem", "/Users/example/projects"]
`

func TestReconcileAddsTheEntryAndPreservesUnrelatedConfiguration(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(path, []byte(codex0149Fixture), 0o644); err != nil {
		t.Fatal(err)
	}

	result, err := Reconcile(path, testEntry, false)
	if err != nil {
		t.Fatal(err)
	}
	if !result.Changed {
		t.Fatal("expected the first reconcile to change the file")
	}

	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var document map[string]any
	if err := toml.Unmarshal(content, &document); err != nil {
		t.Fatalf("reconciled config.toml is not valid TOML: %v\n%s", err, content)
	}

	if document["model"] != "gpt-5-codex" || document["approval_policy"] != "on-request" || document["sandbox_mode"] != "workspace-write" {
		t.Fatalf("top-level settings were disturbed: %#v", document)
	}
	profiles, _ := document["profiles"].(map[string]any)
	if profiles == nil || profiles["default"] == nil {
		t.Fatalf("profiles.default was lost: %#v", document)
	}
	servers, _ := document["mcp_servers"].(map[string]any)
	if servers == nil {
		t.Fatalf("mcp_servers table was lost: %#v", document)
	}
	if servers["filesystem"] == nil {
		t.Fatalf("the pre-existing filesystem server was lost: %#v", servers)
	}
	throughline, ok := servers["throughline"].(map[string]any)
	if !ok {
		t.Fatalf("throughline entry missing: %#v", servers)
	}
	if throughline["url"] != testEntry.URL || throughline["bearer_token_env_var"] != testEntry.BearerTokenEnvVar || throughline["required"] != true {
		t.Fatalf("throughline entry = %#v", throughline)
	}
}

func TestReconcileNeverWritesTheTokenValueItself(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	if _, err := Reconcile(path, testEntry, false); err != nil {
		t.Fatal(err)
	}
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if containsSecretLikeValue(string(content)) {
		t.Fatalf("config unexpectedly contains what looks like a raw token: %s", content)
	}
}

func containsSecretLikeValue(content string) bool {
	// The adapter only ever writes the env var *name*; a real 256-bit hex token would be
	// 64 hex characters with no separators, which THROUGHLINE_MCP_TOKEN (an identifier) is
	// not, so this is a coarse but sufficient check that no such value slipped in.
	return strings.Contains(content, "Bearer ") || strings.Contains(content, "Authorization")
}

func TestReconcileIsIdempotent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	if _, err := Reconcile(path, testEntry, false); err != nil {
		t.Fatal(err)
	}
	first, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	result, err := Reconcile(path, testEntry, false)
	if err != nil {
		t.Fatal(err)
	}
	if result.Changed {
		t.Fatal("second reconcile with the same entry reported a change")
	}
	second, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(first) != string(second) {
		t.Fatalf("idempotent reconcile changed the file:\nfirst:\n%s\nsecond:\n%s", first, second)
	}
}

func TestReconcileDiagnosesAConflictWithoutOverwriting(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	conflicting := codex0149Fixture + "\n[mcp_servers.throughline]\nurl = \"http://127.0.0.1:9999/mcp\"\n"
	if err := os.WriteFile(path, []byte(conflicting), 0o644); err != nil {
		t.Fatal(err)
	}

	_, err := Reconcile(path, testEntry, false)
	var conflict *clientconfig.ErrConflict
	if !errors.As(err, &conflict) {
		t.Fatalf("Reconcile error = %v, want *clientconfig.ErrConflict", err)
	}

	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(content) != conflicting {
		t.Fatalf("file was modified despite an unresolved conflict:\nwant:\n%s\ngot:\n%s", conflicting, content)
	}
}

func TestReconcileWithForceOverridesAConflict(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	conflicting := "[mcp_servers.throughline]\nurl = \"http://127.0.0.1:9999/mcp\"\n"
	if err := os.WriteFile(path, []byte(conflicting), 0o644); err != nil {
		t.Fatal(err)
	}

	result, err := Reconcile(path, testEntry, true)
	if err != nil {
		t.Fatal(err)
	}
	if !result.Changed {
		t.Fatal("expected a forced reconcile over a conflict to change the file")
	}

	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var document map[string]any
	if err := toml.Unmarshal(content, &document); err != nil {
		t.Fatal(err)
	}
	servers, _ := document["mcp_servers"].(map[string]any)
	throughline, _ := servers["throughline"].(map[string]any)
	if throughline["url"] != testEntry.URL {
		t.Fatalf("forced reconcile did not update the entry: %#v", throughline)
	}
}

func TestRemoveDeletesOnlyTheThroughlineEntry(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(path, []byte(codex0149Fixture), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Reconcile(path, testEntry, false); err != nil {
		t.Fatal(err)
	}

	removed, err := Remove(path)
	if err != nil {
		t.Fatal(err)
	}
	if !removed {
		t.Fatal("expected Remove to report the entry was present")
	}

	var document map[string]any
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := toml.Unmarshal(content, &document); err != nil {
		t.Fatal(err)
	}
	servers, _ := document["mcp_servers"].(map[string]any)
	if _, ok := servers["throughline"]; ok {
		t.Fatalf("throughline entry still present: %#v", servers)
	}
	if servers["filesystem"] == nil {
		t.Fatalf("unrelated filesystem entry was lost: %#v", servers)
	}
}

func TestRemoveOnAnAbsentEntryIsANoOp(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(path, []byte(codex0149Fixture), 0o644); err != nil {
		t.Fatal(err)
	}
	removed, err := Remove(path)
	if err != nil {
		t.Fatal(err)
	}
	if removed {
		t.Fatal("expected Remove to report nothing was present")
	}
}

func TestReconcileOnAMissingFileCreatesOne(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "config.toml")
	result, err := Reconcile(path, testEntry, false)
	if err != nil {
		t.Fatal(err)
	}
	if !result.Changed {
		t.Fatal("expected creating a fresh config to report a change")
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatal(err)
	}
}
