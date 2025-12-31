package auth

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha512"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
)

// CalculateGatewayHMAC calculates the HMAC signature for the finn-gw-key header.
// The signature is calculated from: METHOD;PATH?QUERY;SERVICE;BODY
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
	return base64.StdEncoding.EncodeToString(h.Sum(nil))
}

// toriLogin performs the final Tori login using the SPP code.
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
