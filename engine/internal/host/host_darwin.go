//go:build darwin

// Package host is the only Tailmulti package allowed to mutate host networking.
// Open requires explicit activation after host checks and owner authorization.
package host

import (
	"errors"
	"fmt"
	"net"
	"net/netip"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"

	"golang.org/x/sys/unix"
	"tailmulti.local/engine/hosttun"
	"tailmulti.local/engine/internal/routing"
	"tailmulti.local/engine/internal/setup"
	"tailmulti.local/engine/internal/store"
	"tailscale.com/net/routetable"
)

const Header = "# Managed by Tailmulti v1. Retained on stop to prevent DNS fallback.\n"
const ResolverDir = "/private/etc/resolver"
const InstallRoot = "/Library/Application Support/Tailmulti"
const StateDir = InstallRoot + "/state"
const ControlSocket = InstallRoot + "/run/control.sock"
const OwnerPolicy = InstallRoot + "/installation.json"

type Host struct {
	hosttun.Host
	port string
	dir  string
	dns  *nativeDNS
}

func SecureParents(path string, owner uint32) error {
	for p := filepath.Clean(path); ; p = filepath.Dir(p) {
		fi, err := os.Lstat(p)
		if err != nil {
			return err
		}
		st, ok := fi.Sys().(*syscall.Stat_t)
		if !ok || !fi.IsDir() || st.Uid != owner || fi.Mode().Perm()&0022 != 0 {
			return errors.New("unsafe directory ownership or permissions")
		}
		if p == "/" {
			return nil
		}
	}
}
func guard(r routetable.RouteEntry) bool {
	sys, ok := r.Sys.(routetable.RouteEntryBSD)
	return ok && r.Interface == "lo0" && sys.RawFlags&unix.RTF_REJECT != 0 && sys.RawFlags&unix.RTF_PROTO1 != 0 && (r.Dst.Prefix == routing.Pool4 || r.Dst.Prefix == routing.Pool6)
}

func routePrefix(r routetable.RouteEntry) netip.Prefix {
	// The pinned BSD route reader reports IPv6 host routes as /32. The kernel's
	// HOST flag establishes the actual /128; do not treat that entry as a subnet.
	if sys, ok := r.Sys.(routetable.RouteEntryBSD); ok && sys.RawFlags&unix.RTF_HOST != 0 && r.Dst.IsValid() {
		return netip.PrefixFrom(r.Dst.Addr(), r.Dst.Addr().BitLen())
	}
	return r.Dst.Prefix
}
func CheckRoutes(routes []routetable.RouteEntry, own string) error {
	owned := map[netip.Addr]bool{}
	if own != "" {
		iface, err := net.InterfaceByName(own)
		if err != nil {
			return errors.New("cannot inspect Tailmulti interface ownership")
		}
		addrs, err := iface.Addrs()
		if err != nil {
			return errors.New("cannot inspect Tailmulti interface addresses")
		}
		for _, addr := range addrs {
			prefix, err := netip.ParsePrefix(addr.String())
			if err != nil {
				return errors.New("cannot interpret Tailmulti interface addresses")
			}
			if a := prefix.Addr(); a == routing.Host4 || a == routing.Host6 {
				owned[a] = true
			}
		}
	}
	return checkRoutes(routes, own, owned)
}

func checkRoutes(routes []routetable.RouteEntry, own string, owned map[netip.Addr]bool) error {
	for _, r := range routes {
		prefix := routePrefix(r)
		if (own != "" && (r.Interface == own || ownedLocalRoute(r, owned))) || guard(r) || !prefix.IsValid() || prefix.Bits() == 0 {
			continue
		}
		// Reject competing VPN split defaults as well as pool collisions. No VPN replacement.
		if prefix.Bits() == 1 && strings.HasPrefix(r.Interface, "utun") {
			return errors.New("competing VPN split default is active")
		}
		if prefix.Overlaps(routing.Pool4) || prefix.Overlaps(routing.Pool6) {
			return errors.New("existing route overlaps a Tailmulti address pool")
		}
	}
	return nil
}

func ownedLocalRoute(r routetable.RouteEntry, owned map[netip.Addr]bool) bool {
	// Darwin installs an IPv6 host route on lo0 for an address assigned to utun.
	// Accept only our reserved local addresses, proven present on the active utun.
	prefix := routePrefix(r)
	a := prefix.Addr()
	if r.Interface != "lo0" || !prefix.IsValid() || prefix.Bits() != a.BitLen() || !owned[a] || (a != routing.Host4 && a != routing.Host6) {
		return false
	}
	sys, ok := r.Sys.(routetable.RouteEntryBSD)
	return ok && sys.RawFlags&(unix.RTF_UP|unix.RTF_HOST|unix.RTF_LOCAL) == unix.RTF_UP|unix.RTF_HOST|unix.RTF_LOCAL && sys.RawFlags&(unix.RTF_GATEWAY|unix.RTF_REJECT|unix.RTF_BLACKHOLE) == 0
}
func guardArgs(action string, p netip.Prefix) []string {
	family, gateway := "-inet", "127.0.0.1"
	if p.Addr().Is6() {
		family, gateway = "-inet6", "::1"
	}
	return []string{"-n", action, family, "-net", p.String(), gateway, "-reject", "-proto1"}
}
func Open(port string) (*Host, error) {
	if os.Geteuid() != 0 {
		return nil, errors.New("activation requires root")
	}
	routes, err := routetable.Get(65536)
	if err != nil {
		return nil, err
	}
	if err = CheckRoutes(routes, ""); err != nil {
		return nil, err
	}
	if err = prepareResolverDir(ResolverDir); err != nil {
		return nil, err
	}
	if err = SecureParents(ResolverDir, 0); err != nil {
		return nil, err
	}
	// Broader reject routes outlive utun; more specific peer routes override them.
	// Keep any successful guard after partial failure. Recovery never deletes blindly.
	for _, p := range []netip.Prefix{routing.Pool4, routing.Pool6} {
		found := false
		for _, r := range routes {
			if guard(r) && r.Dst.Prefix == p {
				found = true
			}
		}
		if !found {
			if err = exec.Command("/sbin/route", guardArgs("add", p)...).Run(); err != nil {
				return nil, errors.New("failed to install synthetic-pool reject guard")
			}
		}
	}
	routes, err = routetable.Get(65536)
	if err != nil {
		return nil, err
	}
	if err = verifyGuards(routes); err != nil {
		return nil, err
	}
	h, err := hosttun.Open(hosttun.OpenConfig{Name: "utun"})
	if err != nil {
		return nil, err
	}
	return &Host{Host: h, port: port, dir: ResolverDir}, nil
}

func prepareResolverDir(dir string) error {
	if err := os.Mkdir(dir, 0755); err == nil {
		if err = os.Chmod(dir, 0755); err != nil {
			return err
		}
	} else if !errors.Is(err, os.ErrExist) {
		return err
	}
	fi, err := os.Lstat(dir)
	if err != nil || !fi.IsDir() || fi.Mode().Perm()&0005 != 0005 {
		return errors.New("resolver directory must be readable by the system resolver")
	}
	return nil
}
func verifyGuards(routes []routetable.RouteEntry) error {
	for _, p := range []netip.Prefix{routing.Pool4, routing.Pool6} {
		found := false
		for _, r := range routes {
			if guard(r) && r.Dst.Prefix == p {
				found = true
			}
		}
		if !found {
			return errors.New("synthetic-pool reject guard is missing")
		}
	}
	return nil
}
func (h *Host) Apply(p *routing.Plan) error {
	if h.dns != nil && !h.dns.present() {
		return fmt.Errorf("%w: temporary DNS service changed", setup.ResolverConfigurationFailure)
	}
	routes, err := routetable.Get(65536)
	if err != nil {
		return fmt.Errorf("%w: %w", setup.HostInspectionFailure, err)
	}
	if err = CheckRoutes(routes, h.Name()); err != nil {
		return fmt.Errorf("%w: %w", setup.RouteConflict, err)
	}
	if err = verifyGuards(routes); err != nil {
		return fmt.Errorf("%w: %w", setup.RejectGuardMissing, err)
	}
	if err = WriteResolvers(h.dir, h.port, p.Domains); err != nil {
		return fmt.Errorf("%w: %w", setup.ResolverConfigurationFailure, err)
	}
	if err = h.Configure(p.Host); err != nil {
		return fmt.Errorf("%w: %w", setup.TunnelConfigurationFailure, err)
	}
	return nil
}

// WriteResolvers does not overwrite foreign configuration. Prefix-owned files
// persist after exit so a disconnected account's names cannot fall through to public DNS.
func WriteResolvers(dir, port string, domains []string) error {
	if _, err := net.LookupPort("udp", port); err != nil {
		return errors.New("invalid resolver port")
	}
	for _, d := range domains {
		if !routing.ValidName(d) {
			return errors.New("invalid resolver domain")
		}
		path := filepath.Join(dir, d)
		want := []byte(Header + "domain " + d + "\nnameserver 127.0.0.1\nport " + port + "\ntimeout 1\n")
		fi, err := os.Lstat(path)
		if err == nil {
			if !fi.Mode().IsRegular() {
				return errors.New("resolver path is not a regular file")
			}
			got, e := os.ReadFile(path)
			if e != nil {
				return e
			}
			if !strings.HasPrefix(string(got), Header) {
				return errors.New("existing resolver configuration conflicts with Tailmulti")
			}
			if string(got) == string(want) {
				continue
			}
		} else if !errors.Is(err, os.ErrNotExist) {
			return err
		}
		if err = store.Atomic(path, want, 0644); err != nil {
			return err
		}
	}
	return nil
}

// RecoveryReport is read-only. Removing guards/DNS tombstones is an explicit
// uninstall decision after sockets, caches and any active daemon have been closed.
func RecoveryReport() string {
	return fmt.Sprintf("Tailmulti retains reject routes for %s and %s and marked files under %s. See docs/ACTIVATION.md for inspection and cleanup. No changes made.", routing.Pool4, routing.Pool6, ResolverDir)
}
