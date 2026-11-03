package names

import (
	"context"
	"net"
	"net/netip"
	"sync/atomic"
	"testing"
	"time"

	"golang.org/x/net/dns/dnsmessage"
	"tailmulti.local/engine/internal/routing"
)

func TestForwardUDPToTCPAndNetworkChange(t *testing.T) {
	r := new(Resolver)
	ips := make([]netip.Addr, 70)
	for i := range ips {
		ips[i] = netip.AddrFrom4([4]byte{192, 0, 2, byte(i + 1)})
	}
	r.Set(&routing.Plan{Domains: []string{"example.com"}, Records: map[string][]netip.Addr{"large.example.com": ips}})
	s, err := Listen("127.0.0.1:0", r)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	server := netip.MustParseAddrPort(s.Addr())
	for _, changed := range []bool{false, true} {
		var snapshots int
		var udp, tcp atomic.Int32
		f := Forwarder{Snapshot: func() (Upstream, error) {
			snapshots++
			generation := "one"
			if changed && snapshots > 1 {
				generation = "two"
			}
			return Upstream{Servers: []netip.AddrPort{server}, Interface: 7, Identity: generation}, nil
		}, Dial: func(ctx context.Context, network, addr string, index int) (net.Conn, error) {
			if index != 7 || addr != s.Addr() {
				t.Fatal("lost bound upstream selection")
			}
			if network == "udp4" {
				udp.Add(1)
			} else if network == "tcp4" {
				tcp.Add(1)
			} else {
				t.Fatal("unexpected protocol")
			}
			return (&net.Dialer{}).DialContext(ctx, network, addr)
		}}
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		b, err := f.Exchange(ctx, query("large.example.com.", dnsmessage.TypeA))
		cancel()
		if udp.Load() != 1 || tcp.Load() != 1 {
			t.Fatal("truncated UDP did not retry TCP")
		}
		if changed {
			if err == nil {
				t.Fatal("stale network answer published")
			}
			continue
		}
		if err != nil {
			t.Fatal(err)
		}
		a := decodeReply(t, b)
		if a.ID != 42 || len(a.Answers) != len(ips) {
			t.Fatal("framing or ID changed")
		}
	}
}

func TestUDPForwardingDoesNotBlockLocalNamesAndCancels(t *testing.T) {
	r := new(Resolver)
	r.Set(&routing.Plan{Domains: []string{"work.ts.net"}, Records: map[string][]netip.Addr{"peer.work.ts.net": {netip.MustParseAddr("198.18.0.2")}}})
	started := make(chan struct{})
	r.EnableForwarding(context.Background(), func(ctx context.Context, _ []byte) ([]byte, error) {
		close(started)
		<-ctx.Done()
		return nil, ctx.Err()
	}, nil)
	s, err := Listen("127.0.0.1:0", r)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	c, err := net.Dial("udp4", s.Addr())
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	_, _ = c.Write(query("slow.example.com.", dnsmessage.TypeA))
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("query not forwarded")
	}
	local, err := net.Dial("udp4", s.Addr())
	if err != nil {
		t.Fatal(err)
	}
	defer local.Close()
	_ = local.SetDeadline(time.Now().Add(time.Second))
	_, _ = local.Write(query("peer.work.ts.net.", dnsmessage.TypeA))
	b := make([]byte, 512)
	n, err := local.Read(b)
	if err != nil {
		t.Fatal("slow public query blocked MagicDNS", err)
	}
	if len(decodeReply(t, b[:n]).Answers) != 1 {
		t.Fatal("missing local answer")
	}
	done := make(chan struct{})
	go func() { s.Close(); close(done) }()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("shutdown did not cancel forwarding")
	}
}

func TestForwarderRejectsSpoofedQuestionAndID(t *testing.T) {
	u, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	defer u.Close()
	done := make(chan struct{})
	go func() {
		defer close(done)
		b := make([]byte, 4096)
		n, addr, e := u.ReadFromUDP(b)
		if e != nil {
			return
		}
		wire := answerFor(b[:n], addressRR("app.example.com.", "192.0.2.1"))
		wire[0] ^= 1
		_, _ = u.WriteToUDP(wire, addr)
		wire[0] ^= 1
		var bad dnsmessage.Message
		_ = bad.Unpack(wire)
		bad.Questions[0].Name = dnsmessage.MustNewName("wrong.example.com.")
		wrong, _ := bad.Pack()
		_, _ = u.WriteToUDP(wrong, addr)
		_, _ = u.WriteToUDP(wire, addr)
	}()
	f := Forwarder{Snapshot: func() (Upstream, error) {
		return Upstream{Servers: []netip.AddrPort{u.LocalAddr().(*net.UDPAddr).AddrPort()}, Interface: 1}, nil
	}, Dial: func(ctx context.Context, n, a string, _ int) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, n, a)
	}}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	b, err := f.Exchange(ctx, query("app.example.com.", dnsmessage.TypeA))
	if err != nil {
		t.Fatal(err)
	}
	a := decodeReply(t, b)
	if a.ID != 42 || a.Questions[0].Name.String() != "app.example.com." {
		t.Fatal("accepted spoofed response")
	}
	<-done
}
