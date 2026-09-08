package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"sync"

	_ "modernc.org/sqlite"
)

const busyTimeoutMilliseconds = 5000

// permissionPreparationMu ensures a newly created descriptor is closed before another
// goroutine can let SQLite acquire process-scoped POSIX locks on the same inode.
var permissionPreparationMu sync.Mutex

const (
	workspaceDirectoryMode = 0o700
	databaseFileMode       = 0o600
)

type Database struct {
	db *sql.DB
}

func Open(ctx context.Context, path string) (*Database, error) {
	absolutePath, err := filepath.Abs(path)
	if err != nil {
		return nil, fmt.Errorf("resolve database path: %w", err)
	}
	if err := ensurePermissions(filepath.Clean(absolutePath)); err != nil {
		return nil, err
	}
	query := url.Values{}
	query.Add("_pragma", "foreign_keys(1)")
	query.Add("_pragma", "journal_mode(WAL)")
	query.Add("_pragma", fmt.Sprintf("busy_timeout(%d)", busyTimeoutMilliseconds))
	dsn := (&url.URL{Scheme: "file", Path: filepath.Clean(absolutePath), RawQuery: query.Encode()}).String()
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("open sqlite database: %w", err)
	}
	// A single connection gives the process one explicit writer boundary and ensures
	// every operation uses the configured connection pragmas.
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	if err := db.PingContext(ctx); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("connect sqlite database: %w", err)
	}
	return &Database{db: db}, nil
}

// ensurePermissions establishes the database mode before SQLite can create WAL or SHM
// sidecars. SQLite uses the main database's mode for those files, so pre-creating the
// database is necessary when the process umask permits group/other access. These modes
// protect against other OS users; same-user processes remain a convention and there is no
// tamper detection.
func ensurePermissions(path string) error {
	permissionPreparationMu.Lock()
	defer permissionPreparationMu.Unlock()

	directory := filepath.Dir(path)
	if err := os.MkdirAll(directory, workspaceDirectoryMode); err != nil {
		return fmt.Errorf("create workspace database directory: %w", err)
	}
	database, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE|os.O_EXCL, databaseFileMode)
	switch {
	case err == nil:
		if err := database.Chmod(databaseFileMode); err != nil {
			_ = database.Close()
			return fmt.Errorf("set workspace database permissions: %w", err)
		}
		if err := database.Close(); err != nil {
			return fmt.Errorf("close workspace database: %w", err)
		}
	case errors.Is(err, os.ErrExist):
	default:
		return fmt.Errorf("create workspace database: %w", err)
	}
	resolvedPath, err := filepath.EvalSymlinks(path)
	if err != nil {
		return fmt.Errorf("resolve workspace database permission path: %w", err)
	}
	if err := os.Chmod(resolvedPath, databaseFileMode); err != nil {
		return fmt.Errorf("set workspace database permissions: %w", err)
	}
	for _, suffix := range []string{"-wal", "-shm"} {
		sidecar := resolvedPath + suffix
		if err := os.Chmod(sidecar, databaseFileMode); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("set workspace database sidecar permissions: %w", err)
		}
	}
	return nil
}

func (d *Database) Close() error {
	return d.db.Close()
}
