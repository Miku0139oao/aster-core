package provider

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

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

func domainMRSFixture(t *testing.T, n int) []byte {
	t.Helper()
	var b strings.Builder
	for i := 0; i < n; i++ {
		fmt.Fprintf(&b, "d%d.example.com\n", i)
	}
	var encoded bytes.Buffer
	if err := ConvertToMrs([]byte(b.String()), P.Domain, P.TextRule, &encoded); err != nil {
		t.Fatal(err)
	}
	return encoded.Bytes()
}

func peekMRSDecodedCap(t *testing.T) int {
	t.Helper()
	slot := mrsDecodedPool.Get().(*[]byte)
	c := cap(*slot)
	mrsDecodedPool.Put(slot)
	return c
}

func scribbleMRSDecodedSlot() {
	slot := mrsDecodedPool.Get().(*[]byte)
	buf := *slot
	buf = buf[:cap(buf)]
	for i := range buf {
		buf[i] = 0xff
	}
	*slot = buf[:0]
	mrsDecodedPool.Put(slot)
}

func TestMRSRecycleKeepsClassAfterLargeThenSmall(t *testing.T) {
	large := domainMRSFixture(t, 8000)
	small := domainMRSFixture(t, 2)
	live, err := rulesMrsParse(large, NewDomainStrategy())
	if err != nil {
		t.Fatal(err)
	}
	if capPeek := peekMRSDecodedCap(t); capPeek == 0 || capPeek > mrsDecodedClassCap {
		t.Fatalf("pool cap after large parse = %d, want 1..%d", capPeek, mrsDecodedClassCap)
	}
	scribbleMRSDecodedSlot()
	if !live.Match(&C.Metadata{Host: "d1234.example.com"}, C.RuleMatchHelper{}) {
		t.Fatal("Match aliased recycled plaintext after large parse")
	}
	smallLive, err := rulesMrsParse(small, NewDomainStrategy())
	if err != nil {
		t.Fatal(err)
	}
	if capPeek := peekMRSDecodedCap(t); capPeek == 0 || capPeek > mrsDecodedClassCap {
		t.Fatalf("pool cap after small parse = %d, want 1..%d", capPeek, mrsDecodedClassCap)
	}
	scribbleMRSDecodedSlot()
	if !live.Match(&C.Metadata{Host: "d0.example.com"}, C.RuleMatchHelper{}) {
		t.Fatal("first strategy lost after second parse")
	}
	if !smallLive.Match(&C.Metadata{Host: "d0.example.com"}, C.RuleMatchHelper{}) {
		t.Fatal("small fixture missing d0.example.com")
	}
	if smallLive.Match(&C.Metadata{Host: "d1234.example.com"}, C.RuleMatchHelper{}) {
		t.Fatal("small strategy observed large-set host; possible buffer alias")
	}
}

func TestMRSErrorRecycleKeepsClass(t *testing.T) {
	_ = domainMRSFixture(t, 8000) // warm ConvertToMrs / decoder
	cases := []struct {
		name string
		buf  []byte
	}{
		{name: "invalid zstd", buf: []byte("not-zstd")},
		{name: "invalid magic", buf: encodeZstd(t, bytes.Repeat([]byte{0x00}, mrsHeaderSize+8))},
		{name: "invalid behavior", buf: encodeRawMRS(t, P.IPCIDR.Byte(), 1, 0, []byte{1})},
		{name: "fromMrs truncated body", buf: encodeRawMRS(t, P.Domain.Byte(), 1, 0, nil)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := NewDomainStrategy()
			s.Reset()
			trieBefore := s.domainTrie
			_, err := rulesMrsParse(tc.buf, s)
			if err == nil {
				t.Fatal("expected parse error")
			}
			if s.domainTrie != trieBefore {
				t.Fatal("failed FromMrs niled domainTrie")
			}
			if capPeek := peekMRSDecodedCap(t); capPeek == 0 || capPeek > mrsDecodedClassCap {
				t.Fatalf("pool cap after error = %d, want 1..%d", capPeek, mrsDecodedClassCap)
			}
		})
	}
}

func TestMRSFromMrsNilsTrieOnlyOnSuccess(t *testing.T) {
	s := NewDomainStrategy()
	s.Reset()
	if s.domainTrie == nil {
		t.Fatal("Reset should install a builder trie")
	}
	if _, err := rulesMrsParse(domainMRSFixture(t, 4), s); err != nil {
		t.Fatal(err)
	}
	if s.domainTrie != nil {
		t.Fatal("successful FromMrs left domainTrie")
	}
	if !s.Match(&C.Metadata{Host: "d1.example.com"}, C.RuleMatchHelper{}) {
		t.Fatal("expected MRS domain hit")
	}
}

func TestMRSParseFailureDoesNotReplaceSnapshot(t *testing.T) {
	old, err := rulesMrsParse(domainMRSFixture(t, 4), NewDomainStrategy())
	if err != nil {
		t.Fatal(err)
	}
	var bp baseProvider
	bp.setStrategy(old)
	_, err = rulesMrsParse([]byte("not-zstd"), NewDomainStrategy())
	if err == nil {
		t.Fatal("expected detached parse failure")
	}
	if !bp.Match(&C.Metadata{Host: "d0.example.com"}, C.RuleMatchHelper{}) {
		t.Fatal("failed parse replaced published snapshot")
	}
}

func TestMRSMatchAndPublishDoNotAcquireParseMutex(t *testing.T) {
	live, err := rulesMrsParse(domainMRSFixture(t, 4), NewDomainStrategy())
	if err != nil {
		t.Fatal(err)
	}
	var bp baseProvider
	bp.setStrategy(live)

	mrsParseMu.Lock()
	defer mrsParseMu.Unlock()
	bp.setStrategy(live)
	done := make(chan struct{})
	go func() {
		if !bp.Match(&C.Metadata{Host: "d0.example.com"}, C.RuleMatchHelper{}) {
			t.Error("Match missed while parse mutex held")
		}
		_ = bp.Count()
		_ = bp.Strategy()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Match/Count/Strategy blocked on mrsParseMu")
	}
}

func TestMRSParseConcurrentWithMatch(t *testing.T) {
	live, err := rulesMrsParse(domainMRSFixture(t, 8), NewDomainStrategy())
	if err != nil {
		t.Fatal(err)
	}
	var bp baseProvider
	bp.setStrategy(live)
	large := domainMRSFixture(t, 8000)
	invalid := []byte("not-zstd")
	truncated := encodeRawMRS(t, P.Domain.Byte(), 1, 0, nil)

	var wg sync.WaitGroup
	errCh := make(chan error, 2)
	wg.Add(3)
	go func() {
		defer wg.Done()
		for i := 0; i < 8; i++ {
			s := NewDomainStrategy()
			s.Reset()
			if _, err := rulesMrsParse(large, s); err != nil {
				errCh <- err
				return
			}
			_, _ = rulesMrsParse(invalid, NewDomainStrategy())
			_, _ = rulesMrsParse(truncated, NewDomainStrategy())
		}
	}()
	for i := 0; i < 2; i++ {
		go func() {
			defer wg.Done()
			deadline := time.Now().Add(400 * time.Millisecond)
			for time.Now().Before(deadline) {
				if !bp.Match(&C.Metadata{Host: "d0.example.com"}, C.RuleMatchHelper{}) {
					errCh <- errors.New("live snapshot missed during parse")
					return
				}
			}
		}()
	}
	wg.Wait()
	close(errCh)
	for err := range errCh {
		if err != nil {
			t.Fatal(err)
		}
	}
}

func TestWriteL13MRSReloadFixtures(t *testing.T) {
	dir := os.Getenv("L13_MRS_FIXTURE_DIR")
	if dir == "" {
		t.Skip("set L13_MRS_FIXTURE_DIR to write parent A/B fixtures")
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	const n = 50000
	var b strings.Builder
	for i := 0; i < n; i++ {
		fmt.Fprintf(&b, "d%d.example.com\n", i)
	}
	text := []byte(b.String())
	if err := os.WriteFile(filepath.Join(dir, "domains-50000.txt"), text, 0o644); err != nil {
		t.Fatal(err)
	}
	var encoded bytes.Buffer
	if err := ConvertToMrs(text, P.Domain, P.TextRule, &encoded); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "domains-50000.mrs"), encoded.Bytes(), 0o644); err != nil {
		t.Fatal(err)
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
