package auth

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
)

// requestSMSCode sends an SMS verification code.
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

// submitSMSCode verifies the SMS code.
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
