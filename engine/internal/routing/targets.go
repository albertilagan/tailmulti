package routing

import (
	"errors"
	"net/netip"
	"slices"
	"sort"
	"strings"

	"tailmulti.local/engine/profile"
	"tailscale.com/net/tsaddr"
	"tailscale.com/tailcfg"
)

// Service leases follow the service, not whichever peer currently hosts it.
// Reserve a namespace so a service never reuses a device's lease identity.
const serviceLeasePrefix = "service/"

type target struct {
	id, name string
	ips      []netip.Addr
	online   bool
	service  bool
}

func profileTargets(st profile.Status) ([]target, error) {
	targets := make([]target, 0, len(st.Peers)+len(st.Services))
	for _, peer := range st.Peers {
		if strings.HasPrefix(peer.NodeID, serviceLeasePrefix) {
			return nil, errors.New("device identity uses reserved service namespace")
		}
		targets = append(targets, target{id: peer.NodeID, name: Name(peer.DNSName), ips: peer.TailscaleIPs, online: peer.Online})
	}
	seen := map[string]bool{}
	for _, service := range st.Services {
		name := tailcfg.ServiceName(service.Name)
		if name.Validate() != nil || seen[service.Name] {
			return nil, errors.New("invalid or duplicate service identity")
		}
		seen[service.Name] = true
		if !ValidName(Name(st.MagicDNSSuffix)) {
			continue // Never invent a service domain before the tailnet supplies one.
		}
		fqdn := Name(name.WithoutPrefix() + "." + st.MagicDNSSuffix)
		if !ValidName(fqdn) || Name(service.DNSName) != fqdn {
			return nil, errors.New("service name is outside its account namespace")
		}
		ips := slices.Clone(service.TailscaleIPs)
		for _, ip := range ips {
			if !IsCanonical(ip) || ip.Is4In6() || ip == tsaddr.TailscaleServiceIP() || ip == tsaddr.TailscaleServiceIPv6() || slices.Contains(st.SelfIPs, ip) {
				return nil, errors.New("invalid service address")
			}
		}
		sort.Slice(ips, func(i, j int) bool { return ips[i].Less(ips[j]) })
		ips = slices.Compact(ips)
		targets = append(targets, target{id: serviceLeasePrefix + service.Name, name: fqdn, ips: ips, service: true})
	}
	sort.Slice(targets, func(i, j int) bool { return targets[i].id < targets[j].id })
	return targets, nil
}
