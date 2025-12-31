package bot

import (
	"fmt"
	"time"

	"github.com/go-resty/resty/v2"
	"github.com/rs/zerolog/log"
)

// httpClient is reused for file downloads to avoid creating new clients per request
var httpClient = resty.New().SetDebug(false).SetTimeout(30 * time.Second)

func downloadFileID(
	getFileDirectURL func(fileId string) (string, error),
	fileID string,
) ([]byte, error) {
	log.Info().Interface("fileID", fileID).Msg("downloading file id")
	url, err := getFileDirectURL(fileID)
	if err != nil {
		return nil, err
	}
	res, err := httpClient.R().Get(url)
	if err != nil {
		return nil, err
	}
	if res.IsError() {
		return nil, fmt.Errorf("request failed: %v", res)
	}

	return res.Body(), nil
}
