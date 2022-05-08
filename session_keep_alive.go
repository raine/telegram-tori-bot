package main

import (
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

// It appears that access_token does not expire as quickly if there's activity
// associated to it, but haven't been able to confirm
func keepSessionsAlive(toriApiBaseUrl string, userConfigMap UserConfigMap) {
	ticker := time.NewTicker(time.Hour * 24)

	go func() {
		for range ticker.C {
			for _, cfg := range userConfigMap {
				refreshUserConfigAccountToken(toriApiBaseUrl, cfg)
			}
		}
	}()
}
