package main

import (
	"os"
	"testing"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"github.com/raine/telegram-tori-bot/tori"
	"github.com/stretchr/testify/assert"
)

func TestMakeMissingFieldPromptMessage(t *testing.T) {
	filtersSectionNewadJson, err := os.ReadFile("tori/testdata/v1_2_public_filters_section_newad.json")
	if err != nil {
		t.Fatal(err)
	}
	newadFilters, err := tori.ParseNewadFilters(filtersSectionNewadJson)
	if err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		missingField string
		want         tgbotapi.MessageConfig
	}{
		{
			"body",
			tgbotapi.MessageConfig{
				Text:      "Ilmoitusteksti?",
				ParseMode: tgbotapi.ModeMarkdown,
			},
		},
		{
			"price",
			tgbotapi.MessageConfig{
				Text:      "Hinta?",
				ParseMode: tgbotapi.ModeMarkdown,
			},
		},
		{
			"general_condition",
			tgbotapi.MessageConfig{
				Text:      "Kunto?",
				ParseMode: tgbotapi.ModeMarkdown,
				BaseChat: tgbotapi.BaseChat{
					ReplyMarkup: tgbotapi.ReplyKeyboardMarkup{
						Keyboard: [][]tgbotapi.KeyboardButton{
							{
								tgbotapi.KeyboardButton{Text: "Uusi"},
								tgbotapi.KeyboardButton{Text: "Erinomainen"},
								tgbotapi.KeyboardButton{Text: "Hyvä"},
							},
							{
								tgbotapi.KeyboardButton{Text: "Tyydyttävä"},
								tgbotapi.KeyboardButton{Text: "Huono"},
							},
						},
						ResizeKeyboard:  true,
						OneTimeKeyboard: true,
					},
				},
			},
		},
		{
			"delivery_options",
			tgbotapi.MessageConfig{
				Text:      "Voin lähettää tuotteen",
				ParseMode: tgbotapi.ModeMarkdown,
				BaseChat: tgbotapi.BaseChat{
					ReplyMarkup: tgbotapi.ReplyKeyboardMarkup{
						Keyboard: [][]tgbotapi.KeyboardButton{
							{
								tgbotapi.KeyboardButton{
									Text: "Kyllä",
								},
								tgbotapi.KeyboardButton{
									Text: "En",
								},
							},
						},
						ResizeKeyboard:  true,
						OneTimeKeyboard: true,
					},
				},
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.missingField, func(t *testing.T) {
			msg, err := makeMissingFieldPromptMessage(newadFilters.Newad.ParamMap, tc.missingField)
			if err != nil {
				t.Fatal(err)
			}
			assert.Equal(t, tc.want, msg)
		})
	}
}
