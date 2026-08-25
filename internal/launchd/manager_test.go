package launchd

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// fakeRunner records every command it was asked to run and returns a canned response, so
// tests can assert exact launchctl invocations without ever touching the real launchd —
// satisfying the accepted "fixture tests that never mutate the real service" requirement.
type fakeRunner struct {
	calls   [][]string
	outputs map[string]string // joined args -> output
	errs    map[string]error  // joined args -> error
}

func newFakeRunner() *fakeRunner {
	return &fakeRunner{outputs: map[string]string{}, errs: map[string]error{}}
}

func (r *fakeRunner) Run(_ context.Context, name string, args ...string) (string, error) {
	call := append([]string{name}, args...)
	r.calls = append(r.calls, call)
	key := strings.Join(call, " ")
	return r.outputs[key], r.errs[key]
}

func newTestManager(t *testing.T, runner *fakeRunner) *Manager {
	t.Helper()
	directory := t.TempDir()
	return &Manager{
		Label:      "com.throughline.daemon.test",
		PlistPath:  filepath.Join(directory, "LaunchAgents", "com.throughline.daemon.test.plist"),
		Executable: "/usr/local/bin/throughline",
		Addr:       "127.0.0.1:43121",
		LogPath:    filepath.Join(directory, "daemon.log"),
		UID:        501,
		Runner:     runner,
	}
}

func TestStartWritesThePlistAndBootstraps(t *testing.T) {
	runner := newFakeRunner()
	manager := newTestManager(t, runner)

	if err := manager.Start(context.Background()); err != nil {
		t.Fatal(err)
	}

	if len(runner.calls) != 1 {
		t.Fatalf("calls = %v, want exactly one launchctl bootstrap", runner.calls)
	}
	want := []string{"launchctl", "bootstrap", "gui/501", manager.PlistPath}
	if strings.Join(runner.calls[0], " ") != strings.Join(want, " ") {
		t.Fatalf("bootstrap call = %v, want %v", runner.calls[0], want)
	}

	content, err := os.ReadFile(manager.PlistPath)
	if err != nil {
		t.Fatal(err)
	}
	plist := string(content)
	if !strings.Contains(plist, "<key>Label</key>") || !strings.Contains(plist, "com.throughline.daemon.test") {
		t.Fatalf("plist missing label: %s", plist)
	}
	if !strings.Contains(plist, manager.Executable) || !strings.Contains(plist, "--addr") || !strings.Contains(plist, manager.Addr) {
		t.Fatalf("plist missing program arguments: %s", plist)
	}
	if !strings.Contains(plist, manager.LogPath) {
		t.Fatalf("plist missing log redirection: %s", plist)
	}
}

func TestStartWritesAProtectedPlistWithNoTokenOrTarget(t *testing.T) {
	runner := newFakeRunner()
	manager := newTestManager(t, runner)
	if err := manager.Start(context.Background()); err != nil {
		t.Fatal(err)
	}

	info, err := os.Stat(manager.PlistPath)
	if err != nil {
		t.Fatal(err)
	}
	if mode := info.Mode().Perm(); mode != 0o600 {
		t.Fatalf("plist mode = %v, want 0600", mode)
	}

	content, err := os.ReadFile(manager.PlistPath)
	if err != nil {
		t.Fatal(err)
	}
	// Specific markers only: a bare substring like "token" would false-positive against
	// this test's own name embedded in the log path under t.TempDir().
	plist := string(content)
	for _, forbidden := range []string{"Authorization", "Bearer ", "registry.db", ".throughline/config.toml", "credentials"} {
		if strings.Contains(plist, forbidden) {
			t.Fatalf("plist leaked %q: %s", forbidden, content)
		}
	}
}

func TestStopBootsOutTheService(t *testing.T) {
	runner := newFakeRunner()
	manager := newTestManager(t, runner)
	if err := manager.Stop(context.Background()); err != nil {
		t.Fatal(err)
	}
	want := []string{"launchctl", "bootout", "gui/501/com.throughline.daemon.test"}
	if len(runner.calls) != 1 || strings.Join(runner.calls[0], " ") != strings.Join(want, " ") {
		t.Fatalf("calls = %v, want [%v]", runner.calls, want)
	}
}

func TestRestartKickstartsTheService(t *testing.T) {
	runner := newFakeRunner()
	manager := newTestManager(t, runner)
	if err := manager.Restart(context.Background()); err != nil {
		t.Fatal(err)
	}
	want := []string{"launchctl", "kickstart", "-k", "gui/501/com.throughline.daemon.test"}
	if len(runner.calls) != 1 || strings.Join(runner.calls[0], " ") != strings.Join(want, " ") {
		t.Fatalf("calls = %v, want [%v]", runner.calls, want)
	}
}

func TestStatusParsesRunningStateAndPID(t *testing.T) {
	runner := newFakeRunner()
	manager := newTestManager(t, runner)
	manager.CheckHealth = func(context.Context) (string, error) { return "v1.2.3", nil }
	key := strings.Join([]string{"launchctl", "print", "gui/501/com.throughline.daemon.test"}, " ")
	runner.outputs[key] = "gui/501/com.throughline.daemon.test = {\n\tactive count = 1\n\tstate = running\n\tpid = 4242\n}\n"

	status, err := manager.Status(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !status.Running || status.PID != 4242 {
		t.Fatalf("status = %+v, want Running=true PID=4242", status)
	}
	if status.Version != "v1.2.3" {
		t.Fatalf("status.Version = %q, want v1.2.3", status.Version)
	}
}

func TestStatusReportsNotRunningWhenTheServiceIsNotLoaded(t *testing.T) {
	runner := newFakeRunner()
	manager := newTestManager(t, runner)
	key := strings.Join([]string{"launchctl", "print", "gui/501/com.throughline.daemon.test"}, " ")
	runner.errs[key] = errNotFound

	status, err := manager.Status(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if status.Running {
		t.Fatalf("status = %+v, want Running=false", status)
	}
}

func TestLogsReadsTheRedirectedOutputFile(t *testing.T) {
	runner := newFakeRunner()
	manager := newTestManager(t, runner)
	if err := os.MkdirAll(filepath.Dir(manager.LogPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(manager.LogPath, []byte("line one\nline two\nline three\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	lines, err := manager.Logs(context.Background(), 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(lines) != 2 || lines[0] != "line two" || lines[1] != "line three" {
		t.Fatalf("lines = %v, want the last two", lines)
	}
}

var errNotFound = &fakeError{"Could not find service \"com.throughline.daemon.test\" in domain for gui/501"}

type fakeError struct{ message string }

func (e *fakeError) Error() string { return e.message }
