package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"github.com/lithammer/dedent"
	"github.com/raine/telegram-tori-bot/tori"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

func setupWithTestServer(t *testing.T, ts *httptest.Server) (*httptest.Server, int64, *botApiMock, *Bot, *UserSession) {
	userId := int64(1)
	tg := new(botApiMock)
	bot := NewBot(tg, userConfigMap, ts.URL)
	session, err := bot.state.getUserSession(userId)
	if err != nil {
		t.Fatal(err)
	}

	return ts, userId, tg, bot, session
}

func setup(t *testing.T) (*httptest.Server, int64, *botApiMock, *Bot, *UserSession) {
	ts := makeTestServer(t)
	return setupWithTestServer(t, ts)
}

func formatJson(b []byte) string {
	var out bytes.Buffer
	err := json.Indent(&out, b, "", "  ")
	if err != nil {
		panic(err)
	}
	return out.String()
}

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

func (m *botApiMock) GetFileDirectURL(fileID string) (string, error) {
	args := m.Called(fileID)
	return args.Get(0).(string), args.Error(1)
}

func makeUpdateWithMessageText(userId int64, text string) tgbotapi.Update {
	return tgbotapi.Update{
		Message: &tgbotapi.Message{
			From: &tgbotapi.User{
				ID: userId,
			},
			Text: text,
		},
	}
}

func makeMessage(userId int64, text string) tgbotapi.MessageConfig {
	msg := tgbotapi.NewMessage(userId, text)
	msg.ParseMode = tgbotapi.ModeMarkdown
	return msg
}

func makeMessageWithRemoveReplyKeyboard(userId int64, text string) tgbotapi.MessageConfig {
	return makeMessageWithReplyMarkup(
		userId,
		text,
		tgbotapi.ReplyKeyboardRemove{RemoveKeyboard: true, Selective: false},
	)
}

func makeMessageWithFn(userId int64, text string, fn func(msg *tgbotapi.MessageConfig)) tgbotapi.MessageConfig {
	msg := makeMessage(userId, text)
	fn(&msg)
	return msg
}

func makeMessageWithReplyMarkup(userId int64, text string, replyMarkup interface{}) tgbotapi.MessageConfig {
	msg := makeMessage(userId, text)
	msg.ReplyMarkup = replyMarkup
	return msg
}

func makeTestServerWithOnReqFn(t *testing.T, onReq func(r *http.Request)) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		onReq(r)
		var b []byte
		var err error
		w.Header().Set("Content-Type", "application/json")
		methodAndPath := fmt.Sprintf("%s %s", r.Method, r.URL.Path)
		switch methodAndPath {
		case "GET /v2/listings/search":
			w.Write(makeListingsSearchResponse(t, []tori.ListAdItem{
				makeListAdItem("1", "Electronics > Phones and accessories > Phones"),
				makeListAdItem("2", "Electronics > Tv audio video cameras > Television"),
				makeListAdItem("3", "Electronics > Phones and accessories > Tablets"),
				makeListAdItem("4", "Electronics > Phones and accessories > Tablets"),
			},
			))
		case "GET /v2/listings/1":
			w.Write(makeListingResponse(t, "1", tori.Category{Code: "5012", Label: "Puhelimet"}))
		case "GET /v2/listings/2":
			w.Write(makeListingResponse(t, "2", tori.Category{Code: "5022", Label: "Televisiot"}))
		case "GET /v2/listings/4":
			w.Write(makeListingResponse(t, "4", tori.Category{Code: "5031", Label: "Tabletit"}))
		case "GET /v1.2/public/filters":
			b, err = ioutil.ReadFile("tori/testdata/v1_2_public_filters_section_newad.json")
			w.Write(b)
		case "GET /v1.2/private/accounts/123123":
			b, err = ioutil.ReadFile("tori/testdata/v1_2_private_accounts_123123.json")
			w.Write(b)
		case "POST /v2/listings":
			w.Write([]byte("{}"))
		case "POST /v2.2/media":
			media := tori.Media{Id: "a", Url: ""}
			response := tori.UploadMediaResponse{Media: media}
			responseJson, err := json.Marshal(response)
			if err != nil {
				t.Fatal(err)
			}
			w.Header().Set("Content-Type", "plain/text") // Yes, really
			w.Write(responseJson)
		case "GET /1.jpg", "GET /2.jpg":
			w.Write([]byte("123"))
		// For testing JSON archive import
		case "GET /archive.json":
			b, err := ioutil.ReadFile("testdata/archive.json")
			if err != nil {
				t.Fatal(err)
			}
			w.Write(b)
		default:
			t.Fatal(fmt.Sprintf("invalid request to test server: %s %s", r.Method, r.URL.Path))
		}
		if err != nil {
			t.Fatal(err)
		}
	}))
}

func makeTestServer(t *testing.T) *httptest.Server {
	return makeTestServerWithOnReqFn(t, func(r *http.Request) {})
}

func strPtr(v string) *string {
	return &v
}

var userConfigMap = UserConfigMap{
	1: UserConfigItem{
		Token:         "foo",
		ToriAccountId: "123123",
	},
}

func TestMain(m *testing.M) {
	os.Setenv("GO_ENV", "test")
	os.Exit(m.Run())
}

func TestHandleUpdate_ListingStart(t *testing.T) {
	ts, userId, tg, bot, session := setup(t)
	defer ts.Close()
	update := makeUpdateWithMessageText(userId, "iPhone 12")

	tg.On("Send", makeMessage(userId, "*Ilmoituksen otsikko:* iPhone 12")).Return(tgbotapi.Message{}, nil).Once()
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
	tg.On("Send", makeMessage(userId, "Ilmoitusteksti?")).
		Return(tgbotapi.Message{}, nil).Once()

	bot.handleUpdate(update)
	tg.AssertExpectations(t)

	// Skip these fields as they are difficult and not very fruitful to assert
	session.client = nil
	session.bot = nil

	assert.Equal(t, &UserSession{
		userId: 1,
		listing: &tori.Listing{
			Subject:  "iPhone 12",
			Type:     tori.ListingTypeSell,
			Category: "5012",
		},
		toriAccountId: "123123",
		pendingPhotos: nil,
		photos:        nil,
		categories: []tori.Category{
			{Code: "5012", Label: "Puhelimet"},
			{Code: "5022", Label: "Televisiot"},
			{Code: "5031", Label: "Tabletit"},
		},
	}, session)
}

func TestHandleUpdate_EnterBody(t *testing.T) {
	ts, userId, tg, bot, session := setup(t)
	defer ts.Close()

	update := makeUpdateWithMessageText(userId, "Myydään käytetty iPhone 12")
	session.listing = &tori.Listing{
		Subject:  "iPhone 12",
		Category: "5012",
		Type:     tori.ListingTypeSell,
	}

	tg.On("Send", makeMessage(userId, "*Ilmoitusteksti:*\nMyydään käytetty iPhone 12")).
		Return(tgbotapi.Message{}, nil).Once()
	// Bot asks the next field
	tg.On("Send", makeMessage(userId, "Hinta?")).
		Return(tgbotapi.Message{}, nil).Once()

	bot.handleUpdate(update)
	tg.AssertExpectations(t)

	assert.Equal(t, &tori.Listing{
		Subject:  "iPhone 12",
		Body:     "Myydään käytetty iPhone 12",
		Category: "5012",
		Type:     tori.ListingTypeSell,
	}, session.listing)
}

func TestHandleUpdate_EnterPrice(t *testing.T) {
	ts, userId, tg, bot, session := setup(t)
	defer ts.Close()

	update := makeUpdateWithMessageText(userId, "50€")

	session.listing = &tori.Listing{
		Subject:  "iPhone 12",
		Body:     "Myydään käytetty iPhone 12",
		Category: "5012",
		Type:     tori.ListingTypeSell,
	}

	// Bot asks the next field
	tg.On("Send", makeMessageWithReplyMarkup(userId, "Kunto?",
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

	bot.handleUpdate(update)
	tg.AssertExpectations(t)

	assert.Equal(t, &tori.Listing{
		Subject:  "iPhone 12",
		Body:     "Myydään käytetty iPhone 12",
		Category: "5012",
		Type:     tori.ListingTypeSell,
		Price:    50,
	}, session.listing)
}

func TestHandleUpdate_EnterCondition(t *testing.T) {
	ts, userId, tg, bot, session := setup(t)
	defer ts.Close()

	update := makeUpdateWithMessageText(userId, "Uusi")
	session.listing = &tori.Listing{
		Subject:  "iPhone 12",
		Body:     "Myydään käytetty iPhone 12",
		Category: "5012",
		Type:     tori.ListingTypeSell,
		Price:    50,
	}

	// Bot asks the next field
	tg.On("Send", makeMessageWithReplyMarkup(userId, "Laitevalmistaja?",
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

	bot.handleUpdate(update)

	assert.Equal(t, &tori.Listing{
		Subject:  "iPhone 12",
		Body:     "Myydään käytetty iPhone 12",
		Category: "5012",
		Type:     tori.ListingTypeSell,
		Price:    50,
		AdDetails: tori.AdDetails{
			"general_condition": "new",
		},
	}, session.listing)
}

func TestHandleUpdate_EnterManufacturer(t *testing.T) {
	ts, userId, tg, bot, session := setup(t)
	defer ts.Close()

	update := makeUpdateWithMessageText(userId, "Apple")
	session.listing = &tori.Listing{
		Subject:  "iPhone 12",
		Body:     "Myydään käytetty iPhone 12",
		Category: "5012",
		Type:     tori.ListingTypeSell,
		Price:    50,
		AdDetails: tori.AdDetails{
			"general_condition": "new",
		},
	}

	// Bot asks the next field
	tg.On("Send", makeMessageWithReplyMarkup(userId, "Voin lähettää tuotteen",
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

	bot.handleUpdate(update)
	tg.AssertExpectations(t)

	assert.Equal(t, &tori.Listing{
		Subject:  "iPhone 12",
		Body:     "Myydään käytetty iPhone 12",
		Category: "5012",
		Type:     tori.ListingTypeSell,
		Price:    50,
		AdDetails: tori.AdDetails{
			"general_condition": "new",
			"cell_phone":        "apple",
		},
	}, session.listing)
}

func TestHandleUpdate_EnterDeliveryOptions(t *testing.T) {
	ts, userId, tg, bot, session := setup(t)
	defer ts.Close()

	update := makeUpdateWithMessageText(userId, "En")
	session.listing = &tori.Listing{
		Subject:  "iPhone 12",
		Body:     "Myydään käytetty iPhone 12",
		Category: "5012",
		Type:     tori.ListingTypeSell,
		Price:    50,
		AdDetails: tori.AdDetails{
			"general_condition": "new",
			"cell_phone":        "apple",
		},
	}

	tg.On("Send", makeMessageWithRemoveReplyKeyboard(userId, strings.TrimSpace(dedent.Dedent(`
    Ilmoitus on valmis lähetettäväksi, mutta *kuvat puuttuu*.

    /laheta - Lähetä ilmoitus
    /peru - Peru ilmoituksen teko`)),
	)).Return(tgbotapi.Message{}, nil).Once()

	bot.handleUpdate(update)
	tg.AssertExpectations(t)
	assert.Equal(t, &tori.Listing{
		Subject:  "iPhone 12",
		Body:     "Myydään käytetty iPhone 12",
		Category: "5012",
		Type:     tori.ListingTypeSell,
		Price:    50,
		AdDetails: tori.AdDetails{
			"general_condition": "new",
			"cell_phone":        "apple",
			"delivery_options":  []string{},
		},
	}, session.listing)
}

func TestHandleUpdate_AddPhoto(t *testing.T) {
	ts, userId, tg, bot, session := setup(t)
	defer ts.Close()

	session.listing = &tori.Listing{
		Subject:  "iPhone 12",
		Category: "5012",
		Type:     tori.ListingTypeSell,
	}

	tg.On("GetFileDirectURL", "a").Return(ts.URL+"/a.jpg", nil)
	tg.On("GetFileDirectURL", "b").Return(ts.URL+"/b.jpg", nil)
	tg.On("GetFileDirectURL", "c").Return(ts.URL+"/c.jpg", nil)
	tg.On("Send", makeMessage(userId, "3 kuvaa lisätty")).Return(tgbotapi.Message{}, nil).Once()

	const asciiA = 97
	for i := 0; i < 3; i++ {
		go func(i int) {
			bot.handleUpdate(
				tgbotapi.Update{
					Message: &tgbotapi.Message{
						MessageID: i,
						From:      &tgbotapi.User{ID: userId},
						Text:      "",
						Photo: []tgbotapi.PhotoSize{
							{FileID: string(rune(i + asciiA)), FileUniqueID: strconv.Itoa(i + 1), Width: 67, Height: 90, FileSize: 1359},
							{FileID: string(rune(i + asciiA)), FileUniqueID: strconv.Itoa(i + 2), Width: 240, Height: 320, FileSize: 17212},
							{FileID: string(rune(i + asciiA)), FileUniqueID: strconv.Itoa(i + 3), Width: 371, Height: 495, FileSize: 28548},
						},
					},
				},
			)
		}(i)
	}

	assert.Eventually(
		t,
		func() bool {
			return len(session.photos) == 3
		},
		time.Millisecond*100,
		time.Millisecond,
		"expected 3 photos in session.photos",
	)

	assert.Equal(t,
		[]tgbotapi.PhotoSize{
			{FileID: "a", FileUniqueID: "3", Width: 371, Height: 495, FileSize: 28548},
			{FileID: "b", FileUniqueID: "4", Width: 371, Height: 495, FileSize: 28548},
			{FileID: "c", FileUniqueID: "5", Width: 371, Height: 495, FileSize: 28548},
		},
		session.photos,
	)

	tg.AssertExpectations(t)
}

func TestHandleUpdate_AddPhotoInSameMessageAsSubject(t *testing.T) {
	ts, userId, tg, bot, session := setup(t)
	defer ts.Close()

	tg.On("GetFileDirectURL", "a").Return(ts.URL+"/1.jpg", nil)
	tg.On("Send", makeMessage(userId, "*Ilmoituksen otsikko:* iPhone 12")).Return(tgbotapi.Message{}, nil).Once()
	tg.On("Send", makeMessage(userId, "Ilmoitusteksti?")).Return(tgbotapi.Message{}, nil).Once()
	tg.On("Send", makeMessage(userId, "1 kuva lisätty")).Return(tgbotapi.Message{}, nil).Once()
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

	update := tgbotapi.Update{
		Message: &tgbotapi.Message{
			From:    &tgbotapi.User{ID: userId},
			Caption: "iPhone 12",
			Photo: []tgbotapi.PhotoSize{
				{FileID: "a", FileUniqueID: "1", Width: 67, Height: 90, FileSize: 1359},
				{FileID: "a", FileUniqueID: "2", Width: 240, Height: 320, FileSize: 17212},
				{FileID: "a", FileUniqueID: "3", Width: 371, Height: 495, FileSize: 28548},
			},
		},
	}

	bot.handleUpdate(update)

	assert.Eventually(
		t,
		func() bool {
			return len(session.photos) == 1 &&
				assert.ObjectsAreEqual(
					tgbotapi.PhotoSize{
						FileID:       "a",
						FileUniqueID: "3",
						Width:        371,
						Height:       495,
						FileSize:     28548,
					}, session.photos[0])
		},
		time.Millisecond*100,
		time.Millisecond,
		"expected photos in session.photos",
	)

	tg.AssertExpectations(t)
}

func TestHandleUpdate_SendListingWithIncompleteListing(t *testing.T) {
	ts, userId, tg, bot, session := setup(t)
	defer ts.Close()

	update := makeUpdateWithMessageText(userId, "/laheta")
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

	tg.On("Send", makeMessage(userId, "Ilmoituksesta puuttuu kenttiä.")).
		Return(tgbotapi.Message{}, nil).Once()

	bot.handleUpdate(update)
	tg.AssertExpectations(t)
}

func TestHandleUpdate_SendListing(t *testing.T) {
	var postListingJson []byte
	ts := makeTestServerWithOnReqFn(t, func(r *http.Request) {
		if r.Method == "POST" && r.URL.Path == "/v2/listings" {
			if b, err := ioutil.ReadAll(r.Body); err == nil {
				postListingJson = b
			} else {
				t.Fatal(err)
			}
		}
	})
	ts, userId, tg, bot, session := setupWithTestServer(t, ts)
	defer ts.Close()
	update := makeUpdateWithMessageText(userId, "/laheta")
	session.listing = &tori.Listing{
		Subject:  "iPhone 12",
		Body:     "Myydään käytetty iPhone 12",
		Category: "5012",
		Type:     tori.ListingTypeSell,
		Price:    50,
		// Needs to be set true because the archive is created from this listing
		PhoneHidden: true,
		AdDetails: tori.AdDetails{
			"general_condition": "new",
			"cell_phone":        "apple",
			"delivery_options":  []string{},
		},
	}
	session.photos = []tgbotapi.PhotoSize{
		{FileID: "1", FileUniqueID: "1", Width: 371, Height: 495, FileSize: 28548},
		{FileID: "2", FileUniqueID: "2", Width: 371, Height: 495, FileSize: 28548},
	}

	tg.On("GetFileDirectURL", "1").Return(ts.URL+"/1.jpg", nil).Once()
	tg.On("GetFileDirectURL", "2").Return(ts.URL+"/2.jpg", nil).Once()
	tg.On("Send", makeMessageWithRemoveReplyKeyboard(userId, "Ilmoitus lähetetty!")).Return(tgbotapi.Message{}, nil).Once()

	archive := NewListingArchive(*session.listing, session.photos)
	archiveBytes, err := json.Marshal(archive)
	if err != nil {
		t.Fatal(err)
	}
	document := tgbotapi.NewDocument(session.userId, tgbotapi.FileBytes{
		Name:  "archive.json",
		Bytes: archiveBytes,
	})
	document.Caption = session.listing.Subject
	tg.On("Send", document).Return(tgbotapi.Message{}, nil).Once()

	bot.handleUpdate(update)
	tg.AssertExpectations(t)

	wantPostListingJson := `{
  "subject": "iPhone 12",
  "body": "Myydään käytetty iPhone 12",
  "price": {
    "currency": "€",
    "value": 50
  },
  "type": "s",
  "ad_details": {
    "cell_phone": {
      "single": {
        "code": "apple"
      }
    },
    "general_condition": {
      "single": {
        "code": "new"
      }
    }
  },
  "category": "5012",
  "location": {
    "region": "18",
    "zipcode": "00320",
    "area": "313"
  },
  "images": [
    {
      "media_id": "/public/media/ad/a"
    },
    {
      "media_id": "/public/media/ad/a"
    }
  ],
  "phone_hidden": true,
  "account_id": "123123"
}`

	assert.Equal(t, wantPostListingJson, formatJson(postListingJson))
}

func TestHandleUpdate_RemovePhotosCommand(t *testing.T) {
	message := "/poistakuvat"

	ts, userId, tg, bot, session := setup(t)
	defer ts.Close()
	update := makeUpdateWithMessageText(userId, message)

	session.listing = &tori.Listing{
		Subject:  "foo",
		Body:     "bar",
		Category: "5012",
		Type:     tori.ListingTypeSell,
		Price:    50,
	}
	session.photos = []tgbotapi.PhotoSize{
		{FileID: "1", FileUniqueID: "1", Width: 371, Height: 495, FileSize: 28548},
		{FileID: "2", FileUniqueID: "2", Width: 371, Height: 495, FileSize: 28548},
	}

	tg.On("Send", makeMessage(userId, "Kuvat poistettu.")).Return(tgbotapi.Message{}, nil).Once()

	bot.handleUpdate(update)
	tg.AssertExpectations(t)

	assert.Empty(t, session.photos)
}

func TestHandleUpdate_EditSubject(t *testing.T) {
	ts, userId, tg, bot, session := setup(t)
	defer ts.Close()

	session.userSubjectMessageId = 10
	session.botSubjectMessageId = 20
	session.listing = &tori.Listing{
		Subject:  "iPhone 12",
		Body:     "Myydään käytetty iPhone 12",
		Category: "5012",
		Type:     tori.ListingTypeSell,
	}

	update := tgbotapi.Update{
		EditedMessage: &tgbotapi.Message{
			MessageID: 10,
			From:      &tgbotapi.User{ID: userId},
			Text:      "iPhone 13",
		},
	}

	editMsg := tgbotapi.NewEditMessageText(1, 20, "*Ilmoituksen otsikko:* iPhone 13")
	editMsg.ParseMode = tgbotapi.ModeMarkdown
	tg.On("Send", editMsg).Return(tgbotapi.Message{}, nil).Once()

	bot.handleUpdate(update)
	tg.AssertExpectations(t)

	assert.Equal(t, &tori.Listing{
		Subject:  "iPhone 13",
		Body:     "Myydään käytetty iPhone 12",
		Category: "5012",
		Type:     tori.ListingTypeSell,
	}, session.listing)
}

func TestHandleUpdate_EditBody(t *testing.T) {
	ts, userId, tg, bot, session := setup(t)
	defer ts.Close()

	session.userBodyMessageId = 10
	session.botBodyMessageId = 20
	session.listing = &tori.Listing{
		Subject:  "iPhone 12",
		Body:     "Myydään käytetty iPhone 12",
		Category: "5012",
		Type:     tori.ListingTypeSell,
	}

	update := tgbotapi.Update{
		EditedMessage: &tgbotapi.Message{
			MessageID: 10,
			From:      &tgbotapi.User{ID: userId},
			Text:      "Myydään käytetty iPhone 100",
		},
	}

	editMsg := tgbotapi.NewEditMessageText(1, 20, "*Ilmoitusteksti:*\nMyydään käytetty iPhone 100")
	editMsg.ParseMode = tgbotapi.ModeMarkdown
	tg.On("Send", editMsg).Return(tgbotapi.Message{}, nil).Once()

	bot.handleUpdate(update)
	tg.AssertExpectations(t)

	assert.Equal(t, &tori.Listing{
		Subject:  "iPhone 12",
		Body:     "Myydään käytetty iPhone 100",
		Category: "5012",
		Type:     tori.ListingTypeSell,
	}, session.listing)
}

func TestHandleUpdate_UnauthorizedAccess(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {
		t.Fatal("no requests expected")
	}))
	defer ts.Close()
	userId := int64(99999) // Not in userConfigMap
	tg := new(botApiMock)
	bot := NewBot(tg, userConfigMap, ts.URL)

	update := tgbotapi.Update{
		EditedMessage: &tgbotapi.Message{
			MessageID: 10,
			From:      &tgbotapi.User{ID: userId},
			Text:      "/start",
		},
	}

	bot.handleUpdate(update)

	// The test will fail if bot sends any messages
	tg.AssertExpectations(t)
}

func TestHandleUpdate_ImportJson(t *testing.T) {
	ts, userId, tg, bot, session := setup(t)
	defer ts.Close()

	update := tgbotapi.Update{
		Message: &tgbotapi.Message{
			From: &tgbotapi.User{ID: userId},
			Text: "/tuojson",
			ReplyToMessage: &tgbotapi.Message{
				Document: &tgbotapi.Document{
					FileID:       "a",
					FileUniqueID: "1",
					Thumbnail:    (*tgbotapi.PhotoSize)(nil),
					FileName:     "archive.json",
					MimeType:     "application/json",
					FileSize:     589,
				},
			},
		},
	}

	tg.On("GetFileDirectURL", "a").Return(ts.URL+"/archive.json", nil)
	tg.On("Send", makeMessage(userId, "Ilmoitus tuotu arkistosta: Hansket hehe")).Return(tgbotapi.Message{}, nil).Once()

	bot.handleUpdate(update)
	tg.AssertExpectations(t)

	assert.Equal(t, &tori.Listing{
		Subject:     "Hansket hehe",
		Body:        "Tetsetst",
		Price:       60,
		Type:        tori.ListingTypeSell,
		PhoneHidden: true,
		Category:    "3050",
		AdDetails: map[string]any{
			"clothing_kind":     "7",
			"clothing_size":     "21",
			"clothing_sex":      "2",
			"delivery_options":  []string{"delivery_send"},
			"general_condition": "new",
		},
	}, session.listing)

	assert.Equal(t,
		[]tgbotapi.PhotoSize([]tgbotapi.PhotoSize{{
			FileID:       "AgACAgQAAxkBAAIIsWKlqfScWYoKP5x6M6qvPSJd_HWYAAJOuDEbsPUxUSs-WymgTtxjAQADAgADeQADJAQ",
			FileUniqueID: "AQADTrgxG7D1MVF-",
			Width:        1280,
			Height:       960,
			FileSize:     291525,
		}}), session.photos)
}

func TestHandleUpdate_ForgetPrice(t *testing.T) {
	ts, userId, tg, bot, session := setup(t)
	defer ts.Close()

	update := tgbotapi.Update{
		Message: &tgbotapi.Message{
			MessageID: 10,
			From:      &tgbotapi.User{ID: userId},
			Text:      "/unohda hinta",
		},
	}

	session.listing = &tori.Listing{
		Subject:  "iPhone 12",
		Body:     "Myydään käytetty iPhone 12",
		Category: "5012",
		Type:     tori.ListingTypeSell,
		Price:    50,
		// Needs to be set true because the archive is created from this listing
		PhoneHidden: true,
		AdDetails: tori.AdDetails{
			"general_condition": "new",
			"cell_phone":        "apple",
			"delivery_options":  []string{},
		},
	}

	// Bot asks price as it was forgotten just now
	tg.On("Send", makeMessage(userId, "Hinta?")).
		Return(tgbotapi.Message{}, nil).Once()

	bot.handleUpdate(update)
	tg.AssertExpectations(t)

	assert.Equal(t, tori.Price(0), session.listing.Price)
}
