package daemon

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/dennisschroeder/throughline/internal/credential"
)

type fakeManager struct {
	restarts    int
	failRestart bool
}

func (m *fakeManager) Start(context.Context) error { return nil }
func (m *fakeManager) Stop(context.Context) error  { return nil }
func (m *fakeManager) Status(context.Context) (Status, error) {
	return Status{Running: true}, nil
}
func (m *fakeManager) Logs(context.Context, int) ([]string, error) { return nil, nil }
func (m *fakeManager) Restart(context.Context) error {
	m.restarts++
	if m.failRestart {
		return errors.New("simulated restart failure")
	}
	return nil
}

func TestRotateCredentialCommitsAndRestartsOnSuccess(t *testing.T) {
	path := filepath.Join(t.TempDir(), "credentials")
	original, err := credential.LoadOrCreate(path)
	if err != nil {
		t.Fatal(err)
	}
	manager := &fakeManager{}
	verified := false
	newToken, err := RotateCredential(context.Background(), path, manager, func(context.Context) error {
		verified = true
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if newToken == original {
		t.Fatal("rotation did not change the token")
	}
	if !verified {
		t.Fatal("rotation did not call verify")
	}
	if manager.restarts != 1 {
		t.Fatalf("restarts = %d, want 1", manager.restarts)
	}
	onDisk, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(onDisk) != newToken {
		t.Fatalf("on-disk credential = %q, want %q", onDisk, newToken)
	}
	if _, err := os.Stat(path + ".backup"); !os.IsNotExist(err) {
		t.Fatal("backup file was not cleaned up after a successful rotation")
	}
}

func TestRotateCredentialRollsBackWhenRestartFails(t *testing.T) {
	path := filepath.Join(t.TempDir(), "credentials")
	original, err := credential.LoadOrCreate(path)
	if err != nil {
		t.Fatal(err)
	}
	manager := &fakeManager{failRestart: true}
	_, err = RotateCredential(context.Background(), path, manager, nil)
	if err == nil {
		t.Fatal("expected an error when restart fails")
	}
	onDisk, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(onDisk) != original {
		t.Fatalf("on-disk credential after rollback = %q, want the original %q", onDisk, original)
	}
	// Two restarts: the one that failed during rotation, and the one rollback itself issues.
	if manager.restarts != 2 {
		t.Fatalf("restarts = %d, want 2 (rotation attempt + rollback restart)", manager.restarts)
	}
}

func TestRotateCredentialRollsBackWhenVerificationFails(t *testing.T) {
	path := filepath.Join(t.TempDir(), "credentials")
	original, err := credential.LoadOrCreate(path)
	if err != nil {
		t.Fatal(err)
	}
	manager := &fakeManager{}
	_, err = RotateCredential(context.Background(), path, manager, func(context.Context) error {
		return errors.New("simulated verification failure")
	})
	if err == nil {
		t.Fatal("expected an error when verification fails")
	}
	onDisk, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(onDisk) != original {
		t.Fatalf("on-disk credential after rollback = %q, want the original %q", onDisk, original)
	}
}

func TestRotateCredentialFailsClosedWhenNoCredentialExistsYet(t *testing.T) {
	path := filepath.Join(t.TempDir(), "credentials")
	manager := &fakeManager{}
	if _, err := RotateCredential(context.Background(), path, manager, nil); err == nil {
		t.Fatal("expected rotation preflight to fail without an existing credential")
	}
	if manager.restarts != 0 {
		t.Fatalf("restarts = %d, want 0 when preflight fails", manager.restarts)
	}
}
