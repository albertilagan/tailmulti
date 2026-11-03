package routing

import (
	"encoding/json"
	"net/netip"
	"slices"
	"testing"

	"tailmulti.local/engine/profile"
	"tailscale.com/net/packet"
)

func serviceFixtures() (State, []profile.Status) {
	s, snapshots := fixtures()
	for i := range snapshots {
		snapshots[i].Services = []profile.ServiceStatus{{Name: "svc:dashboard", DNSName: "dashboard." + snapshots[i].MagicDNSSuffix, TailscaleIPs: []netip.Addr{netip.MustParseAddr("100.96.0.10"), netip.MustParseAddr("fd7a:115c:a1e0::100a")}}}
	}
	return s, snapshots
}

func TestServicePacketOwnershipAndLifecycle(t *testing.T) {
	s, snapshots := serviceFixtures()
	p, err := Build(&s, snapshots)
	if err != nil {
		t.Fatal(err)
	}
	before := map[string][]netip.Addr{}
	for i, id := range []string{home, work} {
		name := snapshots[i].Services[0].DNSName
		before[id] = slices.Clone(p.Records[name])
		if len(before[id]) != 2 || p.NameOwners[name] != id {
			t.Fatalf("service must resolve automatically in its own account: %s: %v", name, before[id])
		}
		for family, effective := range before[id] {
			host := []netip.Addr{Host4, Host6}[family]
			vip := snapshots[i].Services[0].TailscaleIPs[family]
			out, route, err := p.Mapper.Outbound(udp(host, effective))
			if err != nil || route.ProfileID != id {
				t.Fatalf("service selected wrong account: %v %v", route, err)
			}
			var parsed packet.Parsed
			parsed.Decode(out)
			if parsed.Dst.Addr() != vip || parsed.Src.Addr() != snapshots[i].SelfIPs[family] {
				t.Fatal("service packet did not retain VIP destination and account source")
			}
			reply, err := p.Mapper.Inbound(id, udp(vip, parsed.Src.Addr()))
			if err != nil {
				t.Fatal(err)
			}
			parsed.Decode(reply)
			if parsed.Src.Addr() != effective || parsed.Dst.Addr() != host {
				t.Fatal("service reply crossed address ownership")
			}
			if !p.Canonical[vip].Ambiguous {
				t.Fatal("overlapping service VIP accepted for automatic public-DNS translation")
			}
		}
	}
	if before[home][0] == before[work][0] || len(p.Devices) != 2 {
		t.Fatal("services share local addresses or are misrepresented as devices")
	}
	for _, change := range []string{"withdraw", "disconnect", "remove"} {
		t.Run(change, func(t *testing.T) {
			raw, _ := json.Marshal(s)
			var state State
			if err := json.Unmarshal(raw, &state); err != nil {
				t.Fatal(err)
			}
			current := slices.Clone(snapshots)
			switch change {
			case "withdraw":
				current[0].Services = nil
			case "disconnect":
				state.Accounts[0].Enabled = false
			case "remove":
				state.Accounts[0].Enabled = false
				state.Accounts[0].Removed = true
			}
			inactive, err := Build(&state, current)
			if err != nil {
				t.Fatal(err)
			}
			if len(inactive.Records["dashboard.home.ts.net"]) != 0 || !slices.Contains(inactive.Domains, "dashboard.home.ts.net") || len(inactive.Host.Routes) != len(p.Host.Routes) {
				t.Fatal("withdrawal lost DNS or address tombstones")
			}
			for family, old := range before[home] {
				host := []netip.Addr{Host4, Host6}[family]
				if _, _, err := inactive.Mapper.Outbound(udp(host, old)); err == nil {
					t.Fatal("cached service address escaped after withdrawal")
				}
				vip := snapshots[0].Services[0].TailscaleIPs[family]
				if _, err := inactive.Mapper.Inbound(home, udp(vip, snapshots[0].SelfIPs[family])); err == nil {
					t.Fatal("withdrawn service reply was accepted")
				}
				if _, r, err := inactive.Mapper.Outbound(udp(host, before[work][family])); err != nil || r.ProfileID != work {
					t.Fatal("withdrawing Personal broke Work")
				}
				if !inactive.Canonical[vip].Ambiguous || inactive.Canonical[vip].Active {
					t.Fatal("withdrawing service released canonical ownership")
				}
			}
			state.Accounts[0].Enabled = true
			state.Accounts[0].Removed = false
			restarted, err := Build(&state, snapshots)
			if err != nil || !slices.Equal(restarted.Records["dashboard.home.ts.net"], before[home]) {
				t.Fatal("service addresses changed after restart", err)
			}
		})
	}
}

func TestServiceNameAndAddressConflicts(t *testing.T) {
	for _, kind := range []string{"peer name", "other account name", "peer address", "other service address", "invalid address", "mapped address", "resolver address", "own address", "reserved device identity", "invalid name", "foreign suffix", "duplicate service"} {
		t.Run(kind, func(t *testing.T) {
			s, snapshots := serviceFixtures()
			wantError := true
			switch kind {
			case "peer name":
				snapshots[0].Peers[0].DNSName = "dashboard.home.ts.net"
				wantError = false
			case "other account name":
				snapshots[1].MagicDNSSuffix = "home.ts.net"
				snapshots[1].Services[0].DNSName = "dashboard.home.ts.net"
				wantError = false
			case "peer address":
				snapshots[0].Services[0].TailscaleIPs = snapshots[0].Peers[0].TailscaleIPs
			case "other service address":
				other := snapshots[0].Services[0]
				other.Name, other.DNSName = "svc:other", "other.home.ts.net"
				snapshots[0].Services = append(snapshots[0].Services, other)
			case "invalid address":
				snapshots[0].Services[0].TailscaleIPs = []netip.Addr{netip.MustParseAddr("192.168.1.10")}
			case "mapped address":
				snapshots[0].Services[0].TailscaleIPs = []netip.Addr{netip.MustParseAddr("::ffff:100.96.0.10")}
			case "resolver address":
				snapshots[0].Services[0].TailscaleIPs = []netip.Addr{netip.MustParseAddr("100.100.100.100")}
			case "own address":
				snapshots[0].Services[0].TailscaleIPs = snapshots[0].SelfIPs
			case "reserved device identity":
				snapshots[0].Peers[0].NodeID = "service/svc:dashboard"
			case "invalid name":
				snapshots[0].Services[0].Name = "svc:bad/name"
			case "foreign suffix":
				snapshots[0].Services[0].DNSName = "dashboard.work.ts.net"
			case "duplicate service":
				snapshots[0].Services = append(snapshots[0].Services, snapshots[0].Services[0])
			}
			p, err := Build(&s, snapshots)
			if wantError {
				if err == nil {
					t.Fatal("ambiguous or malformed service was accepted")
				}
			} else if err != nil || len(p.Records["dashboard.home.ts.net"]) != 0 || len(p.Conflicts) == 0 {
				t.Fatal("ambiguous service name resolved", err)
			}
		})
	}
}

func TestServiceHostReplacementKeepsAddresses(t *testing.T) {
	s, snapshots := serviceFixtures()
	snapshots = snapshots[:1]
	snapshots[0].AvailableRoutes = []profile.RouteStatus{{Prefix: netip.MustParsePrefix("100.96.0.10/32"), PrimaryRouter: "first"}, {Prefix: netip.MustParsePrefix("fd7a:115c:a1e0::100a/128"), PrimaryRouter: "first"}}
	p, err := Build(&s, snapshots)
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Conflicts) != 0 {
		t.Fatal("service host route incorrectly called an unsupported subnet")
	}
	before := slices.Clone(p.Records["dashboard.home.ts.net"])
	snapshots[0].Peers = nil
	for i := range snapshots[0].AvailableRoutes {
		snapshots[0].AvailableRoutes[i].PrimaryRouter = "replacement"
	}
	p, err = Build(&s, snapshots)
	if err != nil || !slices.Equal(before, p.Records["dashboard.home.ts.net"]) {
		t.Fatal("service host change reassigned the service address", err)
	}
	snapshots[0].AvailableRoutes = append(snapshots[0].AvailableRoutes, profile.RouteStatus{Prefix: netip.MustParsePrefix("10.0.0.0/24"), PrimaryRouter: "replacement"})
	p, err = Build(&s, snapshots)
	if err != nil || len(p.Conflicts) != 1 {
		t.Fatal("actual subnet no longer reported", err)
	}
	if _, _, err := p.Mapper.Outbound(udp(Host4, netip.MustParseAddr("10.0.0.1"))); err == nil {
		t.Fatal("service support imported an unrelated subnet")
	}
}
