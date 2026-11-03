package daemon

import (
	"context"
	"errors"
	"net/netip"
	"testing"

	"golang.org/x/net/dns/dnsmessage"
	"tailmulti.local/engine/internal/names"
	"tailmulti.local/engine/internal/routing"
	"tailmulti.local/engine/profile"
	"tailmulti.local/engine/tunmux"
)

type aliasNetwork struct {
	disk   *memoryDisk
	fail   bool
	claims int
}

func (n *aliasNetwork) Apply(p *routing.Plan) error {
	for name, owner := range p.Aliases {
		if n.disk.s.Aliases[name] != owner {
			panic("DNS published before durable ownership")
		}
	}
	if n.fail {
		return errors.New("resolver path conflict")
	}
	n.claims = len(p.Aliases)
	return nil
}
func aliasManager(t *testing.T) (*Manager, *memoryDisk, *aliasNetwork, *names.Resolver, AccountView, AccountView) {
	t.Helper()
	disk := &memoryDisk{s: routing.State{Version: 1}}
	network := &aliasNetwork{disk: disk}
	resolver := new(names.Resolver)
	factory := func(a routing.Account, _ *tunmux.ChanTUN) (profile.Engine, error) {
		ip := "100.64.0.2"
		if a.Name == "Work" {
			ip = "100.64.0.3"
		}
		return &fakeEngine{st: profile.Status{BackendState: "Running", MagicDNSSuffix: a.Name + ".ts.net", SelfIPs: []netip.Addr{netip.MustParseAddr("100.64.0.1")}, Peers: []profile.PeerStatus{{NodeID: "peer", DNSName: "peer." + a.Name + ".ts.net", TailscaleIPs: []netip.Addr{netip.MustParseAddr(ip)}}}}}, nil
	}
	m, err := New(context.Background(), disk, factory, network, nil, resolver)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(m.Close)
	a, err := m.Add("Home")
	if err != nil {
		t.Fatal(err)
	}
	b, err := m.Add("Work")
	if err != nil {
		t.Fatal(err)
	}
	if m.Set(a.ID, true, false) != nil || m.Set(b.ID, true, false) != nil {
		t.Fatal("start fixtures")
	}
	return m, disk, network, resolver, a, b
}

func TestClaimPersistenceIsolationAndRestart(t *testing.T) {
	m, disk, network, resolver, home, work := aliasManager(t)
	for _, c := range []struct{ n, id string }{{"personal.example.com", home.ID}, {"company.example.com", work.ID}} {
		if err := m.ClaimNames(context.Background(), []string{c.n}, c.id); err != nil {
			t.Fatal(err)
		}
	}
	if network.claims != 2 {
		t.Fatal("missing host claims")
	}
	if m.ClaimNames(context.Background(), []string{"personal.example.com"}, work.ID) == nil {
		t.Fatal("alias changed account")
	}
	if m.ClaimNames(context.Background(), []string{"anything.home.ts.net"}, work.ID) == nil {
		t.Fatal("tailnet namespace shadowed")
	}
	if m.Set(home.ID, false, false) != nil {
		t.Fatal("disconnect")
	}
	if m.ClaimNames(context.Background(), []string{"new.example.com"}, home.ID) == nil {
		t.Fatal("inactive owner accepted")
	}
	if !m.plan.ActiveProfiles[work.ID] || len(m.plan.Records["peer.work.ts.net"]) != 1 {
		t.Fatal("other account broken")
	}
	if disk.s.Aliases["personal.example.com"] != home.ID {
		t.Fatal("disconnected alias forgotten")
	}
	forwarded := false
	resolver.EnableForwarding(context.Background(), func(context.Context, []byte) ([]byte, error) { forwarded = true; return nil, errors.New("unexpected") }, m.ClaimNames)
	q := dnsmessage.Message{Questions: []dnsmessage.Question{{Name: dnsmessage.MustNewName("personal.example.com."), Class: dnsmessage.ClassINET, Type: dnsmessage.TypeA}}}
	wire, _ := q.Pack()
	var reply dnsmessage.Message
	if reply.Unpack(resolver.Reply(wire)) != nil || reply.RCode != dnsmessage.RCodeServerFailure || forwarded {
		t.Fatal("inactive alias escaped")
	}
	m.Close()
	restarted, err := New(context.Background(), disk, nil, network, nil, new(names.Resolver))
	if err != nil {
		t.Fatal(err)
	}
	defer restarted.Close()
	if restarted.plan.Aliases["personal.example.com"] != home.ID || restarted.plan.ActiveProfiles[home.ID] {
		t.Fatal("restart lost tombstone")
	}
}

func TestClaimFailsBeforeAnswerOnDiskOrHostFailure(t *testing.T) {
	for _, failure := range []string{"disk", "host", "cancel"} {
		t.Run(failure, func(t *testing.T) {
			m, disk, network, resolver, _, work := aliasManager(t)
			switch failure {
			case "disk":
				disk.fail = true
			case "host":
				network.fail = true
			}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			if failure == "cancel" {
				cancel()
			}
			err := m.ClaimNames(ctx, []string{"company.example.com"}, work.ID)
			if err == nil {
				t.Fatal("claim failure hidden")
			}
			if failure == "host" {
				if disk.s.Aliases["company.example.com"] != work.ID || m.health == "ready" {
					t.Fatal("failed host publication did not block")
				}
				q := dnsmessage.Message{Questions: []dnsmessage.Question{{Name: dnsmessage.MustNewName("company.example.com."), Type: dnsmessage.TypeA, Class: dnsmessage.ClassINET}}}
				b, _ := q.Pack()
				var a dnsmessage.Message
				_ = a.Unpack(resolver.Reply(b))
				if len(a.Answers) > 0 {
					t.Fatal("host failure released answer")
				}
			} else if network.claims != 0 || len(disk.s.Aliases) != 0 {
				t.Fatal("failed save reached host")
			}
		})
	}
}
