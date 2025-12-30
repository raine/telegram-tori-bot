package llm

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"

	"github.com/raine/telegram-tori-bot/storage"
	"github.com/rs/zerolog/log"
)

// CachedAnalyzer wraps an Analyzer with SQLite caching.
type CachedAnalyzer struct {
	inner Analyzer
	store storage.SessionStore
}

// NewCachedAnalyzer creates a cached analyzer.
func NewCachedAnalyzer(inner Analyzer, store storage.SessionStore) *CachedAnalyzer {
	return &CachedAnalyzer{inner: inner, store: store}
}

// hashImages creates a SHA256 hash from image data.
// Includes length prefix for each image to prevent boundary collisions.
func hashImages(images [][]byte) string {
	h := sha256.New()
	for _, img := range images {
		// Write length to prevent boundary collisions (e.g. [A,B] vs [AB])
		binary.Write(h, binary.LittleEndian, int64(len(img)))
		h.Write(img)
	}
	return hex.EncodeToString(h.Sum(nil))
}

// AnalyzeImage implements the Analyzer interface with caching.
func (c *CachedAnalyzer) AnalyzeImage(ctx context.Context, imageData []byte, mimeType string) (*AnalysisResult, error) {
	return c.AnalyzeImages(ctx, [][]byte{imageData})
}

// AnalyzeImages implements the Analyzer interface with caching.
func (c *CachedAnalyzer) AnalyzeImages(ctx context.Context, images [][]byte) (*AnalysisResult, error) {
	hash := hashImages(images)

	// Check cache
	if c.store != nil {
		cached, err := c.store.GetVisionCache(hash)
		if err != nil {
			log.Warn().Err(err).Msg("failed to check vision cache")
		} else if cached != nil {
			log.Debug().Str("hash", hash[:16]).Msg("vision cache hit")
			return &AnalysisResult{
				Item: &ItemDescription{
					Title:       cached.Title,
					Description: cached.Description,
					Brand:       cached.Brand,
					Model:       cached.Model,
				},
				Usage: Usage{}, // Zero usage for cached result
			}, nil
		}
	}

	// Call underlying analyzer
	result, err := c.inner.AnalyzeImages(ctx, images)
	if err != nil {
		return nil, err
	}

	// Cache the result
	if c.store != nil && result.Item != nil {
		entry := &storage.VisionCacheEntry{
			Title:       result.Item.Title,
			Description: result.Item.Description,
			Brand:       result.Item.Brand,
			Model:       result.Item.Model,
		}
		if err := c.store.SetVisionCache(hash, entry); err != nil {
			log.Warn().Err(err).Msg("failed to cache vision result")
		} else {
			log.Debug().Str("hash", hash[:16]).Msg("cached vision result")
		}
	}

	return result, nil
}

// Gemini returns the underlying GeminiAnalyzer if available.
// This allows access to GeminiAnalyzer-specific methods like SelectCategory.
func (c *CachedAnalyzer) Gemini() *GeminiAnalyzer {
	if g, ok := c.inner.(*GeminiAnalyzer); ok {
		return g
	}
	return nil
}

// GetGeminiAnalyzer extracts GeminiAnalyzer from an Analyzer.
// Recursively unwraps CachedAnalyzer wrappers to find the underlying GeminiAnalyzer.
func GetGeminiAnalyzer(a Analyzer) *GeminiAnalyzer {
	curr := a
	for {
		switch t := curr.(type) {
		case *GeminiAnalyzer:
			return t
		case *CachedAnalyzer:
			curr = t.inner
		default:
			return nil
		}
	}
}
