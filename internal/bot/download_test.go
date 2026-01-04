package bot

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"github.com/stretchr/testify/assert"
)

func TestDownloadTelegramFile_Success(t *testing.T) {
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
	defer ts.Close()

	getFileDirectURL := func(fileID string) (string, error) {
		return fmt.Sprintf("%s/%s.jpeg", ts.URL, fileID), nil
	}

	photoSize := tgbotapi.PhotoSize{
		FileID:       "foo",
		FileUniqueID: "1",
		Width:        371,
		Height:       495,
		FileSize:     28548,
	}

	bytes, err := DownloadTelegramFile(context.Background(), getFileDirectURL, photoSize.FileID)
	if err != nil {
		t.Fatal(err)
	}

	assert.Equal(t, []byte("123"), bytes)
	assert.True(t, handlerCalled)
}

func TestDownloadTelegramFile_URLResolutionError(t *testing.T) {
	getFileDirectURL := func(fileID string) (string, error) {
		return "", fmt.Errorf("failed to get URL")
	}

	_, err := DownloadTelegramFile(context.Background(), getFileDirectURL, "test-file-id")
	if err == nil {
		t.Fatal("expected error for URL resolution failure")
	}
	if !strings.Contains(err.Error(), "failed to get file URL") {
		t.Errorf("expected error message to contain 'failed to get file URL', got: %v", err)
	}
}

func TestImageDownloader_DownloadFromURL_Success(t *testing.T) {
	imageData := []byte{0x89, 0x50, 0x4E, 0x47} // PNG magic bytes
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/png")
		w.WriteHeader(http.StatusOK)
		w.Write(imageData)
	}))
	defer ts.Close()

	downloader := NewImageDownloader()
	data, err := downloader.DownloadFromURL(context.Background(), ts.URL)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if len(data) != len(imageData) {
		t.Errorf("expected %d bytes, got %d", len(imageData), len(data))
	}
}

func TestImageDownloader_DownloadFromURL_NotFound(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer ts.Close()

	downloader := NewImageDownloader()
	_, err := downloader.DownloadFromURL(context.Background(), ts.URL)
	if err == nil {
		t.Fatal("expected error for 404 response")
	}
}

func TestImageDownloader_DownloadFromURL_ContextCanceled(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// This should never be reached
		t.Error("request should have been canceled")
	}))
	defer ts.Close()

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	downloader := NewImageDownloader()
	_, err := downloader.DownloadFromURL(ctx, ts.URL)
	if err == nil {
		t.Fatal("expected error for canceled context")
	}
}

func TestImageDownloader_DownloadFromURL_SizeLimit(t *testing.T) {
	// Create a large response
	largeData := make([]byte, 100)
	for i := range largeData {
		largeData[i] = byte(i % 256)
	}

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/jpeg")
		w.WriteHeader(http.StatusOK)
		w.Write(largeData)
	}))
	defer ts.Close()

	// Create downloader with small max size
	downloader := NewImageDownloader().WithMaxSize(50)
	_, err := downloader.DownloadFromURL(context.Background(), ts.URL)
	if err == nil {
		t.Fatal("expected error for file exceeding size limit")
	}
	if !strings.Contains(err.Error(), "too large") {
		t.Errorf("expected error message to contain 'too large', got: %v", err)
	}
}

func TestImageDownloader_DownloadFromURL_ContentLengthExceedsLimit(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/jpeg")
		w.Header().Set("Content-Length", "999999999")
		w.WriteHeader(http.StatusOK)
		// Don't actually write that much data
	}))
	defer ts.Close()

	downloader := NewImageDownloader().WithMaxSize(1000)
	_, err := downloader.DownloadFromURL(context.Background(), ts.URL)
	if err == nil {
		t.Fatal("expected error when Content-Length exceeds limit")
	}
	if !strings.Contains(err.Error(), "too large") {
		t.Errorf("expected error message to contain 'too large', got: %v", err)
	}
}

func TestImageDownloader_DownloadFromURL_InvalidContentType(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("<html>not an image</html>"))
	}))
	defer ts.Close()

	downloader := NewImageDownloader()
	_, err := downloader.DownloadFromURL(context.Background(), ts.URL)
	if err == nil {
		t.Fatal("expected error for non-image content type")
	}
	if !strings.Contains(err.Error(), "invalid content type") {
		t.Errorf("expected error message to contain 'invalid content type', got: %v", err)
	}
}

func TestImageDownloader_DownloadFromURL_AcceptsImageContentType(t *testing.T) {
	imageData := []byte{0x89, 0x50, 0x4E, 0x47} // PNG magic bytes
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/png")
		w.WriteHeader(http.StatusOK)
		w.Write(imageData)
	}))
	defer ts.Close()

	downloader := NewImageDownloader()
	data, err := downloader.DownloadFromURL(context.Background(), ts.URL)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if len(data) != len(imageData) {
		t.Errorf("expected %d bytes, got %d", len(imageData), len(data))
	}
}
