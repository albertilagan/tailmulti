package routing

import (
	"encoding/json"
	"net/netip"
	"testing"
)

func TestRetainedCanonicalCollisions(t *testing.T) {
	s, snaps := fixtures()
	ip := netip.MustParseAddr("100.64.0.2")
	p, err := Build(&s, snaps)
	if err != nil {
		t.Fatal(err)
	}
	if !p.Canonical[ip].Ambiguous || p.Canonical[ip].Active {
		t.Fatal("ambiguous DNS owner accepted")
	}
	s.Accounts[1].Enabled = false
	s.Accounts[1].Removed = true
	raw, _ := json.Marshal(s)
	var restored State
	_ = json.Unmarshal(raw, &restored)
	p, err = Build(&restored, snaps[:1])
	if err != nil {
		t.Fatal(err)
	}
	if !p.Canonical[ip].Ambiguous || p.Canonical[ip].Active {
		t.Fatal("removed account released canonical ownership")
	}
	if len(p.Records["server.home.ts.net"]) != 2 {
		t.Fatal("collision broke explicit MagicDNS")
	}
}

func TestRetainedSelfAddressAndAliases(t *testing.T) {
	s, snaps := fixtures()
	snaps[1].Peers = nil
	snaps[1].SelfIPs = []netip.Addr{netip.MustParseAddr("100.64.0.2")}
	s.Aliases = map[string]string{"app.example.com": home}
	p, err := Build(&s, snaps)
	if err != nil {
		t.Fatal(err)
	}
	if !p.Canonical[snaps[1].SelfIPs[0]].Ambiguous {
		t.Fatal("peer/self conflict accepted")
	}
	s.Accounts[1].Enabled = false
	p, err = Build(&s, snaps[:1])
	if err != nil {
		t.Fatal(err)
	}
	if !p.Canonical[snaps[1].SelfIPs[0]].Ambiguous || p.Aliases["app.example.com"] != home {
		t.Fatal("retained ownership lost")
	}
	s.Accounts[0].Enabled = false
	p, err = Build(&s, nil)
	if err != nil {
		t.Fatal(err)
	}
	if p.ActiveProfiles[home] || p.Canonical[netip.MustParseAddr("100.64.0.2")].Active {
		t.Fatal("inactive owner published")
	}
}
