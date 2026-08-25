package registry

import "errors"

// Sentinel errors correspond one-to-one with the stable routing error codes from the
// accepted workspace-routing decisions. Callers map these to the MCP error envelope; no
// error here carries a filesystem path, provider locator, or credential.
var (
	// ErrWorkspaceNotFound means no registry entry exists for the requested workspace_id.
	ErrWorkspaceNotFound = errors.New("workspace_not_found")

	// ErrWorkspacePending means the entry exists but never completed pending-to-active
	// registration (an interrupted or in-progress init).
	ErrWorkspacePending = errors.New("workspace_pending")

	// ErrWorkspaceUnavailable means the entry is active but its canonical root is no
	// longer present on disk.
	ErrWorkspaceUnavailable = errors.New("workspace_unavailable")

	// ErrWorkspaceRegistryConflict means the registry and the live workspace configuration
	// disagree (fingerprint mismatch) and must not be silently reconciled.
	ErrWorkspaceRegistryConflict = errors.New("workspace_registry_conflict")

	// ErrWorkspaceIdentityConflict means two distinct canonical roots currently claim the
	// same workspace_id. Registration fails closed rather than silently reassigning
	// identity; this is the copy-without-fork case.
	ErrWorkspaceIdentityConflict = errors.New("workspace_identity_conflict")

	// ErrGenerationConflict is the optimistic-concurrency mismatch for mutating calls.
	ErrGenerationConflict = errors.New("workspace_registry_generation_conflict")

	// ErrForkSourceUnavailable means Fork was called against a workspace_id that is not
	// currently active.
	ErrForkSourceUnavailable = errors.New("workspace_fork_source_unavailable")
)
