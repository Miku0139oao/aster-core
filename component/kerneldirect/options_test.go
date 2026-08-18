package kerneldirect

import "testing"

func TestNormalizeMaxEntries(t *testing.T) {
	got, err := NormalizeMaxEntries(0)
	if err != nil {
		t.Fatalf("NormalizeMaxEntries(0) error: %v", err)
	}
	if got != DefaultMaxEntries {
		t.Fatalf("NormalizeMaxEntries(0) = %d, want %d", got, DefaultMaxEntries)
	}

	got, err = NormalizeMaxEntries(8192)
	if err != nil {
		t.Fatalf("NormalizeMaxEntries(8192) error: %v", err)
	}
	if got != 8192 {
		t.Fatalf("NormalizeMaxEntries(8192) = %d, want 8192", got)
	}

	if _, err = NormalizeMaxEntries(MaximumMaxEntries + 1); err == nil {
		t.Fatal("NormalizeMaxEntries(65537) should reject values over MaximumMaxEntries")
	}
}
