package provider

import (
	"testing"

	C "github.com/Miku0139oao/aster-core/constant"

	"github.com/stretchr/testify/assert"
)

func TestDomainStrategyDumpMrs(t *testing.T) {
	tests := []struct {
		name     string
		domains  []string
		expected []string
		queries  map[string]bool
	}{
		{
			"suffix-only",
			[]string{".example.com"},
			[]string{".example.com"},
			map[string]bool{"example.com": false, "www.example.com": true},
		},
		{
			"exact-only",
			[]string{"example.com"},
			[]string{"example.com"},
			map[string]bool{"example.com": true, "www.example.com": false},
		},
		{
			"merge-terminals",
			[]string{"example.com", ".example.com"},
			[]string{"+.example.com"},
			map[string]bool{"example.com": true, "www.example.com": true},
		},
		{
			"deduplicate",
			[]string{"+.example.com", "+.example.com", "example.com", ".example.com"},
			[]string{"+.example.com"},
			map[string]bool{"example.com": true, "www.example.com": true},
		},
		{
			"mixed",
			[]string{".example.com", "example.net", "example.org", ".example.org"},
			[]string{"+.example.org", ".example.com", "example.net"},
			map[string]bool{
				"example.com":     false,
				"www.example.com": true,
				"example.net":     true,
				"www.example.net": false,
				"example.org":     true,
				"www.example.org": true,
			},
		},
		{
			"wildcards",
			[]string{"*.example.com", "+.example.org", "test.*.google.com", ".test.*.google.com", ".example.net"},
			[]string{"*.example.com", "+.example.org", "+.test.*.google.com", ".example.net"},
			map[string]bool{
				"example.com":             false,
				"www.example.com":         true,
				"example.org":             true,
				"www.example.org":         true,
				"test.www.google.com":     true,
				"www.test.www.google.com": true,
				"example.net":             false,
				"www.example.net":         true,
			},
		},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			strategy := NewDomainStrategy()
			strategy.Reset()
			for _, domain := range testCase.domains {
				strategy.Insert(domain)
			}
			strategy.FinishInsert()
			rebuilt := NewDomainStrategy()
			rebuilt.Reset()
			var domains []string
			strategy.DumpMrs(func(domain string) bool {
				domains = append(domains, domain)
				rebuilt.Insert(domain)
				return true
			})
			rebuilt.FinishInsert()
			assert.Equal(t, testCase.expected, domains)
			for domain, expected := range testCase.queries {
				metadata := &C.Metadata{Host: domain}
				assert.Equal(t, expected, strategy.Match(metadata, C.RuleMatchHelper{}), domain)
				assert.Equal(t, expected, rebuilt.Match(metadata, C.RuleMatchHelper{}), domain)
			}
		})
	}
}

func TestDomainStrategyDumpMrsEarlyStop(t *testing.T) {
	strategy := NewDomainStrategy()
	strategy.Reset()
	for _, domain := range []string{"a.example.com", "b.example.com", "c.example.com"} {
		strategy.Insert(domain)
	}
	strategy.FinishInsert()

	count := 0
	strategy.DumpMrs(func(string) bool {
		count++
		return false
	})
	assert.Equal(t, 1, count)
}
