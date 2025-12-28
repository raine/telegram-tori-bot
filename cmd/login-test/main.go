package main

import (
	"bufio"
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"crypto/sha512"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"math/rand"
	"net/http"
	"net/http/cookiejar"
	"net/http/httputil"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync/atomic"
	"time"
)

const logDir = "http_logs"

var requestCounter uint64

// LoggingTransport wraps http.RoundTripper to log all requests/responses
type LoggingTransport struct {
	Transport http.RoundTripper
}

func (t *LoggingTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	count := atomic.AddUint64(&requestCounter, 1)
	timestamp := time.Now().Format("15:04:05.000")
	filename := filepath.Join(logDir, fmt.Sprintf("%03d_%s_%s.txt", count, req.Method, sanitizeFilename(req.URL.Path)))

	var logBuf bytes.Buffer
	logBuf.WriteString(fmt.Sprintf("=== REQUEST #%d @ %s ===\n", count, timestamp))
	logBuf.WriteString(fmt.Sprintf("%s %s\n\n", req.Method, req.URL.String()))

	// Log request headers
	logBuf.WriteString("--- Request Headers ---\n")
	for name, values := range req.Header {
		for _, v := range values {
			logBuf.WriteString(fmt.Sprintf("%s: %s\n", name, v))
		}
	}

	// Log request body if present
	if req.Body != nil {
		bodyBytes, err := io.ReadAll(req.Body)
		if err == nil {
			req.Body = io.NopCloser(bytes.NewReader(bodyBytes))
			if len(bodyBytes) > 0 {
				logBuf.WriteString("\n--- Request Body ---\n")
				logBuf.WriteString(string(bodyBytes))
				logBuf.WriteString("\n")
			}
		}
	}

	// Perform the actual request
	resp, err := t.Transport.RoundTrip(req)
	if err != nil {
		logBuf.WriteString(fmt.Sprintf("\n=== ERROR ===\n%v\n", err))
		os.WriteFile(filename, logBuf.Bytes(), 0644)
		return nil, err
	}

	logBuf.WriteString(fmt.Sprintf("\n=== RESPONSE #%d ===\n", count))
	logBuf.WriteString(fmt.Sprintf("Status: %s\n\n", resp.Status))

	// Log response headers
	logBuf.WriteString("--- Response Headers ---\n")
	for name, values := range resp.Header {
		for _, v := range values {
			logBuf.WriteString(fmt.Sprintf("%s: %s\n", name, v))
		}
	}

	// Log response body
	if resp.Body != nil {
		bodyBytes, err := io.ReadAll(resp.Body)
		if err == nil {
			resp.Body = io.NopCloser(bytes.NewReader(bodyBytes))
			if len(bodyBytes) > 0 {
				logBuf.WriteString("\n--- Response Body ---\n")
				// Pretty print JSON if possible
				var prettyJSON bytes.Buffer
				if json.Indent(&prettyJSON, bodyBytes, "", "  ") == nil {
					logBuf.Write(prettyJSON.Bytes())
				} else {
					// Truncate HTML/large responses
					body := string(bodyBytes)
					if len(body) > 2000 {
						logBuf.WriteString(body[:2000])
						logBuf.WriteString("\n... [truncated] ...")
					} else {
						logBuf.WriteString(body)
					}
				}
				logBuf.WriteString("\n")
			}
		}
	}

	os.WriteFile(filename, logBuf.Bytes(), 0644)
	return resp, nil
}

func sanitizeFilename(path string) string {
	// Replace slashes and other problematic chars
	path = strings.ReplaceAll(path, "/", "_")
	path = strings.ReplaceAll(path, "?", "_")
	path = strings.ReplaceAll(path, "&", "_")
	path = strings.ReplaceAll(path, ":", "_")
	if len(path) > 50 {
		path = path[:50]
	}
	if path == "" || path == "_" {
		path = "root"
	}
	return path
}

func initLogging() error {
	// Clear and recreate log directory
	os.RemoveAll(logDir)
	return os.MkdirAll(logDir, 0755)
}

// DumpRequest is a helper for debugging
func DumpRequest(req *http.Request) {
	dump, _ := httputil.DumpRequestOut(req, true)
	fmt.Println(string(dump))
}

const (
	// Android client_id (from AccountConfigTori.java)
	androidClientID    = "6079834b9b0b741812e7e91f"
	androidRedirectURI = "fi.tori.www.6079834b9b0b741812e7e91f://login"

	// Used for SPP exchange step (spidServerClientId in Android)
	exchangeClientID = "650421cf50eeae31ecd2a2d3"

	// HMAC key for gateway signing (decoded from PRODUCTION_HMAC_KEY in AppEnvironment.java)
	hmacKey = "3b535f36-79be-424b-a6fd-116c6e69f137"

	baseURL     = "https://login.vend.fi" // Both iOS and Android use login.vend.fi for Finland
	toriBaseURL = "https://apps-gw-poc.svc.tori.fi"

	// Android SDK user agent
	androidSDKUserAgent = "user-webflows-sdk-android/5.0.0"
	// Tori Android app user agent
	toriAppUserAgent = "Tori/26.4.0 (Android 14; sdk_gphone64_arm64)"
	// Android WebView user agent (for web-based auth steps)
	userAgent = "Mozilla/5.0 (Linux; Android 14; sdk_gphone64_arm64) AppleWebKit/537.36 (KHTML, like Gecko) Version/4.0 Chrome/131.0.6778.39 Mobile Safari/537.36"
)

type AuthClient struct {
	httpClient    *http.Client
	csrfToken     string
	codeVerifier  string
	state         string
	nonce         string
	environmentID string // from CIS response, used as deviceId
}

func NewAuthClient() (*AuthClient, error) {
	jar, err := cookiejar.New(nil)
	if err != nil {
		return nil, err
	}

	return &AuthClient{
		httpClient: &http.Client{
			Jar: jar,
			Transport: &LoggingTransport{
				Transport: http.DefaultTransport,
			},
			CheckRedirect: func(req *http.Request, via []*http.Request) error {
				// Don't follow redirects automatically - we need to handle them
				return http.ErrUseLastResponse
			},
		},
		codeVerifier: generateCodeVerifier(),
	}, nil
}

func generateCodeVerifier() string {
	b := make([]byte, 32)
	rand.Read(b)
	return base64.RawURLEncoding.EncodeToString(b)
}

func generateCodeChallenge(verifier string) string {
	hash := sha256.Sum256([]byte(verifier))
	return base64.RawURLEncoding.EncodeToString(hash[:])
}

// Step 0: Start OAuth flow to set up server-side context
func (c *AuthClient) initSession() error {
	fmt.Println("=== Step 0: Initialize OAuth session ===")

	// First, get the cis-jwe cookie from CIS
	if err := c.fetchClientState(); err != nil {
		fmt.Printf("Warning: CIS call failed: %v\n", err)
	}

	// Generate state for OAuth (mimicking browser's state format)
	stateBytes := make([]byte, 8)
	rand.Read(stateBytes)
	c.state = base64.RawURLEncoding.EncodeToString(stateBytes)

	// Generate nonce
	nonceBytes := make([]byte, 8)
	rand.Read(nonceBytes)
	c.nonce = base64.RawURLEncoding.EncodeToString(nonceBytes)

	// Generate PKCE code challenge
	codeChallenge := generateCodeChallenge(c.codeVerifier)

	// Start with OAuth authorize using Android client with PKCE
	authURL := fmt.Sprintf("%s/oauth/authorize?client_id=%s&redirect_uri=%s&response_type=code&scope=openid+offline_access&state=%s&nonce=%s&prompt=select_account&code_challenge=%s&code_challenge_method=S256",
		baseURL, androidClientID, url.QueryEscape(androidRedirectURI), c.state, c.nonce, codeChallenge)

	fmt.Printf("Auth URL: %s\n", truncate(authURL, 100))

	req, err := http.NewRequest("GET", authURL, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	fmt.Printf("Status: %d\n", resp.StatusCode)
	location := resp.Header.Get("Location")
	fmt.Printf("Location: %s\n", truncate(location, 100))

	// Should redirect to login page - follow it
	if resp.StatusCode == 302 && strings.HasPrefix(location, "http") {
		return c.getLoginPage(location)
	}

	// If we got a 200, try to parse the page
	if resp.StatusCode == 200 {
		return c.parseLoginPage(resp)
	}

	// Print cookies we got anyway
	u, _ := url.Parse(baseURL)
	cookies := c.httpClient.Jar.Cookies(u)
	fmt.Printf("Cookies obtained: %d\n", len(cookies))

	return nil
}

func (c *AuthClient) getLoginPage(loginURL string) error {
	for i := 0; i < 10; i++ {
		fmt.Printf("\nGetting login page: %s\n", truncate(loginURL, 80))

		req, err := http.NewRequest("GET", loginURL, nil)
		if err != nil {
			return err
		}
		req.Header.Set("User-Agent", userAgent)
		req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8")

		resp, err := c.httpClient.Do(req)
		if err != nil {
			return err
		}

		fmt.Printf("Status: %d\n", resp.StatusCode)

		// Follow redirects
		if resp.StatusCode == 301 || resp.StatusCode == 302 || resp.StatusCode == 307 {
			location := resp.Header.Get("Location")
			resp.Body.Close()
			fmt.Printf("Redirect to: %s\n", truncate(location, 80))
			if location == "" || !strings.HasPrefix(location, "http") {
				return fmt.Errorf("invalid redirect: %s", location)
			}
			loginURL = location
			continue
		}

		defer resp.Body.Close()
		return c.parseLoginPage(resp)
	}
	return fmt.Errorf("too many redirects")
}

func (c *AuthClient) parseLoginPage(resp *http.Response) error {
	body, _ := io.ReadAll(resp.Body)

	fmt.Printf("Status: %d\n", resp.StatusCode)

	// Look for bffData div with HTML-encoded JSON
	bffRegex := regexp.MustCompile(`<div id="bffData"[^>]*>([^<]+)</div>`)
	if matches := bffRegex.FindSubmatch(body); len(matches) > 1 {
		// HTML decode the content
		htmlContent := string(matches[1])
		htmlContent = strings.ReplaceAll(htmlContent, "&quot;", `"`)
		htmlContent = strings.ReplaceAll(htmlContent, "&#x2F;", "/")
		htmlContent = strings.ReplaceAll(htmlContent, "&#x3D;", "=")
		htmlContent = strings.ReplaceAll(htmlContent, "&amp;", "&")

		// Parse JSON to get csrfToken
		var bffData struct {
			CsrfToken string `json:"csrfToken"`
		}
		if err := json.Unmarshal([]byte(htmlContent), &bffData); err == nil && bffData.CsrfToken != "" {
			c.csrfToken = bffData.CsrfToken
			fmt.Printf("Found CSRF token in bffData: %s\n", c.csrfToken)
		}
	}

	// Fallback: try other patterns
	if c.csrfToken == "" {
		patterns := []string{
			`"csrfToken"\s*:\s*"([^"]+)"`,
			`csrfToken['":\s]+=?\s*['"]([^'"]+)['"]`,
		}
		for _, pattern := range patterns {
			re := regexp.MustCompile(pattern)
			matches := re.FindSubmatch(body)
			if len(matches) > 1 {
				c.csrfToken = string(matches[1])
				fmt.Printf("Found CSRF token: %s\n", c.csrfToken)
				break
			}
		}
	}

	// Check response headers for csrf
	if c.csrfToken == "" {
		if csrf := resp.Header.Get("X-Csrf-Token"); csrf != "" {
			c.csrfToken = csrf
			fmt.Printf("Found CSRF token in header: %s\n", c.csrfToken)
		}
	}

	// Print cookies we got
	u, _ := url.Parse(baseURL)
	cookies := c.httpClient.Jar.Cookies(u)
	fmt.Printf("Cookies obtained: %d\n", len(cookies))
	for _, cookie := range cookies {
		fmt.Printf("  - %s: %s...\n", cookie.Name, truncate(cookie.Value, 30))
	}

	if c.csrfToken == "" {
		fmt.Println("WARNING: No CSRF token found!")
	}

	// Skip CIS call for web flow - browser uses pulse cookies instead
	// if err := c.fetchClientState(); err != nil {
	// 	fmt.Printf("CIS call failed: %v\n", err)
	// }

	return nil
}

func (c *AuthClient) fetchClientState() error {
	// Call CIS identify/guest to get the cis-jwe cookie (required for iOS OAuth flow)
	fmt.Println("Calling CIS identify/guest to get cis-jwe cookie...")

	// Use the actual CIS endpoint (not login.vend.fi)
	cisURL := "https://cis.m10s.io/api/v2/identify/guest"

	// Generate a device ID
	idfv := fmt.Sprintf("%08X-%04X-%04X-%04X-%012X",
		rand.Uint32(), rand.Uint32()&0xFFFF, rand.Uint32()&0xFFFF,
		rand.Uint32()&0xFFFF, rand.Uint64()&0xFFFFFFFFFFFF)

	// First call without jwe to get initial token
	payload := fmt.Sprintf(`{"trackerType":"iOS","idfv":"%s","includeAdvertising":true,"clientId":"torifi","trackerVersion":"11.4.0","vendors":["delta","adform"]}`, idfv)

	req, err := http.NewRequest("POST", cisURL, strings.NewReader(payload))
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", "iOSPulseTracker/11.4.0 (iPhone 15 Pro; iOS 26.1) AppName/Tori ClientId/torifi")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Origin", "null")
	req.Header.Set("Accept-Language", "en-US,en;q=0.9")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	fmt.Printf("CIS identify status: %d\n", resp.StatusCode)
	fmt.Printf("CIS response: %s\n", truncate(string(body), 300))

	// Check if cis-jwe cookie was set
	cisM10sURL, _ := url.Parse("https://cis.m10s.io")
	cookies := c.httpClient.Jar.Cookies(cisM10sURL)
	for _, cookie := range cookies {
		if cookie.Name == "cis-jwe" {
			fmt.Printf("Got cis-jwe cookie from cis.m10s.io: %s...\n", truncate(cookie.Value, 50))
		}
	}

	// Also check login.vend.fi domain
	vendURL, _ := url.Parse(baseURL)
	vendCookies := c.httpClient.Jar.Cookies(vendURL)
	for _, cookie := range vendCookies {
		if cookie.Name == "cis-jwe" {
			fmt.Printf("Got cis-jwe cookie from login.vend.fi: %s...\n", truncate(cookie.Value, 50))
		}
	}

	// Parse response to get the jwe token and environmentId
	var result struct {
		Data struct {
			JWE           string `json:"jwe"`
			EnvironmentID string `json:"environmentId"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &result); err == nil {
		if result.Data.JWE != "" {
			fmt.Printf("Got JWE token from response: %s...\n", truncate(result.Data.JWE, 50))
			// Set this cookie for login.vend.fi
			vendURL, _ := url.Parse(baseURL)
			c.httpClient.Jar.SetCookies(vendURL, []*http.Cookie{
				{Name: "cis-jwe", Value: result.Data.JWE, Path: "/", Secure: true, HttpOnly: true},
			})
		}
		if result.Data.EnvironmentID != "" {
			c.environmentID = result.Data.EnvironmentID
			fmt.Printf("Got environmentId: %s\n", c.environmentID)
		}
	}

	return nil
}

func (c *AuthClient) fetchCSRFFromConfig() error {
	// Try the config endpoint that might return CSRF
	configURL := fmt.Sprintf("%s/authn/api/config?client_id=%s", baseURL, androidClientID)

	req, err := http.NewRequest("GET", configURL, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("Accept", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	fmt.Printf("Config status: %d\n", resp.StatusCode)
	fmt.Printf("Config response: %s\n", truncate(string(body), 500))

	// Check for X-Csrf-Token in response header
	if csrf := resp.Header.Get("X-Csrf-Token"); csrf != "" {
		c.csrfToken = csrf
		fmt.Printf("Found CSRF token in config header: %s\n", c.csrfToken)
	}

	// Parse JSON for csrfToken
	var config map[string]interface{}
	if err := json.Unmarshal(body, &config); err == nil {
		if csrf, ok := config["csrfToken"].(string); ok {
			c.csrfToken = csrf
			fmt.Printf("Found CSRF token in config: %s\n", c.csrfToken)
		}
	}

	return nil
}

func (c *AuthClient) followRedirect(location string) error {
	fmt.Printf("\nFollowing redirect to: %s\n", truncate(location, 80))

	req, err := http.NewRequest("GET", location, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)

	fmt.Printf("Status: %d\n", resp.StatusCode)

	// Try to find CSRF token
	csrfRegex := regexp.MustCompile(`csrfToken['":\s]+=?\s*['"]([^'"]+)['"]`)
	matches := csrfRegex.FindSubmatch(body)
	if len(matches) > 1 {
		c.csrfToken = string(matches[1])
		fmt.Printf("Found CSRF token: %s\n", c.csrfToken)
	}

	// Look for _csrf input field
	inputRegex := regexp.MustCompile(`name=["']_csrf["'][^>]+value=["']([^"']+)["']`)
	matches = inputRegex.FindSubmatch(body)
	if len(matches) > 1 {
		c.csrfToken = string(matches[1])
		fmt.Printf("Found CSRF in form: %s\n", c.csrfToken)
	}

	// Print cookies
	u, _ := url.Parse(baseURL)
	cookies := c.httpClient.Jar.Cookies(u)
	fmt.Printf("Total cookies now: %d\n", len(cookies))

	return nil
}

// Step 1: Check email status
func (c *AuthClient) checkEmailStatus(email string) error {
	fmt.Println("\n=== Step 1: Check email status ===")

	endpoint := fmt.Sprintf("%s/authn/api/identity/email-status?client_id=%s", baseURL, androidClientID)

	// iOS device fingerprint to avoid triggering MFA
	iOSDeviceData := `{"fonts":["Arial","Arial Hebrew","Arial Rounded MT Bold","Courier","Courier New","Georgia","Helvetica","Helvetica Neue","Impact","Monaco","Palatino","Times","Times New Roman","Trebuchet MS","Verdana"],"hasLiedBrowser":"0","hasLiedOs":"0","platform":"iOS","plugins":[],"userAgent":"Mobile Safari","userAgentVersion":"26.1"}`

	payload := map[string]string{
		"email":      email,
		"deviceData": iOSDeviceData,
	}
	jsonBody, _ := json.Marshal(payload)

	req, err := http.NewRequest("POST", endpoint, bytes.NewReader(jsonBody))
	if err != nil {
		return err
	}

	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Origin", baseURL)
	req.Header.Set("Sec-Fetch-Site", "same-origin")
	req.Header.Set("Sec-Fetch-Mode", "cors")
	if c.csrfToken != "" {
		req.Header.Set("X-Csrf-Token", c.csrfToken)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)

	fmt.Printf("Status: %d\n", resp.StatusCode)
	fmt.Printf("Response: %s\n", string(body))

	// Check if CSRF token is in response header
	if csrf := resp.Header.Get("X-Csrf-Token"); csrf != "" {
		c.csrfToken = csrf
		fmt.Printf("Got CSRF from response: %s\n", c.csrfToken)
	}

	// Check session-id header
	if sid := resp.Header.Get("X-Session-Id"); sid != "" {
		fmt.Printf("Session ID: %s\n", truncate(sid, 50))
	}

	if resp.StatusCode != 200 {
		return fmt.Errorf("email-status failed with status %d", resp.StatusCode)
	}

	return nil
}

// Step 2: Start passwordless authentication
func (c *AuthClient) startPasswordless(email string) (string, error) {
	fmt.Println("\n=== Step 2: Start passwordless auth ===")

	endpoint := fmt.Sprintf("%s/authn/api/identity/passwordless-start/?client_id=%s", baseURL, androidClientID)

	// iOS device fingerprint to avoid triggering MFA
	deviceData := `{"fonts":["Arial","Arial Hebrew","Arial Rounded MT Bold","Courier","Courier New","Georgia","Helvetica","Helvetica Neue","Impact","Monaco","Palatino","Times","Times New Roman","Trebuchet MS","Verdana"],"hasLiedBrowser":"0","hasLiedOs":"0","platform":"iOS","plugins":[],"userAgent":"Mobile Safari","userAgentVersion":"26.1"}`

	data := url.Values{}
	data.Set("connection", "email")
	data.Set("email", email)
	data.Set("deviceData", deviceData)

	// Debug: print cookies being sent
	u, _ := url.Parse(endpoint)
	fmt.Printf("Cookies being sent: %d\n", len(c.httpClient.Jar.Cookies(u)))
	for _, cookie := range c.httpClient.Jar.Cookies(u) {
		fmt.Printf("  - %s\n", cookie.Name)
	}

	req, err := http.NewRequest("POST", endpoint, strings.NewReader(data.Encode()))
	if err != nil {
		return "", err
	}

	// Add Referer header (might be required)
	req.Header.Set("Referer", baseURL+"/authn/")

	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded; charset=utf-8")
	req.Header.Set("Origin", baseURL)
	req.Header.Set("Sec-Fetch-Site", "same-origin")
	req.Header.Set("Sec-Fetch-Mode", "cors")
	if c.csrfToken != "" {
		req.Header.Set("X-Csrf-Token", c.csrfToken)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)

	fmt.Printf("Status: %d\n", resp.StatusCode)
	fmt.Printf("Response: %s\n", string(body))

	if resp.StatusCode != 200 {
		return "", fmt.Errorf("passwordless-start failed with status %d", resp.StatusCode)
	}

	// Parse response to get token
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

// Step 3: Submit email code
// Returns (mfaID, mfaRequired, error)
func (c *AuthClient) submitEmailCode(code, token string) (string, bool, error) {
	fmt.Println("\n=== Step 3: Submit email code ===")

	endpoint := fmt.Sprintf("%s/authn/api/identity/passwordless-code/?client_id=%s", baseURL, androidClientID)

	data := url.Values{}
	data.Set("code", code)
	data.Set("remember", "false")
	data.Set("connection", "email")
	data.Set("passwordlessToken", token)

	req, err := http.NewRequest("POST", endpoint, strings.NewReader(data.Encode()))
	if err != nil {
		return "", false, err
	}

	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded; charset=utf-8")
	req.Header.Set("Origin", baseURL)
	if c.csrfToken != "" {
		req.Header.Set("X-Csrf-Token", c.csrfToken)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", false, err
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)

	fmt.Printf("Status: %d\n", resp.StatusCode)
	fmt.Printf("Response: %s\n", string(body))

	if resp.StatusCode != 200 {
		return "", false, fmt.Errorf("passwordless-code failed with status %d", resp.StatusCode)
	}

	// Check if MFA was skipped (success link present)
	if strings.Contains(string(body), "/success") {
		fmt.Println("MFA skipped! Proceeding directly to finish identity.")
		return "", false, nil
	}

	// Parse response to get MFA ID
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
		// No SMS ID means MFA not required
		fmt.Println("No MFA ID in response, MFA not required.")
		return "", false, nil
	}

	return result.Data.Attributes.SMS.ID, true, nil
}

// Step 4: Request SMS code
func (c *AuthClient) requestSMSCode(mfaID string) (string, error) {
	fmt.Println("\n=== Step 4: Request SMS code ===")

	endpoint := fmt.Sprintf("%s/authn/api/auth/assertion/?client_id=%s", baseURL, androidClientID)

	payload := map[string]string{
		"method": "mfa/sms",
		"mfaId":  mfaID,
	}
	jsonBody, _ := json.Marshal(payload)

	req, err := http.NewRequest("POST", endpoint, bytes.NewReader(jsonBody))
	if err != nil {
		return "", err
	}

	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Origin", baseURL)
	if c.csrfToken != "" {
		req.Header.Set("X-Csrf-Token", c.csrfToken)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)

	fmt.Printf("Status: %d\n", resp.StatusCode)
	fmt.Printf("Response: %s\n", string(body))

	if resp.StatusCode != 200 {
		return "", fmt.Errorf("auth-assertion failed with status %d", resp.StatusCode)
	}

	// Parse response to get nonce
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

// Step 5: Submit SMS code
func (c *AuthClient) submitSMSCode(smsCode, nonce, mfaID string) error {
	fmt.Println("\n=== Step 5: Submit SMS code ===")

	endpoint := fmt.Sprintf("%s/authn/api/auth/assertion/sms?client_id=%s", baseURL, androidClientID)

	data := url.Values{}
	data.Set("nonce", nonce)
	data.Set("mfaId", mfaID)
	data.Set("secret", smsCode)

	req, err := http.NewRequest("POST", endpoint, strings.NewReader(data.Encode()))
	if err != nil {
		return err
	}

	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded; charset=utf-8")
	req.Header.Set("Origin", baseURL)
	if c.csrfToken != "" {
		req.Header.Set("X-Csrf-Token", c.csrfToken)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)

	fmt.Printf("Status: %d\n", resp.StatusCode)
	fmt.Printf("Response: %s\n", string(body))

	if resp.StatusCode != 200 {
		return fmt.Errorf("auth-assertion-sms failed with status %d", resp.StatusCode)
	}

	return nil
}

// Step 6: Finish identity - should now redirect with OAuth code since we started with /authorize
func (c *AuthClient) finishIdentity() (string, error) {
	fmt.Println("\n=== Step 6: Finish identity (expecting OAuth redirect) ===")

	endpoint := fmt.Sprintf("%s/authn/identity/finish/?client_id=%s", baseURL, androidClientID)

	data := url.Values{}
	data.Set("deviceData", `{"fonts":["Arial"],"platform":"macOS"}`)
	data.Set("_csrf", c.csrfToken)
	data.Set("remember", "true")

	req, err := http.NewRequest("POST", endpoint, strings.NewReader(data.Encode()))
	if err != nil {
		return "", err
	}

	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8")
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Origin", baseURL)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	fmt.Printf("Status: %d\n", resp.StatusCode)
	location := resp.Header.Get("Location")
	fmt.Printf("Location: %s\n", truncate(location, 100))

	if location == "" {
		body, _ := io.ReadAll(resp.Body)
		fmt.Printf("Body: %s\n", truncate(string(body), 500))
		return "", fmt.Errorf("no redirect from identity/finish")
	}

	// Follow redirects to get the OAuth code
	return c.followOAuthRedirects(location)
}

func (c *AuthClient) followOAuthRedirects(location string) (string, error) {
	fmt.Println("\n=== Following OAuth redirects ===")

	for i := 0; i < 10; i++ {
		fmt.Printf("Redirect %d: %s\n", i+1, truncate(location, 100))

		// Check if this is a callback URL with the code
		if strings.Contains(location, "code=") {
			// Parse the code from the URL
			u, err := url.Parse(location)
			if err != nil {
				return "", err
			}
			code := u.Query().Get("code")
			if code != "" {
				fmt.Printf("Got OAuth code: %s\n", truncate(code, 30))
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
				fmt.Printf("Got OAuth code: %s\n", truncate(code, 30))
				return code, nil
			}
		}

		// Don't follow non-http redirects
		if !strings.HasPrefix(location, "http") {
			return "", fmt.Errorf("non-http redirect: %s", truncate(location, 50))
		}

		req, err := http.NewRequest("GET", location, nil)
		if err != nil {
			return "", err
		}
		req.Header.Set("User-Agent", userAgent)
		req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8")

		resp, err := c.httpClient.Do(req)
		if err != nil {
			return "", err
		}
		resp.Body.Close()

		fmt.Printf("  Status: %d\n", resp.StatusCode)

		newLocation := resp.Header.Get("Location")
		if newLocation == "" {
			return "", fmt.Errorf("redirect chain ended without code at %s", truncate(location, 50))
		}
		location = newLocation
	}

	return "", fmt.Errorf("too many redirects")
}

// Step 7: Exchange OAuth code for tokens using PKCE
func (c *AuthClient) exchangeCodeForTokens(code string) (string, string, error) {
	fmt.Println("\n=== Step 7: Exchange code for tokens (PKCE) ===")

	endpoint := fmt.Sprintf("%s/oauth/token", baseURL)

	data := url.Values{}
	data.Set("grant_type", "authorization_code")
	data.Set("client_id", androidClientID)
	data.Set("code", code)
	data.Set("redirect_uri", androidRedirectURI)
	data.Set("code_verifier", c.codeVerifier)

	req, err := http.NewRequest("POST", endpoint, strings.NewReader(data.Encode()))
	if err != nil {
		return "", "", err
	}

	req.Header.Set("User-Agent", androidSDKUserAgent)
	req.Header.Set("Accept", "*/*")
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("X-Oidc", "v1")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", "", err
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)

	fmt.Printf("Status: %d\n", resp.StatusCode)
	fmt.Printf("Response: %s\n", truncate(string(body), 300))

	if resp.StatusCode != 200 {
		return "", "", fmt.Errorf("token exchange failed with status %d: %s", resp.StatusCode, string(body))
	}

	var result struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		IDToken      string `json:"id_token"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return "", "", err
	}

	fmt.Printf("Got access_token: %s\n", truncate(result.AccessToken, 50))
	fmt.Printf("Got id_token: %s\n", truncate(result.IDToken, 50))

	return result.AccessToken, result.IDToken, nil
}

// Step 8: Exchange for SPP code
func (c *AuthClient) exchangeForSPPCode(accessToken string) (string, error) {
	fmt.Println("\n=== Step 8: Exchange for SPP code ===")

	endpoint := fmt.Sprintf("%s/api/2/oauth/exchange", baseURL)

	data := url.Values{}
	data.Set("clientId", exchangeClientID)
	data.Set("type", "code")

	req, err := http.NewRequest("POST", endpoint, strings.NewReader(data.Encode()))
	if err != nil {
		return "", err
	}

	req.Header.Set("User-Agent", "AccountSDKIOSWeb/7.0.2 (iPhone; iOS 26.1)")
	req.Header.Set("Accept", "*/*")
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Authorization", "Bearer "+accessToken)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)

	fmt.Printf("Status: %d\n", resp.StatusCode)
	fmt.Printf("Response: %s\n", string(body))

	if resp.StatusCode != 200 {
		return "", fmt.Errorf("SPP exchange failed with status %d", resp.StatusCode)
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

// Step 9: Final Tori login
func (c *AuthClient) toriLogin(idToken, spidCode string) (string, string, error) {
	fmt.Println("\n=== Step 9: Final Tori login ===")

	endpoint := fmt.Sprintf("%s/public/login", toriBaseURL)

	// Android app uses OAuth clientId as deviceId (not a random UUID!)
	// This was discovered by decompiling the Android app - see Account.java:816-857
	// The deviceId parameter in AuthorizeCodeData is set to account.clientId
	deviceID := androidClientID
	fmt.Printf("Using deviceId (iOS clientId): %s\n", deviceID)

	abTestDeviceID := fmt.Sprintf("%08X-%04X-%04X-%04X-%012X",
		rand.Uint32(), rand.Uint32()&0xFFFF, rand.Uint32()&0xFFFF,
		rand.Uint32()&0xFFFF, rand.Uint64()&0xFFFFFFFFFFFF)

	payload := map[string]string{
		"deviceId": deviceID,
		"idToken":  idToken,
		"spidCode": spidCode,
	}
	jsonBody, _ := json.Marshal(payload)

	req, err := http.NewRequest("POST", endpoint, bytes.NewReader(jsonBody))
	if err != nil {
		return "", "", err
	}

	// Use a fresh client without cookies for this request
	// The iOS app doesn't send cookies to the gateway
	cleanClient := &http.Client{
		Transport: &LoggingTransport{Transport: http.DefaultTransport},
	}

	// Standard headers
	req.Header.Set("User-Agent", toriAppUserAgent)
	req.Header.Set("Accept", "application/json; charset=UTF-8")
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept-Language", "en-US,en;q=0.9")

	// Calculate HMAC signature for gateway
	gwService := "LOGIN-SERVER-AUTH"
	gwKey := calculateGatewayHMAC("POST", "/public/login", "", gwService, jsonBody)
	fmt.Printf("Calculated HMAC: %s\n", truncate(gwKey, 50))

	// Gateway headers (Android)
	req.Header.Set("finn-device-info", "Android, mobile")
	req.Header.Set("finn-gw-service", gwService)
	req.Header.Set("finn-gw-key", gwKey)
	req.Header.Set("finn-app-installation-id", "fQ6pfn5oxTK")

	// NMP headers (Android)
	req.Header.Set("x-nmp-os-name", "Android")
	req.Header.Set("x-nmp-app-version-name", "26.4.0")
	req.Header.Set("x-nmp-app-brand", "Tori")
	req.Header.Set("x-nmp-os-version", "14")
	req.Header.Set("x-nmp-device", "sdk_gphone64_arm64")
	req.Header.Set("x-nmp-app-build-number", "26357")
	req.Header.Set("buildnumber", "26357")
	req.Header.Set("ab-test-device-id", abTestDeviceID)

	// CMP consent headers (required for EU services)
	req.Header.Set("cmp-analytics", "1")
	req.Header.Set("cmp-personalisation", "1")
	req.Header.Set("cmp-marketing", "1")
	req.Header.Set("cmp-advertising", "1")

	resp, err := cleanClient.Do(req)
	if err != nil {
		return "", "", err
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)

	fmt.Printf("Status: %d\n", resp.StatusCode)
	fmt.Printf("Response: %s\n", string(body))

	if resp.StatusCode != 200 && resp.StatusCode != 201 {
		return "", "", fmt.Errorf("tori login failed with status %d", resp.StatusCode)
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

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}

// calculateGatewayHMAC calculates the FINN-GW-KEY header value
// Format: HMAC-SHA512(key, "METHOD;PATH?QUERY;SERVICE;BODY") → base64
func calculateGatewayHMAC(method, path, query, service string, body []byte) string {
	// Build the message: METHOD;PATH?QUERY;SERVICE;BODY
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

	// Calculate HMAC-SHA512
	h := hmac.New(sha512.New, []byte(hmacKey))
	h.Write(msg.Bytes())
	signature := h.Sum(nil)

	// Return base64 encoded
	return base64.StdEncoding.EncodeToString(signature)
}

func prompt(message string) string {
	fmt.Print(message)
	reader := bufio.NewReader(os.Stdin)
	input, _ := reader.ReadString('\n')
	return strings.TrimSpace(input)
}

func main() {
	// Check for --test flag
	if len(os.Args) > 1 && os.Args[1] == "--test" {
		runTestMode()
		return
	}

	fmt.Println("=== Tori.fi Login Flow Test ===")
	fmt.Println()

	// Initialize logging - clears and recreates http_logs directory
	if err := initLogging(); err != nil {
		fmt.Printf("Failed to init logging: %v\n", err)
		return
	}
	fmt.Printf("HTTP logs will be saved to: %s/\n\n", logDir)

	client, err := NewAuthClient()
	if err != nil {
		fmt.Printf("Failed to create client: %v\n", err)
		return
	}

	// Step 0: Initialize session
	if err := client.initSession(); err != nil {
		fmt.Printf("Failed to init session: %v\n", err)
		return
	}

	// Get email from user
	email := prompt("\nEnter email: ")
	if email == "" {
		fmt.Println("Email required")
		return
	}

	// Step 1: Check email status
	if err := client.checkEmailStatus(email); err != nil {
		fmt.Printf("Failed to check email status: %v\n", err)
		return
	}

	// Step 2: Start passwordless auth
	token, err := client.startPasswordless(email)
	if err != nil {
		fmt.Printf("Failed to start passwordless: %v\n", err)
		return
	}
	fmt.Printf("Got passwordless token: %s\n", truncate(token, 30))

	// Step 3: Get email code from user
	emailCode := prompt("\nEnter code from email: ")
	if emailCode == "" {
		fmt.Println("Code required")
		return
	}

	mfaID, mfaRequired, err := client.submitEmailCode(emailCode, token)
	if err != nil {
		fmt.Printf("Failed to submit email code: %v\n", err)
		return
	}

	// Only do MFA steps if required
	if mfaRequired {
		fmt.Printf("Got MFA ID: %s\n", mfaID)

		// Step 4: Request SMS
		nonce, err := client.requestSMSCode(mfaID)
		if err != nil {
			fmt.Printf("Failed to request SMS: %v\n", err)
			return
		}
		fmt.Printf("Got nonce: %s\n", truncate(nonce, 30))

		// Step 5: Get SMS code from user
		smsCode := prompt("\nEnter code from SMS: ")
		if smsCode == "" {
			fmt.Println("SMS code required")
			return
		}

		if err := client.submitSMSCode(smsCode, nonce, mfaID); err != nil {
			fmt.Printf("Failed to submit SMS code: %v\n", err)
			return
		}
	} else {
		fmt.Println("MFA not required, skipping SMS steps")
	}

	// Step 6: Finish and get OAuth code
	oauthCode, err := client.finishIdentity()
	if err != nil {
		fmt.Printf("Failed to finish identity: %v\n", err)
		return
	}

	// Step 7: Exchange code for tokens (PKCE)
	accessToken, idToken, err := client.exchangeCodeForTokens(oauthCode)
	if err != nil {
		fmt.Printf("Failed to exchange tokens: %v\n", err)
		return
	}

	// Step 8: Exchange for SPP code
	spidCode, err := client.exchangeForSPPCode(accessToken)
	if err != nil {
		fmt.Printf("Failed to get SPP code: %v\n", err)
		return
	}

	// Step 9: Final Tori login
	userID, bearerToken, err := client.toriLogin(idToken, spidCode)
	if err != nil {
		fmt.Printf("Failed Tori login: %v\n", err)
		return
	}

	fmt.Println("\n=== SUCCESS ===")
	fmt.Printf("User ID: %s\n", userID)
	fmt.Printf("Bearer Token: %s\n", bearerToken)

	// Test that the token works
	if err := testBearerToken(bearerToken); err != nil {
		fmt.Printf("Token test failed: %v\n", err)
	}

	// Save token to file
	tokenData := map[string]string{
		"user_id":      userID,
		"bearer_token": bearerToken,
		"access_token": accessToken,
		"id_token":     idToken,
	}
	tokenJSON, _ := json.MarshalIndent(tokenData, "", "  ")
	if err := os.WriteFile("auth_tokens.json", tokenJSON, 0600); err != nil {
		fmt.Printf("Failed to save tokens: %v\n", err)
	} else {
		fmt.Println("Tokens saved to auth_tokens.json")
	}
}

// Step 10: Test the bearer token works
func testBearerToken(bearerToken string) error {
	fmt.Println("\n=== Step 10: Test bearer token ===")

	// Use public/auth endpoint - returns 204 on success
	endpoint := fmt.Sprintf("%s/public/auth", toriBaseURL)

	req, err := http.NewRequest("GET", endpoint, nil)
	if err != nil {
		return err
	}

	gwService := "LOGIN_SERVER"
	gwKey := calculateGatewayHMAC("GET", "/public/auth", "", gwService, nil)

	req.Header.Set("User-Agent", toriAppUserAgent)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Authorization", "Bearer "+bearerToken)
	req.Header.Set("finn-gw-service", gwService)
	req.Header.Set("finn-gw-key", gwKey)

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)

	fmt.Printf("Status: %d\n", resp.StatusCode)
	if len(body) > 0 {
		fmt.Printf("Response: %s\n", string(body))
	}

	if resp.StatusCode == 204 || resp.StatusCode == 200 {
		fmt.Println("✓ Bearer token is valid!")
		return nil
	}

	return fmt.Errorf("token validation failed with status %d", resp.StatusCode)
}

// testAPIs makes test requests to various endpoints
func testAPIs(bearerToken string) {
	fmt.Println("\n=== Testing API endpoints ===")

	client := &http.Client{}

	// Test 1: Get user profile
	fmt.Println("\n--- GET /v2/me (user profile) ---")
	gwService := "TRUST-PROFILE-API"
	gwKey := calculateGatewayHMAC("GET", "/v2/me", "", gwService, nil)

	req, _ := http.NewRequest("GET", toriBaseURL+"/v2/me", nil)
	req.Header.Set("User-Agent", toriAppUserAgent)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Authorization", "Bearer "+bearerToken)
	req.Header.Set("finn-gw-service", gwService)
	req.Header.Set("finn-gw-key", gwKey)
	req.Header.Set("X-Client-Id", androidClientID)

	resp, err := client.Do(req)
	if err != nil {
		fmt.Printf("Error: %v\n", err)
	} else {
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		fmt.Printf("Status: %d\n", resp.StatusCode)
		var prettyJSON bytes.Buffer
		if json.Indent(&prettyJSON, body, "", "  ") == nil {
			fmt.Printf("Response:\n%s\n", prettyJSON.String())
		} else {
			fmt.Printf("Response: %s\n", string(body))
		}
	}

	// Test 2: Get user's ads/listings
	fmt.Println("\n--- GET /recommerce/api/v1.2/users/me/items (my listings) ---")
	gwService2 := "RECOMMERCE"
	gwKey2 := calculateGatewayHMAC("GET", "/recommerce/api/v1.2/users/me/items", "", gwService2, nil)

	req2, _ := http.NewRequest("GET", toriBaseURL+"/recommerce/api/v1.2/users/me/items", nil)
	req2.Header.Set("User-Agent", toriAppUserAgent)
	req2.Header.Set("Accept", "application/json")
	req2.Header.Set("Authorization", "Bearer "+bearerToken)
	req2.Header.Set("finn-gw-service", gwService2)
	req2.Header.Set("finn-gw-key", gwKey2)

	resp2, err := client.Do(req2)
	if err != nil {
		fmt.Printf("Error: %v\n", err)
	} else {
		body, _ := io.ReadAll(resp2.Body)
		resp2.Body.Close()
		fmt.Printf("Status: %d\n", resp2.StatusCode)
		var prettyJSON bytes.Buffer
		if json.Indent(&prettyJSON, body, "", "  ") == nil {
			fmt.Printf("Response:\n%s\n", prettyJSON.String())
		} else {
			fmt.Printf("Response: %s\n", truncate(string(body), 500))
		}
	}
}

// runTestMode reads saved tokens and tests API endpoints
func runTestMode() {
	fmt.Println("=== Test Mode: Using saved tokens ===")
	fmt.Println()

	// Read saved tokens
	data, err := os.ReadFile("auth_tokens.json")
	if err != nil {
		fmt.Printf("Failed to read auth_tokens.json: %v\n", err)
		fmt.Println("Run without --test first to authenticate")
		return
	}

	var tokens struct {
		BearerToken string `json:"bearer_token"`
		UserID      string `json:"user_id"`
	}
	if err := json.Unmarshal(data, &tokens); err != nil {
		fmt.Printf("Failed to parse tokens: %v\n", err)
		return
	}

	fmt.Printf("User ID: %s\n", tokens.UserID)
	fmt.Printf("Bearer Token: %s\n", truncate(tokens.BearerToken, 30))

	// Test the token is still valid
	if err := testBearerToken(tokens.BearerToken); err != nil {
		fmt.Printf("Token validation failed: %v\n", err)
		return
	}

	// Test various APIs
	testAPIs(tokens.BearerToken)
}
