package daemon

import (
	"context"
	"errors"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestMain re-executes the test binary itself as a tiny stand-in daemon when
// DAEMON_TEST_HELPER=1 is set, following the standard os/exec testing idiom. This lets
// ProcessManager tests exercise real process spawning, PID tracking, log capture, and
// signal-based shutdown without building or running the actual throughline binary (which
// would need a hermetic registry/credential setup of its own — that is covered separately
// by internal/cli's daemon integration tests).
func TestMain(m *testing.M) {
	if os.Getenv("DAEMON_TEST_HELPER") == "1" {
		runHelperDaemon()
		return
	}
	os.Exit(m.Run())
}

// runHelperDaemon stands in for throughline mcp in ProcessManager's own tests: like the
// real command, its very first action is to acquire the single-endpoint-owner lock and
// exit if that fails, so tests observe the same duplicate-owner behavior production code
// has, without needing a real hermetic registry/credential setup.
func runHelperDaemon() {
	addr, lockPath := "", ""
	for i, argument := range os.Args {
		switch argument {
		case "--addr":
			if i+1 < len(os.Args) {
				addr = os.Args[i+1]
			}
		case "--lock":
			if i+1 < len(os.Args) {
				lockPath = os.Args[i+1]
			}
		}
	}
	lock, err := Acquire(lockPath)
	if err != nil {
		os.Stderr.WriteString(err.Error() + "\n")
		os.Exit(1)
	}
	defer lock.Release()

	mux := http.NewServeMux()
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"ok","version":"helper-test"}`))
	})
	println("helper daemon listening on", addr)
	_ = http.ListenAndServe(addr, mux)
}

func newHelperManager(t *testing.T) *ProcessManager {
	t.Helper()
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	addr := freeLoopbackAddr(t)
	directory := t.TempDir()
	return &ProcessManager{
		LockPath:   filepath.Join(directory, "daemon.lock"),
		PIDPath:    filepath.Join(directory, "daemon.pid"),
		LogPath:    filepath.Join(directory, "daemon.log"),
		Addr:       addr,
		Executable: executable,
		Args:       []string{"--addr", addr, "--lock", filepath.Join(directory, "daemon.lock")},
		ExtraEnv:   []string{"DAEMON_TEST_HELPER=1"},
	}
}

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

func waitForListening(t *testing.T, addr string) {
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
	t.Fatalf("nothing listening on %s in time", addr)
}

func TestProcessManagerStartStatusStopLifecycle(t *testing.T) {
	manager := newHelperManager(t)
	ctx := context.Background()

	status, err := manager.Status(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if status.Running {
		t.Fatal("status reports running before Start")
	}

	if err := manager.Start(ctx); err != nil {
		t.Fatal(err)
	}
	waitForListening(t, manager.Addr)

	status, err = manager.Status(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if !status.Running || status.PID == 0 {
		t.Fatalf("status after Start = %+v, want Running with a PID", status)
	}

	if err := manager.Stop(ctx); err != nil {
		t.Fatal(err)
	}
	status, err = manager.Status(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if status.Running {
		t.Fatal("status still reports running after Stop")
	}
}

func TestProcessManagerStartRefusesADuplicateOwner(t *testing.T) {
	manager := newHelperManager(t)
	ctx := context.Background()
	if err := manager.Start(ctx); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = manager.Stop(ctx) })
	waitForListening(t, manager.Addr)

	// A second manager pointed at the same lock (a fresh throughline mcp invocation, or a
	// second `daemon start`) must be refused deterministically, not silently start a
	// second process or hang.
	second := *manager
	second.Addr = freeLoopbackAddr(t)
	second.Args = []string{"--addr", second.Addr, "--lock", manager.LockPath}
	err := second.Start(ctx)
	var alreadyRunning ErrAlreadyRunning
	if err == nil {
		t.Fatal("second Start against a held lock unexpectedly succeeded")
	}
	if !errors.As(err, &alreadyRunning) {
		t.Fatalf("second Start error = %v, want ErrAlreadyRunning", err)
	}
}

func TestProcessManagerRestartReplacesTheRunningProcess(t *testing.T) {
	manager := newHelperManager(t)
	ctx := context.Background()
	if err := manager.Start(ctx); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = manager.Stop(ctx) })
	waitForListening(t, manager.Addr)

	first, err := manager.Status(ctx)
	if err != nil {
		t.Fatal(err)
	}

	if err := manager.Restart(ctx); err != nil {
		t.Fatal(err)
	}
	waitForListening(t, manager.Addr)

	second, err := manager.Status(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if !second.Running {
		t.Fatal("not running after Restart")
	}
	if second.PID == first.PID {
		t.Fatal("Restart kept the same PID; expected a fresh process")
	}
}

func TestProcessManagerLogsCaptureHelperOutput(t *testing.T) {
	manager := newHelperManager(t)
	ctx := context.Background()
	if err := manager.Start(ctx); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = manager.Stop(ctx) })
	waitForListening(t, manager.Addr)

	deadline := time.Now().Add(2 * time.Second)
	var lines []string
	for time.Now().Before(deadline) {
		var err error
		lines, err = manager.Logs(ctx, 10)
		if err != nil {
			t.Fatal(err)
		}
		if len(lines) > 0 {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if len(lines) == 0 {
		t.Fatal("Logs returned nothing after the helper daemon started")
	}
	found := false
	for _, line := range lines {
		if strings.Contains(line, "helper daemon listening") {
			found = true
		}
	}
	if !found {
		t.Fatalf("Logs = %v, want a line about the helper daemon listening", lines)
	}
}
