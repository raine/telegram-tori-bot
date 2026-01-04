package bot

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/rs/zerolog/log"
)

const (
	// DefaultDownloadTimeout is the default timeout for image downloads
	DefaultDownloadTimeout = 30 * time.Second
	// DefaultMaxImageSize is the default maximum image size (10MB)
	DefaultMaxImageSize = 10 * 1024 * 1024
)

// ImageDownloader provides unified image downloading with configurable options.
type ImageDownloader struct {
	client  *http.Client
	timeout time.Duration
	maxSize int64
}

// NewImageDownloader creates a new ImageDownloader with default settings.
func NewImageDownloader() *ImageDownloader {
	return &ImageDownloader{
		client: &http.Client{
			Timeout: DefaultDownloadTimeout,
		},
		timeout: DefaultDownloadTimeout,
		maxSize: DefaultMaxImageSize,
	}
}

// WithTimeout sets a custom timeout for downloads.
func (d *ImageDownloader) WithTimeout(timeout time.Duration) *ImageDownloader {
	d.timeout = timeout
	d.client.Timeout = timeout
	return d
}

// WithMaxSize sets a custom maximum file size.
func (d *ImageDownloader) WithMaxSize(maxSize int64) *ImageDownloader {
	d.maxSize = maxSize
	return d
}

// DownloadFromURL downloads image data from a URL.
// It respects context cancellation and enforces size limits.
func (d *ImageDownloader) DownloadFromURL(ctx context.Context, imageURL string) ([]byte, error) {
	// Create request with timeout context
	reqCtx, cancel := context.WithTimeout(ctx, d.timeout)
	defer cancel()

	req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, imageURL, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	resp, err := d.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to download image: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("download failed: status %d", resp.StatusCode)
	}

	// Validate Content-Type is an image
	contentType := resp.Header.Get("Content-Type")
	if contentType != "" && !strings.HasPrefix(contentType, "image/") {
		return nil, fmt.Errorf("invalid content type: expected image/*, got %s", contentType)
	}

	// Check Content-Length if available
	if resp.ContentLength > d.maxSize {
		return nil, fmt.Errorf("image too large: %d bytes exceeds limit of %d bytes", resp.ContentLength, d.maxSize)
	}

	// Use LimitReader to enforce size limit even if Content-Length is missing or wrong
	limitedReader := io.LimitReader(resp.Body, d.maxSize+1)
	data, err := io.ReadAll(limitedReader)
	if err != nil {
		return nil, fmt.Errorf("failed to read image data: %w", err)
	}

	// Check if we hit the limit
	if int64(len(data)) > d.maxSize {
		return nil, fmt.Errorf("image too large: exceeds limit of %d bytes", d.maxSize)
	}

	return data, nil
}

// DownloadFromTelegramFileID downloads an image from Telegram using a file ID.
// It uses the provided function to resolve the file ID to a direct URL.
func (d *ImageDownloader) DownloadFromTelegramFileID(
	ctx context.Context,
	getFileDirectURL func(fileID string) (string, error),
	fileID string,
) ([]byte, error) {
	log.Info().Str("fileID", fileID).Msg("downloading telegram file")

	url, err := getFileDirectURL(fileID)
	if err != nil {
		return nil, fmt.Errorf("failed to get file URL: %w", err)
	}

	return d.DownloadFromURL(ctx, url)
}

// defaultDownloader is a package-level downloader for convenience functions
var defaultDownloader = NewImageDownloader()

// DownloadImage downloads image data from a URL using the default downloader.
// This is a convenience function that maintains backward compatibility.
func DownloadImage(ctx context.Context, imageURL string) ([]byte, error) {
	return defaultDownloader.DownloadFromURL(ctx, imageURL)
}

// DownloadTelegramFile downloads an image from Telegram using the default downloader.
// This is a convenience function that maintains backward compatibility.
func DownloadTelegramFile(
	ctx context.Context,
	getFileDirectURL func(fileID string) (string, error),
	fileID string,
) ([]byte, error) {
	return defaultDownloader.DownloadFromTelegramFileID(ctx, getFileDirectURL, fileID)
}
