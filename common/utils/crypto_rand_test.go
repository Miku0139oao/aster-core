package utils

import "testing"

func TestCryptoRandomUint64nRange(t *testing.T) {
	for _, max := range []uint64{1, 2, 3, 255, 256, 1000, 1<<63 + 1} {
		for i := 0; i < 1000; i++ {
			value, err := CryptoRandomUint64n(max)
			if err != nil {
				t.Fatal(err)
			}
			if value >= max {
				t.Fatalf("value %d is outside [0, %d)", value, max)
			}
		}
	}
}
