package oauth

import (
	"regexp"
	"strings"
	"sync/atomic"
)

const userAgentPlatform = " (Ubuntu 22.4.0; x86_64) xterm-256color"

var stableVersion = regexp.MustCompile(`^[0-9]+\.[0-9]+\.[0-9]+$`)

var clientVersion atomic.Pointer[string]

func ClientVersion() string {
	if v := clientVersion.Load(); v != nil {
		return *v
	}
	return Version
}

func ClientUserAgent() string {
	return Originator + "/" + ClientVersion() + userAgentPlatform
}

func StableVersion(v string) bool {
	return stableVersion.MatchString(v)
}

func SetClientVersion(v string) bool {
	v = strings.TrimSpace(v)
	if !StableVersion(v) || CompareVersions(v, ClientVersion()) <= 0 {
		return false
	}
	clientVersion.Store(&v)
	return true
}

func ResetClientVersion() {
	clientVersion.Store(nil)
}
