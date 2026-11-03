// Package daemon serializes identity lifecycle and fail-closed plan publication.
package daemon

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"net/netip"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"time"

	"tailmulti.local/engine/internal/names"
	"tailmulti.local/engine/internal/routing"
	"tailmulti.local/engine/internal/setup"
	"tailmulti.local/engine/internal/store"
	"tailmulti.local/engine/packetmap"
	"tailmulti.local/engine/profile"
	"tailmulti.local/engine/tunmux"
	"tailscale.com/types/logger"
)

type Disk interface {
	Load() (routing.State, error)
	Save(routing.State) error
}
type Network interface{ Apply(*routing.Plan) error }
type Factory func(routing.Account, *tunmux.ChanTUN) (profile.Engine, error)
type AccountView struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Enabled   bool   `json:"enabled"`
	State     string `json:"state"`
	PeerCount int    `json:"peerCount"`
	// UI hints only. An assigned identity address is retained across disconnects;
	// the backend state still decides whether authentication is required again.
	HasIdentity bool `json:"hasIdentity"`
	LoginReady  bool `json:"loginReady"`
}
type Status struct {
	Version   int              `json:"version"`
	Mode      string           `json:"mode"`
	Accounts  []AccountView    `json:"accounts"`
	Devices   []routing.Device `json:"devices"`
	Conflicts []string         `json:"conflicts"`
	Health    string           `json:"health"`
}
type runtime struct {
	engine  profile.Engine
	tun     *tunmux.ChanTUN
	cancel  context.CancelFunc
	state   string
	authURL string
}
type Manager struct {
	mu       sync.Mutex
	ctx      context.Context
	disk     Disk
	state    routing.State
	factory  Factory
	network  Network
	mux      *tunmux.Mux
	dns      *names.Resolver
	runtimes map[string]*runtime
	plan     *routing.Plan
	mode     string
	health   string
	updates  chan struct{}
	closed   bool
}

func New(ctx context.Context, disk Disk, factory Factory, network Network, mux *tunmux.Mux, dns *names.Resolver) (*Manager, error) {
	s, err := disk.Load()
	if err != nil {
		return nil, err
	}
	mode := "preview"
	if factory != nil {
		mode = "active"
	}
	m := &Manager{ctx: ctx, disk: disk, state: s, factory: factory, network: network, mux: mux, dns: dns, runtimes: map[string]*runtime{}, mode: mode, health: "ready", updates: make(chan struct{}, 1)}
	if err = m.reconcile(); err != nil {
		return nil, err
	}
	return m, nil
}
func RealFactory(ctx context.Context, dir string) Factory {
	return func(a routing.Account, t *tunmux.ChanTUN) (profile.Engine, error) {
		path := filepath.Join(dir, "profiles", a.ID)
		if err := os.MkdirAll(path, 0700); err != nil {
			return nil, err
		}
		fi, err := os.Lstat(path)
		if err != nil || !fi.IsDir() || fi.Mode().Perm() != 0700 {
			return nil, errors.New("invalid profile directory")
		}
		e := profile.NewTSNetEngine(profile.TSNetConfig{ProfileID: a.ID, Alias: a.Name, Dir: path, Hostname: "tailmulti-" + a.ID[:8], Tun: t, Logf: logger.Discard, UserLogf: logger.Discard})
		if err := e.Start(ctx); err != nil {
			return nil, errors.New("profile engine failed to start")
		}
		return e, nil
	}
}
func (m *Manager) Run() {
	// Events drive normal reconciliation. Polling also detects missed auth/expiry changes.
	ticker := time.NewTicker(3 * time.Second)
	defer ticker.Stop()
	m.mu.Lock()
	if m.closed || m.ctx.Err() != nil {
		m.mu.Unlock()
		return
	}
	for _, a := range m.state.Accounts {
		if a.Enabled && !a.Removed && m.factory != nil {
			_ = m.start(a)
		}
	}
	_ = m.reconcile()
	m.mu.Unlock()
	for {
		select {
		case <-m.ctx.Done():
			return
		case <-ticker.C:
		case <-m.updates:
		}
		m.mu.Lock()
		if !m.closed {
			_ = m.reconcile()
		}
		m.mu.Unlock()
	}
}
func (m *Manager) start(a routing.Account) error {
	if _, ok := m.runtimes[a.ID]; ok {
		return nil
	}
	t := tunmux.NewChanTUN("tailmulti-" + a.ID[:8])
	e, err := m.factory(a, t)
	if err != nil {
		t.Close()
		return err
	}
	if m.mux != nil {
		if err = m.mux.AddProfile(a.ID, t); err != nil {
			e.Close()
			t.Close()
			return err
		}
	}
	ctx, cancel := context.WithCancel(m.ctx)
	rt := &runtime{engine: e, tun: t, cancel: cancel, state: "starting"}
	m.runtimes[a.ID] = rt
	go func() {
		err := e.WatchUpdates(ctx, func() {
			select {
			case m.updates <- struct{}{}:
			default:
			}
		})
		if err != nil && ctx.Err() == nil {
			select {
			case m.updates <- struct{}{}:
			default:
			}
		}
	}()
	return nil
}
func (m *Manager) stop(id string) {
	rt := m.runtimes[id]
	if rt == nil {
		return
	}
	rt.cancel()
	if m.mux != nil {
		m.mux.RemoveProfile(id)
	}
	rt.tun.Close()
	rt.engine.Close()
	delete(m.runtimes, id)
}
func (m *Manager) reconcile() error {
	candidate := store.Clone(m.state)
	var snapshots []profile.Status
	for id, rt := range m.runtimes {
		if rt.tun.IsClosed() {
			rt.state = "unavailable"
			rt.authURL = ""
			continue
		}
		ctx, cancel := context.WithTimeout(m.ctx, 2*time.Second)
		st, err := rt.engine.Status(ctx)
		cancel()
		rt.authURL = ""
		if err != nil {
			rt.state = "unavailable"
			continue
		}
		st.ProfileID = id
		rt.state = st.BackendState
		rt.authURL = st.AuthURL
		snapshots = append(snapshots, st)
	}
	p, err := routing.Build(&candidate, snapshots)
	// Netmap polling verifies host state but must not rewrite identity metadata
	// every three seconds when the durable plan is unchanged.
	if err == nil && !reflect.DeepEqual(candidate, m.state) {
		err = m.disk.Save(candidate)
	}
	if err != nil {
		m.failClosed("state or routing plan failed")
		return errors.New("state or routing plan failed")
	}
	m.state = candidate
	// Publishing removed mappings first prevents a stale host route selecting another profile.
	if m.mux != nil {
		m.mux.SetMapper(p.Mapper)
	}
	if m.network != nil {
		if err = m.network.Apply(p); err != nil {
			m.failClosed(setup.FailureReason(err))
			return errors.New("host network configuration failed")
		}
	}
	m.dns.Set(p)
	m.plan = p
	m.health = "ready"
	return nil
}
func (m *Manager) failClosed(reason string) {
	if m.mux != nil {
		m.mux.SetMapper(packetmap.New(packetmap.Table{}))
	}
	p := &routing.Plan{Records: map[string][]netip.Addr{}, Domains: m.state.Domains, Aliases: m.state.Aliases, Devices: []routing.Device{}, Conflicts: []string{}}
	m.dns.Set(p)
	m.plan = p
	m.health = "blocked: " + reason + "; all packet routes disabled"
}
func (m *Manager) Add(name string) (AccountView, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	name = strings.TrimSpace(name)
	if len(name) == 0 || len(name) > 40 {
		return AccountView{}, errors.New("name must contain 1 to 40 characters")
	}
	for _, c := range name {
		if c < 32 || c == 127 {
			return AccountView{}, errors.New("name contains control characters")
		}
	}
	for _, a := range m.state.Accounts {
		if !a.Removed && strings.EqualFold(a.Name, name) {
			return AccountView{}, errors.New("account name already exists")
		}
	}
	if len(m.state.Accounts) >= 64 {
		return AccountView{}, errors.New("account capacity reached")
	}
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return AccountView{}, errors.New("cannot generate identity")
	}
	a := routing.Account{ID: hex.EncodeToString(b[:]), Name: name}
	candidate := store.Clone(m.state)
	candidate.Accounts = append(candidate.Accounts, a)
	if err := m.disk.Save(candidate); err != nil {
		return AccountView{}, errors.New("cannot save account")
	}
	m.state = candidate
	return AccountView{ID: a.ID, Name: a.Name, State: "disconnected"}, nil
}
func (m *Manager) Set(id string, enabled, remove bool) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	i := -1
	for n, a := range m.state.Accounts {
		if a.ID == id && !a.Removed {
			i = n
			break
		}
	}
	if i < 0 {
		return errors.New("account not found")
	}
	if enabled && m.factory == nil {
		return errors.New("preview mode cannot enroll or connect accounts; privileged activation is required")
	}
	candidate := store.Clone(m.state)
	candidate.Accounts[i].Enabled = enabled
	candidate.Accounts[i].Removed = remove
	if err := m.disk.Save(candidate); err != nil {
		return errors.New("cannot save account change")
	}
	m.state = candidate
	if !enabled {
		err := m.reconcile()
		m.stop(id)
		return err
	}
	if err := m.start(m.state.Accounts[i]); err != nil {
		_ = m.reconcile()
		return errors.New("account engine failed to start")
	}
	return m.reconcile()
}
func (m *Manager) Login(id string) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	rt := m.runtimes[id]
	if rt == nil || rt.authURL == "" {
		return "", errors.New("browser sign-in is not ready yet; try again")
	}
	return rt.authURL, nil
}
func (m *Manager) Status() Status {
	m.mu.Lock()
	defer m.mu.Unlock()
	s := Status{Version: 1, Mode: m.mode, Health: m.health, Accounts: []AccountView{}, Devices: []routing.Device{}, Conflicts: []string{}}
	if m.plan != nil {
		s.Devices = append(s.Devices, m.plan.Devices...)
		s.Conflicts = append(s.Conflicts, m.plan.Conflicts...)
	}
	for _, a := range m.state.Accounts {
		if a.Removed {
			continue
		}
		v := AccountView{ID: a.ID, Name: a.Name, Enabled: a.Enabled, State: "disconnected", HasIdentity: len(m.state.IdentityIPs[a.ID]) > 0}
		if a.Enabled {
			v.State = "unavailable"
		}
		if rt := m.runtimes[a.ID]; rt != nil {
			v.State = rt.state
			v.LoginReady = rt.authURL != ""
			v.HasIdentity = v.HasIdentity || rt.state == "Running" || rt.state == "NeedsMachineAuth"
		}
		if m.mode == "preview" && a.Enabled {
			v.State = "activation required"
		}
		for _, d := range s.Devices {
			if d.ProfileID == a.ID {
				v.PeerCount++
			}
		}
		s.Accounts = append(s.Accounts, v)
	}
	return s
}
func (m *Manager) Close() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.closed = true
	m.failClosed("engine stopped")
	for id := range m.runtimes {
		m.stop(id)
	}
}
