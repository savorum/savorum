package federation

import (
	"crypto/tls"
	"encoding/hex"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"time"

	"github.com/go-ldap/ldap/v3"
)

type directoryConnection interface {
	Bind(string, string) error
	Search(*ldap.SearchRequest) (*ldap.SearchResult, error)
	Close() error
}

func ldapConnect(c *LDAPConfig) (directoryConnection, error) {
	u, e := url.Parse(c.URL)
	if e != nil {
		return nil, fmt.Errorf("connect TLS directory: %w", e)
	}

	secure := &tls.Config{MinVersion: tls.VersionTLS12, ServerName: u.Hostname()}

	conn, e := ldap.DialURL(c.URL, ldap.DialWithDialer(&net.Dialer{Timeout: 10 * time.Second}), ldap.DialWithTLSConfig(secure))
	if e != nil {
		return nil, fmt.Errorf("connect TLS directory: %w", e)
	}

	conn.SetTimeout(10 * time.Second)

	if u.Scheme == "ldap" {
		if e = conn.StartTLS(secure); e != nil {
			_ = conn.Close()
			return nil, fmt.Errorf("connect TLS directory: %w", e)
		}
	}

	return conn, nil
}

func directorySubject(entry *ldap.Entry, attribute string) (string, error) {
	values := entry.GetRawAttributeValues(attribute)
	if len(values) != 1 || len(values[0]) == 0 {
		return "", errors.New("directory must return one immutable object ID")
	}

	if strings.EqualFold(attribute, "objectGUID") {
		b := values[0]
		if len(b) != 16 {
			return "", errors.New("invalid AD objectGUID")
		}
		// AD's first three UUID components use little-endian wire order.
		v := []byte{b[3], b[2], b[1], b[0], b[5], b[4], b[7], b[6], b[8], b[9], b[10], b[11], b[12], b[13], b[14], b[15]}
		s := hex.EncodeToString(v)

		return s[:8] + "-" + s[8:12] + "-" + s[12:16] + "-" + s[16:20] + "-" + s[20:], nil
	}

	return string(values[0]), nil
}

func (h *Handler) ldapLogin(w http.ResponseWriter, r *http.Request, p Provider, t transaction, in loginInput) {
	if in.Login == "" || in.Password == "" {
		h.fail(w, r)
		return
	}

	c := p.LDAP

	search, e := h.ldapDial(c)
	if e != nil {
		h.fail(w, r)
		return
	}
	defer search.Close()

	if c.BindDN != "" {
		secret, e := readSecret(c.BindPasswordFile)
		if e != nil || secret == "" {
			h.fail(w, r)
			return
		}

		if e = search.Bind(c.BindDN, secret); e != nil {
			h.fail(w, r)
			return
		}
	}

	attrs := []string{c.IDAttribute}
	for _, a := range []string{c.DisplayAttribute, c.EmailAttribute, c.GroupAttribute} {
		if a != "" {
			attrs = append(attrs, a)
		}
	}

	result, e := search.Search(ldap.NewSearchRequest(c.UserBase, ldap.ScopeWholeSubtree, ldap.NeverDerefAliases, 2, 10, false, "("+c.LoginAttribute+"="+ldap.EscapeFilter(in.Login)+")", attrs, nil))
	if e != nil || len(result.Entries) != 1 {
		h.fail(w, r)
		return
	}

	entry := result.Entries[0]

	subject, e := directorySubject(entry, c.IDAttribute)
	if e != nil {
		h.fail(w, r)
		return
	}

	user, e := h.ldapDial(c)
	if e != nil {
		h.fail(w, r)
		return
	}
	defer user.Close()

	if e = user.Bind(entry.DN, in.Password); e != nil {
		h.fail(w, r)
		return
	}

	groups := entry.GetAttributeValues(c.GroupAttribute)
	if len(c.RequiredGroups) > 0 {
		allowed := false
		for _, group := range c.RequiredGroups {
			allowed = allowed || slices.Contains(groups, group)
		}

		if !allowed {
			h.fail(w, r)
			return
		}
	}

	h.complete(w, r, p, t, subject, entry.GetAttributeValue(c.DisplayAttribute), entry.GetAttributeValue(c.EmailAttribute), groups, false)
}
