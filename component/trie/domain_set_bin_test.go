package trie

import (
	"bytes"
	"encoding/binary"
	"strconv"
	"strings"
	"testing"
)

func TestDomainSetBinaryRoundTrip(t *testing.T) {
	tree := New[struct{}]()
	for _, domain := range []string{"example.com", "+.example.org", "*.internal", "中文.example"} {
		if err := tree.Insert(domain, struct{}{}); err != nil {
			t.Fatal(err)
		}
	}
	original := tree.NewDomainSet()
	var encoded bytes.Buffer
	if err := original.WriteBin(&encoded); err != nil {
		t.Fatal(err)
	}
	decoded, err := ReadDomainSetBin(&encoded)
	if err != nil {
		t.Fatal(err)
	}
	for _, domain := range []string{"example.com", "www.example.org", "x.internal", "中文.example"} {
		if original.Has(domain) != decoded.Has(domain) {
			t.Fatalf("round-trip mismatch for %q", domain)
		}
	}
}

func TestReadDomainSetBinRejectsMalformedTree(t *testing.T) {
	tests := []struct {
		name        string
		leaves      []uint64
		labelBitmap []uint64
		labels      []byte
	}{
		{name: "disconnected", leaves: []uint64{2}, labelBitmap: []uint64{5}, labels: []byte{'a'}},
		{name: "node label mismatch", leaves: []uint64{2}, labelBitmap: []uint64{7}, labels: []byte{'a'}},
		{name: "leaf padding", leaves: []uint64{2 | 1<<63}, labelBitmap: []uint64{6}, labels: []byte{'a'}},
		{name: "no terminal", leaves: []uint64{0}, labelBitmap: []uint64{6}, labels: []byte{'a'}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var encoded bytes.Buffer
			encoded.WriteByte(1)
			writeDomainSetSection(t, &encoded, tc.leaves)
			writeDomainSetSection(t, &encoded, tc.labelBitmap)
			if err := binary.Write(&encoded, binary.BigEndian, int64(len(tc.labels))); err != nil {
				t.Fatal(err)
			}
			encoded.Write(tc.labels)
			if _, err := ReadDomainSetBin(&encoded); err == nil {
				t.Fatal("malformed domain set was accepted")
			}
		})
	}
}

func TestDomainSetWriteBinMatchesBinaryPackage(t *testing.T) {
	tree := New[struct{}]()
	for _, domain := range []string{"example.com", "+.example.org", "*.internal", "中文.example"} {
		if err := tree.Insert(domain, struct{}{}); err != nil {
			t.Fatal(err)
		}
	}
	set := tree.NewDomainSet()
	var got bytes.Buffer
	if err := set.WriteBin(&got); err != nil {
		t.Fatal(err)
	}
	var want bytes.Buffer
	want.WriteByte(1)
	writeDomainSetSection(t, &want, set.leaves)
	writeDomainSetSection(t, &want, set.labelBitmap)
	if err := binary.Write(&want, binary.BigEndian, int64(len(set.labels))); err != nil {
		t.Fatal(err)
	}
	want.Write(set.labels)
	if !bytes.Equal(got.Bytes(), want.Bytes()) {
		t.Fatal("WriteBin wire format changed")
	}
}

func TestReadDomainSetBinRejectsOversizedLengthBeforeAllocation(t *testing.T) {
	var encoded bytes.Buffer
	encoded.WriteByte(1)
	if err := binary.Write(&encoded, binary.BigEndian, int64((maxBinaryDomainLabels+64)/64+1)); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadDomainSetBin(&encoded); err == nil || !strings.Contains(err.Error(), "leaf bitmap length") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func writeDomainSetSection(t *testing.T, buffer *bytes.Buffer, values []uint64) {
	t.Helper()
	if err := binary.Write(buffer, binary.BigEndian, int64(len(values))); err != nil {
		t.Fatal(err)
	}
	for _, value := range values {
		if err := binary.Write(buffer, binary.BigEndian, value); err != nil {
			t.Fatal(err)
		}
	}
}

func benchDomainSet(b *testing.B, n int) *DomainSet {
	b.Helper()
	tree := New[struct{}]()
	for i := 0; i < n; i++ {
		domain := "+." + strconv.Itoa(i) + ".example.com"
		if err := tree.Insert(domain, struct{}{}); err != nil {
			b.Fatal(err)
		}
	}
	set := tree.NewDomainSet()
	if set == nil {
		b.Fatal("empty domain set")
	}
	return set
}

func BenchmarkDomainSetWriteBin(b *testing.B) {
	set := benchDomainSet(b, 10000)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		var encoded bytes.Buffer
		if err := set.WriteBin(&encoded); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkReadDomainSetBin(b *testing.B) {
	set := benchDomainSet(b, 10000)
	var encoded bytes.Buffer
	if err := set.WriteBin(&encoded); err != nil {
		b.Fatal(err)
	}
	payload := encoded.Bytes()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := ReadDomainSetBin(bytes.NewReader(payload)); err != nil {
			b.Fatal(err)
		}
	}
}
