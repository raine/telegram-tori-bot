package main

import (
	"sync"

	"github.com/pkg/errors"
	"github.com/raine/telegram-tori-bot/tori"
	"github.com/rs/zerolog/log"
)

type BotState struct {
	bot      *Bot
	mu       sync.Mutex
	sessions map[int64]*UserSession
}

func (bs *BotState) newUserSession(userId int64) (*UserSession, error) {
	cfg, ok := bs.bot.userConfigMap[userId]
	if !ok {
		return nil, errors.Errorf("user %d has no config; if this is you, add user with telegramUserId = %d to user_config.toml", userId, userId)
	}

	session := UserSession{
		userId:        userId,
		toriAccountId: cfg.ToriAccountId,
		client: tori.NewClient(tori.ClientOpts{
			Auth:    cfg.Token,
			BaseURL: bs.bot.toriApiBaseUrl,
		}),
		bot: bs.bot,
	}
	log.Info().Int64("userId", userId).Msg("new user session created")
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
