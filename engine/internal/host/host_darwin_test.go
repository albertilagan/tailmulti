//go:build darwin

package host

import (
	"golang.org/x/sys/unix"
	"net"
	"net/netip"
	"os"
	"path/filepath"
	"strings"
	"tailmulti.local/engine/internal/routing"
	"tailscale.com/net/routetable"
	"testing"
)

// This opt-in probe reads an already active interface. It never creates a TUN or changes routes.
func TestLiveActiveHostRoutes(t *testing.T) {
	name := os.Getenv("TAILMULTI_TEST_HOST_INTERFACE")
	if name == "" {
		t.Skip("requires an already active Tailmulti interface")
	}
	iface, err := net.InterfaceByName(name)
	if err != nil {
		t.Fatal("active interface unavailable")
	}
	addrs, err := iface.Addrs()
	if err != nil {
		t.Fatal("cannot read interface addresses")
	}
	owned := map[netip.Addr]bool{}
	for _, addr := range addrs {
		if p, err := netip.ParsePrefix(addr.String()); err == nil {
			owned[p.Addr()] = true
		}
	}
	if !owned[routing.Host4] || !owned[routing.Host6] {
		t.Fatal("interface does not own both Tailmulti local addresses")
	}
	routes, err := routetable.Get(65536)
	if err != nil {
		t.Fatal("cannot inspect live routes")
	}
	if err := CheckRoutes(routes, name); err != nil {
		t.Fatalf("active Tailmulti routes rejected: %v", err)
	}
	if CheckRoutes(routes, "") == nil {
		t.Fatal("active routes accepted without ownership")
	}
}

func TestResolverOwnership(t *testing.T) {
	d := t.TempDir()
	if err := WriteResolvers(d, "53", []string{"home.ts.net"}); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(filepath.Join(d, "home.ts.net"))
	if !strings.HasPrefix(string(b), Header) {
		t.Fatal("ownership marker missing")
	}
	os.WriteFile(filepath.Join(d, "work.ts.net"), []byte("nameserver 10.0.0.1\n"), 0644)
	if err := WriteResolvers(d, "53", []string{"work.ts.net"}); err == nil {
		t.Fatal("foreign DNS overwritten")
	}
	if err := WriteResolvers(d, "53", []string{"../../etc"}); err == nil {
		t.Fatal("unsafe path accepted")
	}
}
func TestGuardAndCollisionPolicy(t *testing.T) {
	r := routetable.RouteEntry{Dst: routetable.RouteDestination{Prefix: routing.Pool4}, Interface: "en0"}
	if CheckRoutes([]routetable.RouteEntry{r}, "") == nil {
		t.Fatal("collision accepted")
	}
	r.Interface = "lo0"
	r.Sys = routetable.RouteEntryBSD{RawFlags: unix.RTF_REJECT | unix.RTF_PROTO1}
	if err := CheckRoutes([]routetable.RouteEntry{r}, ""); err != nil {
		t.Fatal(err)
	}
	r.Dst.Prefix = netip.MustParsePrefix("0.0.0.0/1")
	r.Interface = "utun9"
	if CheckRoutes([]routetable.RouteEntry{r}, "") == nil {
		t.Fatal("competing split default accepted")
	}
	r.Dst.Prefix = netip.MustParsePrefix("0.0.0.0/0")
	if err := CheckRoutes([]routetable.RouteEntry{r}, ""); err != nil {
		t.Fatal(err)
	}
}

func TestOnlyVerifiedLocalLoopbackRoutesAreAccepted(t *testing.T) {
	for _, local := range []netip.Addr{routing.Host4, routing.Host6} {
		owned := map[netip.Addr]bool{local: true}
		base := routetable.RouteEntry{
			Dst:       routetable.RouteDestination{Prefix: netip.PrefixFrom(local, local.BitLen())},
			Interface: "lo0", Sys: routetable.RouteEntryBSD{RawFlags: unix.RTF_UP | unix.RTF_HOST | unix.RTF_LOCAL | unix.RTF_LLINFO},
		}
		if err := checkRoutes([]routetable.RouteEntry{base}, "utun9", owned); err != nil {
			t.Fatal("own local route rejected", err)
		}
		for _, tc := range []struct {
			name   string
			change func(*routetable.RouteEntry)
		}{
			{"foreign interface", func(r *routetable.RouteEntry) { r.Interface = "en0" }},
			{"subnet route", func(r *routetable.RouteEntry) { r.Dst.Prefix = netip.PrefixFrom(local, local.BitLen()-1).Masked() }},
			{"peer address", func(r *routetable.RouteEntry) { r.Dst.Prefix = netip.PrefixFrom(local.Next(), local.BitLen()) }},
			{"gateway route", func(r *routetable.RouteEntry) {
				r.Sys = routetable.RouteEntryBSD{RawFlags: unix.RTF_UP | unix.RTF_HOST | unix.RTF_GATEWAY}
			}},
			{"reject route", func(r *routetable.RouteEntry) {
				r.Sys = routetable.RouteEntryBSD{RawFlags: unix.RTF_UP | unix.RTF_HOST | unix.RTF_REJECT}
			}},
			{"unknown metadata", func(r *routetable.RouteEntry) { r.Sys = nil }},
			{"not kernel local", func(r *routetable.RouteEntry) {
				r.Sys = routetable.RouteEntryBSD{RawFlags: unix.RTF_UP | unix.RTF_HOST}
			}},
		} {
			t.Run(local.String()+"/"+tc.name, func(t *testing.T) {
				r := base
				tc.change(&r)
				if checkRoutes([]routetable.RouteEntry{r}, "utun9", owned) == nil {
					t.Fatal("foreign or unverified route accepted")
				}
			})
		}
		if checkRoutes([]routetable.RouteEntry{base}, "utun9", nil) == nil || CheckRoutes([]routetable.RouteEntry{base}, "") == nil {
			t.Fatal("local route accepted without live ownership")
		}
	}
}

func TestDarwinIPv6HostPrefixUsesKernelFlags(t *testing.T) {
	r := testRoute(routing.Host6.String()+"/32", "lo0")
	r.Sys = routetable.RouteEntryBSD{RawFlags: unix.RTF_UP | unix.RTF_HOST | unix.RTF_LOCAL}
	if routePrefix(r) != netip.PrefixFrom(routing.Host6, 128) {
		t.Fatal("IPv6 host route was treated as a /32 subnet")
	}
	if err := checkRoutes([]routetable.RouteEntry{r}, "utun9", map[netip.Addr]bool{routing.Host6: true}); err != nil {
		t.Fatal("verified local IPv6 host route rejected", err)
	}
	r.Sys = routetable.RouteEntryBSD{RawFlags: unix.RTF_UP}
	if CheckRoutes([]routetable.RouteEntry{r}, "") == nil {
		t.Fatal("actual overlapping subnet route accepted")
	}
	r = testRoute("fd74:6169:ffff::1/32", "utun4")
	r.Sys = routetable.RouteEntryBSD{RawFlags: unix.RTF_UP | unix.RTF_HOST}
	if err := CheckRoutes([]routetable.RouteEntry{r}, ""); err != nil {
		t.Fatal("unrelated IPv6 host route was treated as an overlapping subnet")
	}
}
