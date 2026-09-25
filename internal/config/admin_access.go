package config

import (
	"fmt"
	"net"
	"net/netip"
	"strings"
)

var cgnat = netip.MustParsePrefix("100.64.0.0/10")

type AdminAccess struct {
	Allow []netip.Prefix
}

func ParseAdminAllow(raw string) (AdminAccess, error) {
	var access AdminAccess
	for _, item := range strings.Split(raw, ",") {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		prefix, err := parsePrefix(item)
		if err != nil {
			return AdminAccess{}, fmt.Errorf("%s: %q is not an IP address or CIDR range", EnvAdminAllow, item)
		}
		if !privateRange(prefix) {
			return AdminAccess{}, fmt.Errorf("%s: %s is not a private network range; only 10.0.0.0/8, 172.16.0.0/12, 192.168.0.0/16, 100.64.0.0/10 and fc00::/7 are allowed", EnvAdminAllow, prefix)
		}
		access.Allow = append(access.Allow, prefix)
	}
	return access, nil
}

func parsePrefix(item string) (netip.Prefix, error) {
	if strings.Contains(item, "/") {
		p, err := netip.ParsePrefix(item)
		if err != nil {
			return netip.Prefix{}, err
		}
		return netip.PrefixFrom(p.Addr().Unmap(), p.Bits()-unmapShift(p.Addr())).Masked(), nil
	}
	addr, err := netip.ParseAddr(item)
	if err != nil {
		return netip.Prefix{}, err
	}
	addr = addr.Unmap()
	return netip.PrefixFrom(addr, addr.BitLen()), nil
}

func unmapShift(addr netip.Addr) int {
	if addr.Is4In6() {
		return 96
	}
	return 0
}

func privateRange(p netip.Prefix) bool {
	first, last := p.Masked().Addr(), lastAddr(p)
	ok := func(a netip.Addr) bool { return a.IsPrivate() || cgnat.Contains(a) }
	return ok(first) && ok(last) && sameClass(first, last)
}

func sameClass(a, b netip.Addr) bool {
	for _, p := range []netip.Prefix{
		netip.MustParsePrefix("10.0.0.0/8"),
		netip.MustParsePrefix("172.16.0.0/12"),
		netip.MustParsePrefix("192.168.0.0/16"),
		cgnat,
		netip.MustParsePrefix("fc00::/7"),
	} {
		if p.Contains(a) {
			return p.Contains(b)
		}
	}
	return false
}

func lastAddr(p netip.Prefix) netip.Addr {
	b := p.Masked().Addr().AsSlice()
	for i := p.Bits(); i < len(b)*8; i++ {
		b[i/8] |= 1 << (7 - uint(i%8))
	}
	addr, _ := netip.AddrFromSlice(b)
	return addr
}

func (a AdminAccess) Open() bool {
	return len(a.Allow) > 0
}

func (a AdminAccess) Allows(addr netip.Addr) bool {
	addr = addr.Unmap()
	if addr.IsLoopback() {
		return true
	}
	for _, p := range a.Allow {
		if p.Contains(addr) {
			return true
		}
	}
	return false
}

func (a AdminAccess) AllowsHost(host string) bool {
	if strings.EqualFold(host, "localhost") {
		return true
	}
	addr, err := netip.ParseAddr(strings.Trim(host, "[]"))
	if err != nil {
		return false
	}
	return a.Allows(addr)
}

func (a AdminAccess) String() string {
	parts := make([]string, 0, len(a.Allow))
	for _, p := range a.Allow {
		parts = append(parts, p.String())
	}
	return strings.Join(parts, ",")
}

func ValidateAdminListen(addr string, access AdminAccess) error {
	host, port, err := net.SplitHostPort(strings.TrimSpace(addr))
	if err != nil || port == "" {
		return fmt.Errorf("%s must be host:port", EnvAdminListen)
	}
	if IsLoopbackHost(host) {
		return nil
	}
	if !access.Open() {
		return fmt.Errorf("%s must use a loopback address such as 127.0.0.1; set %s to a private range to open it on the LAN", EnvAdminListen, EnvAdminAllow)
	}
	switch host {
	case "", "0.0.0.0", "::":
		return nil
	}
	ip, err := netip.ParseAddr(host)
	if err != nil {
		return fmt.Errorf("%s must use an IP address, not %q", EnvAdminListen, host)
	}
	if !access.Allows(ip) {
		return fmt.Errorf("%s address %s is outside %s", EnvAdminListen, host, EnvAdminAllow)
	}
	return nil
}
