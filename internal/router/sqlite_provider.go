package router

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/dennisschroeder/throughline/internal/config"
	"github.com/dennisschroeder/throughline/internal/registry"
	throughlinesqlite "github.com/dennisschroeder/throughline/internal/sqlite"
)

// SQLiteProvider is the production PersistenceProvider for registry.ProviderSQLite. It
// opens one configured *sql.DB per active target by reading that target's own
// .throughline/config.toml for database_path, preserving the explicit single-writer
// boundary described in ADR 0005 while letting different workspaces open independently.
// It never receives or stores a raw database path outside its own Open call.
type SQLiteProvider struct{}

func (SQLiteProvider) Kind() registry.ProviderKind { return registry.ProviderSQLite }

func (SQLiteProvider) Open(ctx context.Context, target registry.WorkspaceTarget) (ProviderHandle, error) {
	workspace, err := config.Load(target.CanonicalRoot)
	if err != nil {
		return ProviderHandle{}, fmt.Errorf("%w: load workspace config: %v", ErrProviderUnavailable, err)
	}
	if workspace.Config.WorkspaceID != target.WorkspaceID {
		return ProviderHandle{}, fmt.Errorf("%w: registry and workspace config disagree on workspace_id", registry.ErrWorkspaceRegistryConflict)
	}
	if err := os.MkdirAll(filepath.Dir(workspace.DatabasePath), 0o755); err != nil {
		return ProviderHandle{}, fmt.Errorf("%w: create database directory: %v", ErrProviderUnavailable, err)
	}
	database, err := throughlinesqlite.Open(ctx, workspace.DatabasePath)
	if err != nil {
		return ProviderHandle{}, fmt.Errorf("%w: open database: %v", ErrProviderUnavailable, err)
	}
	if err := database.Migrate(ctx); err != nil {
		_ = database.Close()
		return ProviderHandle{}, fmt.Errorf("%w: migrate database: %v", ErrProviderUnavailable, err)
	}
	return ProviderHandle{Store: database.Store(), Close: database.Close}, nil
}
