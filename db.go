package main

import (
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"fmt"

	_ "modernc.org/sqlite"
)

func openDB(path string) (*sql.DB, error) {
	dsn := fmt.Sprintf("file:%s?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)&_pragma=foreign_keys(1)", path)
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1) // sqlite: serialize writers
	if err := migrate(db); err != nil {
		return nil, err
	}
	return db, nil
}

func migrate(db *sql.DB) error {
	stmts := []string{
		`CREATE TABLE IF NOT EXISTS users(
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			google_sub TEXT NOT NULL UNIQUE,
			name TEXT NOT NULL DEFAULT '',
			name_key TEXT NOT NULL DEFAULT '',
			ref_code TEXT NOT NULL UNIQUE,
			points INTEGER NOT NULL DEFAULT 0,
			tickets INTEGER NOT NULL DEFAULT 0,
			banned INTEGER NOT NULL DEFAULT 0,
			created_at INTEGER NOT NULL
		)`,
		// Name uniqueness is case-insensitive and only applies once a name is set.
		`CREATE UNIQUE INDEX IF NOT EXISTS idx_users_name_key ON users(name_key) WHERE name_key <> ''`,
		`CREATE TABLE IF NOT EXISTS attendance(
			user_id INTEGER NOT NULL REFERENCES users(id),
			day TEXT NOT NULL,
			checked_at INTEGER NOT NULL,
			PRIMARY KEY(user_id, day)
		)`,
		`CREATE TABLE IF NOT EXISTS taps(
			day TEXT NOT NULL,
			from_id INTEGER NOT NULL REFERENCES users(id),
			to_id INTEGER NOT NULL REFERENCES users(id),
			count INTEGER NOT NULL DEFAULT 0,
			PRIMARY KEY(day, from_id, to_id)
		)`,
		`CREATE INDEX IF NOT EXISTS idx_taps_to ON taps(day, to_id)`,
		`CREATE TABLE IF NOT EXISTS referrals(
			invitee_id INTEGER PRIMARY KEY REFERENCES users(id),
			referrer_id INTEGER NOT NULL REFERENCES users(id),
			rewarded_at INTEGER
		)`,
		// How much of today's level quota a user has spent (the rest of a tap
		// spree comes out of referral tickets, tracked on users.tickets).
		`CREATE TABLE IF NOT EXISTS daily_spend(
			user_id INTEGER NOT NULL REFERENCES users(id),
			day TEXT NOT NULL,
			quota_used INTEGER NOT NULL DEFAULT 0,
			PRIMARY KEY(user_id, day)
		)`,
	}
	for _, s := range stmts {
		if _, err := db.Exec(s); err != nil {
			return err
		}
	}
	return nil
}

type user struct {
	ID        int64
	Sub       string
	Name      string
	RefCode   string
	Points    int64
	Tickets   int64
	Banned    bool
	CreatedAt int64
}

func randomHex(n int) string {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return hex.EncodeToString(b)
}

func getUserBySub(db *sql.DB, sub string) (*user, error) {
	var u user
	var banned int
	err := db.QueryRow(`SELECT id, google_sub, name, ref_code, points, tickets, banned, created_at
		FROM users WHERE google_sub=?`, sub).
		Scan(&u.ID, &u.Sub, &u.Name, &u.RefCode, &u.Points, &u.Tickets, &banned, &u.CreatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	u.Banned = banned != 0
	return &u, nil
}

func getUserByID(db *sql.DB, id int64) (*user, error) {
	var u user
	var banned int
	err := db.QueryRow(`SELECT id, google_sub, name, ref_code, points, tickets, banned, created_at
		FROM users WHERE id=?`, id).
		Scan(&u.ID, &u.Sub, &u.Name, &u.RefCode, &u.Points, &u.Tickets, &banned, &u.CreatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	u.Banned = banned != 0
	return &u, nil
}

// attendanceDays is the user's cumulative number of checked-in days
// (optionally including a specific day only if present — it is a plain count).
func attendanceDays(db *sql.DB, userID int64) (int, error) {
	var n int
	err := db.QueryRow(`SELECT COUNT(*) FROM attendance WHERE user_id=?`, userID).Scan(&n)
	return n, err
}

func checkedIn(db *sql.DB, userID int64, day string) (bool, error) {
	var n int
	err := db.QueryRow(`SELECT COUNT(*) FROM attendance WHERE user_id=? AND day=?`, userID, day).Scan(&n)
	return n > 0, err
}
