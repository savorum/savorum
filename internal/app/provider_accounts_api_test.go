package app_test

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"slices"
	"testing"
)

// call sends a request as a signed-in app session and requires a 200.
func (f *nativeFixture) call(session redemption, method, path string, body any) []byte {
	f.t.Helper()

	var encoded []byte

	if body != nil {
		var err error
		if encoded, err = json.Marshal(body); err != nil {
			f.t.Fatal(err)
		}
	}

	request, err := http.NewRequestWithContext(f.t.Context(), method, f.server.URL+path, bytes.NewReader(encoded))
	if err != nil {
		f.t.Fatal(err)
	}

	request.Header.Set("Origin", f.server.URL)
	request.Header.Set("Cookie", "savorum_session="+session.session)
	request.Header.Set("X-CSRF-Token", session.csrf)

	if encoded != nil {
		request.Header.Set("Content-Type", "application/json")
	}

	response, err := f.native().Do(request)
	if err != nil {
		f.t.Fatal(err)
	}
	defer response.Body.Close()

	payload, _ := io.ReadAll(response.Body)
	if response.StatusCode != http.StatusOK {
		f.t.Fatalf("%s %s status = %d, want 200; body = %s", method, path, response.StatusCode, payload)
	}

	return payload
}

// link attaches another provider subject to the signed-in account and returns
// the replacement session.
func (f *nativeFixture) link(session redemption, subject string) redemption {
	f.t.Helper()

	c, back := f.startLink(session, subject)

	linked := f.confirmLink(back.Get("code"), c.verifier, session)
	if linked.status != http.StatusOK {
		f.t.Fatalf("link %s: %+v", subject, linked)
	}

	return linked
}

// userID reads the signed-in account's identifier.
func (f *nativeFixture) userID(session redemption) string {
	f.t.Helper()

	user := field[map[string]any](f.t, f.call(session, http.MethodGet, "/api/v1/account", nil), "user")

	return text(f.t, user, "id")
}

// signInAs signs in with whichever subject still belongs to the account.
func (f *nativeFixture) signInAs(id string, subjects ...string) redemption {
	f.t.Helper()

	for _, subject := range subjects {
		f.idp.next(subject, false, false)

		c := newCeremony(f.t)
		if session := f.redeem(f.returned(f.browser(), c).Get("code"), c.verifier, f.server.URL, ""); session.status == http.StatusOK && f.userID(session) == id {
			return session
		}
	}

	f.t.Fatalf("no subject of %s signs in", id)

	return redemption{}
}

func TestSignInMethodsAreLinkedUnlinkedAndForceUnlinked(t *testing.T) {
	f := newNativeFixture(t)
	alice := f.signIn("alice")

	f.exec("UPDATE users SET instance_admin=1 WHERE display_name='Alice'")

	aliceID := f.userID(alice)
	alice = f.link(alice, "carol")

	listed := f.call(alice, http.MethodGet, "/api/v1/account/methods", nil)

	var methods struct {
		Methods []struct {
			ID string `json:"id"`
		} `json:"methods"`
	}
	if err := json.Unmarshal(listed, &methods); err != nil || len(methods.Methods) < 2 {
		t.Fatalf("methods = %s (%v)", listed, err)
	}

	removed := methods.Methods[1].ID

	f.call(alice, http.MethodDelete, "/api/v1/account/methods/"+removed, nil)

	alice = f.signInAs(aliceID, "alice", "carol")

	if slices.Contains(f.methodIDs(alice, "/api/v1/account/methods"), removed) {
		t.Fatalf("method %s is still linked after deletion", removed)
	}

	bob := f.link(f.signIn("bob"), "erin")
	userID := f.userID(bob)

	listed = f.call(alice, http.MethodGet, "/api/v1/users/"+userID+"/methods", nil)
	if err := json.Unmarshal(listed, &methods); err != nil || len(methods.Methods) < 2 {
		t.Fatalf("managed methods = %s (%v)", listed, err)
	}

	forced := methods.Methods[1].ID

	f.call(alice, http.MethodDelete, "/api/v1/users/"+userID+"/methods/"+forced, nil)

	if slices.Contains(f.methodIDs(alice, "/api/v1/users/"+userID+"/methods"), forced) {
		t.Fatalf("method %s is still linked after force unlink", forced)
	}

	library := field[string](t, f.call(alice, http.MethodPost, "/api/v1/libraries", map[string]any{"name": "Shared"}), "id")

	f.call(alice, http.MethodPost, "/api/v1/account/group-mappings", map[string]any{"id": "cooks", "provider_id": "work", "group": "cooks", "library_id": library, "role": "viewer"})
}

// methodIDs lists the identifiers of the sign-in methods at path.
func (f *nativeFixture) methodIDs(session redemption, path string) []string {
	f.t.Helper()

	var listed struct {
		Methods []struct {
			ID string `json:"id"`
		} `json:"methods"`
	}
	if err := json.Unmarshal(f.call(session, http.MethodGet, path, nil), &listed); err != nil {
		f.t.Fatal(err)
	}

	ids := make([]string, 0, len(listed.Methods))
	for _, method := range listed.Methods {
		ids = append(ids, method.ID)
	}

	return ids
}
