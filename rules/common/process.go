package common

import (
	"strings"

	"github.com/Miku0139oao/aster-core/component/wildcard"
	C "github.com/Miku0139oao/aster-core/constant"
)

type Process struct {
	Base
	pattern      string
	lowerPattern string
	adapter      string
	ruleType     C.RuleType
	regexp       *compiledRegex
}

func (ps *Process) Payload() string {
	return ps.pattern
}

func (ps *Process) Adapter() string {
	return ps.adapter
}

func (ps *Process) RuleType() C.RuleType {
	return ps.ruleType
}

func (ps *Process) Match(metadata *C.Metadata, helper C.RuleMatchHelper) (bool, string) {
	if helper.FindProcess != nil {
		helper.FindProcess()
	}
	var target string
	switch ps.ruleType {
	case C.ProcessName, C.ProcessNameRegex, C.ProcessNameWildcard:
		target = metadata.Process
	default:
		target = metadata.ProcessPath
	}

	switch ps.ruleType {
	case C.ProcessNameRegex, C.ProcessPathRegex:
		return ps.regexp.Match(target), ps.adapter
	case C.ProcessNameWildcard, C.ProcessPathWildcard:
		return wildcard.Match(ps.lowerPattern, asciiLowerOnce(target)), ps.adapter
	default:
		return target == ps.pattern || strings.EqualFold(target, ps.pattern), ps.adapter
	}
}

func NewProcess(pattern string, adapter string, ruleType C.RuleType) (*Process, error) {
	ps := &Process{
		Base:     Base{},
		pattern:  pattern,
		adapter:  adapter,
		ruleType: ruleType,
	}
	switch ps.ruleType {
	case C.ProcessNameRegex, C.ProcessPathRegex:
		r, err := compileIgnoreCaseRegex(pattern)
		if err != nil {
			return nil, err
		}
		ps.regexp = r
	case C.ProcessNameWildcard, C.ProcessPathWildcard:
		ps.lowerPattern = strings.ToLower(pattern)
	default:
	}
	return ps, nil
}

var _ C.Rule = (*Process)(nil)
