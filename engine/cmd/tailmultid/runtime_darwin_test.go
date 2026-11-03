//go:build darwin

package main

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"tailmulti.local/engine/internal/service"
	"tailmulti.local/engine/internal/setup"
	"tailmulti.local/engine/internal/store"
)

func TestNativeServiceOwnerMustMatchSignedBuild(t *testing.T) {
	if err := validateBundledOwner(501, "501"); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		owner    int
		embedded string
	}{
		{502, "501"}, {0, "0"}, {-1, "501"}, {501, ""},
		{501, "invalid"}, {501, "0"}, {501, "4294967295"},
	} {
		if validateBundledOwner(tc.owner, tc.embedded) == nil {
			t.Fatal("invalid or reassigned build owner accepted")
		}
	}
}

func TestInactiveServicePreservesAccountsAcrossFailedActivation(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "state")
	disk, err := store.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer disk.Close()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	factory := func(ctx context.Context, active bool) (service.Session, error) {
		if active {
			return service.Session{}, errors.New("simulated activation failure; no host adapter opened")
		}
		return newSession(ctx, disk, dir, false)
	}
	inspect := func(context.Context) setup.Report { return setup.Report{Version: 1, Ready: true} }
	restore := func() error { t.Fatal("unexpected DNS restoration"); return nil }
	m, err := service.New(ctx, factory, inspect, restore)
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()
	a, err := m.Add("Home")
	if err != nil {
		t.Fatal(err)
	}
	if m.Set(a.ID, true, false) == nil {
		t.Fatal("inactive service connected an identity")
	}
	if _, err = m.Login(a.ID); err == nil {
		t.Fatal("inactive service exposed a login")
	}
	if m.StartNetwork(ctx) == nil {
		t.Fatal("activation failure was hidden")
	}
	st := m.Status()
	if st.Mode != "inactive" || st.Health == "ready" || len(st.Accounts) != 1 || st.Accounts[0].ID != a.ID {
		t.Fatal("failed activation lost inactive state")
	}
	m.Close()
	next, err := service.New(ctx, factory, inspect, restore)
	if err != nil {
		t.Fatal(err)
	}
	defer next.Close()
	st = next.Status()
	if st.Mode != "inactive" || len(st.Accounts) != 1 || st.Accounts[0].ID != a.ID || st.Accounts[0].Enabled {
		t.Fatal("service restart changed the saved account")
	}
}
