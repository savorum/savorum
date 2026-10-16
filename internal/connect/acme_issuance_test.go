package connect

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"math/big"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"golang.org/x/crypto/acme"
)

func TestACMELocalIssuanceAndRenewal(t *testing.T) {
	for _, renewal := range []bool{false, true} {
		t.Run(map[bool]string{false: "issuance", true: "renewal"}[renewal], func(t *testing.T) {
			host := "chosen.connect.test"

			key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
			if err != nil {
				t.Fatal(err)
			}

			root := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "Disposable ACME CA"}, NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(365 * 24 * time.Hour), IsCA: true, BasicConstraintsValid: true, KeyUsage: x509.KeyUsageCertSign}

			rootDER, err := x509.CreateCertificate(rand.Reader, root, root, key.Public(), key)
			if err != nil {
				t.Fatal(err)
			}

			roots := x509.NewCertPool()
			roots.AddCert(root)

			var (
				mu        sync.Mutex
				issued    []byte
				challenge http.Handler
				base      string
			)

			authorized := false

			fixture := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				mu.Lock()
				defer mu.Unlock()

				w.Header().Set("Replay-Nonce", base64.RawURLEncoding.EncodeToString([]byte(time.Now().String())))
				w.Header().Set("Content-Type", "application/json")

				respond := func(status int, value any) {
					w.WriteHeader(status)

					if err := json.NewEncoder(w).Encode(value); err != nil {
						t.Error(err)
					}
				}
				order := func(status string) map[string]any {
					return map[string]any{"status": status, "authorizations": []string{base + "/auth"}, "finalize": base + "/finalize", "certificate": base + "/cert"}
				}

				switch r.URL.Path {
				case "/directory":
					respond(200, map[string]string{"newNonce": base + "/nonce", "newAccount": base + "/account", "newOrder": base + "/new-order"})
				case "/nonce":
					w.WriteHeader(http.StatusOK)
				case "/account":
					w.Header().Set("Location", base+"/account/1")
					respond(201, map[string]string{"status": "valid"})
				case "/new-order":
					authorized = false

					w.Header().Set("Location", base+"/order")
					respond(201, order("pending"))
				case "/auth":
					status := "pending"
					if authorized {
						status = "valid"
					}

					respond(200, map[string]any{"status": status, "identifier": map[string]string{"type": "dns", "value": host}, "challenges": []map[string]string{{"type": "http-01", "url": base + "/challenge", "token": "local-token", "status": status}}})
				case "/challenge":
					request := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "http://"+host+"/.well-known/acme-challenge/local-token", nil)
					result := httptest.NewRecorder()
					challenge.ServeHTTP(result, request)

					if result.Code != 200 || result.Body.Len() == 0 {
						t.Error("ACME HTTP challenge missing")
						w.WriteHeader(http.StatusBadRequest)

						return
					}

					authorized = true

					respond(200, map[string]string{"type": "http-01", "status": "valid", "url": base + "/challenge", "token": "local-token"})
				case "/order":
					status := "ready"
					if issued != nil {
						status = "valid"
					}

					respond(200, order(status))
				case "/finalize":
					var jws struct {
						Payload string `json:"payload"`
					}
					if err := json.NewDecoder(r.Body).Decode(&jws); err != nil {
						t.Error(err)
						w.WriteHeader(http.StatusBadRequest)

						return
					}

					payload, err := base64.RawURLEncoding.DecodeString(jws.Payload)
					if err != nil {
						t.Error(err)
						w.WriteHeader(http.StatusBadRequest)

						return
					}

					var submission struct {
						CSR string `json:"csr"`
					}
					if err = json.Unmarshal(payload, &submission); err != nil {
						t.Error(err)
						w.WriteHeader(http.StatusBadRequest)

						return
					}

					csrDER, err := base64.RawURLEncoding.DecodeString(submission.CSR)
					if err != nil {
						t.Error(err)
						w.WriteHeader(http.StatusBadRequest)

						return
					}

					csr, err := x509.ParseCertificateRequest(csrDER)
					if err != nil || csr.CheckSignature() != nil || len(csr.DNSNames) != 1 || csr.DNSNames[0] != host || !authorized {
						t.Error("invalid or unauthorized CSR")
						w.WriteHeader(http.StatusBadRequest)

						return
					}

					leaf := &x509.Certificate{SerialNumber: big.NewInt(2), DNSNames: csr.DNSNames, NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(90 * 24 * time.Hour), ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}, KeyUsage: x509.KeyUsageDigitalSignature}

					der, err := x509.CreateCertificate(rand.Reader, leaf, root, csr.PublicKey, key)
					if err != nil {
						t.Error(err)
						w.WriteHeader(http.StatusBadRequest)

						return
					}

					issued = append(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: rootDER})...)

					respond(200, order("valid"))
				case "/cert":
					w.Header().Set("Content-Type", "application/pem-certificate-chain")

					if _, err := w.Write(issued); err != nil {
						t.Error(err)
					}
				default:
					t.Errorf("unexpected ACME path %s", r.URL.Path)
					w.WriteHeader(http.StatusNotFound)
				}
			}))
			defer fixture.Close()

			base = fixture.URL

			state := t.TempDir()
			if err = os.Chmod(state, 0o700); err != nil {
				t.Fatal(err)
			} // #nosec G302 -- Only the fixture owner may traverse private state.

			if renewal {
				old, _ := disposableCertificateAt(t, host, time.Now().Add(-90*24*time.Hour), time.Now().Add(time.Hour))

				der, err := x509.MarshalECPrivateKey(fixtureECKey(t, old))
				if err != nil {
					t.Fatal(err)
				}

				cache := append(pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: der}), pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: old.Certificate[0]})...)
				if err = os.WriteFile(filepath.Join(state, host), cache, 0o600); err != nil {
					t.Fatal(err)
				}
			}

			agent := Agent{StateDirectory: state, acmeClient: &acme.Client{DirectoryURL: base + "/directory"}}

			config, handler, err := agent.certificateConfig(host)
			if err != nil {
				t.Fatal(err)
			}

			challenge = handler
			hello := &tls.ClientHelloInfo{ServerName: host, SupportedCurves: []tls.CurveID{tls.CurveP256}, CipherSuites: []uint16{tls.TLS_ECDHE_ECDSA_WITH_AES_128_GCM_SHA256}}

			cert, err := config.GetCertificate(hello)
			if err != nil {
				t.Fatal(err)
			}

			deadline := time.Now().Add(5 * time.Second)
			for renewal && cert.Leaf.NotAfter.Before(time.Now().Add(24*time.Hour)) && time.Now().Before(deadline) {
				time.Sleep(10 * time.Millisecond)

				cert, err = config.GetCertificate(hello)
				if err != nil {
					t.Fatal(err)
				}
			}

			parsedRoot, err := x509.ParseCertificate(rootDER)
			if err != nil {
				t.Fatal(err)
			}

			roots = x509.NewCertPool()
			roots.AddCert(parsedRoot)

			if _, err = cert.Leaf.Verify(x509.VerifyOptions{Roots: roots, DNSName: host}); err != nil {
				t.Fatal("issued certificate unusable", err)
			}

			if cert.Leaf.NotAfter.Before(time.Now().Add(24 * time.Hour)) {
				t.Fatal("renewal did not replace certificate")
			}

			for _, name := range []string{host, "acme_account+key"} {
				info, err := os.Stat(filepath.Join(state, name))
				if err != nil || info.Mode().Perm()&0o077 != 0 {
					t.Fatal("private durable ACME state missing", name, err)
				}
			}
		})
	}
}
