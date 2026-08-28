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

func TestDomainSuffixBoundaries(t *testing.T) {
	rule := NewDomainSuffix("example.com", "DIRECT")
	helper := C.RuleMatchHelper{}
	cases := []struct {
		host string
		want bool
	}{
		{"example.com", true},
		{"www.example.com", true},
		{"notexample.com", false},
		{"example.com.evil", false},
		{"EXAMPLE.COM", true},
		{"a.b.example.com", true},
		{"example.org", false},
		{"", false},
	}
	for _, tc := range cases {
		got, adapter := rule.Match(&C.Metadata{Host: tc.host}, helper)
		if got != tc.want {
			t.Fatalf("host %q: got %v want %v", tc.host, got, tc.want)
		}
		if got && adapter != "DIRECT" {
			t.Fatalf("host %q: adapter %q", tc.host, adapter)
		}
	}
}

func TestDomainSuffixToLowerNotEqualFold(t *testing.T) {
	// strings.EqualFold("SS.com", "ß.com") is true; pre-change Match used ToLower+HasSuffix.
	rule := NewDomainSuffix("ß.com", "DIRECT")
	helper := C.RuleMatchHelper{}
	hit, _ := rule.Match(&C.Metadata{Host: "www.ß.com"}, helper)
	if !hit {
		t.Fatal("expected suffix hit on ß.com")
	}
	miss, _ := rule.Match(&C.Metadata{Host: "SS.com"}, helper)
	if miss {
		t.Fatal("SS.com must not match suffix ß.com (ToLower semantics)")
	}
	mixed, _ := rule.Match(&C.Metadata{Host: "WWW.ß.COM"}, helper)
	if !mixed {
		t.Fatal("expected mixed-case ß.com hit")
	}
}

func TestDomainKeywordToLowerNotEqualFold(t *testing.T) {
	rule := NewDomainKeyword("ß", "PROXY")
	helper := C.RuleMatchHelper{}
	hit, _ := rule.Match(&C.Metadata{Host: "www.ß.de"}, helper)
	if !hit {
		t.Fatal("expected keyword hit on ß")
	}
	miss, _ := rule.Match(&C.Metadata{Host: "SS.com"}, helper)
	if miss {
		t.Fatal("SS.com must not match keyword ß (ToLower+Contains semantics)")
	}
}

func TestDomainKeywordBoundaries(t *testing.T) {
	rule := NewDomainKeyword("google", "PROXY")
	helper := C.RuleMatchHelper{}
	cases := []struct {
		host string
		want bool
	}{
		{"www.google.com", true},
		{"GOOGLE.com", true},
		{"www.goggle.com", false},
		{"example.com", false},
	}
	for _, tc := range cases {
		got, _ := rule.Match(&C.Metadata{Host: tc.host}, helper)
		if got != tc.want {
			t.Fatalf("host %q: got %v want %v", tc.host, got, tc.want)
		}
	}
}
