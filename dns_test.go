package main

import (
	"net"
	"testing"

	"github.com/miekg/dns"
)

func TestZoneAnswersAForBaseNSAndWildcard(t *testing.T) {
	zone, err := NewZone("web.oboard.fun", net.ParseIP("203.0.113.10"))
	if err != nil {
		t.Fatal(err)
	}

	for _, name := range []string{"web.oboard.fun.", "ns1.web.oboard.fun.", "abc123.web.oboard.fun."} {
		msg := queryZone(t, zone, name, dns.TypeA)
		if len(msg.Answer) != 1 {
			t.Fatalf("%s: expected one A answer, got %d", name, len(msg.Answer))
		}
		a, ok := msg.Answer[0].(*dns.A)
		if !ok {
			t.Fatalf("%s: expected A answer, got %T", name, msg.Answer[0])
		}
		if got := a.A.String(); got != "203.0.113.10" {
			t.Fatalf("%s: got %s", name, got)
		}
	}
}

func TestZoneAnswersNSAndSOA(t *testing.T) {
	zone, err := NewZone("web.oboard.fun", net.ParseIP("203.0.113.10"))
	if err != nil {
		t.Fatal(err)
	}

	ns := queryZone(t, zone, "web.oboard.fun.", dns.TypeNS)
	if len(ns.Answer) != 1 {
		t.Fatalf("expected one NS answer, got %d", len(ns.Answer))
	}
	if got := ns.Answer[0].(*dns.NS).Ns; got != "ns1.web.oboard.fun." {
		t.Fatalf("unexpected NS target %s", got)
	}

	soa := queryZone(t, zone, "web.oboard.fun.", dns.TypeSOA)
	if len(soa.Answer) != 1 {
		t.Fatalf("expected one SOA answer, got %d", len(soa.Answer))
	}
	if got := soa.Answer[0].(*dns.SOA).Ns; got != "ns1.web.oboard.fun." {
		t.Fatalf("unexpected SOA ns %s", got)
	}
}

func TestZoneRejectsOutOfZoneName(t *testing.T) {
	zone, err := NewZone("web.oboard.fun", net.ParseIP("203.0.113.10"))
	if err != nil {
		t.Fatal(err)
	}

	msg := queryZone(t, zone, "example.com.", dns.TypeA)
	if msg.Rcode != dns.RcodeNameError {
		t.Fatalf("expected NXDOMAIN, got %s", dns.RcodeToString[msg.Rcode])
	}
}

func queryZone(t *testing.T, zone *Zone, name string, qtype uint16) *dns.Msg {
	t.Helper()
	req := new(dns.Msg)
	req.SetQuestion(name, qtype)
	rec := &dnsResponseRecorder{}
	zone.ServeDNS(rec, req)
	if rec.msg == nil {
		t.Fatal("zone did not write response")
	}
	return rec.msg
}

type dnsResponseRecorder struct {
	msg *dns.Msg
}

func (r *dnsResponseRecorder) LocalAddr() net.Addr         { return &net.IPAddr{} }
func (r *dnsResponseRecorder) RemoteAddr() net.Addr        { return &net.IPAddr{} }
func (r *dnsResponseRecorder) WriteMsg(msg *dns.Msg) error { r.msg = msg; return nil }
func (r *dnsResponseRecorder) Write([]byte) (int, error)   { return 0, nil }
func (r *dnsResponseRecorder) Close() error                { return nil }
func (r *dnsResponseRecorder) TsigStatus() error           { return nil }
func (r *dnsResponseRecorder) TsigTimersOnly(bool)         {}
func (r *dnsResponseRecorder) Hijack()                     {}
func (r *dnsResponseRecorder) Msg() *dns.Msg               { return r.msg }
func (r *dnsResponseRecorder) SetMsg(msg *dns.Msg)         { r.msg = msg }
