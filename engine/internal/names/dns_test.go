package names

import (
	"encoding/binary"
	"golang.org/x/net/dns/dnsmessage"
	"io"
	"net"
	"net/netip"
	"tailmulti.local/engine/internal/routing"
	"testing"
	"time"
)

func query(n string, typ dnsmessage.Type) []byte {
	b, _ := (&dnsmessage.Message{Header: dnsmessage.Header{ID: 42, RecursionDesired: true}, Questions: []dnsmessage.Question{{Name: dnsmessage.MustNewName(n), Type: typ, Class: dnsmessage.ClassINET}}}).Pack()
	return b
}
func TestAuthoritativeDNSAndTombstones(t *testing.T) {
	r := new(Resolver)
	p := &routing.Plan{Domains: []string{"home.ts.net", "work.ts.net"}, Records: map[string][]netip.Addr{"server.home.ts.net": {netip.MustParseAddr("198.18.0.2"), netip.MustParseAddr("fd74:6169:6c6d::2")}}}
	r.Set(p)
	for _, tc := range []struct {
		name    string
		typ     dnsmessage.Type
		code    dnsmessage.RCode
		answers int
	}{{"server.home.ts.net.", dnsmessage.TypeA, 0, 1}, {"SERVER.HOME.TS.NET.", dnsmessage.TypeAAAA, 0, 1}, {"server.work.ts.net.", dnsmessage.TypeA, dnsmessage.RCodeNameError, 0}, {"server.", dnsmessage.TypeA, dnsmessage.RCodeRefused, 0}, {"example.com.", dnsmessage.TypeA, dnsmessage.RCodeRefused, 0}, {"server.home.ts.net.evil.com.", dnsmessage.TypeA, dnsmessage.RCodeRefused, 0}, {"server.home.ts.net.", dnsmessage.Type(65), 0, 0}} {
		var ans dnsmessage.Message
		if err := ans.Unpack(r.Reply(query(tc.name, tc.typ))); err != nil {
			t.Fatal(err)
		}
		if ans.RCode != tc.code || len(ans.Answers) != tc.answers || ans.RecursionAvailable {
			t.Fatalf("%s: %+v", tc.name, ans)
		}
	}
	r.Set(&routing.Plan{Domains: p.Domains, Records: map[string][]netip.Addr{}})
	var ans dnsmessage.Message
	ans.Unpack(r.Reply(query("server.home.ts.net.", dnsmessage.TypeA)))
	if ans.RCode != dnsmessage.RCodeNameError {
		t.Fatal("disconnected name escaped")
	}
}
func TestUDPAndTCP(t *testing.T) {
	r := new(Resolver)
	r.Set(&routing.Plan{Domains: []string{"home.ts.net"}, Records: map[string][]netip.Addr{"server.home.ts.net": {netip.MustParseAddr("198.18.0.2")}}})
	s, err := Listen("127.0.0.1:0", r)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	for _, network := range []string{"udp4", "tcp4"} {
		c, err := net.Dial(network, s.Addr())
		if err != nil {
			t.Fatal(err)
		}
		c.SetDeadline(time.Now().Add(time.Second))
		q := query("server.home.ts.net.", dnsmessage.TypeA)
		if network == "tcp4" {
			h := make([]byte, 2)
			binary.BigEndian.PutUint16(h, uint16(len(q)))
			q = append(h, q...)
		}
		if _, err = c.Write(q); err != nil {
			t.Fatal(err)
		}
		buf := make([]byte, 4096)
		n := 0
		if network == "tcp4" {
			if _, err = io.ReadFull(c, buf[:2]); err != nil {
				t.Fatal(err)
			}
			n = int(binary.BigEndian.Uint16(buf[:2]))
			_, err = io.ReadFull(c, buf[:n])
		} else {
			n, err = c.Read(buf)
		}
		c.Close()
		if err != nil {
			t.Fatal(err)
		}
		var ans dnsmessage.Message
		if err = ans.Unpack(buf[:n]); err != nil || len(ans.Answers) != 1 {
			t.Fatal("missing DNS answer", err)
		}
	}
}
func FuzzDNS(f *testing.F) {
	f.Add(query("server.home.ts.net.", dnsmessage.TypeA))
	r := new(Resolver)
	f.Fuzz(func(t *testing.T, b []byte) { r.Reply(b) })
}
