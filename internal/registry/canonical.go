package registry

import (
	"fmt"
	"os"
	"path/filepath"
)

// CanonicalizeRoot resolves symlinks and returns a clean absolute path, so a symlinked
// workspace root and its real path never register as two identities.
func CanonicalizeRoot(root string) (string, error) {
	absolute, err := filepath.Abs(root)
	if err != nil {
		return "", fmt.Errorf("resolve workspace root: %w", err)
	}
	resolved, err := filepath.EvalSymlinks(absolute)
	if err != nil {
		return "", fmt.Errorf("resolve workspace root symlinks: %w", err)
	}
	return filepath.Clean(resolved), nil
}

// RootAvailable reports whether a canonical root still exists on disk.
func RootAvailable(canonicalRoot string) bool {
	info, err := os.Stat(canonicalRoot)
	if err != nil {
		return false
	}
	return info.IsDir()
}
