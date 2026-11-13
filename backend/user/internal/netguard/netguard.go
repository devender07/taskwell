// Package netguard keeps server-side requests to user-given URLs off private networks (SSRF): it tells public
// addresses from private, loopback and link-local ones, and dials only checked addresses so DNS rebinding can't swap
// one in.
package netguard

import (
	"context"
	"fmt"
	"net"
	"net/netip"
	"strings"
	"time"
)

var blockedPrefixes = []netip.Prefix{
	netip.MustParsePrefix("0.0.0.0/8"),
	netip.MustParsePrefix("100.64.0.0/10"), // CGNAT
	netip.MustParsePrefix("192.0.0.0/24"),
	netip.MustParsePrefix("198.18.0.0/15"),
	netip.MustParsePrefix("240.0.0.0/4"),
	netip.MustParsePrefix("64:ff9b::/96"), // NAT64 can reach IPv4 private ranges
	netip.MustParsePrefix("2002::/16"),    // 6to4 likewise
}

// Blocked reports whether a is loopback, private, link-local, CGNAT, multicast, unspecified, IPv6 ULA or otherwise
// not a public unicast address.
func Blocked(a netip.Addr) bool {
	a = a.Unmap()
	if !a.IsValid() || a.IsLoopback() || a.IsPrivate() || a.IsLinkLocalUnicast() || a.IsLinkLocalMulticast() ||
		a.IsInterfaceLocalMulticast() || a.IsMulticast() || a.IsUnspecified() {
		return true
	}
	for _, p := range blockedPrefixes {
		if p.Contains(a) {
			return true
		}
	}
	return false
}

// PrivateHost reports whether host is localhost or a blocked IP literal.
func PrivateHost(host string) bool {
	if host == "localhost" || strings.HasSuffix(host, ".localhost") {
		return true
	}
	a, err := netip.ParseAddr(strings.Trim(host, "[]"))
	return err == nil && Blocked(a)
}

// Resolver looks up a host name's addresses.
type Resolver func(ctx context.Context, host string) ([]netip.Addr, error)

// Lookup is the system resolver.
func Lookup(ctx context.Context, host string) ([]netip.Addr, error) {
	return net.DefaultResolver.LookupNetIP(ctx, "ip", host)
}

// PrivateError is a host that is, or resolves to, a blocked address.
type PrivateError struct{ Host string }

func (e PrivateError) Error() string { return e.Host + " resolves to a private address" }

// Addrs returns host's addresses (an IP literal is its own), or a PrivateError when any is blocked and not
// allowPrivate.
func Addrs(ctx context.Context, resolve Resolver, host string, allowPrivate bool) ([]netip.Addr, error) {
	host = strings.Trim(host, "[]")
	var ips []netip.Addr
	if ip, err := netip.ParseAddr(host); err == nil {
		ips = []netip.Addr{ip}
	} else if ips, err = resolve(ctx, host); err != nil {
		return nil, fmt.Errorf("resolve %s: %w", host, err)
	}
	if len(ips) == 0 {
		return nil, fmt.Errorf("resolve %s: no addresses", host)
	}
	for _, ip := range ips {
		if !allowPrivate && Blocked(ip) {
			return nil, PrivateError{host}
		}
	}
	return ips, nil
}

// DialContext is an http.Transport DialContext that resolves the host itself and dials the first checked address.
func DialContext(resolve Resolver, allowPrivate bool) func(ctx context.Context, network, addr string) (net.Conn, error) {
	return func(ctx context.Context, _, addr string) (net.Conn, error) {
		host, port, err := net.SplitHostPort(addr)
		if err != nil {
			return nil, err
		}
		ips, err := Addrs(ctx, resolve, host, allowPrivate)
		if err != nil {
			return nil, err
		}
		d := net.Dialer{Timeout: 10 * time.Second}
		return d.DialContext(ctx, "tcp", net.JoinHostPort(ips[0].Unmap().String(), port))
	}
}
