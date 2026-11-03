package routing

import (
	"encoding/json"
	"net/netip"
	"testing"

	"tailmulti.local/engine/profile"
	"tailscale.com/net/packet"
)

const home = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
const work = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"

func fixtures() (State, []profile.Status) {
	s := State{Version: 1, Accounts: []Account{{ID: home, Name: "Home", Enabled: true}, {ID: work, Name: "Work", Enabled: true}}}
	var snaps []profile.Status
	for i, id := range []string{home, work} {
		suffix := []string{"home.ts.net", "work.ts.net"}[i]
		snaps = append(snaps, profile.Status{ProfileID: id, BackendState: "Running", SelfIPs: []netip.Addr{netip.MustParseAddr("100.64.0.1"), netip.MustParseAddr("fd7a:115c:a1e0::1")}, MagicDNSSuffix: suffix, Peers: []profile.PeerStatus{{NodeID: "same-node-id", DNSName: "server." + suffix, TailscaleIPs: []netip.Addr{netip.MustParseAddr("100.64.0.2"), netip.MustParseAddr("fd7a:115c:a1e0::2")}}}})
	}
	return s, snaps
}
func udp(src, dst netip.Addr) []byte {
	if src.Is6() {
		return packet.Generate(packet.UDP6Header{IP6Header: packet.IP6Header{Src: src, Dst: dst}, SrcPort: 4321, DstPort: 22}, []byte("test"))
	}
	return packet.Generate(packet.UDP4Header{IP4Header: packet.IP4Header{Src: src, Dst: dst}, SrcPort: 4321, DstPort: 22}, []byte("test"))
}
func TestTwoOverlappingTailnetsDisconnectAndRestart(t *testing.T) {
	s, snaps := fixtures()
	p, err := Build(&s, snaps)
	if err != nil {
		t.Fatal(err)
	}
	for family := 0; family < 2; family++ {
		a, b := p.Records["server.home.ts.net"][family], p.Records["server.work.ts.net"][family]
		if a == b {
			t.Fatal("collision")
		}
		hostIP := Host4
		if family == 1 {
			hostIP = Host6
		}
		for _, dest := range []struct {
			ip netip.Addr
			id string
		}{{a, home}, {b, work}} {
			translated, r, err := p.Mapper.Outbound(udp(hostIP, dest.ip))
			if err != nil || r.ProfileID != dest.id {
				t.Fatalf("selection: %v %v", r, err)
			}
			var parsed packet.Parsed
			parsed.Decode(translated)
			if parsed.Src.Addr() != snaps[0].SelfIPs[family] || parsed.Dst.Addr() != snaps[0].Peers[0].TailscaleIPs[family] {
				t.Fatal("wrong canonical addresses")
			}
			reply, err := p.Mapper.Inbound(dest.id, udp(parsed.Dst.Addr(), parsed.Src.Addr()))
			if err != nil {
				t.Fatal(err)
			}
			parsed.Decode(reply)
			if parsed.Src.Addr() != dest.ip || parsed.Dst.Addr() != hostIP {
				t.Fatal("wrong return translation")
			}
		}
	}
	before := append([]netip.Addr(nil), p.Records["server.home.ts.net"]...)
	s.Accounts[0].Enabled = false
	p, err = Build(&s, snaps)
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Records["server.home.ts.net"]) != 0 {
		t.Fatal("disabled DNS visible")
	}
	if _, _, err = p.Mapper.Outbound(udp(Host4, before[0])); err == nil {
		t.Fatal("cached address escaped")
	}
	if _, r, err := p.Mapper.Outbound(udp(Host4, p.Records["server.work.ts.net"][0])); err != nil || r.ProfileID != work {
		t.Fatal("other account broken")
	}
	if len(p.Host.Routes) != 4 {
		t.Fatal("tombstone routes lost")
	}
	raw, _ := json.Marshal(s)
	var reloaded State
	json.Unmarshal(raw, &reloaded)
	reloaded.Accounts[0].Enabled = true
	p, err = Build(&reloaded, snaps)
	if err != nil {
		t.Fatal(err)
	}
	if p.Records["server.home.ts.net"][0] != before[0] {
		t.Fatal("restart reassigned address")
	}
}
func TestNamesAndTransitFailClosed(t *testing.T) {
	s, snaps := fixtures()
	snaps[1].Peers[0].DNSName = snaps[0].Peers[0].DNSName
	p, err := Build(&s, snaps)
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Records) != 0 || len(p.Conflicts) == 0 {
		t.Fatal("ambiguous full name resolved")
	}
	l := s.Leases[0]
	if _, _, err = p.Mapper.Outbound(udp(netip.MustParseAddr("100.64.0.99"), l.EffectiveIP)); err == nil {
		t.Fatal("non-host source admitted")
	}
	if _, err = p.Mapper.Inbound(home, udp(netip.MustParseAddr("100.64.0.2"), netip.MustParseAddr("100.64.0.99"))); err == nil {
		t.Fatal("transit inbound admitted")
	}
}
func TestUnavailableNetmapAndRemovedLease(t *testing.T) {
	s, snaps := fixtures()
	p, err := Build(&s, snaps)
	if err != nil {
		t.Fatal(err)
	}
	old := p.Records["server.home.ts.net"][0]
	s.Accounts[0].Removed = true
	s.Accounts[0].Enabled = false
	snaps[1].BackendState = "NeedsLogin"
	p, err = Build(&s, snaps)
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Records) != 0 || len(p.Host.Routes) != 4 {
		t.Fatal("unavailable account has DNS or lost guards")
	}
	if _, _, err = p.Mapper.Outbound(udp(Host4, old)); err == nil {
		t.Fatal("removed mapping active")
	}
	s.Leases[1].EffectiveIP = s.Leases[0].EffectiveIP
	if _, err = Build(&s, nil); err == nil {
		t.Fatal("corrupt duplicate lease accepted")
	}
}
func FuzzMapperMalformed(f *testing.F) {
	f.Add([]byte{0x45})
	f.Add(udp(Host4, netip.MustParseAddr("198.18.0.2")))
	s, snaps := fixtures()
	p, _ := Build(&s, snaps)
	f.Fuzz(func(t *testing.T, b []byte) {
		p.Mapper.Outbound(append([]byte(nil), b...))
		p.Mapper.Inbound(home, append([]byte(nil), b...))
	})
}
