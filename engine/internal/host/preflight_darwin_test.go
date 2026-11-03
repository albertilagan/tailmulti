//go:build darwin

package host

import (
	"encoding/json"
	"errors"
	"net/netip"
	"strconv"
	"strings"
	"testing"

	"golang.org/x/sys/unix"
	"tailmulti.local/engine/internal/names"

	"tailscale.com/net/routetable"
)

func testRoute(prefix, iface string) routetable.RouteEntry {
	return routetable.RouteEntry{Dst: routetable.RouteDestination{Prefix: netip.MustParsePrefix(prefix)}, Interface: iface}
}

func checkState(r PreflightReport, id string) string {
	for _, c := range r.Checks {
		if c.ID == id {
			return c.State
		}
	}
	return "missing"
}

func cleanObservations() observations {
	return observations{
		routes: []routetable.RouteEntry{testRoute("0.0.0.0/0", "en0")},
		dns:    "DNS configuration\nresolver #1\n nameserver[0] : 192.0.2.53\n",
	}
}

func TestPreflightVPNAndDNSDependency(t *testing.T) {
	o := cleanObservations()
	if r := assess(o); !r.Ready {
		t.Fatalf("ordinary configuration rejected: %+v", r.Checks)
	}
	o.routes = append(o.routes, testRoute("100.64.0.0/10", "utun4"), testRoute("192.0.2.0/24", "utun4"))
	r := assess(o)
	if r.Ready || checkState(r, "vpn") != "blocked" || checkState(r, "dns") != "blocked" {
		t.Fatalf("VPN DNS dependency accepted: %+v", r.Checks)
	}
	encoded, _ := json.Marshal(r)
	for _, private := range []string{"192.0.2.53", "utun4", "secret.example.ts.net"} {
		if strings.Contains(string(encoded), private) {
			t.Fatal("preflight exposed a host identifier")
		}
	}
	o.routes = append(o.routes, testRoute("192.0.2.53/32", "en0"))
	if checkState(assess(o), "dns") != "pass" {
		t.Fatal("most-specific DNS route ignored")
	}
}

func TestPreflightUnrelatedVPNDefaults(t *testing.T) {
	for _, tc := range []struct {
		name     string
		routes   []routetable.RouteEntry
		vpn, dns string
		ready    bool
	}{
		{
			name: "disconnected Tailscale with other IPv6 defaults",
			routes: []routetable.RouteEntry{
				testRoute("::/0", "utun0"), testRoute("::/0", "utun1"),
				testRoute("fe80::/64", "utun0"),
			},
			vpn: "pass", dns: "pass", ready: true,
		},
		{
			name:   "default carrying DNS still blocks activation",
			routes: []routetable.RouteEntry{testRoute("0.0.0.0/0", "utun0")},
			vpn:    "pass", dns: "blocked",
		},
		{
			name:   "IPv4 tailnet alongside unrelated defaults",
			routes: []routetable.RouteEntry{testRoute("::/0", "utun0"), testRoute("100.64.0.0/10", "utun4")},
			vpn:    "blocked", dns: "pass",
		},
		{
			name:   "IPv6 tailnet alongside unrelated defaults",
			routes: []routetable.RouteEntry{testRoute("::/0", "utun0"), testRoute("fd7a:115c:a1e0::/48", "utun4")},
			vpn:    "blocked", dns: "pass",
		},
		{
			name:   "tailnet host route still blocks activation",
			routes: []routetable.RouteEntry{testRoute("::/0", "utun0"), testRoute("100.101.102.103/32", "utun4")},
			vpn:    "blocked", dns: "pass",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			o := cleanObservations()
			o.routes = append(o.routes, tc.routes...)
			r := assess(o)
			if checkState(r, "vpn") != tc.vpn || checkState(r, "dns") != tc.dns || r.Ready != tc.ready {
				t.Fatalf("wrong activation decision: %+v", r)
			}
		})
	}
}

func TestPreflightUnknownIsNotReady(t *testing.T) {
	for _, field := range []string{"route", "dns", "socket", "path", "no_dns", "no_route"} {
		t.Run(field, func(t *testing.T) {
			o := cleanObservations()
			switch field {
			case "route":
				o.routeErr = errors.New("private failure")
			case "dns":
				o.dnsErr = errors.New("private failure")
			case "socket":
				o.socketErr = errors.New("private failure")
			case "path":
				o.pathErr = errors.New("private failure")
			case "no_dns":
				o.dns = ""
			case "no_route":
				o.routes = nil
			}
			if r := assess(o); r.Ready {
				t.Fatal("unchecked host accepted")
			}
		})
	}
}

func TestPreflightIgnoresSupplementalDNS(t *testing.T) {
	config := `DNS configuration
resolver #1
 nameserver[0] : 192.0.2.53
 nameserver[1] : 2001:db8::53
resolver #2
 domain : secret.example.ts.net
 nameserver[0] : 100.100.100.100
DNS configuration (for scoped queries)
resolver #1
 nameserver[0] : 10.0.0.53
`
	servers := defaultDNSServers(config)
	if len(servers) != 2 || servers[0].String() != "192.0.2.53" || servers[1].String() != "2001:db8::53" {
		t.Fatalf("wrong default resolvers: %v", servers)
	}
	o := cleanObservations()
	o.dns = config
	o.routes = append(o.routes, testRoute("::/0", "en0"), testRoute("100.64.0.0/10", "utun4"))
	if checkState(assess(o), "dns") != "pass" {
		t.Fatal("supplemental VPN DNS mistaken for default")
	}
}

func TestPreflightDNSPort(t *testing.T) {
	for _, tc := range []struct {
		line    string
		blocked bool
	}{
		{"tcp4 0 0 127.0.0.1.53 *.* LISTEN", true},
		{"udp4 0 0 *.53 *.*", true},
		{"udp6 0 0 ::1.53 *.*", true},
		{"tcp4 0 0 127.0.0.1.53 127.0.0.1.7000 ESTABLISHED", false},
		{"udp4 0 0 *.5353 *.*", false},
		{"udp4 0 0 100.100.100.100.53 *.*", false},
		{"garbage", false},
	} {
		if got := dnsPortInUse(tc.line); got != tc.blocked {
			t.Errorf("%q: blocked=%v", tc.line, got)
		}
	}
}

func TestPreflightAllowsFallbackDNSPorts(t *testing.T) {
	o := cleanObservations()
	o.sockets = "tcp4 0 0 127.0.0.1.53 *.* LISTEN\n"
	if r := assess(o); !r.Ready || checkState(r, "dns_port") != "pass" {
		t.Fatal("occupied default port blocked available fallback", r)
	}
	for _, port := range names.SystemDNSPorts() {
		o.sockets += "udp4 0 0 *." + strconv.Itoa(int(port)) + " *.*\n"
	}
	if r := assess(o); r.Ready || checkState(r, "dns_port") != "blocked" {
		t.Fatal("all occupied ports accepted", r)
	}
}

func TestPreflightPoolAndIPv6VPN(t *testing.T) {
	o := cleanObservations()
	o.routes = append(o.routes, testRoute("198.18.1.0/24", "en0"), testRoute("fd7a:115c:a1e0::/48", "utun8"))
	r := assess(o)
	if r.Ready || checkState(r, "routes") != "blocked" || checkState(r, "vpn") != "blocked" {
		t.Fatal("pool collision or IPv6 VPN missed")
	}
}

func TestIPv6DNSRouteUsesActualHostMask(t *testing.T) {
	server := netip.MustParseAddr("2001:db8::53")
	host := testRoute("2001:db8::1/32", "utun4")
	host.Sys = routetable.RouteEntryBSD{RawFlags: unix.RTF_UP | unix.RTF_HOST}
	routes := []routetable.RouteEntry{testRoute("2001:db8::/64", "en0"), host}
	if viaVPN, routed := dnsRoutes([]netip.Addr{server}, routes); viaVPN || !routed {
		t.Fatal("unrelated IPv6 host route captured DNS")
	}
	routes[1].Dst.Prefix = netip.PrefixFrom(server, 32)
	if viaVPN, routed := dnsRoutes([]netip.Addr{server}, routes); !viaVPN || !routed {
		t.Fatal("actual VPN DNS host route was ignored")
	}
}
