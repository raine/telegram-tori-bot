package main

import (
	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"github.com/raine/go-telegram-bot/tori"
	"github.com/rs/zerolog/log"
)

type BotAPI interface {
	Send(c tgbotapi.Chattable) (tgbotapi.Message, error)
	Request(c tgbotapi.Chattable) (*tgbotapi.APIResponse, error)
}

type Bot struct {
	tg             BotAPI
	state          BotState
	toriApiBaseUrl string
	authMap        map[int64]string
}

func NewBot(tg BotAPI, authMap map[int64]string, toriApiBaseUrl string) *Bot {
	bot := &Bot{
		tg:             tg,
		authMap:        authMap,
		toriApiBaseUrl: toriApiBaseUrl,
	}

	bot.state = bot.NewBotState()
	return bot
}

func (b *Bot) HandleCallback(update tgbotapi.Update) {
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
		log.Error().Err(err).Send()
	}

	callback := tgbotapi.NewCallback(update.CallbackQuery.ID, update.CallbackQuery.Data)
	if _, err := b.tg.Request(callback); err != nil {
		log.Error().Err(err).Send()
	}

	// Prompt user for next field because category has changed and AdDetails has been cleared
	msg, _, err = makeNextFieldPrompt(session.client.GetFiltersSectionNewad, *session.listing)
	if err != nil {
		session.replyWithError(err)
		return
	}
	session.replyWithMessage(msg)
}

func (b *Bot) HandleUpdate(update tgbotapi.Update) {
	// Update is user interacting with inline keyboard
	if update.CallbackQuery != nil {
		b.HandleCallback(update)
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
	log.Info().Str("text", update.Message.Text).Msg("got message")

	switch text := update.Message.Text; text {
	case "/abort":
		session.reset()
		session.reply("Ok!")
	default:
		// Message has a photo
		if len(update.Message.Photo) > 0 {
			session.handlePhoto(update.Message)
		}

		if text == "" {
			return
		}

		// Start a new listing from message
		if session.listing == nil {
			session.listing = newListingFromMessage(text)
			session.reply("*Ilmoituksen otsikko:* %s\n", session.listing.Subject)
			if session.listing.Body != "" {
				session.reply("*Ilmoituksen kuvaus:*\n%s", session.listing.Body)
			}
			categories, err := getDistinctCategoriesFromSearchQuery(session.client, session.listing.Subject)
			if err != nil {
				session.replyWithError(err)
				session.reset()
				return
			}
			if len(categories) == 0 {
				// TODO: add fallback mechanism for selecting category
				session.reply("En keksinyt osastoa otsikon perusteella, eli pieleen meni")
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
				session.replyWithError(err)
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
			}
		}
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
