package daemon

import (
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"testing"
)

func TestAcquireEnforcesOneOwnerWithADeterministicFailure(t *testing.T) {
	path := filepath.Join(t.TempDir(), "daemon.lock")
	first, err := Acquire(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = first.Release() })

	_, err = Acquire(path)
	var alreadyRunning ErrAlreadyRunning
	if err == nil {
		t.Fatal("second Acquire on a held lock unexpectedly succeeded")
	}
	if !errors.As(err, &alreadyRunning) {
		t.Fatalf("second Acquire error = %v, want ErrAlreadyRunning", err)
	}
	if alreadyRunning.PID != os.Getpid() {
		t.Fatalf("ErrAlreadyRunning.PID = %d, want %d", alreadyRunning.PID, os.Getpid())
	}
}

func TestAcquireSucceedsAgainAfterRelease(t *testing.T) {
	path := filepath.Join(t.TempDir(), "daemon.lock")
	first, err := Acquire(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := first.Release(); err != nil {
		t.Fatal(err)
	}
	second, err := Acquire(path)
	if err != nil {
		t.Fatalf("Acquire after Release = %v, want nil", err)
	}
	_ = second.Release()
}

func TestHeldByReportsTheOwningPIDWithoutAcquiring(t *testing.T) {
	path := filepath.Join(t.TempDir(), "daemon.lock")
	if pid := HeldBy(path); pid != 0 {
		t.Fatalf("HeldBy on a nonexistent lock = %d, want 0", pid)
	}
	lock, err := Acquire(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = lock.Release() })
	if pid := HeldBy(path); pid != os.Getpid() {
		t.Fatalf("HeldBy = %d, want %d", pid, os.Getpid())
	}
}

func TestAcquireRecoversFromAStalePIDLeftByAnUncleanShutdown(t *testing.T) {
	path := filepath.Join(t.TempDir(), "daemon.lock")
	// Simulate a lock file left behind by a process that died without releasing flock:
	// the file exists with a PID, but nothing holds the OS-level lock.
	if err := os.WriteFile(path, []byte(strconv.Itoa(999999)), 0o600); err != nil {
		t.Fatal(err)
	}
	lock, err := Acquire(path)
	if err != nil {
		t.Fatalf("Acquire over a stale unlocked PID file = %v, want nil", err)
	}
	_ = lock.Release()
}
