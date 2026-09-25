package config

import (
	"net/netip"
	"testing"
)

func TestAdminListen(t *testing.T) {
	if ResolveAdminListen("") != DefaultAdminListen || ResolveAdminListen("OFF") != "" || ResolveAdminListen(" 127.0.0.1:9 ") != "127.0.0.1:9" {
		t.Fatal("resolve")
	}
	closed := AdminAccess{}
	for _, ok := range []string{"127.0.0.1:8081", "localhost:8081", "[::1]:8081", "127.0.0.2:1"} {
		if err := ValidateAdminListen(ok, closed); err != nil {
			t.Fatalf("%s: %v", ok, err)
		}
	}
	for _, bad := range []string{"0.0.0.0:8081", "192.168.0.10:8081", "example.com:8081", "[::]:8081", "127.0.0.1"} {
		if ValidateAdminListen(bad, closed) == nil {
			t.Fatalf("%s accepted without an allow list", bad)
		}
	}

	lan, err := ParseAdminAllow("192.168.0.0/24")
	if err != nil {
		t.Fatal(err)
	}
	for _, ok := range []string{"0.0.0.0:8081", "[::]:8081", "192.168.0.9:8081", "127.0.0.1:8081"} {
		if err := ValidateAdminListen(ok, lan); err != nil {
			t.Fatalf("%s: %v", ok, err)
		}
	}
	for _, bad := range []string{"10.0.0.5:8081", "gateway.lan:8081"} {
		if ValidateAdminListen(bad, lan) == nil {
			t.Fatalf("%s accepted", bad)
		}
	}
}

func TestParseAdminAllow(t *testing.T) {
	access, err := ParseAdminAllow(" 192.168.0.0/24, 10.1.2.3 ,100.64.0.0/10, fd00::/8,")
	if err != nil {
		t.Fatal(err)
	}
	if access.String() != "192.168.0.0/24,10.1.2.3/32,100.64.0.0/10,fd00::/8" {
		t.Fatalf("parsed = %s", access)
	}
	for _, bad := range []string{
		"0.0.0.0/0", "::/0", "8.8.8.8", "192.0.0.0/8", "10.0.0.0/7", "172.0.0.0/8",
		"100.0.0.0/8", "2001:db8::/32", "not-an-ip", "192.168.0.0/33",
	} {
		if _, err := ParseAdminAllow(bad); err == nil {
			t.Fatalf("%q accepted", bad)
		}
	}
	if a, err := ParseAdminAllow(""); err != nil || a.Open() {
		t.Fatalf("empty = %v %v", a, err)
	}
}

func TestAdminAccessAllows(t *testing.T) {
	access, _ := ParseAdminAllow("192.168.0.0/24")
	cases := map[string]bool{
		"127.0.0.1":          true,
		"::1":                true,
		"192.168.0.23":       true,
		"::ffff:192.168.0.5": true,
		"192.168.1.23":       false,
		"10.0.0.1":           false,
		"8.8.8.8":            false,
	}
	for ip, want := range cases {
		if got := access.Allows(netip.MustParseAddr(ip)); got != want {
			t.Fatalf("Allows(%s) = %v", ip, got)
		}
	}
	hosts := map[string]bool{
		"localhost":   true,
		"127.0.0.1":   true,
		"[::1]":       true,
		"192.168.0.9": true,
		"192.168.1.9": false,
		"gateway.lan": false,
		"evil.test":   false,
	}
	for host, want := range hosts {
		if got := access.AllowsHost(host); got != want {
			t.Fatalf("AllowsHost(%s) = %v", host, got)
		}
	}
	if (AdminAccess{}).AllowsHost("192.168.0.9") {
		t.Fatal("closed access accepted a LAN host")
	}
}
