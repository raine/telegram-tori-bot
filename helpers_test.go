package main

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"github.com/raine/telegram-tori-bot/tori"
	"github.com/stretchr/testify/assert"
)

func TestUploadListingPhotos(t *testing.T) {
	var mediaId atomic.Int32
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/a.jpg", "/b.jpg", "/c.jpg":
			w.Header().Set("Content-Type", "image/jpeg")
			w.Write([]byte("123"))
		case "/v2.2/media":
			w.Header().Set("Content-Type", "plain/text") // Yes, really
			id := mediaId.Add(1)
			json := fmt.Sprintf(`{"image":{"url":"https://images.tori.fi/api/v1/imagestori/images/%d.jpg?rule=images","id":"%d"}}`, id, id)
			io.WriteString(w, json)
		}
	}))

	getFileDirectUrl := func(fileId string) (string, error) {
		return fmt.Sprintf("%s/%s.jpg", ts.URL, fileId), nil
	}

	photoSizes := []tgbotapi.PhotoSize{
		{FileID: "a", FileUniqueID: "1", Width: 371, Height: 495, FileSize: 28548},
		{FileID: "b", FileUniqueID: "1", Width: 371, Height: 495, FileSize: 28548},
		{FileID: "c", FileUniqueID: "1", Width: 371, Height: 495, FileSize: 28548},
	}

	client := tori.NewClient(tori.ClientOpts{
		BaseURL: ts.URL,
		Auth:    "foo",
	})

	got, _ := uploadListingPhotos(context.Background(), getFileDirectUrl, client.UploadMedia, photoSizes)
	want := []tori.Media{
		{Id: "1", Url: "https://images.tori.fi/api/v1/imagestori/images/1.jpg?rule=images"},
		{Id: "2", Url: "https://images.tori.fi/api/v1/imagestori/images/2.jpg?rule=images"},
		{Id: "3", Url: "https://images.tori.fi/api/v1/imagestori/images/3.jpg?rule=images"},
	}
	assert.ElementsMatch(t, want, got)
}
