package auth

import (
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
)

// initOAuthSession starts the OAuth authorize flow with PKCE.
func (a *Authenticator) initOAuthSession() error {
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

// finishIdentity completes the identity verification and gets the OAuth code.
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

// followOAuthRedirects follows the redirect chain until finding the OAuth code.
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

// exchangeCodeForTokens exchanges the OAuth code for access tokens.
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
	if err := jsonUnmarshal(body, &result); err != nil {
		return "", "", "", err
	}

	return result.AccessToken, result.RefreshToken, result.IDToken, nil
}

// exchangeForSPPCode exchanges access token for SPP code (Schibsted Platform code).
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
	if err := jsonUnmarshal(body, &result); err != nil {
		return "", err
	}

	return result.Data.Code, nil
}
