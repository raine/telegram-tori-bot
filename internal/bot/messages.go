package bot

// =============================================================================
// General messages
// =============================================================================

const (
	MsgOk              = `Ok!`
	MsgPhotosRemoved   = "Kuvat poistettu."
	MsgListingSent     = "Ilmoitus lähetetty!"
	MsgUnexpectedErr   = `Odottamaton virhe: %s`
	MsgStartPrompt     = "Lähetä kuva aloittaaksesi ilmoituksen teon"
	MsgDraftExpired    = "Ilmoitusluonnos vanheni käyttämättömyyden vuoksi ja poistettiin."
	MsgVersionInfo     = "Versio: %s\nRakennettu: %s"
	MsgNoListingToSend = "Ei ilmoitusta lähetettäväksi. Lähetä ensin kuva."
)

// =============================================================================
// Login flow messages
// =============================================================================

const (
	MsgLoginPromptEmail     = "Anna sähköpostiosoitteesi:"
	MsgLoginEmailCodeSent   = "Koodi lähetetty sähköpostiisi. Anna koodi:"
	MsgLoginSMSCodeSent     = "SMS-koodi lähetetty. Anna koodi:"
	MsgLoginSuccess         = "Kirjautuminen onnistui!"
	MsgLoginFailed          = "Kirjautuminen epäonnistui: %s"
	MsgLoginTimeout         = "Kirjautuminen aikakatkaistiin. Aloita uudelleen komennolla /login"
	MsgLoginAlreadyLoggedIn = "Olet jo kirjautunut sisään."
	MsgLoginRequired        = "Sinun täytyy kirjautua sisään ensin. Käytä komentoa /login"
	MsgLoginCancelled       = "Kirjautuminen peruutettu."
	MsgLoginInProgress      = "Kirjautuminen kesken. Syötä pyydetty tieto tai peru komennolla /peru"
	MsgLoginFirstRequired   = "Kirjaudu sisään ensin."
)

// =============================================================================
// Postal code messages
// =============================================================================

const (
	MsgPostalCodePrompt        = "Mikä on postinumerosi? (esim. 00100)"
	MsgPostalCodeInvalid       = "Postinumeron tulee olla 5 numeroa (esim. 00100)"
	MsgPostalCodeUpdated       = "✅ Postinumero päivitetty: %s"
	MsgPostalCodeCurrent       = "Nykyinen postinumerosi on *%s*.\n\nSyötä uusi postinumero tai peru komennolla /peru"
	MsgPostalCodeNotSet        = "Postinumeroa ei ole asetettu.\n\nSyötä postinumero (esim. 00100):"
	MsgPostalCodeCommandCancel = "Ok, postinumero ei muutettu."
	MsgPostalCodeMissing       = "postinumero puuttuu"
	MsgPostalCodeNotAvailable  = "Postinumerot eivät ole käytettävissä"
)

// =============================================================================
// Admin command messages
// =============================================================================

const (
	MsgAdminUsage           = "Käyttö:\n`/admin users add <user_id>`\n`/admin users remove <user_id>`\n`/admin users list`"
	MsgAdminUserAddUsage    = "Käyttö: `/admin users add <user_id>`"
	MsgAdminUserRemoveUsage = "Käyttö: `/admin users remove <user_id>`"
	MsgAdminUserInvalidID   = "Virheellinen käyttäjä-ID. Anna numero."
	MsgAdminUserAdded       = "✅ Käyttäjä `%d` lisätty."
	MsgAdminUserRemoved     = "🗑 Käyttäjä `%d` poistettu."
	MsgAdminNoUsers         = "Ei sallittuja käyttäjiä."
	MsgAdminAllowedUsers    = "*Sallitut käyttäjät:*\n"
)

// =============================================================================
// Session/account messages
// =============================================================================

const (
	MsgSessionMaybeExpired      = "Ilmoituksen tekoa ei voi aloittaa, koska tori-käyttäjäsi tiliä ei voitu hakea - sessio vanhentunut?"
	MsgNoLocationsInToriAccount = "Tori-käyttäjäsi tiedoista puuttuu paikkakunta ja postinumero.\n\nAseta ne täällä: https://login.schibsted.fi/account/summary"
)

// =============================================================================
// Template messages
// =============================================================================

const (
	MsgTemplateNotAvailable = "Mallit eivät ole käytettävissä"
	MsgTemplateNotSet       = "Ei tallennettua mallia.\n\nAseta malli: `/malli <teksti>`\n\nMuuttujat: `{{.shipping}}`, `{{.giveaway}}`, `{{.price}}`\n\nEsim: `/malli {{if not .shipping}}Vain nouto Kannelmäestä. {{end}}Mobilepay/käteinen.`"
	MsgTemplateCurrentFmt   = "*Nykyinen malli:*\n`%s`\n\nPoista malli: /poistamalli"
	MsgTemplateSaved        = "✅ Malli tallennettu."
	MsgTemplateDeleted      = "🗑 Malli poistettu."

	// LLM template generation
	MsgCreateTemplateUsage = "Käyttö: `/luomalli <kuvaus>`\nEsim: `/luomalli Kerro että vain nouto, paitsi jos postitus on valittu`"
	MsgGeneratingTemplate  = "Luodaan mallia..."
	MsgTemplateGenerated   = "✅ Malli luotu ja tallennettu:\n`%s`"
	MsgTemplateGenNotAvail = "Mallin luonti ei ole käytettävissä"
	MsgTemplateGenInvalid  = "Virhe: tekoäly loi virheellisen mallin rakenteen. Yritä uudelleen toisella kuvauksella."
)

// =============================================================================
// Image analysis and draft creation messages
// =============================================================================

const (
	MsgAnalyzingImage        = "Analysoidaan kuvaa..."
	MsgAnalyzingImages       = "Analysoidaan %d kuvaa..."
	MsgAddingPhoto           = "Lisätään kuva..."
	MsgPhotoAdded            = "Kuva lisätty! Kuvia yhteensä: %d"
	MsgWaitCreatingDraft     = "Odota hetki, luodaan ilmoitusta..."
	MsgImageAnalysisNotAvail = "Kuva-analyysi ei ole käytettävissä"
	MsgImageDownloadFailed   = "Virhe: kuvien lataus epäonnistui"
	MsgImageUploadFailed     = "Virhe: kuvien lähetys epäonnistui"
	MsgConnectionInitFailed  = "Virhe: ei voitu alustaa yhteyttä"
)

// =============================================================================
// Category selection messages
// =============================================================================

const (
	MsgSelectCategory        = "Valitse osasto"
	MsgCategorySelected      = "Osasto: *%s*"
	MsgNoCategoryPredictions = "Ei osastoehdotuksia, käytetään oletusta."
	MsgNoActiveListing       = "Ei aktiivista ilmoitusta"
	MsgNoActiveListingPhoto  = "Ei aktiivista ilmoitusta. Lähetä ensin kuva."
	MsgNoCategoryOptions     = "Ei osastoehdotuksia saatavilla."
	MsgWhatToChange          = "Mitä haluat muuttaa?"
)

// Button labels for category reselection
const (
	BtnChangeCategory     = "Vaihda osasto"
	BtnReselectAttributes = "Valitse lisätiedot uudelleen"
)

// =============================================================================
// Attribute selection messages
// =============================================================================

const (
	MsgSelectAttribute      = "Valitse %s"
	MsgSelectAttributeRetry = "Valitse jokin vaihtoehdoista tai paina '%s': %s"
)

// =============================================================================
// Price input messages
// =============================================================================

const (
	MsgEnterPrice             = "Syötä hinta"
	MsgEnterPriceWithEstimate = "Syötä hinta%s"
	MsgPriceEstimateFmt       = "\n\n💡 *Hinta-arvio* (%d ilmoitusta):\nKeskihinta: *%d€* (vaihteluväli %d–%d€)"
	MsgPriceConfirmed         = "Hinta: *%d€*"
	MsgPriceGiveaway          = "Hinta: *Annetaan*"
	MsgPriceNotUnderstood     = "En ymmärtänyt hintaa. Syötä hinta numerona (esim. 50€ tai 50)"
	MsgEnterPriceFirst        = "Syötä ensin hinta."
)

// =============================================================================
// Shipping selection messages
// =============================================================================

const (
	MsgShippingQuestion     = "Onko postitus mahdollinen?"
	MsgSelectShippingFirst  = "Valitse ensin postitusvaihtoehto."
	MsgEnterPostalCodeFirst = "Syötä ensin postinumero."

	// Tori Diili shipping messages
	MsgPackageSizePrompt  = "📦 *Valitse paketin koko:*\n\nOstaja maksaa toimituskulut."
	MsgShippingSetupError = "Virhe haettaessa lähetystietoja. Jatketaan ilman lähetystä."
	MsgShippingNoProfile  = "📦 Lähetystietoja ei löytynyt.\n\nAseta lähetystiedot Tori-sovelluksessa ensin (luo ilmoitus ToriDiilillä). Jatketaan ilman lähetystä."
)

// Button labels for shipping
const (
	BtnYes = "Kyllä"
	BtnNo  = "Ei"
)

// =============================================================================
// Listing state messages (flow prompts)
// =============================================================================

const (
	MsgSelectCategoryFirst = "Valitse ensin osasto."
	MsgFillAttributesFirst = "Täytä ensin lisätiedot."
	MsgListingNotReady     = "Ilmoitus ei ole valmis lähetettäväksi."
)

// =============================================================================
// Ad summary messages
// =============================================================================

const (
	MsgSummaryHeader  = "*Ilmoitus valmis:*"
	MsgSendingListing = "Lähetetään ilmoitusta..."
	MsgPublishingSoon = "✅ Ilmoitus julkaistaan kohta..."
)

// Button labels for summary
const (
	BtnPublish = "✅ Julkaise"
	BtnCancel  = "❌ Peru"
)

// =============================================================================
// Edit confirmation messages
// =============================================================================

const (
	MsgTitleUpdated       = "✅ Otsikko päivitetty: %s"
	MsgDescriptionUpdated = "✅ Kuvaus päivitetty"
	MsgChangesConfirm     = "✓ %s"
	MsgMultipleChanges    = "✓ Muutokset tehty:\n- %s"
	MsgPriceChange        = "Hinta: %d€ → %d€"
	MsgTitleChange        = "Otsikko: %s"
	MsgDescriptionChange  = "Kuvaus päivitetty"
	MsgEditTempError      = "Muokkauskomennon käsittely epäonnistui väliaikaisesti. Yritä uudelleen."
)

// =============================================================================
// Bulk mode messages
// =============================================================================

const (
	MsgBulkAlreadyActive       = "Olet jo erätilassa. Käytä /valmis kun olet valmis tai /peru peruuttaaksesi."
	MsgBulkHasActiveListing    = "Sinulla on aktiivinen ilmoitus. Lähetä se ensin /laheta tai peru /peru ennen erätilaa."
	MsgBulkStarted             = "*Erätila aloitettu*\n\nLähetä kuvia luodaksesi useita ilmoituksia kerralla.\n• Yksittäiset kuvat = erilliset ilmoitukset\n• Albumit = yksi ilmoitus useilla kuvilla\n\nMax 10 ilmoitusta. Käytä /valmis kun olet valmis."
	MsgBulkMaxDraftsReached    = "Maksimimäärä (%d) ilmoituksia saavutettu."
	MsgBulkNotInBulkMode       = "Et ole erätilassa. Aloita /era komennolla."
	MsgBulkSendPhotosFirst     = "Lähetä ensin kuvia."
	MsgBulkWaitAnalysis        = "Odota, analysointi on vielä kesken..."
	MsgBulkEditListings        = "📋 *Muokkaa ilmoituksia:*\n\nKlikkaa painikkeita muokataksesi. Kun valmis, käytä /laheta."
	MsgBulkCancelled           = "Erätila peruutettu."
	MsgBulkEnded               = "Erätila päättyi."
	MsgBulkAllSentEnded        = "Kaikki ilmoitukset lähetetty! Erätila päättyi."
	MsgBulkSendPhotosOrFinish  = "Lähetä kuvia tai käytä /valmis kun olet valmis."
	MsgBulkSendPhotosOrCommand = "Lähetä lisää kuvia tai /valmis"
	MsgBulkSendPhotosToStart   = "Lähetä kuvia aloittaaksesi...\n"
	MsgBulkStatusHeader        = "📦 *Ilmoitukset (%d)*\n\n"
	MsgBulkAnalyzing           = "Analysoidaan... (📷 %d)\n"
	MsgBulkError               = "Virhe: %s\n"
	MsgBulkListingDeleted      = "Ilmoitus poistettu."
	MsgBulkEditCancelled       = "Muokkaus peruutettu."
	MsgBulkConfirmDelete       = "Haluatko varmasti poistaa ilmoituksen %d?"
	MsgBulkInvalidNumber       = "Virheellinen numero. Käytä 1-%d."
	MsgBulkListingNotFound     = "Ilmoitusta ei löydy."
	MsgBulkListingNotReady     = "Ilmoitus %d ei ole valmis. Täytä puuttuvat tiedot."
	MsgBulkSendingSingle       = "Lähetetään ilmoitusta %d..."
	MsgBulkPublishedSingle     = "✅ Ilmoitus %d julkaistu!"
	MsgBulkNoReadyListings     = "Ei valmiita ilmoituksia lähetettäväksi. Täytä puuttuvat tiedot."
	MsgBulkSendingMultiple     = "Lähetetään %d ilmoitusta..."
	MsgBulkPublishedMultiple   = "✅ %d ilmoitusta julkaistu!"
	MsgBulkPublishedWithErrors = "✅ %d ilmoitusta julkaistu, ❌ %d epäonnistui."
)

// Bulk mode field editing
const (
	MsgBulkEnterNewTitle  = "Syötä uusi otsikko ilmoitukselle %d:"
	MsgBulkEnterNewDesc   = "Syötä uusi kuvaus ilmoitukselle %d:"
	MsgBulkEnterPrice     = "Syötä hinta ilmoitukselle %d:%s"
	MsgBulkPriceEstimate  = "\n\n*Hinta-arvio* (%d ilmoitusta):\nKeskihinta: *%d€* (vaihteluväli %d–%d€)"
	MsgBulkSelectCategory = "Valitse osasto:"
	MsgBulkTitleUpdated   = "Otsikko päivitetty: *%s*"
	MsgBulkDescUpdated    = "Kuvaus päivitetty."
	MsgBulkPriceSet       = "Hinta asetettu: *%d€*"
	MsgBulkPriceGiveaway  = "Hinta asetettu: *Annetaan*"
	MsgBulkCategorySet    = "Osasto asetettu: *%s*"
	MsgBulkShippingSet    = "Postitus asetettu: *%s*"
)

// Bulk mode button labels
const (
	BtnBulkTitle       = "Otsikko"
	BtnBulkDescription = "Kuvaus"
	BtnBulkPrice       = "Hinta"
	BtnBulkCategory    = "Osasto"
	BtnBulkShipping    = "Postitus"
	BtnBulkDelete      = "Poista"
	BtnBulkGiveaway    = "Annetaan"
	BtnBulkConfirmDel  = "Kyllä, poista"
)

// Bulk mode draft status
const (
	MsgBulkOnePhoto          = "📷 1 kuva\n\n"
	MsgBulkMultiPhotos       = "📷 %d kuvaa\n\n"
	MsgBulkPriceFmt          = "💰 Hinta: %d€\n"
	MsgBulkPriceWithEstimate = "💰 Hinta: %d€ _(keskihinta %d ilmoituksesta, %d–%d€)_\n"
	MsgBulkPriceNotSet       = "💰 Hinta: _ei asetettu_\n"
	MsgBulkPriceGiven        = "💰 Hinta: Annetaan\n"
	MsgBulkCategoryFmt       = "🏷️ Osasto: %s\n"
	MsgBulkCategoryNone      = "🏷️ Osasto: _ei valittu_\n"
	MsgBulkShippingYes       = "🚚 Postitus: Kyllä\n"
	MsgBulkShippingNo        = "🚚 Postitus: Ei\n"
	MsgBulkReadyToSend       = "\n✅ Valmis lähetettäväksi"
	MsgBulkFillMissing       = "\n⚠️ Täytä puuttuvat tiedot"
)

// Draft creation error messages (shared by single listing and bulk modes)
const (
	MsgErrImageDownload  = "Kuvien lataus epäonnistui"
	MsgErrImageAnalysis  = "Kuva-analyysi ei käytettävissä"
	MsgErrAnalysisFailed = "Analyysi epäonnistui"
	MsgErrToriConnection = "Tori-yhteys epäonnistui"
	MsgErrDraftCreation  = "Luonnin luonti epäonnistui"
	MsgErrImageUpload    = "Kuvien lähetys epäonnistui"
	MsgErrImageSet       = "Kuvien asetus epäonnistui"
)

// =============================================================================
// Listing management messages (/ilmoitukset)
// =============================================================================

const (
	MsgNoListings           = "Sinulla ei ole ilmoituksia."
	MsgListingsHeader       = "*Omat ilmoitukset* — Sivu %d/%d (%d %s)\n"
	MsgListingsCountSingle  = "ilmoitus"
	MsgListingsCountPlural  = "ilmoitusta"
	MsgActionFailed         = "❌ Toiminto epäonnistui: %s"
	MsgUnknownAction        = "Tuntematon toiminto: "
	MsgMarkedAsSold         = "✅ Ilmoitus merkitty myydyksi"
	MsgReactivated          = "✅ Ilmoitus aktivoitu uudelleen"
	MsgConfirmDelete        = "⚠️ *Haluatko varmasti poistaa ilmoituksen?*\n\n\"%s\"\n\nTätä ei voi perua."
	MsgRepublishProgress    = "⏳ Luodaan uusi ilmoitus samoilla tiedoilla..."
	MsgRepublishFetchError  = "❌ Virhe haettaessa ilmoituksen tietoja: %s"
	MsgRepublishCreateError = "❌ Virhe luotaessa ilmoitusta: %s"
	MsgRepublishUpdateError = "❌ Virhe päivitettäessä ilmoitusta: %s"
	MsgRepublishDeliveryErr = "❌ Virhe asetettaessa toimitustapoja: %s"
	MsgRepublishPublishErr  = "❌ Virhe julkaistaessa ilmoitusta: %s"
	MsgRepublishSuccess     = "✅ Ilmoitus julkaistu uudelleen!"
)

// Listing detail view
const (
	MsgListingStats       = "👁 %s | ❤️ %s\n"
	MsgListingPending     = "⏳ Tarkistettavana\n"
	MsgListingExpiresDays = "⏰ Vanhenee %d päivässä\n"
	MsgListingStateFmt    = "📋 Tila: %s\n"
)

// Listing action buttons
const (
	BtnMarkAsSold    = "Merkitse myydyksi"
	BtnReactivate    = "Aktivoi uudelleen"
	BtnRepublish     = "Julkaise uudelleen"
	BtnDelete        = "Poista"
	BtnDeleteConfirm = "Poista pysyvästi"
	BtnBack          = "Takaisin"
	BtnShowOld       = "Näytä vanhat"
	BtnHideOld       = "Piilota vanhat"
	BtnClose         = "Sulje"
	BtnPrev          = "Edellinen"
	BtnNext          = "Seuraava"
)

// =============================================================================
// Search watch messages (/haku, /seuraa, /seurattavat)
// =============================================================================

const (
	MsgSearchResults      = "🔍 *Hakutulokset: \"%s\"*\nLöytyi %d ilmoitusta\n\n"
	MsgSearchNoResults    = "🔍 *Hakutulokset: \"%s\"*\nEi tuloksia\n"
	MsgSearchError        = "❌ Haku epäonnistui: %s"
	MsgSearchQueryMissing = "Käyttö: `/haku <hakusana>`\nEsim: `/haku iphone 14`"

	MsgWatchCreated       = "✅ Seuranta luotu: \"%s\"\n\nIlmoitan kun uusia ilmoituksia ilmestyy."
	MsgWatchDeleted       = "🗑 Seuranta poistettu."
	MsgWatchAlreadyExists = "Seuranta haulle \"%s\" on jo olemassa."
	MsgWatchLimitReached  = "Olet saavuttanut seurantojen maksimimäärän (%d)."
	MsgWatchNotFound      = "Seurantaa ei löydy."
	MsgWatchQueryMissing  = "Käyttö: `/seuraa <hakusana>`\nEsim: `/seuraa iphone 14`"

	MsgNoWatches     = "Sinulla ei ole seurantoja.\n\nLuo seuranta: `/seuraa <hakusana>`"
	MsgWatchesHeader = "🔔 *Seurannat* (%d kpl)\n\n"
	MsgWatchItem     = "%d. \"%s\"\n"

	MsgNewListing      = "🔔 *Uusi ilmoitus:* \"%s\"\n\n"
	MsgListingTitle    = "*%s*\n"
	MsgListingPrice    = "💰 %s\n"
	MsgListingLocation = "📍 %s\n"
)

// Search watch button labels
const (
	BtnCreateWatch = "🔔 Seuraa hakua"
	BtnDeleteWatch = "🗑️"
	BtnOpenInTori  = "Avaa Torissa"
)

// Maximum watches per user
const MaxWatchesPerUser = 10
