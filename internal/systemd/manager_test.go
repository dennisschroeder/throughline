package systemd

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// fakeRunner records every command it was asked to run and returns a canned response, so
// tests can assert exact systemctl invocations without ever touching the real systemd —
// satisfying the accepted "fixture tests that never mutate the real service" requirement.
type fakeRunner struct {
	calls   [][]string
	outputs map[string]string
	errs    map[string]error
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
		UnitName:   "throughline-daemon.test.service",
		UnitPath:   filepath.Join(directory, "systemd", "user", "throughline-daemon.test.service"),
		Executable: "/usr/local/bin/throughline",
		Addr:       "127.0.0.1:43121",
		LogPath:    filepath.Join(directory, "daemon.log"),
		Runner:     runner,
	}
}

func TestStartWritesTheUnitReloadsAndStarts(t *testing.T) {
	runner := newFakeRunner()
	manager := newTestManager(t, runner)

	if err := manager.Start(context.Background()); err != nil {
		t.Fatal(err)
	}

	if len(runner.calls) != 2 {
		t.Fatalf("calls = %v, want daemon-reload then start", runner.calls)
	}
	if strings.Join(runner.calls[0], " ") != "systemctl --user daemon-reload" {
		t.Fatalf("first call = %v, want daemon-reload", runner.calls[0])
	}
	wantStart := []string{"systemctl", "--user", "start", "throughline-daemon.test.service"}
	if strings.Join(runner.calls[1], " ") != strings.Join(wantStart, " ") {
		t.Fatalf("second call = %v, want %v", runner.calls[1], wantStart)
	}

	content, err := os.ReadFile(manager.UnitPath)
	if err != nil {
		t.Fatal(err)
	}
	unit := string(content)
	if !strings.Contains(unit, "ExecStart="+manager.Executable+" mcp --addr "+manager.Addr) {
		t.Fatalf("unit missing ExecStart: %s", unit)
	}
	if !strings.Contains(unit, manager.LogPath) {
		t.Fatalf("unit missing log redirection: %s", unit)
	}
}

func TestStartWritesAProtectedUnitWithNoTokenOrTarget(t *testing.T) {
	runner := newFakeRunner()
	manager := newTestManager(t, runner)
	if err := manager.Start(context.Background()); err != nil {
		t.Fatal(err)
	}

	info, err := os.Stat(manager.UnitPath)
	if err != nil {
		t.Fatal(err)
	}
	if mode := info.Mode().Perm(); mode != 0o600 {
		t.Fatalf("unit mode = %v, want 0600", mode)
	}

	content, err := os.ReadFile(manager.UnitPath)
	if err != nil {
		t.Fatal(err)
	}
	unit := string(content)
	for _, forbidden := range []string{"Authorization", "Bearer ", "registry.db", ".throughline/config.toml", "credentials"} {
		if strings.Contains(unit, forbidden) {
			t.Fatalf("unit leaked %q: %s", forbidden, content)
		}
	}
}

func TestStopStopsTheUnit(t *testing.T) {
	runner := newFakeRunner()
	manager := newTestManager(t, runner)
	if err := manager.Stop(context.Background()); err != nil {
		t.Fatal(err)
	}
	want := []string{"systemctl", "--user", "stop", "throughline-daemon.test.service"}
	if len(runner.calls) != 1 || strings.Join(runner.calls[0], " ") != strings.Join(want, " ") {
		t.Fatalf("calls = %v, want [%v]", runner.calls, want)
	}
}

func TestRestartRestartsTheUnit(t *testing.T) {
	runner := newFakeRunner()
	manager := newTestManager(t, runner)
	if err := manager.Restart(context.Background()); err != nil {
		t.Fatal(err)
	}
	want := []string{"systemctl", "--user", "restart", "throughline-daemon.test.service"}
	if len(runner.calls) != 1 || strings.Join(runner.calls[0], " ") != strings.Join(want, " ") {
		t.Fatalf("calls = %v, want [%v]", runner.calls, want)
	}
}

func TestStatusParsesActiveStateAndMainPID(t *testing.T) {
	runner := newFakeRunner()
	manager := newTestManager(t, runner)
	manager.CheckHealth = func(context.Context) (string, error) { return "v1.2.3", nil }
	key := strings.Join([]string{"systemctl", "--user", "show", "throughline-daemon.test.service", "--property=ActiveState,MainPID"}, " ")
	runner.outputs[key] = "ActiveState=active\nMainPID=4242\n"

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

func TestStatusReportsNotRunningWhenInactive(t *testing.T) {
	runner := newFakeRunner()
	manager := newTestManager(t, runner)
	key := strings.Join([]string{"systemctl", "--user", "show", "throughline-daemon.test.service", "--property=ActiveState,MainPID"}, " ")
	runner.outputs[key] = "ActiveState=inactive\nMainPID=0\n"

	status, err := manager.Status(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if status.Running {
		t.Fatalf("status = %+v, want Running=false", status)
	}
}

func TestStatusReportsNotRunningWhenTheUnitCannotBeFound(t *testing.T) {
	runner := newFakeRunner()
	manager := newTestManager(t, runner)
	key := strings.Join([]string{"systemctl", "--user", "show", "throughline-daemon.test.service", "--property=ActiveState,MainPID"}, " ")
	runner.errs[key] = errUnitNotFound

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

var errUnitNotFound = &fakeError{"Unit throughline-daemon.test.service could not be found."}

type fakeError struct{ message string }

func (e *fakeError) Error() string { return e.message }
