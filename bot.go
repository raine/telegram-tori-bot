package main

import (
	"errors"
	"fmt"
	"os"
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
	// after the first photo is sent and keep track of photos since then
	if session.pendingPhotos == nil {
		session.pendingPhotos = new([]tgbotapi.PhotoSize)

		go func() {
			env, _ := os.LookupEnv("GO_ENV")
			if env == "test" {
				time.Sleep(100 * time.Microsecond)
			} else {
				time.Sleep(1 * time.Second)
			}
			session.photos = append(session.photos, *session.pendingPhotos...)
			session.reply("%s lisätty", pluralize("kuva", "kuvaa", len(*session.pendingPhotos)))
			session.pendingPhotos = nil
		}()
	}

	// message.Photo is an array of PhotoSizes and the last one is the largest size
	largestPhoto := message.Photo[len(message.Photo)-1]
	log.Info().Interface("photo", largestPhoto).Msg("added photo to listing")
	pendingPhotos := append(*session.pendingPhotos, largestPhoto)
	session.pendingPhotos = &pendingPhotos
}

// handleCallback is called when a tgbotapi.update with CallbackQuery is
// received. That happens when user interacts with an inline keyboard with
// callback data.
func (b *Bot) handleCallback(update tgbotapi.Update) {
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

	newadFilters, err := fetchNewadFilters(session.client.GetFiltersSectionNewad)
	if err != nil {
		session.replyWithError(err)
		return
	}
	missingFieldBefore := getMissingListingField(newadFilters.Newad.ParamMap, newadFilters.Newad.SettingsParams, *session.listing)

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
	msg, missingFieldNow, err := makeNextFieldPrompt(session.client.GetFiltersSectionNewad, *session.listing)
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

func (b *Bot) handleFreetextReply(update tgbotapi.Update) {
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
		if err != nil {
			session.replyWithError(err)
			return
		}
		listing := newListingFromMessage(text)
		session.userSubjectMessageId = update.Message.MessageID
		session.listing = &listing
		// Remove custom keyboard just in case there was one from previous
		// listing creation that did not finish
		sent := session.replyAndRemoveCustomKeyboard(listingSubjectIsText, session.listing.Subject)
		session.botSubjectMessageId = sent.MessageID
		if session.listing.Body != "" {
			sent = session.reply(listingBodyIsText, session.listing.Body)
			session.botBodyMessageId = sent.MessageID
		}
		categories, err := getCategoriesForSubject(session.client, session.listing.Subject)
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

		msg, _, err = makeNextFieldPrompt(session.client.GetFiltersSectionNewad, *session.listing)
		if err != nil {
			session.replyWithError(err)
			return
		}
		session.replyWithMessage(msg)
	} else {
		// Augment a previously started listing with user's message
		newadFilters, err := fetchNewadFilters(session.client.GetFiltersSectionNewad)
		if err != nil {
			session.replyWithError(err)
			return
		}
		settingsParams := newadFilters.Newad.SettingsParams
		paramMap := newadFilters.Newad.ParamMap

		// User is replying to bot's question, and we can determine what, by
		// getting the next missing field from Listing
		repliedField := getMissingListingField(paramMap, settingsParams, *session.listing)
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

		msg, missingField, err := makeNextFieldPrompt(session.client.GetFiltersSectionNewad, *session.listing)
		if missingField != "" {
			if err != nil {
				session.replyWithError(err)
				return
			}
			session.replyWithMessage(msg)
		} else {
			session.reply(listingReadyToBeSentText)
		}
	}
}

func (b *Bot) sendListingCommand(update tgbotapi.Update) {
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

	_, missingField, err := makeNextFieldPrompt(session.client.GetFiltersSectionNewad, *session.listing)
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
	account, err := session.client.GetAccount(session.toriAccountId)
	if err != nil {
		session.replyWithError(err)
		return
	}
	listingLocation := tori.AccountLocationListToListingLocation(account.Locations)
	session.listing.Location = &listingLocation
	session.listing.AccountId = tori.ParseAccountIdNumberFromPath(account.AccountId)

	// Phone number hidden implicitly
	session.listing.PhoneHidden = true

	medias, err := uploadListingPhotos(b.tg.GetFileDirectURL, session.client.UploadMedia, session.photos)
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

	err = session.client.PostListing(*session.listing)
	if err != nil {
		session.replyWithError(err)
		return
	}

	log.Info().Interface("listing", session.listing).Msg("listing posted successfully")
	session.replyAndRemoveCustomKeyboard(listingSentText)
	session.reset()
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

func (b *Bot) handleUpdate(update tgbotapi.Update) {
	// Update is user interacting with inline keyboard
	if update.CallbackQuery != nil {
		b.handleCallback(update)
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
	switch text := update.Message.Text; text {
	// /start is the command telegram client prompts user to send to a
	// bot when there are no prior messages
	case "/start":
		session.reply(startText)
	case "/peru":
		session.reset()
		session.replyAndRemoveCustomKeyboard(okText)
	case "/laheta":
		b.sendListingCommand(update)
	case "/poistakuvat":
		session.photos = nil
		session.pendingPhotos = nil
		session.reply(photosRemoved)
	default:
		b.handleFreetextReply(update)
	}
}

func makeNextFieldPrompt(
	getNewadFilters func() (tori.NewadFilters, error),
	listing tori.Listing,
) (
	tgbotapi.MessageConfig,
	string,
	error,
) {
	newadFilters, err := fetchNewadFilters(getNewadFilters)
	if err != nil {
		return tgbotapi.MessageConfig{}, "", err
	}
	missingField := getMissingListingField(
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

func fetchNewadFilters(get func() (tori.NewadFilters, error)) (tori.NewadFilters, error) {
	cachedNewadFilters, ok := getCachedNewadFilters()
	if !ok {
		newadFilters, err := get()
		if err != nil {
			return newadFilters, err
		}
		setCachedNewadFilters(newadFilters)
		return newadFilters, nil
	} else {
		return cachedNewadFilters, nil
	}
}
