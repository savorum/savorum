package httpsecurity

import (
	"context"
	"net"
	"net/netip"
	"os"
	"time"
)

// LocalListenerHostnames resolves the machine's own hostname once at startup.
// Only names resolving to the bound address are admitted; incoming Host headers
// never trigger DNS lookups or expand the authority policy.
func LocalListenerHostnames(ctx context.Context, listenAddress string) []string {
	host, _, err := net.SplitHostPort(listenAddress)
	if err != nil {
		return nil
	}

	bound, err := netip.ParseAddr(host)
	if err != nil || bound.IsLoopback() {
		return nil
	}

	name, err := os.Hostname()
	if err != nil || name == "" {
		return nil
	}

	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()

	addresses, err := net.DefaultResolver.LookupNetIP(ctx, "ip", name)
	if err != nil {
		return nil
	}

	if !hostnameMatchesListener(bound, addresses) {
		return nil
	}

	return []string{name}
}

func hostnameMatchesListener(bound netip.Addr, addresses []netip.Addr) bool {
	for _, address := range addresses {
		if address.Unmap() == bound.Unmap() {
			return true
		}
	}

	return false
}

func listenerHostnameOrigins(listenAddress string, names []string) []allowedOrigin {
	_, port, err := net.SplitHostPort(listenAddress)
	if err != nil {
		return nil
	}

	var result []allowedOrigin

	for _, name := range names {
		origins, err := allowedOrigins(net.JoinHostPort(name, port), "")
		if err == nil {
			result = append(result, origins...)
		}
	}

	return result
}
