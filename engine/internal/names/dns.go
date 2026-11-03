// Package names serves isolated tailnet names and translates ordinary DNS answers.
package names

import (
	"context"
	"encoding/binary"
	"io"
	"net"
	"net/netip"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"golang.org/x/net/dns/dnsmessage"
	"tailmulti.local/engine/internal/routing"
)

type View struct {
	Records        map[string][]netip.Addr
	Domains        []string
	Canonical      map[netip.Addr]routing.CanonicalTarget
	Aliases        map[string]string
	NameOwners     map[string]string
	ActiveProfiles map[string]bool
}
type Resolver struct {
	view    atomic.Pointer[View]
	forward atomic.Pointer[forwardConfig]
}

func (r *Resolver) Set(p *routing.Plan) {
	r.view.Store(&View{Records: p.Records, Domains: p.Domains, Canonical: p.Canonical, Aliases: p.Aliases, NameOwners: p.NameOwners, ActiveProfiles: p.ActiveProfiles})
}
func (r *Resolver) Reply(data []byte) []byte {
	return r.ReplyContext(context.Background(), data)
}
func (r *Resolver) ReplyContext(ctx context.Context, data []byte) []byte {
	var q dnsmessage.Message
	if err := q.Unpack(data); err != nil || q.Header.Response || len(q.Questions) != 1 {
		return nil
	}
	ans := dnsmessage.Message{Header: dnsmessage.Header{ID: q.ID, Response: true, Authoritative: true, RecursionDesired: q.RecursionDesired}, Questions: q.Questions}
	question := q.Questions[0]
	name := routing.Name(question.Name.String())
	v := r.view.Load()
	owned := false
	if v != nil {
		for _, d := range v.Domains {
			if name == d || strings.HasSuffix(name, "."+d) {
				owned = true
				break
			}
		}
	}
	if q.Header.OpCode != 0 || question.Class != dnsmessage.ClassINET {
		ans.Header.RCode = dnsmessage.RCodeNotImplemented
	} else if f := r.forward.Load(); f != nil && (!owned || v.Aliases[name] != "") {
		return r.forwardReply(ctx, data, &q, f)
	} else if !owned {
		ans.Header.RCode = dnsmessage.RCodeRefused
	} else if ips, ok := v.Records[name]; !ok {
		ans.Header.RCode = dnsmessage.RCodeNameError
	} else {
		for _, ip := range ips {
			h := dnsmessage.ResourceHeader{Name: question.Name, Class: dnsmessage.ClassINET, TTL: 5}
			if question.Type == dnsmessage.TypeA && ip.Is4() {
				h.Type = dnsmessage.TypeA
				ans.Answers = append(ans.Answers, dnsmessage.Resource{Header: h, Body: &dnsmessage.AResource{A: ip.As4()}})
			}
			if question.Type == dnsmessage.TypeAAAA && ip.Is6() {
				h.Type = dnsmessage.TypeAAAA
				ans.Answers = append(ans.Answers, dnsmessage.Resource{Header: h, Body: &dnsmessage.AAAAResource{AAAA: ip.As16()}})
			}
		}
	}
	b, err := ans.Pack()
	if err != nil {
		return nil
	}
	return b
}

type Server struct {
	udp    *net.UDPConn
	tcp    net.Listener
	wg     sync.WaitGroup
	done   chan struct{}
	once   sync.Once
	slots  chan struct{}
	ctx    context.Context
	cancel context.CancelFunc
}

func Listen(addr string, r *Resolver) (*Server, error) {
	// The caller supplies a loopback literal only. Never expose this DNS service to a tailnet.
	ap, err := netip.ParseAddrPort(addr)
	if err != nil || !ap.Addr().IsLoopback() {
		return nil, &net.AddrError{Err: "DNS requires a loopback address", Addr: addr}
	}
	udp, err := net.ListenUDP("udp4", net.UDPAddrFromAddrPort(ap))
	if err != nil {
		return nil, err
	}
	tcp, err := net.Listen("tcp4", udp.LocalAddr().String())
	if err != nil {
		udp.Close()
		return nil, err
	}
	ctx, cancel := context.WithCancel(context.Background())
	s := &Server{udp: udp, tcp: tcp, done: make(chan struct{}), slots: make(chan struct{}, 64), ctx: ctx, cancel: cancel}
	s.wg.Add(2)
	go func() {
		defer s.wg.Done()
		buf := make([]byte, 4096)
		for {
			n, a, e := udp.ReadFromUDPAddrPort(buf)
			if e != nil {
				return
			}
			select {
			case s.slots <- struct{}{}:
				q := append([]byte(nil), buf[:n]...)
				s.wg.Add(1)
				go func() {
					defer s.wg.Done()
					defer func() { <-s.slots }()
					b := r.ReplyContext(s.ctx, q)
					if len(b) > 512 {
						var m dnsmessage.Message
						if m.Unpack(b) == nil {
							m.Answers, m.Authorities, m.Additionals = nil, nil, nil
							m.Header.Truncated = true
							b, _ = m.Pack()
						}
					}
					if len(b) > 0 {
						_, _ = udp.WriteToUDPAddrPort(b, a)
					}
				}()
			default: // Bounded overload; callers retry rather than growing work without limit.
			}
		}
	}()
	go func() {
		defer s.wg.Done()
		for {
			c, e := tcp.Accept()
			if e != nil {
				return
			}
			select {
			case s.slots <- struct{}{}:
				s.wg.Add(1)
				go s.serveTCP(c, r)
			default:
				c.Close()
			}
		}
	}()
	return s, nil
}
func (s *Server) Addr() string { return s.tcp.Addr().String() }
func (s *Server) serveTCP(c net.Conn, r *Resolver) {
	defer s.wg.Done()
	defer c.Close()
	stop := context.AfterFunc(s.ctx, func() { c.Close() })
	defer stop()
	defer func() { <-s.slots }()
	for {
		c.SetDeadline(time.Now().Add(5 * time.Second))
		var h [2]byte
		if _, e := io.ReadFull(c, h[:]); e != nil {
			return
		}
		n := int(binary.BigEndian.Uint16(h[:]))
		if n == 0 || n > 4096 {
			return
		}
		q := make([]byte, n)
		if _, e := io.ReadFull(c, q); e != nil {
			return
		}
		b := r.ReplyContext(s.ctx, q)
		if len(b) == 0 {
			return
		}
		binary.BigEndian.PutUint16(h[:], uint16(len(b)))
		if _, e := c.Write(append(h[:], b...)); e != nil {
			return
		}
		select {
		case <-s.done:
			return
		default:
		}
	}
}
func (s *Server) Close() {
	s.once.Do(func() { s.cancel(); close(s.done); s.udp.Close(); s.tcp.Close(); s.wg.Wait() })
}
