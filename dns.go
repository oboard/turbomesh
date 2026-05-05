package main

import (
	"fmt"
	"net"
	"strings"
	"time"

	"github.com/miekg/dns"
)

const defaultTTL = 60

type Zone struct {
	Domain   string
	NSHost   string
	PublicIP net.IP
}

type DNSServer struct {
	Network string
	Addr    string
	server  *dns.Server
}

func NewZone(domain string, publicIP net.IP) (*Zone, error) {
	zone := dns.Fqdn(strings.ToLower(strings.TrimSpace(domain)))
	if zone == "." {
		return nil, fmt.Errorf("domain is required")
	}
	ip := publicIP.To4()
	if ip == nil {
		return nil, fmt.Errorf("public IP must be IPv4")
	}
	return &Zone{
		Domain:   zone,
		NSHost:   "ns1." + zone,
		PublicIP: append(net.IP(nil), ip...),
	}, nil
}

func NewDNSServer(network, addr string, zone *Zone) *DNSServer {
	mux := dns.NewServeMux()
	mux.Handle(zone.Domain, zone)
	server := &dns.Server{Addr: addr, Net: network, Handler: mux}
	return &DNSServer{Network: network, Addr: addr, server: server}
}

func (s *DNSServer) ListenAndServe() error {
	return s.server.ListenAndServe()
}

func (s *DNSServer) Shutdown() error {
	return s.server.Shutdown()
}

func (z *Zone) ServeDNS(w dns.ResponseWriter, r *dns.Msg) {
	m := new(dns.Msg)
	m.SetReply(r)
	m.Authoritative = true

	if len(r.Question) == 0 {
		_ = w.WriteMsg(m)
		return
	}

	for _, q := range r.Question {
		name := dns.Fqdn(strings.ToLower(q.Name))
		if !z.inZone(name) {
			m.Rcode = dns.RcodeNameError
			continue
		}

		switch q.Qtype {
		case dns.TypeA:
			if z.isWildcardAName(name) {
				m.Answer = append(m.Answer, z.aRecord(name))
			}
		case dns.TypeNS:
			if name == z.Domain {
				m.Answer = append(m.Answer, z.nsRecord())
			}
		case dns.TypeSOA:
			if name == z.Domain {
				m.Answer = append(m.Answer, z.soaRecord())
			}
		case dns.TypeANY:
			if z.isWildcardAName(name) {
				m.Answer = append(m.Answer, z.aRecord(name))
			}
			if name == z.Domain {
				m.Answer = append(m.Answer, z.nsRecord(), z.soaRecord())
			}
		}

		if name != z.Domain {
			m.Ns = append(m.Ns, z.nsRecord())
		}
	}

	_ = w.WriteMsg(m)
}

func (z *Zone) inZone(name string) bool {
	return name == z.Domain || strings.HasSuffix(name, "."+z.Domain)
}

func (z *Zone) isWildcardAName(name string) bool {
	return name == z.Domain || name == z.NSHost || strings.HasSuffix(name, "."+z.Domain)
}

func (z *Zone) aRecord(name string) dns.RR {
	return &dns.A{
		Hdr: dns.RR_Header{Name: name, Rrtype: dns.TypeA, Class: dns.ClassINET, Ttl: defaultTTL},
		A:   z.PublicIP,
	}
}

func (z *Zone) nsRecord() dns.RR {
	return &dns.NS{
		Hdr: dns.RR_Header{Name: z.Domain, Rrtype: dns.TypeNS, Class: dns.ClassINET, Ttl: defaultTTL},
		Ns:  z.NSHost,
	}
}

func (z *Zone) soaRecord() dns.RR {
	now := uint32(time.Now().Unix() / 60)
	return &dns.SOA{
		Hdr:     dns.RR_Header{Name: z.Domain, Rrtype: dns.TypeSOA, Class: dns.ClassINET, Ttl: defaultTTL},
		Ns:      z.NSHost,
		Mbox:    "hostmaster." + z.Domain,
		Serial:  now,
		Refresh: 300,
		Retry:   120,
		Expire:  3600,
		Minttl:  defaultTTL,
	}
}
