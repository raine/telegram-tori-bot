package main

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"slices"
	"strings"
	"time"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"github.com/raine/telegram-tori-bot/tori"
	"github.com/rs/zerolog/log"
)

type BotAPI interface {
	Send(c tgbotapi.Chattable) (tgbotapi.Message, error)
	Request(c tgbotapi.Chattable) (*tgbotapi.APIResponse, error)
	GetFileDirectURL(fileID string) (string, error)
}

type Bot struct {
	tg             BotAPI
	state          BotState
	toriApiBaseUrl string
	userConfigMap  UserConfigMap
}

func NewBot(tg BotAPI, userConfigMap UserConfigMap, toriApiBaseUrl string) *Bot {
	bot := &Bot{
		tg:             tg,
		userConfigMap:  userConfigMap,
		toriApiBaseUrl: toriApiBaseUrl,
	}

	bot.state = bot.NewBotState()
	return bot
}

func (b *Bot) handlePhoto(message *tgbotapi.Message) {
	session, err := b.state.getUserSession(message.From.ID)
	if err != nil {
		log.Error().Err(err).Send()
		return
	}

	// When photos are sent as a "media group" that appear like a single message
	// with multiple photos, the photos are in fact sent one by one in separate
	// messages. To give feedback like "n photos added", we have to wait a bit
	// after the first photo is sent and keep track of photos since then.
	//
	// Also, in media groups, photos have an order. It looks like the order is
	// based on message's id. So eventually we need to add uploaded photos
	// to session.photo in ordered by message id.
	if session.pendingPhotos == nil {
		session.pendingPhotos = new([]PendingPhoto)

		go func() {
			env, _ := os.LookupEnv("GO_ENV")
			if env == "test" {
				time.Sleep(1000 * time.Microsecond)
			} else {
				time.Sleep(1 * time.Second)
			}

			// Order pending photos batch based on message id, which is the
			// order in which message were sent, but not necessary the order
			// they are processed by the program
			slices.SortStableFunc(*session.pendingPhotos, func(a, b PendingPhoto) int {
				return cmp.Compare(a.messageId, b.messageId)
			})

			for _, pendingPhoto := range *session.pendingPhotos {
				session.photos = append(session.photos, pendingPhoto.photoSize)
			}

			session.reply("%s lisätty", pluralize("kuva", "kuvaa", len(*session.pendingPhotos)))
			session.pendingPhotos = nil
			log.Info().Interface("photos", session.photos).Msg("added pending photos to session")
		}()
	}

	// message.Photo is an array of PhotoSizes and the last one is the largest size
	largestPhoto := message.Photo[len(message.Photo)-1]
	url, err := b.tg.GetFileDirectURL(largestPhoto.FileID)
	if err != nil {
		log.Error().Err(err).Msg("failed to get photo url")
		return
	}

	log.Info().Interface("photo", largestPhoto).Str("url", url).Int("messageId", message.MessageID).Msg("added photo to pending photos")
	pendingPhoto := PendingPhoto{
		messageId: message.MessageID,
		photoSize: largestPhoto,
	}
	pendingPhotos := append(*session.pendingPhotos, pendingPhoto)
	session.pendingPhotos = &pendingPhotos
}

// handleCallback is called when a tgbotapi.update with CallbackQuery is
// received. That happens when user interacts with an inline keyboard with
// callback data.
func (b *Bot) handleCallback(ctx context.Context, update tgbotapi.Update) {
	log.Info().Msg("got callback")

	session, err := b.state.getUserSession(update.CallbackQuery.From.ID)
	if err != nil {
		log.Error().Err(err).Send()
		return
	}

	session.mu.Lock()
	defer session.mu.Unlock()

	var newCategoryCode string
	for _, c := range session.categories {
		if c.Label == update.CallbackQuery.Data {
			newCategoryCode = c.Code
		}
	}

	newadFilters, err := fetchNewadFilters(ctx, session.client.GetFiltersSectionNewad)
	if err != nil {
		session.replyWithError(err)
		return
	}
	missingFieldBefore := tori.GetMissingListingField(newadFilters.Newad.ParamMap, newadFilters.Newad.SettingsParams, *session.listing)

	session.listing.Category = newCategoryCode
	// Clear the AdDetails, since category has changed
	session.listing.AdDetails = nil
	msg := makeCategoryMessage(session.categories, newCategoryCode)
	msgReplyMarkup, _ := msg.ReplyMarkup.(tgbotapi.InlineKeyboardMarkup)
	editMsg := tgbotapi.NewEditMessageTextAndMarkup(
		update.CallbackQuery.From.ID,
		update.CallbackQuery.Message.MessageID,
		msg.Text,
		msgReplyMarkup,
	)
	editMsg.ParseMode = tgbotapi.ModeMarkdown
	_, err = b.tg.Send(editMsg)
	if err != nil {
		session.replyWithError(err)
		return
	}

	callback := tgbotapi.NewCallback(update.CallbackQuery.ID, update.CallbackQuery.Data)
	if _, err := b.tg.Request(callback); err != nil {
		session.replyWithError(err)
		return
	}

	// Prompt user for next field because category has changed and AdDetails has been cleared
	msg, missingFieldNow, err := makeNextFieldPrompt(ctx, session.client.GetFiltersSectionNewad, *session.listing)
	if err != nil {
		session.replyWithError(err)
		return
	}
	// Reduce a bit of noise by not sending the prompt message if it's the same
	// as previous one, before changing category
	if missingFieldBefore != missingFieldNow {
		session.replyWithMessage(msg)
	}
}

func (b *Bot) handleFreetextReply(ctx context.Context, update tgbotapi.Update) {
	var text string
	if update.Message.Caption != "" {
		text = update.Message.Caption
	} else {
		text = update.Message.Text
	}

	session, err := b.state.getUserSession(update.Message.From.ID)
	if err != nil {
		log.Error().Err(err).Send()
		return
	}

	// Message has a photo
	if len(update.Message.Photo) > 0 {
		b.handlePhoto(update.Message)
	}

	if text == "" {
		return
	}

	// Start a new listing from message
	if session.listing == nil {
		// Do the best we can to ensure listing can be eventually sent
		// successfully, instead of failing after user has input all details and
		// bot tries to POST the listing to Tori
		msgText := checkUserPreconditions(ctx, session)
		if msgText != "" {
			session.reply(msgText)
			return
		}

		listing := newListingFromMessage(text)
		session.userSubjectMessageId = update.Message.MessageID
		session.listing = &listing
		// Remove custom keyboard just in case there was one from previous
		// listing creation that did not finish
		sent := session.reply(listingSubjectIsText, session.listing.Subject)
		session.botSubjectMessageId = sent.MessageID
		categories, err := getCategoriesForSubject(ctx, session.client, session.listing.Subject)
		if err != nil {
			session.replyWithError(err)
			session.reset()
			return
		}
		log.Info().Str("subject", session.listing.Subject).Interface("categories", categories).Msg("found categories for subject")
		if len(categories) == 0 {
			// TODO: add fallback mechanism for selecting category
			session.reply(cantFigureOutCategoryText)
			session.reset()
			return
		}
		session.categories = categories
		session.listing.Category = categories[0].Code
		msg := makeCategoryMessage(categories, session.listing.Category)
		session.replyWithMessage(msg)
		log.Info().Interface("listing", session.listing).Msg("started a new listing")

		msg, _, err = makeNextFieldPrompt(ctx, session.client.GetFiltersSectionNewad, *session.listing)
		if err != nil {
			session.replyWithError(err)
			return
		}
		session.replyWithMessage(msg)
	} else {
		// Augment a previously started listing with user's message
		newadFilters, err := fetchNewadFilters(ctx, session.client.GetFiltersSectionNewad)
		if err != nil {
			session.replyWithError(err)
			return
		}
		settingsParams := newadFilters.Newad.SettingsParams
		paramMap := newadFilters.Newad.ParamMap

		// User is replying to bot's question, and we can determine what, by
		// getting the next missing field from Listing
		repliedField := tori.GetMissingListingField(paramMap, settingsParams, *session.listing)
		if repliedField == "" {
			log.Info().Msg("not expecting a reply")
			return
		}

		log.Info().Str("field", repliedField).Msg("user is replying to field")
		newListing, err := setListingFieldFromMessage(paramMap, *session.listing, repliedField, text)
		if err != nil {
			var noLabelFoundError *NoLabelFoundError
			label, _ := getLabelForField(paramMap, repliedField) // can't error in this case
			if errors.As(err, &noLabelFoundError) {
				session.reply(invalidReplyToField, label)
			} else {
				session.replyWithError(err)
			}

			return
		}
		session.listing = &newListing
		log.Info().Interface("listing", newListing).Msg("updated listing")

		if repliedField == "body" {
			session.userBodyMessageId = update.Message.MessageID
			sent := session.reply(listingBodyIsText, session.listing.Body)
			session.botBodyMessageId = sent.MessageID
		}

		msg, missingField, err := makeNextFieldPrompt(ctx, session.client.GetFiltersSectionNewad, *session.listing)
		if missingField != "" {
			if err != nil {
				session.replyWithError(err)
				return
			}
			session.replyWithMessage(msg)
		} else {
			var text string
			if len(session.photos) == 0 {
				text = listingReadyToBeSentNoImagesText
			} else {
				text = listingReadyToBeSentText
			}
			session.replyAndRemoveCustomKeyboard(
				fmt.Sprintf("%s\n%s", text, listingReadyCommands),
			)
		}
	}
}

func (b *Bot) sendListingCommand(ctx context.Context, update tgbotapi.Update) {
	userId := update.Message.From.ID
	session, err := b.state.getUserSession(userId)
	if err != nil {
		log.Error().Err(err).Send()
		return
	}

	if session.listing == nil {
		session.reply(noListingOnSendText)
		return
	}

	_, missingField, err := makeNextFieldPrompt(ctx, session.client.GetFiltersSectionNewad, *session.listing)
	if err != nil {
		log.Error().Stack().Err(err).Send()
		return
	}
	if missingField != "" {
		log.Info().Str("missingField", missingField).Msg("cannot send listing with missing field(s)")
		session.reply(incompleteListingOnSendText)
		return
	}

	// Add location to listing based on logged in user's location
	account, err := session.client.GetAccount(ctx, session.toriAccountId)
	if err != nil {
		session.replyWithError(err)
		return
	}
	listingLocation := tori.AccountLocationListToListingLocation(account.Locations)
	session.listing.Location = &listingLocation
	session.listing.AccountId = tori.ParseAccountIdNumberFromPath(account.AccountId)

	// Phone number hidden implicitly
	session.listing.PhoneHidden = true

	medias, err := uploadListingPhotos(ctx, b.tg.GetFileDirectURL, session.client.UploadMedia, session.photos)
	if err != nil {
		session.replyWithError(err)
		return
	}

	listingImages := make([]tori.ListingMedia, 0, len(medias))
	for _, m := range medias {
		listingImages = append(listingImages, tori.ListingMedia{
			Id: "/public/media/ad/" + m.Id,
		})
	}
	session.listing.Images = &listingImages

	err = session.client.PostListing(ctx, *session.listing)
	if err != nil {
		session.replyWithError(err)
		return
	}
	session.replyAndRemoveCustomKeyboard(listingSentText)

	// Create a JSON archive of listing and photos. The archive can be used
	// later to resend the same listing, perhaps with minor modifications.
	archive := NewListingArchive(*session.listing, session.photos)
	archiveBytes, err := json.Marshal(archive)
	if err != nil {
		session.replyWithError(err)
		return
	}
	document := tgbotapi.NewDocument(session.userId, tgbotapi.FileBytes{
		Name:  "archive.json",
		Bytes: archiveBytes,
	})
	document.Caption = session.listing.Subject

	_, err = b.tg.Send(document)
	if err != nil {
		session.replyWithError(err)
		return
	}

	log.Info().Interface("listing", session.listing).Msg("listing posted successfully")
	session.reset()
}

func (b *Bot) handleImportJson(update tgbotapi.Update) {
	userId := update.Message.From.ID
	session, err := b.state.getUserSession(userId)
	if err != nil {
		log.Error().Err(err).Send()
		return
	}

	replyToMessage := update.Message.ReplyToMessage
	if replyToMessage == nil || replyToMessage.Document == nil || replyToMessage.Document.MimeType != "application/json" {
		session.reply(importJsonInputError)
		return
	}

	archiveBytes, err := downloadFileID(b.tg.GetFileDirectURL, replyToMessage.Document.FileID)
	if err != nil {
		session.replyWithError(err)
		return
	}

	var archive ListingArchive
	err = json.Unmarshal(archiveBytes, &archive)

	if err != nil {
		session.replyWithError(err)
		return
	}

	session.photos = archive.Photos
	session.listing = &archive.Listing

	// When the listing is marshalled for the json archive, empty
	// delivery_options won't exist in the output json. This is because in the
	// json sent to tori, omitting delivery_options means that only pickup is
	// possible. When importing listing to session, we need to set
	// delivery_options to an empty array in the pickup case so that bot knows
	// the field has been asked.
	// See also "empty multi value in AdDetails is not marshaled" test.
	if session.listing.AdDetails["delivery_options"] == nil {
		session.listing.AdDetails["delivery_options"] = []string{}
	}

	session.reply(importJsonSuccessful, session.listing.Subject)
}

func (b *Bot) handleForget(ctx context.Context, update tgbotapi.Update, args []string) {
	userId := update.Message.From.ID
	session, err := b.state.getUserSession(userId)
	if err != nil {
		log.Error().Err(err).Send()
		return
	}
	switch args[0] {
	case "hinta":
		session.listing.Price = 0
	case "kunto":
		delete(session.listing.AdDetails, "general_condition")
	case "lisätiedot":
		session.listing.AdDetails = tori.AdDetails{}
	default:
		session.reply(forgetInvalidField)
		return
	}

	msg, _, err := makeNextFieldPrompt(ctx, session.client.GetFiltersSectionNewad, *session.listing)
	if err != nil {
		session.replyWithError(err)
		return
	}
	session.replyWithMessage(msg)
}

func (b *Bot) handleMessageEdit(update tgbotapi.Update) {
	userId := update.EditedMessage.From.ID
	session, err := b.state.getUserSession(userId)
	if err != nil {
		log.Error().Err(err).Send()
		return
	}

	if session.listing == nil {
		return
	}

	var text string
	if update.EditedMessage.Caption != "" {
		text = update.EditedMessage.Caption
	} else {
		text = update.EditedMessage.Text
	}

	var editMsg tgbotapi.EditMessageTextConfig
	switch update.EditedMessage.MessageID {
	// User edited subject message with the intent of changing the subject
	case session.userSubjectMessageId:
		listing := newListingFromMessage(text)
		log.Info().Str("oldSubject", session.listing.Subject).Str("newSubject", listing.Subject).Msg("listing subject updated")
		session.listing.Subject = listing.Subject

		editMsg = tgbotapi.NewEditMessageText(
			session.userId,
			session.botSubjectMessageId,
			fmt.Sprintf(listingSubjectIsText, session.listing.Subject),
		)
	// User edited body message with the intent of changing the subject
	case session.userBodyMessageId:
		log.Info().Str("oldBody", session.listing.Body).Str("newBody", text).Msg("listing body updated")
		session.listing.Body = strings.TrimSpace(text)

		editMsg = tgbotapi.NewEditMessageText(
			session.userId,
			session.botBodyMessageId,
			fmt.Sprintf(listingBodyIsText, session.listing.Body),
		)
	}

	if editMsg.ChatID != 0 {
		editMsg.ParseMode = tgbotapi.ModeMarkdown
		_, err = b.tg.Send(editMsg)
		log.Info().Interface("editMsg", editMsg).Msg("message edited")
		if err != nil {
			session.replyWithError(err)
			return
		}
	}
}

func (b *Bot) handleUpdate(ctx context.Context, update tgbotapi.Update) {
	// Update is user interacting with inline keyboard
	if update.CallbackQuery != nil {
		b.handleCallback(ctx, update)
		return
	}

	if update.EditedMessage != nil {
		b.handleMessageEdit(update)
		return
	}

	if update.Message == nil {
		return
	}

	userId := update.Message.From.ID
	session, err := b.state.getUserSession(userId)
	if err != nil {
		log.Error().Err(err).Send()
		return
	}

	session.mu.Lock()
	defer session.mu.Unlock()

	log.Info().Str("text", update.Message.Text).Str("caption", update.Message.Caption).Msg("got message")
	command, args := parseCommand(update.Message.Text)
	switch command {
	// /start is the command telegram client prompts user to send to a
	// bot when there are no prior messages
	case "/start":
		session.reply(startText)
	case "/peru":
		session.reset()
		session.replyAndRemoveCustomKeyboard(okText)
	case "/laheta":
		b.sendListingCommand(ctx, update)
	case "/poistakuvat":
		session.photos = nil
		session.pendingPhotos = nil
		session.reply(photosRemoved)
	case "/tuojson":
		b.handleImportJson(update)
	case "/unohda":
		b.handleForget(ctx, update, args)
	default:
		b.handleFreetextReply(ctx, update)
	}
}

func makeNextFieldPrompt(
	ctx context.Context,
	getNewadFilters func(context.Context) (tori.NewadFilters, error),
	listing tori.Listing,
) (
	tgbotapi.MessageConfig,
	string,
	error,
) {
	newadFilters, err := fetchNewadFilters(ctx, getNewadFilters)
	if err != nil {
		return tgbotapi.MessageConfig{}, "", err
	}
	missingField := tori.GetMissingListingField(
		newadFilters.Newad.ParamMap,
		newadFilters.Newad.SettingsParams,
		listing,
	)
	if missingField == "" {
		return tgbotapi.MessageConfig{}, "", nil
	}
	msg, err := makeMissingFieldPromptMessage(newadFilters.Newad.ParamMap, missingField)
	if err != nil {
		return msg, missingField, err
	}
	return msg, missingField, nil
}

func fetchNewadFilters(ctx context.Context, get func(context.Context) (tori.NewadFilters, error)) (tori.NewadFilters, error) {
	cachedNewadFilters, ok := getCachedNewadFilters()
	if !ok {
		newadFilters, err := get(ctx)
		if err != nil {
			return newadFilters, err
		}
		setCachedNewadFilters(newadFilters)
		return newadFilters, nil
	} else {
		return cachedNewadFilters, nil
	}
}

func checkUserPreconditions(ctx context.Context, session *UserSession) string {
	// Check that access token is valid
	account, err := session.client.GetAccount(ctx, session.toriAccountId)
	if err != nil {
		log.Error().Err(err).Msg("precondition check failed: could not get account from tori")
		return sessionMaybeExpiredText
	}

	// Tori account needs to have location set so that it can be added to listing
	if len(account.Locations) == 0 {
		log.Error().Msg("precondition check failed: account does not have locations set")
		return noLocationsInToriAccountText
	}

	return "" // OK
}
