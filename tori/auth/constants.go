package auth

const (
	// Android client_id (from AccountConfigTori.java)
	AndroidClientID    = "6079834b9b0b741812e7e91f"
	AndroidRedirectURI = "fi.tori.www.6079834b9b0b741812e7e91f://login"

	// Used for SPP exchange step (spidServerClientId in Android)
	ExchangeClientID = "650421cf50eeae31ecd2a2d3"

	// HMAC key for gateway signing (decoded from PRODUCTION_HMAC_KEY in AppEnvironment.java)
	HMACKey = "3b535f36-79be-424b-a6fd-116c6e69f137"

	// Base URLs
	LoginBaseURL = "https://login.vend.fi"
	ToriBaseURL  = "https://apps-gw-poc.svc.tori.fi"

	// User agents
	AndroidSDKUserAgent = "user-webflows-sdk-android/5.0.0"
	ToriAppUserAgent    = "Tori/26.4.0 (Android 14; sdk_gphone64_arm64)"
	WebViewUserAgent    = "Mozilla/5.0 (Linux; Android 14; sdk_gphone64_arm64) AppleWebKit/537.36 (KHTML, like Gecko) Version/4.0 Chrome/131.0.6778.39 Mobile Safari/537.36"
)
