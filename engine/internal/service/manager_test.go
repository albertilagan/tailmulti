package service

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"tailmulti.local/engine/internal/daemon"
	"tailmulti.local/engine/internal/setup"
)

type fakeBackend struct{ active bool }

func (b fakeBackend) Status() daemon.Status {
	mode := "preview"
	if b.active {
		mode = "active"
	}
	return daemon.Status{Version: 1, Mode: mode, Health: "ready"}
}
func (b fakeBackend) Add(name string) (daemon.AccountView, error) {
	return daemon.AccountView{Name: name}, nil
}
func (b fakeBackend) Set(string, bool, bool) error { return nil }
func (b fakeBackend) Login(string) (string, error) { return "https://login.tailscale.com/a/test", nil }

func TestServiceStartsInactiveAndRequiresExplicitActivation(t *testing.T) {
	var starts []bool
	closed, restored, checks := 0, 0, 0
	ready := false
	factory := func(_ context.Context, active bool) (Session, error) {
		starts = append(starts, active)
		return Session{Backend: fakeBackend{active}, Close: func() { closed++ }}, nil
	}
	m, err := New(context.Background(), factory, func(context.Context) setup.Report { checks++; return setup.Report{Ready: ready} }, func() error { restored++; return nil })
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()
	if len(starts) != 1 || starts[0] || checks != 0 || m.Status().Mode != "inactive" {
		t.Fatal("startup activated networking")
	}
	if _, err := m.Add("Home"); err != nil {
		t.Fatal(err)
	}
	if m.Set("id", true, false) == nil {
		t.Fatal("inactive service connected an account")
	}
	if _, err := m.Login("id"); err == nil {
		t.Fatal("inactive service returned login")
	}
	if m.StartNetwork(context.Background()) == nil || len(starts) != 1 || closed != 0 {
		t.Fatal("blocked preflight started or replaced a session")
	}
	ready = true
	if err := m.StartNetwork(context.Background()); err != nil {
		t.Fatal(err)
	}
	if m.Status().Mode != "active" || len(starts) != 2 || !starts[1] {
		t.Fatal("explicit activation failed")
	}
	if err := m.StartNetwork(context.Background()); err != nil || len(starts) != 2 {
		t.Fatal("repeated activation was not idempotent")
	}
	if err := m.StopNetwork(false); err != nil {
		t.Fatal(err)
	}
	if m.Status().Mode != "inactive" || closed != 2 || restored != 0 {
		t.Fatal("stop failed or restored DNS without request")
	}
	if err := m.StopNetwork(true); err != nil || restored != 1 {
		t.Fatal("explicit DNS restoration not applied")
	}
	m.Close()
	m.Close()
	if closed != 3 || m.StartNetwork(context.Background()) == nil {
		t.Fatal("closed service restarted or closed twice")
	}
}

func TestFailedActivationKeepsControlUsable(t *testing.T) {
	fail := true
	factory := func(_ context.Context, active bool) (Session, error) {
		if active && fail {
			return Session{}, errors.New("private host detail")
		}
		return Session{Backend: fakeBackend{active}, Close: func() {}}, nil
	}
	m, err := New(context.Background(), factory, func(context.Context) setup.Report { return setup.Report{Ready: true} }, func() error { return nil })
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()
	if err := m.StartNetwork(context.Background()); err == nil || strings.Contains(err.Error(), "private") {
		t.Fatal("unsafe activation failure")
	}
	if m.Status().Mode != "inactive" {
		t.Fatal("failed activation reported active")
	}
	if _, err := m.Add("Still usable"); err != nil {
		t.Fatal("control did not recover", err)
	}
	fail = false
	if err := m.StartNetwork(context.Background()); err != nil {
		t.Fatal("retry failed", err)
	}
	if m.Status().Health != "ready" {
		t.Fatal("stale failure after successful retry")
	}
}

func TestUnexpectedNetworkExitFallsBackToInactive(t *testing.T) {
	done, closed := make(chan error, 1), make(chan struct{}, 1)
	factory := func(_ context.Context, active bool) (Session, error) {
		s := Session{Backend: fakeBackend{active}, Close: func() {}}
		if active {
			s.Done = done
			s.Close = func() { closed <- struct{}{} }
		}
		return s, nil
	}
	m, err := New(context.Background(), factory, func(context.Context) setup.Report { return setup.Report{Ready: true} }, func() error { return nil })
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()
	if err := m.StartNetwork(context.Background()); err != nil {
		t.Fatal(err)
	}
	done <- errors.New("packet worker stopped")
	select {
	case <-closed:
	case <-time.After(time.Second):
		t.Fatal("failed runtime was not closed")
	}
	if s := m.Status(); s.Mode != "inactive" || s.Health == "ready" {
		t.Fatal("unexpected exit was hidden")
	}
	if _, err := m.Add("Recovered"); err != nil {
		t.Fatal("control unavailable after packet failure")
	}
}

func TestConcurrentStartOnlyCreatesOneRuntime(t *testing.T) {
	starts := 0
	factory := func(_ context.Context, active bool) (Session, error) {
		if active {
			starts++
		}
		return Session{Backend: fakeBackend{active}, Close: func() {}}, nil
	}
	m, err := New(context.Background(), factory, func(context.Context) setup.Report { return setup.Report{Ready: true} }, func() error { return nil })
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := m.StartNetwork(context.Background()); err != nil {
				t.Error(err)
			}
			_ = m.Status()
		}()
	}
	wg.Wait()
	if starts != 1 {
		t.Fatal("parallel requests created multiple network runtimes")
	}
}

type healthBackend struct {
	fakeBackend
	health *string
}

func (b healthBackend) Status() daemon.Status {
	s := b.fakeBackend.Status()
	s.Health = *b.health
	return s
}

func TestSetupDoesNotReportReadyWhileRoutingIsPaused(t *testing.T) {
	health := "ready"
	factory := func(_ context.Context, active bool) (Session, error) {
		return Session{Backend: healthBackend{fakeBackend: fakeBackend{active: active}, health: &health}, Close: func() {}}, nil
	}
	m, err := New(context.Background(), factory, func(context.Context) setup.Report { return setup.Report{Ready: true} }, func() error { return nil })
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()
	if err := m.StartNetwork(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !m.Setup(context.Background()).Ready {
		t.Fatal("healthy active service reported blocked")
	}
	health = "blocked: a host route conflicts with Tailmulti; all packet routes disabled"
	r := m.Setup(context.Background())
	if r.Ready || len(r.Checks) != 1 || r.Checks[0].State != "blocked" || r.Checks[0].Detail != m.Status().Health {
		t.Fatal("paused routing was reported ready")
	}
	health = "ready"
	if !m.Setup(context.Background()).Ready {
		t.Fatal("recovery was not reflected in setup")
	}
}
