package identity

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestBrowserCookieNamespaceAndDuplicates(t *testing.T) {
	for _, host := range []bool{false, true} {
		policy := BrowserCookies{HostPrefix: host}

		name := policy.Name(SessionCookie)
		for _, test := range []struct {
			header string
			want   string
			err    error
		}{
			{name + "=legitimate", "legitimate", nil},
			{name + "=one; " + name + "=two", "", ErrCredentials},
			{name + "=same; " + name + "=same", "", ErrCredentials},
			{name + "=one; " + name, "", ErrCredentials},
			{name + "=one; " + name + "=\"unterminated", "", ErrCredentials},
			{"unrelated=one", "", http.ErrNoCookie},
		} {
			r := httptest.NewRequestWithContext(t.Context(), "GET", "https://tenant.example", nil)
			r.Header.Set("Cookie", test.header)

			cookie, err := policy.Read(r, SessionCookie)
			if !errors.Is(err, test.err) || (err == nil && cookie.Value != test.want) {
				t.Fatalf("host=%t header=%q cookie=%v err=%v", host, test.header, cookie, err)
			}
		}
	}

	policy := BrowserCookies{HostPrefix: true}
	r := httptest.NewRequestWithContext(t.Context(), "GET", "https://tenant.example", nil)
	r.Header.Add("Cookie", "savorum_session=parent-forgery; savorum_session=another")

	if _, err := policy.Read(r, SessionCookie); !errors.Is(err, http.ErrNoCookie) {
		t.Fatalf("legacy credential accepted: %v", err)
	}

	r.Header.Add("Cookie", "__Host-savorum_session=legitimate")

	if cookie, err := policy.Read(r, SessionCookie); err != nil || cookie.Value != "legitimate" {
		t.Fatalf("legacy cookie shadowed protected credential: %v %v", cookie, err)
	}

	r.Header.Add("Cookie", "__Host-savorum_session=duplicate")

	if _, err := policy.Read(r, SessionCookie); !errors.Is(err, ErrCredentials) {
		t.Fatalf("multiple Cookie headers accepted: %v", err)
	}
}

func TestBrowserCookieHostRequirementsAndLegacyHTTP(t *testing.T) {
	for _, host := range []bool{false, true} {
		policy := BrowserCookies{HostPrefix: host}

		for _, name := range []string{SessionCookie, LibraryCookie} {
			for _, age := range []int{3600, -1} {
				w := httptest.NewRecorder()
				policy.Set(w, name, "token", age, false)
				cookies := w.Result().Cookies()

				c := cookies[0]
				if c.Name != policy.Name(name) || c.Path != "/" || c.Domain != "" || !c.HttpOnly || c.Secure != host || c.MaxAge != age || c.SameSite != http.SameSiteLaxMode {
					t.Fatalf("cookie requirements: %#v", c)
				}

				if host && (len(cookies) != 2 || cookies[1].Name != name || cookies[1].MaxAge != -1 || cookies[1].Domain != "") {
					t.Fatalf("unsafe legacy migration: %#v", cookies)
				}

				if !host && len(cookies) != 1 {
					t.Fatal("legacy HTTP cookie changed")
				}
			}
		}
	}
}
