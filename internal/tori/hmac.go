package tori

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha512"
	"encoding/base64"
	"strings"
)

const (
	// HMACKey is the shared secret for calculating finn-gw-key header
	HMACKey = "3b535f36-79be-424b-a6fd-116c6e69f137"
)

// CalculateGatewayKey calculates the HMAC signature for the finn-gw-key header.
// The signature is calculated from: METHOD;PATH;SERVICE;BODY
func CalculateGatewayKey(method, path, service string, body []byte) string {
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

	h := hmac.New(sha512.New, []byte(HMACKey))
	h.Write(msg.Bytes())
	return base64.StdEncoding.EncodeToString(h.Sum(nil))
}
