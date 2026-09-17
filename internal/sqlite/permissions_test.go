package sqlite

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"syscall"
	"testing"
	"time"
)

func setPermissiveUmask(t *testing.T) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("POSIX permission bits are not exercised on windows")
	}
	previous := syscall.Umask(0)
	t.Cleanup(func() { syscall.Umask(previous) })
}

func assertMode(t *testing.T, path string, want os.FileMode) {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat %s: %v", path, err)
	}
	if got := info.Mode().Perm(); got != want {
		t.Fatalf("%s mode = %o, want %o", path, got, want)
	}
}

func TestOpenProtectsDatabaseAndWALSidecarsBeforeFirstSQLiteOpen(t *testing.T) {
	setPermissiveUmask(t)
	path := filepath.Join(t.TempDir(), "nested", "workspace.db")
	database, err := Open(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	if err := database.Migrate(context.Background()); err != nil {
		t.Fatal(err)
	}
	defer database.Close()

	assertMode(t, filepath.Dir(path), 0o700)
	assertMode(t, path, 0o600)
	assertMode(t, path+"-wal", 0o600)
	assertMode(t, path+"-shm", 0o600)
}

func TestOpenRepairsPermissiveDatabaseAndSidecarsOnReopen(t *testing.T) {
	setPermissiveUmask(t)
	path := filepath.Join(t.TempDir(), "workspace.db")
	database, err := Open(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	if err := database.Migrate(context.Background()); err != nil {
		t.Fatal(err)
	}
	for _, file := range []string{path, path + "-wal", path + "-shm"} {
		if err := os.Chmod(file, 0o666); err != nil {
			t.Fatal(err)
		}
	}
	second, err := Open(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	defer second.Close()
	assertMode(t, path, 0o600)
	assertMode(t, path+"-wal", 0o600)
	assertMode(t, path+"-shm", 0o600)
}

func TestOpenLeavesExistingDatabaseParentModeUnchanged(t *testing.T) {
	setPermissiveUmask(t)
	directory := t.TempDir()
	if err := os.Chmod(directory, 0o755); err != nil {
		t.Fatal(err)
	}
	database, err := Open(context.Background(), filepath.Join(directory, "workspace.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	assertMode(t, directory, 0o755)
}

func TestOpenRepairsPermissionsWithoutLosingData(t *testing.T) {
	setPermissiveUmask(t)
	path := filepath.Join(t.TempDir(), "workspace.db")
	first, err := Open(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := first.db.ExecContext(context.Background(), "CREATE TABLE permission_probe (value TEXT NOT NULL)"); err != nil {
		t.Fatal(err)
	}
	if _, err := first.db.ExecContext(context.Background(), "INSERT INTO permission_probe(value) VALUES ('preserved')"); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, 0o666); err != nil {
		t.Fatal(err)
	}
	second, err := Open(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	defer first.Close()
	defer second.Close()
	var value string
	if err := second.db.QueryRowContext(context.Background(), "SELECT value FROM permission_probe").Scan(&value); err != nil {
		t.Fatal(err)
	}
	if value != "preserved" {
		t.Fatalf("stored value = %q, want preserved", value)
	}
	assertMode(t, path, 0o600)
}

func TestReopenRecreatesPrivateSidecars(t *testing.T) {
	setPermissiveUmask(t)
	path := filepath.Join(t.TempDir(), "workspace.db")
	first, err := Open(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	if err := first.Migrate(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := first.Close(); err != nil {
		t.Fatal(err)
	}
	for _, suffix := range []string{"-wal", "-shm"} {
		if err := os.Remove(path + suffix); err != nil && !os.IsNotExist(err) {
			t.Fatal(err)
		}
	}
	if err := os.Chmod(path, 0o666); err != nil {
		t.Fatal(err)
	}
	reopened, err := Open(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	if err := reopened.Migrate(context.Background()); err != nil {
		t.Fatal(err)
	}
	assertMode(t, path, 0o600)
	assertMode(t, path+"-wal", 0o600)
	assertMode(t, path+"-shm", 0o600)
}

func TestOpenPreservesSQLitePOSIXLocks(t *testing.T) {
	path := filepath.Join(t.TempDir(), "workspace.db")
	first, err := Open(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	if err := first.Migrate(context.Background()); err != nil {
		t.Fatal(err)
	}
	defer first.Close()
	if !databaseLockHeldByAnotherProcess(t, path) {
		t.Fatal("SQLite did not establish the expected database lock")
	}
	second, err := Open(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	defer second.Close()
	if !databaseLockHeldByAnotherProcess(t, path) {
		t.Fatal("second Open released SQLite POSIX locks held by the process")
	}
}

func TestSQLiteLockProbe(t *testing.T) {
	path := os.Getenv("THROUGHLINE_SQLITE_LOCK_PROBE")
	if path == "" {
		return
	}
	file, err := os.OpenFile(path, os.O_RDWR, 0)
	if err != nil {
		os.Exit(4)
	}
	defer file.Close()
	lock := syscall.Flock_t{Type: syscall.F_WRLCK, Whence: 0, Start: 0x40000000, Len: 512}
	if err := syscall.FcntlFlock(file.Fd(), syscall.F_SETLK, &lock); err == nil {
		os.Exit(0)
	} else if errors.Is(err, syscall.EACCES) || errors.Is(err, syscall.EAGAIN) {
		os.Exit(3)
	}
	os.Exit(4)
}

func databaseLockHeldByAnotherProcess(t *testing.T, path string) bool {
	t.Helper()
	command := exec.Command(os.Args[0], "-test.run=^TestSQLiteLockProbe$")
	command.Env = append(os.Environ(), "THROUGHLINE_SQLITE_LOCK_PROBE="+path)
	err := command.Run()
	if err == nil {
		return false
	}
	var exitError *exec.ExitError
	if errors.As(err, &exitError) && exitError.ExitCode() == 3 {
		return true
	}
	t.Fatalf("run SQLite lock probe: %v", err)
	return false
}

func TestOpenRepairsSidecarsAtResolvedSymlinkTarget(t *testing.T) {
	setPermissiveUmask(t)
	directory := t.TempDir()
	realPath := filepath.Join(directory, "real.db")
	first, err := Open(context.Background(), realPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := first.Migrate(context.Background()); err != nil {
		t.Fatal(err)
	}
	defer first.Close()
	for _, suffix := range []string{"", "-wal", "-shm"} {
		if err := os.Chmod(realPath+suffix, 0o666); err != nil {
			t.Fatal(err)
		}
	}
	linkPath := filepath.Join(directory, "link.db")
	if err := os.Symlink(realPath, linkPath); err != nil {
		t.Fatal(err)
	}
	second, err := Open(context.Background(), linkPath)
	if err != nil {
		t.Fatal(err)
	}
	defer second.Close()
	assertMode(t, realPath, 0o600)
	assertMode(t, realPath+"-wal", 0o600)
	assertMode(t, realPath+"-shm", 0o600)
}

func TestOpenWaitsForPermissionPreparation(t *testing.T) {
	permissionPreparationMu.Lock()
	locked := true
	defer func() {
		if locked {
			permissionPreparationMu.Unlock()
		}
	}()

	started := make(chan struct{})
	finished := make(chan error, 1)
	path := filepath.Join(t.TempDir(), "workspace.db")
	go func() {
		close(started)
		database, err := Open(context.Background(), path)
		if err == nil {
			err = database.Close()
		}
		finished <- err
	}()
	<-started
	select {
	case err := <-finished:
		t.Fatalf("Open bypassed permission preparation lock: %v", err)
	case <-time.After(25 * time.Millisecond):
	}
	permissionPreparationMu.Unlock()
	locked = false
	if err := <-finished; err != nil {
		t.Fatal(err)
	}
}
