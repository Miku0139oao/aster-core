package outboundgroup

import (
	"context"

	"github.com/Miku0139oao/aster-core/common/utils"
	C "github.com/Miku0139oao/aster-core/constant"
	P "github.com/Miku0139oao/aster-core/constant/provider"
)

type ProxyGroup interface {
	C.ProxyAdapter

	Providers() []P.ProxyProvider
	Proxies() []C.Proxy
	Now() string
	Touch()

	Hidden() bool
	Icon() string

	URLTest(ctx context.Context, url string, expectedStatus utils.IntRanges[uint16]) (mp map[string]uint16, err error)
}

var (
	_ ProxyGroup = (*Fallback)(nil)
	_ ProxyGroup = (*LoadBalance)(nil)
	_ ProxyGroup = (*URLTest)(nil)
	_ ProxyGroup = (*Selector)(nil)
)

type SelectAble interface {
	Set(string) error
	ForceSet(name string)
}

var (
	_ SelectAble = (*Fallback)(nil)
	_ SelectAble = (*URLTest)(nil)
	_ SelectAble = (*Selector)(nil)
)
