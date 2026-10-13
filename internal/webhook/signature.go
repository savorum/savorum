package webhook

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"strconv"
	"strings"
	"time"
)

// Signature signs the exact request body and fresh Unix timestamp. Delivery-ID
// and Event-ID are also inside the signed body; retry attempts keep that body.
func Signature(secret string, at time.Time, body []byte) string {
	stamp := strconv.FormatInt(at.Unix(), 10)
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(stamp + "."))
	mac.Write(body)

	return "t=" + stamp + ",v1=" + hex.EncodeToString(mac.Sum(nil))
}

// VerifySignature is a receiver example: compare in constant time, enforce a
// five-minute replay window, then persistently deduplicate the signed delivery ID.
func VerifySignature(secret, header string, body []byte, now time.Time) bool {
	parts := strings.Split(header, ",")
	if len(parts) != 2 || !strings.HasPrefix(parts[0], "t=") || !strings.HasPrefix(parts[1], "v1=") {
		return false
	}

	stamp, err := strconv.ParseInt(strings.TrimPrefix(parts[0], "t="), 10, 64)
	if err != nil {
		return false
	}

	if stamp < now.Unix()-300 || stamp > now.Unix()+300 {
		return false
	}

	signature, err := hex.DecodeString(strings.TrimPrefix(parts[1], "v1="))
	if err != nil {
		return false
	}

	expected := Signature(secret, time.Unix(stamp, 0), body)

	expectedBytes, err := hex.DecodeString(strings.Split(expected, ",v1=")[1])
	if err != nil {
		return false
	}

	return hmac.Equal(signature, expectedBytes)
}
