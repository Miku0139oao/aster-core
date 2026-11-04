package router

import (
	"strings"
)

// regexTLD extracts the final ".tld" label from a geosite regex.
//
// Every regex in the shipped geosite lists has the shape
// "(^|\\.)....\\.tld$": a fixed TLD anchor at the end. The extraction only
// accepts that shape, so "host does not end with tld" is a sound reason to skip
// the regex. Anything else returns ok=false and the matcher stays in the
// always-tried list, costing nothing but a lost optimisation.
func regexTLD(pattern string) (string, bool) {
	p := pattern
	if strings.HasSuffix(p, "$") {
		p = p[:len(p)-1]
	}
	// Walk back over the trailing label: letters, digits, hyphen.
	i := len(p)
	for i > 0 {
		c := p[i-1]
		if c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '-' {
			i--
			continue
		}
		break
	}
	if i == len(p) || i == 0 {
		return "", false
	}
	label := strings.ToLower(p[i:])
	if label == "" {
		return "", false
	}
	// The label must be preceded by a literal dot, possibly escaped as "\.".
	switch {
	case i >= 2 && p[i-2] == '\\' && p[i-1] == '.':
		// "\." boundary
	case p[i-1] == '.':
		// plain "." boundary
	default:
		return "", false
	}
	return label, true
}
