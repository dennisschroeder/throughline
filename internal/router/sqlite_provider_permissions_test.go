package router

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"syscall"
	"testing"

	"github.com/dennisschroeder/throughline/internal/config"
	"github.com/dennisschroeder/throughline/internal/registry"
)

func TestSQLiteProviderProtectsInitializedWorkspaceFilesWithPermissiveUmask(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX permission bits are not exercised on windows")
	}
	previous := syscall.Umask(0)
	t.Cleanup(func() { syscall.Umask(previous) })

	root := t.TempDir()
	workspace, _, err := config.Initialize(root, "", "provider-permissions")
	if err != nil {
		t.Fatal(err)
	}
	handle, err := (SQLiteProvider{}).Open(context.Background(), registry.WorkspaceTarget{
		WorkspaceID:   workspace.Config.WorkspaceID,
		CanonicalRoot: workspace.Root,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer handle.Close()

	assertWorkspaceMode(t, workspace.Directory, 0o700)
	assertWorkspaceMode(t, workspace.ConfigPath, 0o600)
	assertWorkspaceMode(t, workspace.DatabasePath, 0o600)
	assertWorkspaceMode(t, workspace.DatabasePath+"-wal", 0o600)
	assertWorkspaceMode(t, workspace.DatabasePath+"-shm", 0o600)
}

func TestSQLiteProviderRepairsPermissiveWorkspaceFilesOnReopen(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX permission bits are not exercised on windows")
	}
	previous := syscall.Umask(0)
	t.Cleanup(func() { syscall.Umask(previous) })

	root := t.TempDir()
	workspace, _, err := config.Initialize(root, "", "provider-repair-permissions")
	if err != nil {
		t.Fatal(err)
	}
	target := registry.WorkspaceTarget{WorkspaceID: workspace.Config.WorkspaceID, CanonicalRoot: workspace.Root}
	first, err := (SQLiteProvider{}).Open(context.Background(), target)
	if err != nil {
		t.Fatal(err)
	}
	defer first.Close()
	for _, path := range []string{
		workspace.Directory, workspace.ConfigPath, workspace.DatabasePath,
		workspace.DatabasePath + "-wal", workspace.DatabasePath + "-shm",
	} {
		if err := os.Chmod(path, 0o777); err != nil {
			t.Fatal(err)
		}
	}
	second, err := (SQLiteProvider{}).Open(context.Background(), target)
	if err != nil {
		t.Fatal(err)
	}
	defer second.Close()
	assertWorkspaceMode(t, workspace.Directory, 0o700)
	assertWorkspaceMode(t, workspace.ConfigPath, 0o600)
	assertWorkspaceMode(t, workspace.DatabasePath, 0o600)
	assertWorkspaceMode(t, workspace.DatabasePath+"-wal", 0o600)
	assertWorkspaceMode(t, workspace.DatabasePath+"-shm", 0o600)
}

func assertWorkspaceMode(t *testing.T, path string, want os.FileMode) {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat %s: %v", path, err)
	}
	if got := info.Mode().Perm(); got != want {
		t.Fatalf("%s mode = %o, want %o", filepath.Base(path), got, want)
	}
}
