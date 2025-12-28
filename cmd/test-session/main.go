package main

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha512"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
)

const (
	tokenFile   = "auth_tokens.json"
	gatewayBase = "https://apps-gw-poc.svc.tori.fi"
	adinputBase = "https://apps-adinput.svc.tori.fi"
	hmacKey     = "3b535f36-79be-424b-a6fd-116c6e69f137"
	clientID    = "6079834b9b0b741812e7e91f"
)

func main() {
	fmt.Println("=== Testing Tori Session ===")
	fmt.Println()

	// Load tokens from file
	tokens, err := loadTokens()
	if err != nil {
		fmt.Printf("Failed to load tokens: %v\n", err)
		fmt.Println("\nRun login-test first to generate auth_tokens.json")
		os.Exit(1)
	}

	bearerToken := tokens["bearer_token"]
	if bearerToken == "" {
		fmt.Println("No bearer_token found in auth_tokens.json")
		os.Exit(1)
	}

	fmt.Printf("Loaded tokens from %s\n", tokenFile)
	fmt.Printf("User ID: %s\n\n", tokens["user_id"])

	// The bearer token is opaque, not a JWT - skip decoding
	fmt.Printf("Bearer Token: %s...\n\n", bearerToken[:min(50, len(bearerToken))])

	// Test endpoints: path, service
	endpoints := []struct {
		path    string
		service string
	}{
		{"/public/auth", "LOGIN_SERVER"},
		{"/v2/me", "TRUST-PROFILE-API"},
	}

	for _, ep := range endpoints {
		fmt.Printf("\n--- GET %s (%s) ---\n", ep.path, ep.service)
		testEndpoint("GET", ep.path, ep.service, bearerToken)
	}

	// Test creating a new ad draft
	fmt.Printf("\n\n=== Testing Ad Creation ===\n")
	testCreateDraftAd(bearerToken)
}

func loadTokens() (map[string]string, error) {
	data, err := os.ReadFile(tokenFile)
	if err != nil {
		return nil, err
	}

	var tokens map[string]string
	if err := json.Unmarshal(data, &tokens); err != nil {
		return nil, err
	}

	return tokens, nil
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func decodeJWT(jwt string) {
	parts := strings.Split(jwt, ".")
	if len(parts) != 3 {
		fmt.Println("Invalid JWT format")
		return
	}

	payload := parts[1]
	switch len(payload) % 4 {
	case 2:
		payload += "=="
	case 3:
		payload += "="
	}

	decoded, err := base64.URLEncoding.DecodeString(payload)
	if err != nil {
		decoded, err = base64.StdEncoding.DecodeString(payload)
		if err != nil {
			fmt.Printf("Failed to decode: %v\n", err)
			return
		}
	}

	var prettyJSON bytes.Buffer
	if json.Indent(&prettyJSON, decoded, "", "  ") == nil {
		fmt.Println(prettyJSON.String())
	} else {
		fmt.Println(string(decoded))
	}
}

// calculateHMAC calculates the FINN-GW-KEY header value
func calculateHMAC(method, path, service string, body []byte) string {
	var msg bytes.Buffer
	msg.WriteString(strings.ToUpper(method))
	msg.WriteString(";")
	if path != "" && path != "/" {
		msg.WriteString(path)
	}
	msg.WriteString(";")
	msg.WriteString(service)
	msg.WriteString(";")
	if len(body) > 0 {
		msg.Write(body)
	}

	h := hmac.New(sha512.New, []byte(hmacKey))
	h.Write(msg.Bytes())
	return base64.StdEncoding.EncodeToString(h.Sum(nil))
}

func testEndpoint(method, path, service, bearerToken string) {
	url := gatewayBase + path
	gwKey := calculateHMAC(method, path, service, nil)

	req, err := http.NewRequest(method, url, nil)
	if err != nil {
		fmt.Printf("Error creating request: %v\n", err)
		return
	}

	// Android app headers
	req.Header.Set("User-Agent", "Tori/26.4.0 (Android 14; sdk_gphone64_arm64)")
	req.Header.Set("Accept", "application/json; charset=UTF-8")
	req.Header.Set("Accept-Language", "en-US,en;q=0.9")

	// Gateway headers with calculated HMAC
	req.Header.Set("finn-device-info", "Android, mobile")
	req.Header.Set("finn-gw-service", service)
	req.Header.Set("finn-gw-key", gwKey)
	req.Header.Set("finn-app-installation-id", "fQ6pfn5oxTK")

	// NMP headers
	req.Header.Set("x-nmp-os-name", "Android")
	req.Header.Set("x-nmp-os-version", "14")
	req.Header.Set("x-nmp-app-version-name", "26.4.0")
	req.Header.Set("x-nmp-app-build-number", "26357")
	req.Header.Set("x-nmp-app-brand", "Tori")
	req.Header.Set("x-nmp-device", "sdk_gphone64_arm64")

	// X-Client-Id for some endpoints
	req.Header.Set("X-Client-Id", clientID)

	// Auth
	req.Header.Set("Authorization", "Bearer "+bearerToken)

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		fmt.Printf("Error: %v\n", err)
		return
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)

	fmt.Printf("Status: %d\n", resp.StatusCode)

	var prettyJSON bytes.Buffer
	if json.Indent(&prettyJSON, body, "", "  ") == nil {
		fmt.Printf("Response:\n%s\n", prettyJSON.String())
	} else {
		fmt.Printf("Response: %s\n", string(body))
	}
}

// testCreateDraftAd tests creating a new ad draft via the adinput API
func testCreateDraftAd(bearerToken string) {
	path := "/adinput/ad/withModel/recommerce"
	service := "APPS-ADINPUT"
	url := adinputBase + path

	gwKey := calculateHMAC("POST", path, service, nil)

	req, err := http.NewRequest("POST", url, nil)
	if err != nil {
		fmt.Printf("Error creating request: %v\n", err)
		return
	}

	// Headers from captured iOS request
	req.Header.Set("User-Agent", "ToriApp_iOS/26.4.0-26357 (iPhone; CPU iPhone OS 26.1 like Mac OS X)")
	req.Header.Set("Accept", "application/json; charset=UTF-8")
	req.Header.Set("Accept-Language", "en-US,en;q=0.9")
	req.Header.Set("Content-Length", "0")

	// Gateway headers
	req.Header.Set("finn-device-info", "iOS, mobile")
	req.Header.Set("finn-gw-service", service)
	req.Header.Set("finn-gw-key", gwKey)
	req.Header.Set("finn-app-installation-id", "krPfNZ6QYmB")

	// NMP headers
	req.Header.Set("x-nmp-os-name", "iOS")
	req.Header.Set("x-nmp-os-version", "26.1")
	req.Header.Set("x-nmp-app-version-name", "26.4.0")
	req.Header.Set("x-nmp-app-build-number", "26357")
	req.Header.Set("x-nmp-app-brand", "Tori")
	req.Header.Set("x-nmp-device", "iPhone")
	req.Header.Set("x-finn-apps-adinput-version-name", "viewings")

	// Auth
	req.Header.Set("Authorization", "Bearer "+bearerToken)

	fmt.Printf("POST %s\n", url)
	fmt.Printf("Service: %s\n", service)

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		fmt.Printf("Error: %v\n", err)
		return
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)

	fmt.Printf("Status: %d\n", resp.StatusCode)

	// Check for Location header (contains new ad URL)
	if loc := resp.Header.Get("Location"); loc != "" {
		fmt.Printf("Location: %s\n", loc)
	}
	if etag := resp.Header.Get("ETag"); etag != "" {
		fmt.Printf("ETag: %s\n", etag)
	}

	var prettyJSON bytes.Buffer
	if json.Indent(&prettyJSON, body, "", "  ") == nil {
		fmt.Printf("Response:\n%s\n", prettyJSON.String())
	} else if len(body) > 0 {
		fmt.Printf("Response: %s\n", string(body))
	}

	// Extract ad ID if successful
	if resp.StatusCode == 201 {
		var result struct {
			Ad struct {
				ID string `json:"id"`
			} `json:"ad"`
		}
		if json.Unmarshal(body, &result) == nil && result.Ad.ID != "" {
			fmt.Printf("\n✓ Successfully created draft ad with ID: %s\n", result.Ad.ID)
		}
	}
}
