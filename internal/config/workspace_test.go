package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestInitializeIsIdempotentAndFindsWorkspaceFromDescendant(t *testing.T) {
	root := t.TempDir()
	first, created, err := Initialize(root, "")
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
	second, created, err := Initialize(root, "")
	if err != nil {
		t.Fatal(err)
	}
	if created {
		t.Fatal("second initialization should reopen the workspace")
	}
	if first != second {
		t.Fatalf("workspace changed between initializations:\nfirst: %#v\nsecond: %#v", first, second)
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

func TestInitializeResolvesRelativeDatabasePathFromWorkspaceDirectory(t *testing.T) {
	root := t.TempDir()
	workspace, _, err := Initialize(root, filepath.Join("data", "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(root, DirectoryName, "data", "state.db")
	if workspace.DatabasePath != want {
		t.Fatalf("database path %q, want %q", workspace.DatabasePath, want)
	}
}
