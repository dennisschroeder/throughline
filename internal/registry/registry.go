package registry

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"path/filepath"
	"time"

	_ "modernc.org/sqlite"
)

const busyTimeoutMilliseconds = 5000

const schema = `
CREATE TABLE IF NOT EXISTS workspaces (
  workspace_id TEXT PRIMARY KEY,
  provider_kind TEXT NOT NULL,
  provider_locator TEXT NOT NULL,
  canonical_root TEXT NOT NULL,
  config_fingerprint TEXT NOT NULL,
  lifecycle_state TEXT NOT NULL CHECK (lifecycle_state IN ('pending','active')),
  generation INTEGER NOT NULL DEFAULT 1,
  fork_of_workspace_id TEXT NOT NULL DEFAULT '',
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL
);
CREATE UNIQUE INDEX IF NOT EXISTS workspaces_active_canonical_root
  ON workspaces(canonical_root)
  WHERE lifecycle_state = 'active';
`

// Registry is the per-user SQLite-backed exclusive routing allowlist. Every method opens
// or reuses a single connection and performs its own transaction; there is no in-process
// cache, so a lookup always reflects the durable state on disk.
type Registry struct {
	db *sql.DB
}

// Open opens (creating if needed) the registry database at path, applying platform
// permissions (0700 directory, 0600 file) and the schema. Production code should obtain
// path from DefaultPath; tests inject a temporary path.
func Open(ctx context.Context, path string) (*Registry, error) {
	if err := ensurePermissions(path); err != nil {
		return nil, err
	}
	absolutePath, err := filepath.Abs(path)
	if err != nil {
		return nil, fmt.Errorf("resolve registry path: %w", err)
	}
	query := url.Values{}
	query.Add("_pragma", "foreign_keys(1)")
	query.Add("_pragma", "journal_mode(WAL)")
	query.Add("_pragma", fmt.Sprintf("busy_timeout(%d)", busyTimeoutMilliseconds))
	dsn := (&url.URL{Scheme: "file", Path: filepath.Clean(absolutePath), RawQuery: query.Encode()}).String()
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("open registry database: %w", err)
	}
	// One connection gives the registry a single explicit writer boundary, matching the
	// workspace-store convention; reads and writes both go through it.
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	if err := db.PingContext(ctx); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("connect registry database: %w", err)
	}
	if _, err := db.ExecContext(ctx, schema); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("apply registry schema: %w", err)
	}
	if err := ensurePermissions(path); err != nil {
		_ = db.Close()
		return nil, err
	}
	return &Registry{db: db}, nil
}

func (r *Registry) Close() error {
	return r.db.Close()
}

// BeginRegistrationParams describes one throughline init/fork attempt.
type BeginRegistrationParams struct {
	WorkspaceID       string
	ProviderKind      ProviderKind
	ProviderLocator   string
	CanonicalRoot     string
	ConfigFingerprint string
	ForkOfWorkspaceID string
}

// RegistrationResult reports what BeginRegistration did, so callers (throughline init) can
// print an accurate verb and know whether the config file still needs to be written.
type RegistrationResult struct {
	Target  WorkspaceTarget
	Created bool // a brand-new pending entry was inserted
	Moved   bool // an existing active entry's canonical root was reconciled to a new path
}

// BeginRegistration idempotently starts or resumes registering workspace_id at
// canonical_root. It never silently reassigns an identity: if an existing active entry for
// this workspace_id has a different canonical_root that is still present on disk, it fails
// closed with ErrWorkspaceIdentityConflict (the copy-without-fork case). If the previous
// canonical_root is no longer present, the root is reconciled (a move) and the entry stays
// active. A fresh workspace_id is inserted pending; the caller must durably write
// .throughline/config.toml and then call Activate.
func (r *Registry) BeginRegistration(ctx context.Context, params BeginRegistrationParams) (RegistrationResult, error) {
	if params.WorkspaceID == "" || params.CanonicalRoot == "" || params.ConfigFingerprint == "" {
		return RegistrationResult{}, errors.New("registry: workspace_id, canonical_root, and config_fingerprint are required")
	}
	transaction, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return RegistrationResult{}, fmt.Errorf("registry: begin registration: %w", err)
	}
	defer transaction.Rollback()

	existing, err := queryTarget(ctx, transaction, params.WorkspaceID)
	now := time.Now().UTC()
	switch {
	case errors.Is(err, ErrWorkspaceNotFound):
		target := WorkspaceTarget{
			WorkspaceID:       params.WorkspaceID,
			ProviderKind:      params.ProviderKind,
			ProviderLocator:   params.ProviderLocator,
			CanonicalRoot:     params.CanonicalRoot,
			ConfigFingerprint: params.ConfigFingerprint,
			LifecycleState:    LifecyclePending,
			Generation:        1,
			ForkOfWorkspaceID: params.ForkOfWorkspaceID,
			CreatedAt:         now,
			UpdatedAt:         now,
		}
		if err := insertTarget(ctx, transaction, target); err != nil {
			return RegistrationResult{}, err
		}
		if err := transaction.Commit(); err != nil {
			return RegistrationResult{}, fmt.Errorf("registry: commit registration: %w", err)
		}
		return RegistrationResult{Target: target, Created: true}, nil
	case err != nil:
		return RegistrationResult{}, err
	}

	if existing.CanonicalRoot == params.CanonicalRoot {
		existing.ConfigFingerprint = params.ConfigFingerprint
		existing.ProviderKind = params.ProviderKind
		existing.ProviderLocator = params.ProviderLocator
		existing.UpdatedAt = now
		if err := updateTarget(ctx, transaction, existing); err != nil {
			return RegistrationResult{}, err
		}
		if err := transaction.Commit(); err != nil {
			return RegistrationResult{}, fmt.Errorf("registry: commit registration: %w", err)
		}
		return RegistrationResult{Target: existing}, nil
	}

	if existing.LifecycleState == LifecycleActive && RootAvailable(existing.CanonicalRoot) {
		return RegistrationResult{}, ErrWorkspaceIdentityConflict
	}

	// The previously registered root is gone (or registration never completed): reconcile
	// to the new root, preserving identity. Move reconciliation always keeps the entry
	// (re-)active once the caller confirms the config file is durably written.
	existing.CanonicalRoot = params.CanonicalRoot
	existing.ConfigFingerprint = params.ConfigFingerprint
	existing.ProviderKind = params.ProviderKind
	existing.ProviderLocator = params.ProviderLocator
	existing.LifecycleState = LifecyclePending
	existing.Generation++
	existing.UpdatedAt = now
	if err := updateTarget(ctx, transaction, existing); err != nil {
		return RegistrationResult{}, err
	}
	if err := transaction.Commit(); err != nil {
		return RegistrationResult{}, fmt.Errorf("registry: commit registration: %w", err)
	}
	return RegistrationResult{Target: existing, Moved: true}, nil
}

// Activate promotes a pending entry to active after the caller has durably written the
// workspace config file. expectedGeneration guards against a concurrent conflicting
// registration.
func (r *Registry) Activate(ctx context.Context, workspaceID string, expectedGeneration int64) (WorkspaceTarget, error) {
	transaction, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return WorkspaceTarget{}, fmt.Errorf("registry: begin activation: %w", err)
	}
	defer transaction.Rollback()

	target, err := queryTarget(ctx, transaction, workspaceID)
	if err != nil {
		return WorkspaceTarget{}, err
	}
	if target.Generation != expectedGeneration {
		return WorkspaceTarget{}, ErrGenerationConflict
	}
	target.LifecycleState = LifecycleActive
	target.UpdatedAt = time.Now().UTC()
	if err := updateTarget(ctx, transaction, target); err != nil {
		return WorkspaceTarget{}, err
	}
	if err := transaction.Commit(); err != nil {
		return WorkspaceTarget{}, fmt.Errorf("registry: commit activation: %w", err)
	}
	return target, nil
}

// Lookup performs a fresh, read-only resolution of workspace_id. It fails closed:
// ErrWorkspaceNotFound for an unknown id, ErrWorkspacePending for an interrupted
// registration, and ErrWorkspaceUnavailable when the canonical root no longer exists.
func (r *Registry) Lookup(ctx context.Context, workspaceID string) (WorkspaceTarget, error) {
	target, err := queryTarget(ctx, r.db, workspaceID)
	if err != nil {
		return WorkspaceTarget{}, err
	}
	if target.LifecycleState == LifecyclePending {
		return WorkspaceTarget{}, ErrWorkspacePending
	}
	if !RootAvailable(target.CanonicalRoot) {
		return WorkspaceTarget{}, ErrWorkspaceUnavailable
	}
	return target, nil
}

// CheckFingerprint compares a freshly computed config fingerprint against the registry's
// record and returns ErrWorkspaceRegistryConflict on disagreement, without repairing it.
func (r *Registry) CheckFingerprint(ctx context.Context, workspaceID, liveFingerprint string) error {
	target, err := queryTarget(ctx, r.db, workspaceID)
	if err != nil {
		return err
	}
	if target.ConfigFingerprint != liveFingerprint {
		return ErrWorkspaceRegistryConflict
	}
	return nil
}

// Fork creates a brand-new pending workspace_id with fork provenance recorded against
// sourceWorkspaceID, which must currently be active. The caller activates it the same way
// as a fresh registration once init --fork has written the forked config file.
func (r *Registry) Fork(ctx context.Context, sourceWorkspaceID string, params BeginRegistrationParams) (RegistrationResult, error) {
	source, err := r.Lookup(ctx, sourceWorkspaceID)
	if err != nil {
		if errors.Is(err, ErrWorkspaceNotFound) || errors.Is(err, ErrWorkspacePending) || errors.Is(err, ErrWorkspaceUnavailable) {
			return RegistrationResult{}, ErrForkSourceUnavailable
		}
		return RegistrationResult{}, err
	}
	params.ForkOfWorkspaceID = source.WorkspaceID
	return r.BeginRegistration(ctx, params)
}

// Unregister removes routing authority for workspace_id. It never touches the workspace's
// own persisted data; a provider's data survives unregistration unless separately purged.
func (r *Registry) Unregister(ctx context.Context, workspaceID string, expectedGeneration int64) error {
	transaction, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("registry: begin unregister: %w", err)
	}
	defer transaction.Rollback()

	target, err := queryTarget(ctx, transaction, workspaceID)
	if err != nil {
		return err
	}
	if target.Generation != expectedGeneration {
		return ErrGenerationConflict
	}
	if _, err := transaction.ExecContext(ctx, `DELETE FROM workspaces WHERE workspace_id = ?`, workspaceID); err != nil {
		return fmt.Errorf("registry: unregister: %w", err)
	}
	if err := transaction.Commit(); err != nil {
		return fmt.Errorf("registry: commit unregister: %w", err)
	}
	return nil
}

type queryRower interface {
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

type execer interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
}

func queryTarget(ctx context.Context, q queryRower, workspaceID string) (WorkspaceTarget, error) {
	row := q.QueryRowContext(ctx, `
SELECT workspace_id, provider_kind, provider_locator, canonical_root, config_fingerprint,
       lifecycle_state, generation, fork_of_workspace_id, created_at, updated_at
FROM workspaces WHERE workspace_id = ?`, workspaceID)
	var target WorkspaceTarget
	var providerKind, lifecycleState, createdAt, updatedAt string
	err := row.Scan(&target.WorkspaceID, &providerKind, &target.ProviderLocator, &target.CanonicalRoot,
		&target.ConfigFingerprint, &lifecycleState, &target.Generation, &target.ForkOfWorkspaceID,
		&createdAt, &updatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return WorkspaceTarget{}, ErrWorkspaceNotFound
	}
	if err != nil {
		return WorkspaceTarget{}, fmt.Errorf("registry: query workspace: %w", err)
	}
	target.ProviderKind = ProviderKind(providerKind)
	target.LifecycleState = LifecycleState(lifecycleState)
	target.CreatedAt, err = time.Parse(time.RFC3339Nano, createdAt)
	if err != nil {
		return WorkspaceTarget{}, fmt.Errorf("registry: parse created_at: %w", err)
	}
	target.UpdatedAt, err = time.Parse(time.RFC3339Nano, updatedAt)
	if err != nil {
		return WorkspaceTarget{}, fmt.Errorf("registry: parse updated_at: %w", err)
	}
	return target, nil
}

func insertTarget(ctx context.Context, e execer, target WorkspaceTarget) error {
	_, err := e.ExecContext(ctx, `
INSERT INTO workspaces (workspace_id, provider_kind, provider_locator, canonical_root,
  config_fingerprint, lifecycle_state, generation, fork_of_workspace_id, created_at, updated_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		target.WorkspaceID, string(target.ProviderKind), target.ProviderLocator, target.CanonicalRoot,
		target.ConfigFingerprint, string(target.LifecycleState), target.Generation, target.ForkOfWorkspaceID,
		target.CreatedAt.Format(time.RFC3339Nano), target.UpdatedAt.Format(time.RFC3339Nano))
	if err != nil {
		return fmt.Errorf("registry: insert workspace: %w", err)
	}
	return nil
}

func updateTarget(ctx context.Context, e execer, target WorkspaceTarget) error {
	_, err := e.ExecContext(ctx, `
UPDATE workspaces SET provider_kind = ?, provider_locator = ?, canonical_root = ?,
  config_fingerprint = ?, lifecycle_state = ?, generation = ?, updated_at = ?
WHERE workspace_id = ?`,
		string(target.ProviderKind), target.ProviderLocator, target.CanonicalRoot, target.ConfigFingerprint,
		string(target.LifecycleState), target.Generation, target.UpdatedAt.Format(time.RFC3339Nano), target.WorkspaceID)
	if err != nil {
		return fmt.Errorf("registry: update workspace: %w", err)
	}
	return nil
}
