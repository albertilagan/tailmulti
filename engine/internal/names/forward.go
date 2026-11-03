package names

import (
	"context"
	"crypto/rand"
	"encoding/binary"
	"errors"
	"io"
	"net"
	"net/netip"
	"reflect"
	"time"

	"golang.org/x/net/dns/dnsmessage"
)

// Upstream identifies one physical-network resolver group. Identity includes its
// configuration generation; a response from a changed network is discarded.
type Upstream struct {
	Servers   []netip.AddrPort
	Interface int
	Identity  string
}
type Forwarder struct {
	Snapshot func() (Upstream, error)
	Dial     func(context.Context, string, string, int) (net.Conn, error)
}

var errUpstream = errors.New("ordinary network DNS is unavailable")

func (f *Forwarder) Exchange(ctx context.Context, query []byte) ([]byte, error) {
	var q dnsmessage.Message
	if len(query) > 4096 || q.Unpack(query) != nil || q.Response || len(q.Questions) != 1 {
		return nil, errUpstream
	}
	u, err := f.Snapshot()
	if err != nil || len(u.Servers) == 0 || len(u.Servers) > 8 || u.Interface <= 0 {
		return nil, errUpstream
	}
	var id [2]byte
	if _, err = rand.Read(id[:]); err != nil {
		return nil, errUpstream
	}
	wire := append([]byte(nil), query...)
	copy(wire[:2], id[:])
	q.ID = binary.BigEndian.Uint16(id[:])
	for _, server := range u.Servers {
		if ctx.Err() != nil {
			break
		}
		attempt, cancel := context.WithTimeout(ctx, time.Second)
		response, e := f.exchangeOne(attempt, u.Interface, server, "udp", wire, &q)
		cancel()
		if e != nil {
			continue
		}
		var a dnsmessage.Message
		if a.Unpack(response) != nil {
			continue
		}
		if a.Truncated {
			attempt, cancel = context.WithTimeout(ctx, 1500*time.Millisecond)
			response, e = f.exchangeOne(attempt, u.Interface, server, "tcp", wire, &q)
			cancel()
			if e != nil || a.Unpack(response) != nil || a.Truncated {
				continue
			}
		}
		if a.RCode == dnsmessage.RCodeServerFailure || a.RCode == dnsmessage.RCodeRefused {
			continue
		}
		current, e := f.Snapshot()
		if e != nil || !reflect.DeepEqual(u, current) || ctx.Err() != nil {
			return nil, errUpstream
		}
		copy(response[:2], query[:2])
		return response, nil
	}
	return nil, errUpstream
}
func (f *Forwarder) exchangeOne(ctx context.Context, index int, server netip.AddrPort, network string, wire []byte, q *dnsmessage.Message) ([]byte, error) {
	if !server.IsValid() {
		return nil, errUpstream
	}
	family := "4"
	if server.Addr().Is6() {
		family = "6"
	}
	c, err := f.Dial(ctx, network+family, server.String(), index)
	if err != nil {
		return nil, errUpstream
	}
	defer c.Close()
	stop := context.AfterFunc(ctx, func() { c.Close() })
	defer stop()
	if deadline, ok := ctx.Deadline(); ok {
		if c.SetDeadline(deadline) != nil {
			return nil, errUpstream
		}
	}
	if network == "tcp" {
		h := []byte{byte(len(wire) >> 8), byte(len(wire))}
		if _, err = c.Write(append(h, wire...)); err != nil {
			return nil, errUpstream
		}
		if _, err = io.ReadFull(c, h); err != nil {
			return nil, errUpstream
		}
		n := int(binary.BigEndian.Uint16(h))
		if n < 12 {
			return nil, errUpstream
		}
		b := make([]byte, n)
		if _, err = io.ReadFull(c, b); err != nil {
			return nil, errUpstream
		}
		var a dnsmessage.Message
		if a.Unpack(b) != nil || !validResponse(q, &a) {
			return nil, errUpstream
		}
		return b, nil
	}
	if _, err = c.Write(wire); err != nil {
		return nil, errUpstream
	}
	b := make([]byte, 65535)
	for {
		n, e := c.Read(b)
		if e != nil {
			return nil, errUpstream
		}
		var a dnsmessage.Message
		if a.Unpack(b[:n]) == nil && validResponse(q, &a) {
			return b[:n], nil
		}
		if ctx.Err() != nil {
			return nil, errUpstream
		}
	}
}
