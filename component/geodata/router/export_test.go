package router

// RegexTLDForTest exposes regexTLD to the external test package.
func RegexTLDForTest(pattern string) (string, bool) { return regexTLD(pattern) }
