package names

import (
	"bytes"
	"context"
	"errors"
	"net/netip"
	"testing"

	"golang.org/x/net/dns/dnsmessage"
	"tailmulti.local/engine/internal/routing"
)

func translationPlan() *routing.Plan {
	return &routing.Plan{Records: map[string][]netip.Addr{}, Canonical: map[netip.Addr]routing.CanonicalTarget{
		netip.MustParseAddr("100.112.102.98"):     {ProfileID: "work", NodeID: "proxy", EffectiveIP: netip.MustParseAddr("198.18.0.104"), Active: true},
		netip.MustParseAddr("fd7a:115c:a1e0::98"): {ProfileID: "work", NodeID: "proxy", EffectiveIP: netip.MustParseAddr("fd74:6169:6c6d::68"), Active: true},
	}, Aliases: map[string]string{}, NameOwners: map[string]string{}, ActiveProfiles: map[string]bool{"work": true, "home": true}}
}
func addressRR(name, ip string) dnsmessage.Resource {
	a := netip.MustParseAddr(ip)
	h := dnsmessage.ResourceHeader{Name: dnsmessage.MustNewName(name), Class: dnsmessage.ClassINET, TTL: 300}
	if a.Is4() {
		h.Type = dnsmessage.TypeA
		return dnsmessage.Resource{Header: h, Body: &dnsmessage.AResource{A: a.As4()}}
	}
	h.Type = dnsmessage.TypeAAAA
	return dnsmessage.Resource{Header: h, Body: &dnsmessage.AAAAResource{AAAA: a.As16()}}
}
func cnameRR(name, target string) dnsmessage.Resource {
	return dnsmessage.Resource{Header: dnsmessage.ResourceHeader{Name: dnsmessage.MustNewName(name), Class: dnsmessage.ClassINET, Type: dnsmessage.TypeCNAME, TTL: 300}, Body: &dnsmessage.CNAMEResource{CNAME: dnsmessage.MustNewName(target)}}
}
func answerFor(q []byte, rr ...dnsmessage.Resource) []byte {
	var m dnsmessage.Message
	_ = m.Unpack(q)
	m.Response = true
	m.RecursionAvailable = true
	m.AuthenticData = true
	m.Answers = rr
	b, _ := m.Pack()
	return b
}
func decodeReply(t *testing.T, b []byte) dnsmessage.Message {
	t.Helper()
	var m dnsmessage.Message
	if err := m.Unpack(b); err != nil {
		t.Fatal(err)
	}
	return m
}

func TestAutomaticAddressTranslationAndPublicPassthrough(t *testing.T) {
	for _, tc := range []struct {
		ip, want string
		typ      dnsmessage.Type
	}{{"100.112.102.98", "198.18.0.104", dnsmessage.TypeA}, {"fd7a:115c:a1e0::98", "fd74:6169:6c6d::68", dnsmessage.TypeAAAA}, {"192.0.2.20", "192.0.2.20", dnsmessage.TypeA}} {
		t.Run(tc.ip, func(t *testing.T) {
			q := query("app.example.com.", tc.typ)
			raw := answerFor(q, addressRR("app.example.com.", tc.ip))
			p := translationPlan()
			r := new(Resolver)
			r.Set(p)
			claimed := false
			r.EnableForwarding(context.Background(), func(context.Context, []byte) ([]byte, error) { return raw, nil }, func(_ context.Context, names []string, owner string) error {
				if owner != "work" || len(names) != 1 || names[0] != "app.example.com" {
					t.Fatal("wrong claim")
				}
				claimed = true
				p.Aliases = map[string]string{names[0]: owner}
				p.Domains = names
				r.Set(p)
				return nil
			})
			b := r.Reply(q)
			a := decodeReply(t, b)
			if a.RCode != 0 || len(a.Answers) != 1 {
				t.Fatalf("missing answer: %+v", a)
			}
			ip, _ := resourceIP(a.Answers[0])
			if ip.String() != tc.want {
				t.Fatal("wrong mapping", ip)
			}
			if tc.ip == tc.want {
				if claimed || !bytes.Equal(b, raw) {
					t.Fatal("public DNS changed")
				}
			} else if !claimed || a.AuthenticData || a.Answers[0].Header.TTL != 5 {
				t.Fatal("unsafe translated metadata")
			}
		})
	}
}

func TestUnsafeCustomAnswersAndLateDisconnect(t *testing.T) {
	for _, kind := range []string{"unknown", "collision", "disconnected", "wrong account", "new direct owner", "ambiguous direct name", "public reassignment", "mixed public", "mixed accounts", "synthetic injection", "mapped IPv4", "bad ID", "claim failure", "late disconnect", "disconnected target"} {
		t.Run(kind, func(t *testing.T) {
			q := query("app.example.com.", dnsmessage.TypeA)
			p := translationPlan()
			ip := netip.MustParseAddr("100.112.102.98")
			rr := []dnsmessage.Resource{addressRR("app.example.com.", ip.String())}
			switch kind {
			case "unknown":
				delete(p.Canonical, ip)
			case "collision":
				target := p.Canonical[ip]
				target.Ambiguous = true
				p.Canonical[ip] = target
			case "disconnected":
				p.ActiveProfiles["work"] = false
				p.Aliases["app.example.com"] = "work"
			case "wrong account":
				p.Aliases["app.example.com"] = "home"
			case "new direct owner":
				p.Aliases["app.example.com"] = "work"
				p.NameOwners["app.example.com"] = "home"
			case "ambiguous direct name":
				p.Aliases["app.example.com"] = "work"
				p.NameOwners["app.example.com"] = ""
			case "public reassignment":
				p.Aliases["app.example.com"] = "work"
				rr = []dnsmessage.Resource{addressRR("app.example.com.", "192.0.2.20")}
			case "mixed public":
				rr = append(rr, addressRR("app.example.com.", "192.0.2.20"))
			case "mixed accounts":
				other := netip.MustParseAddr("100.64.0.3")
				p.Canonical[other] = routing.CanonicalTarget{ProfileID: "home", EffectiveIP: netip.MustParseAddr("198.18.0.3"), Active: true}
				rr = append(rr, addressRR("app.example.com.", other.String()))
			case "synthetic injection":
				rr = []dnsmessage.Resource{addressRR("app.example.com.", "198.18.0.104")}
			case "mapped IPv4":
				q = query("app.example.com.", dnsmessage.TypeAAAA)
				rr = []dnsmessage.Resource{addressRR("app.example.com.", "::ffff:100.112.102.98")}
			case "disconnected target":
				p.Domains = []string{"work.ts.net"}
				rr = []dnsmessage.Resource{cnameRR("app.example.com.", "proxy.work.ts.net."), addressRR("proxy.work.ts.net.", "192.0.2.20")}
			}
			raw := answerFor(q, rr...)
			if kind == "bad ID" {
				raw[0] ^= 1
			}
			r := new(Resolver)
			r.Set(p)
			forwarded := 0
			r.EnableForwarding(context.Background(), func(context.Context, []byte) ([]byte, error) { forwarded++; return raw, nil }, func(_ context.Context, n []string, owner string) error {
				if kind == "claim failure" {
					return errors.New("disk failed")
				}
				if kind == "late disconnect" {
					p.ActiveProfiles = map[string]bool{}
				}
				p.Aliases = map[string]string{n[0]: owner}
				r.Set(p)
				return nil
			})
			if a := decodeReply(t, r.Reply(q)); a.RCode != dnsmessage.RCodeServerFailure || len(a.Answers) != 0 {
				t.Fatal("unsafe answer released", a)
			}
			if kind == "disconnected" && forwarded != 0 {
				t.Fatal("disconnected name forwarded")
			}
		})
	}
}

func TestCNAMETranslationAndMagicDNSTarget(t *testing.T) {
	for _, local := range []bool{false, true} {
		p := translationPlan()
		q := query("app.example.com.", dnsmessage.TypeA)
		rr := []dnsmessage.Resource{cnameRR("app.example.com.", "proxy.work.ts.net.")}
		if local {
			p.Records["proxy.work.ts.net"] = []netip.Addr{netip.MustParseAddr("198.18.0.104")}
			p.NameOwners["proxy.work.ts.net"] = "work"
			p.Domains = []string{"work.ts.net"}
		} else {
			rr = append(rr, addressRR("proxy.work.ts.net.", "100.112.102.98"))
		}
		raw := answerFor(q, rr...)
		var request dnsmessage.Message
		_ = request.Unpack(q)
		v := &View{Records: p.Records, Canonical: p.Canonical, Aliases: p.Aliases, NameOwners: p.NameOwners, Domains: p.Domains, ActiveProfiles: p.ActiveProfiles}
		b, owner, claims, err := translate(v, &request, raw)
		if err != nil || owner != "work" || len(claims) != 2 {
			t.Fatal("chain not claimed", err)
		}
		a := decodeReply(t, b)
		if len(a.Answers) != 2 || a.AuthenticData {
			t.Fatal("bad chain")
		}
		ip, _ := resourceIP(a.Answers[1])
		if ip.String() != "198.18.0.104" {
			t.Fatal("canonical chain escaped")
		}
	}
}

func TestPrivateHTTPSHintsSuppressedAndPublicHintsPreserved(t *testing.T) {
	for _, ip := range []string{"100.112.102.98", "192.0.2.20"} {
		q := query("app.example.com.", dnsmessage.Type(65))
		v := netip.MustParseAddr(ip).As4()
		data := append([]byte{0, 1, 0, 0, 4, 0, 4}, v[:]...)
		rr := dnsmessage.Resource{Header: dnsmessage.ResourceHeader{Name: dnsmessage.MustNewName("app.example.com."), Class: dnsmessage.ClassINET, Type: dnsmessage.Type(65), TTL: 300}, Body: &dnsmessage.UnknownResource{Type: dnsmessage.Type(65), Data: data}}
		raw := answerFor(q, rr)
		var request dnsmessage.Message
		_ = request.Unpack(q)
		b, owner, _, err := translate(&View{}, &request, raw)
		if err != nil || owner != "" {
			t.Fatal(err)
		}
		a := decodeReply(t, b)
		if ip == "100.112.102.98" {
			if len(a.Answers) != 0 || a.AuthenticData {
				t.Fatal("private hint escaped")
			}
		} else if !bytes.Equal(b, raw) {
			t.Fatal("public HTTPS changed")
		}
	}
}

func FuzzTranslate(f *testing.F) {
	q := query("app.example.com.", dnsmessage.TypeA)
	f.Add(answerFor(q, addressRR("app.example.com.", "100.112.102.98")))
	p := translationPlan()
	r := new(Resolver)
	r.Set(p)
	var request dnsmessage.Message
	_ = request.Unpack(q)
	f.Fuzz(func(t *testing.T, b []byte) { translate(r.view.Load(), &request, b) })
}

func TestOrdinarySRVAndTXTQueriesRemainUnchanged(t *testing.T) {
	for _, name := range []string{"_service._tcp.example.com.", "_dmarc.example.com."} {
		q := query(name, dnsmessage.TypeTXT)
		raw := answerFor(q, dnsmessage.Resource{Header: dnsmessage.ResourceHeader{Name: dnsmessage.MustNewName(name), Class: dnsmessage.ClassINET, Type: dnsmessage.TypeTXT, TTL: 60}, Body: &dnsmessage.TXTResource{TXT: []string{"ordinary-data"}}})
		r := new(Resolver)
		r.Set(translationPlan())
		r.EnableForwarding(context.Background(), func(context.Context, []byte) ([]byte, error) { return raw, nil }, nil)
		if !bytes.Equal(r.Reply(q), raw) {
			t.Fatal("ordinary non-host DNS query changed")
		}
	}
}

func TestMalformedChainsHintsAndPrivateGlue(t *testing.T) {
	for _, kind := range []string{"cycle", "long chain", "unknown private glue", "malformed hint", "private hint v6", "signed translation"} {
		t.Run(kind, func(t *testing.T) {
			q := query("app.example.com.", dnsmessage.TypeA)
			rr := []dnsmessage.Resource{addressRR("app.example.com.", "192.0.2.20")}
			switch kind {
			case "cycle":
				rr = []dnsmessage.Resource{cnameRR("app.example.com.", "other.example.com."), cnameRR("other.example.com.", "app.example.com.")}
			case "long chain":
				rr = nil
				previous := "app.example.com."
				for i := 0; i < 17; i++ {
					next := string(rune('a'+i)) + ".example.com."
					rr = append(rr, cnameRR(previous, next))
					previous = next
				}
			case "malformed hint", "private hint v6":
				q = query("app.example.com.", dnsmessage.TypeHTTPS)
				hint := []byte{1, 2, 3}
				key := dnsmessage.SVCParamIPv4Hint
				if kind == "private hint v6" {
					ip := netip.MustParseAddr("fd7a:115c:a1e0::98").As16()
					hint = ip[:]
					key = dnsmessage.SVCParamIPv6Hint
				}
				rr = []dnsmessage.Resource{{Header: dnsmessage.ResourceHeader{Name: dnsmessage.MustNewName("app.example.com."), Class: dnsmessage.ClassINET, Type: dnsmessage.TypeHTTPS, TTL: 60}, Body: &dnsmessage.HTTPSResource{SVCBResource: dnsmessage.SVCBResource{Priority: 1, Target: dnsmessage.MustNewName("."), Params: []dnsmessage.SVCParam{{Key: key, Value: hint}}}}}}
			case "signed translation":
				rr = []dnsmessage.Resource{addressRR("app.example.com.", "100.112.102.98"), {Header: dnsmessage.ResourceHeader{Name: dnsmessage.MustNewName("app.example.com."), Class: dnsmessage.ClassINET, Type: dnsmessage.Type(46), TTL: 60}, Body: &dnsmessage.UnknownResource{Type: dnsmessage.Type(46), Data: []byte{1, 2, 3}}}}
			}
			raw := answerFor(q, rr...)
			if kind == "unknown private glue" {
				var a dnsmessage.Message
				_ = a.Unpack(raw)
				a.Additionals = []dnsmessage.Resource{addressRR("unrelated.example.com.", "100.64.0.200")}
				raw, _ = a.Pack()
			}
			p := translationPlan()
			r := new(Resolver)
			r.Set(p)
			var request dnsmessage.Message
			_ = request.Unpack(q)
			b, owner, _, err := translate(r.view.Load(), &request, raw)
			if kind == "cycle" || kind == "long chain" || kind == "malformed hint" {
				if err == nil {
					t.Fatal("malformed answer accepted")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			a := decodeReply(t, b)
			if a.AuthenticData || len(a.Additionals) != 0 {
				t.Fatal("unsafe auxiliary data survived")
			}
			if kind == "private hint v6" && len(a.Answers) != 0 {
				t.Fatal("IPv6 hint survived")
			}
			if kind == "signed translation" && (owner != "work" || len(a.Answers) != 1) {
				t.Fatal("DNSSEC proof survived rewrite")
			}
		})
	}
}
