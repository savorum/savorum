package connect

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"golang.org/x/crypto/acme"
)

func disposableCertificate(t *testing.T, host string) (tls.Certificate, *x509.CertPool) {
	t.Helper()
	return disposableCertificateAt(t, host, time.Now().Add(-time.Hour), time.Now().Add(time.Hour))
}

func disposableCertificateAt(t *testing.T, host string, notBefore, notAfter time.Time) (tls.Certificate, *x509.CertPool) {
	t.Helper()

	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}

	der, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}

	template := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: host}, DNSNames: []string{host}, NotBefore: notBefore, NotAfter: notAfter, KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}}

	certDER, err := x509.CreateCertificate(rand.Reader, template, template, key.Public(), key)
	if err != nil {
		t.Fatal(err)
	}

	cert, err := tls.X509KeyPair(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certDER}), pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: der}))
	if err != nil {
		t.Fatal(err)
	}

	pool := x509.NewCertPool()

	parsed, err := x509.ParseCertificate(certDER)
	if err != nil {
		t.Fatal(err)
	}

	pool.AddCert(parsed)

	return cert, pool
}

func TestCertificateMustMatchEnrolledHost(t *testing.T) {
	cert, _ := disposableCertificate(t, "other.connect.test")
	if _, err := CertificateForHost(cert, "chosen.connect.test"); err == nil {
		t.Fatal("mismatched certificate accepted")
	}
}

func TestCertificateAndKeyReadiness(t *testing.T) {
	host := "chosen.connect.test"

	cert, roots := disposableCertificate(t, host)
	for _, bounds := range [][2]time.Time{{time.Now().Add(-2 * time.Hour), time.Now().Add(-time.Hour)}, {time.Now().Add(time.Hour), time.Now().Add(2 * time.Hour)}} {
		invalid, _ := disposableCertificateAt(t, host, bounds[0], bounds[1])
		if _, err := CertificateForHost(invalid, host); err == nil {
			t.Fatal("certificate outside validity period accepted")
		}
	}

	dir := t.TempDir()
	keyPath := filepath.Join(dir, "key.pem")
	certPath := filepath.Join(dir, "cert.pem")

	keyDER, err := x509.MarshalECPrivateKey(fixtureECKey(t, cert))
	if err != nil {
		t.Fatal(err)
	}

	if err = os.WriteFile(keyPath, pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER}), 0o644); err != nil { // #nosec G306 -- The test verifies rejection of a world-readable disposable key.
		t.Fatal(err)
	}

	if err = os.WriteFile(certPath, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: cert.Certificate[0]}), 0o600); err != nil {
		t.Fatal(err)
	}

	a := Agent{KeyFile: keyPath, CertificateFile: certPath, trustRoots: roots}
	if _, _, err = a.certificateConfig(host); err == nil || !strings.Contains(err.Error(), "owner-only") {
		t.Fatalf("world-readable key accepted: %v", err)
	}

	if err = os.Chmod(keyPath, 0o600); err != nil {
		t.Fatal(err)
	}

	if _, _, err = a.certificateConfig(host); err != nil {
		t.Fatal(err)
	}

	a.trustRoots = nil
	if _, _, err = a.certificateConfig(host); err == nil {
		t.Fatal("untrusted certificate chain accepted")
	}

	a.trustRoots = roots

	if _, _, err = a.certificateConfig("other.connect.test"); err == nil {
		t.Fatal("mismatched certificate accepted")
	}
}

func TestACMECacheAndLocalCAFailure(t *testing.T) {
	host := "chosen.connect.test"
	cert, _ := disposableCertificateAt(t, host, time.Now().Add(-time.Hour), time.Now().Add(90*24*time.Hour))

	keyDER, err := x509.MarshalECPrivateKey(fixtureECKey(t, cert))
	if err != nil {
		t.Fatal(err)
	}

	state := t.TempDir()
	if err = os.Chmod(state, 0o700); err != nil { // #nosec G302 -- The directory must be traversable only by its owner.
		t.Fatal(err)
	}

	cache := append(pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER}), pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: cert.Certificate[0]})...)
	if err = os.WriteFile(filepath.Join(state, host), cache, 0o600); err != nil {
		t.Fatal(err)
	}

	localCA := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
	}))
	defer localCA.Close()

	client := &acme.Client{DirectoryURL: localCA.URL, RetryBackoff: func(int, *http.Request, *http.Response) time.Duration { return 0 }}
	a := Agent{StateDirectory: state, acmeClient: client}

	config, challenge, err := a.certificateConfig(host)
	if err != nil || challenge == nil {
		t.Fatalf("ACME setup: %v", err)
	}

	hello := &tls.ClientHelloInfo{ServerName: host, SupportedCurves: []tls.CurveID{tls.CurveP256}, CipherSuites: []uint16{tls.TLS_ECDHE_ECDSA_WITH_AES_128_GCM_SHA256}}

	loaded, err := config.GetCertificate(hello)
	if err != nil || len(loaded.Certificate) == 0 {
		t.Fatalf("cached certificate unavailable: %v", err)
	}

	if _, err = config.GetCertificate(&tls.ClientHelloInfo{ServerName: "other.connect.test"}); err == nil {
		t.Fatal("foreign hostname accepted by ACME policy")
	}

	missingState := t.TempDir()
	if err = os.Chmod(missingState, 0o700); err != nil { // #nosec G302 -- The directory must be traversable only by its owner.
		t.Fatal(err)
	}

	missing := Agent{StateDirectory: missingState, acmeClient: &acme.Client{DirectoryURL: localCA.URL, RetryBackoff: func(int, *http.Request, *http.Response) time.Duration { return 0 }}}

	failed, _, err := missing.certificateConfig(host)
	if err != nil {
		t.Fatal(err)
	}

	if _, err = failed.GetCertificate(hello); err == nil {
		t.Fatal("local CA failure did not block initial issuance")
	}

	soon, _ := disposableCertificateAt(t, host, time.Now().Add(-90*24*time.Hour), time.Now().Add(time.Hour))

	soonKey, err := x509.MarshalECPrivateKey(fixtureECKey(t, soon))
	if err != nil {
		t.Fatal(err)
	}

	renewState := t.TempDir()
	if err = os.Chmod(renewState, 0o700); err != nil { // #nosec G302 -- The directory must be traversable only by its owner.
		t.Fatal(err)
	}

	renewCache := append(pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: soonKey}), pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: soon.Certificate[0]})...)
	if err = os.WriteFile(filepath.Join(renewState, host), renewCache, 0o600); err != nil {
		t.Fatal(err)
	}

	var renewalCalls atomic.Int64

	renewCA := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		renewalCalls.Add(1)
		w.WriteHeader(http.StatusBadRequest)
	}))
	defer renewCA.Close()

	renewAgent := Agent{StateDirectory: renewState, acmeClient: &acme.Client{DirectoryURL: renewCA.URL, RetryBackoff: func(int, *http.Request, *http.Response) time.Duration { return 0 }}}

	renewConfig, _, err := renewAgent.certificateConfig(host)
	if err != nil {
		t.Fatal(err)
	}

	if _, err = renewConfig.GetCertificate(hello); err != nil {
		t.Fatalf("valid cached certificate unavailable during renewal: %v", err)
	}

	deadline := time.Now().Add(2 * time.Second)
	for renewalCalls.Load() == 0 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}

	if renewalCalls.Load() == 0 {
		t.Fatal("near-expiry certificate did not attempt renewal against local CA")
	}

	if _, err = renewConfig.GetCertificate(hello); err != nil {
		t.Fatalf("valid cached certificate lost after renewal failure: %v", err)
	}
}

func fixtureECKey(t *testing.T, cert tls.Certificate) *ecdsa.PrivateKey {
	t.Helper()

	key, ok := cert.PrivateKey.(*ecdsa.PrivateKey)
	if !ok {
		t.Fatal("fixture is not an EC key")
	}

	return key
}
