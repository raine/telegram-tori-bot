package storage

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"sync"
	"time"

	_ "github.com/mattn/go-sqlite3"
	"github.com/raine/telegram-tori-bot/tori/auth"
)

// StoredSession represents a persisted user session.
type StoredSession struct {
	TelegramID  int64
	ToriUserID  string
	Tokens      auth.TokenSet
	LastUpdated time.Time
}

// Template represents a user's description template.
type Template struct {
	ID         int64
	TelegramID int64
	Content    string
}

// SessionStore defines the interface for session persistence.
type SessionStore interface {
	Get(telegramID int64) (*StoredSession, error)
	Save(session *StoredSession) error
	Delete(telegramID int64) error
	GetAll() ([]StoredSession, error)
	Close() error

	// Template methods (single template per user)
	SetTemplate(telegramID int64, content string) error
	GetTemplate(telegramID int64) (*Template, error)
	DeleteTemplate(telegramID int64) error
}

// SQLiteStore implements SessionStore using SQLite with encrypted tokens.
type SQLiteStore struct {
	db            *sql.DB
	encryptionKey []byte
	mu            sync.RWMutex
}

// NewSQLiteStore creates a new SQLite-based session store.
// The dbPath is the path to the SQLite database file.
// The encryptionKey is used to encrypt/decrypt token data.
func NewSQLiteStore(dbPath string, encryptionKey []byte) (*SQLiteStore, error) {
	db, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}

	// Set file permissions (only works on creation)
	if err := os.Chmod(dbPath, 0600); err != nil && !os.IsNotExist(err) {
		// Ignore error if file doesn't exist yet
	}

	store := &SQLiteStore{
		db:            db,
		encryptionKey: encryptionKey,
	}

	if err := store.init(); err != nil {
		db.Close()
		return nil, err
	}

	return store, nil
}

func (s *SQLiteStore) init() error {
	query := `
	CREATE TABLE IF NOT EXISTS sessions (
		telegram_id INTEGER PRIMARY KEY,
		tori_user_id TEXT NOT NULL,
		encrypted_tokens TEXT NOT NULL,
		last_updated DATETIME NOT NULL
	);
	`
	_, err := s.db.Exec(query)
	if err != nil {
		return fmt.Errorf("failed to create sessions table: %w", err)
	}

	templatesQuery := `
	CREATE TABLE IF NOT EXISTS templates (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		telegram_id INTEGER NOT NULL UNIQUE,
		content TEXT NOT NULL,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP
	);
	`
	_, err = s.db.Exec(templatesQuery)
	if err != nil {
		return fmt.Errorf("failed to create templates table: %w", err)
	}

	return nil
}

// Get retrieves a session by Telegram user ID.
// Returns nil, nil if the session doesn't exist.
func (s *SQLiteStore) Get(telegramID int64) (*StoredSession, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var toriUserID, encryptedTokens string
	var lastUpdated time.Time

	err := s.db.QueryRow(
		"SELECT tori_user_id, encrypted_tokens, last_updated FROM sessions WHERE telegram_id = ?",
		telegramID,
	).Scan(&toriUserID, &encryptedTokens, &lastUpdated)

	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to query session: %w", err)
	}

	// Decrypt tokens
	tokensJSON, err := Decrypt(encryptedTokens, s.encryptionKey)
	if err != nil {
		return nil, fmt.Errorf("failed to decrypt tokens: %w", err)
	}

	var tokens auth.TokenSet
	if err := json.Unmarshal(tokensJSON, &tokens); err != nil {
		return nil, fmt.Errorf("failed to unmarshal tokens: %w", err)
	}

	return &StoredSession{
		TelegramID:  telegramID,
		ToriUserID:  toriUserID,
		Tokens:      tokens,
		LastUpdated: lastUpdated,
	}, nil
}

// Save stores or updates a session.
func (s *SQLiteStore) Save(session *StoredSession) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Serialize and encrypt tokens
	tokensJSON, err := json.Marshal(session.Tokens)
	if err != nil {
		return fmt.Errorf("failed to marshal tokens: %w", err)
	}

	encryptedTokens, err := Encrypt(tokensJSON, s.encryptionKey)
	if err != nil {
		return fmt.Errorf("failed to encrypt tokens: %w", err)
	}

	session.LastUpdated = time.Now()

	_, err = s.db.Exec(`
		INSERT INTO sessions (telegram_id, tori_user_id, encrypted_tokens, last_updated)
		VALUES (?, ?, ?, ?)
		ON CONFLICT(telegram_id) DO UPDATE SET
			tori_user_id = excluded.tori_user_id,
			encrypted_tokens = excluded.encrypted_tokens,
			last_updated = excluded.last_updated
	`, session.TelegramID, session.ToriUserID, encryptedTokens, session.LastUpdated)

	if err != nil {
		return fmt.Errorf("failed to save session: %w", err)
	}

	return nil
}

// Delete removes a session by Telegram user ID.
func (s *SQLiteStore) Delete(telegramID int64) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	_, err := s.db.Exec("DELETE FROM sessions WHERE telegram_id = ?", telegramID)
	if err != nil {
		return fmt.Errorf("failed to delete session: %w", err)
	}

	return nil
}

// GetAll retrieves all stored sessions.
func (s *SQLiteStore) GetAll() ([]StoredSession, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	rows, err := s.db.Query("SELECT telegram_id, tori_user_id, encrypted_tokens, last_updated FROM sessions")
	if err != nil {
		return nil, fmt.Errorf("failed to query sessions: %w", err)
	}
	defer rows.Close()

	var sessions []StoredSession
	for rows.Next() {
		var telegramID int64
		var toriUserID, encryptedTokens string
		var lastUpdated time.Time

		if err := rows.Scan(&telegramID, &toriUserID, &encryptedTokens, &lastUpdated); err != nil {
			return nil, fmt.Errorf("failed to scan row: %w", err)
		}

		tokensJSON, err := Decrypt(encryptedTokens, s.encryptionKey)
		if err != nil {
			return nil, fmt.Errorf("failed to decrypt tokens for user %d: %w", telegramID, err)
		}

		var tokens auth.TokenSet
		if err := json.Unmarshal(tokensJSON, &tokens); err != nil {
			return nil, fmt.Errorf("failed to unmarshal tokens for user %d: %w", telegramID, err)
		}

		sessions = append(sessions, StoredSession{
			TelegramID:  telegramID,
			ToriUserID:  toriUserID,
			Tokens:      tokens,
			LastUpdated: lastUpdated,
		})
	}

	return sessions, rows.Err()
}

// Close closes the database connection.
func (s *SQLiteStore) Close() error {
	return s.db.Close()
}

// SetTemplate sets the description template for a user (replacing any existing one).
func (s *SQLiteStore) SetTemplate(telegramID int64, content string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	query := `
	INSERT INTO templates (telegram_id, content)
	VALUES (?, ?)
	ON CONFLICT(telegram_id) DO UPDATE SET
		content = excluded.content,
		created_at = CURRENT_TIMESTAMP;
	`
	_, err := s.db.Exec(query, telegramID, content)
	if err != nil {
		return fmt.Errorf("failed to upsert template: %w", err)
	}
	return nil
}

// GetTemplate retrieves the user's template.
func (s *SQLiteStore) GetTemplate(telegramID int64) (*Template, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var t Template
	err := s.db.QueryRow(
		"SELECT id, telegram_id, content FROM templates WHERE telegram_id = ? LIMIT 1",
		telegramID,
	).Scan(&t.ID, &t.TelegramID, &t.Content)

	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to query template: %w", err)
	}

	return &t, nil
}

// DeleteTemplate removes the user's template.
func (s *SQLiteStore) DeleteTemplate(telegramID int64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, err := s.db.Exec("DELETE FROM templates WHERE telegram_id = ?", telegramID)
	if err != nil {
		return fmt.Errorf("failed to delete template: %w", err)
	}
	return nil
}
