package secondary

import (
	"github.com/coredns/caddy"
	"github.com/coredns/coredns/core/dnsserver"
	"github.com/coredns/coredns/plugin"
	"strings"
)

func init() {
	caddy.RegisterPlugin("secondary", caddy.Plugin{
		ServerType: "dns",
		Action:     setup,
	})
}

func setup(c *caddy.Controller) error {
	s, err := parseConfig(c)
	if err != nil {
		return err
	}

	dnsserver.GetConfig(c).AddPlugin(func(next plugin.Handler) plugin.Handler {
		s.Next = next
		return s
	})

	c.OnStartup(func() error {
		// find all plugins that implement Persistence and add them to Persistors
		plugins := dnsserver.GetConfig(c).Handlers()
		for _, pl := range plugins {
			tr, ok := pl.(TransferPersistence)
			if !ok {
				continue
			}
			s.Persistors = append(s.Persistors, tr)
		}
		return nil
	})

	return nil
}

func parseConfig(c *caddy.Controller) (s *Secondary, err error) {
	s = New()
	for c.Next() {
		if c.NextBlock() {
			if val := c.Val(); val == "primary" {
				if !c.NextArg() {
					return s, c.ArgErr()
				}
				s.Primaries = strings.Split(strings.Trim(c.Val(), " "), " ")
			}
		}
	}
	return
}
