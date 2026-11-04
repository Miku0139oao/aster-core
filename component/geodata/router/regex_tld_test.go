package router

import "testing"

func TestRegexTLDExtraction(t *testing.T) {
	cases := []struct {
		pattern string
		want    string
		ok      bool
	}{
		{`(^|\.)13mei[5-7]\.buzz$`, "buzz", true},
		{`(^|\.)18jmttios[0-9]{2}\.com$`, "com", true},
		{`(^|\.)783([a-z][a-z]?)?\.tv$`, "tv", true},
		{`(^|\.)example\.org`, "org", true},
		{`.*\.googlevideo\.com`, "com", true},
		{`(^|\.)a\.b\.example\.co-uk$`, "co-uk", true},
		{`(^|\.)example\.[a-z]+$`, "", false},
		{`.*`, "", false},
		{`^https?://.*\.example\.com/`, "", false},
		{`(^|\.)foo\.(com|net)$`, "", false},
	}
	for _, c := range cases {
		got, ok := regexTLD(c.pattern)
		if ok != c.ok {
			t.Errorf("regexTLD(%q) ok=%v want %v (got %q)", c.pattern, ok, c.ok, got)
			continue
		}
		if ok && got != c.want {
			t.Errorf("regexTLD(%q) = %q, want %q", c.pattern, got, c.want)
		}
	}
}
