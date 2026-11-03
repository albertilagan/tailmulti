package names

import (
	"context"
	"errors"
	"net/netip"
	"testing"

	"golang.org/x/net/dns/dnsmessage"
	"tailmulti.local/engine/internal/routing"
	"tailmulti.local/engine/profile"
)

func TestServiceDNSDiscoveryAndWithdrawal(t *testing.T) {
	const id = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	state := routing.State{Version: 1, Accounts: []routing.Account{{ID: id, Name: "Home", Enabled: true}}}
	snapshot := profile.Status{ProfileID: id, BackendState: "Running", MagicDNSSuffix: "home.ts.net", SelfIPs: []netip.Addr{netip.MustParseAddr("100.64.0.1"), netip.MustParseAddr("fd7a:115c:a1e0::1")}, Services: []profile.ServiceStatus{{Name: "svc:dashboard", DNSName: "dashboard.home.ts.net", TailscaleIPs: []netip.Addr{netip.MustParseAddr("100.96.0.10"), netip.MustParseAddr("fd7a:115c:a1e0::100a")}}}}
	plan, err := routing.Build(&state, []profile.Status{snapshot})
	if err != nil {
		t.Fatal(err)
	}
	r := new(Resolver)
	r.Set(plan)
	forwarded := false
	r.EnableForwarding(context.Background(), func(context.Context, []byte) ([]byte, error) {
		forwarded = true
		return nil, errors.New("tailnet service must not use public DNS")
	}, nil)
	for _, typ := range []dnsmessage.Type{dnsmessage.TypeA, dnsmessage.TypeAAAA} {
		answer := decodeReply(t, r.Reply(query("dashboard.home.ts.net.", typ)))
		if answer.RCode != 0 || len(answer.Answers) != 1 {
			t.Fatalf("discovered service has no DNS answer: %+v", answer)
		}
		ip, ok := resourceIP(answer.Answers[0])
		if !ok || !routing.IsSynthetic(ip) {
			t.Fatal("service exposed canonical address")
		}
	}
	if forwarded {
		t.Fatal("service query leaked to public DNS")
	}
	snapshot.Services = nil
	plan, err = routing.Build(&state, []profile.Status{snapshot})
	if err != nil {
		t.Fatal(err)
	}
	r.Set(plan)
	answer := decodeReply(t, r.Reply(query("dashboard.home.ts.net.", dnsmessage.TypeA)))
	if answer.RCode != dnsmessage.RCodeNameError || forwarded {
		t.Fatal("withdrawn service name escaped its account")
	}
}
