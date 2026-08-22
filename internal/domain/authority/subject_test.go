package authority

import (
	"bytes"
	"os"
	"strings"
	"testing"
)

func TestAuthorizationSubjectFixture(t *testing.T) {
	raw := readFixture(t, "testdata/authorization_subject.json")
	wantCanonical := bytes.TrimSpace(readFixture(t, "testdata/authorization_subject.canonical.json"))
	wantHash := strings.TrimSpace(string(readFixture(t, "testdata/authorization_subject.sha256")))

	canonical, err := CanonicalizeSubject(raw)
	if err != nil {
		t.Fatalf("canonicalize subject: %v", err)
	}
	if !bytes.Equal(canonical, wantCanonical) {
		t.Fatalf("canonical subject mismatch\nwant: %s\n got: %s", wantCanonical, canonical)
	}
	hash, err := SubjectHash(raw)
	if err != nil {
		t.Fatalf("hash subject: %v", err)
	}
	if hash != wantHash {
		t.Fatalf("hash mismatch: want %s, got %s", wantHash, hash)
	}
}

func TestAuthorizationSubjectCanonicalizationIgnoresObjectAndSetOrder(t *testing.T) {
	first := readFixture(t, "testdata/authorization_subject.json")
	second := []byte(`{
		"action_type":"tool.install",
		"arguments":[],
		"constraints":{"global_install":false},
		"credential_requirements":[],
		"permissions":["config.write:project","network.read"],
		"scope":{"environment":"project"},
		"target":{"package":"browser-mcp<&>\u2028\u2029","version":"2.1.0"}
	}`)

	firstHash, err := SubjectHash(first)
	if err != nil {
		t.Fatal(err)
	}
	secondHash, err := SubjectHash(second)
	if err != nil {
		t.Fatal(err)
	}
	if firstHash != secondHash {
		t.Fatalf("equivalent subjects differ: %s != %s", firstHash, secondHash)
	}
}

func TestAuthorizationSubjectRejectsDuplicateKeys(t *testing.T) {
	_, err := CanonicalizeSubject([]byte(`{"action_type":"one","action_type":"two"}`))
	if err == nil || !strings.Contains(err.Error(), "duplicate object key") {
		t.Fatalf("expected duplicate-key error, got %v", err)
	}
}

func readFixture(t *testing.T, path string) []byte {
	t.Helper()
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return content
}
