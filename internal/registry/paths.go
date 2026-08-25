package registry

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
)

const (
	directoryMode = 0o700
	fileMode      = 0o600
)

// DefaultPath returns the production registry.db location for the current platform:
// ~/Library/Application Support/Throughline/registry.db on macOS, and
// ${XDG_STATE_HOME:-~/.local/state}/throughline/registry.db on Linux. Tests must inject an
// explicit temporary path with Open instead of relying on this function; there is no other
// registry-location routing mechanism.
func DefaultPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve user home directory: %w", err)
	}
	switch runtime.GOOS {
	case "darwin":
		return filepath.Join(home, "Library", "Application Support", "Throughline", "registry.db"), nil
	case "linux":
		stateHome := os.Getenv("XDG_STATE_HOME")
		if stateHome == "" {
			stateHome = filepath.Join(home, ".local", "state")
		}
		return filepath.Join(stateHome, "throughline", "registry.db"), nil
	default:
		return "", fmt.Errorf("unsupported platform %q for the workspace registry", runtime.GOOS)
	}
}

// ensurePermissions creates the registry directory at 0700 and, once the database file
// exists, tightens it to 0600. It never widens permissions on an existing directory or file.
func ensurePermissions(path string) error {
	directory := filepath.Dir(path)
	if err := os.MkdirAll(directory, directoryMode); err != nil {
		return fmt.Errorf("create registry directory: %w", err)
	}
	if err := os.Chmod(directory, directoryMode); err != nil {
		return fmt.Errorf("set registry directory permissions: %w", err)
	}
	if info, err := os.Stat(path); err == nil && !info.IsDir() {
		if err := os.Chmod(path, fileMode); err != nil {
			return fmt.Errorf("set registry file permissions: %w", err)
		}
	}
	return nil
}
