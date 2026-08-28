package common

import (
	C "github.com/Miku0139oao/aster-core/constant"
)

type DomainRegex struct {
	Base
	regex   *compiledRegex
	adapter string
}

func (dr *DomainRegex) RuleType() C.RuleType {
	return C.DomainRegex
}

func (dr *DomainRegex) Match(metadata *C.Metadata, helper C.RuleMatchHelper) (bool, string) {
	return dr.regex.Match(metadata.RuleHost()), dr.adapter
}

func (dr *DomainRegex) Adapter() string {
	return dr.adapter
}

func (dr *DomainRegex) Payload() string {
	return dr.regex.String()
}

func NewDomainRegex(regex string, adapter string) (*DomainRegex, error) {
	r, err := compileIgnoreCaseRegex(regex)
	if err != nil {
		return nil, err
	}
	return &DomainRegex{
		Base:    Base{},
		regex:   r,
		adapter: adapter,
	}, nil
}

var _ C.Rule = (*DomainRegex)(nil)
