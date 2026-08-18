package outbound

import (
	"context"
	"testing"
)

func TestHysteriaDialerUsesCallerContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	dialer := (&Hysteria{}).genHdc(ctx)
	if got := dialer.Context(); got != ctx {
		t.Fatalf("dialer context = %v, want caller context %v", got, ctx)
	}
}
