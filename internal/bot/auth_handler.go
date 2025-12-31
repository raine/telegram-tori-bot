package bot

import (
	"context"

	"github.com/raine/telegram-tori-bot/internal/storage"
	"github.com/raine/telegram-tori-bot/internal/tori/auth"
	"github.com/rs/zerolog/log"
)

// AuthHandler handles authentication flow for the bot.
type AuthHandler struct {
	sessionStore storage.SessionStore
}

// NewAuthHandler creates a new auth handler.
func NewAuthHandler(sessionStore storage.SessionStore) *AuthHandler {
	return &AuthHandler{
		sessionStore: sessionStore,
	}
}

// HandleMessage handles messages during auth flow.
// Returns true if the message was handled (auth flow is active).
// Called from session worker - no locking needed.
func (h *AuthHandler) HandleMessage(ctx context.Context, session *UserSession, text string) bool {
	// Check auth flow timeout
	if session.IsAuthFlowTimedOut() {
		session.authFlow.Reset()
		session.reply(loginTimeoutText)
		return true
	}

	// Check if auth flow is active
	if !session.IsAuthFlowActive() {
		return false
	}

	// Handle auth flow message
	h.handleAuthFlowMessage(ctx, session, text)
	return true
}

// handleAuthFlowMessage handles messages during the login flow.
// Called from session worker - no locking needed.
func (h *AuthHandler) handleAuthFlowMessage(ctx context.Context, session *UserSession, text string) {
	// Handle /peru to cancel login
	if text == "/peru" {
		session.authFlow.Reset()
		session.reply(loginCancelledText)
		return
	}

	// Reject other commands during auth flow
	if len(text) > 0 && text[0] == '/' {
		session.reply(loginInProgressText)
		return
	}

	session.authFlow.Touch()

	switch session.authFlow.State {
	case AuthStateAwaitingEmail:
		h.handleAuthEmail(ctx, session, text)
	case AuthStateAwaitingEmailCode:
		h.handleAuthEmailCode(ctx, session, text)
	case AuthStateAwaitingSMSCode:
		h.handleAuthSMSCode(ctx, session, text)
	}
}

// HandleLoginCommand starts the login flow.
// Called from session worker - no locking needed.
func (h *AuthHandler) HandleLoginCommand(session *UserSession) {
	// Check if already logged in
	if session.isLoggedIn() {
		session.reply(loginAlreadyLoggedInText)
		return
	}

	// Initialize auth flow
	authenticator, err := auth.NewAuthenticator()
	if err != nil {
		session.reply(loginFailedText, err)
		return
	}

	if err := authenticator.InitSession(); err != nil {
		session.reply(loginFailedText, err)
		return
	}

	session.authFlow.Authenticator = authenticator
	session.authFlow.State = AuthStateAwaitingEmail
	session.authFlow.Touch()

	session.reply(loginPromptEmailText)
}

func (h *AuthHandler) handleAuthEmail(ctx context.Context, session *UserSession, email string) {
	session.authFlow.Email = email

	if err := session.authFlow.Authenticator.StartLogin(email); err != nil {
		log.Error().Err(err).Msg("login start failed")
		session.reply(loginFailedText, err)
		session.authFlow.Reset()
		return
	}

	session.authFlow.State = AuthStateAwaitingEmailCode
	session.reply(loginEmailCodeSentText)
}

func (h *AuthHandler) handleAuthEmailCode(ctx context.Context, session *UserSession, code string) {
	mfaRequired, err := session.authFlow.Authenticator.SubmitEmailCode(code)
	if err != nil {
		log.Error().Err(err).Msg("email code submission failed")
		session.reply(loginFailedText, err)
		session.authFlow.Reset()
		return
	}

	if mfaRequired {
		session.authFlow.MFARequired = true
		if err := session.authFlow.Authenticator.RequestSMS(); err != nil {
			log.Error().Err(err).Msg("SMS request failed")
			session.reply(loginFailedText, err)
			session.authFlow.Reset()
			return
		}
		session.authFlow.State = AuthStateAwaitingSMSCode
		session.reply(loginSMSCodeSentText)
		return
	}

	// No MFA required, finalize
	h.finalizeAuth(ctx, session)
}

func (h *AuthHandler) handleAuthSMSCode(ctx context.Context, session *UserSession, code string) {
	if err := session.authFlow.Authenticator.SubmitSMSCode(code); err != nil {
		log.Error().Err(err).Msg("SMS code submission failed")
		session.reply(loginFailedText, err)
		session.authFlow.Reset()
		return
	}

	h.finalizeAuth(ctx, session)
}

func (h *AuthHandler) finalizeAuth(ctx context.Context, session *UserSession) {
	tokens, err := session.authFlow.Authenticator.Finalize()
	if err != nil {
		log.Error().Err(err).Msg("auth finalization failed")
		session.reply(loginFailedText, err)
		session.authFlow.Reset()
		return
	}

	// Save session to store
	if h.sessionStore != nil {
		storedSession := &storage.StoredSession{
			TelegramID: session.userId,
			ToriUserID: tokens.UserID,
			Tokens:     *tokens,
		}
		if err := h.sessionStore.Save(storedSession); err != nil {
			log.Error().Err(err).Msg("failed to save session")
			session.reply(loginFailedText, err)
			session.authFlow.Reset()
			return
		}
	}

	// Update session with tokens
	session.toriAccountId = tokens.UserID
	session.refreshToken = tokens.RefreshToken
	session.deviceID = tokens.DeviceID
	session.bearerToken = tokens.BearerToken

	session.authFlow.Reset()
	session.reply(loginSuccessText)
	log.Info().Int64("userId", session.userId).Str("toriUserId", tokens.UserID).Msg("user logged in successfully")
}

// TryRefreshTokens attempts to refresh the session's tokens using the stored refresh token.
// Note: This method uses locking because it may be called from outside the session worker
// (e.g., during startup or automatic token refresh).
func (h *AuthHandler) TryRefreshTokens(session *UserSession) error {
	session.mu.Lock()
	defer session.mu.Unlock()

	if session.refreshToken == "" {
		return ErrNoRefreshToken
	}
	if session.deviceID == "" {
		return ErrNoDeviceID
	}

	log.Info().Int64("userId", session.userId).Msg("attempting token refresh")

	newTokens, err := auth.RefreshTokens(session.refreshToken, session.deviceID)
	if err != nil {
		return err
	}

	// Update session with new tokens
	session.refreshToken = newTokens.RefreshToken
	session.bearerToken = newTokens.BearerToken
	session.adInputClient = nil // Reset so it gets recreated with new token

	// Persist new tokens to storage
	if h.sessionStore != nil {
		storedSession := &storage.StoredSession{
			TelegramID: session.userId,
			ToriUserID: newTokens.UserID,
			Tokens:     *newTokens,
		}
		if err := h.sessionStore.Save(storedSession); err != nil {
			log.Warn().Err(err).Msg("failed to persist refreshed tokens")
		}
	}

	log.Info().Int64("userId", session.userId).Msg("token refresh successful")
	return nil
}
