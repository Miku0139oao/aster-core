//go:build !(android && cmfa)

package dns

import (
	"net"
	"time"

	"github.com/Miku0139oao/aster-core/component/resolver"
	"github.com/Miku0139oao/aster-core/log"

	"golang.org/x/exp/slices"
)

func (c *systemClient) getDnsClients() ([]dnsClient, error) {
	if last, ok := c.flushAt.Load().(time.Time); ok && !last.IsZero() && time.Since(last) < SystemDnsFlushTime {
		if live := c.live.Load(); live != nil && len(*live) > 0 {
			return *live, nil
		}
	}
	return c.refreshDnsClients()
}

func (c *systemClient) refreshDnsClients() ([]dnsClient, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if time.Since(c.lastFlush) < SystemDnsFlushTime {
		if live := c.live.Load(); live != nil && len(*live) > 0 {
			return *live, nil
		}
	}

	var err error
	if time.Since(c.lastFlush) > SystemDnsFlushTime {
		var nameservers []string
		if nameservers, err = dnsReadConfig(); err == nil {
			log.Debugln("[DNS] system dns update to %s", nameservers)
			for _, addr := range nameservers {
				if resolver.IsSystemDnsBlacklisted(addr) {
					continue
				}
				if _, ok := c.dnsClients[addr]; !ok {
					clients := transform(
						[]NameServer{{
							Addr: net.JoinHostPort(addr, "53"),
							Net:  "udp",
						}},
						nil,
					)
					if len(clients) > 0 {
						c.dnsClients[addr] = &systemDnsClient{
							disableTimes: 0,
							dnsClient:    clients[0],
						}
					}
				}
			}
			available := 0
			for nameserver, sdc := range c.dnsClients {
				if slices.Contains(nameservers, nameserver) {
					sdc.disableTimes = 0 // enable
					available++
				} else {
					if sdc.disableTimes > SystemDnsDeleteTimes {
						delete(c.dnsClients, nameserver) // drop too old dnsClient
					} else {
						sdc.disableTimes++
					}
				}
			}
			if available > 0 {
				c.lastFlush = time.Now()
			}
		}
	}

	live := c.snapshotLiveClients()
	if len(live) > 0 {
		// Store the snapshot before publishing flushAt so a lock-free reader
		// that observes a fresh timestamp also observes the slice.
		c.live.Store(&live)
		if !c.lastFlush.IsZero() {
			c.flushAt.Store(c.lastFlush)
		}
		return live, nil
	}
	c.live.Store(nil)
	return nil, err
}

func (c *systemClient) snapshotLiveClients() []dnsClient {
	n := 0
	for _, sdc := range c.dnsClients {
		if sdc.disableTimes == 0 {
			n++
		}
	}
	if n == 0 {
		return nil
	}
	// cap==len so a caller append cannot alias the stored backing array.
	live := make([]dnsClient, 0, n)
	for _, sdc := range c.dnsClients {
		if sdc.disableTimes == 0 {
			live = append(live, sdc.dnsClient)
		}
	}
	return live
}

func (c *systemClient) ResetConnection() {}
