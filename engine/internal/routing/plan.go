// Package routing compiles profile snapshots into isolated, immutable packet and DNS plans.
package routing

import (
	"errors"
	"fmt"
	"net/netip"
	"sort"
	"strings"

	"github.com/gaissmai/bart"
	"tailmulti.local/engine/effectiveip"
	"tailmulti.local/engine/hosttun"
	"tailmulti.local/engine/packetmap"
	"tailmulti.local/engine/profile"
)

var Pool4 = netip.MustParsePrefix("198.18.0.0/15")
var Pool6 = netip.MustParsePrefix("fd74:6169:6c6d::/64")
var Host4 = Pool4.Addr().Next()
var Host6 = Pool6.Addr().Next()

type Account struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Enabled bool   `json:"enabled"`
	Removed bool   `json:"removed,omitempty"`
}
type State struct {
	Version  int                 `json:"version"`
	Accounts []Account           `json:"accounts"`
	Leases   []effectiveip.Lease `json:"leases"`
	// Domains and exact names are tombstones as well as active DNS claims.
	Domains []string `json:"domains"`
	// Aliases are exact public DNS names previously translated through one account.
	// Retain ownership across disconnect/removal so a cached name cannot switch accounts.
	Aliases     map[string]string       `json:"aliases,omitempty"`
	IdentityIPs map[string][]netip.Addr `json:"identityIPs,omitempty"`
}
type Device struct {
	ProfileID string       `json:"profileId"`
	Name      string       `json:"name"`
	Addresses []netip.Addr `json:"addresses"`
	Online    bool         `json:"online"`
}
type Plan struct {
	Mapper         *packetmap.Mapper
	Host           hosttun.Config
	Records        map[string][]netip.Addr
	Domains        []string
	Devices        []Device
	Conflicts      []string
	Canonical      map[netip.Addr]CanonicalTarget
	Aliases        map[string]string
	NameOwners     map[string]string
	ActiveProfiles map[string]bool
}

type CanonicalTarget struct {
	ProfileID   string
	NodeID      string
	EffectiveIP netip.Addr
	Active      bool
	Ambiguous   bool
}

var canonical4 = netip.MustParsePrefix("100.64.0.0/10")
var canonical6 = netip.MustParsePrefix("fd7a:115c:a1e0::/48")

func IsCanonical(ip netip.Addr) bool {
	return canonical4.Contains(ip.Unmap()) || canonical6.Contains(ip.Unmap())
}

func Name(s string) string { return strings.ToLower(strings.TrimSuffix(strings.TrimSpace(s), ".")) }
func ValidName(s string) bool {
	if len(s) > 253 || !strings.Contains(s, ".") {
		return false
	}
	for _, label := range strings.Split(s, ".") {
		if len(label) == 0 || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
			return false
		}
		for _, c := range label {
			if !(c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '-') {
				return false
			}
		}
	}
	return true
}
func IsSynthetic(ip netip.Addr) bool { return Pool4.Contains(ip.Unmap()) || Pool6.Contains(ip.Unmap()) }
func Validate(s State) error {
	if s.Version != 1 {
		return errors.New("unsupported state version")
	}
	ids := map[string]bool{}
	names := map[string]bool{}
	for _, a := range s.Accounts {
		if len(a.ID) != 32 {
			return errors.New("invalid profile ID")
		}
		for _, c := range a.ID {
			if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f') {
				return errors.New("invalid profile ID")
			}
		}
		if ids[a.ID] || (!a.Removed && names[a.Name]) {
			return errors.New("duplicate profile")
		}
		ids[a.ID] = true
		if !a.Removed {
			names[a.Name] = true
		}
	}
	used := map[netip.Addr]bool{}
	keys := map[effectiveip.NodeKey]bool{}
	for _, l := range s.Leases {
		if !ids[l.NodeKey.ProfileID] || l.NodeKey.NodeID == "" || !l.NodeKey.CanonicalIP.IsValid() || !IsSynthetic(l.EffectiveIP) || l.EffectiveIP == Host4 || l.EffectiveIP == Host6 || l.EffectiveIP == Pool4.Addr() || l.EffectiveIP == Pool6.Addr() || l.EffectiveIP.Is6() != l.NodeKey.CanonicalIP.Is6() || used[l.EffectiveIP] || keys[l.NodeKey] {
			return errors.New("invalid or duplicate address lease")
		}
		used[l.EffectiveIP] = true
		keys[l.NodeKey] = true
	}
	for _, d := range s.Domains {
		if !ValidName(d) {
			return errors.New("invalid DNS domain")
		}
	}
	if len(s.Aliases) > 10000 {
		return errors.New("custom hostname capacity reached")
	}
	for name, id := range s.Aliases {
		if !ValidName(name) || name != Name(name) || !ids[id] {
			return errors.New("invalid custom hostname ownership")
		}
	}
	for id, ips := range s.IdentityIPs {
		if !ids[id] || len(ips) > 256 {
			return errors.New("invalid retained identity addresses")
		}
		for _, ip := range ips {
			if !IsCanonical(ip) || ip.Is4In6() {
				return errors.New("invalid retained identity address")
			}
		}
	}
	return nil
}

// Build mutates only the caller's private state copy. Save it before publishing this plan.
func Build(s *State, snapshots []profile.Status) (*Plan, error) {
	if err := Validate(*s); err != nil {
		return nil, err
	}
	p := &Plan{Records: map[string][]netip.Addr{}, Devices: []Device{}, Conflicts: []string{}, Canonical: map[netip.Addr]CanonicalTarget{}, Aliases: map[string]string{}, NameOwners: map[string]string{}, ActiveProfiles: map[string]bool{}}
	table := packetmap.Table{Destinations: new(bart.Table[packetmap.Destination]), Sources: map[packetmap.SourceKey]packetmap.Source{}, InboundPeers: map[string]*bart.Table[netip.Addr]{}}
	enabled := map[string]bool{}
	for _, a := range s.Accounts {
		enabled[a.ID] = a.Enabled && !a.Removed
	}
	domainSet := map[string]bool{}
	for _, d := range s.Domains {
		domainSet[d] = true
	}
	for name, owner := range s.Aliases {
		p.Aliases[name] = owner
		domainSet[name] = true
	}
	active := []profile.Status{}
	targets := map[string][]target{}
	owners := map[string]map[string]bool{}
	for _, st := range snapshots {
		if !enabled[st.ProfileID] || st.BackendState != "Running" {
			continue
		}
		active = append(active, st)
		var err error
		targets[st.ProfileID], err = profileTargets(st)
		if err != nil {
			return nil, err
		}
		for _, ip := range st.SelfIPs {
			if !IsCanonical(ip) || ip.Is4In6() {
				continue
			}
			if s.IdentityIPs == nil {
				s.IdentityIPs = map[string][]netip.Addr{}
			}
			found := false
			for _, old := range s.IdentityIPs[st.ProfileID] {
				if old == ip {
					found = true
					break
				}
			}
			if !found {
				s.IdentityIPs[st.ProfileID] = append(s.IdentityIPs[st.ProfileID], ip)
			}
			if len(s.IdentityIPs[st.ProfileID]) > 256 {
				return nil, errors.New("retained identity address capacity reached")
			}
		}
		p.ActiveProfiles[st.ProfileID] = true
		if d := Name(st.MagicDNSSuffix); ValidName(d) {
			domainSet[d] = true
		}
		for _, destination := range targets[st.ProfileID] {
			n := destination.name
			if !ValidName(n) {
				continue
			}
			domainSet[n] = true // exact claims also cover shared-in names and removed peers
			if owners[n] == nil {
				owners[n] = map[string]bool{}
			}
			owners[n][st.ProfileID+"/"+destination.id] = true
		}
	}
	sort.Slice(active, func(i, j int) bool { return active[i].ProfileID < active[j].ProfileID })
	// Canonical addresses are unique within a profile. Refuse a malformed netmap.
	for _, st := range active {
		seen := map[netip.Addr]string{}
		for _, destination := range targets[st.ProfileID] {
			for _, ip := range destination.ips {
				if owner, ok := seen[ip]; ok && owner != destination.id {
					return nil, errors.New("ambiguous canonical address within profile")
				}
				seen[ip] = destination.id
			}
		}
	}
	alloc4 := effectiveip.NewAllocator(Pool4, s.Leases, Pool4.Addr(), Host4)
	alloc6 := effectiveip.NewAllocator(Pool6, s.Leases, Pool6.Addr(), Host6)
	leased := map[effectiveip.NodeKey]bool{}
	for _, l := range s.Leases {
		leased[l.NodeKey] = true
	}
	for _, st := range active {
		table.InboundPeers[st.ProfileID] = new(bart.Table[netip.Addr])
		for _, ip := range st.SelfIPs {
			if ip.IsValid() {
				host := Host4
				if ip.Is6() {
					host = Host6
				}
				table.Sources[packetmap.SourceKey{ProfileID: st.ProfileID, IPv6: ip.Is6()}] = packetmap.Source{HostIP: host, CanonicalIP: ip}
			}
		}
		for _, destination := range targets[st.ProfileID] {
			name := destination.name
			dev := Device{ProfileID: st.ProfileID, Name: name, Online: destination.online, Addresses: []netip.Addr{}}
			if destination.id == "" {
				continue
			}
			for _, ip := range destination.ips {
				if !ip.IsValid() || ip.Is4In6() {
					continue
				}
				if _, ok := table.Sources[packetmap.SourceKey{ProfileID: st.ProfileID, IPv6: ip.Is6()}]; !ok {
					continue
				}
				allocator := alloc4
				if ip.Is6() {
					allocator = alloc6
				}
				a, err := allocator.Assign([]effectiveip.Node{{ProfileID: st.ProfileID, NodeID: destination.id, CanonicalIP: ip}})
				if err != nil {
					return nil, err
				}
				l := a.Leases[0]
				if !leased[l.NodeKey] {
					s.Leases = append(s.Leases, l)
					leased[l.NodeKey] = true
				}
				table.Destinations.Insert(netip.PrefixFrom(l.EffectiveIP, l.EffectiveIP.BitLen()), packetmap.Destination{ProfileID: st.ProfileID, CanonicalIP: ip})
				table.InboundPeers[st.ProfileID].Insert(netip.PrefixFrom(ip, ip.BitLen()), l.EffectiveIP)
				dev.Addresses = append(dev.Addresses, l.EffectiveIP)
				if ValidName(name) && len(owners[name]) == 1 {
					p.Records[name] = append(p.Records[name], l.EffectiveIP)
					p.NameOwners[name] = st.ProfileID
				}
			}
			if !destination.service {
				p.Devices = append(p.Devices, dev)
			}
		}
		for _, route := range st.AvailableRoutes {
			// Single Tailscale addresses include Service VIPs. They are handled
			// by the account engine even when subnet acceptance is disabled.
			if route.Prefix.IsSingleIP() && IsCanonical(route.Prefix.Addr()) {
				continue
			}
			p.Conflicts = append(p.Conflicts, "Subnet routes are advertised but are not imported by this milestone.")
			break
		}
	}
	for name, own := range owners {
		if len(own) > 1 {
			p.NameOwners[name] = ""
			p.Conflicts = append(p.Conflicts, "A full hostname has multiple owners; DNS refuses that name.")
		}
	}
	p.Host.LocalAddrs = []netip.Prefix{netip.PrefixFrom(Host4, 32), netip.PrefixFrom(Host6, 128)}
	// Retain every route, including disconnected/removed profiles. Missing maps drop.
	for _, l := range s.Leases {
		src := Host4
		if l.EffectiveIP.Is6() {
			src = Host6
		}
		p.Host.Routes = append(p.Host.Routes, hosttun.Route{Destination: netip.PrefixFrom(l.EffectiveIP, l.EffectiveIP.BitLen()), Source: src})
		if IsCanonical(l.NodeKey.CanonicalIP) {
			ip := l.NodeKey.CanonicalIP
			if old, ok := p.Canonical[ip]; ok && (old.ProfileID != l.NodeKey.ProfileID || old.NodeID != l.NodeKey.NodeID) {
				old.Ambiguous = true
				old.Active = false
				p.Canonical[ip] = old
			} else if !ok {
				_, live := table.Destinations.Lookup(l.EffectiveIP)
				p.Canonical[ip] = CanonicalTarget{ProfileID: l.NodeKey.ProfileID, NodeID: l.NodeKey.NodeID, EffectiveIP: l.EffectiveIP, Active: live}
			}
		}
	}
	// A public DNS answer cannot distinguish a peer from another account's own node.
	for _, ips := range s.IdentityIPs {
		for _, ip := range ips {
			if target, ok := p.Canonical[ip]; ok {
				target.Ambiguous = true
				target.Active = false
				p.Canonical[ip] = target
			}
		}
	}
	s.Domains = nil
	for d := range domainSet {
		s.Domains = append(s.Domains, d)
	}
	sort.Strings(s.Domains)
	p.Domains = append([]string(nil), s.Domains...)
	p.Mapper = packetmap.New(table)
	if len(s.Leases) > 100000 {
		return nil, fmt.Errorf("address lease capacity reached")
	}
	return p, nil
}
