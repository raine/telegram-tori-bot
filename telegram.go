package main

import (
	"github.com/go-resty/resty/v2"
	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"github.com/pkg/errors"
	"github.com/rs/zerolog/log"
)

func downloadPhotoSize(
	getFileDirectURL func(fileId string) (string, error),
	photoSize tgbotapi.PhotoSize,
) ([]byte, error) {
	log.Info().Interface("photoSize", photoSize).Msg("downloading photo size")
	url, err := getFileDirectURL(photoSize.FileID)
	if err != nil {
		return nil, err
	}
	client := resty.New().SetDebug(false)
	res, err := client.R().Get(url)
	if err != nil {
		return nil, err
	}
	if res.IsError() {
		return nil, errors.Errorf("request failed: %v", res)
	}

	return res.Body(), nil
}
