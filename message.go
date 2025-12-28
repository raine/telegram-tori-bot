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
)

func formatReplyText(text string, a ...any) string {
	return fmt.Sprintf(strings.TrimSpace(dedent.Dedent(text)), a...)
}

func parseCommand(s string) (string, []string) {
	parts := strings.Split(s, " ")
	return parts[0], parts[1:]
}
