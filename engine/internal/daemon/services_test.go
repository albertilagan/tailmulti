package daemon

import (
	"context"
	"net/netip"
	"testing"

	"golang.org/x/net/dns/dnsmessage"
	"tailmulti.local/engine/internal/names"
	"tailmulti.local/engine/internal/routing"
	"tailmulti.local/engine/profile"
	"tailmulti.local/engine/tunmux"
)

func TestServiceLeaseMustSaveBeforeDNSPublication(t *testing.T) {
	const id = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	disk := &memoryDisk{s: routing.State{Version: 1, Accounts: []routing.Account{{ID: id, Name: "Home", Enabled: true}}, Domains: []string{"home.ts.net"}}}
	dns := new(names.Resolver)
	m, err := New(context.Background(), disk, nil, nil, nil, dns)
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()
	_, cancel := context.WithCancel(context.Background())
	m.runtimes[id] = &runtime{cancel: cancel, tun: tunmux.NewChanTUN("service-persistence"), engine: &fakeEngine{st: profile.Status{BackendState: "Running", MagicDNSSuffix: "home.ts.net", SelfIPs: []netip.Addr{netip.MustParseAddr("100.64.0.1")}, Services: []profile.ServiceStatus{{Name: "svc:dashboard", DNSName: "dashboard.home.ts.net", TailscaleIPs: []netip.Addr{netip.MustParseAddr("100.96.0.10")}}}}}}
	query, _ := (&dnsmessage.Message{Questions: []dnsmessage.Question{{Name: dnsmessage.MustNewName("dashboard.home.ts.net."), Type: dnsmessage.TypeA, Class: dnsmessage.ClassINET}}}).Pack()
	disk.fail = true
	if err := m.reconcile(); err == nil {
		t.Fatal("service lease save failure was ignored")
	}
	var answer dnsmessage.Message
	if err := answer.Unpack(dns.Reply(query)); err != nil || len(answer.Answers) != 0 {
		t.Fatal("service DNS published before durable lease", err)
	}
	disk.fail = false
	if err := m.reconcile(); err != nil {
		t.Fatal(err)
	}
	if err := answer.Unpack(dns.Reply(query)); err != nil || len(answer.Answers) != 1 || len(disk.s.Leases) != 1 {
		t.Fatal("saved service lease not published", err)
	}
}
