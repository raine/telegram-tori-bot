package main

import (
	"sync"

	"github.com/rs/zerolog/log"
)

type BotState struct {
	bot      *Bot
	mu       sync.Mutex
	sessions map[int64]*UserSession
}

func (bs *BotState) newUserSession(userId int64) (*UserSession, error) {
	session := UserSession{
		userId:   userId,
		sender:   bs.bot.tg,
		authFlow: NewAuthFlow(),
	}

	// Check if user has stored session in database
	if bs.bot.sessionStore != nil {
		storedSession, err := bs.bot.sessionStore.Get(userId)
		if err != nil {
			log.Warn().Err(err).Int64("userId", userId).Msg("failed to get stored session")
		} else if storedSession != nil {
			session.toriAccountId = storedSession.ToriUserID
			session.refreshToken = storedSession.Tokens.RefreshToken
			session.deviceID = storedSession.Tokens.DeviceID
			session.bearerToken = storedSession.Tokens.BearerToken
			log.Info().Int64("userId", userId).Msg("loaded session from database")
			return &session, nil
		}
	}

	// User has no session - they need to log in
	log.Info().Int64("userId", userId).Msg("new user session created (no auth)")
	return &session, nil
}

func (bs *BotState) getUserSession(userId int64) (*UserSession, error) {
	bs.mu.Lock()
	defer bs.mu.Unlock()
	if session, ok := bs.sessions[userId]; !ok {
		session, err := bs.newUserSession(userId)
		if err != nil {
			return nil, err
		} else {
			bs.sessions[userId] = session
			return session, nil
		}
	} else {
		return session, nil
	}
}

func (b *Bot) NewBotState() BotState {
	return BotState{
		bot:      b,
		sessions: make(map[int64]*UserSession),
	}
}
