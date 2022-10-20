package main

import (
	"github.com/go-resty/resty/v2"
	"github.com/pkg/errors"
	"github.com/rs/zerolog/log"
)

func downloadFileID(
	getFileDirectURL func(fileId string) (string, error),
	fileID string,
) ([]byte, error) {
	log.Info().Interface("fileID", fileID).Msg("downloading file id")
	url, err := getFileDirectURL(fileID)
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
