// SPDX-License-Identifier: Apache-2.0
package capability

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strconv"
)

// ResponseSignatureVersion is the G06 versioned response-signing profile. It
// is negotiated on the request with X-DW-Response-Signature-Version and never
// changes the frozen D06 1.0 request envelope.
const ResponseSignatureVersion = "v2"

// SigningKey is the deployment's current HMAC key and its non-secret
// identifier. The identifier lets a Provider select the right key during a
// rotation window without hashing or transmitting the secret.
type SigningKey struct {
	ID     string
	Secret []byte
}

// Validate enforces the same key-id and secret rules as startup config.
func (k SigningKey) Validate() error {
	if k.ID == "" {
		return fmt.Errorf("%w: CAPABILITY_HMAC_KEY_ID is required", ErrConfigInvalid)
	}
	if !validKeyID(k.ID) {
		return fmt.Errorf("%w: CAPABILITY_HMAC_KEY_ID is invalid", ErrConfigInvalid)
	}
	if len(k.Secret) < minHMACSecretBytes {
		return fmt.Errorf("%w: CAPABILITY_HMAC_SECRET must be at least %d bytes", ErrConfigInvalid, minHMACSecretBytes)
	}
	return nil
}

// responseSignature binds a response to the exact request timestamp,
// invocation id, HTTP status, and body bytes. Binding to the timestamp and
// invocation prevents a valid response from being replayed for another call;
// the body hash keeps the MAC input bounded.
func responseSignature(secret []byte, status int, timestamp, invocationID string, body []byte) string {
	bodyHash := sha256.Sum256(body)
	mac := hmac.New(sha256.New, secret)
	mac.Write([]byte(ResponseSignatureVersion))
	mac.Write([]byte("\n"))
	mac.Write([]byte(strconv.Itoa(status)))
	mac.Write([]byte("\n"))
	mac.Write([]byte(timestamp))
	mac.Write([]byte("\n"))
	mac.Write([]byte(invocationID))
	mac.Write([]byte("\n"))
	mac.Write([]byte(hex.EncodeToString(bodyHash[:])))
	return hex.EncodeToString(mac.Sum(nil))
}

// validKeyID mirrors the config-package rule so the capability package can
// validate a key injected by tests without importing config.
func validKeyID(value string) bool {
	if len(value) < 1 || len(value) > 16 {
		return false
	}
	for index, char := range value {
		if char >= 'a' && char <= 'z' || char >= '0' && char <= '9' {
			continue
		}
		if char == '-' && index > 0 && index < len(value)-1 {
			continue
		}
		return false
	}
	return true
}
