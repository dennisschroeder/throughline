package router

import (
	"container/list"
	"context"
	"fmt"
	"strings"
	"sync"
	"unicode"

	"golang.org/x/sync/singleflight"

	"github.com/dennisschroeder/throughline/internal/app"
	"github.com/dennisschroeder/throughline/internal/ports"
	"github.com/dennisschroeder/throughline/internal/registry"
)

const (
	// maxWorkspaceIDLength bounds ErrWorkspaceInvalid input; it is generous relative to a
	// UUIDv7 string and exists only to reject obviously malformed input before it reaches
	// the registry.
	maxWorkspaceIDLength = 256

	defaultMaxRuntimes = 64
)

// Registry is the subset of *registry.Registry the router depends on, so tests can supply
// a fake without a real SQLite file.
type Registry interface {
	Lookup(ctx context.Context, workspaceID string) (registry.WorkspaceTarget, error)
}

// Router is the single deep WorkspaceRouter the MCP adapter depends on: resolve a
// required workspace_id to an application Service for the current request. It performs a
// fresh registry lookup on every call — there is no reload or invalidation to manage — and
// caches constructed runtimes keyed by (workspace_id, target generation) so a repeated
// resolution for an unchanged target is cheap. A generation change (move, relocate, or any
// registry mutation that bumps it) transparently replaces the cached runtime.
type Router struct {
	registry    Registry
	providers   *ProviderManager
	ids         ports.IDGenerator
	clock       ports.Clock
	maxRuntimes int

	mu       sync.Mutex
	runtimes map[string]*runtimeEntry
	order    *list.List // most-recently-used at the back; front is the eviction candidate

	group singleflight.Group
}

type runtimeEntry struct {
	service    *app.Service
	generation int64
	close      func() error
	element    *list.Element // this workspace_id's node in Router.order
}

// New constructs a Router. maxRuntimes bounds how many workspace runtimes stay warm at
// once; 0 uses a sane default. It is never zero in production and always a positive bound
// in tests that exercise eviction.
func New(reg Registry, providers *ProviderManager, ids ports.IDGenerator, clock ports.Clock, maxRuntimes int) *Router {
	if maxRuntimes <= 0 {
		maxRuntimes = defaultMaxRuntimes
	}
	return &Router{
		registry:    reg,
		providers:   providers,
		ids:         ids,
		clock:       clock,
		maxRuntimes: maxRuntimes,
		runtimes:    make(map[string]*runtimeEntry),
		order:       list.New(),
	}
}

// Service resolves workspaceID to an application Service. It fails closed: a missing,
// malformed, unknown, pending, unavailable, or conflicting identity returns the matching
// sentinel error from this package or the registry package, and never falls back to
// another workspace, provider, or cached state from a different identity.
func (r *Router) Service(ctx context.Context, workspaceID string) (*app.Service, error) {
	if err := validateWorkspaceID(workspaceID); err != nil {
		return nil, err
	}

	target, err := r.registry.Lookup(ctx, workspaceID)
	if err != nil {
		return nil, err
	}

	if entry, ok := r.lookupCached(workspaceID, target.Generation); ok {
		return entry.service, nil
	}

	result, err, _ := r.group.Do(workspaceID, func() (any, error) {
		if entry, ok := r.lookupCached(workspaceID, target.Generation); ok {
			return entry, nil
		}
		provider, err := r.providers.Provider(target.ProviderKind)
		if err != nil {
			return nil, err
		}
		handle, err := provider.Open(ctx, target)
		if err != nil {
			return nil, err
		}
		entry := &runtimeEntry{
			service:    app.NewService(handle.Store, r.ids, r.clock),
			generation: target.Generation,
			close:      handle.Close,
		}
		r.store(workspaceID, entry)
		return entry, nil
	})
	if err != nil {
		return nil, err
	}
	return result.(*runtimeEntry).service, nil
}

func (r *Router) lookupCached(workspaceID string, generation int64) (*runtimeEntry, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	entry, ok := r.runtimes[workspaceID]
	if !ok || entry.generation != generation {
		return nil, false
	}
	r.order.MoveToBack(entry.element)
	return entry, true
}

// store installs a freshly constructed runtime, replacing and closing any stale-generation
// entry for the same workspace_id, then evicts the least-recently-used entry if the router
// is over its bound. A failure to open one target never touches any other workspace_id's
// entry.
func (r *Router) store(workspaceID string, entry *runtimeEntry) {
	r.mu.Lock()
	stale, hadStale := r.runtimes[workspaceID]
	if hadStale {
		r.order.Remove(stale.element)
	}
	entry.element = r.order.PushBack(workspaceID)
	r.runtimes[workspaceID] = entry
	var toEvict []*runtimeEntry
	for len(r.runtimes) > r.maxRuntimes {
		front := r.order.Front()
		if front == nil {
			break
		}
		evictedID := front.Value.(string)
		if evictedID == workspaceID {
			break
		}
		evicted := r.runtimes[evictedID]
		delete(r.runtimes, evictedID)
		r.order.Remove(front)
		toEvict = append(toEvict, evicted)
	}
	r.mu.Unlock()

	if hadStale && stale.close != nil {
		_ = stale.close()
	}
	for _, evicted := range toEvict {
		if evicted.close != nil {
			_ = evicted.close()
		}
	}
}

// Close drains the router: every cached runtime's underlying handle is closed. A
// provider's Close typically blocks until connections currently in use are returned, so
// in-flight work finishes before its handle is released.
func (r *Router) Close() error {
	r.mu.Lock()
	entries := make([]*runtimeEntry, 0, len(r.runtimes))
	for id, entry := range r.runtimes {
		entries = append(entries, entry)
		delete(r.runtimes, id)
	}
	r.order.Init()
	r.mu.Unlock()

	var firstErr error
	for _, entry := range entries {
		if entry.close == nil {
			continue
		}
		if err := entry.close(); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

func validateWorkspaceID(workspaceID string) error {
	trimmed := strings.TrimSpace(workspaceID)
	if trimmed == "" {
		return ErrWorkspaceRequired
	}
	if len(trimmed) > maxWorkspaceIDLength {
		return fmt.Errorf("%w: workspace_id exceeds %d characters", ErrWorkspaceInvalid, maxWorkspaceIDLength)
	}
	for _, r := range trimmed {
		if unicode.IsControl(r) {
			return fmt.Errorf("%w: workspace_id contains a control character", ErrWorkspaceInvalid)
		}
	}
	return nil
}
