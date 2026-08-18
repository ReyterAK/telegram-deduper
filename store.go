//
// store.go
// Antidubl — SQLite storage (modernc.org/sqlite, pure Go, CGO-free)
//
// Tables: messages (duplicate comparison window), warnings (per-user
// warning events), bot_messages (scheduled auto-deletion).
//

package main

import (
	"database/sql"
	"encoding/hex"
	"fmt"
	"time"

	_ "modernc.org/sqlite"
)

func nowUnix() int64 { return time.Now().Unix() }

type StoredMessage struct {
	ChatID          int64
	MsgID           int
	UserID          int64
	NormText        string
	HasURL          bool
	MediaUID        string
	ForwardExternal bool
	FwdSource       string
	PhotoHash       string
	TS              int64
}

type BotMessage struct {
	ChatID int64
	MsgID  int
}

type Store struct {
	db *sql.DB
}

const storeSchema = `
CREATE TABLE IF NOT EXISTS messages (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	chat_id INTEGER NOT NULL,
	msg_id INTEGER NOT NULL,
	user_id INTEGER NOT NULL,
	norm_text TEXT NOT NULL DEFAULT '',
	has_url INTEGER NOT NULL DEFAULT 0,
	media_uid TEXT NOT NULL DEFAULT '',
	forward_external INTEGER NOT NULL DEFAULT 0,
	fwd_source TEXT NOT NULL DEFAULT '',
	media_phash TEXT NOT NULL DEFAULT '',
	ts INTEGER NOT NULL,
	UNIQUE(chat_id, msg_id)
);
CREATE INDEX IF NOT EXISTS idx_messages_chat_ts ON messages(chat_id, ts);
CREATE INDEX IF NOT EXISTS idx_messages_chat_text ON messages(chat_id, norm_text);
CREATE INDEX IF NOT EXISTS idx_messages_chat_media ON messages(chat_id, media_uid);

CREATE TABLE IF NOT EXISTS warnings (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	chat_id INTEGER NOT NULL,
	user_id INTEGER NOT NULL,
	ts INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_warnings_user ON warnings(chat_id, user_id, ts);

CREATE TABLE IF NOT EXISTS bot_messages (
	chat_id INTEGER NOT NULL,
	msg_id INTEGER NOT NULL,
	delete_at INTEGER NOT NULL,
	PRIMARY KEY (chat_id, msg_id)
);
CREATE INDEX IF NOT EXISTS idx_bot_messages_due ON bot_messages(delete_at);
`

func OpenStore(path string) (*Store, error) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("open db: %w", err)
	}
	db.SetMaxOpenConns(1)
	if _, err := db.Exec(storeSchema); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("migrate db: %w", err)
	}
	// migration for DBs created before fwd_source existed
	rows, err := db.Query(`PRAGMA table_info(messages)`)
	if err == nil {
		hasFwd := false
		hasPhash := false
		for rows.Next() {
			var cid int
			var name, ctype string
			var notnull, pk int
			var dflt any
			if rows.Scan(&cid, &name, &ctype, &notnull, &dflt, &pk) == nil {
				switch name {
				case "fwd_source":
					hasFwd = true
				case "media_phash":
					hasPhash = true
				}
			}
		}
		rows.Close()
		if !hasFwd {
			if _, err := db.Exec(`ALTER TABLE messages ADD COLUMN fwd_source TEXT NOT NULL DEFAULT ''`); err != nil {
				_ = db.Close()
				return nil, fmt.Errorf("migrate db (fwd_source): %w", err)
			}
		}
		if !hasPhash {
			if _, err := db.Exec(`ALTER TABLE messages ADD COLUMN media_phash TEXT NOT NULL DEFAULT ''`); err != nil {
				_ = db.Close()
				return nil, fmt.Errorf("migrate db (media_phash): %w", err)
			}
		}
	}
	return &Store{db: db}, nil
}

func (s *Store) Close() error { return s.db.Close() }

// ---------------------------------------------------------------------
// messages
// ---------------------------------------------------------------------

func (s *Store) AddMessage(m StoredMessage) error {
	_, err := s.db.Exec(
		`INSERT OR IGNORE INTO messages (chat_id, msg_id, user_id, norm_text, has_url, media_uid, forward_external, fwd_source, media_phash, ts)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		m.ChatID, m.MsgID, m.UserID, m.NormText, b2i(m.HasURL), m.MediaUID, b2i(m.ForwardExternal), m.FwdSource, m.PhotoHash, m.TS)
	return err
}

// FindPhotoDuplicate scans the window for a stored photo whose dHash
// is within the Hamming threshold; the oldest match wins. Returns
// nil when nothing is close enough, plus the closest distance seen.
func (s *Store) FindPhotoDuplicate(chatID int64, since int64, phash string, threshold int) (*StoredMessage, int, error) {
	needle, err := hex.DecodeString(phash)
	if err != nil || len(needle) != 8 {
		return nil, 64, nil
	}
	var want uint64
	for i := range 8 {
		want |= uint64(needle[i]) << (8 * uint(7-i))
	}

	rows, err := s.db.Query(
		`SELECT chat_id, msg_id, user_id, norm_text, has_url, media_uid, forward_external, fwd_source, media_phash, ts
		 FROM messages
		 WHERE chat_id = ? AND ts >= ? AND media_phash != ''
		 ORDER BY ts ASC, id ASC
		 LIMIT 500`, chatID, since)
	if err != nil {
		return nil, 64, err
	}
	defer rows.Close()

	minDist := 64
	for rows.Next() {
		var m StoredMessage
		var hasURL, fe int
		if err := rows.Scan(&m.ChatID, &m.MsgID, &m.UserID, &m.NormText, &hasURL, &m.MediaUID, &fe, &m.FwdSource, &m.PhotoHash, &m.TS); err != nil {
			return nil, 64, err
		}
		m.HasURL = hasURL != 0
		m.ForwardExternal = fe != 0

		raw, err := hex.DecodeString(m.PhotoHash)
		if err != nil || len(raw) != 8 {
			continue
		}
		var got uint64
		for i := range 8 {
			got |= uint64(raw[i]) << (8 * uint(7-i))
		}
		dist := hamming(got, want)
		if dist < minDist {
			minDist = dist
		}
		if dist <= threshold {
			return &m, minDist, nil
		}
	}
	return nil, minDist, rows.Err()
}

// UpdateMessage refreshes the comparable content of an existing
// message (used when a message is edited).
func (s *Store) UpdateMessage(m StoredMessage) error {
	_, err := s.db.Exec(
		`UPDATE messages SET norm_text = ?, has_url = ?, media_uid = ?, forward_external = ?, fwd_source = ?
		 WHERE chat_id = ? AND msg_id = ?`,
		m.NormText, b2i(m.HasURL), m.MediaUID, b2i(m.ForwardExternal), m.FwdSource, m.ChatID, m.MsgID)
	return err
}

// FindDuplicates returns stored messages from the window that match
// the incoming message: the same normalized text, the same media
// file_unique_id, or the same forward source. Ordered oldest first
// so [0] is the original.
func (s *Store) FindDuplicates(chatID int64, since int64, normText, mediaUID, fwdSource string, exceptMsgID int) ([]StoredMessage, error) {
	rows, err := s.db.Query(
		`SELECT chat_id, msg_id, user_id, norm_text, has_url, media_uid, forward_external, fwd_source, ts
		 FROM messages
		 WHERE chat_id = ? AND ts >= ? AND msg_id != ?
		   AND ( (media_uid != '' AND media_uid = ?)
		         OR (media_uid = '' AND norm_text != '' AND norm_text = ?)
		         OR (fwd_source != '' AND fwd_source = ?) )
		 ORDER BY ts ASC, id ASC
		 LIMIT 20`,
		chatID, since, exceptMsgID, mediaUID, normText, fwdSource)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []StoredMessage
	for rows.Next() {
		var m StoredMessage
		var hasURL, fe int
		if err := rows.Scan(&m.ChatID, &m.MsgID, &m.UserID, &m.NormText, &hasURL, &m.MediaUID, &fe, &m.FwdSource, &m.TS); err != nil {
			return nil, err
		}
		m.HasURL = hasURL != 0
		m.ForwardExternal = fe != 0
		out = append(out, m)
	}
	return out, rows.Err()
}

// CleanupRetention deletes messages and warning events older than the
// retention window (both are bounded by it).
func (s *Store) CleanupRetention(chatID int64, retentionDays int) error {
	cutoff := nowUnix() - int64(retentionDays)*86400
	if _, err := s.db.Exec(`DELETE FROM messages WHERE chat_id = ? AND ts < ?`, chatID, cutoff); err != nil {
		return err
	}
	if _, err := s.db.Exec(`DELETE FROM warnings WHERE chat_id = ? AND ts < ?`, chatID, cutoff); err != nil {
		return err
	}
	// housekeeping: remove already-purged bot rows
	_, err := s.db.Exec(`DELETE FROM bot_messages WHERE delete_at < ?`, nowUnix())
	return err
}

// ---------------------------------------------------------------------
// warnings
// ---------------------------------------------------------------------

func (s *Store) AddWarning(chatID, userID, ts int64) error {
	_, err := s.db.Exec(`INSERT INTO warnings (chat_id, user_id, ts) VALUES (?, ?, ?)`, chatID, userID, ts)
	return err
}

func (s *Store) CountWarnings(chatID, userID, since int64) (int, error) {
	var n int
	err := s.db.QueryRow(
		`SELECT COUNT(*) FROM warnings WHERE chat_id = ? AND user_id = ? AND ts >= ?`,
		chatID, userID, since).Scan(&n)
	return n, err
}

func (s *Store) ResetWarnings(chatID, userID int64) error {
	_, err := s.db.Exec(`DELETE FROM warnings WHERE chat_id = ? AND user_id = ?`, chatID, userID)
	return err
}

// ---------------------------------------------------------------------
// bot messages (auto-delete)
// ---------------------------------------------------------------------

func (s *Store) AddBotMessage(chatID int64, msgID int, deleteAt int64) error {
	_, err := s.db.Exec(
		`INSERT OR REPLACE INTO bot_messages (chat_id, msg_id, delete_at) VALUES (?, ?, ?)`,
		chatID, msgID, deleteAt)
	return err
}

func (s *Store) DueBotMessages(now int64) ([]BotMessage, error) {
	rows, err := s.db.Query(`SELECT chat_id, msg_id FROM bot_messages WHERE delete_at <= ?`, now)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []BotMessage
	for rows.Next() {
		var bm BotMessage
		if err := rows.Scan(&bm.ChatID, &bm.MsgID); err != nil {
			return nil, err
		}
		out = append(out, bm)
	}
	return out, rows.Err()
}

func (s *Store) RemoveBotMessage(chatID int64, msgID int) error {
	_, err := s.db.Exec(`DELETE FROM bot_messages WHERE chat_id = ? AND msg_id = ?`, chatID, msgID)
	return err
}

func b2i(b bool) int {
	if b {
		return 1
	}
	return 0
}
