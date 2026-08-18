package provider

import (
	"testing"

	C "github.com/Miku0139oao/aster-core/constant"
	P "github.com/Miku0139oao/aster-core/constant/provider"
)

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
