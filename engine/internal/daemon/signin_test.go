package daemon

import (
	"context"
	"encoding/json"
	"net/netip"
	"strings"
	"testing"

	"tailmulti.local/engine/internal/names"
	"tailmulti.local/engine/internal/routing"
	"tailmulti.local/engine/profile"
	"tailmulti.local/engine/tunmux"
)

func TestSignInHintsPreserveIdentityWithoutExposingURL(t *testing.T) {
	disk := &memoryDisk{s: routing.State{Version: 1}}
	engine := &fakeEngine{st: profile.Status{BackendState: "NeedsLogin", AuthURL: "https://login.tailscale.com/a/test-only"}}
	factory := func(routing.Account, *tunmux.ChanTUN) (profile.Engine, error) { return engine, nil }
	m, err := New(context.Background(), disk, factory, nil, nil, new(names.Resolver))
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()
	a, err := m.Add("Home")
	if err != nil {
		t.Fatal(err)
	}
	if a.HasIdentity || a.LoginReady || a.Enabled {
		t.Fatal("new account claimed an identity or pending login")
	}
	if err := m.Set(a.ID, true, false); err != nil {
		t.Fatal(err)
	}
	st := m.Status()
	if st.Accounts[0].HasIdentity || !st.Accounts[0].LoginReady {
		t.Fatal("pending sign-in hints are incorrect")
	}
	raw, err := json.Marshal(st)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "https://") || strings.Contains(string(raw), "test-only") {
		t.Fatal("status exposed the login URL")
	}
	engine.st = profile.Status{BackendState: "Running", SelfIPs: []netip.Addr{netip.MustParseAddr("100.64.0.8")}}
	if err := m.reconcile(); err != nil {
		t.Fatal(err)
	}
	if !m.Status().Accounts[0].HasIdentity || m.Status().Accounts[0].LoginReady {
		t.Fatal("connected identity was not recognized")
	}
	if err := m.Set(a.ID, false, false); err != nil {
		t.Fatal(err)
	}
	if !m.Status().Accounts[0].HasIdentity || m.Status().Accounts[0].LoginReady {
		t.Fatal("disconnect forgot the saved identity")
	}
	m.Close()
	restarted, err := New(context.Background(), disk, factory, nil, nil, new(names.Resolver))
	if err != nil {
		t.Fatal(err)
	}
	defer restarted.Close()
	if !restarted.Status().Accounts[0].HasIdentity {
		t.Fatal("restart forgot the saved identity")
	}
	other, err := restarted.Add("Work")
	if err != nil {
		t.Fatal(err)
	}
	if other.HasIdentity {
		t.Fatal("new account inherited another account's identity")
	}
}
