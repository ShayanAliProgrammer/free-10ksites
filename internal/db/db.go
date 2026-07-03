// Package db provides a Turso (libSQL) database layer with automatic
// fallback to local SQLite for development. All queries use parameterized
// statements to prevent SQL injection.
package db

import (
	"database/sql"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	_ "github.com/tursodatabase/libsql-client-go/libsql" // Turso driver
	_ "modernc.org/sqlite"                                // Local SQLite fallback
)

// DB wraps a *sql.DB with Turso or local SQLite, tuned for connection pooling.
type DB struct {
	*sql.DB
	isRemote bool
}

// Config holds database connection parameters.
type Config struct {
	// TursoURL is the libsql:// URL for the Turso database.
	// If empty, falls back to local SQLite.
	TursoURL string
	// TursoToken is the auth token for Turso.
	TursoToken string
	// LocalPath is the path for the local SQLite database (used when TursoURL is empty).
	LocalPath string
}

// Open creates a new DB connection. If TursoURL is set, connects to Turso;
// otherwise opens a local SQLite file at LocalPath (directory auto-created).
func Open(cfg Config) (*DB, error) {
	if cfg.TursoURL != "" {
		return openTurso(cfg.TursoURL, cfg.TursoToken)
	}
	return openLocal(cfg.LocalPath)
}

func openTurso(url, token string) (*DB, error) {
	dsn := url
	if token != "" {
		sep := "?"
		if strings.Contains(dsn, "?") {
			sep = "&"
		}
		dsn = dsn + sep + "authToken=" + token
	}
	db, err := sql.Open("libsql", dsn)
	if err != nil {
		return nil, fmt.Errorf("turso open: %w", err)
	}
	// Turso uses HTTP; allow concurrent requests but keep pool bounded.
	db.SetMaxOpenConns(10)
	db.SetMaxIdleConns(5)
	db.SetConnMaxLifetime(5 * time.Minute)
	db.SetConnMaxIdleTime(2 * time.Minute)
	if err := db.Ping(); err != nil {
		return nil, fmt.Errorf("turso ping: %w", err)
	}
	log.Printf("Connected to Turso: %s", url)
	return &DB{DB: db, isRemote: true}, nil
}

func openLocal(path string) (*DB, error) {
	if path == "" {
		// Default: ~/.10ksites/tracker.db
		home, err := os.UserHomeDir()
		if err != nil {
			return nil, fmt.Errorf("home dir: %w", err)
		}
		path = filepath.Join(home, ".10ksites", "tracker.db")
	}
	// Ensure parent directory exists
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, fmt.Errorf("create db dir: %w", err)
	}

	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("sqlite open: %w", err)
	}
	// SQLite with WAL allows concurrent reads + serialized writes.
	db.SetMaxOpenConns(4)
	db.SetMaxIdleConns(2)
	db.SetConnMaxLifetime(5 * time.Minute)
	db.SetConnMaxIdleTime(2 * time.Minute)

	// Enable WAL mode for better concurrent read performance
	if _, err := db.Exec("PRAGMA journal_mode=WAL;"); err != nil {
		log.Printf("Warning: WAL mode failed: %v", err)
	}
	if _, err := db.Exec("PRAGMA busy_timeout=5000;"); err != nil {
		log.Printf("Warning: busy_timeout failed: %v", err)
	}

	if err := db.Ping(); err != nil {
		return nil, fmt.Errorf("sqlite ping: %w", err)
	}
	log.Printf("Using local SQLite: %s", path)
	return &DB{DB: db, isRemote: false}, nil
}

// Init creates the schema if it doesn't exist.
func (db *DB) Init() error {
	schema := `
	CREATE TABLE IF NOT EXISTS website_requests (
		id TEXT PRIMARY KEY,
		tracking_id TEXT UNIQUE NOT NULL,
		requester_name TEXT NOT NULL,
		requester_email TEXT NOT NULL,
		site_type TEXT NOT NULL,
		site_description TEXT NOT NULL,
		inspiration_url TEXT,
		status TEXT NOT NULL DEFAULT 'received',
		progress INTEGER NOT NULL DEFAULT 0,
		estimated_delivery DATETIME,
		delivered_url TEXT,
		admin_notes TEXT,
		queue_position INTEGER,
		created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
		updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
	);
	CREATE INDEX IF NOT EXISTS idx_tracking_id ON website_requests(tracking_id);
	CREATE INDEX IF NOT EXISTS idx_status ON website_requests(status);
	CREATE INDEX IF NOT EXISTS idx_created_at ON website_requests(created_at DESC);
	`
	_, err := db.Exec(schema)
	return err
}

// IsRemote returns true if connected to Turso (vs local SQLite).
func (db *DB) IsRemote() bool { return db.isRemote }
