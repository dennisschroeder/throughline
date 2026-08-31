// Package dashboard is the read-only live dashboard surface for one running daemon: a
// single-use login-link -> session-cookie auth flow, and an SSE endpoint that streams
// per-workspace work-item state and live claim presence. It never mutates domain state; all
// writes remain agent-executed through the existing MCP tools in internal/mcp.
package dashboard

import "sync"

// Hub is the in-process invalidation hub the MCP write path (see
// internal/mcp.Invalidator) fires into after a workspace-scoped write commits, and the SSE
// handler subscribes to for push updates. It carries no payload — a signal only means "this
// workspace's state may have changed, re-read it" — matching the pattern in poll-tergeist's
// internal/live/hub.go. Kept deliberately dependency-free (sync only) so it can be
// constructed before the router/store and passed into both internal/mcp and this package.
type Hub struct {
	mu   sync.RWMutex
	subs map[string]map[chan struct{}]struct{} // workspace_id -> subscribers
}

// NewHub constructs an empty Hub.
func NewHub() *Hub {
	return &Hub{subs: make(map[string]map[chan struct{}]struct{})}
}

// Subscribe registers a new invalidation channel for workspaceID. A receive from the
// channel means "re-read this workspace's state"; it carries no payload. Call unsubscribe
// to deregister and let the channel be garbage collected.
func (h *Hub) Subscribe(workspaceID string) (ch chan struct{}, unsubscribe func()) {
	ch = make(chan struct{}, 1)

	h.mu.Lock()
	if h.subs[workspaceID] == nil {
		h.subs[workspaceID] = make(map[chan struct{}]struct{})
	}
	h.subs[workspaceID][ch] = struct{}{}
	h.mu.Unlock()

	return ch, func() {
		h.mu.Lock()
		delete(h.subs[workspaceID], ch)
		if len(h.subs[workspaceID]) == 0 {
			delete(h.subs, workspaceID)
		}
		h.mu.Unlock()
		close(ch)
	}
}

// Invalidate marks workspaceID as changed for every current subscriber. It never blocks: a
// subscriber already marked dirty (an unconsumed pending signal) is left as-is, since the
// subscriber re-reads current state regardless of how many writes landed since its last
// read. This is intentional coalescing, matching internal/mcp.Invalidator's contract that
// implementations must not block the write path.
func (h *Hub) Invalidate(workspaceID string) {
	h.mu.RLock()
	defer h.mu.RUnlock()
	for ch := range h.subs[workspaceID] {
		select {
		case ch <- struct{}{}:
		default:
		}
	}
}
