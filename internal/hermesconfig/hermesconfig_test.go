package hermesconfig

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"

	"github.com/dennisschroeder/throughline/internal/clientconfig"
)

var testEntry = clientconfig.Entry{
	URL:               "http://127.0.0.1:43121/mcp",
	BearerTokenEnvVar: "THROUGHLINE_MCP_TOKEN",
	Required:          true,
}

// hermes0190Fixture is a representative ~/.hermes/config.yaml as written by Hermes Agent
// 0.19.0: an active profile, a pre-existing unrelated MCP server, and unrelated top-level
// settings. Reconcile must add the throughline entry alongside all of it without disturbing
// any of it, and without depending on server instructions reaching the agent.
const hermes0190Fixture = `active_profile: default
log_level: info
mcp_servers:
  filesystem:
    command: npx
    args:
      - -y
      - "@modelcontextprotocol/server-filesystem"
      - /home/example/projects
profiles:
  default:
    model: hermes-3-405b
    secrets_scope: default
`

func TestReconcileAddsTheEntryAndPreservesUnrelatedConfiguration(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte(hermes0190Fixture), 0o644); err != nil {
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
	if err := yaml.Unmarshal(content, &document); err != nil {
		t.Fatalf("reconciled config.yaml is not valid YAML: %v\n%s", err, content)
	}

	if document["active_profile"] != "default" || document["log_level"] != "info" {
		t.Fatalf("top-level settings were disturbed: %#v", document)
	}
	profiles, _ := document["profiles"].(map[string]any)
	defaultProfile, _ := profiles["default"].(map[string]any)
	if defaultProfile == nil || defaultProfile["model"] != "hermes-3-405b" || defaultProfile["secrets_scope"] != "default" {
		t.Fatalf("profiles were disturbed: %#v", document)
	}
	servers, _ := document["mcp_servers"].(map[string]any)
	if servers == nil || servers["filesystem"] == nil {
		t.Fatalf("the pre-existing filesystem server was lost: %#v", document)
	}
	throughline, ok := servers["throughline"].(map[string]any)
	if !ok {
		t.Fatalf("throughline entry missing: %#v", servers)
	}
	if throughline["url"] != testEntry.URL {
		t.Fatalf("throughline entry = %#v", throughline)
	}
	headers, _ := throughline["headers"].(map[string]any)
	if headers["Authorization"] != "Bearer ${env:THROUGHLINE_MCP_TOKEN}" {
		t.Fatalf("throughline headers = %#v", headers)
	}
}

func TestReconcileNeverWritesTheTokenValueItself(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	if _, err := Reconcile(path, testEntry, false); err != nil {
		t.Fatal(err)
	}
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(content), "Bearer ${env:THROUGHLINE_MCP_TOKEN}") {
		t.Fatalf("expected the env-expansion header form, got: %s", content)
	}
}

func TestReconcileIsIdempotentAndByteStable(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte(hermes0190Fixture), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Reconcile(path, testEntry, false); err != nil {
		t.Fatal(err)
	}
	first, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	// Reconcile repeatedly; map key iteration order is randomized in Go, so this also
	// guards against orderedNode's key sorting regressing into nondeterministic output.
	for i := 0; i < 5; i++ {
		result, err := Reconcile(path, testEntry, false)
		if err != nil {
			t.Fatal(err)
		}
		if result.Changed {
			t.Fatalf("reconcile #%d with the same entry reported a change", i)
		}
		content, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if string(content) != string(first) {
			t.Fatalf("reconcile #%d produced different bytes:\nfirst:\n%s\nnow:\n%s", i, first, content)
		}
	}
}

func TestRemoveDeletesOnlyTheThroughlineEntry(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte(hermes0190Fixture), 0o644); err != nil {
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

	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var document map[string]any
	if err := yaml.Unmarshal(content, &document); err != nil {
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
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte(hermes0190Fixture), 0o644); err != nil {
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

func TestReconcileDiagnosesAConflictWithoutOverwriting(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	conflicting := "mcp_servers:\n  throughline:\n    url: http://127.0.0.1:9999/mcp\n"
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
	path := filepath.Join(t.TempDir(), "config.yaml")
	conflicting := "mcp_servers:\n  throughline:\n    url: http://127.0.0.1:9999/mcp\n"
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
	if err := yaml.Unmarshal(content, &document); err != nil {
		t.Fatal(err)
	}
	servers, _ := document["mcp_servers"].(map[string]any)
	throughline, _ := servers["throughline"].(map[string]any)
	if throughline["url"] != testEntry.URL {
		t.Fatalf("forced reconcile did not update the entry: %#v", throughline)
	}
}

func TestReconcileOnAMissingFileCreatesOne(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "config.yaml")
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
