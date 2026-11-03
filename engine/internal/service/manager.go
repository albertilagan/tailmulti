// Package service keeps authenticated control available while networking stops or fails.
package service

import (
	"context"
	"errors"
	"sync"

	"tailmulti.local/engine/internal/daemon"
	"tailmulti.local/engine/internal/routing"
	"tailmulti.local/engine/internal/setup"
)

type Backend interface {
	Status() daemon.Status
	Add(string) (daemon.AccountView, error)
	Set(string, bool, bool) error
	Login(string) (string, error)
}

type Session struct {
	Backend Backend
	Close   func()
	Done    <-chan error
}

// A failed factory call must release everything it acquired before returning.
type Factory func(context.Context, bool) (Session, error)

type Manager struct {
	mu             sync.Mutex
	ctx            context.Context
	factory        Factory
	inspect        func(context.Context) setup.Report
	restoreDNS     func() error
	session        Session
	active, closed bool
	generation     uint64
	health         string
}

func New(ctx context.Context, factory Factory, inspect func(context.Context) setup.Report, restoreDNS func() error) (*Manager, error) {
	if factory == nil || inspect == nil || restoreDNS == nil {
		return nil, errors.New("service dependencies are required")
	}
	m := &Manager{ctx: ctx, factory: factory, inspect: inspect, restoreDNS: restoreDNS}
	s, err := factory(ctx, false)
	if err != nil {
		return nil, errors.New("service could not load account state")
	}
	if s.Backend == nil || s.Close == nil {
		if s.Close != nil {
			s.Close()
		}
		return nil, errors.New("invalid service session")
	}
	m.session = s
	return m, nil
}

func (m *Manager) Status() daemon.Status {
	m.mu.Lock()
	defer m.mu.Unlock()
	s := daemon.Status{Version: 1, Mode: "inactive", Health: "ready", Accounts: []daemon.AccountView{}, Devices: []routing.Device{}, Conflicts: []string{}}
	if m.session.Backend != nil {
		s = m.session.Backend.Status()
	}
	if !m.active {
		s.Mode = "inactive"
	}
	if m.health != "" {
		s.Health = m.health
	}
	return s
}

func (m *Manager) Setup(ctx context.Context) setup.Report {
	m.mu.Lock()
	active := m.active
	health := m.health
	if active && health == "" && m.session.Backend != nil {
		health = m.session.Backend.Status().Health
	}
	m.mu.Unlock()
	if active {
		if health != "ready" {
			return setup.Report{Version: 1, Ready: false, Checks: []setup.Check{{ID: "network", State: "blocked", Title: "Tailmulti routing is paused", Detail: health}}}
		}
		return setup.Report{Version: 1, Ready: true, Checks: []setup.Check{{ID: "network", State: "pass", Title: "Tailmulti networking is active", Detail: "Stop networking before changing its installation."}}}
	}
	return m.inspect(ctx)
}

func (m *Manager) StartNetwork(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		return errors.New("service is stopped")
	}
	if m.active {
		return nil
	}
	if !m.inspect(ctx).Ready {
		return errors.New("host setup checks did not pass; resolve them before enabling networking")
	}
	m.closeCurrent()
	s, err := m.factory(m.ctx, true)
	if err != nil || s.Backend == nil || s.Close == nil {
		if s.Close != nil {
			s.Close()
		}
		m.makeInactive("Networking could not start. Run the setup checks and retry.")
		return errors.New(m.health)
	}
	m.session, m.active, m.health = s, true, ""
	m.watch(m.generation, s.Done)
	return nil
}

func (m *Manager) StopNetwork(restoreDNS bool) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		return errors.New("service is stopped")
	}
	if m.active {
		m.closeCurrent()
		m.makeInactive("")
	}
	if restoreDNS {
		if err := m.restoreDNS(); err != nil {
			m.health = "Networking stopped, but owned DNS entries could not all be restored."
			return errors.New(m.health)
		}
	}
	if m.session.Backend == nil {
		return errors.New("service could not reload account state")
	}
	m.health = ""
	return nil
}

func (m *Manager) Add(name string) (daemon.AccountView, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed || m.session.Backend == nil {
		return daemon.AccountView{}, errors.New("account state is unavailable")
	}
	return m.session.Backend.Add(name)
}

func (m *Manager) Set(id string, enabled, remove bool) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed || m.session.Backend == nil {
		return errors.New("account state is unavailable")
	}
	if enabled && !m.active {
		return errors.New("enable Tailmulti networking before connecting an account")
	}
	return m.session.Backend.Set(id, enabled, remove)
}

func (m *Manager) Login(id string) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed || !m.active || m.session.Backend == nil {
		return "", errors.New("enable Tailmulti networking before signing in")
	}
	return m.session.Backend.Login(id)
}

func (m *Manager) Close() {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		return
	}
	m.closed = true
	m.closeCurrent()
}

func (m *Manager) closeCurrent() {
	m.generation++
	if m.session.Close != nil {
		m.session.Close()
	}
	m.session, m.active = Session{}, false
}

func (m *Manager) makeInactive(health string) {
	s, err := m.factory(m.ctx, false)
	if err != nil || s.Backend == nil || s.Close == nil {
		if s.Close != nil {
			s.Close()
		}
		m.health = "Networking stopped, but account state could not be reloaded."
		return
	}
	m.session, m.active, m.health = s, false, health
}

func (m *Manager) watch(generation uint64, done <-chan error) {
	if done == nil {
		return
	}
	go func() {
		select {
		case <-m.ctx.Done():
			return
		case <-done:
		}
		m.mu.Lock()
		defer m.mu.Unlock()
		if m.closed || !m.active || m.generation != generation {
			return
		}
		m.closeCurrent()
		m.makeInactive("Networking stopped unexpectedly. Check setup before starting it again.")
	}()
}
