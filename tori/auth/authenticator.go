// Package auth implements the multi-step Tori.fi authentication flow.
// The authentication process involves:
// 1. OAuth/PKCE initialization
// 2. Passwordless email login
// 3. Optional MFA via SMS
// 4. Token exchange with Tori.fi
package auth

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"html"
	"io"
	"net/http"
	"net/http/cookiejar"
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

// InitSession initializes the OAuth session.
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

	return a.initOAuthSession()
}

// StartLogin initiates the passwordless login flow with the given email.
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

// RequestSMS requests an SMS code to be sent.
// Only call if SubmitEmailCode returned mfaRequired=true.
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
	// Finish identity and get OAuth code
	oauthCode, err := a.finishIdentity()
	if err != nil {
		return nil, fmt.Errorf("identity finish failed: %w", err)
	}
	a.oauthCode = oauthCode

	// Exchange code for tokens
	accessToken, refreshToken, idToken, err := a.exchangeCodeForTokens()
	if err != nil {
		return nil, fmt.Errorf("token exchange failed: %w", err)
	}
	a.accessToken = accessToken
	a.idToken = idToken

	// Exchange for SPP code
	spidCode, err := a.exchangeForSPPCode()
	if err != nil {
		return nil, fmt.Errorf("SPP exchange failed: %w", err)
	}

	// Final Tori login
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

// --- Internal helper methods ---

// getLoginPage follows redirects to reach the login page.
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

// parseLoginPage extracts CSRF token from the login page.
func (a *Authenticator) parseLoginPage(resp *http.Response) error {
	body, _ := io.ReadAll(resp.Body)

	// Look for bffData div with HTML-encoded JSON
	if matches := bffDataRegex.FindSubmatch(body); len(matches) > 1 {
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

// --- Crypto helpers ---

func generateCodeVerifier() string {
	b := make([]byte, 32)
	rand.Read(b)
	return base64.RawURLEncoding.EncodeToString(b)
}

func generateCodeChallenge(verifier string) string {
	hash := sha256.Sum256([]byte(verifier))
	return base64.RawURLEncoding.EncodeToString(hash[:])
}

func generateDeviceID() string {
	b := make([]byte, 16)
	rand.Read(b)
	// Format as UUID v4
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	return fmt.Sprintf("%08x-%04x-%04x-%04x-%012x",
		b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}

func generateUUID() string {
	b := make([]byte, 16)
	rand.Read(b)
	return fmt.Sprintf("%08X-%04X-%04X-%04X-%012X",
		b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}

// jsonUnmarshal is a helper that wraps json.Unmarshal.
func jsonUnmarshal(data []byte, v interface{}) error {
	return json.Unmarshal(data, v)
}
