// Package httpsecurity defines the HTTP trust boundary shared by every
// Savorum route. It keeps externally relevant limits and proxy handling in one
// place so adding a route cannot accidentally bypass the server baseline.
package httpsecurity

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"

	"golang.org/x/net/idna"
)

// Request limits bound bodies, headers, and each supported import envelope.
const (
	DefaultRequestBodyBytes int64 = 16 << 20
	AssetRequestBodyBytes   int64 = 64<<20 + 1
	ExternalImportBodyBytes int64 = 64 << 20
	PackageImportBodyBytes  int64 = 64 << 20
	MaximumHeaderBytes            = 64 << 10

	ReadHeaderTimeout    = 5 * time.Second
	ReadTimeout          = 30 * time.Second
	WriteTimeout         = 30 * time.Second
	IdleTimeout          = 2 * time.Minute
	maximumForwardedHops = 32
)

var forwardedHeaders = []string{
	"Forwarded",
	"X-Forwarded-For",
	"X-Forwarded-Host",
	"X-Forwarded-Proto",
	"X-Forwarded-Port",
	"X-Forwarded-Prefix",
	"X-Real-Ip",
}

type contextKey uint8

const (
	clientAddressKey contextKey = iota
	immediatePeerKey
)

// Options controls which immediate network peers may describe the original
// request. An empty list is deliberately safe: all forwarding metadata is
// discarded.
type Options struct {
	// SAMLACSPaths are exact configured ACS paths. Only POST may bypass Origin; SAML validates signatures and browser-bound transactions.
	SAMLACSPaths   []string
	TrustedProxies []netip.Prefix
	// ListenAddress is the server's configured host:port. When set, requests
	// are accepted only for authorities derived from this listener or the
	// configured application origin.
	ListenAddress string
	// LocalHostnames are startup-verified names for the listener, never request-supplied names.
	LocalHostnames []string
	// ApplicationOrigin resolves the configured public URL for unsafe-request
	// and authority checks. An empty result uses the effective request origin.
	ApplicationOrigin func(context.Context) (string, error)
	// ConnectOrigin is an optional second HTTPS authority for remote access.
	ConnectOrigin        string
	ResolveConnectOrigin func() string
}

// ParseTrustedProxies accepts individual IP addresses or CIDR prefixes. An IP
// address is converted to a one-address prefix so configuration stays exact.
func ParseTrustedProxies(values []string) ([]netip.Prefix, error) {
	trusted := make([]netip.Prefix, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			return nil, errors.New("trusted proxy cannot be empty")
		}

		if prefix, err := netip.ParsePrefix(value); err == nil {
			trusted = append(trusted, prefix.Masked())
			continue
		}

		address, err := netip.ParseAddr(value)
		if err != nil {
			return nil, fmt.Errorf("invalid trusted proxy %q: use an IP address or CIDR prefix", value)
		}

		trusted = append(trusted, netip.PrefixFrom(address, address.BitLen()))
	}

	return trusted, nil
}

// NewServer applies the connection-level limits that cannot be enforced by a
// handler. Handler-level limits and headers are installed by NewHandler.
func NewServer(address string, handler http.Handler) *http.Server {
	return &http.Server{
		Addr:              address,
		Handler:           handler,
		ReadHeaderTimeout: ReadHeaderTimeout,
		ReadTimeout:       ReadTimeout,
		WriteTimeout:      WriteTimeout,
		IdleTimeout:       IdleTimeout,
		MaxHeaderBytes:    MaximumHeaderBytes,
	}
}

// NewHandler applies the response policy, request-body limits, and proxy trust
// rules to all UI, API, OpenAPI, and error responses.
func NewHandler(next http.Handler, options Options) http.Handler {
	localOrigins := listenerHostnameOrigins(options.ListenAddress, options.LocalHostnames)

	return http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		setSecurityHeaders(response.Header())

		options := options
		if options.ResolveConnectOrigin != nil {
			options.ConnectOrigin = options.ResolveConnectOrigin()
		}

		allowed, configuredOrigin, err := policyOrigins(request.Context(), options, localOrigins)
		if err != nil {
			WriteProblem(response, http.StatusInternalServerError, "origin-policy-unavailable", "The request authority policy could not be evaluated")
			return
		}

		if options.ListenAddress != "" && !authorityAllowed(request.Host, allowed) {
			WriteProblem(response, http.StatusMisdirectedRequest, "request-authority-rejected", "The request authority is not configured for this Savorum instance")
			return
		}

		request = request.WithContext(context.WithValue(request.Context(), immediatePeerKey, request.RemoteAddr))
		if err := applyForwarded(request, options.TrustedProxies); err != nil {
			WriteProblem(response, http.StatusBadRequest, "invalid-forwarded-request", err.Error())
			return
		}

		if options.ListenAddress != "" {
			effective, err := effectiveRequestOrigin(request)
			if connectRequest(request, options.ConnectOrigin) {
				effective, err = parseOrigin(options.ConnectOrigin)
			}

			if err != nil || !originAllowed(effective, allowed) {
				WriteProblem(response, http.StatusMisdirectedRequest, "request-authority-rejected", "The request authority is not configured for this Savorum instance")
				return
			}
		}

		if unsafeMethod(request.Method) {
			if (request.Method != http.MethodPost || !slices.Contains(options.SAMLACSPaths, request.URL.Path)) && !allowUnsafeRequest(request, configuredOrigin, options.ConnectOrigin, connectRequest(request, options.ConnectOrigin)) {
				WriteProblem(response, http.StatusForbidden, "cross-site-request-rejected", "Cross-site requests cannot change this Savorum library")
				return
			}
		}

		if request.ContentLength > requestBodyLimit(request) {
			WriteProblem(response, http.StatusRequestEntityTooLarge, "request-too-large", "The request body exceeds the configured size limit")
			return
		}

		if request.Body != nil {
			request.Body = http.MaxBytesReader(response, request.Body, requestBodyLimit(request))
		}

		request = withConnectOrigin(request, options.ConnectOrigin)

		next.ServeHTTP(response, request)
	})
}

// policyOrigins returns every origin the instance accepts and the configured
// application origin.
func policyOrigins(ctx context.Context, options Options, localOrigins []allowedOrigin) ([]allowedOrigin, string, error) {
	configuredOrigin, err := resolveApplicationOrigin(ctx, options.ApplicationOrigin)
	if err != nil {
		return nil, "", err
	}

	allowed, err := allowedOrigins(options.ListenAddress, configuredOrigin)
	if err != nil {
		return nil, "", err
	}

	allowed = append(allowed, localOrigins...)

	if options.ConnectOrigin != "" {
		remote, err := allowedOrigins("", options.ConnectOrigin)
		if err != nil {
			return nil, "", err
		}

		allowed = append(allowed, remote...)
	}

	return allowed, configuredOrigin, nil
}

func resolveApplicationOrigin(ctx context.Context, configured func(context.Context) (string, error)) (string, error) {
	if configured == nil {
		return "", nil
	}

	return configured(ctx)
}

type allowedOrigin struct {
	origin      *url.URL
	authority   authority
	allowNoPort bool
	allowLAN    bool
}

type authority struct {
	host string
	port string
}

func allowedOrigins(listenAddress, configuredOrigin string) ([]allowedOrigin, error) {
	result := make([]allowedOrigin, 0, 4)

	if strings.TrimSpace(listenAddress) != "" {
		host, port, err := net.SplitHostPort(strings.TrimSpace(listenAddress))
		if err != nil {
			return nil, fmt.Errorf("parse listen address: %w", err)
		}

		isUnspecified := false

		hosts := []string{host}
		if host == "" {
			hosts = []string{"localhost", "127.0.0.1", "::1"}
			isUnspecified = true
		} else if address, err := netip.ParseAddr(host); err == nil {
			if address.IsLoopback() || address.IsUnspecified() {
				hosts = []string{"localhost", "127.0.0.1", "::1"}
				isUnspecified = address.IsUnspecified()
			}
		} else if strings.EqualFold(strings.TrimSuffix(host, "."), "localhost") {
			hosts = []string{"localhost", "127.0.0.1", "::1"}
		}

		for _, candidate := range hosts {
			value := "http://" + net.JoinHostPort(candidate, port)

			origin, err := parseOrigin(value)
			if err != nil {
				return nil, err
			}

			normalized, err := parseAuthority(origin.Host)
			if err != nil {
				return nil, err
			}

			result = append(result, allowedOrigin{
				origin:      origin,
				authority:   normalized,
				allowNoPort: port == "80",
				allowLAN:    isUnspecified,
			})
		}
	}

	if strings.TrimSpace(configuredOrigin) != "" {
		origin, err := parseOrigin(configuredOrigin)
		if err != nil {
			return nil, err
		}

		normalized, err := parseAuthority(origin.Host)
		if err != nil {
			return nil, err
		}

		if normalized.port == "" {
			normalized.port = originPort(origin)
		}

		result = append(result, allowedOrigin{origin: origin, authority: normalized, allowNoPort: normalized.port == defaultPort(origin.Scheme)})
	}

	return result, nil
}

func authorityAllowed(value string, allowed []allowedOrigin) bool {
	supplied, err := parseAuthority(value)
	if err != nil {
		return false
	}

	for _, candidate := range allowed {
		portMatches := supplied.port == candidate.authority.port || (supplied.port == "" && candidate.allowNoPort)
		if !portMatches {
			continue
		}

		if supplied.host == candidate.authority.host {
			return true
		}

		if candidate.allowLAN && isLANHost(supplied.host) {
			return true
		}
	}

	return false
}

func isLANHost(host string) bool {
	if strings.HasSuffix(host, ".local") {
		return true
	}

	if addr, err := netip.ParseAddr(host); err == nil {
		return addr.IsPrivate() || addr.IsLoopback() || addr.IsLinkLocalUnicast()
	}

	return false
}

func isLANOrigin(value *url.URL, candidate allowedOrigin) bool {
	host, err := normalizeHost(value.Hostname())
	if err != nil {
		return false
	}

	port := originPort(value)
	if port != candidate.authority.port && (port != "80" || !candidate.allowNoPort) {
		return false
	}

	return isLANHost(host)
}

func originAllowed(value *url.URL, allowed []allowedOrigin) bool {
	for _, candidate := range allowed {
		if sameOrigin(value, candidate.origin) {
			return true
		}

		if candidate.allowLAN && isLANOrigin(value, candidate) {
			return true
		}
	}

	return false
}

func parseAuthority(value string) (authority, error) {
	if err := validateForwardedHost(value); err != nil {
		return authority{}, err
	}

	parsed, err := url.Parse("http://" + value)
	if err != nil {
		return authority{}, fmt.Errorf("parse request authority: %w", err)
	}

	host, err := normalizeHost(parsed.Hostname())
	if err != nil {
		return authority{}, err
	}

	return authority{host: host, port: parsed.Port()}, nil
}

func normalizeHost(value string) (string, error) {
	value = strings.TrimSuffix(strings.ToLower(strings.TrimSpace(value)), ".")
	if value == "" || strings.Contains(value, "%") {
		return "", errors.New("host is not valid")
	}

	if address, err := netip.ParseAddr(value); err == nil {
		return address.Unmap().String(), nil
	}

	value, err := idna.Lookup.ToASCII(value)
	if err != nil || value == "" {
		return "", errors.New("host is not valid")
	}

	return strings.ToLower(value), nil
}

func unsafeMethod(method string) bool {
	switch method {
	case http.MethodGet, http.MethodHead, http.MethodOptions:
		return false
	default:
		return true
	}
}

func allowUnsafeRequest(request *http.Request, configuredOrigin string, connectOrigin string, remote bool) bool {
	switch fetchSite := strings.ToLower(strings.TrimSpace(request.Header.Get("Sec-Fetch-Site"))); fetchSite {
	case "cross-site":
		return false
	case "", "same-origin", "same-site", "none":
	default:
		return false
	}

	originValues := request.Header.Values("Origin")
	if len(originValues) == 0 {
		// CLI clients and server-to-server integrations do not normally send
		// browser provenance headers. They remain supported until an explicit
		// authentication mechanism can identify them more strongly.
		return true
	}

	if len(originValues) != 1 {
		return false
	}

	supplied, err := parseOrigin(originValues[0])
	if err != nil {
		return false
	}

	if remote && connectOrigin != "" {
		expected, err := parseOrigin(connectOrigin)
		return err == nil && sameOrigin(supplied, expected)
	}

	var expected *url.URL
	if strings.TrimSpace(configuredOrigin) != "" {
		expected, err = parseOrigin(configuredOrigin)
	} else {
		expected, err = effectiveRequestOrigin(request)
	}

	if err != nil {
		return false
	}

	return sameOrigin(supplied, expected)
}

func connectRequest(request *http.Request, origin string) bool {
	if origin == "" {
		return false
	}

	u, err := parseOrigin(origin)
	if err != nil || request.Host != u.Host {
		return false
	}

	if trusted, _ := request.Context().Value(connectTransportKey{}).(bool); trusted {
		return true
	}

	host, _, err := net.SplitHostPort(request.RemoteAddr)
	if err != nil {
		return false
	}

	ip := net.ParseIP(host)

	return ip != nil && ip.IsLoopback()
}

func parseOrigin(value string) (*url.URL, error) {
	value = strings.TrimSpace(value)
	if value == "" || value == "null" || strings.ContainsAny(value, " \t\r\n") {
		return nil, errors.New("origin is not an absolute HTTP(S) origin")
	}

	parsed, err := url.Parse(value)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Hostname() == "" ||
		parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" || (parsed.Path != "" && parsed.Path != "/") {
		return nil, errors.New("origin is not an absolute HTTP(S) origin")
	}

	if err := validateForwardedHost(parsed.Host); err != nil {
		return nil, err
	}

	return parsed, nil
}

func effectiveRequestOrigin(request *http.Request) (*url.URL, error) {
	scheme := strings.ToLower(request.URL.Scheme)
	if scheme == "" {
		if request.TLS != nil {
			scheme = "https"
		} else {
			scheme = "http"
		}
	}

	return parseOrigin(scheme + "://" + request.Host)
}

func sameOrigin(left, right *url.URL) bool {
	leftHost, leftErr := normalizeHost(left.Hostname())
	rightHost, rightErr := normalizeHost(right.Hostname())

	return leftErr == nil && rightErr == nil && strings.EqualFold(left.Scheme, right.Scheme) &&
		leftHost == rightHost && originPort(left) == originPort(right)
}

func originPort(value *url.URL) string {
	if port := value.Port(); port != "" {
		return port
	}

	return defaultPort(value.Scheme)
}

func defaultPort(scheme string) string {
	if scheme == "https" {
		return "443"
	}

	return "80"
}

// ClientAddress returns the validated originating client address. It is the
// immediate peer unless a trusted proxy supplied a valid forwarding chain.
func ClientAddress(request *http.Request) netip.Addr {
	if address, ok := request.Context().Value(clientAddressKey).(netip.Addr); ok {
		return address
	}

	address, _ := remoteAddress(request.RemoteAddr)

	return address
}

func setSecurityHeaders(header http.Header) {
	header.Set("Content-Security-Policy", "default-src 'self'; base-uri 'none'; object-src 'none'; frame-ancestors 'none'; form-action 'self'; img-src 'self' data: blob:; font-src 'self'; style-src 'self' 'unsafe-inline'; script-src 'self' 'unsafe-inline'; connect-src 'self'; worker-src 'self' blob:")
	header.Set("Permissions-Policy", "camera=(), geolocation=(), microphone=()")
	header.Set("Referrer-Policy", "no-referrer")
	header.Set("X-Content-Type-Options", "nosniff")
	header.Set("X-Frame-Options", "DENY")
}

func requestBodyLimit(request *http.Request) int64 {
	if request.Method != http.MethodPost && request.Method != http.MethodPut && request.Method != http.MethodPatch {
		return DefaultRequestBodyBytes
	}

	switch request.URL.Path {
	case "/api/v1/import-jobs":
		return 90 << 20 // Bounded base64 envelope for at most 64 MiB of file bytes.
	case "/api/v1/assets":
		return AssetRequestBodyBytes
	case "/api/v1/recipes/import-external", "/api/v1/recipes/import-preview":
		return ExternalImportBodyBytes
	case "/api/v1/recipes/import-package":
		return PackageImportBodyBytes
	default:
		return DefaultRequestBodyBytes
	}
}

func applyForwarded(request *http.Request, trusted []netip.Prefix) error {
	peer, peerOK := remoteAddress(request.RemoteAddr)

	isTrusted := peerOK && contains(trusted, peer)
	if !isTrusted {
		stripForwarded(request.Header)

		if peerOK {
			*request = *request.WithContext(context.WithValue(request.Context(), clientAddressKey, peer))
		}

		return nil
	}

	// RFC 7239 permits multiple proxy-added elements and quoted values. Savorum
	// does not need that metadata and removes it rather than partially parsing a
	// format whose trust semantics would be easy to get wrong. The three
	// X-Forwarded fields below form the explicitly supported proxy contract.
	request.Header.Del("Forwarded")

	proto, err := oneForwardedValue(request.Header.Values("X-Forwarded-Proto"), "X-Forwarded-Proto")
	if err != nil {
		return err
	}

	if proto != "" {
		proto = strings.ToLower(proto)
		if proto != "http" && proto != "https" {
			return errors.New("X-Forwarded-Proto must be exactly http or https")
		}

		request.URL.Scheme = proto
		request.Header.Set("X-Forwarded-Proto", proto)
	}

	host, err := oneForwardedValue(request.Header.Values("X-Forwarded-Host"), "X-Forwarded-Host")
	if err != nil {
		return err
	}

	if host != "" {
		if err := validateForwardedHost(host); err != nil {
			return err
		}

		request.Host = host
		request.Header.Set("X-Forwarded-Host", host)
	}

	client := peer

	if values := request.Header.Values("X-Forwarded-For"); len(values) > 0 {
		chain, err := parseForwardedFor(values)
		if err != nil {
			return err
		}

		client = originalClient(chain, trusted)
		request.Header.Set("X-Forwarded-For", joinAddresses(chain))
	}

	request.Header.Del("X-Forwarded-Port")
	request.Header.Del("X-Forwarded-Prefix")
	request.Header.Del("X-Real-Ip")
	request.RemoteAddr = net.JoinHostPort(client.String(), "0")
	*request = *request.WithContext(context.WithValue(request.Context(), clientAddressKey, client))

	return nil
}

func stripForwarded(header http.Header) {
	for _, name := range forwardedHeaders {
		header.Del(name)
	}
}

func remoteAddress(value string) (netip.Addr, bool) {
	if addressPort, err := netip.ParseAddrPort(value); err == nil {
		return addressPort.Addr().Unmap(), true
	}

	address, err := netip.ParseAddr(value)
	if err != nil {
		return netip.Addr{}, false
	}

	return address.Unmap(), true
}

func contains(prefixes []netip.Prefix, address netip.Addr) bool {
	address = address.Unmap()
	for _, prefix := range prefixes {
		if prefix.Contains(address) {
			return true
		}
	}

	return false
}

func oneForwardedValue(values []string, name string) (string, error) {
	if len(values) == 0 {
		return "", nil
	}

	if len(values) != 1 || strings.Contains(values[0], ",") {
		return "", fmt.Errorf("%s must contain exactly one value", name)
	}

	value := strings.TrimSpace(values[0])
	if value == "" {
		return "", fmt.Errorf("%s cannot be empty", name)
	}

	return value, nil
}

func validateForwardedHost(value string) error {
	if strings.ContainsAny(value, " /\\@\t\r\n") {
		return errors.New("X-Forwarded-Host is not a valid host")
	}

	parsed, err := url.Parse("http://" + value)
	if err != nil || parsed.Host != value || parsed.Hostname() == "" || parsed.Path != "" {
		return errors.New("X-Forwarded-Host is not a valid host")
	}

	if port := parsed.Port(); port != "" {
		number, err := strconv.Atoi(port)
		if err != nil || number < 1 || number > 65535 {
			return errors.New("X-Forwarded-Host has an invalid port")
		}
	}

	return nil
}

func parseForwardedFor(values []string) ([]netip.Addr, error) {
	var chain []netip.Addr

	for _, value := range values {
		for part := range strings.SplitSeq(value, ",") {
			address, err := netip.ParseAddr(strings.TrimSpace(part))
			if err != nil || address.Zone() != "" {
				return nil, errors.New("X-Forwarded-For must contain only IP addresses")
			}

			chain = append(chain, address.Unmap())
			if len(chain) > maximumForwardedHops {
				return nil, fmt.Errorf("X-Forwarded-For cannot contain more than %d addresses", maximumForwardedHops)
			}
		}
	}

	if len(chain) == 0 {
		return nil, errors.New("X-Forwarded-For cannot be empty")
	}

	return chain, nil
}

func originalClient(chain []netip.Addr, trusted []netip.Prefix) netip.Addr {
	for _, v := range slices.Backward(chain) {
		if !contains(trusted, v) {
			return v
		}
	}

	return chain[0]
}

func joinAddresses(addresses []netip.Addr) string {
	values := make([]string, len(addresses))
	for index, address := range addresses {
		values[index] = address.String()
	}

	return strings.Join(values, ", ")
}

// WriteProblem keeps boundary errors machine-readable across transport, account,
// and external authentication handlers without exposing internal error values.
func WriteProblem(response http.ResponseWriter, status int, kind, detail string) {
	type problem struct {
		Code   string `json:"code"`
		Type   string `json:"type"`
		Title  string `json:"title"`
		Status int    `json:"status"`
		Detail string `json:"detail"`
	}

	response.Header().Set("Content-Type", "application/problem+json")
	response.WriteHeader(status)

	body, err := json.Marshal(problem{
		Code: kind, Type: "urn:savorum:problem:" + kind, Title: http.StatusText(status),
		Status: status, Detail: detail,
	})
	if err != nil {
		return
	}

	_, _ = response.Write(append(body, '\n'))
}

// ImmediatePeer returns the original socket peer, unaffected by forwarding metadata.
func ImmediatePeer(request *http.Request) string {
	if peer, ok := request.Context().Value(immediatePeerKey).(string); ok {
		return peer
	}

	return request.RemoteAddr
}

type connectTransportKey struct{}

// ConnectTransport marks requests from the TLS tunnel. Apply only to the tunnel handler.
func ConnectTransport(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), connectTransportKey{}, true)))
	})
}

type connectOriginKey struct{}

// ConnectOrigin returns the verified tunnel origin, or an empty string for local requests.
func ConnectOrigin(ctx context.Context) string {
	origin, _ := ctx.Value(connectOriginKey{}).(string)
	return origin
}

func withConnectOrigin(request *http.Request, origin string) *http.Request {
	if !connectRequest(request, origin) {
		origin = ""
	}

	return request.WithContext(context.WithValue(request.Context(), connectOriginKey{}, origin))
}
