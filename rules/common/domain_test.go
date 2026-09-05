package common

import (
	"strings"
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

func TestDomainSuffixPayloadLowered(t *testing.T) {
	rule := NewDomainSuffix("Example.COM", "DIRECT")
	if rule.Payload() != "example.com" {
		t.Fatalf("payload: %q", rule.Payload())
	}
}

// matchDomainSuffixRef is an independent copy of the pre-change matcher
// (host==suffix || HasSuffix(host, "."+suffix), then the same ASCII/Unicode branches).
func matchDomainSuffixRef(host, suffix string) bool {
	dotSuffix := "." + suffix
	if host == suffix || strings.HasSuffix(host, dotSuffix) {
		return true
	}
	if isASCIILower(host) {
		return false
	}
	if !isASCII(host) || !isASCII(suffix) {
		domain := strings.ToLower(host)
		return domain == suffix || strings.HasSuffix(domain, dotSuffix)
	}
	if strings.EqualFold(host, suffix) {
		return true
	}
	n := len(suffix)
	return len(host) > n && host[len(host)-n-1] == '.' && strings.EqualFold(host[len(host)-n:], suffix)
}

func TestMatchDomainSuffixMatchesOldHasSuffixReference(t *testing.T) {
	suffixes := []string{
		"",
		"com",
		"example.com",
		".example.com",
		"example.com.",
		"ß.com",
		"ss.com",
	}
	hosts := []string{
		"",
		".",
		"com",
		".com",
		"com.",
		"example.com",
		".example.com",
		"example.com.",
		"www.example.com",
		"notexample.com",
		"EXAMPLE.COM",
		"WWW.Example.COM",
		"a.b.example.com",
		"example.com.evil",
		strings.Repeat("a.", 64) + "example.com",
		"www.ß.com",
		"WWW.ß.COM",
		"SS.com",
		"www.ss.com",
	}
	for _, suffix := range suffixes {
		lowered := strings.ToLower(suffix)
		for _, host := range hosts {
			got := matchDomainSuffix(host, lowered)
			want := matchDomainSuffixRef(host, lowered)
			if got != want {
				t.Fatalf("host %q suffix %q: got %v want %v", host, lowered, got, want)
			}
			rule := NewDomainSuffix(suffix, "DIRECT")
			matched, adapter := rule.Match(&C.Metadata{Host: host}, C.RuleMatchHelper{})
			if matched != want {
				t.Fatalf("Match host %q suffix %q: got %v want %v", host, suffix, matched, want)
			}
			if matched && adapter != "DIRECT" {
				t.Fatalf("adapter %q", adapter)
			}
		}
	}
}
