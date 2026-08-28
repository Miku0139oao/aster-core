package wrapper

import (
	"testing"

	C "github.com/Miku0139oao/aster-core/constant"
	"github.com/Miku0139oao/aster-core/rules/common"
)

func BenchmarkWrapperDomainSuffixChain32(b *testing.B) {
	suffixes := []string{
		"google.com", "googleapis.com", "gstatic.com", "youtube.com",
		"facebook.com", "fbcdn.net", "twitter.com", "x.com",
		"github.com", "githubusercontent.com", "cloudflare.com", "cloudfront.net",
		"amazon.com", "amazonaws.com", "apple.com", "icloud.com",
		"microsoft.com", "live.com", "office.com", "windows.net",
		"netflix.com", "nflxvideo.net", "spotify.com", "reddit.com",
		"wikipedia.org", "wikimedia.org", "baidu.com", "qq.com",
		"tiktok.com", "bytedance.com", "example.org", "example.com",
	}
	rules := make([]C.Rule, 0, len(suffixes))
	for _, suffix := range suffixes {
		rules = append(rules, NewRuleWrapper(common.NewDomainSuffix(suffix, "PROXY")))
	}
	meta := &C.Metadata{Host: "www.example.com"}
	helper := C.RuleMatchHelper{}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		for _, rule := range rules {
			if ok, _ := rule.Match(meta, helper); ok {
				break
			}
		}
	}
}

func BenchmarkWrapperMiss(b *testing.B) {
	rule := NewRuleWrapper(common.NewDomain("www.example.com", "DIRECT"))
	meta := &C.Metadata{Host: "cdn.example.net"}
	helper := C.RuleMatchHelper{}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = rule.Match(meta, helper)
	}
}
