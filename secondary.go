package secondary

import (
	"context"
	"github.com/coredns/coredns/plugin"
	clog "github.com/coredns/coredns/plugin/pkg/log"
	"github.com/coredns/coredns/request"
	"github.com/miekg/dns"
)

const name = "secondary"

var log = clog.NewWithPlugin("secondary")

func (s *Secondary) Name() string {
	return name
}

type TransferPersistence interface {
	Name() string
	Persist(zone string, records []*dns.RR) error
	RetrieveSOA(zoneName string) *dns.SOA
}

type Secondary struct {
	Primaries  []string
	Persistors []TransferPersistence
	Next       plugin.Handler
}

func New() (s *Secondary) {
	return &Secondary{}
}

func (s *Secondary) ServeDNS(ctx context.Context, w dns.ResponseWriter, r *dns.Msg) (int, error) {

	state := request.Request{W: w, Req: r}

	// if the query isn't a notify pass it along
	if state.Req.Opcode != dns.OpcodeNotify {
		log.Debugf("the opcode isn't a notification; moving the query along to the next plugin, %s", r.Question[0].String())
		return plugin.NextOrFailure(s.Name(), s.Next, ctx, w, r)
	}

	log.Debugf("received notify question; %s", r.Question[0].String())

	// write the reply to NOTIFY
	m := new(dns.Msg)
	m.SetReply(r)
	_ = w.WriteMsg(m)

	// retrieve existing SOA record for zone
	var knownSOA *dns.SOA
	for _, p := range s.Persistors {
		knownSOA = p.RetrieveSOA(state.QName())
		if knownSOA != nil {
			break
		}
	}

	// determine if the zone should be transfer
	var shouldTransfer bool
	if knownSOA != nil {
		should, err := s.ShouldTransfer(state.QName(), knownSOA)
		shouldTransfer = should && err != nil
	}

	// retrieved changed records
	var records []*dns.RR
	if shouldTransfer {
		records = s.TransferIn(state.QName(), knownSOA)
	}

	// persist retrieved records from primary
	if records != nil {
		for _, p := range s.Persistors {
			err := p.Persist(state.QName(), records)
			if err != nil {
				log.Error("the was error persisting zone records for zone %s to persistence %s with error message: %s", state.QName(), p.Name(), err.Error())
			}
		}
	}

	return dns.RcodeSuccess, nil
}

func (s *Secondary) TransferIn(zoneName string, knownSOA *dns.SOA) (records []*dns.RR) {
	m := new(Msg)

	if knownSOA != nil {
		m.SetIxfr2(zoneName, knownSOA)
	} else {
		m.SetAxfr(zoneName)
	}

	t := new(dns.Transfer)

	for _, p := range s.Primaries {
		c, err := t.In(m.Msg, p)
		if err != nil {
			return
		}

		for env := range c {
			if env.Error != nil {
				continue
			}
			for _, rr := range env.RR {
				records = append(records, &rr)
			}
		}

		// if we got records from this primary we don't need to lookup in a different primary
		// this logic assumes that there is a one-to-one relationship between hostedZones and primaries
		if len(records) > 0 {
			break
		}
	}
	return
}

func (s *Secondary) ShouldTransfer(zoneName string, knownSOA *dns.SOA) (bool, error) {
	c := new(dns.Client)
	c.Net = "tcp" // do this query over TCP to minimize spoofing
	m := new(dns.Msg)
	m.SetQuestion(zoneName, dns.TypeSOA)

	var Err error
	serial := -1

	for _, p := range s.Primaries {
		Err = nil
		ret, _, err := c.Exchange(m, p)
		if err != nil || ret.Rcode != dns.RcodeSuccess {
			Err = err
			continue
		}
		for _, a := range ret.Answer {
			if a.Header().Rrtype == dns.TypeSOA {
				serial = int(a.(*dns.SOA).Serial)
				break
			}
		}
	}
	if serial == -1 {
		return false, Err
	}
	if knownSOA == nil {
		return true, Err
	}
	return less(knownSOA.Serial, uint32(serial)), Err
}

type Msg struct {
	*dns.Msg
}

func (m *Msg) SetIxfr2(zoneName string, soa *dns.SOA) {
	m.SetIxfr(zoneName, soa.Serial, soa.Ns, soa.Mbox)
}

// less returns true of a is smaller than b when taking RFC 1982 serial arithmetic into account.
func less(a, b uint32) bool {
	if a < b {
		return (b - a) <= MaxSerialIncrement
	}
	return (a - b) > MaxSerialIncrement
}

// MaxSerialIncrement is the maximum difference between two serial numbers. If the difference between
// two serials is greater than this number, the smaller one is considered greater.
const MaxSerialIncrement uint32 = 2147483647
