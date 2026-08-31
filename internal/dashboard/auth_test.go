package dashboard

import (
	"errors"
	"testing"
	"time"
)

func TestMintExchangeReturnsSessionForWorkspaceAndActor(t *testing.T) {
	store := NewAuthStore(func() time.Time { return time.Unix(1000, 0) })

	token, expiresAt, err := store.MintLoginToken("ws-1", "agent:dashboard-worker")
	if err != nil {
		t.Fatalf("mint: %v", err)
	}
	if token == "" {
		t.Fatal("mint returned empty token")
	}
	wantExpiry := time.Unix(1000, 0).Add(LoginTokenTTL).UTC()
	if !expiresAt.Equal(wantExpiry) {
		t.Fatalf("expiresAt = %v, want %v", expiresAt, wantExpiry)
	}

	session, err := store.ExchangeLoginToken(token)
	if err != nil {
		t.Fatalf("exchange: %v", err)
	}
	if session.WorkspaceID != "ws-1" || session.ActorID != "agent:dashboard-worker" {
		t.Fatalf("session = %+v", session)
	}
	if session.ID == "" {
		t.Fatal("session has empty id")
	}

	resolved, err := store.Session(session.ID)
	if err != nil {
		t.Fatalf("resolve session: %v", err)
	}
	if resolved.WorkspaceID != "ws-1" {
		t.Fatalf("resolved session = %+v", resolved)
	}
}

func TestExchangeRejectsReuse(t *testing.T) {
	store := NewAuthStore(func() time.Time { return time.Unix(1000, 0) })
	token, _, err := store.MintLoginToken("ws-1", "agent:a")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.ExchangeLoginToken(token); err != nil {
		t.Fatalf("first exchange: %v", err)
	}
	if _, err := store.ExchangeLoginToken(token); !errors.Is(err, ErrTokenInvalid) {
		t.Fatalf("second exchange err = %v, want ErrTokenInvalid", err)
	}
}

func TestExchangeRejectsUnknownToken(t *testing.T) {
	store := NewAuthStore(nil)
	if _, err := store.ExchangeLoginToken("does-not-exist"); !errors.Is(err, ErrTokenInvalid) {
		t.Fatalf("err = %v, want ErrTokenInvalid", err)
	}
}

func TestExchangeRejectsExpiredToken(t *testing.T) {
	now := time.Unix(1000, 0)
	store := NewAuthStore(func() time.Time { return now })
	token, _, err := store.MintLoginToken("ws-1", "agent:a")
	if err != nil {
		t.Fatal(err)
	}
	now = now.Add(LoginTokenTTL + time.Second)
	if _, err := store.ExchangeLoginToken(token); !errors.Is(err, ErrTokenExpired) {
		t.Fatalf("err = %v, want ErrTokenExpired", err)
	}
}

func TestSessionRejectsExpiredSession(t *testing.T) {
	now := time.Unix(1000, 0)
	store := NewAuthStore(func() time.Time { return now })
	token, _, err := store.MintLoginToken("ws-1", "agent:a")
	if err != nil {
		t.Fatal(err)
	}
	session, err := store.ExchangeLoginToken(token)
	if err != nil {
		t.Fatal(err)
	}
	now = now.Add(SessionTTL + time.Second)
	if _, err := store.Session(session.ID); !errors.Is(err, ErrSessionInvalid) {
		t.Fatalf("err = %v, want ErrSessionInvalid", err)
	}
}

func TestSessionRejectsUnknownID(t *testing.T) {
	store := NewAuthStore(nil)
	if _, err := store.Session("nope"); !errors.Is(err, ErrSessionInvalid) {
		t.Fatalf("err = %v, want ErrSessionInvalid", err)
	}
}

func TestMintingTwiceProducesIndependentTokens(t *testing.T) {
	store := NewAuthStore(func() time.Time { return time.Unix(1000, 0) })
	tokenA, _, err := store.MintLoginToken("ws-1", "agent:a")
	if err != nil {
		t.Fatal(err)
	}
	tokenB, _, err := store.MintLoginToken("ws-1", "agent:a")
	if err != nil {
		t.Fatal(err)
	}
	if tokenA == tokenB {
		t.Fatal("expected distinct tokens")
	}
	if _, err := store.ExchangeLoginToken(tokenA); err != nil {
		t.Fatalf("exchange tokenA: %v", err)
	}
	// Consuming tokenA must not affect tokenB.
	if _, err := store.ExchangeLoginToken(tokenB); err != nil {
		t.Fatalf("exchange tokenB: %v", err)
	}
}
