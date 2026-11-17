package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/netip"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"tailmulti.local/engine/internal/control"
	"tailmulti.local/engine/internal/daemon"
	"tailmulti.local/engine/internal/routing"
	"tailmulti.local/engine/internal/setup"
)

const (
	homeID  = "aaaa1111aaaa1111aaaa1111aaaa1111"
	workID  = "aaaa2222aaaa2222aaaa2222aaaa2222"
	authURL = "https://login.tailscale.com/a/synthetic"
)

// fake is a scripted service behind the real control server.
type fake struct {
	mu         sync.Mutex
	status     daemon.Status
	ready      bool
	restoreDNS *bool
	calls      []string
	// loginAfter is how many status reads pass before the sign-in page is ready.
	loginAfter int
}

func (f *fake) Status() daemon.Status {
	f.mu.Lock()
	defer f.mu.Unlock()
	s := f.status
	s.Accounts = append([]daemon.AccountView{}, f.status.Accounts...)
	for i := range f.status.Accounts {
		if a := &f.status.Accounts[i]; a.Enabled && a.State == "NeedsLogin" && !a.LoginReady {
			if f.loginAfter--; f.loginAfter <= 0 {
				a.LoginReady = true
			}
		}
	}
	return s
}
func (f *fake) Add(name string) (daemon.AccountView, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	a := daemon.AccountView{ID: "bbbb3333bbbb3333bbbb3333bbbb3333", Name: name, State: "disconnected"}
	f.status.Accounts = append(f.status.Accounts, a)
	return a, nil
}
func (f *fake) Set(id string, enabled, remove bool) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	for i := range f.status.Accounts {
		if a := &f.status.Accounts[i]; a.ID == id {
			if f.status.Mode != "active" && enabled {
				return errors.New("activation required")
			}
			a.Enabled = enabled
			if enabled {
				a.State = "NeedsLogin"
			}
			if remove {
				f.status.Accounts = append(f.status.Accounts[:i], f.status.Accounts[i+1:]...)
			}
			f.calls = append(f.calls, map[bool]string{true: "connect ", false: "disconnect "}[enabled]+id)
			return nil
		}
	}
	return errors.New("account not found")
}
func (f *fake) Login(id string) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, a := range f.status.Accounts {
		if a.ID == id && a.LoginReady {
			return authURL, nil
		}
	}
	return "", errors.New("browser sign-in is not ready yet; try again")
}
func (f *fake) Setup(context.Context) setup.Report {
	state := "pass"
	if !f.ready {
		state = "blocked"
	}
	return setup.Report{Version: 1, Ready: f.ready, Checks: []setup.Check{{ID: "vpn", State: state, Title: "Another VPN owns the routes", Detail: "Disconnect it first."}}}
}
func (f *fake) StartNetwork(context.Context) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.status.Mode = "active"
	return nil
}
func (f *fake) StopNetwork(restoreDNS bool) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.status.Mode, f.restoreDNS = "inactive", &restoreDNS
	return nil
}
func (f *fake) Build() string { return "17" }
func (f *fake) Restart()      {}

func newFake() *fake {
	return &fake{ready: true, status: daemon.Status{Version: 1, Mode: "active", Health: "ready", Conflicts: []string{},
		Accounts: []daemon.AccountView{
			{ID: homeID, Name: "Home", Enabled: true, State: "Running", PeerCount: 2, HasIdentity: true},
			{ID: workID, Name: "Work", State: "disconnected"},
		},
		Devices: []routing.Device{
			{ProfileID: homeID, Name: "nas.home.example", Addresses: []netip.Addr{netip.MustParseAddr("198.18.0.2")}, Online: true},
			{ProfileID: homeID, Name: "laptop.home.example", Addresses: []netip.Addr{netip.MustParseAddr("198.18.0.3")}},
		}}}
}

type harness struct {
	t      *testing.T
	socket string
	opened []string
}

func serve(t *testing.T, b control.Backend) *harness {
	t.Helper()
	dir, err := os.MkdirTemp("", "tm-cli-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	socket := filepath.Join(dir, "s")
	s, err := control.Listen(socket, os.Getuid(), b)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	return &harness{t: t, socket: socket}
}

func (h *harness) run(args ...string) (int, string, string) {
	var stdout, stderr bytes.Buffer
	e := env{stdout: &stdout, stderr: &stderr, uid: os.Getuid(), sleep: func(time.Duration) {}, signInTimeout: 2 * time.Second,
		open: func(u string) error { h.opened = append(h.opened, u); return nil }}
	code := run(context.Background(), append([]string{"--socket", h.socket}, args...), e)
	return code, stdout.String(), stderr.String()
}

func (h *harness) ok(args ...string) string {
	h.t.Helper()
	code, stdout, stderr := h.run(args...)
	if code != 0 {
		h.t.Fatalf("%v: exit %d: %s", args, code, stderr)
	}
	return stdout
}

func TestStatusAndDevices(t *testing.T) {
	h := serve(t, newFake())
	out := h.ok("status")
	for _, want := range []string{"Tailmulti is on.", "Home", "Connected", "aaaa1111", "Work", "Sign-in required"} {
		if !strings.Contains(out, want) {
			t.Fatalf("status lacks %q:\n%s", want, out)
		}
	}
	var decoded struct {
		Accounts []struct{ ID string }
	}
	if err := json.Unmarshal([]byte(h.ok("status", "--json")), &decoded); err != nil || len(decoded.Accounts) != 2 || decoded.Accounts[0].ID != homeID {
		t.Fatalf("status --json: %v %+v", err, decoded)
	}
	out = h.ok("devices", "home", "--online")
	if !strings.Contains(out, "nas.home.example") || !strings.Contains(out, "198.18.0.2") || strings.Contains(out, "laptop") {
		t.Fatalf("devices:\n%s", out)
	}
	// Flags after "--" are positional: this is two arguments, not the --online filter.
	if code, _, _ := h.run("devices", "--", "home", "--online"); code != 2 {
		t.Fatalf("flag after -- was parsed: exit %d", code)
	}
	if out = h.ok("devices", "--online", "--", "home"); strings.Contains(out, "laptop") {
		t.Fatalf("devices with --:\n%s", out)
	}
	if out = h.ok("devices", "Work"); !strings.Contains(out, "No devices.") {
		t.Fatalf("devices for an empty account:\n%s", out)
	}
}

func TestUpStopsAtFailedHostCheckAndDownRestoresDNS(t *testing.T) {
	f := newFake()
	f.status.Mode, f.ready = "inactive", false
	h := serve(t, f)
	code, _, stderr := h.run("up")
	if code != 1 || !strings.Contains(stderr, "Another VPN owns the routes") || f.status.Mode != "inactive" {
		t.Fatalf("up with a failed check: %d %s mode %s", code, stderr, f.status.Mode)
	}
	f.ready = true
	if out := h.ok("up"); !strings.Contains(out, "Tailmulti is on.") {
		t.Fatal(out)
	}
	h.ok("down")
	if f.restoreDNS == nil || !*f.restoreDNS {
		t.Fatal("down must restore DNS by default")
	}
	h.ok("up")
	h.ok("down", "--keep-dns")
	if *f.restoreDNS {
		t.Fatal("--keep-dns must leave resolver files")
	}
}

func TestLoginStartsAccountAndOpensBrowserWithoutPrintingURL(t *testing.T) {
	f := newFake()
	f.loginAfter = 3
	h := serve(t, f)
	out := h.ok("login", "work")
	if len(h.opened) != 1 || h.opened[0] != authURL {
		t.Fatalf("browser: %v", h.opened)
	}
	if strings.Contains(out, "login.tailscale.com") {
		t.Fatal("sign-in URL printed without --print-url")
	}
	if len(f.calls) != 1 || f.calls[0] != "connect "+workID {
		t.Fatalf("calls: %v", f.calls)
	}
	if out = h.ok("login", "--print-url", "aaaa2222"); strings.TrimSpace(out) != authURL || len(h.opened) != 1 {
		t.Fatalf("--print-url: %q opened %v", out, h.opened)
	}
	if out = h.ok("login", "Home"); !strings.Contains(out, "already signed in") || len(h.opened) != 1 {
		t.Fatalf("connected account: %q", out)
	}
}

func TestLoginRequiresNetworkOn(t *testing.T) {
	f := newFake()
	f.status.Mode = "inactive"
	h := serve(t, f)
	code, _, stderr := h.run("login", "Work")
	if code != 1 || !strings.Contains(stderr, "tailmulti up") || len(f.calls) != 0 || len(h.opened) != 0 {
		t.Fatalf("%d %s %v", code, stderr, f.calls)
	}
}

func TestAccountLifecycleAndResolution(t *testing.T) {
	f := newFake()
	h := serve(t, f)
	if out := h.ok("account", "add", "Side Project"); !strings.Contains(out, "tailmulti login 'Side Project'") {
		t.Fatal(out)
	}
	h.ok("disconnect", "HOME")
	if f.calls[len(f.calls)-1] != "disconnect "+homeID {
		t.Fatalf("calls: %v", f.calls)
	}
	h.ok("account", "remove", "side project")
	if out := h.ok("account", "list"); strings.Contains(out, "Side Project") {
		t.Fatal(out)
	}
	for ref, want := range map[string]string{"aaaa": "more than one", "nobody": "no account named", "aa": "no account named"} {
		if code, _, stderr := h.run("connect", ref); code != 1 || !strings.Contains(stderr, want) {
			t.Fatalf("%s: %d %s", ref, code, stderr)
		}
	}
}

func TestDoctorVersionAndUsage(t *testing.T) {
	f := newFake()
	h := serve(t, f)
	if out := h.ok("doctor"); !strings.Contains(out, "ok    Another VPN") || !strings.Contains(out, "Service build 17") {
		t.Fatal(out)
	}
	f.ready = false
	if code, out, _ := h.run("doctor"); code != 1 || !strings.Contains(out, "FAIL  Another VPN") {
		t.Fatalf("%d %s", code, out)
	}
	if out := h.ok("version"); !strings.Contains(out, "tailmulti dev (build dev)") || !strings.Contains(out, "service build 17") {
		t.Fatal(out)
	}
	for _, args := range [][]string{{"bogus"}, {"status", "extra"}, {"down", "--nope"}, {"connect"}} {
		if code, _, stderr := h.run(args...); code != 2 || !strings.Contains(stderr, "Usage:") {
			t.Fatalf("%v: %d %s", args, code, stderr)
		}
	}
}

func TestServiceNotRunningAndWrongServerIdentity(t *testing.T) {
	var stdout, stderr bytes.Buffer
	e := env{stdout: &stdout, stderr: &stderr, uid: os.Getuid()}
	// t.TempDir is too long for a Unix socket path on macOS.
	dir, err := os.MkdirTemp("", "tm-cli-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)
	missing := filepath.Join(dir, "s")
	if code := run(context.Background(), []string{"--socket", missing, "status"}, e); code != 1 || !strings.Contains(stderr.String(), "isn't running") {
		t.Fatalf("%d %s", code, stderr.String())
	}
	stderr.Reset()
	if code := run(context.Background(), []string{"--socket", missing, "version"}, e); code != 0 || !strings.Contains(stdout.String(), "service not running") {
		t.Fatalf("%d %s", code, stdout.String())
	}
	// A server owned by anyone other than the expected user must be refused before any request.
	h := serve(t, newFake())
	stderr.Reset()
	e.uid = os.Getuid() + 1
	if code := run(context.Background(), []string{"--socket", h.socket, "status"}, e); code != 1 || !strings.Contains(stderr.String(), "identity does not match") {
		t.Fatalf("%d %s", code, stderr.String())
	}
}
