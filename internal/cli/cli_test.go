package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/dennisschroeder/throughline/internal/config"
	"github.com/dennisschroeder/throughline/internal/daemon"
	"github.com/dennisschroeder/throughline/internal/registry"
)

func withTestCredential(t *testing.T) {
	t.Helper()
	previous := credentialPathForTesting
	credentialPathForTesting = filepath.Join(t.TempDir(), "credentials")
	t.Cleanup(func() { credentialPathForTesting = previous })
}

// withTestClientConfigs redirects the three managed harness adapters at files inside a
// temp directory, so setup/uninstall tests never touch a real ~/.codex, ~/.claude.json, or
// ~/.hermes. It returns the config paths keyed by adapter name for assertions. Every
// harness is marked "detected" (detectPath == configPath, which setup/uninstall check with
// os.Stat) only once the test itself creates that file, matching production's "only touch
// an installed harness" behavior.
func withTestClientConfigs(t *testing.T) map[string]string {
	t.Helper()
	previous := clientConfigPathsForTesting
	directory := t.TempDir()
	paths := map[string]string{
		"codex":       filepath.Join(directory, "codex.toml"),
		"claude-code": filepath.Join(directory, "claude.json"),
		"hermes":      filepath.Join(directory, "hermes.yaml"),
	}
	clientConfigPathsForTesting = map[string][2]string{
		"codex":       {paths["codex"], paths["codex"]},
		"claude-code": {paths["claude-code"], paths["claude-code"]},
		"hermes":      {paths["hermes"], paths["hermes"]},
	}
	t.Cleanup(func() { clientConfigPathsForTesting = previous })
	return paths
}

// fakeCLIServiceManager is a minimal daemon.ServiceManager double for setup/uninstall tests
// that must not touch a real launchd/systemd instance.
type fakeCLIServiceManager struct {
	starts, stops int
}

func (m *fakeCLIServiceManager) Start(context.Context) error   { m.starts++; return nil }
func (m *fakeCLIServiceManager) Stop(context.Context) error    { m.stops++; return nil }
func (m *fakeCLIServiceManager) Restart(context.Context) error { return nil }
func (m *fakeCLIServiceManager) Status(context.Context) (daemon.Status, error) {
	return daemon.Status{Running: m.starts > m.stops}, nil
}
func (m *fakeCLIServiceManager) Logs(context.Context, int) ([]string, error) { return nil, nil }

func withTestServiceManager(t *testing.T) *fakeCLIServiceManager {
	t.Helper()
	previous := serviceManagerForTesting
	manager := &fakeCLIServiceManager{}
	serviceManagerForTesting = manager
	t.Cleanup(func() { serviceManagerForTesting = previous })
	return manager
}

// freeLoopbackAddr finds an available loopback port for a test to bind runMCP to.
func freeLoopbackAddr(t *testing.T) string {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := listener.Addr().String()
	if err := listener.Close(); err != nil {
		t.Fatal(err)
	}
	return addr
}

func withTestRegistry(t *testing.T) {
	t.Helper()
	previous := registryPathForTesting
	registryPathForTesting = filepath.Join(t.TempDir(), "registry.db")
	t.Cleanup(func() { registryPathForTesting = previous })
}

func TestInitCanRunTwice(t *testing.T) {
	withTestRegistry(t)
	root := t.TempDir()
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	if code := Run(context.Background(), []string{"init", root}, &stdout, &stderr); code != 0 {
		t.Fatalf("first init exited %d: %s", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "initialized Throughline workspace") {
		t.Fatalf("first init output = %q", stdout.String())
	}
	firstWorkspace, err := config.Load(root)
	if err != nil {
		t.Fatal(err)
	}
	stdout.Reset()
	stderr.Reset()
	if code := Run(context.Background(), []string{"init", root}, &stdout, &stderr); code != 0 {
		t.Fatalf("second init exited %d: %s", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "reopened Throughline workspace") {
		t.Fatalf("second init output = %q", stdout.String())
	}
	secondWorkspace, err := config.Load(root)
	if err != nil {
		t.Fatal(err)
	}
	if firstWorkspace.Config.WorkspaceID != secondWorkspace.Config.WorkspaceID {
		t.Fatalf("workspace_id changed across idempotent init: %q -> %q", firstWorkspace.Config.WorkspaceID, secondWorkspace.Config.WorkspaceID)
	}

	for _, path := range []string{
		filepath.Join(root, config.DirectoryName, config.ConfigFileName),
		filepath.Join(root, config.DirectoryName, config.DefaultDatabasePath),
	} {
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("expected initialized file %q: %v", path, err)
		}
	}
}

// TestReadyAndShowInspectExecutionGraph proves ready/show are daemon clients, not storage
// openers: it seeds the execution graph entirely through the running daemon's MCP endpoint
// (never opening the workspace database directly from the test) and only then exercises the
// ready/show commands against that same daemon.
func TestReadyAndShowInspectExecutionGraph(t *testing.T) {
	withTestRegistry(t)
	withTestCredential(t)
	ctx := context.Background()
	root := t.TempDir()
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	if code := Run(ctx, []string{"init", root}, &stdout, &stderr); code != 0 {
		t.Fatalf("init exited %d: %s", code, stderr.String())
	}
	workspace, err := config.Load(root)
	if err != nil {
		t.Fatal(err)
	}

	addr := freeLoopbackAddr(t)
	serveCtx, cancel := context.WithCancel(ctx)
	serveErrs := make(chan error, 1)
	var serveOut, serveErr bytes.Buffer
	go func() { serveErrs <- runMCP(serveCtx, []string{"--addr", addr}, &serveOut, &serveErr) }()
	t.Cleanup(func() {
		cancel()
		if err := <-serveErrs; err != nil {
			t.Errorf("runMCP returned an error after shutdown: %v", err)
		}
	})
	waitForHealth(t, addr)

	call := func(name string, arguments map[string]any) map[string]any {
		t.Helper()
		if _, ok := arguments["workspace_id"]; !ok {
			arguments["workspace_id"] = workspace.Config.WorkspaceID
		}
		result, err := callDaemonTool(ctx, addr, name, arguments)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		payload, _ := result.(map[string]any)
		return payload
	}

	call("register_actor", map[string]any{"actor_id": "human:reviewer", "kind": "human", "display_name": "Reviewer", "idempotency_key": "register-reviewer"})
	call("register_actor", map[string]any{"actor_id": "agent:researcher", "kind": "agent", "display_name": "Researcher", "idempotency_key": "register-researcher"})
	objective := call("create_objective", map[string]any{
		"actor_id": "human:owner", "idempotency_key": "create-objective-cli",
		"key": "OBJ-CLI", "title": "Inspect research work", "desired_outcome": "Ready work is visible.", "phase": "planning",
	})
	plan := call("propose_plan", map[string]any{
		"objective_id": objective["id"], "actor_id": "agent:planner", "idempotency_key": "propose-plan-cli", "title": "Research plan", "revision": 1,
		"items": []any{map[string]any{
			"client_ref": "research", "key": "TH-CLI", "title": "Prepare the research dossier", "kind": "research",
			"priority": "high", "estimated_scope": "small", "execution_policy": "autonomous_with_report", "required_actor_kind": "agent",
		}},
	})
	planDetail, _ := plan["plan"].(map[string]any)
	call("review_plan", map[string]any{"plan_id": planDetail["id"], "actor_id": "human:reviewer", "idempotency_key": "review-plan-cli", "decision": "approved", "reason": "Ready for execution.", "expected_version": 1})
	call("transition_objective", map[string]any{"objective_id": objective["id"], "actor_id": "human:reviewer", "idempotency_key": "transition-objective-cli", "target_phase": "execution", "reason": "Begin work.", "expected_version": 1})

	items := call("list_items", map[string]any{"objective_id": objective["id"]})["items"].([]any)
	firstWorkItem, _ := items[0].(map[string]any)["work_item"].(map[string]any)
	itemContext := call("get_item", map[string]any{"id": firstWorkItem["id"]})
	workItemContext, _ := itemContext["work_item"].(map[string]any)
	readyItem := call("transition_item", map[string]any{
		"id": firstWorkItem["id"], "target_status": "ready", "actor_id": "human:reviewer", "reason": "Queue work.",
		"expected_version": workItemContext["version"], "idempotency_key": "ready-cli-item",
	})
	itemID, _ := readyItem["id"].(string)

	stdout.Reset()
	stderr.Reset()
	if code := Run(ctx, []string{"ready", "--addr", addr, "--actor", "agent:researcher", root}, &stdout, &stderr); code != 0 {
		t.Fatalf("ready exited %d: %s", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "TH-CLI\tPrepare the research dossier") {
		t.Fatalf("ready output = %q", stdout.String())
	}
	stdout.Reset()
	stderr.Reset()
	if code := Run(ctx, []string{"show", "--addr", addr, itemID, root}, &stdout, &stderr); code != 0 {
		t.Fatalf("show exited %d: %s", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), `"key": "TH-CLI"`) || !strings.Contains(stdout.String(), `"execution_status": "ready"`) {
		t.Fatalf("show output = %q", stdout.String())
	}
}

func TestInitReconcilesAMovedWorkspaceUnderTheSameIdentity(t *testing.T) {
	withTestRegistry(t)
	ctx := context.Background()
	parent := t.TempDir()
	original := filepath.Join(parent, "original")
	if err := os.MkdirAll(original, 0o755); err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	if code := Run(ctx, []string{"init", original}, &stdout, &stderr); code != 0 {
		t.Fatalf("init exited %d: %s", code, stderr.String())
	}
	workspace, err := config.Load(original)
	if err != nil {
		t.Fatal(err)
	}

	moved := filepath.Join(parent, "moved")
	if err := os.Rename(original, moved); err != nil {
		t.Fatal(err)
	}

	stdout.Reset()
	stderr.Reset()
	if code := Run(ctx, []string{"init", moved}, &stdout, &stderr); code != 0 {
		t.Fatalf("init at moved location exited %d: %s", code, stderr.String())
	}
	movedWorkspace, err := config.Load(moved)
	if err != nil {
		t.Fatal(err)
	}
	if movedWorkspace.Config.WorkspaceID != workspace.Config.WorkspaceID {
		t.Fatalf("workspace_id changed across a move: %q -> %q", workspace.Config.WorkspaceID, movedWorkspace.Config.WorkspaceID)
	}
}

func TestInitOnACopyWithoutForkFailsClosedWithIdentityConflict(t *testing.T) {
	withTestRegistry(t)
	ctx := context.Background()
	parent := t.TempDir()
	original := filepath.Join(parent, "original")
	if err := os.MkdirAll(original, 0o755); err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	if code := Run(ctx, []string{"init", original}, &stdout, &stderr); code != 0 {
		t.Fatalf("init exited %d: %s", code, stderr.String())
	}

	copyDir := filepath.Join(parent, "copy")
	copyDirectoryTree(t, original, copyDir)

	stdout.Reset()
	stderr.Reset()
	code := Run(ctx, []string{"init", copyDir}, &stdout, &stderr)
	if code == 0 {
		t.Fatalf("init on a copy without --fork unexpectedly succeeded: %s", stdout.String())
	}
	if !strings.Contains(stderr.String(), "workspace_identity_conflict") {
		t.Fatalf("init on a copy error = %q, want workspace_identity_conflict", stderr.String())
	}
}

func TestInitForkGivesACopyAnIndependentIdentity(t *testing.T) {
	withTestRegistry(t)
	ctx := context.Background()
	parent := t.TempDir()
	original := filepath.Join(parent, "original")
	if err := os.MkdirAll(original, 0o755); err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	if code := Run(ctx, []string{"init", original}, &stdout, &stderr); code != 0 {
		t.Fatalf("init exited %d: %s", code, stderr.String())
	}
	originalWorkspace, err := config.Load(original)
	if err != nil {
		t.Fatal(err)
	}

	copyDir := filepath.Join(parent, "copy")
	copyDirectoryTree(t, original, copyDir)

	stdout.Reset()
	stderr.Reset()
	if code := Run(ctx, []string{"init", "--fork", copyDir}, &stdout, &stderr); code != 0 {
		t.Fatalf("init --fork exited %d: %s", code, stderr.String())
	}
	forkedWorkspace, err := config.Load(copyDir)
	if err != nil {
		t.Fatal(err)
	}
	if forkedWorkspace.Config.WorkspaceID == originalWorkspace.Config.WorkspaceID {
		t.Fatal("forked workspace kept the source workspace_id")
	}

	// The original is still independently routable at its own root.
	stdout.Reset()
	stderr.Reset()
	if code := Run(ctx, []string{"init", original}, &stdout, &stderr); code != 0 {
		t.Fatalf("re-init of the source after fork exited %d: %s", code, stderr.String())
	}
}

func TestUnregisterRemovesRoutingAuthorityButKeepsWorkspaceData(t *testing.T) {
	withTestRegistry(t)
	ctx := context.Background()
	root := t.TempDir()
	var stdout, stderr bytes.Buffer
	if code := Run(ctx, []string{"init", root}, &stdout, &stderr); code != 0 {
		t.Fatalf("init exited %d: %s", code, stderr.String())
	}
	workspace, err := config.Load(root)
	if err != nil {
		t.Fatal(err)
	}

	stdout.Reset()
	stderr.Reset()
	if code := Run(ctx, []string{"unregister", root}, &stdout, &stderr); code != 0 {
		t.Fatalf("unregister exited %d: %s", code, stderr.String())
	}
	if _, err := os.Stat(filepath.Join(root, config.DirectoryName, config.DefaultDatabasePath)); err != nil {
		t.Fatalf("expected workspace database to survive unregister: %v", err)
	}

	registryHandle, err := openRegistry(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer registryHandle.Close()
	if _, err := registryHandle.Lookup(ctx, workspace.Config.WorkspaceID); !errors.Is(err, registry.ErrWorkspaceNotFound) {
		t.Fatalf("lookup after unregister = %v, want ErrWorkspaceNotFound", err)
	}
}

func TestInitRejectsALegacyWorkspaceConfig(t *testing.T) {
	withTestRegistry(t)
	root := t.TempDir()
	legacyDirectory := filepath.Join(root, config.DirectoryName)
	if err := os.MkdirAll(legacyDirectory, 0o755); err != nil {
		t.Fatal(err)
	}
	legacyConfig := "schema_version = 1\ndatabase_path = 'throughline.db'\nitem_key_prefix = 'TH'\n"
	if err := os.WriteFile(filepath.Join(legacyDirectory, config.ConfigFileName), []byte(legacyConfig), 0o644); err != nil {
		t.Fatal(err)
	}

	var stdout, stderr bytes.Buffer
	code := Run(context.Background(), []string{"init", root}, &stdout, &stderr)
	if code == 0 {
		t.Fatalf("init on a legacy workspace unexpectedly succeeded: %s", stdout.String())
	}
	if !strings.Contains(stderr.String(), "legacy_workspace_unsupported") {
		t.Fatalf("init on a legacy workspace error = %q, want legacy_workspace_unsupported", stderr.String())
	}
}

// TestDomainCommandsRejectALegacyWorkspaceConfig confirms every legacy-facing command
// fails closed with the same stable code, not just throughline init.
func TestDomainCommandsRejectALegacyWorkspaceConfig(t *testing.T) {
	withTestRegistry(t)
	withTestCredential(t)
	root := t.TempDir()
	legacyDirectory := filepath.Join(root, config.DirectoryName)
	if err := os.MkdirAll(legacyDirectory, 0o755); err != nil {
		t.Fatal(err)
	}
	legacyConfig := "schema_version = 1\ndatabase_path = 'throughline.db'\nitem_key_prefix = 'TH'\n"
	if err := os.WriteFile(filepath.Join(legacyDirectory, config.ConfigFileName), []byte(legacyConfig), 0o644); err != nil {
		t.Fatal(err)
	}

	for _, args := range [][]string{
		{"ready", "--actor", "agent:test", root},
		{"show", "some-id", root},
	} {
		var stdout, stderr bytes.Buffer
		code := Run(context.Background(), args, &stdout, &stderr)
		if code == 0 {
			t.Fatalf("%v on a legacy workspace unexpectedly succeeded: %s", args, stdout.String())
		}
		if !strings.Contains(stderr.String(), "legacy_workspace_unsupported") {
			t.Fatalf("%v error = %q, want legacy_workspace_unsupported", args, stderr.String())
		}
	}

	previousWD, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(root); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(previousWD) })
	var doctorOut, doctorErr bytes.Buffer
	if code := Run(context.Background(), []string{"doctor"}, &doctorOut, &doctorErr); code != 0 {
		t.Fatalf("doctor exited %d: %s", code, doctorErr.String())
	}
	if !strings.Contains(doctorOut.String(), "legacy_workspace_unsupported") {
		t.Fatalf("doctor output = %q, want legacy_workspace_unsupported", doctorOut.String())
	}
}

func copyDirectoryTree(t *testing.T, source, destination string) {
	t.Helper()
	if err := filepath.Walk(source, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		relative, err := filepath.Rel(source, path)
		if err != nil {
			return err
		}
		target := filepath.Join(destination, relative)
		if info.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		content, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		return os.WriteFile(target, content, info.Mode())
	}); err != nil {
		t.Fatal(err)
	}
}

func TestMCPServesAuthenticatedHealthAndDoctorAndDaemonStatusAgree(t *testing.T) {
	withTestRegistry(t)
	withTestCredential(t)
	root := t.TempDir()

	var initOut, initErr bytes.Buffer
	if code := Run(context.Background(), []string{"init", root}, &initOut, &initErr); code != 0 {
		t.Fatalf("init exited %d: %s", code, initErr.String())
	}

	addr := freeLoopbackAddr(t)
	ctx, cancel := context.WithCancel(context.Background())
	serveErrs := make(chan error, 1)
	var serveOut, serveErrBuf bytes.Buffer
	go func() {
		serveErrs <- runMCP(ctx, []string{"--addr", addr}, &serveOut, &serveErrBuf)
	}()
	t.Cleanup(func() {
		cancel()
		if err := <-serveErrs; err != nil {
			t.Errorf("runMCP returned an error after shutdown: %v", err)
		}
	})
	waitForHealth(t, addr)

	workspace, err := config.Load(root)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Getwd(); err != nil {
		t.Fatal(err)
	}
	previousWD, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(root); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(previousWD) })

	var doctorOut, doctorErr bytes.Buffer
	if code := Run(context.Background(), []string{"doctor", "--addr", addr}, &doctorOut, &doctorErr); code != 0 {
		t.Fatalf("doctor exited %d: %s", code, doctorErr.String())
	}
	if !strings.Contains(doctorOut.String(), "workspace_id="+workspace.Config.WorkspaceID) {
		t.Fatalf("doctor output missing workspace_id: %s", doctorOut.String())
	}
	if !strings.Contains(doctorOut.String(), "registry: active") {
		t.Fatalf("doctor output missing registry agreement: %s", doctorOut.String())
	}
	if !strings.Contains(doctorOut.String(), "daemon: reachable") {
		t.Fatalf("doctor output missing daemon reachability: %s", doctorOut.String())
	}

	var statusOut, statusErr bytes.Buffer
	if code := Run(context.Background(), []string{"daemon", "status", "--addr", addr, "--json"}, &statusOut, &statusErr); code != 0 {
		t.Fatalf("daemon status exited %d: %s", code, statusErr.String())
	}
	var status struct {
		Reachable bool   `json:"reachable"`
		Version   string `json:"version"`
	}
	if err := json.Unmarshal(statusOut.Bytes(), &status); err != nil {
		t.Fatalf("daemon status output = %q: %v", statusOut.String(), err)
	}
	if !status.Reachable {
		t.Fatalf("daemon status reachable = false: %s", statusOut.String())
	}
}

func TestSetupConfiguresDetectedHarnessesAndStartsTheManagedService(t *testing.T) {
	withTestCredential(t)
	paths := withTestClientConfigs(t)
	manager := withTestServiceManager(t)

	// Only codex and hermes "are installed"; claude-code is not, and setup must skip it
	// without creating a file for it.
	if err := os.WriteFile(paths["codex"], []byte("model = \"gpt-5-codex\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(paths["hermes"], []byte("log_level: info\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	var stdout, stderr bytes.Buffer
	if code := Run(context.Background(), []string{"setup"}, &stdout, &stderr); code != 0 {
		t.Fatalf("setup exited %d: %s", code, stderr.String())
	}
	if manager.starts != 1 {
		t.Fatalf("service starts = %d, want 1", manager.starts)
	}
	if !strings.Contains(stdout.String(), "codex: configured") || !strings.Contains(stdout.String(), "hermes: configured") {
		t.Fatalf("setup output = %q", stdout.String())
	}
	if !strings.Contains(stdout.String(), "claude-code: not detected, skipped") {
		t.Fatalf("setup output = %q, want claude-code skipped", stdout.String())
	}
	if _, err := os.Stat(paths["claude-code"]); !os.IsNotExist(err) {
		t.Fatal("setup created a config file for an undetected harness")
	}

	codexContent, err := os.ReadFile(paths["codex"])
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(codexContent), "mcp_servers.throughline") && !strings.Contains(string(codexContent), "[mcp_servers.throughline]") {
		t.Fatalf("codex config missing the throughline entry: %s", codexContent)
	}
	if !strings.Contains(string(codexContent), "gpt-5-codex") {
		t.Fatalf("setup disturbed unrelated codex settings: %s", codexContent)
	}
}

func TestSetupRollsBackEveryTargetWhenTheServiceFailsToStart(t *testing.T) {
	withTestCredential(t)
	paths := withTestClientConfigs(t)
	failing := &failingStartManager{}
	previous := serviceManagerForTesting
	serviceManagerForTesting = failing
	t.Cleanup(func() { serviceManagerForTesting = previous })

	original := "model = \"gpt-5-codex\"\n"
	if err := os.WriteFile(paths["codex"], []byte(original), 0o644); err != nil {
		t.Fatal(err)
	}

	var stdout, stderr bytes.Buffer
	code := Run(context.Background(), []string{"setup"}, &stdout, &stderr)
	if code == 0 {
		t.Fatal("expected setup to fail when the service cannot start")
	}
	content, err := os.ReadFile(paths["codex"])
	if err != nil {
		t.Fatal(err)
	}
	if string(content) != original {
		t.Fatalf("codex config after a failed setup = %q, want the original restored", content)
	}
}

type failingStartManager struct{}

func (failingStartManager) Start(context.Context) error   { return errors.New("simulated failure") }
func (failingStartManager) Stop(context.Context) error    { return nil }
func (failingStartManager) Restart(context.Context) error { return nil }
func (failingStartManager) Status(context.Context) (daemon.Status, error) {
	return daemon.Status{}, nil
}
func (failingStartManager) Logs(context.Context, int) ([]string, error) { return nil, nil }

func TestUninstallStopsTheServiceRemovesEntriesAndPreservesWorkspaceData(t *testing.T) {
	withTestRegistry(t)
	withTestCredential(t)
	paths := withTestClientConfigs(t)
	manager := withTestServiceManager(t)

	root := t.TempDir()
	var initOut, initErr bytes.Buffer
	if code := Run(context.Background(), []string{"init", root}, &initOut, &initErr); code != 0 {
		t.Fatalf("init exited %d: %s", code, initErr.String())
	}
	databasePath := filepath.Join(root, config.DirectoryName, config.DefaultDatabasePath)

	if err := os.WriteFile(paths["codex"], []byte("model = \"gpt-5-codex\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	var setupOut, setupErr bytes.Buffer
	if code := Run(context.Background(), []string{"setup"}, &setupOut, &setupErr); code != 0 {
		t.Fatalf("setup exited %d: %s", code, setupErr.String())
	}

	var stdout, stderr bytes.Buffer
	if code := Run(context.Background(), []string{"uninstall"}, &stdout, &stderr); code != 0 {
		t.Fatalf("uninstall exited %d: %s", code, stderr.String())
	}
	if manager.stops != 1 {
		t.Fatalf("service stops = %d, want 1", manager.stops)
	}
	if !strings.Contains(stdout.String(), "codex: removed the throughline entry") {
		t.Fatalf("uninstall output = %q", stdout.String())
	}
	codexContent, err := os.ReadFile(paths["codex"])
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(codexContent), "throughline") {
		t.Fatalf("codex config still references throughline after uninstall: %s", codexContent)
	}
	if !strings.Contains(string(codexContent), "gpt-5-codex") {
		t.Fatalf("uninstall disturbed unrelated codex settings: %s", codexContent)
	}

	if _, err := os.Stat(databasePath); err != nil {
		t.Fatalf("uninstall must preserve workspace data: %v", err)
	}
	registryPath, err := registryPath()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(registryPath); err != nil {
		t.Fatalf("uninstall must preserve the registry: %v", err)
	}
}

func TestMCPRejectsANonLoopbackAddr(t *testing.T) {
	withTestRegistry(t)
	withTestCredential(t)
	var stdout, stderr bytes.Buffer
	code := Run(context.Background(), []string{"mcp", "--addr", "0.0.0.0:43121"}, &stdout, &stderr)
	if code == 0 {
		t.Fatal("mcp on a non-loopback address unexpectedly succeeded")
	}
	if !strings.Contains(stderr.String(), "loopback") {
		t.Fatalf("non-loopback error = %q", stderr.String())
	}
}

func TestDoctorReportsAnUnreachableDaemonWithoutFailing(t *testing.T) {
	withTestRegistry(t)
	withTestCredential(t)
	root := t.TempDir()
	var initOut, initErr bytes.Buffer
	if code := Run(context.Background(), []string{"init", root}, &initOut, &initErr); code != 0 {
		t.Fatalf("init exited %d: %s", code, initErr.String())
	}
	previousWD, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(root); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(previousWD) })

	addr := freeLoopbackAddr(t) // nothing is listening here
	var stdout, stderr bytes.Buffer
	if code := Run(context.Background(), []string{"doctor", "--addr", addr}, &stdout, &stderr); code != 0 {
		t.Fatalf("doctor exited %d: %s", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "daemon: unreachable") {
		t.Fatalf("doctor output = %q, want unreachable daemon", stdout.String())
	}
}

// waitForHealth polls the daemon's /health endpoint (unauthenticated request is enough to
// prove the listener is up; 401 counts as "serving") until it responds or the deadline
// passes.
func waitForHealth(t *testing.T, addr string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		conn, err := net.DialTimeout("tcp", addr, 100*time.Millisecond)
		if err == nil {
			_ = conn.Close()
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("daemon at %s did not start listening in time", addr)
}

func TestVersionCommandUsesDevelopmentFallbackWhenUninjected(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	if code := Run(context.Background(), []string{"version"}, &stdout, &stderr); code != 0 {
		t.Fatalf("version exited %d: %s", code, stderr.String())
	}
	output := strings.TrimSpace(stdout.String())
	if output == "" {
		t.Fatal("version output is empty")
	}
	if !strings.Contains(output, "throughline version") {
		t.Fatalf("version output = %q", output)
	}
	if strings.Contains(output, "()") || strings.Contains(output, "commit , built") {
		t.Fatalf("version output has empty fields: %q", output)
	}
}

func TestVersionCommandReportsInjectedValuesVerbatim(t *testing.T) {
	previousVersion, previousCommit, previousDate := version, commit, date
	t.Cleanup(func() {
		version, commit, date = previousVersion, previousCommit, previousDate
	})
	version, commit, date = "v0.1.0", "abc123def456", "2026-08-23T10:00:00Z"

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	if code := Run(context.Background(), []string{"version"}, &stdout, &stderr); code != 0 {
		t.Fatalf("version exited %d: %s", code, stderr.String())
	}
	output := stdout.String()
	if !strings.Contains(output, "v0.1.0") || !strings.Contains(output, "abc123def456") || !strings.Contains(output, "2026-08-23T10:00:00Z") {
		t.Fatalf("version output = %q", output)
	}
}

func TestVersionAndDoubleDashVersionFlagProduceIdenticalOutput(t *testing.T) {
	var stdoutVersion bytes.Buffer
	var stderrVersion bytes.Buffer
	if code := Run(context.Background(), []string{"version"}, &stdoutVersion, &stderrVersion); code != 0 {
		t.Fatalf("version exited %d: %s", code, stderrVersion.String())
	}

	var stdoutFlag bytes.Buffer
	var stderrFlag bytes.Buffer
	if code := Run(context.Background(), []string{"--version"}, &stdoutFlag, &stderrFlag); code != 0 {
		t.Fatalf("--version exited %d: %s", code, stderrFlag.String())
	}

	if stdoutVersion.String() != stdoutFlag.String() {
		t.Fatalf("version output %q != --version output %q", stdoutVersion.String(), stdoutFlag.String())
	}
}
