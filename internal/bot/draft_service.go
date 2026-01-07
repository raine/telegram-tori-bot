package bot

import (
	"context"
	"fmt"

	"github.com/raine/telegram-tori-bot/internal/llm"
	"github.com/raine/telegram-tori-bot/internal/tori"
	"github.com/rs/zerolog/log"
)

// OnModelCallback is called when the AdModel is received during draft creation.
// This allows the caller to handle model data (e.g., populating category cache).
type OnModelCallback func(model *tori.AdModel)

// PhotoInput represents a photo to be processed for draft creation.
type PhotoInput struct {
	FileID string
	Width  int
	Height int
}

// DraftCreationResult contains the result of the draft creation workflow.
type DraftCreationResult struct {
	// Vision analysis results
	Title       string
	Description string

	// Tori draft data
	DraftID string
	ETag    string

	// Uploaded images
	Images []UploadedImage

	// Category predictions
	CategoryPredictions []tori.CategoryPrediction
}

// DraftService encapsulates the common draft creation workflow:
// Download images → Vision analysis → Create Tori draft → Upload images → Patch draft → Get predictions
type DraftService struct {
	visionAnalyzer   llm.Analyzer
	getFileDirectURL func(fileID string) (string, error)
	onModel          OnModelCallback
}

// NewDraftService creates a new DraftService.
func NewDraftService(visionAnalyzer llm.Analyzer, getFileDirectURL func(fileID string) (string, error)) *DraftService {
	return &DraftService{
		visionAnalyzer:   visionAnalyzer,
		getFileDirectURL: getFileDirectURL,
	}
}

// WithOnModelCallback sets a callback that is called when the AdModel is received.
// This is useful for populating caches like the category service.
func (s *DraftService) WithOnModelCallback(callback OnModelCallback) *DraftService {
	s.onModel = callback
	return s
}

// CreateDraftFromPhotos performs the complete draft creation workflow:
// 1. Downloads photos from Telegram
// 2. Analyzes images with vision LLM
// 3. Creates Tori draft
// 4. Uploads images to Tori
// 5. Patches draft with image data
// 6. Gets category predictions
//
// Returns a DraftCreationResult on success, or an error with a user-friendly message.
func (s *DraftService) CreateDraftFromPhotos(
	ctx context.Context,
	client tori.AdService,
	photos []PhotoInput,
) (*DraftCreationResult, error) {
	if len(photos) == 0 {
		return nil, fmt.Errorf("no photos provided")
	}

	// Step 1: Download all photos from Telegram
	photoDataList, validPhotos, err := s.downloadPhotos(ctx, photos)
	if err != nil {
		return nil, err
	}

	// Step 2: Analyze images with vision LLM
	analysisResult, err := s.analyzeImages(ctx, photoDataList)
	if err != nil {
		return nil, err
	}

	log.Info().
		Str("title", analysisResult.Item.Title).
		Int("imageCount", len(photoDataList)).
		Float64("cost", analysisResult.Usage.CostUSD).
		Msg("image(s) analyzed")

	// Step 3: Create Tori draft
	toriDraft, model, err := client.CreateDraftAd(ctx)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", MsgErrDraftCreation, err)
	}

	log.Info().Str("draftId", toriDraft.ID).Msg("draft ad created")

	// Call the model callback if provided (e.g., to populate category cache)
	if s.onModel != nil && model != nil {
		s.onModel(model)
	}

	// Step 4: Upload images to Tori
	uploadedImages, err := s.uploadImages(ctx, client, toriDraft.ID, photoDataList, validPhotos)
	if err != nil {
		return nil, err
	}

	// Step 5: Set images on draft
	etag, err := s.setImagesOnDraft(ctx, client, toriDraft.ID, toriDraft.ETag, uploadedImages)
	if err != nil {
		return nil, err
	}

	// Step 6: Get category predictions
	categories, err := client.GetCategoryPredictions(ctx, toriDraft.ID)
	if err != nil {
		log.Warn().Err(err).Msg("failed to get category predictions")
		categories = []tori.CategoryPrediction{}
	}

	return &DraftCreationResult{
		Title:               analysisResult.Item.Title,
		Description:         analysisResult.Item.Description,
		DraftID:             toriDraft.ID,
		ETag:                etag,
		Images:              uploadedImages,
		CategoryPredictions: categories,
	}, nil
}

// downloadPhotos downloads all photos from Telegram and returns the photo data
// along with the valid photos (those that were successfully downloaded).
func (s *DraftService) downloadPhotos(ctx context.Context, photos []PhotoInput) ([][]byte, []PhotoInput, error) {
	var photoDataList [][]byte
	var validPhotos []PhotoInput

	for _, photo := range photos {
		data, err := DownloadTelegramFile(ctx, s.getFileDirectURL, photo.FileID)
		if err != nil {
			log.Error().Err(err).Str("fileID", photo.FileID).Msg("failed to download photo")
			continue
		}
		photoDataList = append(photoDataList, data)
		validPhotos = append(validPhotos, photo)
	}

	if len(photoDataList) == 0 {
		return nil, nil, fmt.Errorf(MsgErrImageDownload)
	}

	return photoDataList, validPhotos, nil
}

// analyzeImages analyzes the images using the vision LLM.
func (s *DraftService) analyzeImages(ctx context.Context, photoDataList [][]byte) (*llm.AnalysisResult, error) {
	if s.visionAnalyzer == nil {
		return nil, fmt.Errorf(MsgErrImageAnalysis)
	}

	result, err := s.visionAnalyzer.AnalyzeImages(ctx, photoDataList)
	if err != nil {
		log.Error().Err(err).Msg("vision analysis failed")
		return nil, fmt.Errorf("%s: %w", MsgErrAnalysisFailed, err)
	}

	return result, nil
}

// uploadImages uploads all images to the Tori draft and returns the uploaded image info.
func (s *DraftService) uploadImages(
	ctx context.Context,
	client tori.AdService,
	draftID string,
	photoDataList [][]byte,
	validPhotos []PhotoInput,
) ([]UploadedImage, error) {
	var uploadedImages []UploadedImage

	for i, photoData := range photoDataList {
		resp, err := client.UploadImage(ctx, draftID, photoData)
		if err != nil {
			log.Error().Err(err).Int("index", i).Msg("failed to upload photo")
			continue
		}
		uploadedImages = append(uploadedImages, UploadedImage{
			ImagePath: resp.ImagePath,
			Location:  resp.Location,
			Width:     validPhotos[i].Width,
			Height:    validPhotos[i].Height,
		})
	}

	if len(uploadedImages) == 0 {
		return nil, fmt.Errorf(MsgErrImageUpload)
	}

	return uploadedImages, nil
}

// setImagesOnDraft patches the draft with the uploaded image data and returns the new ETag.
func (s *DraftService) setImagesOnDraft(
	ctx context.Context,
	client tori.AdService,
	draftID, etag string,
	images []UploadedImage,
) (string, error) {
	if len(images) == 0 {
		return etag, nil
	}

	imageData := make([]map[string]any, len(images))
	for i, img := range images {
		imageData[i] = map[string]any{
			"uri":    img.ImagePath,
			"width":  img.Width,
			"height": img.Height,
			"type":   "image/jpg",
		}
	}

	patchResp, err := client.PatchItem(ctx, draftID, etag, map[string]any{
		"image": imageData,
	})
	if err != nil {
		return "", fmt.Errorf("%s: %w", MsgErrImageSet, err)
	}

	return patchResp.ETag, nil
}

// UploadAdditionalPhoto uploads a single photo to an existing draft.
// Returns the uploaded image info and the new ETag.
func (s *DraftService) UploadAdditionalPhoto(
	ctx context.Context,
	client tori.AdService,
	draftID, etag string,
	photo PhotoInput,
	existingImages []UploadedImage,
) (*UploadedImage, string, error) {
	// Download the photo
	photoData, err := DownloadTelegramFile(ctx, s.getFileDirectURL, photo.FileID)
	if err != nil {
		return nil, "", fmt.Errorf("failed to download photo: %w", err)
	}

	// Upload to Tori
	resp, err := client.UploadImage(ctx, draftID, photoData)
	if err != nil {
		return nil, "", fmt.Errorf("failed to upload image: %w", err)
	}

	uploaded := &UploadedImage{
		ImagePath: resp.ImagePath,
		Location:  resp.Location,
		Width:     photo.Width,
		Height:    photo.Height,
	}

	// Combine with existing images
	allImages := make([]UploadedImage, len(existingImages)+1)
	copy(allImages, existingImages)
	allImages[len(existingImages)] = *uploaded

	// Set all images on draft
	newEtag, err := s.setImagesOnDraft(ctx, client, draftID, etag, allImages)
	if err != nil {
		return nil, "", err
	}

	return uploaded, newEtag, nil
}
