package db

import (
	"database/sql"
	"fmt"
	"time"

	"lmvpn/internal/model"
)

// CreateProfile inserts a new server profile and returns its ID.
func (s *Store) CreateProfile(p *model.ServerProfile) (int64, error) {
	res, err := s.db.Exec(
		`INSERT INTO server_profiles
		   (name, protocol, host, server_ips, port, path,
		    username, auth_mode, routing_mode,
		    cidr_v4, cidr_v6, cidr_v4_urls, cidr_v6_urls,
		    mtu_override, auto_connect,
		    tls_ca_cert, tls_ca_path, tls_insecure, tls_pinned_hash,
		    ip_preference)
		 VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		p.Name, p.Protocol, p.Host, p.ServerIPs, p.Port, p.Path,
		p.Username, p.AuthMode, p.RoutingMode,
		p.CIDRV4, p.CIDRV6, p.CIDRV4URLs, p.CIDRV6URLs,
		p.MTUOverride, p.AutoConnect,
		p.TLSCACert, p.TLSCAPath, p.TLSInsecure, p.TLSPinnedHash,
		p.IPPreference,
	)
	if err != nil {
		return 0, fmt.Errorf("insert profile: %w", err)
	}
	id, _ := res.LastInsertId()
	p.ID = id
	p.CreatedAt = time.Now()
	return id, nil
}

// GetProfile returns a single profile by ID.
func (s *Store) GetProfile(id int64) (*model.ServerProfile, error) {
	p := &model.ServerProfile{}
	var last sql.NullTime
	err := s.db.QueryRow(
		`SELECT id, name, protocol, host, server_ips, port, path,
		        username, auth_mode, routing_mode,
		        cidr_v4, cidr_v6, cidr_v4_urls, cidr_v6_urls,
		        mtu_override, auto_connect,
		        tls_ca_cert, tls_ca_path, tls_insecure, tls_pinned_hash,
		        ip_preference,
		        created_at, last_connected_at
		 FROM server_profiles WHERE id = ?`, id,
	).Scan(&p.ID, &p.Name, &p.Protocol, &p.Host, &p.ServerIPs, &p.Port, &p.Path,
		&p.Username, &p.AuthMode, &p.RoutingMode,
		&p.CIDRV4, &p.CIDRV6, &p.CIDRV4URLs, &p.CIDRV6URLs,
		&p.MTUOverride, &p.AutoConnect,
		&p.TLSCACert, &p.TLSCAPath, &p.TLSInsecure, &p.TLSPinnedHash,
		&p.IPPreference, &p.CreatedAt, &last)
	if err != nil {
		return nil, fmt.Errorf("get profile %d: %w", id, err)
	}
	if last.Valid {
		p.LastConnectedAt = &last.Time
	}
	return p, nil
}

// ListProfiles returns all saved profiles ordered by name.
func (s *Store) ListProfiles() ([]model.ServerProfile, error) {
	rows, err := s.db.Query(
		`SELECT id, name, protocol, host, server_ips, port, path,
		        username, auth_mode, routing_mode,
		        cidr_v4, cidr_v6, cidr_v4_urls, cidr_v6_urls,
		        mtu_override, auto_connect,
		        tls_ca_cert, tls_ca_path, tls_insecure, tls_pinned_hash,
		        ip_preference,
		        created_at, last_connected_at
		 FROM server_profiles ORDER BY name`)
	if err != nil {
		return nil, fmt.Errorf("list profiles: %w", err)
	}
	defer rows.Close()

	var out []model.ServerProfile
	for rows.Next() {
		var p model.ServerProfile
		var last sql.NullTime
		if err := rows.Scan(&p.ID, &p.Name, &p.Protocol, &p.Host, &p.ServerIPs,
			&p.Port, &p.Path, &p.Username, &p.AuthMode, &p.RoutingMode,
			&p.CIDRV4, &p.CIDRV6, &p.CIDRV4URLs, &p.CIDRV6URLs,
			&p.MTUOverride, &p.AutoConnect,
			&p.TLSCACert, &p.TLSCAPath, &p.TLSInsecure, &p.TLSPinnedHash,
			&p.IPPreference, &p.CreatedAt, &last); err != nil {
			return nil, err
		}
		if last.Valid {
			p.LastConnectedAt = &last.Time
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// UpdateProfile updates an existing profile.
func (s *Store) UpdateProfile(p *model.ServerProfile) error {
	_, err := s.db.Exec(
		`UPDATE server_profiles SET
		    name = ?, protocol = ?, host = ?, server_ips = ?, port = ?, path = ?,
		    username = ?, auth_mode = ?, routing_mode = ?,
		    cidr_v4 = ?, cidr_v6 = ?, cidr_v4_urls = ?, cidr_v6_urls = ?,
		    mtu_override = ?, auto_connect = ?,
		    tls_ca_cert = ?, tls_ca_path = ?, tls_insecure = ?, tls_pinned_hash = ?,
		    ip_preference = ?
		 WHERE id = ?`,
		p.Name, p.Protocol, p.Host, p.ServerIPs, p.Port, p.Path,
		p.Username, p.AuthMode, p.RoutingMode,
		p.CIDRV4, p.CIDRV6, p.CIDRV4URLs, p.CIDRV6URLs,
		p.MTUOverride, p.AutoConnect,
		p.TLSCACert, p.TLSCAPath, p.TLSInsecure, p.TLSPinnedHash,
		p.IPPreference, p.ID)
	if err != nil {
		return fmt.Errorf("update profile %d: %w", p.ID, err)
	}
	return nil
}

// DeleteProfile removes a profile by ID.
func (s *Store) DeleteProfile(id int64) error {
	_, err := s.db.Exec(`DELETE FROM server_profiles WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("delete profile %d: %w", id, err)
	}
	return nil
}

// TouchLastConnected records the connection timestamp for a profile.
func (s *Store) TouchLastConnected(id int64) error {
	_, err := s.db.Exec(
		`UPDATE server_profiles SET last_connected_at = ? WHERE id = ?`,
		time.Now(), id)
	return err
}
