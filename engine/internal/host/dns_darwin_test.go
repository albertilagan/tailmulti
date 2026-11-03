//go:build darwin

package host

import (
	"context"
	"encoding/json"
	"net"
	"os"
	"testing"
	"time"

	"golang.org/x/net/dns/dnsmessage"
)

func dnsFixture() map[string]any {
	return map[string]any{
		"State:/Network/Global/IPv4":            map[string]any{"PrimaryService": "wifi", "PrimaryInterface": "en7"},
		"Setup:/Network/Service/wifi/Interface": map[string]any{"DeviceName": "en7", "Type": "IEEE80211"},
		"State:/Network/Service/wifi/DNS":       map[string]any{"ServerAddresses": []string{"192.0.2.53"}},
	}
}
func TestBaseDNSSelection(t *testing.T) {
	for _, kind := range []string{"dhcp", "manual", "ipv6 only", "same primaries", "two networks", "tunnel", "loopback", "canonical", "synthetic", "mapped loopback", "bad type", "wrong interface", "no servers", "scoped server", "foreign scope"} {
		t.Run(kind, func(t *testing.T) {
			m := dnsFixture()
			want := "192.0.2.53:53"
			fail := false
			switch kind {
			case "manual":
				m["Setup:/Network/Service/wifi/DNS"] = map[string]any{"ServerAddresses": []string{"192.0.2.54"}, "ServerPort": 5353}
				want = "192.0.2.54:5353"
			case "ipv6 only":
				m["State:/Network/Global/IPv6"] = m["State:/Network/Global/IPv4"]
				delete(m, "State:/Network/Global/IPv4")
				m["State:/Network/Service/wifi/DNS"] = map[string]any{"ServerAddresses": []string{"2001:db8::53"}}
				want = "[2001:db8::53]:53"
			case "same primaries":
				m["State:/Network/Global/IPv6"] = m["State:/Network/Global/IPv4"]
			case "two networks":
				m["State:/Network/Global/IPv6"] = map[string]any{"PrimaryService": "other", "PrimaryInterface": "en8"}
				fail = true
			case "tunnel":
				m["Setup:/Network/Service/wifi/Interface"] = map[string]any{"DeviceName": "en7", "Type": "VPN"}
				fail = true
			case "wrong interface":
				m["Setup:/Network/Service/wifi/Interface"] = map[string]any{"DeviceName": "en8", "Type": "Ethernet"}
				fail = true
			case "bad type":
				m["State:/Network/Service/wifi/DNS"] = map[string]any{"ServerAddresses": "192.0.2.53"}
				fail = true
			case "no servers":
				delete(m, "State:/Network/Service/wifi/DNS")
				fail = true
			case "scoped server":
				m["State:/Network/Service/wifi/DNS"] = map[string]any{"ServerAddresses": []string{"fe80::53"}}
				want = "[fe80::53%en7]:53"
			default:
				ip := map[string]string{"loopback": "127.0.0.1", "canonical": "100.100.100.100", "synthetic": "198.18.0.2", "mapped loopback": "::ffff:127.0.0.1", "foreign scope": "fe80::53%en8"}[kind]
				m["Setup:/Network/Service/wifi/DNS"] = map[string]any{"ServerAddresses": []string{ip}}
				fail = true
			}
			b, _ := json.Marshal(m)
			u, err := baseDNS(b, func(name string) (*net.Interface, error) {
				return &net.Interface{Name: name, Index: 7, Flags: net.FlagUp}, nil
			})
			if fail {
				if err == nil {
					t.Fatal("unsafe base DNS accepted")
				}
				return
			}
			if err != nil || len(u.Servers) != 1 || u.Servers[0].String() != want || u.Interface != 7 {
				t.Fatal("incorrect base selection", err)
			}
		})
	}
}

func TestReadOnlyNativeDNSForwarding(t *testing.T) {
	if os.Getenv("TAILMULTI_TEST_ORDINARY_DNS") != "1" {
		t.Skip("explicit read-only host DNS check")
	}
	u, err := ordinaryDNS()
	if err != nil {
		t.Fatal(err)
	}
	if len(u.Servers) == 0 || u.Interface <= 0 {
		t.Fatal("no physical DNS")
	}
	q := dnsmessage.Message{Header: dnsmessage.Header{ID: 1234, RecursionDesired: true}, Questions: []dnsmessage.Question{{Name: dnsmessage.MustNewName("example.com."), Type: dnsmessage.TypeA, Class: dnsmessage.ClassINET}}}
	b, _ := q.Pack()
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
	defer cancel()
	b, err = NewDNSForwarder().Exchange(ctx, b)
	if err != nil {
		t.Fatal(err)
	}
	var a dnsmessage.Message
	if a.Unpack(b) != nil || a.ID != q.ID || a.RCode != 0 || len(a.Answers) == 0 {
		t.Fatal("physical DNS query failed")
	}
	t.Log("Read-only native snapshot and interface-bound DNS passed; no resolver installation")
}
