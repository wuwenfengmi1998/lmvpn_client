// Package db manages the SQLite database connection, schema migrations,
// and data access for server profiles and connection logs.
//
// It uses modernc.org/sqlite (a pure-Go driver) so the binary has no
// CGO dependency and cross-compiles trivially.
package db

import (
	"database/sql"
	"fmt"

	"lmvpn/internal/paths"

	_ "modernc.org/sqlite"
)

// Store wraps the database handle and provides data access methods.
type Store struct {
	db *sql.DB
}

// Open creates or opens the SQLite database and runs migrations.
func Open() (*Store, error) {
	db, err := sql.Open("sqlite", paths.DBPath()+"?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)")
	if err != nil {
		return nil, fmt.Errorf("open db: %w", err)
	}
	db.SetMaxOpenConns(1) // sqlite serialises writes
	s := &Store{db: db}
	if err := s.migrate(); err != nil {
		db.Close()
		return nil, fmt.Errorf("migrate: %w", err)
	}
	return s, nil
}

// Close closes the database connection.
func (s *Store) Close() error {
	return s.db.Close()
}

func (s *Store) migrate() error {
	_, err := s.db.Exec(schema)
	return err
}

const schema = `
CREATE TABLE IF NOT EXISTS server_profiles (
	id              INTEGER PRIMARY KEY AUTOINCREMENT,
	name            TEXT    NOT NULL,
	server_url      TEXT    NOT NULL,
	username        TEXT    NOT NULL,
	auth_mode       TEXT    NOT NULL DEFAULT 'both',
	routing_mode    TEXT    NOT NULL DEFAULT 'full',
	custom_cidrs    TEXT    NOT NULL DEFAULT '',
	mtu_override    INTEGER NOT NULL DEFAULT 0,
	auto_connect    INTEGER NOT NULL DEFAULT 0,
	created_at      DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
	last_connected_at DATETIME
);

CREATE TABLE IF NOT EXISTS connection_logs (
	id          INTEGER PRIMARY KEY AUTOINCREMENT,
	profile_id  INTEGER NOT NULL,
	started_at  DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
	ended_at    DATETIME,
	assigned_ip TEXT    NOT NULL DEFAULT '',
	rx_bytes    INTEGER NOT NULL DEFAULT 0,
	tx_bytes    INTEGER NOT NULL DEFAULT 0,
	status      TEXT    NOT NULL DEFAULT 'connected',
	error_msg   TEXT    NOT NULL DEFAULT '',
	FOREIGN KEY (profile_id) REFERENCES server_profiles(id) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS idx_logs_profile ON connection_logs(profile_id);
`
