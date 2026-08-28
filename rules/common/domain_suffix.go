package common

import (
	"strings"

	C "github.com/Miku0139oao/aster-core/constant"
)

type DomainSuffix struct {
	Base
	suffix    string
	dotSuffix string
	adapter   string
}

func (ds *DomainSuffix) RuleType() C.RuleType {
	return C.DomainSuffix
}

func (ds *DomainSuffix) Match(metadata *C.Metadata, helper C.RuleMatchHelper) (bool, string) {
	return matchDomainSuffix(metadata.RuleHost(), ds.suffix, ds.dotSuffix), ds.adapter
}

func (ds *DomainSuffix) Adapter() string {
	return ds.adapter
}

func (ds *DomainSuffix) Payload() string {
	return ds.suffix
}

func NewDomainSuffix(suffix string, adapter string) *DomainSuffix {
	suffix = strings.ToLower(suffix)
	return &DomainSuffix{
		Base:      Base{},
		suffix:    suffix,
		dotSuffix: "." + suffix,
		adapter:   adapter,
	}
}

var _ C.Rule = (*DomainSuffix)(nil)
