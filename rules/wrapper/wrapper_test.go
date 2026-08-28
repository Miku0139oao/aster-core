package wrapper

import (
	"testing"
	"time"

	C "github.com/Miku0139oao/aster-core/constant"
	"github.com/Miku0139oao/aster-core/rules/common"
)

func TestWrapperHitAndMissCounts(t *testing.T) {
	w, ok := NewRuleWrapper(common.NewDomain("www.example.com", "DIRECT")).(*RuleWrapper)
	if !ok {
		t.Fatal("expected *RuleWrapper")
	}
	helper := C.RuleMatchHelper{}
	hitMeta := &C.Metadata{Host: "www.example.com"}
	missMeta := &C.Metadata{Host: "cdn.example.net"}

	okHit, adapter := w.Match(hitMeta, helper)
	if !okHit || adapter != "DIRECT" {
		t.Fatalf("hit: %v %q", okHit, adapter)
	}
	if w.HitCount() != 1 {
		t.Fatalf("hit count %d", w.HitCount())
	}
	if w.HitAt().IsZero() || w.HitAt().Equal(time.Unix(0, 0)) {
		t.Fatalf("hit at not recorded: %v", w.HitAt())
	}

	for i := 0; i < 3; i++ {
		okMiss, _ := w.Match(missMeta, helper)
		if okMiss {
			t.Fatal("expected miss")
		}
	}
	if w.MissCount() != 3 {
		t.Fatalf("miss count %d", w.MissCount())
	}
	if w.MissAt().Equal(time.Unix(0, 0)) {
		t.Fatal("first miss should record timestamp")
	}

	w.SetDisabled(true)
	okHit, adapter = w.Match(hitMeta, helper)
	if okHit || adapter != "" {
		t.Fatalf("disabled match: %v %q", okHit, adapter)
	}
	if w.HitCount() != 1 {
		t.Fatalf("disabled rule should not count hits, got %d", w.HitCount())
	}
}
