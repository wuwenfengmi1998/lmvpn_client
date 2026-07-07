// Package db manages the SQLite database connection, schema migrations,
// and data access for server profiles and connection logs.
//
// It uses modernc.org/sqlite (a pure-Go driver) so the binary has no
// CGO dependency and cross-compiles trivially.
package db

import (
	"database/sql"
	"fmt"
	"strings"

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
	_, err := s.db.Exec(schemaV2)
	if err != nil {
		return err
	}
	if err := s.migrateV2(); err != nil {
		return err
	}
	return s.migrateV3()
}

func (s *Store) migrateV2() error {
	// Detect if migration is needed by checking if old server_url column
	// still exists. If protocol column is present, v2 is already in place.
	_, err := s.db.Exec(`SELECT protocol, host, server_ips, port, path FROM server_profiles LIMIT 0`)
	if err == nil {
		return nil // already v2
	}

	// Check if old table exists.
	var count int
	err = s.db.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='server_profiles' AND sql LIKE '%server_url%'`).Scan(&count)
	if err != nil || count == 0 {
		return nil // nothing to migrate
	}

	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("migrate v2 begin: %w", err)
	}
	defer tx.Rollback()

	_, err = tx.Exec(`ALTER TABLE server_profiles RENAME TO server_profiles_old`)
	if err != nil {
		return fmt.Errorf("migrate v2 rename: %w", err)
	}

	_, err = tx.Exec(schemaV2)
	if err != nil {
		return fmt.Errorf("migrate v2 create new: %w", err)
	}

	rows, err := tx.Query(`SELECT id, name, server_url, username, auth_mode, routing_mode, custom_cidrs, mtu_override, auto_connect, created_at, last_connected_at FROM server_profiles_old`)
	if err != nil {
		return fmt.Errorf("migrate v2 read old: %w", err)
	}
	defer rows.Close()

	insert, err := tx.Prepare(`INSERT INTO server_profiles (id, name, protocol, host, server_ips, port, path, username, auth_mode, routing_mode, custom_cidrs, mtu_override, auto_connect, created_at, last_connected_at) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`)
	if err != nil {
		return fmt.Errorf("migrate v2 prepare insert: %w", err)
	}
	defer insert.Close()

	for rows.Next() {
		var id int64
		var name, serverURL, username, authMode, routingMode, customCIDRs string
		var mtuOverride int
		var autoConnect int
		var createdAt, lastConnectedAt sql.NullString
		if err := rows.Scan(&id, &name, &serverURL, &username, &authMode, &routingMode, &customCIDRs, &mtuOverride, &autoConnect, &createdAt, &lastConnectedAt); err != nil {
			return fmt.Errorf("migrate v2 scan: %w", err)
		}
		protocol, host, ips, port, path := parseOldURL(serverURL)
		_, err = insert.Exec(id, name, protocol, host, ips, port, path, username, authMode, routingMode, customCIDRs, mtuOverride, autoConnect, nullStr(createdAt), nullStr(lastConnectedAt))
		if err != nil {
			return fmt.Errorf("migrate v2 insert: %w", err)
		}
	}

	if err := rows.Err(); err != nil {
		return err
	}

	_, err = tx.Exec(`DROP TABLE server_profiles_old`)
	if err != nil {
		return fmt.Errorf("migrate v2 drop old: %w", err)
	}

	return tx.Commit()
}

func parseOldURL(raw string) (protocol, host, ips, path string, port int) {
	u := raw
	switch {
	case strings.HasPrefix(u, "wss://"):
		protocol = "wss"
		u = u[6:]
	case strings.HasPrefix(u, "ws://"):
		protocol = "ws"
		u = u[5:]
	default:
		protocol = "wss"
	}

	path = "/ws"
	if i := strings.IndexByte(u, '/'); i >= 0 {
		path = u[i:]
		u = u[:i]
	}

	port = 443
	if i := strings.LastIndexByte(u, ':'); i >= 0 {
		if p, err := stringToInt(u[i+1:]); err == nil {
			port = p
			u = u[:i]
		}
	}

	host = u
	return
}

func stringToInt(s string) (int, error) {
	var n int
	for _, c := range s {
		if c < '0' || c > '9' {
			return 0, fmt.Errorf("not a number: %s", s)
		}
		n = n*10 + int(c-'0')
	}
	return n, nil
}

func nullStr(s sql.NullString) sql.NullString {
	return s
}

// migrateV3 adds the assigned_ip6 column to connection_logs for IPv6
// dual-stack support. Idempotent: skips if the column already exists.
func (s *Store) migrateV3() error {
	if columnExists(s.db, "connection_logs", "assigned_ip6") {
		return nil
	}
	_, err := s.db.Exec(`ALTER TABLE connection_logs ADD COLUMN assigned_ip6 TEXT NOT NULL DEFAULT ''`)
	if err != nil {
		return fmt.Errorf("migrate v3 add assigned_ip6: %w", err)
	}
	return nil
}

// columnExists reports whether a column exists on a table.
func columnExists(db *sql.DB, table, column string) bool {
	rows, err := db.Query(fmt.Sprintf("PRAGMA table_info(%s)", table))
	if err != nil {
		return false
	}
	defer rows.Close()
	for rows.Next() {
		var cid int
		var name, ctype string
		var notnull, pk int
		var dflt sql.NullString
		if err := rows.Scan(&cid, &name, &ctype, &notnull, &dflt, &pk); err != nil {
			return false
		}
		if name == column {
			return true
		}
	}
	return false
}

const schemaV2 = `
CREATE TABLE IF NOT EXISTS server_profiles (
	id              INTEGER PRIMARY KEY AUTOINCREMENT,
	name            TEXT    NOT NULL,
	protocol        TEXT    NOT NULL DEFAULT 'wss',
	host            TEXT    NOT NULL,
	server_ips      TEXT    NOT NULL DEFAULT '',
	port            INTEGER NOT NULL DEFAULT 443,
	path            TEXT    NOT NULL DEFAULT '/ws',
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
	id           INTEGER PRIMARY KEY AUTOINCREMENT,
	profile_id   INTEGER NOT NULL,
	started_at   DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
	ended_at     DATETIME,
	assigned_ip  TEXT    NOT NULL DEFAULT '',
	assigned_ip6 TEXT    NOT NULL DEFAULT '',
	rx_bytes     INTEGER NOT NULL DEFAULT 0,
	tx_bytes     INTEGER NOT NULL DEFAULT 0,
	status       TEXT    NOT NULL DEFAULT 'connected',
	error_msg    TEXT    NOT NULL DEFAULT '',
	FOREIGN KEY (profile_id) REFERENCES server_profiles(id) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS idx_logs_profile ON connection_logs(profile_id);
`
