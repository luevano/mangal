package storage

import (
	"database/sql"
	"fmt"
	"time"
)

// healthIgnoresTable: the health findings dismissed (fine as they are), by check and
// subject key.
const healthIgnoresTable = `CREATE TABLE health_ignores (
	check_id  TEXT NOT NULL,
	subject   TEXT NOT NULL,
	message   TEXT NOT NULL DEFAULT '',
	dismissed INTEGER NOT NULL,
	PRIMARY KEY (check_id, subject)
)`

// HealthIgnore is a dismissed health finding.
type HealthIgnore struct {
	Check string `json:"check"`
	// Subject is the finding's subject key (e.g. "manga/5").
	Subject string `json:"subject"`
	// Message is what the finding said when it was dismissed.
	Message   string    `json:"message,omitempty"`
	Dismissed time.Time `json:"dismissed"`
}

// HealthIgnores are the dismissed health findings.
type HealthIgnores struct{ DB *DB }

// List is every dismissed finding, by check and subject.
func (h HealthIgnores) List() ([]HealthIgnore, error) {
	list, err := queryAll(h.DB, func(rows *sql.Rows) (HealthIgnore, error) {
		var i HealthIgnore
		var at int64
		err := rows.Scan(&i.Check, &i.Subject, &i.Message, &at)
		i.Dismissed = time.Unix(at, 0)
		return i, err
	}, `SELECT check_id, subject, message, dismissed FROM health_ignores ORDER BY check_id, subject`)
	if err != nil {
		return nil, fmt.Errorf("dismissed health findings: %w", err)
	}
	return list, nil
}

// Dismiss dismisses the finding (again: its message and time are updated).
func (h HealthIgnores) Dismiss(i HealthIgnore) error {
	if i.Dismissed.IsZero() {
		i.Dismissed = time.Now()
	}
	_, err := h.DB.Exec(`INSERT INTO health_ignores (check_id, subject, message, dismissed) VALUES (?, ?, ?, ?)
ON CONFLICT (check_id, subject) DO UPDATE SET message = excluded.message, dismissed = excluded.dismissed`,
		i.Check, i.Subject, i.Message, i.Dismissed.Unix())
	if err != nil {
		return fmt.Errorf("dismiss %s %s: %w", i.Check, i.Subject, err)
	}
	return nil
}

// Undismiss brings the finding back, false when it wasn't dismissed.
func (h HealthIgnores) Undismiss(check, subject string) (bool, error) {
	res, err := h.DB.Exec(`DELETE FROM health_ignores WHERE check_id = ? AND subject = ?`, check, subject)
	if err != nil {
		return false, fmt.Errorf("undismiss %s %s: %w", check, subject, err)
	}
	n, _ := res.RowsAffected()
	return n != 0, nil
}

// forgetSubject drops the dismissed findings of a subject that's gone (e.g. "manga/5"):
// its id may be reused by a new one, which mustn't inherit them.
func forgetSubject(tx *sql.Tx, subject string) error {
	if _, err := tx.Exec(`DELETE FROM health_ignores WHERE subject = ? OR subject LIKE ?`, subject, subject+"/%"); err != nil {
		return fmt.Errorf("forget the dismissed findings of %s: %w", subject, err)
	}
	return nil
}
