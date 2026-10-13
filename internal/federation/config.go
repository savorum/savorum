// Package federation implements externally verified, provider-neutral browser authentication.
package federation

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/netip"
	"net/url"
	"os"
	"regexp"
	"strings"

	"github.com/savorum/savorum/internal/access"
)

// Config is immutable, restart-bound deployment configuration. It is never an API response.
type Config struct {
	Providers []Provider `json:"providers"`
}

// Provider identifies one explicitly configured external trust domain.
type Provider struct {
	ID                 string         `json:"id"`
	Kind               string         `json:"kind"`
	DisplayName        string         `json:"display_name"`
	Disabled           bool           `json:"disabled,omitempty"`
	AutoProvision      bool           `json:"auto_provision,omitempty"`
	AllowLocalRecovery bool           `json:"allow_local_recovery,omitempty"`
	DefaultLibrary     string         `json:"default_library,omitempty"`
	DefaultRole        access.Role    `json:"default_role,omitempty"`
	OIDC               *OIDCConfig    `json:"oidc,omitempty"`
	LDAP               *LDAPConfig    `json:"ldap,omitempty"`
	Proxy              *ProxyConfig   `json:"proxy,omitempty"`
	SAML               *SAMLConfig    `json:"saml,omitempty"`
	Managed            *ManagedConfig `json:"managed,omitempty"`
}

// OIDCConfig selects the OIDC trust domain and explicit claim mapping.
type OIDCConfig struct {
	GroupsClaimPath  string   `json:"groups_claim_path,omitempty"`
	Issuer           string   `json:"issuer"`
	ClientID         string   `json:"client_id"`
	ClientSecretFile string   `json:"client_secret_file"`
	Scopes           []string `json:"scopes,omitempty"`
}

// LDAPConfig defines TLS directory search and immutable identity attributes.
type LDAPConfig struct {
	URL              string   `json:"url"`
	BindDN           string   `json:"bind_dn,omitempty"`
	BindPasswordFile string   `json:"bind_password_file,omitempty"`
	UserBase         string   `json:"user_base"`
	LoginAttribute   string   `json:"login_attribute"`
	DisplayAttribute string   `json:"display_attribute,omitempty"`
	EmailAttribute   string   `json:"email_attribute,omitempty"`
	IDAttribute      string   `json:"id_attribute"`
	GroupAttribute   string   `json:"group_attribute,omitempty"`
	RequiredGroups   []string `json:"required_groups,omitempty"`
}

// ProxyConfig defines the immediate peer and explicit identity headers.
type ProxyConfig struct {
	TrustedPeers  []string `json:"trusted_peers"`
	SubjectHeader string   `json:"subject_header"`
	DisplayHeader string   `json:"display_header,omitempty"`
	EmailHeader   string   `json:"email_header,omitempty"`
	// Strict defaults to true. False deliberately allows direct non-proxy requests.
	Strict *bool `json:"strict,omitempty"`
}

// SAMLConfig selects trusted IdP metadata and the service-provider key pair.
type SAMLConfig struct {
	GroupsAttribute     string `json:"groups_attribute,omitempty"`
	MetadataFile        string `json:"metadata_file,omitempty"`
	MetadataURL         string `json:"metadata_url,omitempty"`
	CertificateFile     string `json:"certificate_file"`
	PrivateKeyFile      string `json:"private_key_file"`
	SubjectAttribute    string `json:"subject_attribute,omitempty"`
	DisplayAttribute    string `json:"display_attribute,omitempty"`
	EmailAttribute      string `json:"email_attribute,omitempty"`
	AllowMutableSubject bool   `json:"allow_mutable_subject,omitempty"`
}

var (
	identifier = regexp.MustCompile(`^[a-z][a-z0-9_-]{0,63}$`)
	attribute  = regexp.MustCompile(`^[a-zA-Z][a-zA-Z0-9;-]*$`)
)

// Load reads a bounded JSON document with secret-file references, rejecting typos.
func Load(path string) (Config, error) {
	if path == "" {
		return Config{}, nil
	}

	//nolint:gosec // This path is owner-supplied restart-bound deployment configuration.
	f, err := os.Open(path)
	if err != nil {
		return Config{}, fmt.Errorf("open provider configuration: %w", err)
	}
	defer f.Close()

	d := json.NewDecoder(io.LimitReader(f, 1<<20))
	d.DisallowUnknownFields()

	var c Config
	if err := d.Decode(&c); err != nil {
		return c, fmt.Errorf("decode provider configuration: %w", err)
	}

	var extra any
	if d.Decode(&extra) != io.EOF {
		return c, errors.New("provider configuration must contain one JSON object")
	}

	return c, c.Validate()
}

func secureURL(raw string) bool {
	u, e := url.Parse(raw)
	return e == nil && u.Scheme == "https" && u.Host != "" && u.User == nil && u.Fragment == ""
}

// Validate rejects ambiguous identities and unsafe deployment defaults before startup.
func (c Config) Validate() error {
	seen := map[string]bool{}
	for _, p := range c.Providers {
		if !identifier.MatchString(p.ID) || p.ID == "local" || p.ID == "passkey" || p.ID == "recovery" || p.ID == "token" || p.ID == "system" || seen[p.ID] {
			return errors.New("provider IDs must be unique stable lowercase identifiers; local and passkey are reserved")
		}

		seen[p.ID] = true
		if strings.TrimSpace(p.DisplayName) == "" {
			return fmt.Errorf("provider %s requires display_name", p.ID)
		}

		if p.DefaultLibrary != "" && p.DefaultRole != access.Viewer && p.DefaultRole != access.Editor {
			return fmt.Errorf("provider %s default role must be viewer or editor", p.ID)
		}

		switch p.Kind {
		case "oidc":
			if err := p.validateOIDC(); err != nil {
				return err
			}
		case "ldap":
			if err := p.validateLDAP(); err != nil {
				return err
			}
		case "proxy":
			if err := p.validatePROXY(); err != nil {
				return err
			}
		case "saml":
			if err := p.validateSAML(); err != nil {
				return err
			}
		case "managed":
			if err := p.validateManaged(); err != nil {
				return err
			}
		default:
			return fmt.Errorf("unsupported provider kind %q", p.Kind)
		}
	}

	return nil
}

func parsePeer(raw string) (netip.Prefix, error) {
	if p, e := netip.ParsePrefix(raw); e == nil {
		return p, nil
	}

	a, e := netip.ParseAddr(raw)
	if e != nil {
		return netip.Prefix{}, errors.New("invalid trusted proxy peer")
	}

	return netip.PrefixFrom(a, a.BitLen()), nil
}

func readSecret(path string) (string, error) {
	if path == "" {
		return "", nil
	}

	//nolint:gosec // Secret paths come only from owner-supplied deployment configuration.
	b, e := os.ReadFile(path)
	if e != nil {
		return "", errors.New("cannot read provider secret file")
	}

	if len(b) > 65536 {
		return "", errors.New("provider secret file too large")
	}

	return strings.TrimRight(string(b), "\r\n"), nil
}

func (p Provider) validateOIDC() error {
	if p.OIDC == nil || !secureURL(p.OIDC.Issuer) || p.OIDC.ClientID == "" {
		return fmt.Errorf("provider %s requires HTTPS issuer and client ID", p.ID)
	}

	return nil
}

func (p Provider) validateLDAP() error {
	if p.LDAP == nil {
		return errors.New("LDAP configuration required")
	}

	l := p.LDAP

	u, e := url.Parse(l.URL)
	if e != nil || (u.Scheme != "ldaps" && u.Scheme != "ldap") || u.Host == "" || u.User != nil {
		return errors.New("LDAP requires ldaps:// or ldap:// with mandatory StartTLS")
	}

	if l.UserBase == "" || !attribute.MatchString(l.LoginAttribute) || !attribute.MatchString(l.IDAttribute) {
		return errors.New("LDAP requires user base and valid login/immutable ID attributes")
	}

	for _, a := range []string{l.DisplayAttribute, l.EmailAttribute, l.GroupAttribute} {
		if a != "" && !attribute.MatchString(a) {
			return errors.New("invalid LDAP attribute")
		}
	}

	if l.BindDN != "" && l.BindPasswordFile == "" {
		return errors.New("LDAP bind identity requires secret file")
	}

	return nil
}

func (p Provider) validatePROXY() error {
	if p.Proxy == nil || len(p.Proxy.TrustedPeers) == 0 || p.Proxy.SubjectHeader == "" {
		return errors.New("proxy requires explicit trusted peers and subject header")
	}

	for _, s := range p.Proxy.TrustedPeers {
		if _, e := parsePeer(s); e != nil {
			return e
		}
	}

	for _, s := range []string{p.Proxy.SubjectHeader, p.Proxy.DisplayHeader, p.Proxy.EmailHeader} {
		if s != "" && (http.CanonicalHeaderKey(s) == "" || strings.ContainsAny(s, " \r\n:")) {
			return errors.New("invalid proxy identity header")
		}
	}

	return nil
}

func (p Provider) validateSAML() error {
	if p.SAML == nil {
		return errors.New("SAML configuration required")
	}

	s := p.SAML
	if (s.MetadataFile == "") == (s.MetadataURL == "") || s.CertificateFile == "" || s.PrivateKeyFile == "" {
		return errors.New("SAML requires one metadata source and SP certificate/key files")
	}

	if s.MetadataURL != "" && !secureURL(s.MetadataURL) {
		return errors.New("SAML metadata URL must use HTTPS")
	}

	if strings.Contains(strings.ToLower(s.SubjectAttribute), "mail") && !s.AllowMutableSubject {
		return errors.New("mutable SAML email subject requires allow_mutable_subject acknowledgement")
	}

	return nil
}

// SAMLACSPaths lists only enabled assertion consumers for the server's narrow cross-site POST exception.
func (c Config) SAMLACSPaths() []string {
	out := []string{}

	for _, p := range c.Providers {
		if !p.Disabled && p.Kind == "saml" {
			out = append(out, "/api/v1/auth/"+p.ID+"/acs")
		}
	}

	return out
}
