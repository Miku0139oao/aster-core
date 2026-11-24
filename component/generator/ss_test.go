package generator

import (
	"encoding/base64"
	"testing"
)

func TestGenShadowsocksPassword(t *testing.T) {
	for _, bits := range []int{128, 256, 512} {
		password, err := GenShadowsocksPassword(bits)
		if err != nil {
			t.Fatalf("GenShadowsocksPassword(%d): %v", bits, err)
		}
		decoded, err := base64.StdEncoding.DecodeString(password)
		if err != nil {
			t.Fatalf("GenShadowsocksPassword(%d) returned invalid base64: %v", bits, err)
		}
		if len(decoded) != bits/8 {
			t.Fatalf("GenShadowsocksPassword(%d) decoded to %d bytes, want %d", bits, len(decoded), bits/8)
		}
	}

	other, err := GenShadowsocksPassword(256)
	if err != nil {
		t.Fatalf("GenShadowsocksPassword(256): %v", err)
	}
	first, err := GenShadowsocksPassword(256)
	if err != nil {
		t.Fatalf("GenShadowsocksPassword(256): %v", err)
	}
	if first == other {
		t.Fatal("GenShadowsocksPassword returned the same password twice")
	}

	if _, err := GenShadowsocksPassword(100); err == nil {
		t.Fatal("GenShadowsocksPassword(100) did not return an error")
	}
}
