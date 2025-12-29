package main

import (
	"fmt"
	"strings"

	"github.com/lithammer/dedent"
)

const (
	listingSentText = "Ilmoitus lähetetty!"
	photosRemoved   = "Kuvat poistettu."

	unexpectedErrorText          = `Odottamaton virhe: %s`
	okText                       = `Ok!`
	startText                    = "Lähetä kuva aloittaaksesi ilmoituksen teon"
	sessionMaybeExpiredText      = "Ilmoituksen tekoa ei voi aloittaa, koska tori-käyttäjäsi tiliä ei voitu hakea - sessio vanhentunut?"
	noLocationsInToriAccountText = "Tori-käyttäjäsi tiedoista puuttuu paikkakunta ja postinumero.\n\nAseta ne täällä: https://login.schibsted.fi/account/summary"

	// Login flow messages
	loginPromptEmailText     = "Anna sähköpostiosoitteesi:"
	loginEmailCodeSentText   = "Koodi lähetetty sähköpostiisi. Anna koodi:"
	loginSMSCodeSentText     = "SMS-koodi lähetetty. Anna koodi:"
	loginSuccessText         = "Kirjautuminen onnistui!"
	loginFailedText          = "Kirjautuminen epäonnistui: %s"
	loginTimeoutText         = "Kirjautuminen aikakatkaistiin. Aloita uudelleen komennolla /login"
	loginAlreadyLoggedInText = "Olet jo kirjautunut sisään."
	loginRequiredText        = "Sinun täytyy kirjautua sisään ensin. Käytä komentoa /login"
	loginCancelledText       = "Kirjautuminen peruutettu."
	loginInProgressText      = "Kirjautuminen kesken. Syötä pyydetty tieto tai peru komennolla /peru"

	// Postal code messages
	postalCodePromptText        = "Mikä on postinumerosi? (esim. 00100)"
	postalCodeInvalidText       = "Postinumeron tulee olla 5 numeroa (esim. 00100)"
	postalCodeUpdatedText       = "✅ Postinumero päivitetty: %s"
	postalCodeCurrentText       = "Nykyinen postinumerosi on *%s*.\n\nSyötä uusi postinumero tai peru komennolla /peru"
	postalCodeNotSetText        = "Postinumeroa ei ole asetettu.\n\nSyötä postinumero (esim. 00100):"
	postalCodeCommandCancelText = "Ok, postinumero ei muutettu."
)

func formatReplyText(text string, a ...any) string {
	return fmt.Sprintf(strings.TrimSpace(dedent.Dedent(text)), a...)
}

func parseCommand(s string) (string, []string) {
	parts := strings.Split(s, " ")
	return parts[0], parts[1:]
}

// isValidPostalCode validates Finnish postal codes (5 digits).
func isValidPostalCode(code string) bool {
	if len(code) != 5 {
		return false
	}
	for _, c := range code {
		if c < '0' || c > '9' {
			return false
		}
	}
	return true
}
