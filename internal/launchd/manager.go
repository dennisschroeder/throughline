// Package launchd implements daemon.ServiceManager for macOS launchd user (LaunchAgent)
// services. It is one adapter behind the daemon-management seam internal/daemon defines;
// Throughline does not implement its own process supervisor. All service definitions it
// writes contain only the daemon's loopback address, executable path, and log path — never
// a bearer token, workspace path, registry path, or provider locator.
package launchd

import (
	"bytes"
	"context"
	"fmt"
	"html"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/dennisschroeder/throughline/internal/daemon"
)

// DefaultLabel is the one launchd service label Throughline ever registers.
const DefaultLabel = "com.throughline.daemon"

var _ daemon.ServiceManager = (*Manager)(nil)

// DefaultPlistPath returns the production LaunchAgent plist location for the current user.
func DefaultPlistPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve user home directory: %w", err)
	}
	return filepath.Join(home, "Library", "LaunchAgents", DefaultLabel+".plist"), nil
}

// CommandRunner executes one launchctl invocation. The production Runner shells out;
// fixture tests inject a fake that records the exact command and args without ever
// touching the real launchd, satisfying the accepted "never mutate the real service"
// requirement.
type CommandRunner interface {
	Run(ctx context.Context, name string, args ...string) (output string, err error)
}

type execRunner struct{}

func (execRunner) Run(ctx context.Context, name string, args ...string) (string, error) {
	command := exec.CommandContext(ctx, name, args...)
	output, err := command.CombinedOutput()
	return string(output), err
}

// Manager implements daemon.ServiceManager by mapping every lifecycle operation onto one
// launchd LaunchAgent: Start writes the plist and bootstraps it, Stop boots it out, Restart
// kickstarts it, Status prints and parses its state, and Logs reads the file launchd
// redirects its output to.
type Manager struct {
	Label      string // defaults to DefaultLabel
	PlistPath  string
	Executable string
	Addr       string
	LogPath    string
	UID        int // defaults to os.Getuid()
	Runner     CommandRunner

	// CheckHealth, if set, is called by Status to populate Version once launchd reports
	// the service running.
	CheckHealth func(ctx context.Context) (version string, err error)
}

func (m *Manager) label() string {
	if m.Label != "" {
		return m.Label
	}
	return DefaultLabel
}

func (m *Manager) uid() int {
	if m.UID != 0 {
		return m.UID
	}
	return os.Getuid()
}

func (m *Manager) domainTarget() string {
	return fmt.Sprintf("gui/%d", m.uid())
}

func (m *Manager) serviceTarget() string {
	return fmt.Sprintf("%s/%s", m.domainTarget(), m.label())
}

func (m *Manager) runner() CommandRunner {
	if m.Runner != nil {
		return m.Runner
	}
	return execRunner{}
}

// Start (re)writes the plist with the current configuration, mode 0600 since it names the
// executable and log path, then bootstraps it into the user's GUI domain.
func (m *Manager) Start(ctx context.Context) error {
	if err := m.writePlist(); err != nil {
		return err
	}
	if _, err := m.runner().Run(ctx, "launchctl", "bootstrap", m.domainTarget(), m.PlistPath); err != nil {
		return fmt.Errorf("launchctl bootstrap: %w", err)
	}
	return nil
}

func (m *Manager) Stop(ctx context.Context) error {
	if _, err := m.runner().Run(ctx, "launchctl", "bootout", m.serviceTarget()); err != nil {
		return fmt.Errorf("launchctl bootout: %w", err)
	}
	return nil
}

func (m *Manager) Restart(ctx context.Context) error {
	if _, err := m.runner().Run(ctx, "launchctl", "kickstart", "-k", m.serviceTarget()); err != nil {
		return fmt.Errorf("launchctl kickstart: %w", err)
	}
	return nil
}

// Status parses `launchctl print <service target>` for the running PID. launchd reports
// "state = running" and "pid = <n>" as separate lines when active; their absence (or a
// non-zero launchctl exit, which usually means the service is not loaded) means not
// running rather than an error.
func daemonStatusFromPrint(output string) (running bool, pid int) {
	for _, line := range strings.Split(output, "\n") {
		trimmed := strings.TrimSpace(line)
		switch {
		case strings.HasPrefix(trimmed, "state = "):
			running = strings.TrimPrefix(trimmed, "state = ") == "running"
		case strings.HasPrefix(trimmed, "pid = "):
			pid, _ = strconv.Atoi(strings.TrimPrefix(trimmed, "pid = "))
		}
	}
	return running, pid
}

// Status parses launchctl's report of the service rather than erroring when the service
// simply is not loaded (a non-zero launchctl exit almost always means exactly that): the
// returned error is reserved for a genuinely unexpected failure to invoke launchctl itself.
func (m *Manager) Status(ctx context.Context) (daemon.Status, error) {
	output, _ := m.runner().Run(ctx, "launchctl", "print", m.serviceTarget())
	running, pid := daemonStatusFromPrint(output)
	status := daemon.Status{Running: running, PID: pid}
	if running && m.CheckHealth != nil {
		if version, err := m.CheckHealth(ctx); err == nil {
			status.Version = version
		}
	}
	return status, nil
}

func (m *Manager) Logs(ctx context.Context, n int) ([]string, error) {
	return daemon.ReadLogTail(m.LogPath, n)
}

func (m *Manager) writePlist() error {
	directory := filepath.Dir(m.PlistPath)
	if err := os.MkdirAll(directory, 0o755); err != nil {
		return fmt.Errorf("create LaunchAgents directory: %w", err)
	}
	content := renderPlist(m.label(), m.Executable, m.Addr, m.LogPath)
	temporary, err := os.CreateTemp(directory, ".plist-*.tmp")
	if err != nil {
		return fmt.Errorf("create temporary plist: %w", err)
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)
	if err := temporary.Chmod(0o600); err != nil {
		_ = temporary.Close()
		return fmt.Errorf("set temporary plist permissions: %w", err)
	}
	if _, err := temporary.WriteString(content); err != nil {
		_ = temporary.Close()
		return fmt.Errorf("write temporary plist: %w", err)
	}
	if err := temporary.Close(); err != nil {
		return fmt.Errorf("close temporary plist: %w", err)
	}
	if err := os.Rename(temporaryPath, m.PlistPath); err != nil {
		return fmt.Errorf("install plist: %w", err)
	}
	return nil
}

// renderPlist builds the LaunchAgent property list. It contains only the daemon's
// invocation (executable, "mcp", "--addr", addr) and where to redirect its output — never a
// bearer token, workspace path, registry path, or provider locator, none of which this
// function even receives.
func renderPlist(label, executable, addr, logPath string) string {
	var b bytes.Buffer
	b.WriteString(`<?xml version="1.0" encoding="UTF-8"?>` + "\n")
	b.WriteString(`<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">` + "\n")
	b.WriteString(`<plist version="1.0">` + "\n<dict>\n")
	b.WriteString("  <key>Label</key>\n  <string>" + html.EscapeString(label) + "</string>\n")
	b.WriteString("  <key>ProgramArguments</key>\n  <array>\n")
	for _, argument := range []string{executable, "mcp", "--addr", addr} {
		b.WriteString("    <string>" + html.EscapeString(argument) + "</string>\n")
	}
	b.WriteString("  </array>\n")
	b.WriteString("  <key>RunAtLoad</key>\n  <true/>\n")
	b.WriteString("  <key>KeepAlive</key>\n  <dict>\n    <key>SuccessfulExit</key>\n    <false/>\n  </dict>\n")
	b.WriteString("  <key>ProcessType</key>\n  <string>Background</string>\n")
	b.WriteString("  <key>StandardOutPath</key>\n  <string>" + html.EscapeString(logPath) + "</string>\n")
	b.WriteString("  <key>StandardErrorPath</key>\n  <string>" + html.EscapeString(logPath) + "</string>\n")
	b.WriteString("</dict>\n</plist>\n")
	return b.String()
}
