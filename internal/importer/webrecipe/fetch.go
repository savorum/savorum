package webrecipe

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/savorum/savorum/internal/httpsecurity"
)

// MaximumPageBytes bounds a recipe page. Real pages are large; this is not.
const MaximumPageBytes = 8 << 20

// MaximumImageBytes bounds one fetched photograph.
const MaximumImageBytes = 16 << 20

// MaximumRedirects bounds a redirect chain. Every hop is revalidated against
// SSRF.
const MaximumRedirects = 5

// MaximumResponseHeaderBytes prevents a remote server from moving an
// otherwise bounded response into an unbounded header block.
const MaximumResponseHeaderBytes = 64 << 10

const userAgent = "Savorum/1.0 (recipe import)"

// Page is a fetched or supplied document together with where it came from.
// The URL matters beyond provenance: candidate selection uses it to recognize
// which of several Recipe objects on a page is the one that was asked for.
type Page struct {
	// URL is the address after redirects, which is the page that was actually
	// read and therefore the one worth recording.
	URL         string
	ContentType string
	Body        []byte
}

// Fetch retrieves a recipe page under Savorum's outbound policy: HTTP and
// HTTPS only, no private or loopback destination at any hop, a bounded
// redirect chain, a body ceiling enforced while streaming rather than after
// the download, and timeouts throughout.
//
// Fetch deliberately does not try to defeat bot protection; a refusing site is
// reported as FetchBlocked, and the person can import a saved page instead.
func Fetch(ctx context.Context, rawURL string) (*Page, error) {
	parsed, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil || parsed.Hostname() == "" || parsed.User != nil {
		return nil, failuref(FetchRejected, nil, "that does not look like a web address")
	}

	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return nil, failuref(FetchRejected, nil, "only http and https addresses can be imported")
	}

	client := &http.Client{
		Transport:     SafeTransport(),
		Timeout:       30 * time.Second,
		CheckRedirect: SafeRedirectPolicy,
	}

	request, err := http.NewRequestWithContext(ctx, http.MethodGet, parsed.String(), nil)
	if err != nil {
		// net/http errors may quote the complete URL. The address can contain a
		// bearer query or fragment, so it is deliberately not retained as a
		// wrapped error that a caller might log.
		return nil, failuref(FetchRejected, nil, "that address could not be requested")
	}

	request.Header.Set("Accept", "text/html,application/xhtml+xml,application/ld+json;q=0.9,application/json;q=0.8")
	request.Header.Set("Accept-Language", "en;q=0.9,*;q=0.5")
	request.Header.Set("User-Agent", userAgent)

	response, err := client.Do(request)
	if err != nil {
		return nil, classifyTransportError(err)
	}
	defer response.Body.Close()

	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return nil, failuref(FetchBlocked, nil, "the site answered %s", strings.TrimSpace(response.Status))
	}

	mediaType := strings.ToLower(strings.TrimSpace(strings.Split(response.Header.Get("Content-Type"), ";")[0]))
	if !supportedPageType(mediaType) {
		return nil, failuref(UnsupportedContentType, nil, "that address is %s rather than a web page", displayType(mediaType))
	}

	if response.ContentLength > MaximumPageBytes {
		return nil, failuref(FetchTooLarge, nil, "that page is larger than Savorum will download")
	}
	// One byte past the ceiling distinguishes a page that exactly fills the
	// budget from a truncated one.
	body, err := io.ReadAll(io.LimitReader(response.Body, MaximumPageBytes+1))
	if err != nil {
		return nil, classifyTransportError(err)
	}

	if len(body) > MaximumPageBytes {
		return nil, failuref(FetchTooLarge, nil, "that page is larger than Savorum will download")
	}

	if len(body) == 0 {
		return nil, failuref(NoRecipeStructuredData, nil, "that page is empty")
	}

	resolved := parsed.String()
	if response.Request != nil && response.Request.URL != nil {
		resolved = response.Request.URL.String()
	}

	return &Page{URL: resolved, ContentType: mediaType, Body: body}, nil
}

// SafeTransport is the outbound transport for every fetch of remote content
// Savorum makes on a user's behalf. Photographs must use it too: an image
// address inside JSON-LD is as untrusted as the page that carried it.
func SafeTransport() *http.Transport { return guardedTransport() }

// SafeRedirectPolicy bounds a redirect chain and refuses a hop that leaves
// HTTP. Each hop's destination is revalidated by the transport's own dialer,
// so a public hostname redirecting to a private address is caught at the point
// of connection rather than only where the chain began.
func SafeRedirectPolicy(request *http.Request, via []*http.Request) error {
	if len(via) > MaximumRedirects {
		return failuref(FetchRejected, nil, "that address redirects too many times")
	}

	if request.URL.Scheme != "http" && request.URL.Scheme != "https" {
		return failuref(FetchRejected, nil, "that address redirects somewhere Savorum will not follow")
	}

	if request.URL.User != nil {
		return failuref(FetchRejected, nil, "that address redirects to a URL containing credentials")
	}
	// Fetch requests never need application, proxy, or site credentials. Strip
	// them explicitly on every hop so a future caller cannot accidentally turn
	// a safe redirect policy into a credential-forwarding client.
	request.Header.Del("Authorization")
	request.Header.Del("Proxy-Authorization")
	request.Header.Del("Cookie")
	// net/http derives Referer from the complete preceding URL before invoking
	// CheckRedirect. Delete it here on every hop so query credentials never
	// reach either a same-origin or cross-origin redirect target.
	request.Header.Del("Referer")

	return nil
}

// guardedTransport resolves every connection itself and dials only an address
// it has just checked. Checking in the dialer closes the DNS-rebinding window
// and covers every redirect hop.
func guardedTransport() *http.Transport {
	dialer := &net.Dialer{Timeout: 10 * time.Second, KeepAlive: 30 * time.Second}

	return &http.Transport{
		DialContext: func(ctx context.Context, network, address string) (net.Conn, error) {
			return guardedDial(ctx, network, address, net.DefaultResolver.LookupIPAddr, dialer.DialContext)
		},
		TLSHandshakeTimeout:    10 * time.Second,
		ResponseHeaderTimeout:  20 * time.Second,
		MaxResponseHeaderBytes: MaximumResponseHeaderBytes,
		MaxIdleConns:           4,
		MaxConnsPerHost:        4,
		IdleConnTimeout:        30 * time.Second,
	}
}

func guardedDial(ctx context.Context, network, address string, lookup func(context.Context, string) ([]net.IPAddr, error), dial func(context.Context, string, string) (net.Conn, error)) (net.Conn, error) {
	host, port, err := net.SplitHostPort(address)
	if err != nil {
		return nil, fmt.Errorf("parse dial address: %w", err)
	}

	resolved, err := lookup(ctx, host)
	if err != nil {
		return nil, fmt.Errorf("resolve import host: %w", err)
	}

	if len(resolved) == 0 {
		return nil, failuref(FetchRejected, nil, "that address has no public destination")
	}

	for _, candidate := range resolved {
		if blockedDestination(candidate.IP) {
			return nil, failuref(FetchRejected, nil, "that address points inside this network")
		}
	}

	return dial(ctx, network, net.JoinHostPort(resolved[0].IP.String(), port))
}

// blockedDestination checks dial targets against non-public IP ranges.
// It is overridden only in package tests to verify redirect refusal.
var blockedDestination = blockedIP

func blockedIP(ip net.IP) bool { return httpsecurity.NonPublicIP(ip) }

func supportedPageType(mediaType string) bool {
	switch mediaType {
	case "", "text/html", "application/xhtml+xml", "application/json", "application/ld+json", "text/plain":
		return true
	}

	return false
}

func displayType(mediaType string) string {
	if mediaType == "" {
		return "an unknown kind of file"
	}

	return mediaType
}

func classifyTransportError(err error) error {
	if failure, ok := errors.AsType[*Failure](err); ok {
		return failure
	}

	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, os.ErrDeadlineExceeded) {
		return failuref(FetchTimeout, err, "the site did not answer in time")
	}

	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
		return failuref(FetchTimeout, nil, "the site did not answer in time")
	}

	if errors.Is(err, context.Canceled) {
		return failuref(FetchTimeout, nil, "the import was stopped before the site answered")
	}
	// url.Error includes its URL in Error(). Keep the useful classification but
	// never make an untrusted fetch address available to ordinary error logs.
	return failuref(FetchBlocked, nil, "the site could not be reached")
}
