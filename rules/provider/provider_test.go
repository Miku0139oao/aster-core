package provider

import (
	"sync"
	"testing"
	"time"

	C "github.com/Miku0139oao/aster-core/constant"
	P "github.com/Miku0139oao/aster-core/constant/provider"
)

type providerTestStrategy struct{ match bool }

func (*providerTestStrategy) Behavior() P.RuleBehavior                    { return P.Domain }
func (s *providerTestStrategy) Match(*C.Metadata, C.RuleMatchHelper) bool { return s.match }
func (*providerTestStrategy) Count() int                                  { return 1 }
func (*providerTestStrategy) Reset()                                      {}
func (*providerTestStrategy) Insert(string)                               {}
func (*providerTestStrategy) FinishInsert()                               {}

type providerTestRuleProvider struct {
	baseProvider
	name string
}

func (p *providerTestRuleProvider) Name() string             { return p.name }
func (*providerTestRuleProvider) VehicleType() P.VehicleType { return P.Inline }
func (*providerTestRuleProvider) Initial() error             { return nil }
func (*providerTestRuleProvider) Update() error              { return nil }

func newProviderTestRuleProvider(name string, match bool) *providerTestRuleProvider {
	provider := &providerTestRuleProvider{baseProvider: baseProvider{behavior: P.Domain}, name: name}
	provider.setStrategy(&providerTestStrategy{match: match})
	return provider
}

func TestRuleSetPinsProviderGeneration(t *testing.T) {
	oldProvider := newProviderTestRuleProvider("rules", true)
	newProvider := newProviderTestRuleProvider("rules", false)
	rule, err := NewRuleSet("rules", "DIRECT", false, true, oldProvider)
	if err != nil {
		t.Fatal(err)
	}
	rule.BindRuleProviders(map[string]P.RuleProvider{"rules": oldProvider})
	if matched, _ := rule.Match(&C.Metadata{Host: "example.com"}, C.RuleMatchHelper{}); !matched {
		t.Fatal("bound provider did not match")
	}
	rule.BindRuleProviders(map[string]P.RuleProvider{"rules": newProvider})
	if matched, _ := rule.Match(&C.Metadata{Host: "example.com"}, C.RuleMatchHelper{}); matched {
		t.Fatal("explicit generation rebind did not take effect")
	}
}

func TestBaseProviderPublishesStrategiesAtomically(t *testing.T) {
	provider := newProviderTestRuleProvider("rules", false)
	var wg sync.WaitGroup
	wg.Add(3)
	go func() {
		defer wg.Done()
		for i := 0; i < 10_000; i++ {
			provider.setStrategy(&providerTestStrategy{match: i&1 == 0})
		}
	}()
	for i := 0; i < 2; i++ {
		go func() {
			defer wg.Done()
			deadline := time.Now().Add(100 * time.Millisecond)
			for time.Now().Before(deadline) {
				_ = provider.Match(&C.Metadata{Host: "example.com"}, C.RuleMatchHelper{})
				_ = provider.Count()
				_ = provider.Strategy()
			}
		}()
	}
	wg.Wait()
}

func TestRulesParseSingleLineYAML(t *testing.T) {
	strategy, err := rulesParse(
		[]byte("payload: [example.com, api.example.com]"),
		NewDomainStrategy(),
		P.YamlRule,
	)
	if err != nil {
		t.Fatalf("parse single-line YAML: %v", err)
	}

	metadata := &C.Metadata{Host: "api.example.com"}
	if !strategy.Match(metadata, C.RuleMatchHelper{}) {
		t.Fatalf("single-line YAML payload was not loaded")
	}
}
