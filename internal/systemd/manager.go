// Package systemd implements daemon.ServiceManager for Linux systemd --user services. It
// is one adapter behind the daemon-management seam internal/daemon defines, alongside
// internal/launchd for macOS; Throughline does not implement its own process supervisor.
// Every unit it writes contains only the daemon's loopback address, executable path, and
// log path — never a bearer token, workspace path, registry path, or provider locator.
package systemd

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/dennisschroeder/throughline/internal/daemon"
)

// DefaultUnitName is the one systemd user unit Throughline ever registers.
const DefaultUnitName = "throughline-daemon.service"

var _ daemon.ServiceManager = (*Manager)(nil)

// DefaultUnitPath returns the production unit file location for the current user.
func DefaultUnitPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve user home directory: %w", err)
	}
	configHome := os.Getenv("XDG_CONFIG_HOME")
	if configHome == "" {
		configHome = filepath.Join(home, ".config")
	}
	return filepath.Join(configHome, "systemd", "user", DefaultUnitName), nil
}

// CommandRunner executes one systemctl (or loginctl) invocation. The production Runner
// shells out; fixture tests inject a fake that records the exact command and args without
// ever touching the real systemd, satisfying the accepted "never mutate the real service"
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
// systemd --user unit.
type Manager struct {
	UnitName   string // defaults to DefaultUnitName
	UnitPath   string
	Executable string
	Addr       string
	LogPath    string
	Runner     CommandRunner

	// CheckHealth, if set, is called by Status to populate Version once systemd reports
	// the unit active.
	CheckHealth func(ctx context.Context) (version string, err error)
}

func (m *Manager) unitName() string {
	if m.UnitName != "" {
		return m.UnitName
	}
	return DefaultUnitName
}

func (m *Manager) runner() CommandRunner {
	if m.Runner != nil {
		return m.Runner
	}
	return execRunner{}
}

// Start (re)writes the unit file, mode 0600 since it names the executable and log path,
// reloads the user systemd instance's configuration, and starts (or restarts, if already
// running) the unit.
func (m *Manager) Start(ctx context.Context) error {
	if err := m.writeUnit(); err != nil {
		return err
	}
	if _, err := m.runner().Run(ctx, "systemctl", "--user", "daemon-reload"); err != nil {
		return fmt.Errorf("systemctl daemon-reload: %w", err)
	}
	if _, err := m.runner().Run(ctx, "systemctl", "--user", "start", m.unitName()); err != nil {
		return fmt.Errorf("systemctl start: %w", err)
	}
	return nil
}

func (m *Manager) Stop(ctx context.Context) error {
	if _, err := m.runner().Run(ctx, "systemctl", "--user", "stop", m.unitName()); err != nil {
		return fmt.Errorf("systemctl stop: %w", err)
	}
	return nil
}

func (m *Manager) Restart(ctx context.Context) error {
	if _, err := m.runner().Run(ctx, "systemctl", "--user", "restart", m.unitName()); err != nil {
		return fmt.Errorf("systemctl restart: %w", err)
	}
	return nil
}

// Status calls `systemctl --user show` for stable machine-readable properties rather than
// parsing the human-oriented `status` subcommand's output. A non-zero systemctl exit
// (typically "unit could not be found") is treated as not running rather than an error.
func (m *Manager) Status(ctx context.Context) (daemon.Status, error) {
	output, runErr := m.runner().Run(ctx, "systemctl", "--user", "show", m.unitName(),
		"--property=ActiveState,MainPID")
	if runErr != nil {
		return daemon.Status{}, nil
	}
	properties := parseProperties(output)
	status := daemon.Status{
		Running: properties["ActiveState"] == "active",
	}
	if pid, err := strconv.Atoi(properties["MainPID"]); err == nil {
		status.PID = pid
	}
	if status.Running && m.CheckHealth != nil {
		if version, err := m.CheckHealth(ctx); err == nil {
			status.Version = version
		}
	}
	return status, nil
}

func (m *Manager) Logs(ctx context.Context, n int) ([]string, error) {
	return daemon.ReadLogTail(m.LogPath, n)
}

func parseProperties(output string) map[string]string {
	properties := map[string]string{}
	for _, line := range strings.Split(output, "\n") {
		key, value, ok := strings.Cut(strings.TrimSpace(line), "=")
		if ok {
			properties[key] = value
		}
	}
	return properties
}

func (m *Manager) writeUnit() error {
	directory := filepath.Dir(m.UnitPath)
	if err := os.MkdirAll(directory, 0o755); err != nil {
		return fmt.Errorf("create systemd user unit directory: %w", err)
	}
	content := renderUnit(m.Executable, m.Addr, m.LogPath)
	temporary, err := os.CreateTemp(directory, ".unit-*.tmp")
	if err != nil {
		return fmt.Errorf("create temporary unit file: %w", err)
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)
	if err := temporary.Chmod(0o600); err != nil {
		_ = temporary.Close()
		return fmt.Errorf("set temporary unit file permissions: %w", err)
	}
	if _, err := temporary.WriteString(content); err != nil {
		_ = temporary.Close()
		return fmt.Errorf("write temporary unit file: %w", err)
	}
	if err := temporary.Close(); err != nil {
		return fmt.Errorf("close temporary unit file: %w", err)
	}
	if err := os.Rename(temporaryPath, m.UnitPath); err != nil {
		return fmt.Errorf("install unit file: %w", err)
	}
	return nil
}

// renderUnit builds the systemd user-service unit. It contains only the daemon's
// invocation (executable, "mcp", "--addr", addr) and where to redirect its output — never a
// bearer token, workspace path, registry path, or provider locator, none of which this
// function even receives. StandardOutput/StandardError point at the same log file so
// daemon.ReadLogTail sees one combined stream, matching the launchd adapter.
func renderUnit(executable, addr, logPath string) string {
	var b bytes.Buffer
	b.WriteString("[Unit]\n")
	b.WriteString("Description=Throughline workspace-routing daemon\n\n")
	b.WriteString("[Service]\n")
	b.WriteString("Type=simple\n")
	b.WriteString(fmt.Sprintf("ExecStart=%s mcp --addr %s\n", quoteUnitArg(executable), quoteUnitArg(addr)))
	b.WriteString("Restart=on-failure\n")
	b.WriteString("StandardOutput=append:" + logPath + "\n")
	b.WriteString("StandardError=append:" + logPath + "\n\n")
	b.WriteString("[Install]\n")
	b.WriteString("WantedBy=default.target\n")
	return b.String()
}

// quoteUnitArg wraps an ExecStart argument in double quotes if it contains whitespace, per
// systemd.service(5)'s command-line quoting rules; addr and a typical executable path never
// do, but this keeps the unit correct if either ever contains a space.
func quoteUnitArg(value string) string {
	if strings.ContainsAny(value, " \t") {
		return `"` + strings.ReplaceAll(value, `"`, `\"`) + `"`
	}
	return value
}
