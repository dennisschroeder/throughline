package dashboard

import (
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	throughlinerouter "github.com/dennisschroeder/throughline/internal/router"
)

// SessionCookieName is the HttpOnly cookie ExchangeLoginTokenHandler sets and every other
// dashboard route requires. Scoped to CookiePath so it is never sent to /mcp or /health.
const SessionCookieName = "throughline_dashboard_session"

// CookiePath is the path the session cookie is scoped to.
const CookiePath = "/dashboard"

// Config wires a Handlers to the rest of the daemon.
type Config struct {
	// Router resolves workspace_id to an application Service, exactly like the MCP
	// adapter; the dashboard reads through the same seam, never a separate DB handle.
	Router *throughlinerouter.Router
	// Hub is the invalidation hub the MCP write path signals into. Kept wired for future
	// push-based work; the poll-based /api/v1/changes contract this package implements
	// today does not consume it.
	Hub *Hub
	// Auth stores login tokens and sessions. Optional; NewHandlers constructs one from
	// Now if nil.
	Auth *AuthStore
	// AllowedHosts is the same loopback host allow-list daemonhttp.Protect enforces on
	// the bearer-protected routes; the dashboard's cookie-authenticated routes enforce it
	// independently since they sit outside Protect (see internal/cli's routing comment).
	AllowedHosts []string
	// Now returns the current time; defaults to time.Now. Tests override it.
	Now func() time.Time
	// Logger receives dashboard-specific diagnostics. Nil disables logging.
	Logger *slog.Logger
}

// Handlers exposes the dashboard's HTTP handlers. Construct with NewHandlers. Every
// handler here is a GET — per ADR 0027 and this package's own doc comment, the dashboard
// never mutates domain state; it calls no Throughline write/mutation application-service
// method. Every decision the UI supports produces a copyable MCP tool-call prompt for a
// human to paste into an agent session, never a POST.
type Handlers struct {
	router       *throughlinerouter.Router
	hub          *Hub
	auth         *AuthStore
	allowedHosts []string
	now          func() time.Time
	logger       *slog.Logger
}

// NewHandlers builds a Handlers from cfg.
func NewHandlers(cfg Config) *Handlers {
	now := cfg.Now
	if now == nil {
		now = time.Now
	}
	auth := cfg.Auth
	if auth == nil {
		auth = NewAuthStore(now)
	}
	return &Handlers{
		router:       cfg.Router,
		hub:          cfg.Hub,
		auth:         auth,
		allowedHosts: cfg.AllowedHosts,
		now:          now,
		logger:       cfg.Logger,
	}
}

// mintRequest is the JSON body MintLoginTokenHandler accepts.
type mintRequest struct {
	WorkspaceID string `json:"workspace_id"`
	ActorID     string `json:"actor_id"`
}

type mintResponse struct {
	Token     string `json:"token"`
	URL       string `json:"url"`
	ExpiresAt string `json:"expires_at"`
}

// MintLoginTokenHandler mints a single-use login token for the workspace/actor an
// already-authenticated MCP client (holder of the daemon bearer token) names in the JSON
// body. Mount this behind daemonhttp.Protect — it does not check the bearer token itself,
// it relies on the surrounding mux to have already required one, exactly like /mcp does.
func (h *Handlers) MintLoginTokenHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		var req mintRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "invalid request body", http.StatusBadRequest)
			return
		}
		req.WorkspaceID = strings.TrimSpace(req.WorkspaceID)
		req.ActorID = strings.TrimSpace(req.ActorID)
		if req.WorkspaceID == "" || req.ActorID == "" {
			http.Error(w, "workspace_id and actor_id are required", http.StatusBadRequest)
			return
		}
		// Fail fast on an unroutable workspace rather than minting a token that can never
		// be exchanged into a working session.
		if _, err := h.router.Service(r.Context(), req.WorkspaceID); err != nil {
			http.Error(w, fmt.Sprintf("workspace_id not routable: %v", err), http.StatusBadRequest)
			return
		}
		token, expiresAt, err := h.auth.MintLoginToken(req.WorkspaceID, req.ActorID)
		if err != nil {
			h.logError("mint login token", err)
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}
		loginURL := (&url.URL{Scheme: "http", Host: r.Host, Path: "/dashboard/login", RawQuery: "token=" + token}).String()
		writeJSON(w, http.StatusOK, mintResponse{Token: token, URL: loginURL, ExpiresAt: expiresAt.Format(time.RFC3339)})
	})
}

// ExchangeLoginTokenHandler is the GET endpoint a browser opens with ?token=... from the
// minted login link. On success it sets the session cookie and redirects to the dashboard
// page; on failure (unknown, expired, or already-used token) it returns 401 with no cookie.
// It sits outside the bearer-token boundary by design (a browser tab has no bearer token to
// present) but is still host/origin-restricted to loopback, matching the accepted decision.
func (h *Handlers) ExchangeLoginTokenHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !h.hostAllowed(r) {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		token := strings.TrimSpace(r.URL.Query().Get("token"))
		if token == "" {
			http.Error(w, "missing token", http.StatusBadRequest)
			return
		}
		session, err := h.auth.ExchangeLoginToken(token)
		if err != nil {
			http.Error(w, err.Error(), http.StatusUnauthorized)
			return
		}
		// Secure:false is deliberate, not an oversight: this cookie is served only over
		// plain http://127.0.0.1 (the daemon never terminates TLS), and browsers do not
		// reliably apply the localhost secure-context exception to the Secure attribute —
		// confirmed in the TH-VIZ-01-SPIKE PoC. HttpOnly+SameSite=Strict is the real
		// boundary here, not Secure.
		http.SetCookie(w, &http.Cookie{
			Name:     SessionCookieName,
			Value:    session.ID,
			Path:     CookiePath,
			Expires:  session.ExpiresAt,
			HttpOnly: true,
			Secure:   false,
			SameSite: http.SameSiteStrictMode,
		})
		http.Redirect(w, r, "/dashboard/", http.StatusFound)
	})
}

// StaticHandler serves the single self-contained frontend page. It requires no session: the
// page itself carries no workspace data, it only knows how to call the /api/v1/* routes
// (which do require the cookie) once loaded.
func (h *Handlers) StaticHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !h.hostAllowed(r) {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store")
		_, _ = w.Write(indexHTML)
	})
}

func (h *Handlers) sessionFromRequest(r *http.Request) (*Session, error) {
	cookie, err := r.Cookie(SessionCookieName)
	if err != nil {
		return nil, errors.New("missing session cookie")
	}
	session, err := h.auth.Session(cookie.Value)
	if err != nil {
		return nil, err
	}
	return session, nil
}

func (h *Handlers) hostAllowed(r *http.Request) bool {
	host := r.Host
	if h2, _, err := net.SplitHostPort(host); err == nil {
		host = h2
	}
	for _, candidate := range h.allowedHosts {
		if candidateHost, _, err := net.SplitHostPort(candidate); err == nil {
			candidate = candidateHost
		}
		if strings.EqualFold(host, candidate) {
			return true
		}
	}
	return false
}

func (h *Handlers) logError(msg string, err error) {
	if h.logger == nil {
		return
	}
	h.logger.Error("dashboard_"+strings.ReplaceAll(msg, " ", "_"), "error", err)
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}
