package bot

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"github.com/stretchr/testify/assert"
)

func TestDownloadPhotoSize(t *testing.T) {
	var handlerCalled bool
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/foo.jpeg" {
			handlerCalled = true
			w.Header().Set("Content-Type", "image/jpeg")
			w.Write([]byte("123"))
		} else {
			t.Fatal(fmt.Sprintf("invalid request to test server: %s %s", r.Method, r.URL.Path))
		}
	}))

	getFileDirectUrl := func(fileId string) (string, error) {
		return fmt.Sprintf("%s/%s.jpeg", ts.URL, fileId), nil
	}

	photoSize := tgbotapi.PhotoSize{
		FileID:       "foo",
		FileUniqueID: "1",
		Width:        371,
		Height:       495,
		FileSize:     28548,
	}

	bytes, err := downloadFileID(getFileDirectUrl, photoSize.FileID)
	if err != nil {
		t.Fatal(err)
	}

	assert.Equal(t, []byte("123"), bytes)
	assert.True(t, handlerCalled)
}
