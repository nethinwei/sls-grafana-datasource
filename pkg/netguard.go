package main

import (
	"fmt"
	"net"
	"net/http"
	"syscall"
	"time"
)

// newPublicOnlyHTTPClient returns an HTTP client that refuses to connect to non-public addresses
// (loopback, private, link-local, unspecified, multicast, CGNAT, etc.).
//
// It guards outbound calls whose host is built from data-source settings (e.g. the STS sign-in
// request) against SSRF. The check runs in the dialer's Control hook, i.e. on the IP actually being
// connected to after DNS resolution, so DNS rebinding cannot bypass it. Proxies from the environment
// are disabled for the same reason: the dialed address would be the proxy, not the target.
func newPublicOnlyHTTPClient(timeout time.Duration) *http.Client {
	dialer := &net.Dialer{
		Timeout:   10 * time.Second,
		KeepAlive: 30 * time.Second,
		Control:   denyNonPublicAddress,
	}
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.Proxy = nil
	transport.DialContext = dialer.DialContext
	return &http.Client{Transport: transport, Timeout: timeout}
}

func denyNonPublicAddress(network, address string, _ syscall.RawConn) error {
	host, _, err := net.SplitHostPort(address)
	if err != nil {
		return err
	}
	ip := net.ParseIP(host)
	if ip == nil {
		return fmt.Errorf("refusing to connect to %q: not an IP address", address)
	}
	if !isPublicIP(ip) {
		return fmt.Errorf("refusing to connect to non-public address %s", ip)
	}
	return nil
}

// sharedAddressSpace is 100.64.0.0/10 (RFC 6598, carrier-grade NAT), which net.IP.IsPrivate does not cover.
var sharedAddressSpace = &net.IPNet{IP: net.IPv4(100, 64, 0, 0), Mask: net.CIDRMask(10, 32)}

func isPublicIP(ip net.IP) bool {
	if ip.IsLoopback() || ip.IsPrivate() || ip.IsUnspecified() || ip.IsMulticast() ||
		ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() || ip.IsInterfaceLocalMulticast() {
		return false
	}
	if ip4 := ip.To4(); ip4 != nil && sharedAddressSpace.Contains(ip4) {
		return false
	}
	return true
}
