package bot

import (
	"time"

	"github.com/raine/telegram-tori-bot/internal/tori/auth"
)

// AuthState represents the current state of the authentication flow.
type AuthState int

const (
	AuthStateNone AuthState = iota
	AuthStateAwaitingEmail
	AuthStateAwaitingEmailCode
	AuthStateAwaitingSMSCode
)

// AuthFlowTimeout is how long we wait for user input before resetting the auth flow.
const AuthFlowTimeout = 15 * time.Minute

// AuthFlow tracks the state of an ongoing authentication attempt.
type AuthFlow struct {
	State           AuthState
	Email           string
	Authenticator   *auth.Authenticator
	MFARequired     bool
	LastInteraction time.Time
}

// NewAuthFlow creates a new auth flow in the initial state.
func NewAuthFlow() *AuthFlow {
	return &AuthFlow{
		State:           AuthStateNone,
		LastInteraction: time.Now(),
	}
}

// IsActive returns true if an auth flow is in progress.
func (f *AuthFlow) IsActive() bool {
	return f.State != AuthStateNone
}

// IsTimedOut returns true if the auth flow has been inactive for too long.
func (f *AuthFlow) IsTimedOut() bool {
	if !f.IsActive() {
		return false
	}
	return time.Since(f.LastInteraction) > AuthFlowTimeout
}

// Reset clears the auth flow state.
func (f *AuthFlow) Reset() {
	f.State = AuthStateNone
	f.Email = ""
	f.Authenticator = nil
	f.MFARequired = false
	f.LastInteraction = time.Now()
}

// Touch updates the last interaction time.
func (f *AuthFlow) Touch() {
	f.LastInteraction = time.Now()
}

func (s AuthState) String() string {
	switch s {
	case AuthStateNone:
		return "None"
	case AuthStateAwaitingEmail:
		return "AwaitingEmail"
	case AuthStateAwaitingEmailCode:
		return "AwaitingEmailCode"
	case AuthStateAwaitingSMSCode:
		return "AwaitingSMSCode"
	default:
		return "Unknown"
	}
}
