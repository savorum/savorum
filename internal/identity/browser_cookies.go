package identity

import (
	"errors"
	"net/http"
	"strings"
)

// Cookie names shared by account and federation boundaries.
const (
	SessionCookie = "savorum_session"
	LibraryCookie = "savorum_library"
)

// BrowserCookies selects one cookie namespace for the whole instance.
// Hosted instances never accept the default namespace as a credential fallback.
type BrowserCookies struct {
	HostPrefix bool
}

// Name returns the only accepted cookie name for this instance.
func (p BrowserCookies) Name(name string) string {
	if p.HostPrefix {
		return "__Host-" + name
	}

	return name
}

// Read rejects repeated or malformed selected cookies before choosing a value.
func (p BrowserCookies) Read(r *http.Request, name string) (*http.Cookie, error) {
	selected := p.Name(name)
	count := 0

	for _, header := range r.Header.Values("Cookie") {
		for part := range strings.SplitSeq(header, ";") {
			key, _, _ := strings.Cut(strings.TrimSpace(part), "=")
			if strings.TrimSpace(key) == selected {
				count++
			}
		}
	}

	if count == 0 {
		return nil, http.ErrNoCookie
	}

	if count != 1 {
		return nil, ErrCredentials
	}

	cookie, err := r.Cookie(selected)
	if err != nil {
		return nil, ErrCredentials
	}

	return cookie, nil
}

// Validate refuses ambiguous session and library selectors before dispatch.
func (p BrowserCookies) Validate(r *http.Request) error {
	for _, name := range []string{SessionCookie, LibraryCookie} {
		if _, err := p.Read(r, name); err != nil && !errors.Is(err, http.ErrNoCookie) {
			return err
		}
	}

	return nil
}

// Set also expires host-only default-namespace cookies; a parent-domain cookie is ignored,
// because an incoming Cookie header cannot prove its Domain or Path attributes.
func (p BrowserCookies) Set(w http.ResponseWriter, name, value string, maxAge int, secure bool) {
	//nolint:gosec // Hosted cookies are always Secure; explicit HTTP self-hosting remains supported.
	http.SetCookie(w, &http.Cookie{Name: p.Name(name), Value: value, Path: "/", HttpOnly: true, Secure: secure || p.HostPrefix, SameSite: http.SameSiteLaxMode, MaxAge: maxAge})

	if p.HostPrefix {
		http.SetCookie(w, &http.Cookie{Name: name, Path: "/", HttpOnly: true, Secure: true, SameSite: http.SameSiteLaxMode, MaxAge: -1})
	}
}
