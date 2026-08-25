package daemon

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// Status is the provider-neutral lifecycle snapshot every ServiceManager adapter reports.
type Status struct {
	Running bool
	PID     int
	// Version is populated by an authenticated health probe when the caller supplies one
	// (CheckHealth); it is empty if the process is not running or no probe was configured.
	Version string
}

// ServiceManager is the one seam every daemon-management adapter implements: launchd
// (WR-07) and systemd --user (WR-08) wrap the same OS-level unit/service concepts behind
// this interface. ProcessManager here is the OS-agnostic reference adapter this work item's
// own tests exercise; it is not a substitute for a real supervised service (no auto-start
// on login, no crash-restart) and throughline daemon uses whichever adapter setup installed.
type ServiceManager interface {
	Start(ctx context.Context) error
	Stop(ctx context.Context) error
	Restart(ctx context.Context) error
	Status(ctx context.Context) (Status, error)
	Logs(ctx context.Context, lines int) ([]string, error)
}

// ProcessManager runs the daemon as a plain detached background process, using Lock/Acquire
// as its single-endpoint-owner enforcement and a separate PID file to track and stop the
// process it started. LockPath must be the same path throughline mcp itself acquires via
// Acquire, so a duplicate Start (or a manually launched throughline mcp) is refused
// deterministically before any process is spawned.
type ProcessManager struct {
	LockPath   string
	PIDPath    string
	LogPath    string
	Addr       string
	Executable string // defaults to os.Executable() if empty

	// Args, if set, replaces the default ["mcp", "--addr", Addr] argument list Start
	// passes to Executable — tests use this to run a lightweight stand-in instead of the
	// real throughline binary.
	Args []string

	// ExtraEnv, if set, is appended to the spawned process's environment (which otherwise
	// inherits this process's own). Tests use this to point a stand-in executable at a
	// hermetic mode without touching package-level state shared with the real command.
	ExtraEnv []string

	// CheckHealth, if set, is called by Status to populate Version once the process is
	// confirmed alive; it is expected to hit the daemon's authenticated /health endpoint.
	CheckHealth func(ctx context.Context) (version string, err error)
}

func (m *ProcessManager) args() []string {
	if m.Args != nil {
		return m.Args
	}
	return []string{"mcp", "--addr", m.Addr}
}

func (m *ProcessManager) executable() (string, error) {
	if m.Executable != "" {
		return m.Executable, nil
	}
	return os.Executable()
}

// Start refuses to run if the lock is already held by a live process (ErrAlreadyRunning),
// then spawns `<executable> mcp --addr <addr>` detached from this process's session,
// appending its output to LogPath, and records its PID.
func (m *ProcessManager) Start(ctx context.Context) error {
	lock, err := Acquire(m.LockPath)
	if err != nil {
		return err
	}
	// ProcessManager only uses the lock as a non-blocking pre-flight check here; the
	// spawned child acquires it itself once its own runMCP starts, which is what actually
	// enforces single ownership against every other caller, including one bypassing this
	// manager entirely.
	if err := lock.Release(); err != nil {
		return fmt.Errorf("release daemon lock preflight: %w", err)
	}

	executable, err := m.executable()
	if err != nil {
		return fmt.Errorf("resolve throughline executable: %w", err)
	}
	logFile, err := os.OpenFile(m.LogPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return fmt.Errorf("open daemon log: %w", err)
	}
	defer logFile.Close()

	command := exec.Command(executable, m.args()...)
	command.Stdout = logFile
	command.Stderr = logFile
	command.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if m.ExtraEnv != nil {
		command.Env = append(os.Environ(), m.ExtraEnv...)
	}
	if err := command.Start(); err != nil {
		return fmt.Errorf("start daemon process: %w", err)
	}
	if err := os.WriteFile(m.PIDPath, []byte(strconv.Itoa(command.Process.Pid)), 0o600); err != nil {
		return fmt.Errorf("write daemon pid file: %w", err)
	}
	// Detach: this manager does not wait for or reap the child. A future Stop signals it
	// by PID; if it exits on its own, Status/Stop discover that from the PID's liveness.
	go func() { _ = command.Wait() }()
	return nil
}

// Stop sends SIGTERM to the recorded PID, waits briefly for it to exit, and escalates to
// SIGKILL if it has not. It is a no-op, not an error, if nothing is recorded as running.
func (m *ProcessManager) Stop(ctx context.Context) error {
	pid := readPIDFile(m.PIDPath)
	if pid == 0 || !processAlive(pid) {
		_ = os.Remove(m.PIDPath)
		return nil
	}
	if err := syscall.Kill(pid, syscall.SIGTERM); err != nil && !errors.Is(err, syscall.ESRCH) {
		return fmt.Errorf("signal daemon process: %w", err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if !processAlive(pid) {
			_ = os.Remove(m.PIDPath)
			return nil
		}
		time.Sleep(50 * time.Millisecond)
	}
	if err := syscall.Kill(pid, syscall.SIGKILL); err != nil && !errors.Is(err, syscall.ESRCH) {
		return fmt.Errorf("force-kill daemon process: %w", err)
	}
	_ = os.Remove(m.PIDPath)
	return nil
}

func (m *ProcessManager) Restart(ctx context.Context) error {
	if err := m.Stop(ctx); err != nil {
		return err
	}
	return m.Start(ctx)
}

func (m *ProcessManager) Status(ctx context.Context) (Status, error) {
	pid := readPIDFile(m.PIDPath)
	if pid == 0 || !processAlive(pid) {
		return Status{Running: false}, nil
	}
	status := Status{Running: true, PID: pid}
	if m.CheckHealth != nil {
		if version, err := m.CheckHealth(ctx); err == nil {
			status.Version = version
		}
	}
	return status, nil
}

// Logs returns up to the last n lines of LogPath.
func (m *ProcessManager) Logs(ctx context.Context, n int) ([]string, error) {
	return ReadLogTail(m.LogPath, n)
}

// ReadLogTail returns up to the last n lines of path (or every line if n <= 0), or nil if
// the file does not exist yet. It reads the whole file rather than seeking from the end
// because daemon logs are small structured lines, not a high-volume stream any adapter here
// needs to handle efficiently; launchd and systemd adapters share this so log formatting
// stays consistent regardless of which ServiceManager wrote the file.
func ReadLogTail(path string, n int) ([]string, error) {
	file, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("open daemon log: %w", err)
	}
	defer file.Close()

	var lines []string
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for scanner.Scan() {
		lines = append(lines, scanner.Text())
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("read daemon log: %w", err)
	}
	if n > 0 && len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return lines, nil
}

func readPIDFile(path string) int {
	content, err := os.ReadFile(path)
	if err != nil {
		return 0
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(content)))
	if err != nil {
		return 0
	}
	return pid
}

func processAlive(pid int) bool {
	return syscall.Kill(pid, 0) == nil
}
