package config

import (
	"bytes"
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
	CurrentSchema       = 1
)

type Config struct {
	SchemaVersion int    `toml:"schema_version"`
	DatabasePath  string `toml:"database_path"`
	ItemKeyPrefix string `toml:"item_key_prefix"`
}

type Workspace struct {
	Root         string
	Directory    string
	ConfigPath   string
	DatabasePath string
	Config       Config
}

func Initialize(root, databasePath string) (Workspace, bool, error) {
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

	if databasePath == "" {
		databasePath = DefaultDatabasePath
	}
	workspaceConfig := Config{
		SchemaVersion: CurrentSchema,
		DatabasePath:  databasePath,
		ItemKeyPrefix: "TH",
	}
	encoded, err := toml.Marshal(workspaceConfig)
	if err != nil {
		return Workspace{}, false, fmt.Errorf("encode workspace config: %w", err)
	}
	file, err := os.OpenFile(configPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		return Workspace{}, false, fmt.Errorf("create workspace config: %w", err)
	}
	if _, err := file.Write(encoded); err != nil {
		_ = file.Close()
		return Workspace{}, false, fmt.Errorf("write workspace config: %w", err)
	}
	if err := file.Close(); err != nil {
		return Workspace{}, false, fmt.Errorf("close workspace config: %w", err)
	}

	workspace, err := Load(root)
	if err != nil {
		return Workspace{}, false, err
	}
	return workspace, true, nil
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
	if workspaceConfig.SchemaVersion != CurrentSchema {
		return Workspace{}, fmt.Errorf("unsupported workspace config schema %d", workspaceConfig.SchemaVersion)
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
