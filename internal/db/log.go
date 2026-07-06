package db

import (
	"fmt"
	"time"

	"lmvpn/internal/model"
)

// StartLog creates a connection log entry and returns its ID.
func (s *Store) StartLog(profileID int64) (int64, error) {
	res, err := s.db.Exec(
		`INSERT INTO connection_logs (profile_id, started_at, status)
		 VALUES (?, ?, 'connected')`,
		profileID, time.Now())
	if err != nil {
		return 0, fmt.Errorf("start log: %w", err)
	}
	id, _ := res.LastInsertId()
	return id, nil
}

// FinishLog finalises a connection log entry with final stats.
func (s *Store) FinishLog(id int64, status model.ConnectionStatus, assignedIP string, rxBytes, txBytes int64, errMsg string) error {
	_, err := s.db.Exec(
		`UPDATE connection_logs SET
		    ended_at = ?, assigned_ip = ?, rx_bytes = ?, tx_bytes = ?,
		    status = ?, error_msg = ?
		 WHERE id = ?`,
		time.Now(), assignedIP, rxBytes, txBytes, status, errMsg, id)
	if err != nil {
		return fmt.Errorf("finish log %d: %w", id, err)
	}
	return nil
}

// RecentLogs returns the most recent N connection logs for a profile.
func (s *Store) RecentLogs(profileID int64, limit int) ([]model.ConnectionLog, error) {
	rows, err := s.db.Query(
		`SELECT id, profile_id, started_at, ended_at, assigned_ip,
		        rx_bytes, tx_bytes, status, error_msg
		 FROM connection_logs WHERE profile_id = ?
		 ORDER BY started_at DESC LIMIT ?`,
		profileID, limit)
	if err != nil {
		return nil, fmt.Errorf("recent logs: %w", err)
	}
	defer rows.Close()

	var out []model.ConnectionLog
	for rows.Next() {
		var l model.ConnectionLog
		var ended interface{}
		if err := rows.Scan(&l.ID, &l.ProfileID, &l.StartedAt, &ended,
			&l.AssignedIP, &l.RxBytes, &l.TxBytes, &l.Status, &l.ErrorMsg); err != nil {
			return nil, err
		}
		if t, ok := ended.(time.Time); ok {
			l.EndedAt = &t
		}
		out = append(out, l)
	}
	return out, rows.Err()
}
