//go:build darwin

package host

import (
	"bytes"
	"context"
	"errors"
	"net/netip"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"tailmulti.local/engine/internal/names"
	"tailmulti.local/engine/internal/setup"
	"tailscale.com/net/routetable"
)

type Check = setup.Check
type PreflightReport = setup.Report

type observations struct {
	routes                     []routetable.RouteEntry
	routeErr                   error
	dns, sockets               string
	dnsErr, socketErr, pathErr error
}

// Preflight is read-only; activation must repeat it before acquiring resources.
func Preflight(ctx context.Context) PreflightReport {
	var o observations
	o.routes, o.routeErr = routetable.Get(65536)
	o.dns, o.dnsErr = inspectCommand(ctx, "/usr/sbin/scutil", "--dns")
	o.sockets, o.socketErr = inspectCommand(ctx, "/usr/sbin/netstat", "-an")
	for _, path := range []string{StateDir, filepath.Dir(ControlSocket), ResolverDir} {
		if err := safeExistingParents(path); err != nil {
			o.pathErr = err
			break
		}
	}
	if fi, err := os.Lstat(ResolverDir); err == nil && fi.Mode().Perm()&0005 != 0005 {
		o.pathErr = errors.New("resolver directory is not readable by the system resolver")
	}
	r := assess(o)
	if _, err := ordinaryDNS(); err != nil {
		r.Ready = false
		r.Checks = append(r.Checks, Check{ID: "automatic_dns", State: "blocked", Title: "Ordinary network DNS is unavailable", Detail: "Automatic hostname translation needs one physical network with usable DNS servers. No DNS settings have been changed."})
	} else {
		r.Checks = append(r.Checks, Check{ID: "automatic_dns", State: "pass", Title: "Ordinary network DNS is available", Detail: "Tailmulti can forward lookups using this network's existing DNS servers."})
	}
	return r
}

func assess(o observations) PreflightReport {
	r := PreflightReport{Version: 1, Ready: true, Checks: []Check{}}
	add := func(id, state, title, detail string) {
		r.Checks = append(r.Checks, Check{ID: id, State: state, Title: title, Detail: detail})
		if state != "pass" {
			r.Ready = false
		}
	}
	if o.routeErr != nil {
		add("routes", "unknown", "Routes could not be checked", "Try again before enabling networking.")
	} else if CheckRoutes(o.routes, "") != nil {
		add("routes", "blocked", "A route conflicts with Tailmulti", "Another route or VPN already uses an address range Tailmulti needs. No routes have been changed.")
	} else {
		add("routes", "pass", "Local address ranges are available", "No conflicting synthetic-address routes were found.")
	}
	if o.routeErr != nil {
		add("vpn", "unknown", "VPN routing could not be checked", "Try again before enabling networking.")
	} else if otherTailnetVPN(o.routes) {
		add("vpn", "blocked", "Another VPN is routing Tailscale traffic", "Keep it installed. Temporarily disconnect it before enabling Tailmulti, after checking that DNS does not depend on it.")
	} else {
		add("vpn", "pass", "No competing Tailscale route found", "Tailmulti will check again immediately before activation.")
	}
	servers := defaultDNSServers(o.dns)
	viaVPN, routed := dnsRoutes(servers, o.routes)
	if o.dnsErr != nil || o.routeErr != nil || len(servers) == 0 || !routed {
		add("dns", "unknown", "System DNS could not be verified", "Check this Mac's network connection and try again. Tailmulti has not changed DNS.")
	} else if viaVPN {
		add("dns", "blocked", "System DNS depends on a VPN", "Keep that VPN connected for now. This Mac needs working DNS outside the VPN before it can switch to Tailmulti.")
	} else {
		add("dns", "pass", "No VPN route to system DNS found", "While networking is on, Tailmulti will forward ordinary DNS using this network's existing servers.")
	}
	if o.socketErr != nil {
		add("dns_port", "unknown", "The local DNS port could not be checked", "Try again before enabling networking.")
	} else if allDNSPortsInUse(o.sockets) {
		add("dns_port", "blocked", "No protected local DNS port is available", "All supported local DNS ports appear occupied. Existing services have not been stopped.")
	} else {
		add("dns_port", "pass", "A local DNS port is available", "Tailmulti will reserve an unused protected local port automatically. Existing DNS services stay in place.")
	}
	if o.pathErr != nil {
		add("paths", "blocked", "System folder permissions need attention", "Tailmulti requires root-owned, non-writable system folders. No permissions have been changed.")
	} else {
		add("paths", "pass", "System folder permissions are suitable", "Missing Tailmulti folders will only be created during approved setup.")
	}
	return r
}

func otherTailnetVPN(routes []routetable.RouteEntry) bool {
	v4, v6 := netip.MustParsePrefix("100.64.0.0/10"), netip.MustParsePrefix("fd7a:115c:a1e0::/48")
	for _, r := range routes {
		// A default route covers every address; it does not identify a Tailscale tunnel.
		// DNS dependence and conflicting split defaults have separate checks.
		prefix := routePrefix(r)
		if strings.HasPrefix(r.Interface, "utun") && prefix.IsValid() && prefix.Bits() > 0 && (prefix.Overlaps(v4) || prefix.Overlaps(v6)) {
			return true
		}
	}
	return false
}

// Supplemental and scoped resolvers do not establish ordinary DNS independence.
func defaultDNSServers(config string) []netip.Addr {
	var servers, current []netip.Addr
	domain := false
	flush := func() {
		if !domain {
			servers = append(servers, current...)
		}
		current, domain = nil, false
	}
	for _, raw := range strings.Split(config, "\n") {
		line := strings.TrimSpace(raw)
		if strings.HasPrefix(line, "DNS configuration (for ") {
			break
		}
		if strings.HasPrefix(line, "resolver #") {
			flush()
			continue
		}
		key, value, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		key, value = strings.TrimSpace(key), strings.TrimSpace(value)
		if key == "domain" && value != "" {
			domain = true
		}
		if strings.HasPrefix(key, "nameserver[") {
			if a, err := netip.ParseAddr(value); err == nil {
				current = append(current, a.WithZone(""))
			}
		}
	}
	flush()
	return servers
}

func dnsRoutes(servers []netip.Addr, routes []routetable.RouteEntry) (viaVPN, allRouted bool) {
	allRouted = len(servers) != 0
	for _, server := range servers {
		best, vpn := -1, false
		for _, r := range routes {
			prefix := routePrefix(r)
			if !prefix.IsValid() || !prefix.Contains(server) || prefix.Bits() < best {
				continue
			}
			v := strings.HasPrefix(r.Interface, "utun")
			if prefix.Bits() > best {
				vpn = v
			} else {
				vpn = vpn || v
			}
			best = prefix.Bits()
		}
		allRouted = allRouted && best >= 0
		viaVPN = viaVPN || vpn
	}
	return
}

func dnsPortInUse(table string) bool {
	return portInUse(table, 53)
}

func allDNSPortsInUse(table string) bool {
	for _, port := range names.SystemDNSPorts() {
		if !portInUse(table, port) {
			return false
		}
	}
	return true
}

func portInUse(table string, port uint16) bool {
	for _, line := range strings.Split(table, "\n") {
		f := strings.Fields(line)
		if len(f) < 5 || (!strings.HasPrefix(f[0], "tcp") && !strings.HasPrefix(f[0], "udp")) {
			continue
		}
		if strings.HasPrefix(f[0], "tcp") && f[len(f)-1] != "LISTEN" {
			continue
		}
		suffix := "." + strconv.Itoa(int(port))
		switch f[3] {
		case "*" + suffix, "0.0.0.0" + suffix, "127.0.0.1" + suffix, "::" + suffix, "::1" + suffix:
			return true
		}
	}
	return false
}

func safeExistingParents(path string) error {
	for {
		_, err := os.Lstat(path)
		if err == nil {
			return SecureParents(path, 0)
		}
		if !errors.Is(err, os.ErrNotExist) || path == "/" {
			return errors.New("cannot inspect system directory")
		}
		path = filepath.Dir(path)
	}
}

type inspectionBuffer struct {
	bytes.Buffer
	overflow bool
}

func (b *inspectionBuffer) Write(p []byte) (int, error) {
	if b.Len()+len(p) > 2*1024*1024 {
		b.overflow = true
		return len(p), nil
	}
	return b.Buffer.Write(p)
}

func inspectCommand(ctx context.Context, executable string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, executable, args...)
	var out inspectionBuffer
	cmd.Stdout = &out
	if err := cmd.Run(); err != nil || out.overflow {
		return "", errors.New("host inspection failed")
	}
	return out.String(), nil
}
