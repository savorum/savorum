// Package dnssd provides DNS-SD (Bonjour / mDNS) service advertisement for Savorum.
package dnssd

import (
	"errors"
	"fmt"
	"net"
	"strings"
	"sync"

	"github.com/grandcat/zeroconf"
)

const (
	// ServiceType is the DNS-SD service type for Savorum servers.
	ServiceType = "_savorum._tcp"
	// Domain is the link-local domain for mDNS.
	Domain = "local."
	// ProtocolVersion is the published discovery contract version.
	ProtocolVersion = "1"
)

// Advertiser manages the lifecycle of a DNS-SD service registration.
type Advertiser struct {
	server *zeroconf.Server
	mu     sync.Mutex
	closed bool
}

// Options configures a DNS-SD advertisement.
type Options struct {
	Name       string
	Address    string
	Port       int
	InstanceID string
}

// Start registers and begins advertising the Savorum DNS-SD service.
// The TXT record is intentionally kept minimal (version and instance ID only)
// to prevent broadcasting private information across the local network.
func Start(opts Options) (*Advertiser, error) {
	if opts.Port <= 0 || opts.Port > 65535 {
		return nil, fmt.Errorf("invalid port %d: must be between 1 and 65535", opts.Port)
	}

	name := strings.TrimSpace(opts.Name)
	if name == "" {
		name = "Savorum"
	}

	instanceID := strings.TrimSpace(opts.InstanceID)
	if instanceID == "" {
		return nil, errors.New("instance ID is required for DNS-SD advertisement")
	}

	txt := []string{
		"version=" + ProtocolVersion,
		"instance=" + instanceID,
	}

	var (
		server *zeroconf.Server
		err    error
	)

	if ip := net.ParseIP(opts.Address); ip != nil && !ip.IsUnspecified() {
		suffix := strings.ToLower(instanceID)
		name += " (" + suffix[len(suffix)-min(6, len(suffix)):] + ")"
		server, err = zeroconf.RegisterProxy(name, ServiceType, Domain, opts.Port,
			"savorum-"+suffix, []string{ip.String()}, txt, nil)
	} else {
		server, err = zeroconf.Register(name, ServiceType, Domain, opts.Port, txt, nil)
	}

	if err != nil {
		return nil, fmt.Errorf("register DNS-SD service: %w", err)
	}

	return &Advertiser{server: server}, nil
}

// Shutdown stops the DNS-SD advertisement and removes the service from the network.
func (a *Advertiser) Shutdown() {
	if a == nil {
		return
	}

	a.mu.Lock()
	defer a.mu.Unlock()

	if a.closed {
		return
	}

	a.closed = true
	if a.server != nil {
		a.server.Shutdown()
	}
}
