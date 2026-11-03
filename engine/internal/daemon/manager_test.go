package daemon

import (
	"context"
	"errors"
	"golang.org/x/net/dns/dnsmessage"
	"net"
	"net/netip"
	"strings"
	"tailmulti.local/engine/internal/names"
	"tailmulti.local/engine/internal/routing"
	"tailmulti.local/engine/internal/setup"
	"tailmulti.local/engine/internal/store"
	"tailmulti.local/engine/profile"
	"tailmulti.local/engine/tunmux"
	"testing"
	"time"
)

type memoryDisk struct {
	s    routing.State
	fail bool
}

func (d *memoryDisk) Load() (routing.State, error) { return store.Clone(d.s), nil }
func (d *memoryDisk) Save(s routing.State) error {
	if d.fail {
		return errors.New("disk failure")
	}
	d.s = store.Clone(s)
	return nil
}

type fakeEngine struct {
	st     profile.Status
	closed bool
	fail   bool
}

func (e *fakeEngine) Start(context.Context) error { return nil }
func (e *fakeEngine) Close() error                { e.closed = true; return nil }
func (e *fakeEngine) Dial(context.Context, string, string) (net.Conn, error) {
	return nil, errors.New("unused")
}
func (e *fakeEngine) Status(context.Context) (profile.Status, error) {
	if e.fail {
		return profile.Status{}, errors.New("status failure")
	}
	return e.st, nil
}
func (e *fakeEngine) WatchUpdates(ctx context.Context, _ func()) error { <-ctx.Done(); return nil }

type fakeNetwork struct {
	applied int
	disk    *memoryDisk
	fail    bool
	err     error
}

func TestClosedSessionCannotStartSavedIdentities(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	disk := &memoryDisk{s: routing.State{Version: 1, Accounts: []routing.Account{
		{ID: strings.Repeat("a", 32), Name: "Home", Enabled: true},
	}}}
	starts := 0
	factory := func(routing.Account, *tunmux.ChanTUN) (profile.Engine, error) {
		starts++
		return &fakeEngine{}, nil
	}
	m, err := New(ctx, disk, factory, nil, nil, new(names.Resolver))
	if err != nil {
		t.Fatal(err)
	}
	m.Close()
	m.Run()
	if starts != 0 || len(m.runtimes) != 0 {
		t.Fatal("closed session started saved identities")
	}
}

func TestGlobalStopPreservesWhichAccountsResume(t *testing.T) {
	disk := &memoryDisk{s: routing.State{Version: 1, Accounts: []routing.Account{
		{ID: strings.Repeat("a", 32), Name: "Personal", Enabled: true},
		{ID: strings.Repeat("b", 32), Name: "Paused", Enabled: false},
		{ID: strings.Repeat("c", 32), Name: "Work", Enabled: true},
	}}}
	for cycle := 0; cycle < 2; cycle++ {
		ctx, cancel := context.WithCancel(context.Background())
		started := make(chan string, 3)
		factory := func(a routing.Account, _ *tunmux.ChanTUN) (profile.Engine, error) {
			started <- a.ID
			return &fakeEngine{st: profile.Status{BackendState: "Running"}}, nil
		}
		m, err := New(ctx, disk, factory, nil, nil, new(names.Resolver))
		if err != nil {
			cancel()
			t.Fatal(err)
		}
		done := make(chan struct{})
		go func() { m.Run(); close(done) }()
		seen := map[string]bool{}
		for len(seen) < 2 {
			select {
			case id := <-started:
				seen[id] = true
			case <-time.After(time.Second):
				m.Close()
				cancel()
				<-done
				t.Fatal("saved connected accounts did not resume")
			}
		}
		m.Close()
		cancel()
		<-done
		if !seen[strings.Repeat("a", 32)] || !seen[strings.Repeat("c", 32)] || seen[strings.Repeat("b", 32)] {
			t.Fatal("global restart changed the selected accounts")
		}
		for _, a := range disk.s.Accounts {
			if a.Enabled != (a.Name != "Paused") {
				t.Fatal("global stop overwrote connection preferences")
			}
		}
	}
}

func (n *fakeNetwork) Apply(p *routing.Plan) error {
	n.applied++
	if n.err != nil {
		return n.err
	}
	if len(p.Devices) > 0 && len(n.disk.s.Leases) == 0 {
		panic("published before save")
	}
	if n.fail {
		return errors.New("host failure")
	}
	return nil
}

func TestHostFailureShowsSafeReasonAndRecovers(t *testing.T) {
	for _, tc := range []struct {
		name string
		err  error
		want string
	}{
		{"route conflict", errors.Join(setup.RouteConflict, errors.New("private-host-address")), "a host route conflicts with Tailmulti"},
		{"unknown failure", errors.New("private-host-address"), "configuration failed"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			disk := &memoryDisk{s: routing.State{Version: 1}}
			network := &fakeNetwork{disk: disk}
			m, err := New(context.Background(), disk, nil, network, nil, new(names.Resolver))
			if err != nil {
				t.Fatal(err)
			}
			defer m.Close()
			network.err = tc.err
			if err := m.reconcile(); err == nil || strings.Contains(err.Error(), "private-host-address") {
				t.Fatal("missing or unsafe public failure")
			}
			status := m.Status()
			if status.Health != "blocked: "+tc.want+"; all packet routes disabled" || len(status.Devices) != 0 {
				t.Fatalf("wrong redacted status: %+v", status)
			}
			network.err = nil
			if err := m.reconcile(); err != nil || m.Status().Health != "ready" {
				t.Fatal("host recovery retained stale failure")
			}
		})
	}
}
func TestLifecycleIsolationPersistenceAndNoCredentialsInStatus(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	disk := &memoryDisk{s: routing.State{Version: 1}}
	network := &fakeNetwork{disk: disk}
	engines := map[string]*fakeEngine{}
	factory := func(a routing.Account, _ *tunmux.ChanTUN) (profile.Engine, error) {
		e := &fakeEngine{st: profile.Status{ProfileID: a.ID, BackendState: "Running", AuthURL: "https://login.tailscale.com/a/SECRET", MagicDNSSuffix: strings.ToLower(a.Name) + ".ts.net", SelfIPs: []netip.Addr{netip.MustParseAddr("100.64.0.1")}, Peers: []profile.PeerStatus{{NodeID: "same-id", DNSName: "server." + strings.ToLower(a.Name) + ".ts.net", TailscaleIPs: []netip.Addr{netip.MustParseAddr("100.64.0.2")}}}}}
		engines[a.ID] = e
		return e, nil
	}
	m, err := New(ctx, disk, factory, network, nil, new(names.Resolver))
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()
	a, _ := m.Add("Home")
	b, _ := m.Add("Work")
	if err = m.Set(a.ID, true, false); err != nil {
		t.Fatal(err)
	}
	if err = m.Set(b.ID, true, false); err != nil {
		t.Fatal(err)
	}
	if len(m.Status().Devices) != 2 {
		t.Fatal("both accounts not active")
	}
	if u, err := m.Login(a.ID); err != nil || !strings.Contains(u, "SECRET") {
		t.Fatal("login absent")
	}
	first := m.plan.Records["server.home.ts.net"][0]
	if err = m.Set(a.ID, false, false); err != nil {
		t.Fatal(err)
	}
	if !engines[a.ID].closed || engines[b.ID].closed {
		t.Fatal("wrong identity closed")
	}
	if len(m.Status().Devices) != 1 || m.Status().Devices[0].ProfileID != b.ID {
		t.Fatal("healthy account disrupted")
	}
	if _, err = m.Login(a.ID); err == nil {
		t.Fatal("retained auth URL after disconnect")
	}
	if err = m.Set(a.ID, true, false); err != nil {
		t.Fatal(err)
	}
	if m.plan.Records["server.home.ts.net"][0] != first {
		t.Fatal("reconnect changed lease")
	}
	engines[a.ID].fail = true
	m.mu.Lock()
	err = m.reconcile()
	m.mu.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	if len(m.Status().Devices) != 1 {
		t.Fatal("status failure affected healthy account")
	}
	network.fail = true
	m.mu.Lock()
	err = m.reconcile()
	m.mu.Unlock()
	if err == nil || m.Status().Health == "ready" || len(m.Status().Devices) != 0 {
		t.Fatal("host failure not fail closed")
	}
}
func TestPreviewAndFailedSaveDoNotStartIdentities(t *testing.T) {
	disk := &memoryDisk{s: routing.State{Version: 1}}
	m, err := New(context.Background(), disk, nil, nil, nil, new(names.Resolver))
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()
	a, _ := m.Add("Home")
	if err = m.Set(a.ID, true, false); err == nil {
		t.Fatal("preview enrolled account")
	}
	if m.Status().Accounts[0].Enabled {
		t.Fatal("preview mutated desired state")
	}
	disk.fail = true
	if _, err = m.Add("Work"); err == nil || len(m.Status().Accounts) != 1 {
		t.Fatal("failed save published")
	}
}
func TestSaveFailureWithdrawsDNS(t *testing.T) {
	d := &memoryDisk{s: routing.State{Version: 1, Domains: []string{"home.ts.net"}}}
	r := new(names.Resolver)
	m, err := New(context.Background(), d, nil, nil, nil, r)
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()
	// Introduce a new peer whose lease must become durable before publication.
	a, err := m.Add("Home")
	if err != nil {
		t.Fatal(err)
	}
	m.state.Accounts[0].Enabled = true
	ctx, cancel := context.WithCancel(context.Background())
	_ = ctx
	m.runtimes[a.ID] = &runtime{cancel: cancel, tun: tunmux.NewChanTUN("save-test"), engine: &fakeEngine{st: profile.Status{BackendState: "Running", MagicDNSSuffix: "home.ts.net", SelfIPs: []netip.Addr{netip.MustParseAddr("100.64.0.1")}, Peers: []profile.PeerStatus{{NodeID: "new-peer", DNSName: "x.home.ts.net", TailscaleIPs: []netip.Addr{netip.MustParseAddr("100.64.0.2")}}}}}}
	d.fail = true
	m.mu.Lock()
	err = m.reconcile()
	m.mu.Unlock()
	if err == nil {
		t.Fatal("missing failure")
	}
	q, _ := (&dnsmessage.Message{Questions: []dnsmessage.Question{{Name: dnsmessage.MustNewName("x.home.ts.net."), Type: dnsmessage.TypeA, Class: dnsmessage.ClassINET}}}).Pack()
	var ans dnsmessage.Message
	ans.Unpack(r.Reply(q))
	if ans.RCode != dnsmessage.RCodeNameError {
		t.Fatal("failure escaped local DNS")
	}
}

func TestUnexpectedAccountTUNClosureWithdrawsOnlyItsDNS(t *testing.T) {
	disk := &memoryDisk{s: routing.State{Version: 1}}
	factory := func(a routing.Account, _ *tunmux.ChanTUN) (profile.Engine, error) {
		return &fakeEngine{st: profile.Status{BackendState: "Running", MagicDNSSuffix: strings.ToLower(a.Name) + ".ts.net", SelfIPs: []netip.Addr{netip.MustParseAddr("100.64.0.1")}, Peers: []profile.PeerStatus{{NodeID: "peer", DNSName: "server." + strings.ToLower(a.Name) + ".ts.net", TailscaleIPs: []netip.Addr{netip.MustParseAddr("100.64.0.2")}}}}}, nil
	}
	m, err := New(context.Background(), disk, factory, nil, nil, new(names.Resolver))
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()
	a, _ := m.Add("Home")
	b, _ := m.Add("Work")
	if err = m.Set(a.ID, true, false); err != nil {
		t.Fatal(err)
	}
	if err = m.Set(b.ID, true, false); err != nil {
		t.Fatal(err)
	}
	m.runtimes[a.ID].tun.Close()
	m.mu.Lock()
	err = m.reconcile()
	m.mu.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	s := m.Status()
	if len(s.Devices) != 1 || s.Devices[0].ProfileID != b.ID {
		t.Fatal("dead channel affected other account")
	}
	if s.Accounts[0].State != "unavailable" {
		t.Fatal("dead channel still shown as running")
	}
}

func TestUnchangedReconcileDoesNotRewriteState(t *testing.T) {
	d := &memoryDisk{s: routing.State{Version: 1}}
	m, err := New(context.Background(), d, nil, nil, nil, new(names.Resolver))
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()
	// Writes would fail, but an unchanged plan needs no new durable state.
	d.fail = true
	m.mu.Lock()
	err = m.reconcile()
	m.mu.Unlock()
	if err != nil {
		t.Fatal("unchanged poll attempted a write", err)
	}
}
