package registry

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func openTestRegistry(t *testing.T) (*Registry, context.Context) {
	t.Helper()
	ctx := context.Background()
	registry, err := Open(ctx, filepath.Join(t.TempDir(), "registry.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = registry.Close() })
	return registry, ctx
}

func params(id, root, fingerprint string) BeginRegistrationParams {
	return BeginRegistrationParams{
		WorkspaceID:       id,
		ProviderKind:      ProviderSQLite,
		ProviderLocator:   id,
		CanonicalRoot:     root,
		ConfigFingerprint: fingerprint,
	}
}

func registerAndActivate(t *testing.T, reg *Registry, ctx context.Context, id, root, fingerprint string) WorkspaceTarget {
	t.Helper()
	result, err := reg.BeginRegistration(ctx, params(id, root, fingerprint))
	if err != nil {
		t.Fatal(err)
	}
	target, err := reg.Activate(ctx, id, result.Target.Generation)
	if err != nil {
		t.Fatal(err)
	}
	return target
}

func TestBeginRegistrationIsAtomicPendingThenActive(t *testing.T) {
	reg, ctx := openTestRegistry(t)
	root := t.TempDir()

	result, err := reg.BeginRegistration(ctx, params("ws-1", root, "fp-1"))
	if err != nil {
		t.Fatal(err)
	}
	if !result.Created {
		t.Fatal("expected a fresh registration to be created")
	}
	if result.Target.LifecycleState != LifecyclePending {
		t.Fatalf("lifecycle_state = %q, want pending", result.Target.LifecycleState)
	}
	if _, err := reg.Lookup(ctx, "ws-1"); !errors.Is(err, ErrWorkspacePending) {
		t.Fatalf("lookup of a pending entry = %v, want ErrWorkspacePending", err)
	}

	activated, err := reg.Activate(ctx, "ws-1", result.Target.Generation)
	if err != nil {
		t.Fatal(err)
	}
	if activated.LifecycleState != LifecycleActive {
		t.Fatalf("lifecycle_state after activate = %q, want active", activated.LifecycleState)
	}
	found, err := reg.Lookup(ctx, "ws-1")
	if err != nil {
		t.Fatal(err)
	}
	if found.CanonicalRoot != root {
		t.Fatalf("canonical_root = %q, want %q", found.CanonicalRoot, root)
	}
}

func TestActivateRejectsAGenerationConflict(t *testing.T) {
	reg, ctx := openTestRegistry(t)
	root := t.TempDir()
	result, err := reg.BeginRegistration(ctx, params("ws-1", root, "fp-1"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := reg.Activate(ctx, "ws-1", result.Target.Generation+1); !errors.Is(err, ErrGenerationConflict) {
		t.Fatalf("activate with wrong generation = %v, want ErrGenerationConflict", err)
	}
}

func TestBeginRegistrationIsIdempotentFromTheSameRoot(t *testing.T) {
	reg, ctx := openTestRegistry(t)
	root := t.TempDir()
	registerAndActivate(t, reg, ctx, "ws-1", root, "fp-1")

	result, err := reg.BeginRegistration(ctx, params("ws-1", root, "fp-1"))
	if err != nil {
		t.Fatal(err)
	}
	if result.Created || result.Moved {
		t.Fatalf("idempotent re-registration reported Created=%v Moved=%v", result.Created, result.Moved)
	}
	if result.Target.LifecycleState != LifecycleActive {
		t.Fatalf("lifecycle_state after idempotent re-registration = %q, want active", result.Target.LifecycleState)
	}
}

func TestMoveReconciliationUpdatesRootWhenThePriorRootIsGone(t *testing.T) {
	reg, ctx := openTestRegistry(t)
	parent := t.TempDir()
	originalRoot := filepath.Join(parent, "original")
	if err := os.MkdirAll(originalRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	registerAndActivate(t, reg, ctx, "ws-1", originalRoot, "fp-1")

	movedRoot := filepath.Join(parent, "moved")
	if err := os.Rename(originalRoot, movedRoot); err != nil {
		t.Fatal(err)
	}

	result, err := reg.BeginRegistration(ctx, params("ws-1", movedRoot, "fp-1"))
	if err != nil {
		t.Fatal(err)
	}
	if !result.Moved {
		t.Fatal("expected move reconciliation to be reported")
	}
	if _, err := reg.Activate(ctx, "ws-1", result.Target.Generation); err != nil {
		t.Fatal(err)
	}
	found, err := reg.Lookup(ctx, "ws-1")
	if err != nil {
		t.Fatal(err)
	}
	if found.CanonicalRoot != movedRoot {
		t.Fatalf("canonical_root after move = %q, want %q", found.CanonicalRoot, movedRoot)
	}
}

func TestCopyWithoutForkFailsClosedWithIdentityConflict(t *testing.T) {
	reg, ctx := openTestRegistry(t)
	originalRoot := t.TempDir()
	registerAndActivate(t, reg, ctx, "ws-1", originalRoot, "fp-1")

	copyRoot := t.TempDir()
	if _, err := reg.BeginRegistration(ctx, params("ws-1", copyRoot, "fp-1")); !errors.Is(err, ErrWorkspaceIdentityConflict) {
		t.Fatalf("registering a copy at a second still-present root = %v, want ErrWorkspaceIdentityConflict", err)
	}
	// The original registration must be untouched by the rejected attempt.
	found, err := reg.Lookup(ctx, "ws-1")
	if err != nil {
		t.Fatal(err)
	}
	if found.CanonicalRoot != originalRoot {
		t.Fatalf("canonical_root after rejected copy = %q, want %q", found.CanonicalRoot, originalRoot)
	}
}

func TestForkCreatesAnIndependentIdentityWithProvenance(t *testing.T) {
	reg, ctx := openTestRegistry(t)
	sourceRoot := t.TempDir()
	registerAndActivate(t, reg, ctx, "ws-source", sourceRoot, "fp-1")

	forkRoot := t.TempDir()
	result, err := reg.Fork(ctx, "ws-source", params("ws-fork", forkRoot, "fp-2"))
	if err != nil {
		t.Fatal(err)
	}
	if result.Target.ForkOfWorkspaceID != "ws-source" {
		t.Fatalf("fork_of_workspace_id = %q, want ws-source", result.Target.ForkOfWorkspaceID)
	}
	if _, err := reg.Activate(ctx, "ws-fork", result.Target.Generation); err != nil {
		t.Fatal(err)
	}

	// Both identities are independently routable.
	if _, err := reg.Lookup(ctx, "ws-source"); err != nil {
		t.Fatal(err)
	}
	forked, err := reg.Lookup(ctx, "ws-fork")
	if err != nil {
		t.Fatal(err)
	}
	if forked.CanonicalRoot != forkRoot {
		t.Fatalf("forked canonical_root = %q, want %q", forked.CanonicalRoot, forkRoot)
	}
}

func TestForkFailsWhenTheSourceIsNotActive(t *testing.T) {
	reg, ctx := openTestRegistry(t)
	if _, err := reg.Fork(ctx, "does-not-exist", params("ws-fork", t.TempDir(), "fp-1")); !errors.Is(err, ErrForkSourceUnavailable) {
		t.Fatalf("fork from an unknown source = %v, want ErrForkSourceUnavailable", err)
	}
}

func TestNestedWorkspacesRegisterIndependently(t *testing.T) {
	reg, ctx := openTestRegistry(t)
	parentRoot := t.TempDir()
	childRoot := filepath.Join(parentRoot, "nested-child")
	if err := os.MkdirAll(childRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	registerAndActivate(t, reg, ctx, "ws-parent", parentRoot, "fp-parent")
	registerAndActivate(t, reg, ctx, "ws-child", childRoot, "fp-child")

	parent, err := reg.Lookup(ctx, "ws-parent")
	if err != nil {
		t.Fatal(err)
	}
	child, err := reg.Lookup(ctx, "ws-child")
	if err != nil {
		t.Fatal(err)
	}
	if parent.CanonicalRoot == child.CanonicalRoot {
		t.Fatal("nested workspaces resolved to the same canonical root")
	}
}

func TestSymlinkedRootCanonicalizesToTheSameIdentity(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlinks are not exercised on windows")
	}
	reg, ctx := openTestRegistry(t)
	parent := t.TempDir()
	realRoot := filepath.Join(parent, "real")
	if err := os.MkdirAll(realRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	linkRoot := filepath.Join(parent, "link")
	if err := os.Symlink(realRoot, linkRoot); err != nil {
		t.Fatal(err)
	}

	canonicalViaReal, err := CanonicalizeRoot(realRoot)
	if err != nil {
		t.Fatal(err)
	}
	canonicalViaLink, err := CanonicalizeRoot(linkRoot)
	if err != nil {
		t.Fatal(err)
	}
	if canonicalViaReal != canonicalViaLink {
		t.Fatalf("symlinked root canonicalized to %q, want %q", canonicalViaLink, canonicalViaReal)
	}

	registerAndActivate(t, reg, ctx, "ws-1", canonicalViaReal, "fp-1")
	// Registering again via the symlink's canonical form must be the same identity, not a conflict.
	result, err := reg.BeginRegistration(ctx, params("ws-1", canonicalViaLink, "fp-1"))
	if err != nil {
		t.Fatal(err)
	}
	if result.Created || result.Moved {
		t.Fatalf("re-registration via a symlinked path reported Created=%v Moved=%v", result.Created, result.Moved)
	}
}

func TestLookupReturnsUnavailableWhenTheCanonicalRootIsGone(t *testing.T) {
	reg, ctx := openTestRegistry(t)
	root := filepath.Join(t.TempDir(), "will-be-removed")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	registerAndActivate(t, reg, ctx, "ws-1", root, "fp-1")
	if err := os.RemoveAll(root); err != nil {
		t.Fatal(err)
	}
	if _, err := reg.Lookup(ctx, "ws-1"); !errors.Is(err, ErrWorkspaceUnavailable) {
		t.Fatalf("lookup after root removal = %v, want ErrWorkspaceUnavailable", err)
	}
}

func TestLookupReturnsNotFoundForAnUnknownID(t *testing.T) {
	reg, ctx := openTestRegistry(t)
	if _, err := reg.Lookup(ctx, "does-not-exist"); !errors.Is(err, ErrWorkspaceNotFound) {
		t.Fatalf("lookup of an unknown id = %v, want ErrWorkspaceNotFound", err)
	}
}

func TestCheckFingerprintDetectsDrift(t *testing.T) {
	reg, ctx := openTestRegistry(t)
	root := t.TempDir()
	registerAndActivate(t, reg, ctx, "ws-1", root, "fp-1")

	if err := reg.CheckFingerprint(ctx, "ws-1", "fp-1"); err != nil {
		t.Fatalf("matching fingerprint = %v, want nil", err)
	}
	if err := reg.CheckFingerprint(ctx, "ws-1", "fp-drifted"); !errors.Is(err, ErrWorkspaceRegistryConflict) {
		t.Fatalf("drifted fingerprint = %v, want ErrWorkspaceRegistryConflict", err)
	}
}

func TestUnregisterRemovesTheEntry(t *testing.T) {
	reg, ctx := openTestRegistry(t)
	root := t.TempDir()
	target := registerAndActivate(t, reg, ctx, "ws-1", root, "fp-1")

	if err := reg.Unregister(ctx, "ws-1", target.Generation); err != nil {
		t.Fatal(err)
	}
	if _, err := reg.Lookup(ctx, "ws-1"); !errors.Is(err, ErrWorkspaceNotFound) {
		t.Fatalf("lookup after unregister = %v, want ErrWorkspaceNotFound", err)
	}
	// Re-registering the same id afterward is a fresh registration, not an error.
	result, err := reg.BeginRegistration(ctx, params("ws-1", root, "fp-2"))
	if err != nil {
		t.Fatal(err)
	}
	if !result.Created {
		t.Fatal("expected re-registration after unregister to be a fresh entry")
	}
}

func TestUnregisterRejectsAGenerationConflict(t *testing.T) {
	reg, ctx := openTestRegistry(t)
	root := t.TempDir()
	target := registerAndActivate(t, reg, ctx, "ws-1", root, "fp-1")
	if err := reg.Unregister(ctx, "ws-1", target.Generation+1); !errors.Is(err, ErrGenerationConflict) {
		t.Fatalf("unregister with wrong generation = %v, want ErrGenerationConflict", err)
	}
}

func TestOpenSetsRestrictivePermissions(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX permission bits are not exercised on windows")
	}
	path := filepath.Join(t.TempDir(), "nested", "registry.db")
	reg, ctx := func() (*Registry, context.Context) {
		ctx := context.Background()
		registry, err := Open(ctx, path)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = registry.Close() })
		return registry, ctx
	}()
	_ = ctx
	_ = reg

	directoryInfo, err := os.Stat(filepath.Dir(path))
	if err != nil {
		t.Fatal(err)
	}
	if mode := directoryInfo.Mode().Perm(); mode != 0o700 {
		t.Fatalf("registry directory mode = %v, want 0700", mode)
	}
	fileInfo, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if mode := fileInfo.Mode().Perm(); mode != 0o600 {
		t.Fatalf("registry file mode = %v, want 0600", mode)
	}
}
