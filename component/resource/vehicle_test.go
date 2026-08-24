package resource

import (
	"errors"
	"strings"
	"testing"
)

func TestReadWithLimitRejectsInsteadOfTruncating(t *testing.T) {
	buf, err := readWithLimit(strings.NewReader("123456"), 5)
	if !errors.Is(err, ErrResourceTooLarge) {
		t.Fatalf("error = %v, want ErrResourceTooLarge", err)
	}
	if buf != nil {
		t.Fatalf("oversized read returned truncated data %q", buf)
	}
}

func TestReadWithLimitAcceptsExactBoundaryAndUnlimited(t *testing.T) {
	for _, test := range []struct {
		name  string
		limit int64
	}{
		{name: "exact", limit: 6},
		{name: "unlimited", limit: 0},
	} {
		t.Run(test.name, func(t *testing.T) {
			buf, err := readWithLimit(strings.NewReader("123456"), test.limit)
			if err != nil {
				t.Fatal(err)
			}
			if string(buf) != "123456" {
				t.Fatalf("data = %q", buf)
			}
		})
	}
}
