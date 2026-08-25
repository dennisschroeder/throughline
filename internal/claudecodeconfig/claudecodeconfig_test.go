package claudecodeconfig

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dennisschroeder/throughline/internal/clientconfig"
)

var testEntry = clientconfig.Entry{
	URL:               "http://127.0.0.1:43121/mcp",
	BearerTokenEnvVar: "THROUGHLINE_MCP_TOKEN",
	Required:          true,
}

// claude2231Fixture is a representative ~/.claude.json as written by Claude Code 2.1.231:
// top-level installation/usage state, a pre-existing unrelated user-scoped MCP server, and
// a per-project section. Reconcile must add the throughline entry alongside all of it
// without disturbing any of it.
const claude2231Fixture = `{
  "numStartups": 42,
  "installMethod": "homebrew",
  "theme": "dark",
  "mcpServers": {
    "filesystem": {
      "type": "stdio",
      "command": "npx",
      "args": ["-y", "@modelcontextprotocol/server-filesystem", "/Users/example/projects"]
    }
  },
  "projects": {
    "/Users/example/projects/demo": {
      "allowedTools": ["Bash", "Read"]
    }
  }
}
`

func TestReconcileAddsTheEntryAndPreservesUnrelatedConfiguration(t *testing.T) {
	path := filepath.Join(t.TempDir(), "claude.json")
	if err := os.WriteFile(path, []byte(claude2231Fixture), 0o644); err != nil {
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
	if err := json.Unmarshal(content, &document); err != nil {
		t.Fatalf("reconciled claude.json is not valid JSON: %v\n%s", err, content)
	}

	if document["numStartups"] != float64(42) || document["installMethod"] != "homebrew" || document["theme"] != "dark" {
		t.Fatalf("top-level settings were disturbed: %#v", document)
	}
	projects, _ := document["projects"].(map[string]any)
	if projects == nil || projects["/Users/example/projects/demo"] == nil {
		t.Fatalf("projects section was lost: %#v", document)
	}
	servers, _ := document["mcpServers"].(map[string]any)
	if servers == nil || servers["filesystem"] == nil {
		t.Fatalf("the pre-existing filesystem server was lost: %#v", document)
	}
	throughline, ok := servers["throughline"].(map[string]any)
	if !ok {
		t.Fatalf("throughline entry missing: %#v", servers)
	}
	if throughline["url"] != testEntry.URL || throughline["type"] != "http" {
		t.Fatalf("throughline entry = %#v", throughline)
	}
	headers, _ := throughline["headers"].(map[string]any)
	if headers["Authorization"] != "Bearer ${THROUGHLINE_MCP_TOKEN}" {
		t.Fatalf("throughline headers = %#v", headers)
	}
}

func TestReconcileNeverWritesTheTokenValueItself(t *testing.T) {
	path := filepath.Join(t.TempDir(), "claude.json")
	if _, err := Reconcile(path, testEntry, false); err != nil {
		t.Fatal(err)
	}
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	// The adapter only ever writes the ${VAR} expansion form, never a resolved secret.
	if strings.Contains(string(content), "Bearer ${THROUGHLINE_MCP_TOKEN}") == false {
		t.Fatalf("expected the env-expansion header form, got: %s", content)
	}
}

func TestReconcileIsIdempotent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "claude.json")
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
	path := filepath.Join(t.TempDir(), "claude.json")
	conflicting := `{"mcpServers":{"throughline":{"type":"http","url":"http://127.0.0.1:9999/mcp"}}}`
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
	path := filepath.Join(t.TempDir(), "claude.json")
	conflicting := `{"mcpServers":{"throughline":{"type":"http","url":"http://127.0.0.1:9999/mcp"}}}`
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
	if err := json.Unmarshal(content, &document); err != nil {
		t.Fatal(err)
	}
	servers, _ := document["mcpServers"].(map[string]any)
	throughline, _ := servers["throughline"].(map[string]any)
	if throughline["url"] != testEntry.URL {
		t.Fatalf("forced reconcile did not update the entry: %#v", throughline)
	}
}

func TestRemoveDeletesOnlyTheThroughlineEntry(t *testing.T) {
	path := filepath.Join(t.TempDir(), "claude.json")
	if err := os.WriteFile(path, []byte(claude2231Fixture), 0o644); err != nil {
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
	if err := json.Unmarshal(content, &document); err != nil {
		t.Fatal(err)
	}
	servers, _ := document["mcpServers"].(map[string]any)
	if _, ok := servers["throughline"]; ok {
		t.Fatalf("throughline entry still present: %#v", servers)
	}
	if servers["filesystem"] == nil {
		t.Fatalf("unrelated filesystem entry was lost: %#v", servers)
	}
}

func TestRemoveOnAnAbsentEntryIsANoOp(t *testing.T) {
	path := filepath.Join(t.TempDir(), "claude.json")
	if err := os.WriteFile(path, []byte(claude2231Fixture), 0o644); err != nil {
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
	path := filepath.Join(t.TempDir(), "nested", "claude.json")
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
