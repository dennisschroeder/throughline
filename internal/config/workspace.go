package config

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/pelletier/go-toml/v2"
)

const (
	DirectoryName       = ".throughline"
	ConfigFileName      = "config.toml"
	DefaultDatabasePath = "throughline.db"
	CurrentSchema       = 2

	// LegacySchema is the pre-routing single-workspace schema. Its config.toml has no
	// workspace_id; Load rejects it with ErrLegacyWorkspace instead of silently treating
	// it as routable. There is no automatic migration: see the clean-cut cutover
	// procedure in docs/product/workspace-routing-spec.md.
	LegacySchema = 1
)

// ErrLegacyWorkspace is returned by Load when a workspace's config.toml predates workspace
// identity (schema_version 1, no workspace_id). Callers should surface remediation
// pointing at export/archive/setup/init, never fall back to routing it.
var ErrLegacyWorkspace = errors.New("legacy_workspace_unsupported")

type Config struct {
	SchemaVersion int    `toml:"schema_version"`
	WorkspaceID   string `toml:"workspace_id"`
	DatabasePath  string `toml:"database_path"`
	ItemKeyPrefix string `toml:"item_key_prefix"`
}

// Fingerprint is a stable content hash of the fields that identify this workspace's
// configuration. The registry stores it at registration time and compares it against a
// freshly loaded value to detect drift between the registry and the workspace's own
// config.toml, without ever storing the config file's path or contents itself.
func (c Config) Fingerprint() string {
	sum := sha256.Sum256([]byte(fmt.Sprintf("%d|%s|%s|%s", c.SchemaVersion, c.WorkspaceID, c.DatabasePath, c.ItemKeyPrefix)))
	return hex.EncodeToString(sum[:])
}

type Workspace struct {
	Root         string
	Directory    string
	ConfigPath   string
	DatabasePath string
	Config       Config
}

// Initialize idempotently creates or reopens the workspace at root. On first creation it
// writes workspaceID into config.toml via an atomic fsync-and-rename so a crash mid-write
// never leaves a partially written config; workspaceID is ignored when the workspace
// already exists. The caller (throughline init) is responsible for registering the
// resulting identity in the global registry before or after this call, per its own
// pending-to-active recovery contract.
func Initialize(root, databasePath, workspaceID string) (Workspace, bool, error) {
	root, err := filepath.Abs(root)
	if err != nil {
		return Workspace{}, false, fmt.Errorf("resolve workspace root: %w", err)
	}
	root = filepath.Clean(root)
	info, err := os.Stat(root)
	if err != nil {
		return Workspace{}, false, fmt.Errorf("inspect workspace root: %w", err)
	}
	if !info.IsDir() {
		return Workspace{}, false, errors.New("workspace root is not a directory")
	}

	directory := filepath.Join(root, DirectoryName)
	if err := os.MkdirAll(directory, 0o755); err != nil {
		return Workspace{}, false, fmt.Errorf("create workspace directory: %w", err)
	}
	configPath := filepath.Join(directory, ConfigFileName)
	if _, err := os.Stat(configPath); err == nil {
		workspace, loadErr := Load(root)
		if loadErr != nil {
			return Workspace{}, false, loadErr
		}
		if databasePath != "" && databasePath != workspace.Config.DatabasePath {
			return Workspace{}, false, fmt.Errorf("workspace already uses database_path %q", workspace.Config.DatabasePath)
		}
		return workspace, false, nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return Workspace{}, false, fmt.Errorf("inspect workspace config: %w", err)
	}

	if workspaceID == "" {
		return Workspace{}, false, errors.New("workspace_id is required to create a new workspace")
	}
	if databasePath == "" {
		databasePath = DefaultDatabasePath
	}
	workspaceConfig := Config{
		SchemaVersion: CurrentSchema,
		WorkspaceID:   workspaceID,
		DatabasePath:  databasePath,
		ItemKeyPrefix: "TH",
	}
	encoded, err := toml.Marshal(workspaceConfig)
	if err != nil {
		return Workspace{}, false, fmt.Errorf("encode workspace config: %w", err)
	}
	if err := writeFileAtomically(configPath, encoded, 0o644, false); err != nil {
		return Workspace{}, false, err
	}

	workspace, err := Load(root)
	if err != nil {
		return Workspace{}, false, err
	}
	return workspace, true, nil
}

// Fork rewrites an already-initialized workspace's config.toml with a brand-new
// workspace_id, keeping its existing database_path and item_key_prefix. It is the only
// sanctioned way an independent copy of a workspace directory diverges from the workspace
// it was copied from; a plain copy otherwise keeps the original identity. Fork returns the
// rewritten workspace along with the workspace_id it forked from, so the caller can record
// fork provenance in the registry.
func Fork(root, newWorkspaceID string) (Workspace, string, error) {
	if newWorkspaceID == "" {
		return Workspace{}, "", errors.New("workspace_id is required to fork a workspace")
	}
	existing, err := Load(root)
	if err != nil {
		return Workspace{}, "", err
	}
	sourceWorkspaceID := existing.Config.WorkspaceID
	forked := Config{
		SchemaVersion: CurrentSchema,
		WorkspaceID:   newWorkspaceID,
		DatabasePath:  existing.Config.DatabasePath,
		ItemKeyPrefix: existing.Config.ItemKeyPrefix,
	}
	encoded, err := toml.Marshal(forked)
	if err != nil {
		return Workspace{}, "", fmt.Errorf("encode forked workspace config: %w", err)
	}
	if err := writeFileAtomically(existing.ConfigPath, encoded, 0o644, true); err != nil {
		return Workspace{}, "", err
	}
	workspace, err := Load(root)
	if err != nil {
		return Workspace{}, "", err
	}
	return workspace, sourceWorkspaceID, nil
}

// writeFileAtomically writes content to a temporary file in the same directory as path,
// fsyncs it, and renames it into place, so config.toml never exists partially written.
// overwrite must be true for Fork's intentional replacement of an existing config file;
// fresh Initialize calls pass false so a concurrent creator is never silently clobbered.
func writeFileAtomically(path string, content []byte, mode os.FileMode, overwrite bool) error {
	directory := filepath.Dir(path)
	temporary, err := os.CreateTemp(directory, ".config-*.toml.tmp")
	if err != nil {
		return fmt.Errorf("create temporary workspace config: %w", err)
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)

	if err := temporary.Chmod(mode); err != nil {
		_ = temporary.Close()
		return fmt.Errorf("set temporary workspace config permissions: %w", err)
	}
	if _, err := temporary.Write(content); err != nil {
		_ = temporary.Close()
		return fmt.Errorf("write temporary workspace config: %w", err)
	}
	if err := temporary.Sync(); err != nil {
		_ = temporary.Close()
		return fmt.Errorf("sync temporary workspace config: %w", err)
	}
	if err := temporary.Close(); err != nil {
		return fmt.Errorf("close temporary workspace config: %w", err)
	}
	if !overwrite {
		if _, err := os.Stat(path); err == nil {
			return fmt.Errorf("workspace config already exists at %s", path)
		} else if !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("inspect workspace config: %w", err)
		}
	}
	if err := os.Rename(temporaryPath, path); err != nil {
		return fmt.Errorf("rename workspace config into place: %w", err)
	}
	return nil
}

func Find(start string) (Workspace, error) {
	current, err := filepath.Abs(start)
	if err != nil {
		return Workspace{}, fmt.Errorf("resolve workspace search path: %w", err)
	}
	current = filepath.Clean(current)
	for {
		if _, err := os.Stat(filepath.Join(current, DirectoryName, ConfigFileName)); err == nil {
			return Load(current)
		} else if !errors.Is(err, os.ErrNotExist) {
			return Workspace{}, fmt.Errorf("inspect workspace config: %w", err)
		}
		parent := filepath.Dir(current)
		if parent == current {
			return Workspace{}, errors.New("no Throughline workspace found")
		}
		current = parent
	}
}

func Load(root string) (Workspace, error) {
	root, err := filepath.Abs(root)
	if err != nil {
		return Workspace{}, fmt.Errorf("resolve workspace root: %w", err)
	}
	root = filepath.Clean(root)
	directory := filepath.Join(root, DirectoryName)
	configPath := filepath.Join(directory, ConfigFileName)
	content, err := os.ReadFile(configPath)
	if err != nil {
		return Workspace{}, fmt.Errorf("read workspace config: %w", err)
	}
	var workspaceConfig Config
	decoder := toml.NewDecoder(bytes.NewReader(content))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&workspaceConfig); err != nil {
		return Workspace{}, fmt.Errorf("decode workspace config: %w", err)
	}
	if workspaceConfig.SchemaVersion == LegacySchema {
		return Workspace{}, fmt.Errorf("%w: %s predates workspace identity; export, archive, run throughline setup, and reinitialize with throughline init", ErrLegacyWorkspace, configPath)
	}
	if workspaceConfig.SchemaVersion != CurrentSchema {
		return Workspace{}, fmt.Errorf("unsupported workspace config schema %d", workspaceConfig.SchemaVersion)
	}
	if workspaceConfig.WorkspaceID == "" {
		return Workspace{}, errors.New("workspace_id is required")
	}
	if workspaceConfig.DatabasePath == "" {
		return Workspace{}, errors.New("workspace database_path is required")
	}
	if workspaceConfig.ItemKeyPrefix == "" {
		return Workspace{}, errors.New("workspace item_key_prefix is required")
	}
	databasePath := workspaceConfig.DatabasePath
	if !filepath.IsAbs(databasePath) {
		databasePath = filepath.Join(directory, databasePath)
	}
	databasePath, err = filepath.Abs(databasePath)
	if err != nil {
		return Workspace{}, fmt.Errorf("resolve database path: %w", err)
	}
	return Workspace{
		Root:         root,
		Directory:    directory,
		ConfigPath:   configPath,
		DatabasePath: filepath.Clean(databasePath),
		Config:       workspaceConfig,
	}, nil
}
