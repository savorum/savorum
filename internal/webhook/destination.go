package webhook

import (
	"context"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/savorum/savorum/internal/httpsecurity"
)

// DestinationPolicy has no proxy/environment escape hatch. Only deployment
// configuration may authorize explicit private IP ranges.
type DestinationPolicy struct {
	TrustedNetworks []netip.Prefix
	resolver        addressResolver
}
type addressResolver interface {
	LookupIPAddr(context.Context, string) ([]net.IPAddr, error)
}

func (p DestinationPolicy) addressAllowed(ip net.IP) bool {
	address, ok := netip.AddrFromSlice(ip)
	if !ok {
		return false
	}

	address = address.Unmap()
	if address.IsUnspecified() || address.IsMulticast() {
		return false
	}

	if !httpsecurity.NonPublicIP(ip) {
		return true
	}

	for _, prefix := range p.TrustedNetworks {
		if prefix.Contains(address) {
			return true
		}
	}

	return false
}

func destinationURL(raw string) (*url.URL, error) {
	u, err := url.Parse(raw)
	if err != nil || len(raw) > 2048 || u.Scheme != "https" || u.Hostname() == "" || u.User != nil || u.Fragment != "" || strings.Contains(u.Host, "%") {
		return nil, ErrDestination
	}

	if u.Port() != "" {
		if port, err := strconv.Atoi(u.Port()); err != nil || port < 1 || port > 65535 {
			return nil, ErrDestination
		}
	}

	return u, nil
}

func (p DestinationPolicy) resolve(ctx context.Context, host string) ([]net.IPAddr, error) {
	resolver := p.resolver
	if resolver == nil {
		resolver = net.DefaultResolver
	}

	ips, err := resolver.LookupIPAddr(ctx, host)
	if err != nil || len(ips) == 0 {
		return nil, ErrDestination
	}

	for _, ip := range ips {
		if !p.addressAllowed(ip.IP) {
			return nil, ErrDestination
		}
	}

	return ips, nil
}

// Validate rejects unsafe destinations before configuration or a redirect is accepted.
func (p DestinationPolicy) Validate(ctx context.Context, raw string) error {
	u, err := destinationURL(raw)
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	_, err = p.resolve(ctx, u.Hostname())

	return err
}

func (p DestinationPolicy) client() *http.Client {
	dialer := &net.Dialer{Timeout: 5 * time.Second}
	transport := &http.Transport{
		Proxy: nil, DisableKeepAlives: true, TLSHandshakeTimeout: 5 * time.Second, ResponseHeaderTimeout: 8 * time.Second, MaxResponseHeaderBytes: 16 << 10,
		DialContext: func(ctx context.Context, network, address string) (net.Conn, error) {
			host, port, err := net.SplitHostPort(address)
			if err != nil {
				return nil, ErrDestination
			}

			ips, err := p.resolve(ctx, host)
			if err != nil {
				return nil, err
			}

			return dialer.DialContext(ctx, network, net.JoinHostPort(ips[0].IP.String(), port))
		},
	}

	return &http.Client{Transport: transport, Timeout: 10 * time.Second, CheckRedirect: func(request *http.Request, via []*http.Request) error {
		if len(via) > 3 || request.Method != http.MethodPost {
			return http.ErrUseLastResponse
		}

		return p.Validate(request.Context(), request.URL.String())
	}}
}
