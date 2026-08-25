// Package credential manages the single per-user bearer token that protects the
// Throughline daemon's loopback HTTP endpoint. The token is generated once, stored
// user-readable-only, and never enters workspace configuration, the registry, MCP output,
// or logs.
package credential

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
)

const (
	directoryMode = 0o700
	fileMode      = 0o600

	// tokenBytes is 256 bits, matching the accepted decision.
	tokenBytes = 32
)

// DefaultPath returns the production credential file location: the same per-user state
// directory as the registry, alongside it rather than inside it so a registry backup never
// incidentally includes the credential.
func DefaultPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve user home directory: %w", err)
	}
	switch runtime.GOOS {
	case "darwin":
		return filepath.Join(home, "Library", "Application Support", "Throughline", "credentials"), nil
	case "linux":
		stateHome := os.Getenv("XDG_STATE_HOME")
		if stateHome == "" {
			stateHome = filepath.Join(home, ".local", "state")
		}
		return filepath.Join(stateHome, "throughline", "credentials"), nil
	default:
		return "", fmt.Errorf("unsupported platform %q for the daemon credential", runtime.GOOS)
	}
}

// LoadOrCreate reads the token at path, generating and durably writing a fresh
// cryptographically random one if none exists yet. The file and its directory are
// user-readable-only.
func LoadOrCreate(path string) (string, error) {
	content, err := os.ReadFile(path)
	if err == nil {
		return string(content), nil
	}
	if !errors.Is(err, os.ErrNotExist) {
		return "", fmt.Errorf("read credential: %w", err)
	}
	return Regenerate(path)
}

// Regenerate always writes a fresh cryptographically random token to path, overwriting
// whatever was there, via an atomic fsync-and-rename write so a crash mid-write never
// leaves a truncated credential file. Callers that need rollback (daemon.RotateCredential)
// must back up the existing file themselves before calling this.
func Regenerate(path string) (string, error) {
	buffer := make([]byte, tokenBytes)
	if _, err := rand.Read(buffer); err != nil {
		return "", fmt.Errorf("generate credential: %w", err)
	}
	token := hex.EncodeToString(buffer)

	directory := filepath.Dir(path)
	if err := os.MkdirAll(directory, directoryMode); err != nil {
		return "", fmt.Errorf("create credential directory: %w", err)
	}
	if err := os.Chmod(directory, directoryMode); err != nil {
		return "", fmt.Errorf("set credential directory permissions: %w", err)
	}
	temporary, err := os.CreateTemp(directory, ".credentials-*.tmp")
	if err != nil {
		return "", fmt.Errorf("create temporary credential: %w", err)
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)
	if err := temporary.Chmod(fileMode); err != nil {
		_ = temporary.Close()
		return "", fmt.Errorf("set temporary credential permissions: %w", err)
	}
	if _, err := temporary.WriteString(token); err != nil {
		_ = temporary.Close()
		return "", fmt.Errorf("write temporary credential: %w", err)
	}
	if err := temporary.Sync(); err != nil {
		_ = temporary.Close()
		return "", fmt.Errorf("sync temporary credential: %w", err)
	}
	if err := temporary.Close(); err != nil {
		return "", fmt.Errorf("close temporary credential: %w", err)
	}
	if err := os.Rename(temporaryPath, path); err != nil {
		return "", fmt.Errorf("install credential: %w", err)
	}
	return token, nil
}

// Equal compares a presented token against the expected one in constant time, so response
// latency cannot be used to guess the credential byte by byte.
func Equal(expected, presented string) bool {
	return subtle.ConstantTimeCompare([]byte(expected), []byte(presented)) == 1
}
