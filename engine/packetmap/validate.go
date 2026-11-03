package packetmap

import (
	"encoding/binary"
	"errors"
)

// Tailmulti accepts TCP, UDP and ICMP echo only. Fragments and extension headers
// require flow-aware translation, including quoted IPs in ICMP errors, and are
// deliberately refused until implemented. Check lengths before checksum mutation.
func validatePacket(p []byte) error {
	bad := errors.New("malformed or unsupported packet")
	if len(p) == 0 {
		return bad
	}
	off, proto := 0, byte(0)
	switch p[0] >> 4 {
	case 4:
		if len(p) < 20 {
			return bad
		}
		off = int(p[0]&15) * 4
		if off != 20 || int(binary.BigEndian.Uint16(p[2:4])) != len(p) || binary.BigEndian.Uint16(p[6:8])&0x3fff != 0 {
			return bad
		}
		proto = p[9]
	case 6:
		if len(p) < 40 || int(binary.BigEndian.Uint16(p[4:6]))+40 != len(p) {
			return bad
		}
		off = 40
		proto = p[6]
	default:
		return bad
	}
	switch proto {
	case 6:
		if len(p) < off+20 || int(p[off+12]>>4)*4 < 20 || int(p[off+12]>>4)*4 > len(p)-off {
			return bad
		}
	case 17:
		if len(p) < off+8 || int(binary.BigEndian.Uint16(p[off+4:off+6])) != len(p)-off {
			return bad
		}
	case 1:
		if p[0]>>4 != 4 || len(p) < off+8 || (p[off] != 0 && p[off] != 8) {
			return bad
		}
	case 58:
		if p[0]>>4 != 6 || len(p) < off+8 || (p[off] != 128 && p[off] != 129) {
			return bad
		}
	default:
		return bad
	}
	return nil
}

// Tailscale's incremental checksum helper updates even a disabled UDPv4
// checksum. Preserve that sentinel and encode a computed zero as all ones.
func preserveUDPChecksum(p []byte) func() {
	off, proto := 20, p[9]
	if p[0]>>4 == 6 {
		off, proto = 40, p[6]
	}
	if proto != 17 {
		return func() {}
	}
	absent := p[0]>>4 == 4 && p[off+6] == 0 && p[off+7] == 0
	return func() {
		if absent {
			p[off+6], p[off+7] = 0, 0
		} else if p[off+6] == 0 && p[off+7] == 0 {
			p[off+6], p[off+7] = 255, 255
		}
	}
}
