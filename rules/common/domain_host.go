package common

import "strings"

func isASCIILower(s string) bool {
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c >= 0x80 || (c >= 'A' && c <= 'Z') {
			return false
		}
	}
	return true
}

func isASCII(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] >= 0x80 {
			return false
		}
	}
	return true
}

func matchDomainExact(host, domain string) bool {
	return host == domain || strings.EqualFold(host, domain)
}

// hasDotLabelSuffix is host==suffix or HasSuffix(host, "."+suffix) without allocating.
func hasDotLabelSuffix(host, suffix string) bool {
	if host == suffix {
		return true
	}
	n := len(suffix)
	return len(host) > n && host[len(host)-n-1] == '.' && host[len(host)-n:] == suffix
}

func matchDomainSuffix(host, suffix string) bool {
	if hasDotLabelSuffix(host, suffix) {
		return true
	}
	if isASCIILower(host) {
		return false
	}
	// EqualFold is not ToLower+HasSuffix: "SS" folds to "ß". Only use it when
	// both sides are ASCII, where the two algorithms agree.
	if !isASCII(host) || !isASCII(suffix) {
		return hasDotLabelSuffix(strings.ToLower(host), suffix)
	}
	if strings.EqualFold(host, suffix) {
		return true
	}
	n := len(suffix)
	return len(host) > n && host[len(host)-n-1] == '.' && strings.EqualFold(host[len(host)-n:], suffix)
}

func matchDomainKeyword(host, keyword string) bool {
	if keyword == "" {
		return true
	}
	if strings.Contains(host, keyword) {
		return true
	}
	if isASCIILower(host) {
		return false
	}
	if !isASCII(host) || !isASCII(keyword) {
		return strings.Contains(strings.ToLower(host), keyword)
	}
	n := len(keyword)
	if n > len(host) {
		return false
	}
	for i := 0; i+n <= len(host); i++ {
		if strings.EqualFold(host[i:i+n], keyword) {
			return true
		}
	}
	return false
}

func asciiLowerOnce(s string) string {
	if isASCIILower(s) {
		return s
	}
	return strings.ToLower(s)
}
