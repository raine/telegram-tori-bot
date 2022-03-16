package main

import (
	"fmt"
	"sync"

	"github.com/raine/go-telegram-bot/tori"
	"github.com/rs/zerolog/log"
)

type BotState struct {
	bot      *Bot
	mu       sync.Mutex
	sessions map[int64]*UserSession
}

func (bs *BotState) newUserSession(userId int64) (*UserSession, error) {
	token, ok := bs.bot.authMap[userId]
	if !ok {
		return nil, fmt.Errorf("user %d has no auth token set", userId)
	}

	session := UserSession{
		userId: userId,
		client: tori.NewClient(tori.ClientOpts{
			Auth:    token,
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
