package common

import (
	"regexp"

	"github.com/dlclark/regexp2"
)

// compiledRegex prefers Go's RE2 engine for Match() when the pattern is in a
// conservative subset that agrees with regexp2 on ASCII host/process names.
// .NET-only syntax keeps the regexp2 fallback (already compiled once).
type compiledRegex struct {
	pattern string
	fast    *regexp.Regexp
	slow    *regexp2.Regexp
}

func compileIgnoreCaseRegex(pattern string) (*compiledRegex, error) {
	slow, err := regexp2.Compile(pattern, regexp2.IgnoreCase)
	if err != nil {
		return nil, err
	}
	cr := &compiledRegex{pattern: pattern, slow: slow}
	if stdPattern, ok := stdlibIgnoreCasePattern(pattern); ok {
		if fast, err := regexp.Compile(stdPattern); err == nil {
			cr.fast = fast
			cr.slow = nil // RE2 path owns Match; drop regexp2 runner/mutex
		}
	}
	return cr, nil
}

func stdlibIgnoreCasePattern(pattern string) (string, bool) {
	for i := 0; i < len(pattern); i++ {
		switch pattern[i] {
		case '\\':
			if i+1 >= len(pattern) {
				return "", false
			}
			switch pattern[i+1] {
			case '.', '*', '+', '?', '(', ')', '[', ']', '{', '}', '|', '^', '$', '\\':
				i++
			default:
				return "", false
			}
		case '(':
			if i+1 < len(pattern) && pattern[i+1] == '?' {
				return "", false
			}
		case '[':
			// Character classes diverge (.NET POSIX/collation vs RE2). Keep regexp2.
			return "", false
		}
	}
	return "(?i:" + pattern + ")", true
}

func (c *compiledRegex) Match(s string) bool {
	if c.fast != nil {
		return c.fast.MatchString(s)
	}
	ok, _ := c.slow.MatchString(s)
	return ok
}

func (c *compiledRegex) String() string {
	return c.pattern
}
