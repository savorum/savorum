// Package connect provides optional outbound remote access.
package connect

import (
	"bufio"
	"context"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"

	"golang.org/x/crypto/acme"
	"golang.org/x/crypto/acme/autocert"
)

// CertificateStatus reports TLS readiness and expiry.
type CertificateStatus struct {
	Ready    bool
	NotAfter time.Time
}

// Agent terminates tunneled TLS and serves requests through Handler or LocalURL.
type Agent struct {
	Handler           http.Handler
	ConnectionStatus  func(bool)
	RelayURL          string
	InstanceID        string
	Credential        string
	RemoteOrigin      string
	LocalURL          string
	TLSConfig         *tls.Config
	RelayTLSConfig    *tls.Config
	Client            *http.Client
	ChallengeHandler  http.Handler
	CertificateFile   string
	KeyFile           string
	StateDirectory    string
	ACMEEmail         string
	CertificateStatus func(CertificateStatus)
	acmeClient        *acme.Client
	trustRoots        *x509.CertPool
}

func agentRunID() (string, error) {
	var data [32]byte
	if _, err := rand.Read(data[:]); err != nil {
		return "", fmt.Errorf("create Connect agent run: %w", err)
	}

	return base64.RawURLEncoding.EncodeToString(data[:]), nil
}

func (a Agent) validate() (*url.URL, *url.URL, *url.URL, error) {
	relay, e := url.Parse(a.RelayURL)
	if e != nil || relay.Host == "" || relay.User != nil || relay.Path != "" || relay.RawQuery != "" || relay.ForceQuery || relay.Fragment != "" || (relay.Scheme != "https" && (relay.Scheme != "http" || !isLoopback(relay.Hostname()))) {
		return nil, nil, nil, errors.New("relay must be an HTTPS origin or loopback HTTP origin")
	}

	local, e := url.Parse(a.LocalURL)
	if a.Handler == nil && (e != nil || local.Scheme != "http" || !isLoopback(local.Hostname()) || local.User != nil || local.Path != "" || local.RawQuery != "" || local.Fragment != "") {
		return nil, nil, nil, errors.New("local server must be a loopback HTTP origin")
	}

	remote, e := url.Parse(a.RemoteOrigin)
	if e != nil || remote.Scheme != "https" || remote.User != nil || remote.Path != "" || remote.RawQuery != "" || remote.ForceQuery || remote.Fragment != "" || remote.Port() != "" || !validRemoteHostname(remote.Hostname()) {
		return nil, nil, nil, errors.New("remote origin must be this instance's HTTPS hostname")
	}

	if a.InstanceID == "" || a.Credential == "" {
		return nil, nil, nil, errors.New("instance and credential are required")
	}

	return relay, local, remote, nil
}

// Run maintains outbound control polls and accepts encrypted client streams.
func (a Agent) Run(ctx context.Context) error {
	relay, local, remote, err := a.validate()
	if err != nil {
		return err
	}

	runID, err := agentRunID()
	if err != nil {
		return err
	}

	client := a.Client
	if client == nil {
		client = &http.Client{Timeout: 15 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }, Transport: &http.Transport{Proxy: nil, MaxIdleConnsPerHost: 8}}
	}

	if a.Handler == nil {
		if err = checkLocalServer(ctx, client, local); err != nil {
			return err
		}
	}

	challengeHandler := a.ChallengeHandler
	if a.TLSConfig == nil {
		a.TLSConfig, challengeHandler, err = a.certificateConfig(remote.Hostname())
		if err != nil {
			return err
		}
	}

	config := a.clientTLSConfig(remote.Hostname())

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	listener := newAgentListener()

	var challengeListener *agentListener

	var live sync.Map

	proxy := httputil.ReverseProxy{Rewrite: func(pr *httputil.ProxyRequest) {
		pr.Out.URL.Scheme = "http"
		pr.Out.URL.Host = local.Host

		pr.Out.Host = pr.In.Host
		for _, key := range []string{"Forwarded", "X-Forwarded-For", "X-Forwarded-Host", "X-Forwarded-Proto", "X-Forwarded-Port", "X-Real-IP"} {
			pr.Out.Header.Del(key)
		}
	}, ModifyResponse: func(response *http.Response) error {
		response.Header.Set("Strict-Transport-Security", "max-age=63072000; includeSubDomains")
		return nil
	}, Transport: &http.Transport{Proxy: nil, DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
		return (&net.Dialer{Timeout: 5 * time.Second}).DialContext(ctx, "tcp", local.Host)
	}, MaxIdleConnsPerHost: 32}, FlushInterval: -1, ErrorHandler: func(w http.ResponseWriter, _ *http.Request, _ error) {
		w.Header().Set("Strict-Transport-Security", "max-age=63072000; includeSubDomains")
		http.Error(w, "Bad Gateway", http.StatusBadGateway)
	}}

	var handler http.Handler = &proxy
	if a.Handler != nil {
		handler = a.Handler
	}

	protected := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Host != remote.Host {
			http.Error(w, "Misdirected Request", http.StatusMisdirectedRequest)
			return
		}

		for _, key := range []string{"Forwarded", "X-Forwarded-For", "X-Forwarded-Host", "X-Forwarded-Proto", "X-Forwarded-Port", "X-Real-IP"} {
			r.Header.Del(key)
		}

		w.Header().Set("Strict-Transport-Security", "max-age=63072000; includeSubDomains")
		handler.ServeHTTP(w, r)
	})
	server := &http.Server{Handler: protected, ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 30 * time.Second, IdleTimeout: time.Minute, MaxHeaderBytes: 64 << 10}

	serveDone := make(chan error, 1)
	go func() { serveDone <- server.Serve(listener) }()

	var challengeDone chan error

	if challengeHandler != nil {
		challengeListener = newAgentListener()
		challengeDone = make(chan error, 1)

		challengeServer := &http.Server{Handler: challengeHandler, ReadHeaderTimeout: 5 * time.Second, MaxHeaderBytes: 8 << 10}
		go func() { challengeDone <- challengeServer.Serve(challengeListener) }()
	}

	var workers sync.WaitGroup

	attachments := make(chan struct{}, 32)

	results := make(chan error, 4)
	base := strings.TrimRight(relay.String(), "/") + "/connect/v1/agent/" + url.PathEscape(a.InstanceID)

	for range 4 {
		workers.Go(func() {
			results <- a.poll(ctx, client, relay, base, runID, config, listener, challengeListener, &live, attachments)
		})
	}

	var result error
	select {
	case <-ctx.Done():
	case result = <-results:
	}

	cancel()

	_ = listener.Close()
	if challengeListener != nil {
		_ = challengeListener.Close()
	}

	live.Range(func(key, _ any) bool {
		if conn, ok := key.(net.Conn); ok {
			_ = conn.Close()
		}

		return true
	})
	workers.Wait()

	_ = server.Close()

	<-serveDone

	if challengeDone != nil {
		<-challengeDone
	}

	if ctx.Err() != nil && result == nil {
		return nil
	}

	return result
}

func (a Agent) clientTLSConfig(host string) *tls.Config {
	config := a.TLSConfig.Clone()
	config.MinVersion = tls.VersionTLS12

	config.NextProtos = []string{"http/1.1"}
	if a.StateDirectory != "" {
		config.NextProtos = append(config.NextProtos, acme.ALPNProto)
	}

	reportCertificate := a.certificateReporter()
	reportCertificate(CertificateStatus{})

	if len(config.Certificates) != 0 && len(config.Certificates[0].Certificate) != 0 {
		leaf, err := x509.ParseCertificate(config.Certificates[0].Certificate[0])
		if err == nil {
			reportCertificate(CertificateStatus{Ready: time.Now().Before(leaf.NotAfter) && !time.Now().Before(leaf.NotBefore), NotAfter: leaf.NotAfter})
		}
	}

	original := config.GetCertificate
	config.GetCertificate = func(hello *tls.ClientHelloInfo) (*tls.Certificate, error) {
		if hello.ServerName != host {
			return nil, errors.New("unregistered TLS hostname")
		}

		var (
			cert    *tls.Certificate
			certErr error
		)

		switch {
		case original != nil:
			cert, certErr = original(hello)
		case len(config.Certificates) != 0:
			cert = &config.Certificates[0]
		default:
			return nil, errors.New("TLS certificate unavailable")
		}

		if certErr != nil {
			reportCertificate(CertificateStatus{})
			return nil, certErr
		}

		if cert == nil || len(cert.Certificate) == 0 {
			reportCertificate(CertificateStatus{})
			return nil, errors.New("TLS certificate unavailable")
		}

		leaf, parseErr := x509.ParseCertificate(cert.Certificate[0])
		if parseErr != nil || leaf.VerifyHostname(host) != nil || time.Now().Before(leaf.NotBefore) || !time.Now().Before(leaf.NotAfter) {
			reportCertificate(CertificateStatus{})
			return nil, errors.New("TLS certificate is invalid for registered hostname")
		}

		reportCertificate(CertificateStatus{Ready: true, NotAfter: leaf.NotAfter})

		return cert, nil
	}

	return config
}

func (a Agent) certificateReporter() func(CertificateStatus) {
	var (
		mu       sync.Mutex
		previous CertificateStatus
	)

	initialized := false

	return func(status CertificateStatus) {
		mu.Lock()
		defer mu.Unlock()

		if initialized && status == previous {
			return
		}

		initialized = true

		previous = status
		if a.CertificateStatus != nil {
			a.CertificateStatus(status)
		}
	}
}

func (a Agent) certificateConfig(host string) (*tls.Config, http.Handler, error) {
	if a.CertificateFile != "" || a.KeyFile != "" {
		if a.CertificateFile == "" || a.KeyFile == "" || a.StateDirectory != "" {
			return nil, nil, errors.New("choose a certificate pair or ACME state directory")
		}

		keyInfo, err := os.Lstat(a.KeyFile)
		if err != nil {
			return nil, nil, fmt.Errorf("prepare local TLS: %w", err)
		}

		if !keyInfo.Mode().IsRegular() || keyInfo.Mode().Perm()&0o077 != 0 {
			return nil, nil, errors.New("TLS private key file must be regular and owner-only")
		}

		cert, err := tls.LoadX509KeyPair(a.CertificateFile, a.KeyFile)
		if err != nil {
			return nil, nil, fmt.Errorf("prepare local TLS: %w", err)
		}

		config, err := CertificateForHost(cert, host)
		if err != nil {
			return nil, nil, fmt.Errorf("prepare local TLS: %w", err)
		}

		intermediates := x509.NewCertPool()

		for _, encoded := range cert.Certificate[1:] {
			intermediate, parseErr := x509.ParseCertificate(encoded)
			if parseErr != nil {
				return nil, nil, fmt.Errorf("invalid TLS certificate chain: %w", parseErr)
			}

			intermediates.AddCert(intermediate)
		}

		if _, err = config.Certificates[0].Leaf.Verify(x509.VerifyOptions{DNSName: host, Roots: a.trustRoots, Intermediates: intermediates, KeyUsages: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}}); err != nil {
			return nil, nil, fmt.Errorf("TLS certificate chain is not trusted: %w", err)
		}

		return config, nil, nil
	}

	if a.StateDirectory == "" {
		return nil, nil, errors.New("certificate pair or ACME state directory is required")
	}

	if err := os.MkdirAll(a.StateDirectory, 0o700); err != nil {
		return nil, nil, fmt.Errorf("prepare local TLS: %w", err)
	}

	info, err := os.Stat(a.StateDirectory)
	if err != nil {
		return nil, nil, fmt.Errorf("prepare local TLS: %w", err)
	}

	if !info.IsDir() || info.Mode().Perm()&0o077 != 0 {
		return nil, nil, errors.New("ACME state directory must be owner-only")
	}

	manager := &autocert.Manager{Cache: autocert.DirCache(a.StateDirectory), Prompt: autocert.AcceptTOS, HostPolicy: autocert.HostWhitelist(host), Email: a.ACMEEmail, Client: a.acmeClient}

	return &tls.Config{GetCertificate: manager.GetCertificate}, manager.HTTPHandler(http.NotFoundHandler()), nil
}

func (a Agent) poll(ctx context.Context, client *http.Client, relay *url.URL, base, runID string, config *tls.Config, listener, challengeListener *agentListener, live *sync.Map, attachments chan struct{}) error {
	var tunnels sync.WaitGroup
	defer tunnels.Wait()

	for ctx.Err() == nil {
		req, e := http.NewRequestWithContext(ctx, http.MethodGet, base+"/next", nil)
		if e != nil {
			return fmt.Errorf("prepare agent poll: %w", e)
		}

		req.Header.Set("Authorization", "Bearer "+a.Credential)
		req.Header.Set("X-Savorum-Agent-Run", runID)

		response, e := client.Do(req)
		if e != nil {
			if ctx.Err() != nil {
				return nil
			}

			if a.ConnectionStatus != nil {
				a.ConnectionStatus(false)
			}

			pause(ctx)

			continue
		}

		if a.ConnectionStatus != nil {
			a.ConnectionStatus(response.StatusCode == http.StatusNoContent || response.StatusCode == http.StatusOK)
		}

		if response.StatusCode == http.StatusNoContent {
			_ = response.Body.Close()
			continue
		}

		if response.StatusCode == http.StatusUnauthorized || response.StatusCode == http.StatusPaymentRequired {
			_ = response.Body.Close()
			return fmt.Errorf("connect agent access denied: %d", response.StatusCode)
		}

		if response.StatusCode != http.StatusOK {
			_ = response.Body.Close()

			if a.ConnectionStatus != nil {
				a.ConnectionStatus(false)
			}

			pause(ctx)

			continue
		}

		var work assignment

		e = json.NewDecoder(io.LimitReader(response.Body, 4096)).Decode(&work)
		_ = response.Body.Close()

		if e != nil || work.ID == "" {
			pause(ctx)
			continue
		}

		select {
		case attachments <- struct{}{}:
		default:
			continue
		}

		tunnels.Go(func() {
			release := sync.OnceFunc(func() { <-attachments })
			a.attach(ctx, relay, base, runID, work, config, listener, challengeListener, live, release)
		})
	}

	return nil
}

func (a Agent) attach(ctx context.Context, relay *url.URL, base, runID string, work assignment, config *tls.Config, listener, challengeListener *agentListener, live *sync.Map, release func()) {
	submitted := false
	defer func() {
		if !submitted {
			release()
		}
	}()

	if (work.Kind != "tls" && work.Kind != "challenge") || (work.Kind == "challenge" && challengeListener == nil) {
		return
	}

	conn, err := dialControl(ctx, relay, a.RelayTLSConfig)
	if err != nil {
		return
	}

	stopClose := context.AfterFunc(ctx, func() { _ = conn.Close() })
	defer stopClose()

	_ = conn.SetDeadline(time.Now().Add(10 * time.Second))

	path := strings.TrimPrefix(base, strings.TrimRight(relay.String(), "/")) + "/attach/" + url.PathEscape(work.ID)

	_, err = fmt.Fprintf(conn, "GET %s HTTP/1.1\r\nHost: %s\r\nAuthorization: Bearer %s\r\nX-Savorum-Agent-Run: %s\r\nConnection: Upgrade\r\nUpgrade: savorum-connect/1\r\n\r\n", path, relay.Host, a.Credential, runID)
	if err != nil {
		_ = conn.Close()
		return
	}

	reader := bufio.NewReader(conn)
	request := &http.Request{Method: http.MethodGet}

	response, err := http.ReadResponse(reader, request)
	if err != nil {
		_ = conn.Close()
		return
	}

	if response.StatusCode != http.StatusSwitchingProtocols {
		_ = response.Body.Close()
		_ = conn.Close()

		return
	}

	_ = conn.SetDeadline(time.Time{})

	tracked := &trackedConn{Conn: conn, onClose: func() { live.Delete(conn); release() }}
	live.Store(conn, struct{}{})
	tracked.bindContext(ctx)

	upstream := net.Conn(&bufferedConn{Conn: tracked, reader: reader})
	selected := listener

	if work.Kind == "tls" {
		upstream = tls.Server(upstream, config)
	} else {
		selected = challengeListener
	}

	if !selected.submit(ctx, upstream) {
		_ = tracked.Close()
		return
	}

	submitted = true
}

func dialControl(ctx context.Context, origin *url.URL, tlsConfig *tls.Config) (net.Conn, error) {
	address := origin.Host
	if origin.Port() == "" {
		if origin.Scheme == "https" {
			address = net.JoinHostPort(origin.Hostname(), "443")
		} else {
			address = net.JoinHostPort(origin.Hostname(), "80")
		}
	}

	conn, err := (&net.Dialer{Timeout: 5 * time.Second}).DialContext(ctx, "tcp", address)
	if err != nil {
		return nil, fmt.Errorf("prepare Connect connection: %w", err)
	}

	if origin.Scheme != "https" {
		return conn, nil
	}

	config := &tls.Config{MinVersion: tls.VersionTLS12, ServerName: origin.Hostname(), NextProtos: []string{"http/1.1"}}
	if tlsConfig != nil {
		config = tlsConfig.Clone()
		config.ServerName = origin.Hostname()
		config.NextProtos = []string{"http/1.1"}
	}

	secured := tls.Client(conn, config)
	if err = secured.HandshakeContext(ctx); err != nil {
		_ = conn.Close()
		return nil, fmt.Errorf("prepare Connect connection: %w", err)
	}

	return secured, nil
}

func checkLocalServer(ctx context.Context, client *http.Client, local *url.URL) error {
	probe := *local
	probe.Path = "/api/v1/account"

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, probe.String(), nil)
	if err != nil {
		return fmt.Errorf("prepare local server probe: %w", err)
	}

	response, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("check local Savorum server: %w", err)
	}
	defer response.Body.Close()

	if response.StatusCode != http.StatusOK || !strings.HasPrefix(response.Header.Get("Content-Type"), "application/json") {
		return errors.New("local Savorum account endpoint is unavailable")
	}

	var account struct {
		AuthEnabled bool `json:"auth_enabled"`
	}
	if err := json.NewDecoder(io.LimitReader(response.Body, 1<<16)).Decode(&account); err != nil || !account.AuthEnabled {
		return errors.New("local Savorum server must require authentication")
	}

	return nil
}

func isLoopback(host string) bool {
	ip := net.ParseIP(host)
	return host == "localhost" || (ip != nil && ip.IsLoopback())
}

func pause(ctx context.Context) {
	select {
	case <-ctx.Done():
	case <-time.After(time.Second):
	}
}

type agentListener struct {
	conns chan net.Conn
	done  chan struct{}
	once  sync.Once
	mu    sync.Mutex
}

func newAgentListener() *agentListener {
	return &agentListener{conns: make(chan net.Conn, 32), done: make(chan struct{})}
}

func (l *agentListener) Accept() (net.Conn, error) {
	select {
	case conn := <-l.conns:
		return conn, nil
	case <-l.done:
		return nil, net.ErrClosed
	}
}

func (l *agentListener) Close() error {
	l.once.Do(func() {
		l.mu.Lock()
		defer l.mu.Unlock()

		close(l.done)

		for {
			select {
			case conn := <-l.conns:
				_ = conn.Close()
			default:
				return
			}
		}
	})

	return nil
}
func (*agentListener) Addr() net.Addr { return &net.TCPAddr{IP: net.IPv4(127, 0, 0, 1)} }
func (l *agentListener) submit(ctx context.Context, conn net.Conn) bool {
	l.mu.Lock()
	defer l.mu.Unlock()

	if ctx.Err() != nil {
		return false
	}

	select {
	case <-l.done:
		return false
	default:
	}

	select {
	case l.conns <- conn:
		return true
	default:
		return false
	}
}

type trackedConn struct {
	net.Conn
	once       sync.Once
	onClose    func()
	mu         sync.Mutex
	stopCancel func() bool
}

func (c *trackedConn) bindContext(ctx context.Context) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.stopCancel = context.AfterFunc(ctx, func() { _ = c.Close() })
}

func (c *trackedConn) Close() error {
	c.once.Do(func() {
		c.mu.Lock()
		if c.stopCancel != nil {
			c.stopCancel()
		}
		c.mu.Unlock()
		c.onClose()
	})

	if err := c.Conn.Close(); err != nil {
		return fmt.Errorf("close agent flow: %w", err)
	}

	return nil
}

type bufferedConn struct {
	net.Conn
	reader *bufio.Reader
}

//nolint:wrapcheck // io.Reader requires an unwrapped io.EOF.
func (c *bufferedConn) Read(p []byte) (int, error) { return c.reader.Read(p) }

// CertificateForHost builds a TLS config after checking hostname and validity dates.
func CertificateForHost(cert tls.Certificate, host string) (*tls.Config, error) {
	if len(cert.Certificate) == 0 {
		return nil, errors.New("certificate chain is empty")
	}

	leaf, err := x509.ParseCertificate(cert.Certificate[0])
	if err != nil {
		return nil, fmt.Errorf("prepare Connect connection: %w", err)
	}

	if err = leaf.VerifyHostname(host); err != nil {
		return nil, fmt.Errorf("prepare Connect connection: %w", err)
	}

	now := time.Now()
	if now.Before(leaf.NotBefore) || !now.Before(leaf.NotAfter) {
		return nil, errors.New("TLS certificate is outside its validity period")
	}

	cert.Leaf = leaf

	return &tls.Config{MinVersion: tls.VersionTLS12, Certificates: []tls.Certificate{cert}}, nil
}

func validRemoteHostname(host string) bool {
	if host != strings.ToLower(host) || !strings.Contains(host, ".") || len(host) > 253 {
		return false
	}

	for label := range strings.SplitSeq(host, ".") {
		if len(label) == 0 || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
			return false
		}

		for _, c := range label {
			if (c < 'a' || c > 'z') && (c < '0' || c > '9') && c != '-' {
				return false
			}
		}
	}

	return true
}
