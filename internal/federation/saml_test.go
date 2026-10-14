package federation

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"encoding/xml"
	"math/big"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/beevik/etree"
	"github.com/crewjam/saml"
	dsig "github.com/russellhaering/goxmldsig"
)

func samlTestProvider(t *testing.T) (Provider, *rsa.PrivateKey, []byte) {
	t.Helper()

	key, e := rsa.GenerateKey(rand.Reader, 2048)
	if e != nil {
		t.Fatal(e)
	}

	template := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "test"}, NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(24 * time.Hour), KeyUsage: x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment}

	cert, e := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if e != nil {
		t.Fatal(e)
	}

	dir := t.TempDir()
	certFile := filepath.Join(dir, "cert.pem")
	keyFile := filepath.Join(dir, "key.pem")
	metadataFile := filepath.Join(dir, "metadata.xml")

	if e = os.WriteFile(certFile, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: cert}), 0o600); e != nil {
		t.Fatal(e)
	}

	if e = os.WriteFile(keyFile, pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)}), 0o600); e != nil {
		t.Fatal(e)
	}

	metadata := saml.EntityDescriptor{EntityID: "https://idp.example/metadata", ValidUntil: time.Now().Add(time.Hour), IDPSSODescriptors: []saml.IDPSSODescriptor{{ProtocolSupportEnumeration: "urn:oasis:names:tc:SAML:2.0:protocol", KeyDescriptors: []saml.KeyDescriptor{{Use: "signing", KeyInfo: saml.KeyInfo{X509Data: saml.X509Data{X509Certificates: []saml.X509Certificate{{Data: base64.StdEncoding.EncodeToString(cert)}}}}}}, SingleSignOnServices: []saml.Endpoint{{Binding: saml.HTTPRedirectBinding, Location: "https://idp.example/sso"}}}}}

	data, e := xml.Marshal(metadata)
	if e != nil {
		t.Fatal(e)
	}

	if e = os.WriteFile(metadataFile, data, 0o600); e != nil {
		t.Fatal(e)
	}

	return Provider{ID: "saml", Kind: "saml", DisplayName: "SAML", SAML: &SAMLConfig{MetadataFile: metadataFile, CertificateFile: certFile, PrivateKeyFile: keyFile}}, key, cert
}

func TestSAMLProtocolValidation(t *testing.T) {
	p, key, cert := samlTestProvider(t)
	for _, scenario := range []string{"valid", "unsigned", "tampered", "wrong_issuer", "wrong_audience", "wrong_recipient", "wrong_destination", "wrong_request", "expired", "transient_subject", "native_valid", "native_tampered"} {
		t.Run(scenario, func(t *testing.T) {
			h, f := testHandler(t, p)
			native := strings.HasPrefix(scenario, "native_")
			scenario = strings.TrimPrefix(scenario, "native_")

			begin := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/api/v1/auth/saml/login", strings.NewReader(`{}`))
			begin.Header.Set("Origin", "https://recipes.example")

			if native {
				begin = httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/api/v1/auth/saml/native?"+nativeEntryQuery(testVerifier), nil)
			}

			started := httptest.NewRecorder()
			h.ServeHTTP(started, begin)

			var start map[string]string

			if native {
				if started.Code != http.StatusSeeOther {
					t.Fatal(started.Code, started.Body.String())
				}

				start = map[string]string{"authorization_url": started.Header().Get("Location")}
			} else {
				if started.Code != 200 {
					t.Fatal(started.Code, started.Body.String())
				}

				if e := json.Unmarshal(started.Body.Bytes(), &start); e != nil {
					t.Fatal(e)
				}
			}

			authorization, e := url.Parse(start["authorization_url"])
			if e != nil {
				t.Fatal(e)
			}

			state := authorization.Query().Get("RelayState")
			if state == "" || authorization.Query().Get("SAMLRequest") == "" {
				t.Fatal("missing Redirect request")
			}

			var txn transaction
			if e = json.Unmarshal([]byte(f.transactions[state].payload), &txn); e != nil {
				t.Fatal(e)
			}

			now := time.Now().UTC()
			acs := "https://recipes.example/api/v1/auth/saml/acs"
			a := &saml.Assertion{ID: "assertion-one", IssueInstant: now, Version: "2.0", Issuer: saml.Issuer{Value: "https://idp.example/metadata"}, Subject: &saml.Subject{NameID: &saml.NameID{Format: string(saml.PersistentNameIDFormat), Value: "stable-subject"}, SubjectConfirmations: []saml.SubjectConfirmation{{Method: "urn:oasis:names:tc:SAML:2.0:cm:bearer", SubjectConfirmationData: &saml.SubjectConfirmationData{Recipient: acs, InResponseTo: txn.RequestID, NotOnOrAfter: now.Add(time.Minute)}}}}, Conditions: &saml.Conditions{NotBefore: now.Add(-time.Minute), NotOnOrAfter: now.Add(time.Minute), AudienceRestrictions: []saml.AudienceRestriction{{Audience: saml.Audience{Value: "https://recipes.example/api/v1/auth/saml/metadata"}}}}}
			response := saml.Response{ID: "response-one", Version: "2.0", IssueInstant: now, Destination: acs, InResponseTo: txn.RequestID, Issuer: &saml.Issuer{Value: "https://idp.example/metadata"}, Status: saml.Status{StatusCode: saml.StatusCode{Value: saml.StatusSuccess}}, Assertion: a}

			switch scenario {
			case "wrong_issuer":
				a.Issuer.Value = "https://evil.example"
			case "wrong_audience":
				a.Conditions.AudienceRestrictions[0].Audience.Value = "https://other.example"
			case "wrong_recipient":
				a.Subject.SubjectConfirmations[0].SubjectConfirmationData.Recipient = "https://other.example/acs"
			case "wrong_destination":
				response.Destination = "https://other.example/acs"
			case "wrong_request":
				response.InResponseTo = "another-request"
			case "expired":
				a.Conditions.NotOnOrAfter = now.Add(-time.Hour)
			case "transient_subject":
				a.Subject.NameID.Format = "urn:oasis:names:tc:SAML:2.0:nameid-format:transient"
			}

			root := response.Element()

			if scenario != "unsigned" {
				signer := dsig.NewDefaultSigningContext(dsig.TLSCertKeyStore(tls.Certificate{Certificate: [][]byte{cert}, PrivateKey: key}))
				signer.IdAttribute = "ID"
				signer.Prefix = "ds"

				signer.Canonicalizer = dsig.MakeC14N10ExclusiveCanonicalizerWithPrefixList("")
				if e = signer.SetSignatureMethod(dsig.RSASHA256SignatureMethod); e != nil {
					t.Fatal(e)
				}

				root, e = signer.SignEnveloped(root)
				if e != nil {
					t.Fatal(e)
				}
			}

			doc := etree.NewDocument()
			doc.SetRoot(root)

			raw, e := doc.WriteToBytes()
			if e != nil {
				t.Fatal(e)
			}

			if scenario == "tampered" {
				raw = []byte(strings.ReplaceAll(string(raw), "stable-subject", "attacker"))
			}

			form := url.Values{"RelayState": {state}, "SAMLResponse": {base64.StdEncoding.EncodeToString(raw)}}
			request := httptest.NewRequestWithContext(t.Context(), http.MethodPost, acs, strings.NewReader(form.Encode()))
			request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			request.AddCookie(started.Result().Cookies()[0])

			result := httptest.NewRecorder()
			h.ServeHTTP(result, request)

			if native {
				back := nativeReturnQuery(t, result)
				if scenario == "valid" && (back.Get("code") == "" || len(f.grants) != 1 || f.subjects[0] != "saml:stable-subject") {
					t.Fatalf("valid native assertion: %v", back)
				}

				if scenario != "valid" && (back.Get("error") != "authentication_failed" || len(f.grants) != 0) {
					t.Fatalf("invalid native assertion: %v", back)
				}

				return
			}

			if scenario == "valid" {
				if result.Code != 303 || len(f.subjects) != 1 || f.subjects[0] != "saml:stable-subject" {
					t.Fatalf("valid assertion rejected: %d %s", result.Code, result.Body.String())
				}

				if e = f.ClaimFederationAssertion(request.Context(), p.ID, "assertion-one", now.Add(time.Hour)); e == nil {
					t.Fatal("assertion replay accepted")
				}
			} else if result.Code != 401 || len(f.subjects) != 0 {
				t.Fatalf("invalid assertion accepted: %d %v", result.Code, f.subjects)
			}
		})
	}
}

func TestSAMLMetadataRefreshReplacesSigningKeys(t *testing.T) {
	p, _, _ := samlTestProvider(t)
	h, _ := testHandler(t, p)
	r := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "https://recipes.example/api/v1/auth/saml/metadata", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)

	if w.Code != 200 || !strings.Contains(w.Body.String(), "AssertionConsumerService") {
		t.Fatal(w.Code, w.Body.String())
	}

	before, err := h.samlProvider(r, p)
	if err != nil {
		t.Fatal(err)
	}

	removed := before.IDPMetadata.IDPSSODescriptors[0].KeyDescriptors[0].KeyInfo.X509Data.X509Certificates[0].Data

	h.metadataCache[p.ID] = samlMetadataCache{}

	newer, _, _ := samlTestProvider(t)

	data, e := os.ReadFile(newer.SAML.MetadataFile)
	if e != nil {
		t.Fatal(e)
	}

	//nolint:gosec // Both metadata paths come from this test's temporary directory.
	if e = os.WriteFile(p.SAML.MetadataFile, data, 0o600); e != nil {
		t.Fatal(e)
	}

	sp, e := h.samlProvider(r, p)
	if e != nil {
		t.Fatal(e)
	}

	keys := sp.IDPMetadata.IDPSSODescriptors[0].KeyDescriptors
	if len(keys) != 1 || keys[0].KeyInfo.X509Data.X509Certificates[0].Data == removed {
		t.Fatal("refreshed metadata did not replace the signing key")
	}
}
