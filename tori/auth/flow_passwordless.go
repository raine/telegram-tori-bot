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

// checkEmailStatus verifies the email can be used for passwordless login.
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

// startPasswordless initiates the passwordless email login flow.
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

// submitEmailCode verifies the email code and returns MFA requirements.
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
