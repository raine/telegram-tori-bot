package main

import (
	"context"
	"time"

	"github.com/raine/telegram-tori-bot/tori"
	"github.com/rs/zerolog/log"
)

func refreshUserConfigAccountToken(toriApiBaseUrl string, cfg UserConfigItem) {
	client := tori.NewClient(tori.ClientOpts{
		Auth:    cfg.Token,
		BaseURL: toriApiBaseUrl,
	})

	log.Info().Str("toriAccountId", cfg.ToriAccountId).
		Msg("getting tori account to refresh session")

	_, err := client.GetAccount(cfg.ToriAccountId)
	if err != nil {
		log.Info().Str("toriAccountId", cfg.ToriAccountId).
			Msg("could not get tori account to refresh session")
	}
}

// keepSessionsAlive periodically refreshes user sessions to prevent token expiration.
// It appears that access_token does not expire as quickly if there's activity
// associated to it, but haven't been able to confirm.
func keepSessionsAlive(ctx context.Context, toriApiBaseUrl string, userConfigMap UserConfigMap) error {
	refresh := func() {
		for _, cfg := range userConfigMap {
			refreshUserConfigAccountToken(toriApiBaseUrl, cfg)
		}
	}

	// Run immediately on startup
	refresh()

	ticker := time.NewTicker(time.Hour * 24)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			log.Info().Msg("stopping session keep-alive")
			return ctx.Err()
		case <-ticker.C:
			refresh()
		}
	}
}
