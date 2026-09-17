package config

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"syscall"
	"testing"
)

func withUmask(t *testing.T, mask int) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("POSIX permission bits are not exercised on windows")
	}
	previous := syscall.Umask(mask)
	t.Cleanup(func() { syscall.Umask(previous) })
}

func TestInitializeIsIdempotentAndFindsWorkspaceFromDescendant(t *testing.T) {
	root := t.TempDir()
	first, created, err := Initialize(root, "", "ws-first")
	if err != nil {
		t.Fatal(err)
	}
	if !created {
		t.Fatal("first initialization should create the workspace")
	}
	if filepath.Base(first.Directory) != ".throughline" {
		t.Fatalf("workspace directory = %q", first.Directory)
	}
	if filepath.Base(first.DatabasePath) != "throughline.db" {
		t.Fatalf("database path = %q", first.DatabasePath)
	}
	if first.Config.ItemKeyPrefix != "TH" {
		t.Fatalf("item key prefix = %q", first.Config.ItemKeyPrefix)
	}
	if first.Config.WorkspaceID != "ws-first" {
		t.Fatalf("workspace_id = %q, want ws-first", first.Config.WorkspaceID)
	}
	second, created, err := Initialize(root, "", "ws-should-be-ignored")
	if err != nil {
		t.Fatal(err)
	}
	if created {
		t.Fatal("second initialization should reopen the workspace")
	}
	if first != second {
		t.Fatalf("workspace changed between initializations:\nfirst: %#v\nsecond: %#v", first, second)
	}
	if second.Config.WorkspaceID != "ws-first" {
		t.Fatalf("reopen changed workspace_id to %q", second.Config.WorkspaceID)
	}

	nested := filepath.Join(root, "notes", "research")
	if err := os.MkdirAll(nested, 0o755); err != nil {
		t.Fatal(err)
	}
	found, err := Find(nested)
	if err != nil {
		t.Fatal(err)
	}
	if found.Root != first.Root {
		t.Fatalf("found root %q, want %q", found.Root, first.Root)
	}
}

func TestInitializeRequiresAWorkspaceIDForFreshCreation(t *testing.T) {
	root := t.TempDir()
	if _, _, err := Initialize(root, "", ""); err == nil {
		t.Fatal("expected an error when creating a workspace without a workspace_id")
	}
}

func TestInitializeResolvesRelativeDatabasePathFromWorkspaceDirectory(t *testing.T) {
	root := t.TempDir()
	workspace, _, err := Initialize(root, filepath.Join("data", "state.db"), "ws-1")
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(root, DirectoryName, "data", "state.db")
	if workspace.DatabasePath != want {
		t.Fatalf("database path %q, want %q", workspace.DatabasePath, want)
	}
}

func TestLoadRejectsALegacyConfigWithoutWorkspaceIdentity(t *testing.T) {
	root := t.TempDir()
	directory := filepath.Join(root, DirectoryName)
	if err := os.MkdirAll(directory, 0o755); err != nil {
		t.Fatal(err)
	}
	legacy := "schema_version = 1\ndatabase_path = 'throughline.db'\nitem_key_prefix = 'TH'\n"
	if err := os.WriteFile(filepath.Join(directory, ConfigFileName), []byte(legacy), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := Load(root)
	if !errors.Is(err, ErrLegacyWorkspace) {
		t.Fatalf("Load on a legacy config = %v, want ErrLegacyWorkspace", err)
	}
}

func TestForkAssignsANewWorkspaceIDAndKeepsStorageSettings(t *testing.T) {
	root := t.TempDir()
	original, _, err := Initialize(root, "", "ws-original")
	if err != nil {
		t.Fatal(err)
	}
	forked, sourceID, err := Fork(root, "ws-forked")
	if err != nil {
		t.Fatal(err)
	}
	if sourceID != "ws-original" {
		t.Fatalf("fork source id = %q, want ws-original", sourceID)
	}
	if forked.Config.WorkspaceID != "ws-forked" {
		t.Fatalf("forked workspace_id = %q, want ws-forked", forked.Config.WorkspaceID)
	}
	if forked.Config.DatabasePath != original.Config.DatabasePath {
		t.Fatalf("forked database_path = %q, want %q", forked.Config.DatabasePath, original.Config.DatabasePath)
	}
}

func TestFingerprintChangesWhenIdentifyingFieldsChange(t *testing.T) {
	base := Config{SchemaVersion: CurrentSchema, WorkspaceID: "ws-1", DatabasePath: "throughline.db", ItemKeyPrefix: "TH"}
	changed := base
	changed.WorkspaceID = "ws-2"
	if base.Fingerprint() == changed.Fingerprint() {
		t.Fatal("fingerprint did not change when workspace_id changed")
	}
	same := base
	if base.Fingerprint() != same.Fingerprint() {
		t.Fatal("fingerprint changed for an identical config")
	}
}

func TestInitializeProtectsWorkspaceDirectoryAndConfigWithPermissiveUmask(t *testing.T) {
	withUmask(t, 0)
	root := t.TempDir()
	workspace, _, err := Initialize(root, "", "ws-permissions")
	if err != nil {
		t.Fatal(err)
	}
	directoryInfo, err := os.Stat(workspace.Directory)
	if err != nil {
		t.Fatal(err)
	}
	if got := directoryInfo.Mode().Perm(); got != 0o700 {
		t.Fatalf("workspace directory mode = %o, want 0700", got)
	}
	configInfo, err := os.Stat(workspace.ConfigPath)
	if err != nil {
		t.Fatal(err)
	}
	if got := configInfo.Mode().Perm(); got != 0o600 {
		t.Fatalf("workspace config mode = %o, want 0600", got)
	}

	if err := os.Chmod(workspace.Directory, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(workspace.ConfigPath, 0o644); err != nil {
		t.Fatal(err)
	}
	reopened, created, err := Initialize(root, "", "ignored")
	if err != nil {
		t.Fatal(err)
	}
	if created || reopened.Config.WorkspaceID != "ws-permissions" {
		t.Fatalf("reopened workspace = %#v, created=%v", reopened, created)
	}
	directoryInfo, err = os.Stat(workspace.Directory)
	if err != nil {
		t.Fatal(err)
	}
	configInfo, err = os.Stat(workspace.ConfigPath)
	if err != nil {
		t.Fatal(err)
	}
	if got := directoryInfo.Mode().Perm(); got != 0o700 {
		t.Fatalf("repaired workspace directory mode = %o, want 0700", got)
	}
	if got := configInfo.Mode().Perm(); got != 0o600 {
		t.Fatalf("repaired workspace config mode = %o, want 0600", got)
	}
}
