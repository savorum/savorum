package app_test

import (
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/url"
	"strings"
	"testing"
)

func (c ceremony) challenge() string {
	sum := sha256.Sum256([]byte(c.verifier))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}

func (c ceremony) linkEntry(base, ticket string) string {
	return c.entry(base) + "&" + url.Values{"link_ticket": {ticket}}.Encode()
}

// appPost sends a JSON request from the app's cookie-less client with its session.
func (f *nativeFixture) appPost(path string, body any, session redemption) (int, map[string]any, []*http.Cookie) {
	f.t.Helper()

	payload, err := json.Marshal(body)
	if err != nil {
		f.t.Fatal(err)
	}

	request, err := http.NewRequestWithContext(f.t.Context(), http.MethodPost, f.server.URL+path, strings.NewReader(string(payload)))
	if err != nil {
		f.t.Fatal(err)
	}

	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Origin", f.server.URL)

	if session.session != "" {
		request.Header.Set("Cookie", "savorum_session="+session.session)
		request.Header.Set("X-CSRF-Token", session.csrf)
	}

	response, err := f.native().Do(request)
	if err != nil {
		f.t.Fatal(err)
	}
	defer response.Body.Close()

	value := map[string]any{}
	_ = json.NewDecoder(response.Body).Decode(&value)

	return response.StatusCode, value, response.Cookies()
}

func (f *nativeFixture) linkTicket(session redemption, c ceremony) (int, string) {
	f.t.Helper()

	status, value, _ := f.appPost("/api/v1/auth/work/native-link-tickets", map[string]string{"code_challenge": c.challenge(), "code_challenge_method": "S256"}, session)
	ticket, _ := value["link_ticket"].(string)

	return status, ticket
}

func (f *nativeFixture) confirmLink(code, verifier string, session redemption) redemption {
	f.t.Helper()

	status, value, cookies := f.appPost("/api/v1/auth/native-links", map[string]string{"code": code, "code_verifier": verifier, "redirect_uri": nativeReturnTarget}, session)
	out := redemption{status: status}

	for _, cookie := range cookies {
		if cookie.Name == "savorum_session" && cookie.MaxAge > 0 {
			out.session = cookie.Value
		}
	}

	out.csrf, _ = value["csrf_token"].(string)
	if user, ok := value["user"].(map[string]any); ok {
		out.userName, _ = user["display_name"].(string)
	}

	return out
}

// signIn completes a native sign-in as subject and returns the app's session.
func (f *nativeFixture) signIn(subject string) redemption {
	f.t.Helper()
	f.idp.next(subject, false, false)

	c := newCeremony(f.t)

	session := f.redeem(f.returned(f.browser(), c).Get("code"), c.verifier, f.server.URL, "")
	if session.status != http.StatusOK {
		f.t.Fatalf("sign-in as %s: %+v", subject, session)
	}

	return session
}

// startLink obtains a ticket for session and completes the browser part as subject.
func (f *nativeFixture) startLink(session redemption, subject string) (ceremony, url.Values) {
	f.t.Helper()

	c := newCeremony(f.t)

	status, ticket := f.linkTicket(session, c)
	if status != http.StatusOK || ticket == "" {
		f.t.Fatalf("link ticket: status=%d", status)
	}

	f.idp.next(subject, false, false)

	return c, f.linkReturned(c, ticket)
}

// linkReturned opens a link entry in a new authentication browser and returns
// the app's callback query.
func (f *nativeFixture) linkReturned(c ceremony, ticket string) url.Values {
	f.t.Helper()

	browser := f.browser()

	response := visit(f.t, browser, c.linkEntry(f.server.URL, ticket))
	location, err := url.Parse(response.Header.Get("Location"))

	if response.StatusCode != http.StatusSeeOther || err != nil || location.Scheme+"://"+location.Host+location.Path != nativeReturnTarget || location.Query().Get("state") != c.state {
		f.t.Fatalf("link did not return to the app: status=%d location=%q", response.StatusCode, response.Header.Get("Location"))
	}

	if browserSession(f.t, browser, f.server.URL) != "" {
		f.t.Fatal("native link left a browser session")
	}

	return location.Query()
}

func (f *nativeFixture) exec(query string) {
	f.t.Helper()

	database, err := sql.Open("sqlite", f.database)
	if err != nil {
		f.t.Fatal(err)
	}
	defer database.Close()

	if _, err = database.ExecContext(f.t.Context(), query); err != nil {
		f.t.Fatal(err)
	}
}

func TestNativeProviderLinkOldFlowLosesTheTransactionCookie(t *testing.T) {
	f := newNativeFixture(t)
	alice := f.signIn("alice")

	status, value, cookies := f.appPost("/api/v1/auth/work/login", map[string]bool{"link": true}, alice)

	start, _ := value["authorization_url"].(string)
	if status != http.StatusOK || start == "" || len(cookies) == 0 {
		t.Fatalf("web link start: status=%d", status)
	}

	// The transaction cookie went to the app's client, which keeps no cookies.
	if final := visit(t, f.browser(), start); final.StatusCode != http.StatusUnauthorized {
		t.Fatalf("callback without the transaction cookie: status %d", final.StatusCode)
	}
}

func TestNativeProviderLinkCompletesInTheBrowserAndConfirmsNatively(t *testing.T) {
	f := newNativeFixture(t)
	alice := f.signIn("alice")

	c, back := f.startLink(alice, "carol")
	if back.Get("code") == "" || back.Get("error") != "" {
		t.Fatalf("no code returned: %v", back)
	}

	linked := f.confirmLink(back.Get("code"), c.verifier, alice)
	if linked.status != http.StatusOK || linked.session == "" || linked.csrf == "" || linked.userName != "Alice" {
		t.Fatalf("confirmation: %+v", linked)
	}

	if f.account(alice.session) != "" {
		t.Fatal("the app's previous session survived the link")
	}

	if f.account(linked.session) != "Alice" {
		t.Fatal("replacement session does not belong to Alice")
	}

	if replay := f.confirmLink(back.Get("code"), c.verifier, linked); replay.status != http.StatusBadRequest || replay.session != "" {
		t.Fatalf("replayed code accepted: %+v", replay)
	}

	// The linked subject now signs in to Alice's account.
	if carol := f.signIn("carol"); carol.userName != "Alice" {
		t.Fatalf("linked subject signed in as %q", carol.userName)
	}
}

func TestNativeProviderLinkRequiresTheAppsConfirmation(t *testing.T) {
	f := newNativeFixture(t)
	alice := f.signIn("alice")
	bob := f.signIn("bob")

	t.Run("wrong verifier", func(t *testing.T) {
		_, back := f.startLink(alice, "carol")

		got := f.confirmLink(back.Get("code"), newCeremony(t).verifier, alice)
		if got.status != http.StatusBadRequest || got.session != "" || f.account(alice.session) != "Alice" {
			t.Fatalf("wrong verifier: %+v", got)
		}
	})

	t.Run("another account's session", func(t *testing.T) {
		c, back := f.startLink(alice, "carol")

		got := f.confirmLink(back.Get("code"), c.verifier, bob)
		if got.status != http.StatusBadRequest || got.session != "" || f.account(bob.session) != "Bob" {
			t.Fatalf("other account confirmed: %+v", got)
		}
	})

	t.Run("no session", func(t *testing.T) {
		c, back := f.startLink(alice, "carol")

		if got := f.confirmLink(back.Get("code"), c.verifier, redemption{}); got.status != http.StatusUnauthorized || got.session != "" {
			t.Fatalf("session-less confirmation: %+v", got)
		}
	})

	t.Run("expired code", func(t *testing.T) {
		c, back := f.startLink(alice, "carol")
		f.exec(`UPDATE native_link_grants SET expires_at=unixepoch()-1`)

		if got := f.confirmLink(back.Get("code"), c.verifier, alice); got.status != http.StatusBadRequest {
			t.Fatalf("expired code: %+v", got)
		}
	})

	t.Run("sign-in endpoint", func(t *testing.T) {
		c, back := f.startLink(alice, "carol")

		if got := f.redeem(back.Get("code"), c.verifier, f.server.URL, ""); got.status != http.StatusUnauthorized || got.session != "" {
			t.Fatalf("link code signed in: %+v", got)
		}
	})

	// None of the unconfirmed ceremonies linked Carol.
	if carol := f.signIn("carol"); carol.userName != "Carol" {
		t.Fatalf("unconfirmed subject belongs to %q", carol.userName)
	}
}

func TestNativeProviderLinkRefusesAnIdentityOfAnotherAccount(t *testing.T) {
	f := newNativeFixture(t)
	alice := f.signIn("alice")
	f.signIn("bob")

	c, back := f.startLink(alice, "bob")
	if got := f.confirmLink(back.Get("code"), c.verifier, alice); got.status != http.StatusConflict || got.session != "" || f.account(alice.session) != "Alice" {
		t.Fatalf("linked another account's identity: %+v", got)
	}
}

func TestNativeProviderLinkTicketIsBoundAndSingleUse(t *testing.T) {
	f := newNativeFixture(t)
	alice := f.signIn("alice")

	if status, _ := f.linkTicket(redemption{}, newCeremony(t)); status != http.StatusUnauthorized {
		t.Fatalf("ticket without a session: %d", status)
	}

	if status, _ := f.linkTicket(redemption{session: alice.session, csrf: "wrong"}, newCeremony(t)); status != http.StatusUnauthorized {
		t.Fatalf("ticket without CSRF proof: %d", status)
	}

	rejected := func(t *testing.T, c ceremony, ticket string) {
		t.Helper()

		back := f.linkReturned(c, ticket)
		if back.Get("error") != "authentication_failed" || back.Get("code") != "" {
			t.Fatalf("entry accepted: %v", back)
		}
	}

	t.Run("other challenge", func(t *testing.T) {
		_, ticket := f.linkTicket(alice, newCeremony(t))
		rejected(t, newCeremony(t), ticket)
	})

	t.Run("replayed ticket", func(t *testing.T) {
		c := newCeremony(t)
		_, ticket := f.linkTicket(alice, c)
		f.linkReturned(c, ticket)
		rejected(t, c, ticket)
	})

	t.Run("expired ticket", func(t *testing.T) {
		c := newCeremony(t)
		_, ticket := f.linkTicket(alice, c)
		f.exec(`UPDATE native_link_tickets SET expires_at=unixepoch()-1`)
		rejected(t, c, ticket)
	})

	t.Run("signed out before the browser opened", func(t *testing.T) {
		session := f.signIn("alice")
		c := newCeremony(t)
		_, ticket := f.linkTicket(session, c)
		f.exec(`UPDATE browser_sessions SET revoked_at=unixepoch() WHERE revoked_at IS NULL`)
		rejected(t, c, ticket)
	})
}

func TestNativeProviderLinkRechecksTheSessionAtTheCallback(t *testing.T) {
	f := newNativeFixture(t)
	alice := f.signIn("alice")
	c := newCeremony(t)
	_, ticket := f.linkTicket(alice, c)

	browser := f.browser()
	browser.CheckRedirect = func(r *http.Request, _ []*http.Request) error {
		if r.URL.Path == "/authorize" {
			return http.ErrUseLastResponse
		}

		return nil
	}

	provider := visit(t, browser, c.linkEntry(f.server.URL, ticket)).Header.Get("Location")
	f.exec(`UPDATE browser_sessions SET revoked_at=unixepoch() WHERE revoked_at IS NULL`)

	browser.CheckRedirect = f.browser().CheckRedirect

	location, err := url.Parse(visit(t, browser, provider).Header.Get("Location"))
	if err != nil || location.Query().Get("error") != "authentication_failed" || location.Query().Get("code") != "" {
		t.Fatalf("callback after sign-out: %v", location)
	}
}
