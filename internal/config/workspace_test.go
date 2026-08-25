package config

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

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
