package common

import (
	"testing"

	C "github.com/Miku0139oao/aster-core/constant"
)

func TestDomainRegexStdlibFastPath(t *testing.T) {
	rule, err := NewDomainRegex(`^.*\.example\.com$`, "DIRECT")
	if err != nil {
		t.Fatal(err)
	}
	if rule.regex.fast == nil {
		t.Fatal("expected stdlib fast path for simple domain regex")
	}
	helper := C.RuleMatchHelper{}
	hit, adapter := rule.Match(&C.Metadata{Host: "www.example.com"}, helper)
	if !hit || adapter != "DIRECT" {
		t.Fatalf("expected hit, got %v %q", hit, adapter)
	}
	mixed, _ := rule.Match(&C.Metadata{Host: "WWW.EXAMPLE.COM"}, helper)
	if !mixed {
		t.Fatal("expected ignore-case hit")
	}
	miss, _ := rule.Match(&C.Metadata{Host: "example.net"}, helper)
	if miss {
		t.Fatal("expected miss")
	}
	if rule.Payload() != `^.*\.example\.com$` {
		t.Fatalf("payload: %q", rule.Payload())
	}
}

func TestDomainRegexInvalidPattern(t *testing.T) {
	_, err := NewDomainRegex(`(`, "DIRECT")
	if err == nil {
		t.Fatal("expected compile error")
	}
}

func TestDomainRegexCharacterClassUsesRegexp2(t *testing.T) {
	rule, err := NewDomainRegex(`[0-9]+\.example\.com`, "DIRECT")
	if err != nil {
		t.Fatal(err)
	}
	if rule.regex.fast != nil {
		t.Fatal("character classes must stay on regexp2")
	}
	hit, _ := rule.Match(&C.Metadata{Host: "9.example.com"}, C.RuleMatchHelper{})
	if !hit {
		t.Fatal("expected regexp2 character-class hit")
	}
	miss, _ := rule.Match(&C.Metadata{Host: "a.example.com"}, C.RuleMatchHelper{})
	if miss {
		t.Fatal("expected character-class miss")
	}
}

func TestDomainRegexLookbehindFallback(t *testing.T) {
	rule, err := NewDomainRegex(`(?<=www\.)example\.com`, "REJECT")
	if err != nil {
		t.Fatal(err)
	}
	if rule.regex.fast != nil {
		t.Fatal("lookbehind should use regexp2 fallback")
	}
	helper := C.RuleMatchHelper{}
	hit, _ := rule.Match(&C.Metadata{Host: "www.example.com"}, helper)
	if !hit {
		t.Fatal("expected regexp2 lookbehind hit")
	}
	miss, _ := rule.Match(&C.Metadata{Host: "ftp.example.com"}, helper)
	if miss {
		t.Fatal("expected lookbehind miss")
	}
}

func TestProcessNameRegexFastPath(t *testing.T) {
	rule, err := NewProcess(`chrome.*`, "DIRECT", C.ProcessNameRegex)
	if err != nil {
		t.Fatal(err)
	}
	if rule.regexp.fast == nil {
		t.Fatal("expected stdlib fast path")
	}
	hit, _ := rule.Match(&C.Metadata{Process: "Chrome.exe"}, C.RuleMatchHelper{})
	if !hit {
		t.Fatal("expected process regex hit")
	}
}

func TestProcessNameWildcardPreservesPayload(t *testing.T) {
	rule, err := NewProcess("Chrome*", "DIRECT", C.ProcessNameWildcard)
	if err != nil {
		t.Fatal(err)
	}
	if rule.Payload() != "Chrome*" {
		t.Fatalf("payload changed: %q", rule.Payload())
	}
	hit, _ := rule.Match(&C.Metadata{Process: "chrome.exe"}, C.RuleMatchHelper{})
	if !hit {
		t.Fatal("expected wildcard hit")
	}
}
