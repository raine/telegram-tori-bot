package storage

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/raine/telegram-tori-bot/internal/tori/auth"
	"github.com/rs/zerolog/log"
	_ "modernc.org/sqlite"
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

// VisionCacheEntry represents a cached vision analysis result.
type VisionCacheEntry struct {
	Title       string
	Description string
	Brand       string
	Model       string
}

// AllowedUser represents a user in the whitelist.
type AllowedUser struct {
	TelegramID int64
	AddedAt    time.Time
	AddedBy    int64
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

	// Postal code methods
	SetPostalCode(telegramID int64, postalCode string) error
	GetPostalCode(telegramID int64) (string, error)

	// Installation ID methods (for Tori API finn-app-installation-id header)
	GetInstallationID(telegramID int64) (string, error)
	SetInstallationID(telegramID int64, installationID string) error

	// Vision cache methods
	GetVisionCache(imageHash string) (*VisionCacheEntry, error)
	SetVisionCache(imageHash string, entry *VisionCacheEntry) error

	// Allowed users methods
	IsUserAllowed(telegramID int64) (bool, error)
	AddAllowedUser(telegramID, addedBy int64) error
	RemoveAllowedUser(telegramID int64) error
	GetAllowedUsers() ([]AllowedUser, error)
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
	// Configure SQLite with WAL mode and busy timeout for better concurrency
	dsn := fmt.Sprintf("%s?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)", dbPath)
	db, err := sql.Open("sqlite", dsn)
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

	userSettingsQuery := `
	CREATE TABLE IF NOT EXISTS user_settings (
		telegram_id INTEGER PRIMARY KEY,
		postal_code TEXT,
		installation_id TEXT
	);
	`
	_, err = s.db.Exec(userSettingsQuery)
	if err != nil {
		return fmt.Errorf("failed to create user_settings table: %w", err)
	}

	// Migration: add installation_id column if it doesn't exist (for existing databases)
	if _, err := s.db.Exec("ALTER TABLE user_settings ADD COLUMN installation_id TEXT"); err != nil {
		// "duplicate column name" error is expected if column already exists
		if !strings.Contains(err.Error(), "duplicate column name") {
			log.Warn().Err(err).Msg("failed to add installation_id column (migration)")
		}
	}

	visionCacheQuery := `
	CREATE TABLE IF NOT EXISTS vision_cache (
		image_hash TEXT PRIMARY KEY,
		title TEXT NOT NULL,
		description TEXT NOT NULL,
		brand TEXT,
		model TEXT,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP
	);
	`
	_, err = s.db.Exec(visionCacheQuery)
	if err != nil {
		return fmt.Errorf("failed to create vision_cache table: %w", err)
	}

	allowedUsersQuery := `
	CREATE TABLE IF NOT EXISTS allowed_users (
		telegram_id INTEGER PRIMARY KEY,
		added_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		added_by INTEGER
	);
	`
	_, err = s.db.Exec(allowedUsersQuery)
	if err != nil {
		return fmt.Errorf("failed to create allowed_users table: %w", err)
	}

	watchesQuery := `
	CREATE TABLE IF NOT EXISTS watches (
		id TEXT PRIMARY KEY,
		user_id INTEGER NOT NULL,
		query TEXT NOT NULL,
		created_at DATETIME NOT NULL
	);
	`
	_, err = s.db.Exec(watchesQuery)
	if err != nil {
		return fmt.Errorf("failed to create watches table: %w", err)
	}

	watchSeenListingsQuery := `
	CREATE TABLE IF NOT EXISTS watch_seen_listings (
		watch_id TEXT NOT NULL,
		listing_id TEXT NOT NULL,
		seen_at DATETIME NOT NULL,
		PRIMARY KEY (watch_id, listing_id),
		FOREIGN KEY (watch_id) REFERENCES watches(id) ON DELETE CASCADE
	);
	`
	_, err = s.db.Exec(watchSeenListingsQuery)
	if err != nil {
		return fmt.Errorf("failed to create watch_seen_listings table: %w", err)
	}

	// Enable foreign keys for cascade delete
	_, err = s.db.Exec("PRAGMA foreign_keys = ON")
	if err != nil {
		return fmt.Errorf("failed to enable foreign keys: %w", err)
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

// SetPostalCode sets the postal code for a user.
func (s *SQLiteStore) SetPostalCode(telegramID int64, postalCode string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	query := `
	INSERT INTO user_settings (telegram_id, postal_code)
	VALUES (?, ?)
	ON CONFLICT(telegram_id) DO UPDATE SET
		postal_code = excluded.postal_code;
	`
	_, err := s.db.Exec(query, telegramID, postalCode)
	if err != nil {
		return fmt.Errorf("failed to set postal code: %w", err)
	}
	return nil
}

// GetPostalCode retrieves the postal code for a user.
// Returns empty string if not set.
func (s *SQLiteStore) GetPostalCode(telegramID int64) (string, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var postalCode sql.NullString
	err := s.db.QueryRow(
		"SELECT postal_code FROM user_settings WHERE telegram_id = ?",
		telegramID,
	).Scan(&postalCode)

	if err == sql.ErrNoRows {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("failed to query postal code: %w", err)
	}

	return postalCode.String, nil
}

// GetInstallationID retrieves the installation ID for a user.
// Returns empty string if not set.
func (s *SQLiteStore) GetInstallationID(telegramID int64) (string, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var installationID sql.NullString
	err := s.db.QueryRow(
		"SELECT installation_id FROM user_settings WHERE telegram_id = ?",
		telegramID,
	).Scan(&installationID)

	if err == sql.ErrNoRows {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("failed to query installation id: %w", err)
	}

	return installationID.String, nil
}

// SetInstallationID sets the installation ID for a user.
func (s *SQLiteStore) SetInstallationID(telegramID int64, installationID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	query := `
	INSERT INTO user_settings (telegram_id, installation_id)
	VALUES (?, ?)
	ON CONFLICT(telegram_id) DO UPDATE SET
		installation_id = excluded.installation_id;
	`
	_, err := s.db.Exec(query, telegramID, installationID)
	if err != nil {
		return fmt.Errorf("failed to set installation id: %w", err)
	}
	return nil
}

// GetVisionCache retrieves a cached vision analysis result by image hash.
// Returns nil, nil if no cache entry exists.
func (s *SQLiteStore) GetVisionCache(imageHash string) (*VisionCacheEntry, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var entry VisionCacheEntry
	var brand, model sql.NullString
	err := s.db.QueryRow(
		"SELECT title, description, brand, model FROM vision_cache WHERE image_hash = ?",
		imageHash,
	).Scan(&entry.Title, &entry.Description, &brand, &model)

	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to query vision cache: %w", err)
	}

	entry.Brand = brand.String
	entry.Model = model.String

	return &entry, nil
}

// SetVisionCache stores a vision analysis result in the cache.
func (s *SQLiteStore) SetVisionCache(imageHash string, entry *VisionCacheEntry) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	_, err := s.db.Exec(`
		INSERT INTO vision_cache (image_hash, title, description, brand, model)
		VALUES (?, ?, ?, ?, ?)
		ON CONFLICT(image_hash) DO UPDATE SET
			title = excluded.title,
			description = excluded.description,
			brand = excluded.brand,
			model = excluded.model,
			created_at = CURRENT_TIMESTAMP
	`, imageHash, entry.Title, entry.Description, entry.Brand, entry.Model)

	if err != nil {
		return fmt.Errorf("failed to cache vision result: %w", err)
	}
	return nil
}

// IsUserAllowed checks if a user is in the whitelist.
func (s *SQLiteStore) IsUserAllowed(telegramID int64) (bool, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var count int
	err := s.db.QueryRow(
		"SELECT COUNT(*) FROM allowed_users WHERE telegram_id = ?",
		telegramID,
	).Scan(&count)

	if err != nil {
		return false, fmt.Errorf("failed to check allowed user: %w", err)
	}

	return count > 0, nil
}

// AddAllowedUser adds a user to the whitelist.
func (s *SQLiteStore) AddAllowedUser(telegramID, addedBy int64) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	_, err := s.db.Exec(`
		INSERT INTO allowed_users (telegram_id, added_by)
		VALUES (?, ?)
		ON CONFLICT(telegram_id) DO UPDATE SET
			added_by = excluded.added_by,
			added_at = CURRENT_TIMESTAMP
	`, telegramID, addedBy)

	if err != nil {
		return fmt.Errorf("failed to add allowed user: %w", err)
	}
	return nil
}

// RemoveAllowedUser removes a user from the whitelist.
func (s *SQLiteStore) RemoveAllowedUser(telegramID int64) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	_, err := s.db.Exec("DELETE FROM allowed_users WHERE telegram_id = ?", telegramID)
	if err != nil {
		return fmt.Errorf("failed to remove allowed user: %w", err)
	}
	return nil
}

// GetAllowedUsers returns all users in the whitelist.
func (s *SQLiteStore) GetAllowedUsers() ([]AllowedUser, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	rows, err := s.db.Query("SELECT telegram_id, added_at, added_by FROM allowed_users ORDER BY added_at")
	if err != nil {
		return nil, fmt.Errorf("failed to query allowed users: %w", err)
	}
	defer rows.Close()

	var users []AllowedUser
	for rows.Next() {
		var user AllowedUser
		if err := rows.Scan(&user.TelegramID, &user.AddedAt, &user.AddedBy); err != nil {
			return nil, fmt.Errorf("failed to scan allowed user: %w", err)
		}
		users = append(users, user)
	}

	return users, rows.Err()
}
