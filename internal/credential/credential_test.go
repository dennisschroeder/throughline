package credential

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestLoadOrCreateGeneratesAndPersistsAToken(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "credentials")
	first, err := LoadOrCreate(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(first) != tokenBytes*2 { // hex-encoded
		t.Fatalf("token length = %d, want %d", len(first), tokenBytes*2)
	}
	second, err := LoadOrCreate(path)
	if err != nil {
		t.Fatal(err)
	}
	if first != second {
		t.Fatal("LoadOrCreate generated a new token on the second call")
	}
}

func TestLoadOrCreateGeneratesDistinctTokensForDistinctPaths(t *testing.T) {
	one, err := LoadOrCreate(filepath.Join(t.TempDir(), "credentials"))
	if err != nil {
		t.Fatal(err)
	}
	two, err := LoadOrCreate(filepath.Join(t.TempDir(), "credentials"))
	if err != nil {
		t.Fatal(err)
	}
	if one == two {
		t.Fatal("two fresh credential paths produced the same token")
	}
}

func TestLoadOrCreateSetsRestrictivePermissions(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX permission bits are not exercised on windows")
	}
	path := filepath.Join(t.TempDir(), "nested", "credentials")
	if _, err := LoadOrCreate(path); err != nil {
		t.Fatal(err)
	}
	directoryInfo, err := os.Stat(filepath.Dir(path))
	if err != nil {
		t.Fatal(err)
	}
	if mode := directoryInfo.Mode().Perm(); mode != directoryMode {
		t.Fatalf("credential directory mode = %v, want %v", mode, os.FileMode(directoryMode))
	}
	fileInfo, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if mode := fileInfo.Mode().Perm(); mode != fileMode {
		t.Fatalf("credential file mode = %v, want %v", mode, os.FileMode(fileMode))
	}
}

func TestEqualUsesConstantTimeComparisonSemantics(t *testing.T) {
	if !Equal("secret-token", "secret-token") {
		t.Fatal("identical tokens compared unequal")
	}
	if Equal("secret-token", "wrong-token") {
		t.Fatal("different tokens compared equal")
	}
	if Equal("secret-token", "secret-tok") {
		t.Fatal("a truncated token compared equal")
	}
	// Equal alone does not special-case an empty expected token; callers (daemonhttp)
	// must reject an empty configured token before ever calling Equal.
}
