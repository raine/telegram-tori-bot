package auth

import (
	"bytes"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/sha512"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"html"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"regexp"
	"strings"
)

// Package-level compiled regexes for performance
var (
	bffDataRegex   = regexp.MustCompile(`<div id="bffData"[^>]*>([^<]+)</div>`)
	csrfJSONRegex  = regexp.MustCompile(`"csrfToken"\s*:\s*"([^"]+)"`)
	csrfOtherRegex = regexp.MustCompile(`csrfToken['\":\s]+=?\s*['"]([^'"]+)['"]`)
)

// Authenticator handles the multi-step Tori authentication flow.
type Authenticator struct {
	httpClient *http.Client

	// OAuth/PKCE state
	codeVerifier string
	state        string
	nonce        string

	// Session state from server
	csrfToken         string
	passwordlessToken string
	mfaID             string
	mfaNonce          string

	// Result tokens
	oauthCode   string
	accessToken string
	idToken     string

	// Device ID (UUID) - generated once per authenticator instance
	deviceID string
}

// NewAuthenticator creates a new authenticator instance.
func NewAuthenticator() (*Authenticator, error) {
	jar, err := cookiejar.New(nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create cookie jar: %w", err)
	}

	return &Authenticator{
		httpClient: &http.Client{
			Jar: jar,
			CheckRedirect: func(req *http.Request, via []*http.Request) error {
				return http.ErrUseLastResponse
			},
		},
		codeVerifier: generateCodeVerifier(),
		deviceID:     generateDeviceID(),
	}, nil
}

// generateDeviceID creates a new random UUID for device identification.
// This mimics how the Android app generates its installation ID.
func generateDeviceID() string {
	b := make([]byte, 16)
	rand.Read(b)
	// Format as UUID v4: xxxxxxxx-xxxx-4xxx-yxxx-xxxxxxxxxxxx
	b[6] = (b[6] & 0x0f) | 0x40 // Version 4
	b[8] = (b[8] & 0x3f) | 0x80 // Variant 10
	return fmt.Sprintf("%08x-%04x-%04x-%04x-%012x",
		b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}

// InitSession initializes the OAuth session and returns any error.
// This must be called before StartLogin.
func (a *Authenticator) InitSession() error {
	// Generate state for OAuth
	stateBytes := make([]byte, 8)
	if _, err := rand.Read(stateBytes); err != nil {
		return fmt.Errorf("failed to generate state: %w", err)
	}
	a.state = base64.RawURLEncoding.EncodeToString(stateBytes)

	// Generate nonce
	nonceBytes := make([]byte, 8)
	if _, err := rand.Read(nonceBytes); err != nil {
		return fmt.Errorf("failed to generate nonce: %w", err)
	}
	a.nonce = base64.RawURLEncoding.EncodeToString(nonceBytes)

	// Generate PKCE code challenge
	codeChallenge := generateCodeChallenge(a.codeVerifier)

	// Start OAuth authorize flow
	authURL := fmt.Sprintf("%s/oauth/authorize?client_id=%s&redirect_uri=%s&response_type=code&scope=openid+offline_access&state=%s&nonce=%s&prompt=select_account&code_challenge=%s&code_challenge_method=S256",
		LoginBaseURL, AndroidClientID, url.QueryEscape(AndroidRedirectURI), a.state, a.nonce, codeChallenge)

	req, err := http.NewRequest("GET", authURL, nil)
	if err != nil {
		return fmt.Errorf("failed to create auth request: %w", err)
	}
	req.Header.Set("User-Agent", WebViewUserAgent)
	req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8")

	resp, err := a.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("auth request failed: %w", err)
	}
	defer resp.Body.Close()

	// Follow redirects to login page
	location := resp.Header.Get("Location")
	if resp.StatusCode == 302 && strings.HasPrefix(location, "http") {
		return a.getLoginPage(location)
	}

	if resp.StatusCode == 200 {
		return a.parseLoginPage(resp)
	}

	return nil
}

// StartLogin initiates the passwordless login flow with the given email.
// Returns an error if the email check or passwordless start fails.
func (a *Authenticator) StartLogin(email string) error {
	// Step 1: Check email status
	if err := a.checkEmailStatus(email); err != nil {
		return fmt.Errorf("email status check failed: %w", err)
	}

	// Step 2: Start passwordless auth
	token, err := a.startPasswordless(email)
	if err != nil {
		return fmt.Errorf("passwordless start failed: %w", err)
	}
	a.passwordlessToken = token

	return nil
}

// SubmitEmailCode submits the code received via email.
// Returns (mfaRequired, error). If mfaRequired is true, call RequestSMS next.
func (a *Authenticator) SubmitEmailCode(code string) (bool, error) {
	mfaID, mfaRequired, err := a.submitEmailCode(code)
	if err != nil {
		return false, fmt.Errorf("email code submission failed: %w", err)
	}

	a.mfaID = mfaID
	return mfaRequired, nil
}

// RequestSMS requests an SMS code to be sent. Only call if SubmitEmailCode returned mfaRequired=true.
func (a *Authenticator) RequestSMS() error {
	if a.mfaID == "" {
		return fmt.Errorf("no MFA ID available; email code must be submitted first")
	}

	nonce, err := a.requestSMSCode()
	if err != nil {
		return fmt.Errorf("SMS request failed: %w", err)
	}
	a.mfaNonce = nonce

	return nil
}

// SubmitSMSCode submits the SMS verification code.
func (a *Authenticator) SubmitSMSCode(code string) error {
	if a.mfaNonce == "" || a.mfaID == "" {
		return fmt.Errorf("MFA not initialized; call RequestSMS first")
	}

	if err := a.submitSMSCode(code); err != nil {
		return fmt.Errorf("SMS code submission failed: %w", err)
	}

	return nil
}

// Finalize completes the authentication flow and returns the token set.
// Call this after SubmitEmailCode (if no MFA) or SubmitSMSCode (if MFA required).
func (a *Authenticator) Finalize() (*TokenSet, error) {
	// Step 6: Finish identity and get OAuth code
	oauthCode, err := a.finishIdentity()
	if err != nil {
		return nil, fmt.Errorf("identity finish failed: %w", err)
	}
	a.oauthCode = oauthCode

	// Step 7: Exchange code for tokens
	accessToken, refreshToken, idToken, err := a.exchangeCodeForTokens()
	if err != nil {
		return nil, fmt.Errorf("token exchange failed: %w", err)
	}
	a.accessToken = accessToken
	a.idToken = idToken

	// Step 8: Exchange for SPP code
	spidCode, err := a.exchangeForSPPCode()
	if err != nil {
		return nil, fmt.Errorf("SPP exchange failed: %w", err)
	}

	// Step 9: Final Tori login
	userID, bearerToken, err := a.toriLogin(spidCode)
	if err != nil {
		return nil, fmt.Errorf("Tori login failed: %w", err)
	}

	return &TokenSet{
		UserID:       userID,
		BearerToken:  bearerToken,
		AccessToken:  accessToken,
		RefreshToken: refreshToken,
		IDToken:      idToken,
		DeviceID:     a.deviceID,
	}, nil
}

// --- Internal methods ---

func (a *Authenticator) getLoginPage(loginURL string) error {
	for i := 0; i < 10; i++ {
		req, err := http.NewRequest("GET", loginURL, nil)
		if err != nil {
			return err
		}
		req.Header.Set("User-Agent", WebViewUserAgent)
		req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8")

		resp, err := a.httpClient.Do(req)
		if err != nil {
			return err
		}

		if resp.StatusCode == 301 || resp.StatusCode == 302 || resp.StatusCode == 307 {
			location := resp.Header.Get("Location")
			resp.Body.Close()
			if location == "" || !strings.HasPrefix(location, "http") {
				return fmt.Errorf("invalid redirect: %s", location)
			}
			loginURL = location
			continue
		}

		defer resp.Body.Close()
		return a.parseLoginPage(resp)
	}
	return fmt.Errorf("too many redirects")
}

func (a *Authenticator) parseLoginPage(resp *http.Response) error {
	body, _ := io.ReadAll(resp.Body)

	// Look for bffData div with HTML-encoded JSON
	if matches := bffDataRegex.FindSubmatch(body); len(matches) > 1 {
		// Use html.UnescapeString instead of manual replacements
		htmlContent := html.UnescapeString(string(matches[1]))

		var bffData struct {
			CsrfToken string `json:"csrfToken"`
		}
		if err := json.Unmarshal([]byte(htmlContent), &bffData); err == nil && bffData.CsrfToken != "" {
			a.csrfToken = bffData.CsrfToken
		}
	}

	// Fallback patterns using package-level compiled regexes
	if a.csrfToken == "" {
		for _, re := range []*regexp.Regexp{csrfJSONRegex, csrfOtherRegex} {
			if matches := re.FindSubmatch(body); len(matches) > 1 {
				a.csrfToken = string(matches[1])
				break
			}
		}
	}

	// Check response headers
	if a.csrfToken == "" {
		if csrf := resp.Header.Get("X-Csrf-Token"); csrf != "" {
			a.csrfToken = csrf
		}
	}

	return nil
}

func (a *Authenticator) checkEmailStatus(email string) error {
	endpoint := fmt.Sprintf("%s/authn/api/identity/email-status?client_id=%s", LoginBaseURL, AndroidClientID)

	deviceData := `{"fonts":["Arial","Arial Hebrew","Arial Rounded MT Bold","Courier","Courier New","Georgia","Helvetica","Helvetica Neue","Impact","Monaco","Palatino","Times","Times New Roman","Trebuchet MS","Verdana"],"hasLiedBrowser":"0","hasLiedOs":"0","platform":"iOS","plugins":[],"userAgent":"Mobile Safari","userAgentVersion":"26.1"}`

	payload := map[string]string{
		"email":      email,
		"deviceData": deviceData,
	}
	jsonBody, _ := json.Marshal(payload)

	req, err := http.NewRequest("POST", endpoint, bytes.NewReader(jsonBody))
	if err != nil {
		return err
	}

	req.Header.Set("User-Agent", WebViewUserAgent)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Origin", LoginBaseURL)
	req.Header.Set("Sec-Fetch-Site", "same-origin")
	req.Header.Set("Sec-Fetch-Mode", "cors")
	if a.csrfToken != "" {
		req.Header.Set("X-Csrf-Token", a.csrfToken)
	}

	resp, err := a.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if csrf := resp.Header.Get("X-Csrf-Token"); csrf != "" {
		a.csrfToken = csrf
	}

	if resp.StatusCode != 200 {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("email-status failed with status %d: %s", resp.StatusCode, string(body))
	}

	return nil
}

func (a *Authenticator) startPasswordless(email string) (string, error) {
	endpoint := fmt.Sprintf("%s/authn/api/identity/passwordless-start/?client_id=%s", LoginBaseURL, AndroidClientID)

	deviceData := `{"fonts":["Arial","Arial Hebrew","Arial Rounded MT Bold","Courier","Courier New","Georgia","Helvetica","Helvetica Neue","Impact","Monaco","Palatino","Times","Times New Roman","Trebuchet MS","Verdana"],"hasLiedBrowser":"0","hasLiedOs":"0","platform":"iOS","plugins":[],"userAgent":"Mobile Safari","userAgentVersion":"26.1"}`

	data := url.Values{}
	data.Set("connection", "email")
	data.Set("email", email)
	data.Set("deviceData", deviceData)

	req, err := http.NewRequest("POST", endpoint, strings.NewReader(data.Encode()))
	if err != nil {
		return "", err
	}

	req.Header.Set("Referer", LoginBaseURL+"/authn/")
	req.Header.Set("User-Agent", WebViewUserAgent)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded; charset=utf-8")
	req.Header.Set("Origin", LoginBaseURL)
	req.Header.Set("Sec-Fetch-Site", "same-origin")
	req.Header.Set("Sec-Fetch-Mode", "cors")
	if a.csrfToken != "" {
		req.Header.Set("X-Csrf-Token", a.csrfToken)
	}

	resp, err := a.httpClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)

	if resp.StatusCode != 200 {
		return "", fmt.Errorf("passwordless-start failed with status %d: %s", resp.StatusCode, string(body))
	}

	var result struct {
		Data struct {
			Attributes struct {
				Token string `json:"token"`
			} `json:"attributes"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return "", err
	}

	return result.Data.Attributes.Token, nil
}

func (a *Authenticator) submitEmailCode(code string) (string, bool, error) {
	endpoint := fmt.Sprintf("%s/authn/api/identity/passwordless-code/?client_id=%s", LoginBaseURL, AndroidClientID)

	data := url.Values{}
	data.Set("code", code)
	data.Set("remember", "false")
	data.Set("connection", "email")
	data.Set("passwordlessToken", a.passwordlessToken)

	req, err := http.NewRequest("POST", endpoint, strings.NewReader(data.Encode()))
	if err != nil {
		return "", false, err
	}

	req.Header.Set("User-Agent", WebViewUserAgent)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded; charset=utf-8")
	req.Header.Set("Origin", LoginBaseURL)
	if a.csrfToken != "" {
		req.Header.Set("X-Csrf-Token", a.csrfToken)
	}

	resp, err := a.httpClient.Do(req)
	if err != nil {
		return "", false, err
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)

	if resp.StatusCode != 200 {
		return "", false, fmt.Errorf("passwordless-code failed with status %d: %s", resp.StatusCode, string(body))
	}

	// Check if MFA was skipped
	if strings.Contains(string(body), "/success") {
		return "", false, nil
	}

	var result struct {
		Data struct {
			Attributes struct {
				SMS struct {
					ID string `json:"id"`
				} `json:"sms"`
			} `json:"attributes"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return "", false, err
	}

	if result.Data.Attributes.SMS.ID == "" {
		return "", false, nil
	}

	return result.Data.Attributes.SMS.ID, true, nil
}

func (a *Authenticator) requestSMSCode() (string, error) {
	endpoint := fmt.Sprintf("%s/authn/api/auth/assertion/?client_id=%s", LoginBaseURL, AndroidClientID)

	payload := map[string]string{
		"method": "mfa/sms",
		"mfaId":  a.mfaID,
	}
	jsonBody, _ := json.Marshal(payload)

	req, err := http.NewRequest("POST", endpoint, bytes.NewReader(jsonBody))
	if err != nil {
		return "", err
	}

	req.Header.Set("User-Agent", WebViewUserAgent)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Origin", LoginBaseURL)
	if a.csrfToken != "" {
		req.Header.Set("X-Csrf-Token", a.csrfToken)
	}

	resp, err := a.httpClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)

	if resp.StatusCode != 200 {
		return "", fmt.Errorf("auth-assertion failed with status %d: %s", resp.StatusCode, string(body))
	}

	var result struct {
		Data struct {
			Attributes struct {
				Nonce string `json:"nonce"`
			} `json:"attributes"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return "", err
	}

	return result.Data.Attributes.Nonce, nil
}

func (a *Authenticator) submitSMSCode(smsCode string) error {
	endpoint := fmt.Sprintf("%s/authn/api/auth/assertion/sms?client_id=%s", LoginBaseURL, AndroidClientID)

	data := url.Values{}
	data.Set("nonce", a.mfaNonce)
	data.Set("mfaId", a.mfaID)
	data.Set("secret", smsCode)

	req, err := http.NewRequest("POST", endpoint, strings.NewReader(data.Encode()))
	if err != nil {
		return err
	}

	req.Header.Set("User-Agent", WebViewUserAgent)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded; charset=utf-8")
	req.Header.Set("Origin", LoginBaseURL)
	if a.csrfToken != "" {
		req.Header.Set("X-Csrf-Token", a.csrfToken)
	}

	resp, err := a.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("auth-assertion-sms failed with status %d: %s", resp.StatusCode, string(body))
	}

	return nil
}

func (a *Authenticator) finishIdentity() (string, error) {
	endpoint := fmt.Sprintf("%s/authn/identity/finish/?client_id=%s", LoginBaseURL, AndroidClientID)

	data := url.Values{}
	data.Set("deviceData", `{"fonts":["Arial"],"platform":"macOS"}`)
	data.Set("_csrf", a.csrfToken)
	data.Set("remember", "true")

	req, err := http.NewRequest("POST", endpoint, strings.NewReader(data.Encode()))
	if err != nil {
		return "", err
	}

	req.Header.Set("User-Agent", WebViewUserAgent)
	req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8")
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Origin", LoginBaseURL)

	resp, err := a.httpClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	location := resp.Header.Get("Location")
	if location == "" {
		body, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("no redirect from identity/finish: %s", string(body))
	}

	return a.followOAuthRedirects(location)
}

func (a *Authenticator) followOAuthRedirects(location string) (string, error) {
	for i := 0; i < 10; i++ {
		// Check if this is a callback URL with the code
		if strings.Contains(location, "code=") {
			u, err := url.Parse(location)
			if err != nil {
				return "", err
			}
			code := u.Query().Get("code")
			if code != "" {
				return code, nil
			}
		}

		// Check if this is an app scheme redirect
		if strings.HasPrefix(location, "fi.tori.www.") {
			u, err := url.Parse(location)
			if err != nil {
				return "", err
			}
			code := u.Query().Get("code")
			if code != "" {
				return code, nil
			}
		}

		// Don't follow non-http redirects
		if !strings.HasPrefix(location, "http") {
			return "", fmt.Errorf("non-http redirect: %s", location)
		}

		req, err := http.NewRequest("GET", location, nil)
		if err != nil {
			return "", err
		}
		req.Header.Set("User-Agent", WebViewUserAgent)
		req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8")

		resp, err := a.httpClient.Do(req)
		if err != nil {
			return "", err
		}
		resp.Body.Close()

		newLocation := resp.Header.Get("Location")
		if newLocation == "" {
			return "", fmt.Errorf("redirect chain ended without code")
		}
		location = newLocation
	}

	return "", fmt.Errorf("too many redirects")
}

func (a *Authenticator) exchangeCodeForTokens() (accessToken, refreshToken, idToken string, err error) {
	endpoint := fmt.Sprintf("%s/oauth/token", LoginBaseURL)

	data := url.Values{}
	data.Set("grant_type", "authorization_code")
	data.Set("client_id", AndroidClientID)
	data.Set("code", a.oauthCode)
	data.Set("redirect_uri", AndroidRedirectURI)
	data.Set("code_verifier", a.codeVerifier)

	req, err := http.NewRequest("POST", endpoint, strings.NewReader(data.Encode()))
	if err != nil {
		return "", "", "", err
	}

	req.Header.Set("User-Agent", AndroidSDKUserAgent)
	req.Header.Set("Accept", "*/*")
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("X-Oidc", "v1")

	resp, err := a.httpClient.Do(req)
	if err != nil {
		return "", "", "", err
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)

	if resp.StatusCode != 200 {
		return "", "", "", fmt.Errorf("token exchange failed with status %d: %s", resp.StatusCode, string(body))
	}

	var result struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		IDToken      string `json:"id_token"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return "", "", "", err
	}

	return result.AccessToken, result.RefreshToken, result.IDToken, nil
}

func (a *Authenticator) exchangeForSPPCode() (string, error) {
	endpoint := fmt.Sprintf("%s/api/2/oauth/exchange", LoginBaseURL)

	data := url.Values{}
	data.Set("clientId", ExchangeClientID)
	data.Set("type", "code")

	req, err := http.NewRequest("POST", endpoint, strings.NewReader(data.Encode()))
	if err != nil {
		return "", err
	}

	req.Header.Set("User-Agent", "AccountSDKIOSWeb/7.0.2 (iPhone; iOS 26.1)")
	req.Header.Set("Accept", "*/*")
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Authorization", "Bearer "+a.accessToken)

	resp, err := a.httpClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)

	if resp.StatusCode != 200 {
		return "", fmt.Errorf("SPP exchange failed with status %d: %s", resp.StatusCode, string(body))
	}

	var result struct {
		Data struct {
			Code string `json:"code"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return "", err
	}

	return result.Data.Code, nil
}

func (a *Authenticator) toriLogin(spidCode string) (string, string, error) {
	endpoint := fmt.Sprintf("%s/public/login", ToriBaseURL)

	abTestDeviceID := generateUUID()

	payload := map[string]string{
		"deviceId": a.deviceID,
		"idToken":  a.idToken,
		"spidCode": spidCode,
	}
	jsonBody, _ := json.Marshal(payload)

	req, err := http.NewRequest("POST", endpoint, bytes.NewReader(jsonBody))
	if err != nil {
		return "", "", err
	}

	// Calculate HMAC signature
	gwService := "LOGIN-SERVER-AUTH"
	gwKey := CalculateGatewayHMAC("POST", "/public/login", "", gwService, jsonBody)

	req.Header.Set("User-Agent", ToriAppUserAgent)
	req.Header.Set("Accept", "application/json; charset=UTF-8")
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept-Language", "en-US,en;q=0.9")

	// Gateway headers
	req.Header.Set("finn-device-info", "Android, mobile")
	req.Header.Set("finn-gw-service", gwService)
	req.Header.Set("finn-gw-key", gwKey)
	req.Header.Set("finn-app-installation-id", a.deviceID)

	// NMP headers
	req.Header.Set("x-nmp-os-name", "Android")
	req.Header.Set("x-nmp-app-version-name", "26.4.0")
	req.Header.Set("x-nmp-app-brand", "Tori")
	req.Header.Set("x-nmp-os-version", "14")
	req.Header.Set("x-nmp-device", "sdk_gphone64_arm64")
	req.Header.Set("x-nmp-app-build-number", "26357")
	req.Header.Set("buildnumber", "26357")
	req.Header.Set("ab-test-device-id", abTestDeviceID)

	// CMP consent headers
	req.Header.Set("cmp-analytics", "1")
	req.Header.Set("cmp-personalisation", "1")
	req.Header.Set("cmp-marketing", "1")
	req.Header.Set("cmp-advertising", "1")

	// Use a fresh client without cookies
	cleanClient := &http.Client{}
	resp, err := cleanClient.Do(req)
	if err != nil {
		return "", "", err
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)

	if resp.StatusCode != 200 && resp.StatusCode != 201 {
		return "", "", fmt.Errorf("tori login failed with status %d: %s", resp.StatusCode, string(body))
	}

	var result struct {
		UserID int `json:"userId"`
		Token  struct {
			Type  string `json:"type"`
			Value string `json:"value"`
		} `json:"token"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return "", "", err
	}

	return fmt.Sprintf("%d", result.UserID), result.Token.Value, nil
}

// --- Helper functions ---

func generateCodeVerifier() string {
	b := make([]byte, 32)
	rand.Read(b)
	return base64.RawURLEncoding.EncodeToString(b)
}

func generateCodeChallenge(verifier string) string {
	hash := sha256.Sum256([]byte(verifier))
	return base64.RawURLEncoding.EncodeToString(hash[:])
}

func generateUUID() string {
	b := make([]byte, 16)
	rand.Read(b)
	return fmt.Sprintf("%08X-%04X-%04X-%04X-%012X",
		b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}

// RefreshTokens uses a refresh token to obtain new access tokens.
// The deviceID should be the same one used during initial login.
// Returns a new TokenSet with updated tokens.
func RefreshTokens(refreshToken, deviceID string) (*TokenSet, error) {
	// Step 1: Use refresh token to get new OAuth tokens
	endpoint := fmt.Sprintf("%s/oauth/token", LoginBaseURL)

	data := url.Values{}
	data.Set("grant_type", "refresh_token")
	data.Set("client_id", AndroidClientID)
	data.Set("refresh_token", refreshToken)

	req, err := http.NewRequest("POST", endpoint, strings.NewReader(data.Encode()))
	if err != nil {
		return nil, fmt.Errorf("failed to create refresh request: %w", err)
	}

	req.Header.Set("User-Agent", AndroidSDKUserAgent)
	req.Header.Set("Accept", "*/*")
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("X-Oidc", "v1")

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("refresh request failed: %w", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)

	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("token refresh failed with status %d: %s", resp.StatusCode, string(body))
	}

	var tokenResult struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		IDToken      string `json:"id_token"`
	}
	if err := json.Unmarshal(body, &tokenResult); err != nil {
		return nil, fmt.Errorf("failed to parse refresh response: %w", err)
	}

	// Step 2: Exchange for SPP code
	exchangeEndpoint := fmt.Sprintf("%s/api/2/oauth/exchange", LoginBaseURL)

	exchangeData := url.Values{}
	exchangeData.Set("clientId", ExchangeClientID)
	exchangeData.Set("type", "code")

	exchangeReq, err := http.NewRequest("POST", exchangeEndpoint, strings.NewReader(exchangeData.Encode()))
	if err != nil {
		return nil, fmt.Errorf("failed to create exchange request: %w", err)
	}

	exchangeReq.Header.Set("User-Agent", "AccountSDKIOSWeb/7.0.2 (iPhone; iOS 26.1)")
	exchangeReq.Header.Set("Accept", "*/*")
	exchangeReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	exchangeReq.Header.Set("Authorization", "Bearer "+tokenResult.AccessToken)

	exchangeResp, err := client.Do(exchangeReq)
	if err != nil {
		return nil, fmt.Errorf("exchange request failed: %w", err)
	}
	defer exchangeResp.Body.Close()

	exchangeBody, _ := io.ReadAll(exchangeResp.Body)

	if exchangeResp.StatusCode != 200 {
		return nil, fmt.Errorf("SPP exchange failed with status %d: %s", exchangeResp.StatusCode, string(exchangeBody))
	}

	var exchangeResult struct {
		Data struct {
			Code string `json:"code"`
		} `json:"data"`
	}
	if err := json.Unmarshal(exchangeBody, &exchangeResult); err != nil {
		return nil, fmt.Errorf("failed to parse exchange response: %w", err)
	}

	// Step 3: Final Tori login
	loginEndpoint := fmt.Sprintf("%s/public/login", ToriBaseURL)

	loginPayload := map[string]string{
		"deviceId": deviceID,
		"idToken":  tokenResult.IDToken,
		"spidCode": exchangeResult.Data.Code,
	}
	loginBody, _ := json.Marshal(loginPayload)

	loginReq, err := http.NewRequest("POST", loginEndpoint, bytes.NewReader(loginBody))
	if err != nil {
		return nil, fmt.Errorf("failed to create login request: %w", err)
	}

	gwService := "LOGIN-SERVER-AUTH"
	gwKey := CalculateGatewayHMAC("POST", "/public/login", "", gwService, loginBody)

	loginReq.Header.Set("User-Agent", ToriAppUserAgent)
	loginReq.Header.Set("Accept", "application/json; charset=UTF-8")
	loginReq.Header.Set("Content-Type", "application/json")
	loginReq.Header.Set("finn-gw-service", gwService)
	loginReq.Header.Set("finn-gw-key", gwKey)
	loginReq.Header.Set("finn-app-installation-id", deviceID)

	loginResp, err := client.Do(loginReq)
	if err != nil {
		return nil, fmt.Errorf("login request failed: %w", err)
	}
	defer loginResp.Body.Close()

	loginRespBody, _ := io.ReadAll(loginResp.Body)

	if loginResp.StatusCode != 200 && loginResp.StatusCode != 201 {
		return nil, fmt.Errorf("tori login failed with status %d: %s", loginResp.StatusCode, string(loginRespBody))
	}

	var loginResult struct {
		UserID int `json:"userId"`
		Token  struct {
			Value string `json:"value"`
		} `json:"token"`
	}
	if err := json.Unmarshal(loginRespBody, &loginResult); err != nil {
		return nil, fmt.Errorf("failed to parse login response: %w", err)
	}

	return &TokenSet{
		UserID:       fmt.Sprintf("%d", loginResult.UserID),
		BearerToken:  loginResult.Token.Value,
		AccessToken:  tokenResult.AccessToken,
		RefreshToken: tokenResult.RefreshToken,
		IDToken:      tokenResult.IDToken,
		DeviceID:     deviceID,
	}, nil
}

// CalculateGatewayHMAC calculates the FINN-GW-KEY header value.
// Format: HMAC-SHA512(key, "METHOD;PATH?QUERY;SERVICE;BODY") → base64
func CalculateGatewayHMAC(method, path, query, service string, body []byte) string {
	var msg bytes.Buffer
	msg.WriteString(strings.ToUpper(method))
	msg.WriteString(";")
	if path != "" && path != "/" {
		msg.WriteString(path)
	}
	if query != "" {
		msg.WriteString("?")
		msg.WriteString(query)
	}
	msg.WriteString(";")
	msg.WriteString(service)
	msg.WriteString(";")
	if len(body) > 0 {
		msg.Write(body)
	}

	h := hmac.New(sha512.New, []byte(HMACKey))
	h.Write(msg.Bytes())
	signature := h.Sum(nil)

	return base64.StdEncoding.EncodeToString(signature)
}
