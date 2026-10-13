package federation

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-ldap/ldap/v3"
)

type fakeDirectory struct {
	binds   [][2]string
	search  *ldap.SearchRequest
	entries []*ldap.Entry
	bindErr error
}

func (d *fakeDirectory) Bind(dn, password string) error {
	d.binds = append(d.binds, [2]string{dn, password})
	return d.bindErr
}

func (d *fakeDirectory) Search(r *ldap.SearchRequest) (*ldap.SearchResult, error) {
	d.search = r
	return &ldap.SearchResult{Entries: d.entries}, nil
}
func (*fakeDirectory) Close() error { return nil }
func TestLDAPSearchThenFreshBind(t *testing.T) {
	for _, scenario := range []string{"valid", "zero_results", "ambiguous", "bad_password", "missing_id", "empty_password"} {
		t.Run(scenario, func(t *testing.T) {
			p := Provider{ID: "ldap", Kind: "ldap", DisplayName: "Directory", LDAP: &LDAPConfig{URL: "ldaps://directory.example", UserBase: "ou=Users,dc=example", LoginAttribute: "uid", IDAttribute: "entryUUID", DisplayAttribute: "cn"}}
			h, f := testHandler(t, p)
			entry := &ldap.Entry{DN: "cn=Renamed,ou=Users,dc=example", Attributes: []*ldap.EntryAttribute{{Name: "entryUUID", ByteValues: [][]byte{[]byte("stable-entry")}}}}
			search := &fakeDirectory{entries: []*ldap.Entry{entry}}
			user := &fakeDirectory{}
			password := "directory-password"

			switch scenario {
			case "zero_results":
				search.entries = nil
			case "ambiguous":
				search.entries = append(search.entries, entry)
			case "bad_password":
				user.bindErr = errors.New("denied")
			case "missing_id":
				entry.Attributes = nil
			case "empty_password":
				password = ""
			}

			connections := 0
			h.ldapDial = func(*LDAPConfig) (directoryConnection, error) {
				connections++
				if connections == 1 {
					return search, nil
				}

				return user, nil
			}
			r := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/api/v1/auth/ldap/login", strings.NewReader(`{"login":"*)(uid=*)","password":"`+password+`"}`))
			r.Header.Set("Origin", "https://recipes.example")

			w := httptest.NewRecorder()
			h.ServeHTTP(w, r)

			if scenario == "valid" {
				if w.Code != 200 || connections != 2 || len(search.binds) != 0 || len(user.binds) != 1 || user.binds[0] != [2]string{entry.DN, password} {
					t.Fatal(w.Code, connections, user.binds)
				}

				if search.search.Filter != `(uid=\2a\29\28uid=\2a\29)` {
					t.Fatal("unescaped filter", search.search.Filter)
				}

				if len(f.subjects) != 1 || f.subjects[0] != "ldap:stable-entry" {
					t.Fatal(f.subjects)
				}
			} else if w.Code != 401 || len(f.subjects) != 0 {
				t.Fatalf("invalid LDAP credentials accepted: %d %v", w.Code, f.subjects)
			}
		})
	}
}

func TestLDAPRejectsUnauthenticatedTLS(t *testing.T) {
	server := httptest.NewTLSServer(nil)
	defer server.Close()

	if conn, e := ldapConnect(&LDAPConfig{URL: "ldaps" + strings.TrimPrefix(server.URL, "https")}); e == nil {
		_ = conn.Close()

		t.Fatal("untrusted certificate accepted")
	}
}
