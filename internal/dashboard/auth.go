package dashboard

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"sync"
	"time"
)

// LoginTokenTTL bounds how long a minted login link stays valid before it must be
// exchanged. Short-lived by design: the token is meant to be opened in a browser within
// seconds of being minted by the agent that requested it, not stored or reused.
const LoginTokenTTL = 2 * time.Minute

// SessionTTL bounds how long an exchanged session cookie stays valid. Long enough for a
// dashboard to stay open for a working session; the browser must go through the login-link
// flow again once it expires, since the dashboard has no refresh/renew endpoint of its own.
const SessionTTL = 12 * time.Hour

var (
	// ErrTokenInvalid covers an unknown, already-consumed, or malformed login token.
	ErrTokenInvalid = errors.New("dashboard: login token invalid or already used")
	// ErrTokenExpired covers a well-formed, unused token past its TTL.
	ErrTokenExpired = errors.New("dashboard: login token expired")
	// ErrSessionInvalid covers a missing, unknown, or expired session cookie.
	ErrSessionInvalid = errors.New("dashboard: session invalid or expired")
)

// Session is what a session cookie resolves to: the workspace and actor the login link was
// minted for. A dashboard session is scoped to exactly one workspace for its lifetime —
// there is no cross-workspace switch — matching the mint request that created it.
type Session struct {
	ID          string
	WorkspaceID string
	ActorID     string
	CreatedAt   time.Time
	ExpiresAt   time.Time
}

type loginToken struct {
	workspaceID string
	actorID     string
	expiresAt   time.Time
	used        bool
}

// AuthStore holds single-use login tokens and the sessions they exchange into, entirely
// in-process memory. This matches the validated spike pattern: the daemon is the sole
// authority for these short-lived credentials, nothing here needs to survive a daemon
// restart (a restarted daemon simply requires a fresh login link, same as any other
// in-memory session store), and per-process storage means no additional file or database
// surface to secure. Every method is safe for concurrent use.
type AuthStore struct {
	mu       sync.Mutex
	tokens   map[string]*loginToken
	sessions map[string]*Session
	now      func() time.Time
}

// NewAuthStore constructs an empty AuthStore. now defaults to time.Now; tests may override
// it to control expiry deterministically.
func NewAuthStore(now func() time.Time) *AuthStore {
	if now == nil {
		now = time.Now
	}
	return &AuthStore{
		tokens:   make(map[string]*loginToken),
		sessions: make(map[string]*Session),
		now:      now,
	}
}

// MintLoginToken issues a fresh single-use token for workspaceID/actorID, valid for
// LoginTokenTTL. Every call mints an independent token; minting a second token does not
// invalidate an earlier unused one.
func (s *AuthStore) MintLoginToken(workspaceID, actorID string) (token string, expiresAt time.Time, err error) {
	raw, err := randomOpaqueID()
	if err != nil {
		return "", time.Time{}, fmt.Errorf("mint login token: %w", err)
	}
	expiresAt = s.now().Add(LoginTokenTTL).UTC()

	s.mu.Lock()
	defer s.mu.Unlock()
	s.evictExpiredLocked()
	s.tokens[raw] = &loginToken{workspaceID: workspaceID, actorID: actorID, expiresAt: expiresAt}
	return raw, expiresAt, nil
}

// ExchangeLoginToken consumes token — a second call with the same value always fails, even
// if the first call happened microseconds ago — and returns a fresh Session cookie value on
// success. Consumption and session creation happen under the same lock so no concurrent
// exchange can observe the token as still valid.
func (s *AuthStore) ExchangeLoginToken(token string) (*Session, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	// Deliberately not calling evictExpiredLocked() here: it would delete an expired
	// token before the check below gets a chance to distinguish ErrTokenExpired (a
	// specific, useful diagnostic) from ErrTokenInvalid (unknown/already-used). Expired
	// tokens for other exchanges still get swept by MintLoginToken and Session.

	record, ok := s.tokens[token]
	if !ok {
		return nil, ErrTokenInvalid
	}
	if record.used {
		delete(s.tokens, token) // a reuse attempt on a consumed token gets no more information out of it
		return nil, ErrTokenInvalid
	}
	now := s.now()
	if !now.Before(record.expiresAt) {
		delete(s.tokens, token)
		return nil, ErrTokenExpired
	}
	record.used = true
	delete(s.tokens, token) // single-use: gone the instant it is spent, not just flagged

	sessionID, err := randomOpaqueID()
	if err != nil {
		return nil, fmt.Errorf("create session: %w", err)
	}
	session := &Session{
		ID:          sessionID,
		WorkspaceID: record.workspaceID,
		ActorID:     record.actorID,
		CreatedAt:   now.UTC(),
		ExpiresAt:   now.Add(SessionTTL).UTC(),
	}
	s.sessions[sessionID] = session
	return session, nil
}

// Session resolves a session cookie value to its Session, failing if unknown or expired.
func (s *AuthStore) Session(id string) (*Session, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	session, ok := s.sessions[id]
	if !ok {
		return nil, ErrSessionInvalid
	}
	if !s.now().Before(session.ExpiresAt) {
		delete(s.sessions, id)
		return nil, ErrSessionInvalid
	}
	return session, nil
}

// evictExpiredLocked drops expired tokens and sessions opportunistically on every mutating
// call, so a long-lived daemon process does not accumulate unbounded stale entries without
// needing a background goroutine. Callers must hold s.mu.
func (s *AuthStore) evictExpiredLocked() {
	now := s.now()
	for key, record := range s.tokens {
		if !now.Before(record.expiresAt) {
			delete(s.tokens, key)
		}
	}
	for key, session := range s.sessions {
		if !now.Before(session.ExpiresAt) {
			delete(s.sessions, key)
		}
	}
}

// randomOpaqueID mirrors internal/credential's token generation: 256 bits of
// crypto/rand, hex-encoded.
func randomOpaqueID() (string, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf), nil
}
