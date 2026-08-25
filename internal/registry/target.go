// Package registry implements the per-user SQLite workspace registry: the exclusive
// routing allowlist mapping a logical workspace_id to a provider-neutral WorkspaceTarget.
// The registry never stores bearer credentials, provider secrets, or database paths; it
// is not itself a workspace persistence provider.
package registry

import "time"

// LifecycleState is the registration state of a registry entry. Only active entries are
// routable; pending entries exist only to make initialization atomic and recoverable.
type LifecycleState string

const (
	LifecyclePending LifecycleState = "pending"
	LifecycleActive  LifecycleState = "active"
)

// ProviderKind identifies which PersistenceProvider adapter owns a workspace's storage
// topology. The registry never interprets ProviderLocator; only the matching provider does.
type ProviderKind string

const (
	ProviderSQLite ProviderKind = "sqlite"
)

// WorkspaceTarget is the provider-neutral, secret-free record the registry hands to a
// resolver: workspace identity, provider kind, an opaque non-secret provider locator, the
// canonical physical workspace root, a config fingerprint, lifecycle state, and the
// optimistic-concurrency generation.
type WorkspaceTarget struct {
	WorkspaceID       string
	ProviderKind      ProviderKind
	ProviderLocator   string
	CanonicalRoot     string
	ConfigFingerprint string
	LifecycleState    LifecycleState
	Generation        int64
	ForkOfWorkspaceID string
	CreatedAt         time.Time
	UpdatedAt         time.Time
}
