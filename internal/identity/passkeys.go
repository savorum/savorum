package identity

import (
	"time"

	"github.com/go-webauthn/webauthn/webauthn"
)

// Passkey holds public credential material and user-visible lifecycle metadata.
type Passkey struct {
	ID         string              `json:"id"`
	UserID     string              `json:"-"`
	Credential webauthn.Credential `json:"-"`
	Name       string              `json:"name"`
	CreatedAt  time.Time           `json:"created_at"`
	LastUsedAt *time.Time          `json:"last_used_at"`
	RPID       string              `json:"rp_id"`
}
