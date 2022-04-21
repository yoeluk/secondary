package secondary

import (
	"github.com/coredns/caddy"
	"github.com/coredns/coredns/core/dnsserver"
	"github.com/coredns/coredns/plugin"
)

func init() {
	caddy.RegisterPlugin("secondary", caddy.Plugin{
		ServerType: "dns",
		Action:     setup,
	})
}

func setup(c *caddy.Controller) error {
	s := New()

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
