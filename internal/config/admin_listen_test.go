package config

import "testing"

func TestAdminListen(t *testing.T) {
	if ResolveAdminListen("") != DefaultAdminListen || ResolveAdminListen("OFF") != "" || ResolveAdminListen(" 127.0.0.1:9 ") != "127.0.0.1:9" {
		t.Fatal("resolve")
	}
	for _, ok := range []string{"127.0.0.1:8081", "localhost:8081", "[::1]:8081", "127.0.0.2:1"} {
		if err := ValidateAdminListen(ok); err != nil {
			t.Fatalf("%s: %v", ok, err)
		}
	}
	for _, bad := range []string{"0.0.0.0:8081", "192.168.0.10:8081", "example.com:8081", "[::]:8081", "127.0.0.1"} {
		if ValidateAdminListen(bad) == nil {
			t.Fatalf("%s accepted", bad)
		}
	}
}
