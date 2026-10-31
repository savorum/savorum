package app_test

import (
	"bytes"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"net/http"
	"testing"
)

// authenticator is a software WebAuthn authenticator holding one ES256
// credential, with attestation "none" and user verification always performed.
type authenticator struct {
	t          *testing.T
	key        *ecdsa.PrivateKey
	credential []byte
	counter    uint32
}

func newAuthenticator(t *testing.T) *authenticator {
	t.Helper()

	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}

	credential := make([]byte, 32)
	if _, err := rand.Read(credential); err != nil {
		t.Fatal(err)
	}

	return &authenticator{t: t, key: key, credential: credential}
}

func encoding(value []byte) string { return base64.RawURLEncoding.EncodeToString(value) }

// The CBOR below covers only what a registration needs: unsigned and negative
// integers, byte and text strings, and maps with those as keys and values.
func cborHead(major byte, n uint64) []byte {
	switch {
	case n < 24:
		return []byte{major<<5 | byte(n)}
	case n < 256:
		return []byte{major<<5 | 24, byte(n)}
	default:
		head := []byte{major<<5 | 25, 0, 0}
		binary.BigEndian.PutUint16(head[1:], uint16(n)) //nolint:gosec // callers pass lengths below 65536

		return head
	}
}

func cborInt(n int) []byte {
	if n >= 0 {
		return cborHead(0, uint64(n))
	}

	return cborHead(1, uint64(-1-n))
}

func cborBytes(value []byte) []byte { return append(cborHead(2, uint64(len(value))), value...) }
func cborText(value string) []byte  { return append(cborHead(3, uint64(len(value))), value...) }

func cborMap(pairs ...[]byte) []byte {
	return append(cborHead(5, uint64(len(pairs)/2)), bytes.Join(pairs, nil)...)
}

func (a *authenticator) publicKey() []byte {
	x, y := a.key.X.FillBytes(make([]byte, 32)), a.key.Y.FillBytes(make([]byte, 32))

	return cborMap(
		cborInt(1), cborInt(2), cborInt(3), cborInt(-7), cborInt(-1), cborInt(1),
		cborInt(-2), cborBytes(x), cborInt(-3), cborBytes(y),
	)
}

func (a *authenticator) authenticatorData(rpID string, flags byte, attested bool) []byte {
	hash := sha256.Sum256([]byte(rpID))
	data := append(hash[:], flags)
	data = binary.BigEndian.AppendUint32(data, a.counter)

	if attested {
		data = append(data, make([]byte, 16)...)
		data = binary.BigEndian.AppendUint16(data, uint16(len(a.credential))) //nolint:gosec // the credential is 32 bytes
		data = append(data, a.credential...)
		data = append(data, a.publicKey()...)
	}

	return data
}

func clientData(t *testing.T, kind, challenge, origin string) []byte {
	t.Helper()

	encoded, err := json.Marshal(map[string]any{"type": kind, "challenge": challenge, "origin": origin, "crossOrigin": false})
	if err != nil {
		t.Fatal(err)
	}

	return encoded
}

const (
	userPresent  = 0x01
	userVerified = 0x04
	attestedData = 0x40
)

// register answers a creation challenge.
func (a *authenticator) register(challenge, rpID, origin string) map[string]any {
	data := a.authenticatorData(rpID, userPresent|userVerified|attestedData, true)
	attestation := cborMap(cborText("fmt"), cborText("none"), cborText("attStmt"), cborMap(), cborText("authData"), cborBytes(data))

	return map[string]any{
		"id": encoding(a.credential), "rawId": encoding(a.credential), "type": "public-key",
		"response": map[string]any{
			"clientDataJSON":    encoding(clientData(a.t, "webauthn.create", challenge, origin)),
			"attestationObject": encoding(attestation),
		},
		"clientExtensionResults": map[string]any{},
	}
}

// assert answers a request challenge for the given user handle.
func (a *authenticator) assert(challenge, rpID, origin string, handle []byte) map[string]any {
	a.counter++

	data := a.authenticatorData(rpID, userPresent|userVerified, false)
	client := clientData(a.t, "webauthn.get", challenge, origin)
	hash := sha256.Sum256(client)

	signature, err := ecdsa.SignASN1(rand.Reader, a.key, sha256Sum(append(append([]byte(nil), data...), hash[:]...)))
	if err != nil {
		a.t.Fatal(err)
	}

	return map[string]any{
		"id": encoding(a.credential), "rawId": encoding(a.credential), "type": "public-key",
		"response": map[string]any{
			"clientDataJSON": encoding(client), "authenticatorData": encoding(data),
			"signature": encoding(signature), "userHandle": encoding(handle),
		},
		"clientExtensionResults": map[string]any{},
	}
}

func sha256Sum(data []byte) []byte {
	sum := sha256.Sum256(data)

	return sum[:]
}

func TestAPasskeyIsRegisteredRenamedUsedToSignInAndRemoved(t *testing.T) {
	instance := newAccounts(t)
	admin := instance.signIn("admin", adminPassword)
	device := newAuthenticator(t)

	const rpID = "savorum.test"

	creation := decode[struct {
		PublicKey struct {
			Challenge string `json:"challenge"`
			User      struct {
				ID string `json:"id"`
			} `json:"user"`
		} `json:"publicKey"` //nolint:tagliatelle // WebAuthn defines this member name
	}](t, admin.send(http.MethodPost, "/api/v1/account/passkeys/register/begin", nil))

	handle, err := base64.RawURLEncoding.DecodeString(creation.PublicKey.User.ID)
	if err != nil {
		t.Fatal(err)
	}

	registered, err := json.Marshal(device.register(creation.PublicKey.Challenge, rpID, publicURL))
	if err != nil {
		t.Fatal(err)
	}

	admin.raw(http.MethodPost, "/api/v1/account/passkeys/register/finish?name=Laptop", registered, "application/json", http.StatusOK)

	listed := admin.send(http.MethodGet, "/api/v1/account/passkeys", nil)
	passkey := firstPasskey(t, listed.Body.Bytes())

	admin.send(http.MethodPut, "/api/v1/account/passkeys/"+passkey, map[string]any{"name": "Work laptop"})

	anonymous := instance.anonymous()
	request := decode[struct {
		PublicKey struct {
			Challenge string `json:"challenge"`
		} `json:"publicKey"` //nolint:tagliatelle // WebAuthn defines this member name
	}](t, anonymous.send(http.MethodPost, "/api/v1/account/passkeys/login/begin", nil))

	assertion, err := json.Marshal(device.assert(request.PublicKey.Challenge, rpID, publicURL, handle))
	if err != nil {
		t.Fatal(err)
	}

	login := anonymous.raw(http.MethodPost, "/api/v1/account/passkeys/login/finish", assertion, "application/json", http.StatusOK)

	signedIn := &session{accounts: instance}

	for _, cookie := range login.Result().Cookies() {
		if cookie.Name == "savorum_session" {
			signedIn.cookie = cookie.Value
		}
	}

	signedIn.csrf = field[string](t, login.Body.Bytes(), "csrf_token")

	if got, want := text(t, field[map[string]any](t, signedIn.send(http.MethodGet, "/api/v1/account", nil).Body.Bytes(), "user"), "id"),
		text(t, field[map[string]any](t, admin.send(http.MethodGet, "/api/v1/account", nil).Body.Bytes(), "user"), "id"); got != want {
		t.Fatalf("passkey session belongs to %s, want %s", got, want)
	}

	admin.send(http.MethodDelete, "/api/v1/account/passkeys/"+passkey, nil)
}

func firstPasskey(t *testing.T, payload []byte) string {
	t.Helper()

	var listed struct {
		Passkeys []struct {
			ID string `json:"id"`
		} `json:"passkeys"`
	}
	if err := json.Unmarshal(payload, &listed); err != nil || len(listed.Passkeys) == 0 {
		t.Fatalf("no passkey in %s (%v)", payload, err)
	}

	return listed.Passkeys[0].ID
}
