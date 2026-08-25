// Package router implements WorkspaceRouter: the single deep interface the MCP adapter
// uses to resolve a required workspace_id to an application Service for the current
// request. The router interprets no provider locator itself; it resolves an active
// WorkspaceTarget through the registry and delegates it to a ProviderManager, so no layer
// above a PersistenceProvider assumes one workspace equals one database, file, schema, or
// connection.
package router

import "errors"

// Sentinel errors match the stable routing error codes from the accepted decisions. None
// carries a filesystem path, provider locator, or credential; callers map these to the MCP
// error envelope.
var (
	ErrWorkspaceRequired = errors.New("workspace_required")
	ErrWorkspaceInvalid  = errors.New("workspace_invalid")

	// ErrProviderUnsupported means no PersistenceProvider is registered for the target's
	// provider kind.
	ErrProviderUnsupported = errors.New("provider_unsupported")

	// ErrProviderUnavailable means the provider for a known, active target could not open
	// or migrate its store.
	ErrProviderUnavailable = errors.New("provider_unavailable")

	// ErrWorkspaceBusy means a resolution attempt was abandoned because a concurrent one
	// for the same workspace_id is still in flight and the caller asked not to wait.
	ErrWorkspaceBusy = errors.New("workspace_busy")
)
