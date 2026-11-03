//go:build darwin

package hosttun

import (
	"net/netip"
	"os"
	"testing"
)

// This reads an existing, empty Tailmulti interface without running host commands.
func TestLiveEmptyHostConfiguration(t *testing.T) {
	name := os.Getenv("TAILMULTI_TEST_HOST_INTERFACE")
	if name == "" {
		t.Skip("requires an active Tailmulti interface with no accounts")
	}
	h := &darwinHost{name: name}
	actual, err := h.readInstalledState(nil)
	if err != nil {
		t.Fatal("cannot inspect the active interface")
	}
	want := []netip.Prefix{netip.MustParsePrefix("198.18.0.1/32"), netip.MustParsePrefix("fd74:6169:6c6d::1/128")}
	if verifyHostConfig(want, nil, actual.localAddrs, actual.routes) != nil {
		t.Fatal("installed interface does not match the empty Tailmulti configuration")
	}
	if verifyDarwinScopedDefaults(nil, actual.scopedDefaults) != nil {
		t.Fatal("unexpected scoped default routes")
	}
}
