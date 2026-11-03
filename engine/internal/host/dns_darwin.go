//go:build darwin

package host

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net"
	"net/netip"
	"strconv"
	"strings"
	"syscall"

	"golang.org/x/sys/unix"
	"tailmulti.local/engine/internal/names"
	"tailmulti.local/engine/internal/routing"
)

var errBaseDNS = errors.New("automatic DNS needs one ordinary physical network with usable DNS servers")

type dnsEntity struct {
	Servers      []string `json:"ServerAddresses"`
	Port         int      `json:"ServerPort"`
	MatchDomains []string `json:"SupplementalMatchDomains"`
}
type primaryEntity struct {
	Service   string `json:"PrimaryService"`
	Interface string `json:"PrimaryInterface"`
}

// Refuse ambiguous networks; never silently replace unreachable manual DNS with DHCP.
func baseDNS(data []byte, lookup func(string) (*net.Interface, error)) (names.Upstream, error) {
	var entries map[string]json.RawMessage
	if json.Unmarshal(data, &entries) != nil {
		return names.Upstream{}, errBaseDNS
	}
	var primary primaryEntity
	for _, family := range []string{"IPv4", "IPv6"} {
		b := entries["State:/Network/Global/"+family]
		if len(b) == 0 {
			continue
		}
		var p primaryEntity
		if json.Unmarshal(b, &p) != nil {
			return names.Upstream{}, errBaseDNS
		}
		if p.Service == "" {
			continue
		}
		if p.Interface == "" || strings.Contains(p.Service, "/") || (primary.Service != "" && primary != p) {
			return names.Upstream{}, errBaseDNS
		}
		primary = p
	}
	if primary.Service == "" {
		return names.Upstream{}, errBaseDNS
	}
	var device struct {
		Name string `json:"DeviceName"`
		Type string `json:"Type"`
	}
	if json.Unmarshal(entries["Setup:/Network/Service/"+primary.Service+"/Interface"], &device) != nil || device.Name != primary.Interface || (device.Type != "Ethernet" && device.Type != "IEEE80211") {
		return names.Upstream{}, errBaseDNS
	}
	iface, err := lookup(device.Name)
	if err != nil || iface.Index <= 0 || iface.Flags&net.FlagUp == 0 || iface.Flags&net.FlagLoopback != 0 {
		return names.Upstream{}, errBaseDNS
	}
	var state, setup dnsEntity
	for _, row := range []struct {
		key string
		dst *dnsEntity
	}{{"State", &state}, {"Setup", &setup}} {
		b := entries[row.key+":/Network/Service/"+primary.Service+"/DNS"]
		if len(b) > 0 && json.Unmarshal(b, row.dst) != nil {
			return names.Upstream{}, errBaseDNS
		}
	}
	selected := state
	if len(setup.Servers) > 0 {
		selected = setup
	}
	if setup.Port != 0 {
		selected.Port = setup.Port
	} else if selected.Port == 0 {
		selected.Port = state.Port
	}
	if selected.Port == 0 {
		selected.Port = 53
	}
	if selected.Port < 1 || selected.Port > 65535 || len(selected.Servers) == 0 || len(selected.Servers) > 8 {
		return names.Upstream{}, errBaseDNS
	}
	for _, d := range selected.MatchDomains {
		if d != "" {
			return names.Upstream{}, errBaseDNS
		}
	}
	u := names.Upstream{Interface: iface.Index}
	for _, s := range selected.Servers {
		ip, e := netip.ParseAddr(s)
		if e != nil || ip.Is4In6() || ip.IsUnspecified() || ip.IsLoopback() || ip.IsMulticast() || routing.IsCanonical(ip) || routing.IsSynthetic(ip) {
			return names.Upstream{}, errBaseDNS
		}
		if ip.Is6() && ip.IsLinkLocalUnicast() {
			if ip.Zone() != "" && ip.Zone() != device.Name {
				return names.Upstream{}, errBaseDNS
			}
			ip = ip.WithZone(device.Name)
		} else if ip.Zone() != "" {
			return names.Upstream{}, errBaseDNS
		}
		u.Servers = append(u.Servers, netip.AddrPortFrom(ip, uint16(selected.Port)))
	}
	sum := sha256.Sum256(data)
	u.Identity = hex.EncodeToString(sum[:])
	return u, nil
}

func ordinaryDNS() (names.Upstream, error) {
	b, err := readDNSSnapshot()
	if err != nil {
		return names.Upstream{}, err
	}
	return baseDNS(b, net.InterfaceByName)
}

func NewDNSForwarder() *names.Forwarder {
	return &names.Forwarder{Snapshot: ordinaryDNS, Dial: func(ctx context.Context, network, address string, index int) (net.Conn, error) {
		d := net.Dialer{Control: func(_, _ string, raw syscall.RawConn) error {
			var optionErr error
			err := raw.Control(func(fd uintptr) {
				if strings.HasSuffix(network, "6") {
					optionErr = unix.SetsockoptInt(int(fd), unix.IPPROTO_IPV6, unix.IPV6_BOUND_IF, index)
				} else {
					optionErr = unix.SetsockoptInt(int(fd), unix.IPPROTO_IP, unix.IP_BOUND_IF, index)
				}
			})
			if err != nil {
				return err
			}
			return optionErr
		}}
		return d.DialContext(ctx, network, address)
	}}
}

func (h *Host) EnableAutomaticDNS() error {
	if h.dns != nil {
		return errors.New("automatic DNS already enabled")
	}
	if _, err := ordinaryDNS(); err != nil {
		return err
	}
	port, err := strconv.ParseUint(h.port, 10, 16)
	if err != nil || port == 0 || port >= 1024 {
		return errors.New("automatic DNS requires a protected local port")
	}
	n, err := installDefaultDNS(uint16(port))
	if err != nil {
		return err
	}
	if !n.present() {
		n.close()
		return errors.New("temporary DNS service could not be verified")
	}
	h.dns = n
	return nil
}
func (h *Host) Close() error {
	if h.dns != nil {
		h.dns.close()
	}
	return h.Host.Close()
}
