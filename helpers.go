package main

import (
	"context"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"github.com/raine/telegram-tori-bot/tori"
	"github.com/rs/zerolog/log"
	"golang.org/x/sync/errgroup"
)

// uploadListingPhotos uploads given tgbotapi.PhotoSizes to tori
func uploadListingPhotos(
	ctx context.Context,
	getFileDirectURL func(fileId string) (string, error),
	toriUploadMedia func(ctx context.Context, data []byte) (tori.Media, error),
	photoSizes []tgbotapi.PhotoSize,
) ([]tori.Media, error) {
	medias := make([]tori.Media, len(photoSizes))
	g, ctx := errgroup.WithContext(ctx)
	for i := range photoSizes {
		i := i
		g.Go(func() error {
			photo, err := downloadFileID(getFileDirectURL, photoSizes[i].FileID)
			if err != nil {
				log.Error().Err(err).Msg("failed to download photo size")
				return err
			}

			m, err := toriUploadMedia(ctx, photo)
			if err != nil {
				log.Error().Err(err).Msg("failed to upload photo to tori")
				return err
			}

			medias[i] = m
			return nil
		})
	}

	err := g.Wait()
	if err != nil {
		return medias, err
	}
	return medias, nil
}
