package bot

import (
	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"github.com/rs/zerolog/log"
)

// Command defines a bot command with its handler key and Telegram menu description.
type Command struct {
	Name        string // Command name without slash (e.g., "start")
	Description string // Description shown in Telegram command menu
}

// botCommands defines all available bot commands.
// This is the single source of truth for command definitions.
var botCommands = []Command{
	{Name: "peru", Description: "Peru ilmoituksen teko"},
	{Name: "laheta", Description: "Lähetä ilmoitus"},
	{Name: "era", Description: "Aloita erätila (useita ilmoituksia)"},
	{Name: "valmis", Description: "Lopeta kuvien lähetys erätilassa"},
	{Name: "poistakuvat", Description: "Poista ilmoituksen kuvat"},
	{Name: "osasto", Description: "Vaihda osastoa"},
	{Name: "malli", Description: "Näytä/aseta kuvauspohja"},
	{Name: "poistamalli", Description: "Poista kuvauspohja"},
	{Name: "postinumero", Description: "Näytä/vaihda postinumero"},
	{Name: "ilmoitukset", Description: "Hallitse Tori-ilmoituksia"},
	{Name: "login", Description: "Kirjaudu Toriin"},
	{Name: "versio", Description: "Näytä version tiedot"},
}

// RegisterCommands sets the bot's command menu in Telegram.
// This should be called once at startup.
func RegisterCommands(tg *tgbotapi.BotAPI) {
	commands := make([]tgbotapi.BotCommand, len(botCommands))
	for i, cmd := range botCommands {
		commands[i] = tgbotapi.BotCommand{
			Command:     cmd.Name,
			Description: cmd.Description,
		}
	}

	config := tgbotapi.NewSetMyCommands(commands...)
	if _, err := tg.Request(config); err != nil {
		log.Error().Err(err).Msg("failed to set bot commands")
	} else {
		log.Info().Int("count", len(commands)).Msg("registered bot commands")
	}
}
