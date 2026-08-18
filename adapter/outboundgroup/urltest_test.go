package outboundgroup

import (
	"context"
	"testing"

	"github.com/Miku0139oao/aster-core/adapter/outbound"
	adapterprovider "github.com/Miku0139oao/aster-core/adapter/provider"
	"github.com/Miku0139oao/aster-core/common/utils"
	C "github.com/Miku0139oao/aster-core/constant"
	P "github.com/Miku0139oao/aster-core/constant/provider"
)

type urlTestProxyStub struct {
	*outbound.Base
	alive bool
	delay uint16
}

func (p *urlTestProxyStub) Adapter() C.ProxyAdapter {
	return p.Base
}

func (p *urlTestProxyStub) AliveForTestUrl(string) bool {
	return p.alive
}

func (p *urlTestProxyStub) DelayHistory() []C.DelayHistory {
	return nil
}

func (p *urlTestProxyStub) ExtraDelayHistories() map[string]C.ProxyState {
	return nil
}

func (p *urlTestProxyStub) LastDelayForTestUrl(string) uint16 {
	if !p.alive {
		return 0xffff
	}
	return p.delay
}

func (p *urlTestProxyStub) URLTest(context.Context, string, utils.IntRanges[uint16]) (uint16, error) {
	return p.delay, nil
}

func newURLTestProxyStub(name string, alive bool, delay uint16) C.Proxy {
	return &urlTestProxyStub{
		Base: outbound.NewBase(outbound.BaseOption{
			Name: name,
			Type: C.Direct,
			UDP:  true,
		}),
		alive: alive,
		delay: delay,
	}
}

func TestURLTestSkipsDeadFirstProxyWhenAliveProxyHasNoDelay(t *testing.T) {
	dead := newURLTestProxyStub("dead", false, 0xffff)
	alive := newURLTestProxyStub("alive", true, 0xffff)
	healthCheck := adapterprovider.NewHealthCheck([]C.Proxy{dead, alive}, "https://example.com", 0, 0, true, nil)
	provider, err := adapterprovider.NewCompatibleProvider("test", []C.Proxy{dead, alive}, healthCheck)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = provider.Close() })

	group, err := NewURLTest(
		GroupCommonOption{Name: "url-test", URL: "https://example.com"},
		URLTestOption{},
		dead,
		[]P.ProxyProvider{provider},
	)
	if err != nil {
		t.Fatal(err)
	}

	if got, want := group.Now(), "alive"; got != want {
		t.Fatalf("selected proxy = %q, want %q", got, want)
	}
}
