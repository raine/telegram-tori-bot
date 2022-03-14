package main

import (
	"io/ioutil"
	"net/http"
	"net/http/httptest"
	"testing"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"github.com/raine/go-telegram-bot/tori"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

type botApiMock struct {
	mock.Mock
}

func (m *botApiMock) Send(c tgbotapi.Chattable) (tgbotapi.Message, error) {
	args := m.Called(c)
	return args.Get(0).(tgbotapi.Message), args.Error(1)
}

func (m *botApiMock) Request(c tgbotapi.Chattable) (*tgbotapi.APIResponse, error) {
	args := m.Called(c)
	return args.Get(0).(*tgbotapi.APIResponse), args.Error(1)
}

func makeMessage(userId int64, text string) tgbotapi.MessageConfig {
	msg := tgbotapi.NewMessage(userId, text)
	msg.ParseMode = tgbotapi.ModeMarkdown
	return msg
}

func makeMessageWithFn(userId int64, text string, fn func(msg *tgbotapi.MessageConfig)) tgbotapi.MessageConfig {
	msg := makeMessage(userId, text)
	fn(&msg)
	return msg
}

func makeMessageWithReplyMarkup(userId int64, text string, replyMarkup tgbotapi.ReplyKeyboardMarkup) tgbotapi.MessageConfig {
	msg := makeMessage(userId, text)
	msg.ReplyMarkup = replyMarkup
	return msg
}

func makeTestServer(t *testing.T) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var b []byte
		var err error
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/v2/listings/search":
			b = makeListingsSearchResponse(t)
		case "/v2/listings/1":
			w.Write(makeListingResponse(t, "1", tori.Category{Code: "5012", Label: "Puhelimet"}))
		case "/v2/listings/2":
			w.Write(makeListingResponse(t, "2", tori.Category{Code: "5022", Label: "Televisiot"}))
		case "/v2/listings/4":
			w.Write(makeListingResponse(t, "4", tori.Category{Code: "5031", Label: "Tabletit"}))
		case "/v1.2/public/filters":
			b, err = ioutil.ReadFile("tori/testdata/v1_2_public_filters_section_newad.json")
		default:
			t.Fatal("invalid path " + r.URL.Path)
		}
		if err != nil {
			t.Fatal(err)
		}
		w.Write(b)
	}))
}

func strPtr(v string) *string {
	return &v
}

func TestHandleUpdate_ListingStart(t *testing.T) {
	ts := makeTestServer(t)
	defer ts.Close()
	userId := int64(1)
	tg := new(botApiMock)
	bot := NewBot(tg, ts.URL)
	update := tgbotapi.Update{
		Message: &tgbotapi.Message{
			From: &tgbotapi.User{
				ID: userId,
			},
			Text: "iPhone 12\n\nMyydään käytetty iPhone 12",
		},
	}

	tg.On("Send", makeMessage(userId, "*Ilmoituksen otsikko:* iPhone 12\n")).Return(tgbotapi.Message{}, nil).Once()
	tg.On("Send", makeMessage(userId, "*Ilmoituksen kuvaus:*\nMyydään käytetty iPhone 12")).Return(tgbotapi.Message{}, nil).Once()
	tg.On("Send", makeMessageWithFn(userId, "*Osasto:* Puhelimet\n", func(msg *tgbotapi.MessageConfig) {
		msg.ReplyMarkup = tgbotapi.InlineKeyboardMarkup{
			InlineKeyboard: [][]tgbotapi.InlineKeyboardButton{
				{
					{Text: "Televisiot", CallbackData: strPtr("Televisiot")},
					{Text: "Tabletit", CallbackData: strPtr("Tabletit")},
				},
			},
		}
	})).
		Return(tgbotapi.Message{}, nil).Once()
	// Bot prompts for first missing field in listing
	tg.On("Send", makeMessage(userId, "Hinta?\n")).
		Return(tgbotapi.Message{}, nil).Once()

	bot.HandleUpdate(update)
	tg.AssertExpectations(t)
	session := bot.state.getUserSession(userId)
	// Skip these fields as they are difficult and not very fruitful to assert
	session.client = nil
	session.bot = nil

	assert.Equal(t, &UserSession{
		userId: 1,
		listing: &tori.Listing{
			Subject:  "iPhone 12",
			Body:     "Myydään käytetty iPhone 12",
			Type:     tori.ListingTypeSell,
			Category: "5012",
		},
		pendingPhotos: nil,
		photos:        nil,
		categories: []tori.Category{
			{Code: "5012", Label: "Puhelimet"},
			{Code: "5022", Label: "Televisiot"},
			{Code: "5031", Label: "Tabletit"},
		},
	}, session)
}

func TestHandleUpdate_EnterPrice(t *testing.T) {
	ts := makeTestServer(t)
	defer ts.Close()

	userId := int64(1)
	tg := new(botApiMock)
	bot := NewBot(tg, ts.URL)
	update := tgbotapi.Update{
		Message: &tgbotapi.Message{
			From: &tgbotapi.User{
				ID: userId,
			},
			Text: "50€",
		},
	}

	session := bot.state.getUserSession(userId)
	session.listing = &tori.Listing{
		Subject:  "iPhone 12",
		Category: "5012",
		Type:     tori.ListingTypeSell,
	}

	// Bot asks the next field
	tg.On("Send", makeMessageWithReplyMarkup(userId, "Kunto?\n",
		tgbotapi.ReplyKeyboardMarkup{
			Keyboard: [][]tgbotapi.KeyboardButton{{
				tgbotapi.KeyboardButton{Text: "Uusi"},
				tgbotapi.KeyboardButton{Text: "Erinomainen"},
				tgbotapi.KeyboardButton{Text: "Hyvä"},
			}, {
				tgbotapi.KeyboardButton{Text: "Tyydyttävä"},
				tgbotapi.KeyboardButton{Text: "Huono"},
			}},
			ResizeKeyboard:  true,
			OneTimeKeyboard: true,
		},
	)).
		Return(tgbotapi.Message{}, nil).Once()

	bot.HandleUpdate(update)
	tg.AssertExpectations(t)

	listing := bot.state.getUserSession(userId).listing
	assert.Equal(t, &tori.Listing{
		Subject:  "iPhone 12",
		Category: "5012",
		Type:     tori.ListingTypeSell,
		Price:    50,
	}, listing)
}

func TestHandleUpdate_EnterCondition(t *testing.T) {
	ts := makeTestServer(t)
	defer ts.Close()

	userId := int64(1)
	tg := new(botApiMock)
	bot := NewBot(tg, ts.URL)
	update := tgbotapi.Update{
		Message: &tgbotapi.Message{
			From: &tgbotapi.User{
				ID: userId,
			},
			Text: "Uusi",
		},
	}

	session := bot.state.getUserSession(userId)
	session.listing = &tori.Listing{
		Subject:  "iPhone 12",
		Category: "5012",
		Type:     tori.ListingTypeSell,
		Price:    50,
	}

	// Bot asks the next field
	tg.On("Send", makeMessageWithReplyMarkup(userId, "Laitevalmistaja?\n",
		tgbotapi.ReplyKeyboardMarkup{
			Keyboard: [][]tgbotapi.KeyboardButton{
				{tgbotapi.KeyboardButton{Text: "Apple"}, tgbotapi.KeyboardButton{Text: "Doro"}, tgbotapi.KeyboardButton{Text: "HTC"}},
				{tgbotapi.KeyboardButton{Text: "Huawei"}, tgbotapi.KeyboardButton{Text: "LG"}, tgbotapi.KeyboardButton{Text: "Motorola"}},
				{tgbotapi.KeyboardButton{Text: "Nokia"}, tgbotapi.KeyboardButton{Text: "Samsung"}, tgbotapi.KeyboardButton{Text: "Sony"}},
				{tgbotapi.KeyboardButton{Text: "OnePlus"}, tgbotapi.KeyboardButton{Text: "Xiaomi"}, tgbotapi.KeyboardButton{Text: "Muut"}},
			},
			ResizeKeyboard:  true,
			OneTimeKeyboard: true,
		})).
		Return(tgbotapi.Message{}, nil).Once()

	bot.HandleUpdate(update)

	listing := bot.state.getUserSession(userId).listing
	assert.Equal(t, &tori.Listing{
		Subject:  "iPhone 12",
		Category: "5012",
		Type:     tori.ListingTypeSell,
		Price:    50,
		AdDetails: tori.AdDetails{
			"general_condition": "new",
		},
	}, listing)
}

func TestHandleUpdate_EnterManufacturer(t *testing.T) {
	ts := makeTestServer(t)
	defer ts.Close()

	userId := int64(1)
	tg := new(botApiMock)
	bot := NewBot(tg, ts.URL)
	update := tgbotapi.Update{
		Message: &tgbotapi.Message{
			From: &tgbotapi.User{
				ID: userId,
			},
			Text: "Apple",
		},
	}

	session := bot.state.getUserSession(userId)
	session.listing = &tori.Listing{
		Subject:  "iPhone 12",
		Category: "5012",
		Type:     tori.ListingTypeSell,
		Price:    50,
		AdDetails: tori.AdDetails{
			"general_condition": "new",
		},
	}

	// Bot asks the next field
	tg.On("Send", makeMessageWithReplyMarkup(userId, "Voin lähettää tuotteen\n",
		tgbotapi.ReplyKeyboardMarkup{
			Keyboard: [][]tgbotapi.KeyboardButton{
				{
					tgbotapi.KeyboardButton{Text: "Kyllä"},
					tgbotapi.KeyboardButton{Text: "En"},
				},
			},
			ResizeKeyboard:  true,
			OneTimeKeyboard: true,
		})).
		Return(tgbotapi.Message{}, nil).Once()

	bot.HandleUpdate(update)
	tg.AssertExpectations(t)

	listing := bot.state.getUserSession(userId).listing
	assert.Equal(t, &tori.Listing{
		Subject:  "iPhone 12",
		Category: "5012",
		Type:     tori.ListingTypeSell,
		Price:    50,
		AdDetails: tori.AdDetails{
			"general_condition": "new",
			"cell_phone":        "apple",
		},
	}, listing)
}

func TestHandleUpdate_EnterDeliveryOptions(t *testing.T) {
	ts := makeTestServer(t)
	defer ts.Close()

	userId := int64(1)
	tg := new(botApiMock)
	bot := NewBot(tg, ts.URL)
	update := tgbotapi.Update{
		Message: &tgbotapi.Message{
			From: &tgbotapi.User{
				ID: userId,
			},
			Text: "En",
		},
	}

	session := bot.state.getUserSession(userId)
	session.listing = &tori.Listing{
		Subject:  "iPhone 12",
		Category: "5012",
		Type:     tori.ListingTypeSell,
		Price:    50,
		AdDetails: tori.AdDetails{
			"general_condition": "new",
			"cell_phone":        "apple",
		},
	}

	bot.HandleUpdate(update)
	tg.AssertExpectations(t)

	listing := bot.state.getUserSession(userId).listing
	assert.Equal(t, &tori.Listing{
		Subject:  "iPhone 12",
		Category: "5012",
		Type:     tori.ListingTypeSell,
		Price:    50,
		AdDetails: tori.AdDetails{
			"general_condition": "new",
			"cell_phone":        "apple",
			"delivery_options":  []string{},
		},
	}, listing)
}
