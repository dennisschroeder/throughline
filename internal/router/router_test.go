package router

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/dennisschroeder/throughline/internal/app"
	"github.com/dennisschroeder/throughline/internal/config"
	"github.com/dennisschroeder/throughline/internal/domain/work"
	"github.com/dennisschroeder/throughline/internal/registry"
	throughlinesqlite "github.com/dennisschroeder/throughline/internal/sqlite"
)

type fakeIDs struct {
	mu   sync.Mutex
	next int
}

func (f *fakeIDs) New() (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.next++
	return fmt.Sprintf("id-%d", f.next), nil
}

type fakeClock struct{}

func (fakeClock) Now() time.Time { return time.Date(2026, 8, 25, 12, 0, 0, 0, time.UTC) }

// fakeRegistry lets tests control exactly what Lookup returns, including error injection,
// without a real SQLite-backed registry.Registry.
type fakeRegistry struct {
	mu      sync.Mutex
	targets map[string]registry.WorkspaceTarget
	errs    map[string]error
	calls   int
}

func newFakeRegistry() *fakeRegistry {
	return &fakeRegistry{targets: map[string]registry.WorkspaceTarget{}, errs: map[string]error{}}
}

func (r *fakeRegistry) set(target registry.WorkspaceTarget) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.targets[target.WorkspaceID] = target
}

func (r *fakeRegistry) fail(workspaceID string, err error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.errs[workspaceID] = err
}

func (r *fakeRegistry) Lookup(_ context.Context, workspaceID string) (registry.WorkspaceTarget, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.calls++
	if err, ok := r.errs[workspaceID]; ok {
		return registry.WorkspaceTarget{}, err
	}
	target, ok := r.targets[workspaceID]
	if !ok {
		return registry.WorkspaceTarget{}, registry.ErrWorkspaceNotFound
	}
	return target, nil
}

// fakeSharedProvider is one PersistenceProvider Go value that serves many WorkspaceTargets
// from an internal map keyed by workspace_id, standing in for a provider that would
// otherwise multiplex many workspaces through one shared backend (e.g. a Postgres pool).
// It exists to prove the router's ProviderManager/Router layering makes no
// workspace-equals-database assumption: the very same *fakeSharedProvider instance, not a
// fresh one per target, resolves every workspace_id below.
type fakeSharedProvider struct {
	mu      sync.Mutex
	opened  map[string]*throughlinesqlite.Database
	openLog []string
	dir     string
}

func newFakeSharedProvider(dir string) *fakeSharedProvider {
	return &fakeSharedProvider{opened: map[string]*throughlinesqlite.Database{}, dir: dir}
}

func (p *fakeSharedProvider) Kind() registry.ProviderKind { return "fake_shared" }

func (p *fakeSharedProvider) Open(ctx context.Context, target registry.WorkspaceTarget) (ProviderHandle, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.openLog = append(p.openLog, target.WorkspaceID)
	database, ok := p.opened[target.WorkspaceID]
	if !ok {
		var err error
		database, err = throughlinesqlite.Open(ctx, filepath.Join(p.dir, target.WorkspaceID+".db"))
		if err != nil {
			return ProviderHandle{}, err
		}
		if err := database.Migrate(ctx); err != nil {
			return ProviderHandle{}, err
		}
		p.opened[target.WorkspaceID] = database
	}
	return ProviderHandle{Store: database.Store()}, nil
}

func target(id string, generation int64) registry.WorkspaceTarget {
	return registry.WorkspaceTarget{
		WorkspaceID:     id,
		ProviderKind:    "fake_shared",
		ProviderLocator: id,
		CanonicalRoot:   "/unused/in/these/fakes",
		Generation:      generation,
		LifecycleState:  registry.LifecycleActive,
	}
}

func createObjective(t *testing.T, ctx context.Context, service *app.Service, key string) work.Objective {
	t.Helper()
	if _, err := service.RegisterActor(ctx, app.RegisterActorCommand{
		Actor:          work.Actor{ID: "agent:test", Kind: work.ActorTypeAgent, DisplayName: "test"},
		IdempotencyKey: "register-" + key,
	}); err != nil {
		t.Fatal(err)
	}
	objective, err := service.CreateObjective(ctx, app.CreateObjectiveCommand{
		ActorID: "agent:test", IdempotencyKey: "create-" + key,
		Key: key, Title: key, DesiredOutcome: "isolation probe", Phase: work.ObjectiveIdea,
	})
	if err != nil {
		t.Fatal(err)
	}
	return objective
}

func TestServiceResolvesWorkspaceIDToAService(t *testing.T) {
	ctx := context.Background()
	reg := newFakeRegistry()
	reg.set(target("ws-a", 1))
	provider := newFakeSharedProvider(t.TempDir())
	router := New(reg, NewProviderManager(provider), &fakeIDs{}, fakeClock{}, 0)
	t.Cleanup(func() { _ = router.Close() })

	service, err := router.Service(ctx, "ws-a")
	if err != nil {
		t.Fatal(err)
	}
	if service == nil {
		t.Fatal("expected a non-nil service")
	}
}

func TestServiceFailsClosedForMissingMalformedAndUnknownIdentities(t *testing.T) {
	ctx := context.Background()
	reg := newFakeRegistry()
	router := New(reg, NewProviderManager(newFakeSharedProvider(t.TempDir())), &fakeIDs{}, fakeClock{}, 0)
	t.Cleanup(func() { _ = router.Close() })

	if _, err := router.Service(ctx, ""); !errors.Is(err, ErrWorkspaceRequired) {
		t.Fatalf("empty workspace_id = %v, want ErrWorkspaceRequired", err)
	}
	if _, err := router.Service(ctx, "bad\x00id"); !errors.Is(err, ErrWorkspaceInvalid) {
		t.Fatalf("malformed workspace_id = %v, want ErrWorkspaceInvalid", err)
	}
	if _, err := router.Service(ctx, "does-not-exist"); !errors.Is(err, registry.ErrWorkspaceNotFound) {
		t.Fatalf("unknown workspace_id = %v, want ErrWorkspaceNotFound", err)
	}
}

func TestServicePropagatesRegistryFailureModesUnchanged(t *testing.T) {
	ctx := context.Background()
	reg := newFakeRegistry()
	reg.fail("pending-ws", registry.ErrWorkspacePending)
	reg.fail("gone-ws", registry.ErrWorkspaceUnavailable)
	reg.fail("conflict-ws", registry.ErrWorkspaceRegistryConflict)
	router := New(reg, NewProviderManager(newFakeSharedProvider(t.TempDir())), &fakeIDs{}, fakeClock{}, 0)
	t.Cleanup(func() { _ = router.Close() })

	for id, want := range map[string]error{
		"pending-ws":  registry.ErrWorkspacePending,
		"gone-ws":     registry.ErrWorkspaceUnavailable,
		"conflict-ws": registry.ErrWorkspaceRegistryConflict,
	} {
		if _, err := router.Service(ctx, id); !errors.Is(err, want) {
			t.Fatalf("%s: got %v, want %v", id, err, want)
		}
	}
}

func TestServiceRejectsAnUnsupportedProviderKind(t *testing.T) {
	ctx := context.Background()
	reg := newFakeRegistry()
	target := target("ws-a", 1)
	target.ProviderKind = registry.ProviderKind("postgres_not_registered")
	reg.set(target)
	router := New(reg, NewProviderManager(newFakeSharedProvider(t.TempDir())), &fakeIDs{}, fakeClock{}, 0)
	t.Cleanup(func() { _ = router.Close() })

	if _, err := router.Service(ctx, "ws-a"); !errors.Is(err, ErrProviderUnsupported) {
		t.Fatalf("unsupported provider kind = %v, want ErrProviderUnsupported", err)
	}
}

func TestServiceCachesByGenerationAndReplacesOnChange(t *testing.T) {
	ctx := context.Background()
	reg := newFakeRegistry()
	reg.set(target("ws-a", 1))
	provider := newFakeSharedProvider(t.TempDir())
	router := New(reg, NewProviderManager(provider), &fakeIDs{}, fakeClock{}, 0)
	t.Cleanup(func() { _ = router.Close() })

	if _, err := router.Service(ctx, "ws-a"); err != nil {
		t.Fatal(err)
	}
	if _, err := router.Service(ctx, "ws-a"); err != nil {
		t.Fatal(err)
	}
	provider.mu.Lock()
	opensAtGenerationOne := len(provider.openLog)
	provider.mu.Unlock()
	if opensAtGenerationOne != 1 {
		t.Fatalf("provider.Open called %d times for an unchanged target, want 1", opensAtGenerationOne)
	}

	reg.set(target("ws-a", 2))
	if _, err := router.Service(ctx, "ws-a"); err != nil {
		t.Fatal(err)
	}
	provider.mu.Lock()
	opensAfterGenerationBump := len(provider.openLog)
	provider.mu.Unlock()
	if opensAfterGenerationBump != 2 {
		t.Fatalf("provider.Open called %d times after a generation bump, want 2", opensAfterGenerationBump)
	}
}

func TestConcurrentResolutionsForTheSameWorkspaceOpenTheProviderOnce(t *testing.T) {
	ctx := context.Background()
	reg := newFakeRegistry()
	reg.set(target("ws-a", 1))
	provider := newFakeSharedProvider(t.TempDir())
	router := New(reg, NewProviderManager(provider), &fakeIDs{}, fakeClock{}, 0)
	t.Cleanup(func() { _ = router.Close() })

	var wg sync.WaitGroup
	errs := make([]error, 20)
	for i := range errs {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_, errs[i] = router.Service(ctx, "ws-a")
		}(i)
	}
	wg.Wait()
	for _, err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	provider.mu.Lock()
	opens := len(provider.openLog)
	provider.mu.Unlock()
	if opens != 1 {
		t.Fatalf("provider.Open called %d times for 20 concurrent resolutions of one workspace, want 1", opens)
	}
}

func TestOneWorkspaceFailureDoesNotAffectAnother(t *testing.T) {
	ctx := context.Background()
	reg := newFakeRegistry()
	reg.set(target("ws-good", 1))
	reg.fail("ws-bad", registry.ErrWorkspaceUnavailable)
	router := New(reg, NewProviderManager(newFakeSharedProvider(t.TempDir())), &fakeIDs{}, fakeClock{}, 0)
	t.Cleanup(func() { _ = router.Close() })

	if _, err := router.Service(ctx, "ws-bad"); !errors.Is(err, registry.ErrWorkspaceUnavailable) {
		t.Fatalf("ws-bad = %v, want ErrWorkspaceUnavailable", err)
	}
	if _, err := router.Service(ctx, "ws-good"); err != nil {
		t.Fatalf("ws-good after ws-bad failed = %v, want nil", err)
	}
}

func TestRouterEvictsLeastRecentlyUsedRuntimesWhenBounded(t *testing.T) {
	ctx := context.Background()
	reg := newFakeRegistry()
	reg.set(target("ws-a", 1))
	reg.set(target("ws-b", 1))
	reg.set(target("ws-c", 1))
	provider := newFakeSharedProvider(t.TempDir())
	router := New(reg, NewProviderManager(provider), &fakeIDs{}, fakeClock{}, 2)
	t.Cleanup(func() { _ = router.Close() })

	if _, err := router.Service(ctx, "ws-a"); err != nil {
		t.Fatal(err)
	}
	if _, err := router.Service(ctx, "ws-b"); err != nil {
		t.Fatal(err)
	}
	if _, err := router.Service(ctx, "ws-c"); err != nil { // evicts ws-a (least recently used)
		t.Fatal(err)
	}
	if _, err := router.Service(ctx, "ws-a"); err != nil { // reopens ws-a
		t.Fatal(err)
	}

	provider.mu.Lock()
	opensOfA := 0
	for _, id := range provider.openLog {
		if id == "ws-a" {
			opensOfA++
		}
	}
	provider.mu.Unlock()
	if opensOfA != 2 {
		t.Fatalf("ws-a opened %d times, want 2 (evicted once and reopened)", opensOfA)
	}
}

func TestFakeSharedProviderIsolatesWorkspacesUnderOneProviderInstance(t *testing.T) {
	ctx := context.Background()
	reg := newFakeRegistry()
	reg.set(target("ws-a", 1))
	reg.set(target("ws-b", 1))
	provider := newFakeSharedProvider(t.TempDir())
	router := New(reg, NewProviderManager(provider), &fakeIDs{}, fakeClock{}, 0)
	t.Cleanup(func() { _ = router.Close() })

	serviceA, err := router.Service(ctx, "ws-a")
	if err != nil {
		t.Fatal(err)
	}
	serviceB, err := router.Service(ctx, "ws-b")
	if err != nil {
		t.Fatal(err)
	}

	createObjective(t, ctx, serviceA, "OBJ-A")
	createObjective(t, ctx, serviceB, "OBJ-B")

	itemsA, err := serviceA.ListWorkItems(ctx)
	if err != nil {
		t.Fatal(err)
	}
	itemsB, err := serviceB.ListWorkItems(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(itemsA) != 0 || len(itemsB) != 0 {
		t.Fatalf("unexpected pre-existing work items: A=%d B=%d", len(itemsA), len(itemsB))
	}

	// The decisive assertion: one *fakeSharedProvider instance served both targets.
	provider.mu.Lock()
	defer provider.mu.Unlock()
	if len(provider.opened) != 2 {
		t.Fatalf("provider opened %d distinct stores, want 2 (one per workspace, from one provider instance)", len(provider.opened))
	}
	if provider.opened["ws-a"] == provider.opened["ws-b"] {
		t.Fatal("ws-a and ws-b resolved to the same underlying database handle")
	}
}

func TestCloseDrainsAndClosesEveryRuntimeUsingTheRealSQLiteProvider(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	workspace, _, err := config.Initialize(dir, "", "ws-a")
	if err != nil {
		t.Fatal(err)
	}

	reg := newFakeRegistry()
	sqliteTarget := target(workspace.Config.WorkspaceID, 1)
	sqliteTarget.ProviderKind = registry.ProviderSQLite
	sqliteTarget.CanonicalRoot = dir
	reg.set(sqliteTarget)

	router := New(reg, NewProviderManager(SQLiteProvider{}), &fakeIDs{}, fakeClock{}, 0)
	if _, err := router.Service(ctx, workspace.Config.WorkspaceID); err != nil {
		t.Fatal(err)
	}
	if err := router.Close(); err != nil {
		t.Fatal(err)
	}
}
