package federation

import (
	"context"
	"crypto"
	"crypto/tls"
	"crypto/x509"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/crewjam/saml"
	dsig "github.com/russellhaering/goxmldsig"
)

func (h *Handler) samlLocalProvider(ctx context.Context, p Provider) (*saml.ServiceProvider, error) {
	c := p.SAML

	pair, e := tls.LoadX509KeyPair(c.CertificateFile, c.PrivateKeyFile)
	if e != nil {
		return nil, errors.New("cannot load SAML SP key pair")
	}

	cert, e := x509.ParseCertificate(pair.Certificate[0])
	if e != nil {
		return nil, fmt.Errorf("parse SP certificate: %w", e)
	}

	key, ok := pair.PrivateKey.(crypto.Signer)
	if !ok {
		return nil, errors.New("invalid SP signer")
	}

	metadataURL, _ := url.Parse(h.endpoint(ctx, p, "metadata"))
	acs, _ := url.Parse(h.endpoint(ctx, p, "acs"))

	entityID := strings.TrimRight(h.options.PublicURL, "/") + "/api/v1/auth/" + p.ID + "/metadata"

	sp := &saml.ServiceProvider{EntityID: entityID, MetadataURL: *metadataURL, AcsURL: *acs, Key: key, Certificate: cert, AuthnNameIDFormat: saml.PersistentNameIDFormat, AllowIDPInitiated: false, SignatureMethod: dsig.RSASHA256SignatureMethod}

	return sp, nil
}

func (h *Handler) samlProvider(r *http.Request, p Provider) (*saml.ServiceProvider, error) {
	sp, err := h.samlLocalProvider(r.Context(), p)
	if err != nil {
		return nil, err
	}

	metadata, err := h.cachedSAMLMetadata(r, p)
	if err != nil {
		return nil, err
	}

	sp.IDPMetadata = metadata
	if !secureURL(sp.GetSSOBindingLocation(saml.HTTPRedirectBinding)) {
		return nil, errors.New("SAML Redirect SSO endpoint must use HTTPS")
	}

	return sp, nil
}

// Refresh attempts have no waiting queue and are spaced even after failures.
// Successful metadata is reused for five minutes, never beyond ValidUntil.
type samlMetadataCache struct {
	metadata   *saml.EntityDescriptor
	next       time.Time
	refreshing bool
}

func (h *Handler) cachedSAMLMetadata(r *http.Request, p Provider) (*saml.EntityDescriptor, error) {
	if err := r.Context().Err(); err != nil {
		return nil, err
	}

	now := time.Now()

	h.metadataMu.Lock()
	if h.metadataCache == nil {
		h.metadataCache = map[string]samlMetadataCache{}
	}

	entry := h.metadataCache[p.ID]
	if entry.refreshing || now.Before(entry.next) {
		h.metadataMu.Unlock()

		if entry.metadata != nil && (entry.metadata.ValidUntil.IsZero() || now.Before(entry.metadata.ValidUntil)) {
			return entry.metadata, nil
		}

		return nil, errors.New("SAML metadata refresh unavailable")
	}

	entry.refreshing = true
	entry.next = now.Add(time.Minute)
	h.metadataCache[p.ID] = entry
	h.metadataMu.Unlock()

	metadata, err := h.loadSAMLMetadata(r, p)
	if err == nil {
		err = h.acceptSAMLMetadata(p.ID, metadata)
	}

	h.metadataMu.Lock()

	entry.refreshing = false
	if err == nil {
		entry.metadata = metadata

		entry.next = time.Now().Add(5 * time.Minute)
		if !metadata.ValidUntil.IsZero() && metadata.ValidUntil.Before(entry.next) {
			entry.next = metadata.ValidUntil
			if floor := time.Now().Add(time.Minute); entry.next.Before(floor) {
				entry.next = floor
			}
		}
	}

	h.metadataCache[p.ID] = entry
	h.metadataMu.Unlock()

	return metadata, err
}

func (h *Handler) samlMetadata(w http.ResponseWriter, r *http.Request) {
	p, ok := h.providers[r.PathValue("provider")]
	if !ok || p.Kind != "saml" {
		http.NotFound(w, r)
		return
	}

	sp, e := h.samlLocalProvider(r.Context(), p)
	if e != nil {
		h.fail(w, r)
		return
	}

	data, e := xml.Marshal(sp.Metadata())
	if e != nil {
		h.fail(w, r)
		return
	}

	w.Header().Set("Content-Type", "application/samlmetadata+xml")
	// XML encoder escapes metadata; response is application/samlmetadata+xml with nosniff.
	_, _ = w.Write(data)
}

func (h *Handler) samlBegin(w http.ResponseWriter, r *http.Request, p Provider, t transaction) {
	location, e := h.samlAuthorization(w, r, p, t)
	if e != nil {
		h.fail(w, r)
		return
	}

	writeJSON(w, map[string]string{"authorization_url": location})
}

func (h *Handler) samlAuthorization(w http.ResponseWriter, r *http.Request, p Provider, t transaction) (string, error) {
	sp, e := h.samlProvider(r, p)
	if e != nil {
		return "", e
	}

	request, e := sp.MakeAuthenticationRequest(sp.GetSSOBindingLocation(saml.HTTPRedirectBinding), saml.HTTPRedirectBinding, saml.HTTPPostBinding)
	if e != nil {
		return "", fmt.Errorf("create SAML request: %w", e)
	}

	t.RequestID = request.ID

	state, e := h.saveTransaction(w, r, p, t)
	if e != nil {
		return "", e
	}

	location, e := request.Redirect(state, sp)
	if e != nil {
		return "", fmt.Errorf("encode SAML request: %w", e)
	}

	return location.String(), nil
}

func samlAttribute(a *saml.Assertion, name string) string {
	if name == "" {
		return ""
	}

	var found []string

	for _, statement := range a.AttributeStatements {
		for _, attr := range statement.Attributes {
			if attr.Name == name {
				for _, v := range attr.Values {
					found = append(found, v.Value)
				}
			}
		}
	}

	if len(found) != 1 {
		return ""
	}

	return found[0]
}

func (h *Handler) samlCallback(w http.ResponseWriter, r *http.Request) {
	p, ok := h.providers[r.PathValue("provider")]
	if !ok || p.Kind != "saml" {
		http.NotFound(w, r)
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	if r.ParseForm() != nil {
		h.fail(w, r)
		return
	}

	t, e := h.consume(w, r, p, r.PostForm.Get("RelayState"))
	if e != nil || t.RequestID == "" {
		h.fail(w, r)
		return
	}

	sp, e := h.samlProvider(r, p)
	if e != nil {
		h.failAfter(w, r, t, "authentication_failed")
		return
	}
	// Validate against the configured ACS, never forwarded or Host headers.
	parsed := r.Clone(r.Context())
	u := sp.AcsURL
	parsed.URL = &u

	assertion, e := sp.ParseResponse(parsed, []string{t.RequestID})
	if e != nil || assertion.Conditions == nil || assertion.ID == "" {
		h.failAfter(w, r, t, "authentication_failed")
		return
	}

	if e = h.store.ClaimFederationAssertion(r.Context(), p.ID, assertion.ID, assertion.Conditions.NotOnOrAfter.Add(5*time.Minute)); e != nil {
		h.failAfter(w, r, t, "authentication_failed")
		return
	}

	subject := samlAttribute(assertion, p.SAML.SubjectAttribute)
	if p.SAML.SubjectAttribute == "" && assertion.Subject != nil && assertion.Subject.NameID != nil && assertion.Subject.NameID.Format == string(saml.PersistentNameIDFormat) {
		subject = assertion.Subject.NameID.Value
	}

	groups := []string{}

	if p.SAML.GroupsAttribute != "" {
		for _, statement := range assertion.AttributeStatements {
			for _, attr := range statement.Attributes {
				if attr.Name == p.SAML.GroupsAttribute {
					for _, v := range attr.Values {
						groups = append(groups, v.Value)
					}
				}
			}
		}
	}

	h.complete(w, r, p, t, subject, samlAttribute(assertion, p.SAML.DisplayAttribute), samlAttribute(assertion, p.SAML.EmailAttribute), groups, true)
}

func (h *Handler) loadSAMLMetadata(r *http.Request, p Provider) (*saml.EntityDescriptor, error) {
	c := p.SAML

	var (
		data []byte
		e    error
	)

	if c.MetadataFile != "" {
		var file *os.File

		file, e = os.Open(c.MetadataFile)
		if e == nil {
			defer file.Close()

			var info os.FileInfo

			info, e = file.Stat()
			if e == nil && (!info.Mode().IsRegular() || info.Size() > 1<<20) {
				e = errors.New("invalid metadata file")
			}

			if e == nil {
				data, e = io.ReadAll(io.LimitReader(file, 1<<20+1))
			}
		}
	} else {
		req, err := http.NewRequestWithContext(r.Context(), http.MethodGet, c.MetadataURL, nil)
		if err != nil {
			return nil, fmt.Errorf("load SAML metadata: %w", err)
		}

		res, err := h.client.Do(req)
		if err != nil {
			return nil, fmt.Errorf("load SAML metadata: %w", err)
		}
		defer res.Body.Close()

		if res.StatusCode != http.StatusOK {
			return nil, errors.New("metadata unavailable")
		}

		data, e = io.ReadAll(io.LimitReader(res.Body, 1<<20+1))
	}

	if e != nil || len(data) > 1<<20 {
		return nil, errors.New("invalid metadata document")
	}

	var metadata saml.EntityDescriptor
	if e = xml.Unmarshal(data, &metadata); e != nil || metadata.EntityID == "" || len(metadata.IDPSSODescriptors) == 0 {
		return nil, errors.New("metadata requires one IdP EntityDescriptor")
	}

	if !metadata.ValidUntil.IsZero() && !metadata.ValidUntil.After(time.Now()) {
		return nil, errors.New("IdP metadata expired")
	}

	if err := h.store.BindFederationIssuer(r.Context(), p.ID, metadata.EntityID); err != nil {
		return nil, fmt.Errorf("bind SAML trust domain: %w", err)
	}

	return &metadata, nil
}

// acceptSAMLMetadata makes the freshly loaded metadata the only source of signing
// keys. A key it no longer lists stops verifying assertions immediately.
func (h *Handler) acceptSAMLMetadata(provider string, metadata *saml.EntityDescriptor) error {
	h.metadataMu.Lock()
	defer h.metadataMu.Unlock()

	if old := h.metadata[provider]; old != nil && old.EntityID != metadata.EntityID {
		return errors.New("SAML entity ID changed; configure a new provider")
	}

	h.metadata[provider] = metadata

	return nil
}
