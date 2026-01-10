# telegram-tori-bot

Telegram-botti, joka tekee tavaroiden myymisestä Torissa mahdollisimman
vaivatonta. Tavaroiden laittaminen myyntiin tällä on oikeasti mukavaa. Hyödyntää
Telegramin kuvanlähetystä ja botti-ominaisuuksia kuten mukautettuja
näppäimistöjä.

## Ominaisuudet

### Tekoälypohjainen ilmoituksen luonti

Lähetä kuva ja botti käyttää Gemini Vision API:a luodakseen automaattisesti
otsikon ja kuvauksen ilmoituksellesi. Useammat kuvat (albumit) analysoidaan
yhdessä paremman kontekstin saamiseksi.

### Tekoälyautomaatio

- **Automaattinen osastovalinta**: Tekoäly valitsee automaattisesti sopivimman
  osaston tuotteen perusteella
- **Automaattinen ominaisuuksien täyttö**: Osastokohtaiset ominaisuudet (koko,
  väri, kunto jne.) täytetään automaattisesti
- **Hintasuositukset**: Näyttää vastaavien ilmoitusten hintoja, jotta voit
  hinnoitella tuotteesi kilpailukykyisesti

### Luonnollinen kieli muokkauksessa

Muokkaa ilmoitusluonnosta kirjoittamalla suomeksi, esim. "vaihda hinnaksi 40e"
tai "lisää että koirataloudesta". Botti ymmärtää ja tekee muutokset.

### Lahjoitustila

Listaa tavaroita ilmaiseksi valitsemalla "Annetaan"-painike hintakyselyssä.
Kuvaus muokataan automaattisesti "Annetaan"-muotoon.

### Tori Diili -postitus

Ota Tori Diili -postitus käyttöön tarjotaksesi ostajille turvallisen postituksen
integroidulla maksulla. Kun valitset "Kyllä" postitukselle, botti hakee
tallennetun toimitusosoitteesi Torista ja pyytää valitsemaan paketin koon:

- **S** (max 4kg, 40×32×15cm) - 2,99€
- **M** (max 25kg, 40×32×26cm) - 4,99€
- **L** (max 25kg, 100×60×60cm) - 12,99€

Näytetyt hinnat ovat esimerkkejä ja voivat muuttua.

**Huom**: Sinulla täytyy olla tallennettu toimitusprofiili Torissa. Luo
sellainen tekemällä ensin Tori Diili -ilmoitus virallisessa Tori-sovelluksessa.

### Kuvausmallit

Tallenna kuvausmalli komennolla `/malli`, joka lisätään kaikkiin ilmoituksiisi.
Tukee ehdollista tekstiä Go:n
[text/template](https://pkg.go.dev/text/template)-syntaksilla.

Muuttujat:

- `{{.shipping}}` - postitus mahdollinen (true/false)
- `{{.giveaway}}` - annetaan ilmaiseksi (true/false)
- `{{.price}}` - hinta euroina (0 jos annetaan)

```
/malli Nouto Kannelmäestä{{if .shipping}} tai postitus{{end}}. Mobilepay/käteinen.
```

### Ilmoitusten hallinta

Käytä `/ilmoitukset`-komentoa selataksesi ja hallitaksesi olemassa olevia
Tori-ilmoituksiasi suoraan Telegramissa:

- Näe kaikki aktiiviset ja odottavat ilmoituksesi klikkaus- ja
  suosikkitilastoilla
- Merkitse tuotteet myydyiksi tai aktivoi myydyt ilmoitukset uudelleen
- Poista ilmoituksia vahvistuksella
- Vaihda aktiivisten ja vanhempien/vanhentuneiden ilmoitusten välillä
- Julkaise vanhentuneet ilmoitukset uudelleen yhdellä napinpainalluksella
  (kopioi kaikki tiedot kuvat mukaan lukien)

### Muut ominaisuudet

- **Sisäänrakennettu kirjautuminen**: Kirjaudu suoraan botin kautta komennolla
  `/login` (sähköpostin vahvistuskoodi)

## Pikaopas

1. **Lataa**
   [uusin versio](https://github.com/raine/telegram-tori-bot/releases/latest)
   alustallesi:

   | Alusta                | Lataus                                                                                                                                         |
   | --------------------- | ---------------------------------------------------------------------------------------------------------------------------------------------- |
   | Linux (x64)           | [telegram-tori-bot-linux-amd64](https://github.com/raine/telegram-tori-bot/releases/latest/download/telegram-tori-bot-linux-amd64)             |
   | Linux (ARM64)         | [telegram-tori-bot-linux-arm64](https://github.com/raine/telegram-tori-bot/releases/latest/download/telegram-tori-bot-linux-arm64)             |
   | macOS (Apple Silicon) | [telegram-tori-bot-darwin-arm64](https://github.com/raine/telegram-tori-bot/releases/latest/download/telegram-tori-bot-darwin-arm64)           |
   | Windows (x64)         | [telegram-tori-bot-windows-amd64.exe](https://github.com/raine/telegram-tori-bot/releases/latest/download/telegram-tori-bot-windows-amd64.exe) |

2. **Suorita** ladattu tiedosto (tuplaklikkaa tai aja terminaalista)

3. **Seuraa asennusvelhoa** - se opastaa sinut läpi:
   - Telegram-botin luominen @BotFatherin kautta
   - Gemini API -avaimen hankkiminen
   - Telegram-käyttäjätunnuksesi löytäminen

4. **Aloita botin käyttö**
   - Etsi bottisi Telegramista sen käyttäjänimellä
   - Lähetä `/start`, sitten `/login` yhdistääksesi Tori-tilisi
   - Lähetä kuva jostain mitä haluat myydä

### Windows-käyttäjille

Windows saattaa näyttää "Windows suojasi tietokonettasi" -varoituksen
allekirjoittamattomille ohjelmille. Klikkaa "Lisätietoja" ja sitten "Suorita
silti" jatkaaksesi.

### Vaihtoehto: Asenna Go:lla

Jos sinulla on Go asennettuna, voit asentaa myös näin:

```sh
go install github.com/raine/telegram-tori-bot@latest
```

## Tekoälyn kustannukset

Botti käyttää Googlen Gemini API:a kuva- ja tekstinkäsittelyyn. Ilmaistaso
saattaa riittää hyvin (10 pyyntöä/min, 250 pyyntöä/päivä). Maksullisen tason
kustannukset ovat minimaaliset - tyypillinen ilmoituksen luonti maksaa reilusti
alle 0,01 USD:

```
INF image(s) analyzed cost=0.0008925 imageCount=1 title="LUMI Recovery Pod kylmäallas"
INF category selection llm call costUSD=0.000019725 inputTokens=223 model=gemini-2.5-flash-lite outputTokens=10
```

## Asetukset

Asennusvelho luo automaattisesti `.env`-tiedoston asetuksillasi. Voit myös
asettaa nämä ympäristömuuttujina:

| Muuttuja            | Pakollinen | Kuvaus                                                |
| ------------------- | ---------- | ----------------------------------------------------- |
| `BOT_TOKEN`         | Kyllä      | Telegram-botin token @BotFatherilta                   |
| `GEMINI_API_KEY`    | Kyllä      | Google Gemini API -avain tekoälyominaisuuksille       |
| `TORI_TOKEN_KEY`    | Kyllä      | Salainen avain Tori-tunnistautumistokenien salaukseen |
| `ADMIN_TELEGRAM_ID` | Kyllä      | Telegram-käyttäjätunnuksesi (tulee ylläpitäjäksi)     |
| `TORI_DB_PATH`      | Ei         | SQLite-tietokannan polku (oletus: `sessions.db`)      |

## Käyttöönotto

Torin kirjautuminen käyttää reCAPTCHA-validointia IP-maineen perusteella. Botin
täytyy toimia IP-osoitteesta, josta olet aiemmin kirjautunut Toriin selaimella
tai virallisella sovelluksella. Epäluotettavat IP:t epäonnistuvat "reCaptcha was
invalid" -virheellä kirjautumisen yhteydessä. Raspberry Pi kotiverkossasi on
helppo vaihtoehto, koska käytät todennäköisesti jo Toria samasta IP-osoitteesta.

### Raspberry Pi -käyttöönotto

[`deployment/`](deployment/)-hakemisto sisältää esimerkkiasetukset Raspberry
Pi:lle systemd-palveluna.

## Käyttöoikeuksien hallinta

Botti käyttää sallittujen käyttäjien listaa. Vain ylläpitäjä (määritetty
`ADMIN_TELEGRAM_ID`:llä) ja erikseen sallitut käyttäjät voivat käyttää bottia.
Luvattomat käyttäjät eivät saa vastausta.

### Ylläpitäjäkomennot

Ylläpitäjä voi hallita sallittuja käyttäjiä näillä komennoilla (eivät näy botin
valikossa):

- `/admin users add <käyttäjä_id>` - Lisää käyttäjä sallittujen listalle
- `/admin users remove <käyttäjä_id>` - Poista käyttäjä sallittujen listalta
- `/admin users list` - Listaa kaikki sallitut käyttäjät

## Komennot

| Komento        | Kuvaus                                |
| -------------- | ------------------------------------- |
| `/login`       | Kirjaudu Tori-tilillesi               |
| `/peru`        | Peruuta nykyinen ilmoituksen luonti   |
| `/laheta`      | Julkaise ilmoitus                     |
| `/era`         | Siirry erätilaan (useita ilmoituksia) |
| `/valmis`      | Lopeta kuvien lisääminen erätilassa   |
| `/poistakuvat` | Poista ilmoituksen kuvat              |
| `/osasto`      | Vaihda osastoa                        |
| `/malli`       | Näytä tai aseta kuvausmalli           |
| `/poistamalli` | Poista kuvausmalli                    |
| `/postinumero` | Näytä tai vaihda postinumero          |
| `/ilmoitukset` | Hallitse Tori-ilmoituksiasi           |

## UKK

### Miten aloitan alusta jos teen virheen jota ei voi perua?

Käytä komentoa `/peru`. Se unohtaa kaiken nykyisestä ilmoituksen luonnista ja
poistaa mahdollisen luodun luonnoksen Torista.

### Mikä ladatuista kuvista tulee ilmoituksen pääkuvaksi?

Ensimmäinen ladattu kuva. Kun lataat useita kuvia Telegram-sovelluksessa, kuvien
järjestystä voi muuttaa.

## Kehitys

Projekti käyttää [`just`](https://github.com/casey/just)-komentorivityökalua.

```sh
git clone https://github.com/raine/telegram-tori-bot.git
cd telegram-tori-bot
just build    # Käännä projekti
just check    # Aja formatointi, vet, käännös ja testit
just test     # Aja vain testit
just run      # Käynnistä botti
```

Aja `just -l` nähdäksesi kaikki käytettävissä olevat komennot.
