package sqlite

import (
	"context"
	"database/sql"
	"fmt"
	"net/url"
	"path/filepath"

	_ "modernc.org/sqlite"
)

const busyTimeoutMilliseconds = 5000

type Database struct {
	db *sql.DB
}

func Open(ctx context.Context, path string) (*Database, error) {
	absolutePath, err := filepath.Abs(path)
	if err != nil {
		return nil, fmt.Errorf("resolve database path: %w", err)
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

func (d *Database) Close() error {
	return d.db.Close()
}
