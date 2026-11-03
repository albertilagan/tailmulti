package names

import (
	"context"
	"errors"
	"net/netip"
	"sort"
	"strings"
	"time"

	"golang.org/x/net/dns/dnsmessage"
	"tailmulti.local/engine/internal/routing"
)

type ExchangeFunc func(context.Context, []byte) ([]byte, error)
type ClaimFunc func(context.Context, []string, string) error
type forwardConfig struct {
	ctx      context.Context
	exchange ExchangeFunc
	claim    ClaimFunc
}

// EnableForwarding is called before installing the system default resolver.
// Claim must durably protect names on the host before acknowledging success.
func (r *Resolver) EnableForwarding(ctx context.Context, exchange ExchangeFunc, claim ClaimFunc) {
	r.forward.Store(&forwardConfig{ctx, exchange, claim})
}

var errUnsafeAnswer = errors.New("DNS answer cannot be assigned to one active account")

func replyError(q *dnsmessage.Message, code dnsmessage.RCode) []byte {
	m := dnsmessage.Message{Header: dnsmessage.Header{ID: q.ID, Response: true, RecursionDesired: q.RecursionDesired, RecursionAvailable: true, RCode: code}, Questions: q.Questions}
	b, _ := m.Pack()
	return b
}

func validResponse(q, a *dnsmessage.Message) bool {
	return a.Response && a.ID == q.ID && a.OpCode == q.OpCode && len(a.Questions) == 1 && a.Questions[0].Type == q.Questions[0].Type && a.Questions[0].Class == q.Questions[0].Class && strings.EqualFold(a.Questions[0].Name.String(), q.Questions[0].Name.String())
}

func (r *Resolver) forwardReply(parent context.Context, data []byte, q *dnsmessage.Message, f *forwardConfig) []byte {
	fail := func() []byte { return replyError(q, dnsmessage.RCodeServerFailure) }
	name := routing.Name(q.Questions[0].Name.String())
	v := r.view.Load()
	if v == nil || f.exchange == nil || f.ctx.Err() != nil {
		return fail()
	}
	if owner := v.Aliases[name]; owner != "" && !v.ActiveProfiles[owner] {
		return fail()
	}
	ctx, cancel := context.WithTimeout(parent, 4*time.Second)
	defer cancel()
	stop := context.AfterFunc(f.ctx, cancel)
	defer stop()
	raw, err := f.exchange(ctx, data)
	if err != nil || ctx.Err() != nil {
		return fail()
	}
	v = r.view.Load()
	b, owner, claims, err := translate(v, q, raw)
	if err != nil {
		return fail()
	}
	if owner == "" {
		if r.view.Load() != v {
			return fail()
		}
		return b
	}
	if f.claim == nil || f.claim(ctx, claims, owner) != nil || ctx.Err() != nil {
		return fail()
	}
	// Re-evaluate the original answer after the durable claim. A disconnect or
	// newly discovered collision while saving must never release a stale answer.
	v = r.view.Load()
	b, currentOwner, currentClaims, err := translate(v, q, raw)
	if err != nil || currentOwner != owner {
		return fail()
	}
	for _, n := range currentClaims {
		if v.Aliases[n] != owner && v.NameOwners[n] != owner {
			return fail()
		}
	}
	if r.view.Load() != v {
		return fail()
	}
	return b
}

// translate preserves untouched public responses. Rebuilt private answers include
// only the checked CNAME chain and address records; DNSSEC proofs no longer apply.
func translate(v *View, q *dnsmessage.Message, raw []byte) ([]byte, string, []string, error) {
	var a dnsmessage.Message
	if v == nil || len(raw) > 65535 || a.Unpack(raw) != nil || !validResponse(q, &a) || a.Truncated {
		return nil, "", nil, errUnsafeAnswer
	}
	name := routing.Name(q.Questions[0].Name.String())
	knownOwner := v.Aliases[name]
	if knownOwner != "" && !v.ActiveProfiles[knownOwner] {
		return nil, "", nil, errUnsafeAnswer
	}
	chain := map[string]bool{name: true}
	ordered := []string{name}
	var links []dnsmessage.Resource
	current := name
	for depth := 0; ; depth++ {
		var next string
		var link dnsmessage.Resource
		for _, rr := range a.Answers {
			if routing.Name(rr.Header.Name.String()) != current {
				continue
			}
			if c, ok := rr.Body.(*dnsmessage.CNAMEResource); ok {
				n := routing.Name(c.CNAME.String())
				if n == "" || (next != "" && next != n) {
					return nil, "", nil, errUnsafeAnswer
				}
				next, link = n, rr
			}
		}
		if next == "" {
			break
		}
		if depth >= 15 || chain[next] {
			return nil, "", nil, errUnsafeAnswer
		}
		chain[next] = true
		ordered = append(ordered, next)
		links = append(links, link)
		current = next
	}
	owner := ""
	for _, n := range ordered {
		if previous := v.Aliases[n]; previous != "" {
			if direct, known := v.NameOwners[n]; known && direct != previous {
				return nil, "", nil, errUnsafeAnswer
			}
		}
		if previous := v.Aliases[n]; previous != "" && (!v.ActiveProfiles[previous] || (knownOwner != "" && knownOwner != previous)) {
			return nil, "", nil, errUnsafeAnswer
		}
		for _, domain := range v.Domains {
			if n == domain || strings.HasSuffix(n, "."+domain) {
				if v.Aliases[n] == "" && len(v.Records[n]) == 0 {
					return nil, "", nil, errUnsafeAnswer
				}
			}
		}
	}
	assign := func(id string) bool {
		if id == "" || !v.ActiveProfiles[id] || (knownOwner != "" && knownOwner != id) || (owner != "" && owner != id) {
			return false
		}
		owner = id
		return true
	}
	var addresses []dnsmessage.Resource
	modified, privateHint, publicAddress := false, false, false
	for _, rr := range a.Answers {
		ip, isIP := resourceIP(rr)
		if isIP && (routing.IsCanonical(ip) || routing.IsSynthetic(ip)) {
			if a.RCode != dnsmessage.RCodeSuccess {
				return nil, "", nil, errUnsafeAnswer
			}
			if !chain[routing.Name(rr.Header.Name.String())] || routing.IsSynthetic(ip) || rr.Header.Class != dnsmessage.ClassINET {
				return nil, "", nil, errUnsafeAnswer
			}
			target, ok := v.Canonical[ip]
			if !ok || !target.Active || target.Ambiguous || !assign(target.ProfileID) {
				return nil, "", nil, errUnsafeAnswer
			}
			if rr.Header.Type != q.Questions[0].Type {
				return nil, "", nil, errUnsafeAnswer
			}
			rr.Header.TTL = 5
			if ip.Is4() {
				rr.Body = &dnsmessage.AResource{A: target.EffectiveIP.As4()}
			} else {
				rr.Body = &dnsmessage.AAAAResource{AAAA: target.EffectiveIP.As16()}
			}
			addresses = append(addresses, rr)
			modified = true
		} else if isIP && chain[routing.Name(rr.Header.Name.String())] {
			publicAddress = true
		}
		if rr.Header.Type == dnsmessage.Type(64) || rr.Header.Type == dnsmessage.Type(65) {
			unsafe, err := privateHints(rr)
			if err != nil {
				return nil, "", nil, err
			}
			privateHint = privateHint || unsafe
		}
	}
	// Public recursive resolvers can return NXDOMAIN after a CNAME to MagicDNS.
	// Finish only that exact chain using our existing authoritative peer record.
	if len(links) > 0 && len(addresses) == 0 && len(v.Records[current]) > 0 && (q.Questions[0].Type == dnsmessage.TypeA || q.Questions[0].Type == dnsmessage.TypeAAAA) {
		if a.RCode != dnsmessage.RCodeSuccess && a.RCode != dnsmessage.RCodeNameError {
			return nil, "", nil, errUnsafeAnswer
		}
		if !assign(v.NameOwners[current]) {
			return nil, "", nil, errUnsafeAnswer
		}
		for _, ip := range v.Records[current] {
			rr := dnsmessage.Resource{Header: dnsmessage.ResourceHeader{Name: dnsmessage.MustNewName(current + "."), Class: dnsmessage.ClassINET, Type: q.Questions[0].Type, TTL: 5}}
			if ip.Is4() && rr.Header.Type == dnsmessage.TypeA {
				rr.Body = &dnsmessage.AResource{A: ip.As4()}
			} else if ip.Is6() && rr.Header.Type == dnsmessage.TypeAAAA {
				rr.Body = &dnsmessage.AAAAResource{AAAA: ip.As16()}
			} else {
				continue
			}
			addresses = append(addresses, rr)
		}
		modified = true
	}
	for _, section := range [][]dnsmessage.Resource{a.Authorities, a.Additionals} {
		for _, rr := range section {
			if ip, ok := resourceIP(rr); ok && (routing.IsCanonical(ip) || routing.IsSynthetic(ip)) {
				modified = true
			}
			if rr.Header.Type == dnsmessage.Type(64) || rr.Header.Type == dnsmessage.Type(65) {
				bad, err := privateHints(rr)
				if err != nil {
					return nil, "", nil, err
				}
				modified = modified || bad
			}
		}
	}
	if owner != "" && publicAddress {
		return nil, "", nil, errUnsafeAnswer
	}
	for _, n := range ordered {
		if previous := v.Aliases[n]; previous != "" && (owner == "" || previous != owner) {
			// A known private alias may yield NODATA, but may not move to a public IP.
			if publicAddress || owner != "" || !v.ActiveProfiles[previous] {
				return nil, "", nil, errUnsafeAnswer
			}
		}
	}
	if knownOwner != "" && publicAddress {
		return nil, "", nil, errUnsafeAnswer
	}
	if privateHint || ((knownOwner != "" || len(v.Records[current]) > 0) && (q.Questions[0].Type == dnsmessage.Type(64) || q.Questions[0].Type == dnsmessage.Type(65))) {
		// Address hints bypass A/AAAA translation. NODATA makes clients ask A/AAAA.
		a.Answers = nil
		a.Authorities = nil
		a.Additionals = nil
		a.AuthenticData = false
		a.RCode = 0
		b, e := a.Pack()
		return b, "", nil, e
	}
	if !modified {
		return raw, "", nil, nil
	}
	if owner != "" {
		for i := range links {
			links[i].Header.TTL = 5
		}
		a.Answers = append(links, addresses...)
		a.RCode = 0
	} else {
		// Auxiliary private addresses are never exposed as unmodified glue.
		for _, rr := range a.Answers {
			if _, ok := rr.Body.(*dnsmessage.UnknownResource); ok {
				return nil, "", nil, errUnsafeAnswer
			}
		}
	}
	a.Authorities = nil
	a.Additionals = nil
	a.AuthenticData = false
	a.Authoritative = false
	b, err := a.Pack()
	if err != nil {
		return nil, "", nil, err
	}
	sort.Strings(ordered)
	return b, owner, ordered, nil
}

func resourceIP(rr dnsmessage.Resource) (netip.Addr, bool) {
	switch b := rr.Body.(type) {
	case *dnsmessage.AResource:
		return netip.AddrFrom4(b.A), true
	case *dnsmessage.AAAAResource:
		return netip.AddrFrom16(b.AAAA), true
	}
	return netip.Addr{}, false
}

func privateHints(rr dnsmessage.Resource) (bool, error) {
	var svc *dnsmessage.SVCBResource
	switch body := rr.Body.(type) {
	case *dnsmessage.HTTPSResource:
		svc = &body.SVCBResource
	case *dnsmessage.SVCBResource:
		svc = body
	default:
		return false, errUnsafeAnswer
	}
	if ip, err := netip.ParseAddr(strings.TrimSuffix(svc.Target.String(), ".")); err == nil && (routing.IsCanonical(ip) || routing.IsSynthetic(ip)) {
		return true, nil
	}
	for _, param := range svc.Params {
		width := 0
		if param.Key == dnsmessage.SVCParamIPv4Hint {
			width = 4
		}
		if param.Key == dnsmessage.SVCParamIPv6Hint {
			width = 16
		}
		if width == 0 {
			continue
		}
		if len(param.Value) == 0 || len(param.Value)%width != 0 {
			return false, errUnsafeAnswer
		}
		for i := 0; i < len(param.Value); i += width {
			ip, ok := netip.AddrFromSlice(param.Value[i : i+width])
			if !ok {
				return false, errUnsafeAnswer
			}
			if routing.IsCanonical(ip) || routing.IsSynthetic(ip) {
				return true, nil
			}
		}
	}
	return false, nil
}
