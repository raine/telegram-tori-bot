package main

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"github.com/lithammer/dedent"
	"github.com/pkg/errors"
	"github.com/raine/telegram-tori-bot/tori"
)

const (
	listingSubjectIsText     = "*Ilmoituksen otsikko:* %s"
	listingBodyIsText        = "*Ilmoituksen kuvaus:*\n%s"
	listingReadyToBeSentText = `
    Ilmoitus on valmis lähetettäväksi.

    /laheta - Lähetä ilmoitus
    /peru - Peru ilmoituksen teko`
	cantFigureOutCategoryText   = "En keksinyt osastoa otsikon perusteella, eli pieleen meni."
	incompleteListingOnSendText = "Ilmoituksesta puuttuu kenttiä."
	noListingOnSendText         = "Ei ole ilmoitusta mitä lähettää."
	listingSentText             = "Ilmoitus lähetetty!"
	photosRemoved               = "Kuvat poistettu."
	invalidReplyToField         = `Vastauksesi ei sovi kenttään "%s". Valitse vastaus nappuloista viestikentän alapuolelta.`
	unexpectedErrorText         = `Odottamaton virhe: %s`
	okText                      = `Ok!`
	startText                   = "Aloita ilmoituksen teko kirjoittamalla tavaran otsikko"
)

func makeCategoriesInlineKeyboard(categories []tori.Category) tgbotapi.InlineKeyboardMarkup {
	buttonsPerRow := 2

	var rows [][]tgbotapi.InlineKeyboardButton
	for i := 0; i < len(categories); i += buttonsPerRow {
		end := i + buttonsPerRow
		if end > len(categories) {
			end = len(categories)
		}

		var row []tgbotapi.InlineKeyboardButton
		for _, value := range categories[i:end] {
			label := value.Label
			row = append(row, tgbotapi.InlineKeyboardButton(tgbotapi.InlineKeyboardButton{
				Text:         value.Label,
				CallbackData: &label,
			}))
		}

		rows = append(rows, row)
	}

	return tgbotapi.NewInlineKeyboardMarkup(rows...)
}

// makeCategoryMessage creates a telegram message with current category as
// Text, and the other available categories as inline keyboard
func makeCategoryMessage(categories []tori.Category, categoryCode string) tgbotapi.MessageConfig {
	var currentCategoryLabel string
	for _, c := range categories {
		if c.Code == categoryCode {
			currentCategoryLabel = c.Label
		}
	}

	var inlineKeyboardCategories []tori.Category
	for _, c := range categories {
		if c.Code != categoryCode {
			inlineKeyboardCategories = append(inlineKeyboardCategories, c)
		}
	}

	msg := tgbotapi.NewMessage(0, fmt.Sprintf("*Osasto:* %s\n", currentCategoryLabel))
	msg.ParseMode = tgbotapi.ModeMarkdown

	if len(categories) > 1 {
		msg.ReplyMarkup = makeCategoriesInlineKeyboard(inlineKeyboardCategories)
	}

	return msg
}

func valuesListToReplyKeyboard(valuesList []tori.Value) tgbotapi.ReplyKeyboardMarkup {
	buttonsPerRow := 3

	var rows [][]tgbotapi.KeyboardButton
	for i := 0; i < len(valuesList); i += buttonsPerRow {
		end := i + buttonsPerRow
		if end > len(valuesList) {
			end = len(valuesList)
		}

		var row []tgbotapi.KeyboardButton
		for _, value := range valuesList[i:end] {
			row = append(row, tgbotapi.NewKeyboardButton(value.Label))
		}

		rows = append(rows, row)
	}

	return tgbotapi.NewOneTimeReplyKeyboard(rows...)
}

func makeMissingFieldPromptMessage(
	paramMap tori.ParamMap,
	missingField string,
) (tgbotapi.MessageConfig, error) {
	msg := tgbotapi.NewMessage(0, "")
	msg.ParseMode = tgbotapi.ModeMarkdown

	// body is not in tori's param_map
	if missingField == "body" {
		msg.Text = "Ilmoitusteksti?"
		return msg, nil
	}

	param := paramMap[missingField]
	switch {
	case param.SingleSelection != nil:
		msg.ReplyMarkup = valuesListToReplyKeyboard(param.SingleSelection.ValuesList)
		msg.Text = fmt.Sprintf("%s?", (*param.SingleSelection).Label)
	case param.MultiSelection != nil:
		// delivery_options param is multi selection with single value. For a
		// bot, it makes more sense as a single selection with yes/no answers,
		// but in tori UI it is a checkbox multi selection.
		if missingField == "delivery_options" {
			msg.Text = param.MultiSelection.ValuesList[0].Label
			msg.ReplyMarkup = valuesListToReplyKeyboard([]tori.Value{
				{Label: "Kyllä", Value: "yes"},
				{Label: "En", Value: "no"},
			})
			return msg, nil
		}
		return msg, errors.Errorf("multi selection param %s not implemented", missingField)
	case param.Text != nil:
		msg.Text = fmt.Sprintf("%s?", (*param.Text).Label)
	default:
		return msg, errors.Errorf("could not find param for missing field '%s'", missingField)
	}
	return msg, nil
}

func parsePriceMessage(message string) (tori.Price, error) {
	var price tori.Price
	re := regexp.MustCompile(`(\d+)€?`)
	m := re.FindStringSubmatch(message)
	if m == nil {
		return price, errors.Errorf("failed to parse price from message")
	} else {
		n, err := strconv.Atoi(m[1])
		if err != nil {
			return price, err
		}
		return tori.Price(n), err
	}
}

func formatReplyText(text string, a ...any) string {
	return fmt.Sprintf(strings.TrimSpace(dedent.Dedent(text)), a...)
}
