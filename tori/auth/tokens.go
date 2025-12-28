package auth

import "time"

// TokenSet holds all tokens obtained from the authentication flow.
type TokenSet struct {
	UserID       string    `json:"user_id"`
	BearerToken  string    `json:"bearer_token"`  // Tori API token
	AccessToken  string    `json:"access_token"`  // OAuth access token
	RefreshToken string    `json:"refresh_token"` // OAuth refresh token
	IDToken      string    `json:"id_token"`      // OAuth ID token
	DeviceID     string    `json:"device_id"`     // Unique device ID (UUID) per user
	ExpiresAt    time.Time `json:"expires_at,omitempty"`
}

// IsExpired returns true if the token set has expired.
func (t *TokenSet) IsExpired() bool {
	if t.ExpiresAt.IsZero() {
		return false
	}
	return time.Now().After(t.ExpiresAt)
}
