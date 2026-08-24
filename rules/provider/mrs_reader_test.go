package provider

import (
	"bytes"
	"encoding/binary"
	"errors"
	"io"
	"testing"

	C "github.com/Miku0139oao/aster-core/constant"
	P "github.com/Miku0139oao/aster-core/constant/provider"

	"github.com/klauspost/compress/zstd"
)

func TestRulesMrsParseRoundTrip(t *testing.T) {
	var encoded bytes.Buffer
	if err := ConvertToMrs([]byte("example.com\n+.example.org\n"), P.Domain, P.TextRule, &encoded); err != nil {
		t.Fatal(err)
	}
	strategy, err := rulesMrsParse(encoded.Bytes(), NewDomainStrategy())
	if err != nil {
		t.Fatal(err)
	}
	for _, host := range []string{"example.com", "www.example.org"} {
		if !strategy.Match(&C.Metadata{Host: host}, C.RuleMatchHelper{}) {
			t.Fatalf("MRS strategy does not match %q", host)
		}
	}
}

func TestRulesMrsParseRejectsInvalidCountsAndReservedLength(t *testing.T) {
	tests := []struct {
		name     string
		count    int64
		reserved int64
	}{
		{name: "negative count", count: -1},
		{name: "oversized count", count: maxMRSRuleCount + 1},
		{name: "negative reserved", count: 1, reserved: -1},
		{name: "oversized reserved", count: 1, reserved: maxMRSReservedBytes + 1},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			encoded := encodeRawMRS(t, P.Domain.Byte(), tc.count, tc.reserved, nil)
			if _, err := rulesMrsParse(encoded, NewDomainStrategy()); err == nil {
				t.Fatal("invalid MRS header was accepted")
			}
		})
	}
}

func TestRulesMrsParseRejectsTrailingData(t *testing.T) {
	var valid bytes.Buffer
	if err := ConvertToMrs([]byte("example.com\n"), P.Domain, P.TextRule, &valid); err != nil {
		t.Fatal(err)
	}
	decoder, err := zstd.NewReader(bytes.NewReader(valid.Bytes()))
	if err != nil {
		t.Fatal(err)
	}
	raw, err := io.ReadAll(decoder)
	decoder.Close()
	if err != nil {
		t.Fatal(err)
	}
	raw = append(raw, 0xff)
	encoded := encodeZstd(t, raw)
	if _, err := rulesMrsParse(encoded, NewDomainStrategy()); err == nil {
		t.Fatal("MRS trailing data was accepted")
	}
}

func TestBoundedMRSReaderDetectsOverflow(t *testing.T) {
	reader := &boundedMRSReader{reader: bytes.NewReader([]byte{1, 2}), remaining: 1}
	var first [1]byte
	if _, err := io.ReadFull(reader, first[:]); err != nil {
		t.Fatal(err)
	}
	if _, err := reader.Read(first[:]); !errors.Is(err, errMRSDecodedTooLarge) {
		t.Fatalf("unexpected overflow error: %v", err)
	}
}

func encodeRawMRS(t *testing.T, behavior byte, count, reserved int64, body []byte) []byte {
	t.Helper()
	var raw bytes.Buffer
	raw.Write(MrsMagicBytes[:])
	raw.WriteByte(behavior)
	if err := binary.Write(&raw, binary.BigEndian, count); err != nil {
		t.Fatal(err)
	}
	if err := binary.Write(&raw, binary.BigEndian, reserved); err != nil {
		t.Fatal(err)
	}
	raw.Write(body)
	return encodeZstd(t, raw.Bytes())
}

func encodeZstd(t *testing.T, raw []byte) []byte {
	t.Helper()
	var encoded bytes.Buffer
	writer, err := zstd.NewWriter(&encoded)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := writer.Write(raw); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	return encoded.Bytes()
}
