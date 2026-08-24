package strmatcher

import "testing"

func TestACAutomatonRejectsUnsupportedPatternWithoutMutation(t *testing.T) {
	ac := NewACAutomaton()
	if ac.Add("foo@bar", Substr) {
		t.Fatal("unsupported pattern was accepted by compact automaton")
	}
	if len(ac.trie) != 1 || ac.count != 0 {
		t.Fatal("rejected pattern partially mutated automaton")
	}
}

func TestACAutomatonUnsupportedInputDoesNotPanicOrAliasA(t *testing.T) {
	ac := NewACAutomaton()
	if !ac.Add("A", Full) || !ac.Add("foo", Substr) {
		t.Fatal("supported pattern was rejected")
	}
	ac.Build()

	for _, input := range []string{"@", "\x7f", "☃"} {
		if ac.Match(input) {
			t.Fatalf("unsupported input %q aliased a valid pattern", input)
		}
	}
	if !ac.Match("éfoo") || !ac.Match("foo☃") {
		t.Fatal("unsupported-byte boundary hid a valid substring")
	}
}

func TestMphMatcherFallsBackForUnsupportedSubstringBytes(t *testing.T) {
	group := NewMphMatcherGroup()
	for _, pattern := range []string{"foo@bar", "雪.example"} {
		if _, err := group.AddPattern(pattern, Substr); err != nil {
			t.Fatal(err)
		}
	}
	group.Build()

	for _, input := range []string{"prefix-foo@bar-suffix", "www.雪.example.test"} {
		if len(group.Match(input)) == 0 {
			t.Fatalf("fallback substring did not match %q", input)
		}
	}
	if len(group.Match("fooaabar")) != 0 {
		t.Fatal("unsupported @ byte aliased A/a")
	}
}
