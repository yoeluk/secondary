package secondary

import (
	"context"
	"github.com/coredns/coredns/plugin"
	clog "github.com/coredns/coredns/plugin/pkg/log"
	"github.com/coredns/coredns/request"
	"github.com/miekg/dns"
	"strings"
	"time"
)

const name = "secondary"

var log = clog.NewWithPlugin("secondary")

func (s *Secondary) Name() string {
	return name
}

type TransferPersistence interface {
	Name() string
	Persist(zone string, records []dns.RR) error
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

	log.Debugf("received notify question: %s", r.Question[0].String())

	log.Debugf("current primaries are: %s", strings.Join(s.Primaries, " "))

	if len(s.Persistors) == 0 {
		log.Errorf("no transfer persistence was detected")
	}

	// write the reply to NOTIFY
	m := new(dns.Msg)
	m.SetReply(r)
	m.Authoritative = true
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
	should, primary, err := s.ShouldTransfer(state.QName(), knownSOA)
	shouldTransfer := should && len(primary) > 0 && err != nil
	if err != nil {
		log.Errorf("the zone %s couldn't be found among the primaries", state.QName())
	}

	// retrieved changed records
	var records []dns.RR
	if shouldTransfer {
		records = s.TransferIn(state.QName(), knownSOA, primary)
	}

	// persist retrieved records from primary
	if records != nil && len(records) > 0 {
		for _, p := range s.Persistors {
			err := p.Persist(state.QName(), records)
			if err != nil {
				log.Errorf("the was error persisting zone records for zone %s to persistence %s with error message: %s", state.QName(), p.Name(), err.Error())
			}
		}
	}

	return dns.RcodeSuccess, nil
}

func (s *Secondary) TransferIn(zoneName string, knownSOA *dns.SOA, primary string) (records []dns.RR) {
	m := new(dns.Msg)

	if knownSOA != nil {
		m.SetIxfr(zoneName, knownSOA.Serial, knownSOA.Ns, knownSOA.Mbox)
	} else {
		m.SetAxfr(zoneName)
	}

	records = s.In(m, primary)
	return
}

func (s *Secondary) In(m *dns.Msg, primary string) (records []dns.RR) {
	t := new(dns.Transfer)

	c, err := t.In(m, primary)
	if err != nil {
		log.Errorf("found an error during t.In for server %s, with error message %s", primary, err.Error())
		return
	}

	for env := range c {
		if env.Error != nil {
			continue
		}
		for _, rr := range env.RR {
			records = append(records, rr)
		}
	}
	return
}

func (s *Secondary) ShouldTransfer(zoneName string, knownSOA *dns.SOA) (bool, string, error) {
	c := new(dns.Client)
	c.Timeout = 3 * time.Second

	m := new(dns.Msg)
	m.SetQuestion(zoneName, dns.TypeSOA)
	m.RecursionDesired = true

	var primary string
	var Err error
	serial := -1

	for _, p := range s.Primaries {
		Err = nil
		ret, _, err := c.Exchange(m, p)
		if err != nil || ret.Rcode != dns.RcodeSuccess {
			Err = err
			if err != nil {
				log.Errorf("there was an error contacting master %s: %s", p, err.Error())
			}
			if ret == nil {
				log.Errorf("the response from primary %s was nil for zone %s", p, zoneName)
			}
			continue
		}
		for _, a := range ret.Answer {
			if a.Header().Rrtype == dns.TypeSOA {
				serial = int(a.(*dns.SOA).Serial)
				log.Debugf("found primary with zone %", zoneName)
				primary = p
				break
			}
		}
	}
	if serial == -1 {
		return false, primary, Err
	}
	if knownSOA == nil {
		return true, primary, Err
	}
	return less(knownSOA.Serial, uint32(serial)), primary, Err
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
