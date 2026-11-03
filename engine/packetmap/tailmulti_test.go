package packetmap

import (
	"encoding/binary"
	"github.com/gaissmai/bart"
	"net/netip"
	"testing"
)

func sum(b []byte) uint16 {
	var n uint32
	for len(b) >= 2 {
		n += uint32(binary.BigEndian.Uint16(b))
		b = b[2:]
	}
	if len(b) > 0 {
		n += uint32(b[0]) << 8
	}
	for n>>16 != 0 {
		n = (n & 0xffff) + (n >> 16)
	}
	return ^uint16(n)
}
func pseudo(p []byte, proto byte) []byte {
	if p[0]>>4 == 4 {
		b := append([]byte{}, p[12:20]...)
		return append(b, 0, proto, 0, byte(len(p)-20))
	}
	b := append([]byte{}, p[8:40]...)
	return append(b, 0, 0, 0, byte(len(p)-40), 0, 0, 0, proto)
}
func transportPacket(src, dst netip.Addr, proto byte) []byte {
	off := 20
	if src.Is6() {
		off = 40
	}
	size := 20
	if proto == 17 || proto == 1 || proto == 58 {
		size = 8
	}
	b := make([]byte, off+size+4)
	if off == 20 {
		b[0] = 0x45
		b[8] = 64
		b[9] = proto
		binary.BigEndian.PutUint16(b[2:4], uint16(len(b)))
		copy(b[12:16], src.AsSlice())
		copy(b[16:20], dst.AsSlice())
	} else {
		b[0] = 0x60
		b[6] = proto
		b[7] = 64
		binary.BigEndian.PutUint16(b[4:6], uint16(size+4))
		copy(b[8:24], src.AsSlice())
		copy(b[24:40], dst.AsSlice())
	}
	binary.BigEndian.PutUint16(b[off:off+2], 1234)
	binary.BigEndian.PutUint16(b[off+2:off+4], 22)
	cs := off + 16
	switch proto {
	case 6:
		b[off+12] = 0x50
		b[off+13] = 0x02
	case 17:
		binary.BigEndian.PutUint16(b[off+4:off+6], uint16(size+4))
		cs = off + 6
	case 1:
		b[off] = 8
		b[off+1] = 0
		cs = off + 2
	case 58:
		b[off] = 128
		b[off+1] = 0
		cs = off + 2
	}
	b[cs], b[cs+1] = 0, 0
	bytes := b[off:]
	if proto != 1 {
		bytes = append(pseudo(b, proto), bytes...)
	}
	binary.BigEndian.PutUint16(b[cs:cs+2], sum(bytes))
	if off == 20 {
		binary.BigEndian.PutUint16(b[10:12], sum(b[:20]))
	}
	return b
}
func TestTailmultiTranslationChecksums(t *testing.T) {
	for _, v6 := range []bool{false, true} {
		host, eff, self, peer := netip.MustParseAddr("198.18.0.1"), netip.MustParseAddr("198.18.0.2"), netip.MustParseAddr("100.64.0.1"), netip.MustParseAddr("100.64.0.2")
		protos := []byte{6, 17, 1}
		if v6 {
			host = netip.MustParseAddr("fd74:6169:6c6d::1")
			eff = host.Next()
			self = netip.MustParseAddr("fd7a:115c:a1e0::1")
			peer = self.Next()
			protos = []byte{6, 17, 58}
		}
		table := Table{Destinations: new(bart.Table[Destination]), Sources: map[SourceKey]Source{{ProfileID: "home", IPv6: v6}: {HostIP: host, CanonicalIP: self}}, InboundPeers: map[string]*bart.Table[netip.Addr]{"home": new(bart.Table[netip.Addr])}}
		table.Destinations.Insert(netip.PrefixFrom(eff, eff.BitLen()), Destination{ProfileID: "home", CanonicalIP: peer})
		table.InboundPeers["home"].Insert(netip.PrefixFrom(peer, peer.BitLen()), eff)
		m := New(table)
		check := func(p []byte, proto byte) {
			t.Helper()
			off := 20
			if v6 {
				off = 40
			} else if sum(p[:20]) != 0 {
				t.Fatal("bad IPv4 checksum")
			}
			data := p[off:]
			if proto != 1 {
				data = append(pseudo(p, proto), data...)
			}
			if sum(data) != 0 {
				t.Fatalf("invalid transport checksum ipv6=%t proto=%d", v6, proto)
			}
		}
		for _, proto := range protos {
			out, _, err := m.Outbound(transportPacket(host, eff, proto))
			if err != nil {
				t.Fatal(err)
			}
			check(out, proto)
			in, err := m.Inbound("home", transportPacket(peer, self, proto))
			if err != nil {
				t.Fatal(err)
			}
			check(in, proto)
		}
		if !v6 {
			b := transportPacket(host, eff, 17)
			b[26], b[27] = 0, 0
			out, _, err := m.Outbound(b)
			if err != nil || out[26] != 0 || out[27] != 0 {
				t.Fatal("UDP disabled checksum changed")
			}
		}
	}
}
func TestTailmultiRejectMalformedLengthsAndFragments(t *testing.T) {
	valid := transportPacket(netip.MustParseAddr("198.18.0.1"), netip.MustParseAddr("198.18.0.2"), 6)
	for _, mutate := range []func([]byte){func(b []byte) { b[6] = 0x20 }, func(b []byte) { b[2], b[3] = 0, 10 }, func(b []byte) { b[32] = 0x10 }, func(b []byte) { b[9] = 99 }} {
		b := append([]byte(nil), valid...)
		mutate(b)
		if validatePacket(b) == nil {
			t.Fatal("malformed packet accepted")
		}
	}
}
