package storage

import (
	"database/sql"
	"fmt"
	"time"

	"github.com/google/uuid"
)

// Watch represents a user's search watch/alert.
type Watch struct {
	ID        string
	UserID    int64
	Query     string
	CreatedAt time.Time
}

// CreateWatch creates a new watch for a user.
func (s *SQLiteStore) CreateWatch(userID int64, query string) (*Watch, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	watch := &Watch{
		ID:        uuid.New().String(),
		UserID:    userID,
		Query:     query,
		CreatedAt: time.Now(),
	}

	_, err := s.db.Exec(
		`INSERT INTO watches (id, user_id, query, created_at) VALUES (?, ?, ?, ?)`,
		watch.ID, watch.UserID, watch.Query, watch.CreatedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create watch: %w", err)
	}

	return watch, nil
}

// GetWatchesByUser retrieves all watches for a specific user.
func (s *SQLiteStore) GetWatchesByUser(userID int64) ([]Watch, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	rows, err := s.db.Query(
		`SELECT id, user_id, query, created_at FROM watches WHERE user_id = ? ORDER BY created_at DESC`,
		userID,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to query watches: %w", err)
	}
	defer rows.Close()

	var watches []Watch
	for rows.Next() {
		var w Watch
		if err := rows.Scan(&w.ID, &w.UserID, &w.Query, &w.CreatedAt); err != nil {
			return nil, fmt.Errorf("failed to scan watch: %w", err)
		}
		watches = append(watches, w)
	}

	return watches, rows.Err()
}

// GetAllWatches retrieves all watches across all users (for polling).
func (s *SQLiteStore) GetAllWatches() ([]Watch, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	rows, err := s.db.Query(`SELECT id, user_id, query, created_at FROM watches`)
	if err != nil {
		return nil, fmt.Errorf("failed to query all watches: %w", err)
	}
	defer rows.Close()

	var watches []Watch
	for rows.Next() {
		var w Watch
		if err := rows.Scan(&w.ID, &w.UserID, &w.Query, &w.CreatedAt); err != nil {
			return nil, fmt.Errorf("failed to scan watch: %w", err)
		}
		watches = append(watches, w)
	}

	return watches, rows.Err()
}

// DeleteWatch removes a watch by ID and user ID (for security).
func (s *SQLiteStore) DeleteWatch(id string, userID int64) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	result, err := s.db.Exec(`DELETE FROM watches WHERE id = ? AND user_id = ?`, id, userID)
	if err != nil {
		return fmt.Errorf("failed to delete watch: %w", err)
	}

	rowsAffected, _ := result.RowsAffected()
	if rowsAffected == 0 {
		return fmt.Errorf("watch not found")
	}

	return nil
}

// IsListingSeen checks if a listing has already been seen for a watch.
func (s *SQLiteStore) IsListingSeen(watchID, listingID string) (bool, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var count int
	err := s.db.QueryRow(
		`SELECT COUNT(*) FROM watch_seen_listings WHERE watch_id = ? AND listing_id = ?`,
		watchID, listingID,
	).Scan(&count)
	if err != nil {
		return false, fmt.Errorf("failed to check seen listing: %w", err)
	}

	return count > 0, nil
}

// MarkListingSeen marks a listing as seen for a watch.
func (s *SQLiteStore) MarkListingSeen(watchID, listingID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	_, err := s.db.Exec(
		`INSERT OR IGNORE INTO watch_seen_listings (watch_id, listing_id, seen_at) VALUES (?, ?, ?)`,
		watchID, listingID, time.Now(),
	)
	if err != nil {
		return fmt.Errorf("failed to mark listing as seen: %w", err)
	}

	return nil
}

// MarkListingsSeenBatch marks multiple listings as seen for a watch in one transaction.
func (s *SQLiteStore) MarkListingsSeenBatch(watchID string, listingIDs []string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback()

	stmt, err := tx.Prepare(`INSERT OR IGNORE INTO watch_seen_listings (watch_id, listing_id, seen_at) VALUES (?, ?, ?)`)
	if err != nil {
		return fmt.Errorf("failed to prepare statement: %w", err)
	}
	defer stmt.Close()

	now := time.Now()
	for _, listingID := range listingIDs {
		_, err := stmt.Exec(watchID, listingID, now)
		if err != nil {
			return fmt.Errorf("failed to mark listing as seen: %w", err)
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("failed to commit transaction: %w", err)
	}

	return nil
}

// GetSeenListingIDs returns all seen listing IDs for a watch.
func (s *SQLiteStore) GetSeenListingIDs(watchID string) (map[string]bool, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	rows, err := s.db.Query(`SELECT listing_id FROM watch_seen_listings WHERE watch_id = ?`, watchID)
	if err != nil {
		return nil, fmt.Errorf("failed to query seen listings: %w", err)
	}
	defer rows.Close()

	seen := make(map[string]bool)
	for rows.Next() {
		var listingID string
		if err := rows.Scan(&listingID); err != nil {
			return nil, fmt.Errorf("failed to scan listing ID: %w", err)
		}
		seen[listingID] = true
	}

	return seen, rows.Err()
}

// CountWatchesByUser returns the number of watches for a user.
func (s *SQLiteStore) CountWatchesByUser(userID int64) (int, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var count int
	err := s.db.QueryRow(`SELECT COUNT(*) FROM watches WHERE user_id = ?`, userID).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("failed to count watches: %w", err)
	}

	return count, nil
}

// WatchExistsForQuery checks if a watch already exists for a user and query.
func (s *SQLiteStore) WatchExistsForQuery(userID int64, query string) (bool, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var count int
	err := s.db.QueryRow(
		`SELECT COUNT(*) FROM watches WHERE user_id = ? AND query = ?`,
		userID, query,
	).Scan(&count)
	if err != nil {
		return false, fmt.Errorf("failed to check watch exists: %w", err)
	}

	return count > 0, nil
}

// PruneOldSeenListings removes seen listings older than the given duration.
func (s *SQLiteStore) PruneOldSeenListings(olderThan time.Duration) (int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	cutoff := time.Now().Add(-olderThan)
	result, err := s.db.Exec(`DELETE FROM watch_seen_listings WHERE seen_at < ?`, cutoff)
	if err != nil {
		return 0, fmt.Errorf("failed to prune old seen listings: %w", err)
	}

	return result.RowsAffected()
}

// GetWatch retrieves a single watch by ID.
func (s *SQLiteStore) GetWatch(id string) (*Watch, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var w Watch
	err := s.db.QueryRow(
		`SELECT id, user_id, query, created_at FROM watches WHERE id = ?`,
		id,
	).Scan(&w.ID, &w.UserID, &w.Query, &w.CreatedAt)

	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get watch: %w", err)
	}

	return &w, nil
}
