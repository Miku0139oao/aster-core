package common

import (
	"testing"

	C "github.com/Miku0139oao/aster-core/constant"
)

func TestDomainMatchersIgnoreHostCase(t *testing.T) {
	metadata := &C.Metadata{Host: "WWW.Example.COM"}

	tests := []struct {
		name  string
		match func(*C.Metadata) bool
	}{
		{
			name: "domain",
			match: func(metadata *C.Metadata) bool {
				rule := NewDomain("www.example.com", "DIRECT")
				matched, _ := rule.Match(metadata, C.RuleMatchHelper{})
				return matched
			},
		},
		{
			name: "domain suffix",
			match: func(metadata *C.Metadata) bool {
				rule := NewDomainSuffix("example.com", "DIRECT")
				matched, _ := rule.Match(metadata, C.RuleMatchHelper{})
				return matched
			},
		},
		{
			name: "domain keyword",
			match: func(metadata *C.Metadata) bool {
				rule := NewDomainKeyword("EXAMPLE", "DIRECT")
				matched, _ := rule.Match(metadata, C.RuleMatchHelper{})
				return matched
			},
		},
		{
			name: "domain wildcard",
			match: func(metadata *C.Metadata) bool {
				rule, err := NewDomainWildcard("*.example.com", "DIRECT")
				if err != nil {
					t.Fatalf("construct wildcard rule: %v", err)
				}
				matched, _ := rule.Match(metadata, C.RuleMatchHelper{})
				return matched
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if !test.match(metadata) {
				t.Fatalf("matcher did not match host %q", metadata.Host)
			}
		})
	}
}
