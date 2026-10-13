package httpsecurity

import (
	"net"
	"net/netip"
)

var nonPublicPrefixes = []netip.Prefix{
	// IANA IPv4 special-purpose ranges that are not ordinary public hosts.
	netip.MustParsePrefix("0.0.0.0/8"),
	netip.MustParsePrefix("10.0.0.0/8"),
	netip.MustParsePrefix("100.64.0.0/10"),
	netip.MustParsePrefix("127.0.0.0/8"),
	netip.MustParsePrefix("169.254.0.0/16"),
	netip.MustParsePrefix("172.16.0.0/12"),
	netip.MustParsePrefix("192.0.0.0/24"),
	netip.MustParsePrefix("192.0.2.0/24"),
	netip.MustParsePrefix("192.88.99.0/24"),
	netip.MustParsePrefix("192.168.0.0/16"),
	netip.MustParsePrefix("198.18.0.0/15"),
	netip.MustParsePrefix("198.51.100.0/24"),
	netip.MustParsePrefix("203.0.113.0/24"),
	netip.MustParsePrefix("240.0.0.0/4"),

	// Only assigned global-unicast IPv6 space is eligible below. These are the
	// special ranges inside that space which are still not globally reachable.
	netip.MustParsePrefix("2001::/23"),
	netip.MustParsePrefix("2001:db8::/32"),
	netip.MustParsePrefix("2002::/16"),
	netip.MustParsePrefix("3fff::/20"),
}

var assignedIPv6GlobalUnicast = netip.MustParsePrefix("2000::/3")

// NonPublicIP rejects every destination that is not an ordinary public internet
// host. The explicit prefixes follow IANA's special-purpose registries; the
// generic checks also cover multicast, private, loopback, and link-local space.
func NonPublicIP(ip net.IP) bool {
	address, ok := netip.AddrFromSlice(ip)
	if !ok {
		return true
	}

	address = address.Unmap()
	if !address.IsGlobalUnicast() || address.IsPrivate() || address.IsLoopback() || address.IsLinkLocalUnicast() || address.IsMulticast() || address.IsUnspecified() {
		return true
	}

	if address.Is6() && !assignedIPv6GlobalUnicast.Contains(address) {
		return true
	}

	for _, prefix := range nonPublicPrefixes {
		if prefix.Contains(address) {
			return true
		}
	}

	return false
}
