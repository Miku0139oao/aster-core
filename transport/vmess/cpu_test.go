package vmess

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"hash"
	"io"
	"net"
	"testing"
	"time"

	"github.com/gobwas/ws"
	"github.com/gobwas/ws/wsutil"
)

type discardCloser struct {
	io.Writer
}

func (discardCloser) Close() error { return nil }

type nopAddr struct{}

func (nopAddr) Network() string { return "tcp" }
func (nopAddr) String() string  { return "127.0.0.1:0" }

type discardConn struct{}

func (discardConn) Read([]byte) (int, error)         { return 0, io.EOF }
func (discardConn) Write(b []byte) (int, error)      { return len(b), nil }
func (discardConn) Close() error                     { return nil }
func (discardConn) LocalAddr() net.Addr              { return nopAddr{} }
func (discardConn) RemoteAddr() net.Addr             { return nopAddr{} }
func (discardConn) SetDeadline(time.Time) error      { return nil }
func (discardConn) SetReadDeadline(time.Time) error  { return nil }
func (discardConn) SetWriteDeadline(time.Time) error { return nil }

func testPayload(n int) []byte {
	b := make([]byte, n)
	for i := range b {
		b[i] = byte(i)
	}
	return b
}

func newTestGCM() (cipher.AEAD, []byte) {
	key := bytes.Repeat([]byte{0x11}, 16)
	iv := bytes.Repeat([]byte{0x22}, 16)
	block, err := aes.NewCipher(key)
	if err != nil {
		panic(err)
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		panic(err)
	}
	return aead, iv
}

func TestChunkRoundTrip(t *testing.T) {
	payloads := [][]byte{
		testPayload(1),
		testPayload(100),
		testPayload(chunkSize),
		testPayload(chunkSize + 7),
		testPayload(chunkSize * 2),
	}
	for _, payload := range payloads {
		var buf bytes.Buffer
		w := newChunkWriter(discardCloser{&buf})
		if _, err := w.Write(payload); err != nil {
			t.Fatalf("write: %v", err)
		}
		got := make([]byte, len(payload))
		r := newChunkReader(&buf)
		if _, err := io.ReadFull(r, got); err != nil {
			t.Fatalf("read: %v", err)
		}
		if !bytes.Equal(got, payload) {
			t.Fatalf("mismatch len=%d", len(payload))
		}
	}
}

func TestChunkWireLengthPrefix(t *testing.T) {
	payload := testPayload(20)
	var buf bytes.Buffer
	w := newChunkWriter(discardCloser{&buf})
	if _, err := w.Write(payload); err != nil {
		t.Fatal(err)
	}
	raw := buf.Bytes()
	if len(raw) != lenSize+len(payload) {
		t.Fatalf("wire len=%d want=%d", len(raw), lenSize+len(payload))
	}
	if got := int(binary.BigEndian.Uint16(raw[:2])); got != len(payload) {
		t.Fatalf("len prefix=%d want=%d", got, len(payload))
	}
	if !bytes.Equal(raw[2:], payload) {
		t.Fatal("payload mismatch")
	}
}

func TestAEADRoundTrip(t *testing.T) {
	aead, iv := newTestGCM()
	payloads := [][]byte{
		testPayload(1),
		testPayload(100),
		testPayload(1024),
		testPayload(8192),
		testPayload(chunkSize - aead.Overhead()),
		testPayload((chunkSize - aead.Overhead()) + 50),
	}
	for _, payload := range payloads {
		var buf bytes.Buffer
		w := newAEADWriter(&buf, aead, iv)
		r := newAEADReader(&buf, aead, iv)
		if _, err := w.Write(payload); err != nil {
			t.Fatalf("write: %v", err)
		}
		got := make([]byte, len(payload))
		if _, err := io.ReadFull(r, got); err != nil {
			t.Fatalf("read len=%d: %v", len(payload), err)
		}
		if !bytes.Equal(got, payload) {
			t.Fatalf("mismatch len=%d", len(payload))
		}
	}
}

func TestAEADReadLargeDest(t *testing.T) {
	aead, iv := newTestGCM()
	payload := testPayload(1024)
	var buf bytes.Buffer
	w := newAEADWriter(&buf, aead, iv)
	if _, err := w.Write(payload); err != nil {
		t.Fatal(err)
	}
	r := newAEADReader(&buf, aead, iv)
	got := make([]byte, len(payload)+64) // >= ciphertext so decrypt-in-dest is used
	n, err := r.Read(got)
	if err != nil {
		t.Fatal(err)
	}
	if n != len(payload) || !bytes.Equal(got[:n], payload) {
		t.Fatalf("n=%d mismatch", n)
	}
}

func TestAEADReadRejectsShortChunk(t *testing.T) {
	aead, iv := newTestGCM()
	for _, size := range []uint16{0, 1, 8, 15} {
		var raw [2]byte
		binary.BigEndian.PutUint16(raw[:], size)
		r := newAEADReader(bytes.NewReader(raw[:]), aead, iv)
		n, err := r.Read(make([]byte, 64))
		if err == nil || n != 0 {
			t.Fatalf("size=%d n=%d err=%v", size, n, err)
		}
	}
}

func TestAEADReadPartial(t *testing.T) {
	aead, iv := newTestGCM()
	payload := testPayload(1024)
	var buf bytes.Buffer
	w := newAEADWriter(&buf, aead, iv)
	if _, err := w.Write(payload); err != nil {
		t.Fatal(err)
	}
	r := newAEADReader(&buf, aead, iv)
	got := make([]byte, 0, len(payload))
	tmp := make([]byte, 17)
	for len(got) < len(payload) {
		n, err := r.Read(tmp)
		if err != nil {
			t.Fatalf("partial read: %v", err)
		}
		got = append(got, tmp[:n]...)
	}
	if !bytes.Equal(got, payload) {
		t.Fatal("partial read mismatch")
	}
}

// kdfReference is the historical nested-hmac.New implementation.
// Production kdf must match it byte-for-byte (wire protocol).
func kdfReference(key []byte, path ...string) []byte {
	hmacCreator := &hMacCreatorRef{value: []byte(kdfSaltConstVMessAEADKDF)}
	for _, v := range path {
		hmacCreator = &hMacCreatorRef{value: []byte(v), parent: hmacCreator}
	}
	hmacf := hmacCreator.Create()
	hmacf.Write(key)
	return hmacf.Sum(nil)
}

type hMacCreatorRef struct {
	parent *hMacCreatorRef
	value  []byte
}

func (h *hMacCreatorRef) Create() hash.Hash {
	if h.parent == nil {
		return hmac.New(sha256.New, h.value)
	}
	return hmac.New(h.parent.Create, h.value)
}

func TestKDFGolden(t *testing.T) {
	key := []byte{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16}
	got := kdf(key, []byte(kdfSaltConstAuthIDEncryptionKey))
	want, err := hex.DecodeString("d36dc1757471f558494c042fdcab9eae8ec58065f2502579e552760178815412")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("kdf=%x want=%x", got, want)
	}
}

func TestKDFMatchesReference(t *testing.T) {
	key := []byte{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16}
	authID := bytes.Repeat([]byte{0xab}, 16)
	nonce := []byte{9, 8, 7, 6, 5, 4, 3, 2}
	longKey := bytes.Repeat([]byte{0xcd}, 80)
	cases := [][]string{
		{kdfSaltConstAuthIDEncryptionKey},
		{kdfSaltConstAEADRespHeaderLenKey},
		{kdfSaltConstVMessHeaderPayloadLengthAEADKey, string(authID), string(nonce)},
		{kdfSaltConstVMessHeaderPayloadLengthAEADIV, string(authID), string(nonce)},
		{kdfSaltConstVMessHeaderPayloadAEADKey, string(authID), string(nonce)},
		{kdfSaltConstVMessHeaderPayloadAEADIV, string(authID), string(nonce)},
		{string(longKey), string(authID)},
	}
	for _, path := range cases {
		ref := kdfReference(key, path...)
		args := make([][]byte, len(path))
		for i, p := range path {
			args[i] = []byte(p)
		}
		got := kdf(key, args...)
		if !bytes.Equal(got, ref) {
			t.Fatalf("path=%q\ngot %x\nref %x", path, got, ref)
		}
	}
}

func TestSealHeaderLength(t *testing.T) {
	var key [16]byte
	copy(key[:], bytes.Repeat([]byte{0x33}, 16))
	data := testPayload(64)
	out := sealVMessAEADHeader(key, data, time.Unix(1_700_000_000, 0))
	want := 16 + 18 + 8 + len(data) + 16
	if len(out) != want {
		t.Fatalf("len=%d want=%d", len(out), want)
	}
}

func BenchmarkChunkWrite8K(b *testing.B) {
	payload := testPayload(8192)
	w := newChunkWriter(discardCloser{io.Discard})
	b.SetBytes(int64(len(payload)))
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := w.Write(payload); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkChunkWrite16K(b *testing.B) {
	payload := testPayload(16384)
	w := newChunkWriter(discardCloser{io.Discard})
	b.SetBytes(int64(len(payload)))
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := w.Write(payload); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkAEADWrite8K(b *testing.B) {
	aead, iv := newTestGCM()
	payload := testPayload(8192)
	w := newAEADWriter(io.Discard, aead, iv)
	b.SetBytes(int64(len(payload)))
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := w.Write(payload); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkAEADWrite16K(b *testing.B) {
	aead, iv := newTestGCM()
	payload := testPayload(16384)
	w := newAEADWriter(io.Discard, aead, iv)
	b.SetBytes(int64(len(payload)))
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := w.Write(payload); err != nil {
			b.Fatal(err)
		}
	}
}

type resetReader struct {
	data []byte
	off  int
}

func (r *resetReader) Read(p []byte) (int, error) {
	if r.off >= len(r.data) {
		return 0, io.EOF
	}
	n := copy(p, r.data[r.off:])
	r.off += n
	return n, nil
}

func BenchmarkAEADRead8K(b *testing.B) {
	aead, iv := newTestGCM()
	payload := testPayload(8192)
	var encoded bytes.Buffer
	w := newAEADWriter(&encoded, aead, iv)
	if _, err := w.Write(payload); err != nil {
		b.Fatal(err)
	}
	src := &resetReader{data: encoded.Bytes()}
	r := newAEADReader(src, aead, iv)
	out := make([]byte, len(payload))
	b.SetBytes(int64(len(payload)))
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		src.off = 0
		r.count = 0
		r.offset = 0
		r.buf = nil
		if _, err := io.ReadFull(r, out); err != nil {
			b.Fatal(err)
		}
	}
}

func TestWebsocketWriteRoundTrip(t *testing.T) {
	for _, n := range []int{0, 1, 125, 126, 127, 256} {
		var buf bytes.Buffer
		wsc := newWebsocketConn(&bufferConn{Buffer: &buf}, ws.StateClientSide)
		payload := testPayload(n)
		if _, err := wsc.Write(payload); err != nil {
			t.Fatalf("n=%d write: %v", n, err)
		}
		r := wsutil.NewReader(&buf, ws.StateServerSide)
		hdr, err := r.NextFrame()
		if err != nil {
			t.Fatalf("n=%d frame: %v", n, err)
		}
		if hdr.OpCode != ws.OpBinary || !hdr.Fin {
			t.Fatalf("n=%d opcode=%v fin=%v", n, hdr.OpCode, hdr.Fin)
		}
		got, err := io.ReadAll(r)
		if err != nil {
			t.Fatalf("n=%d read: %v", n, err)
		}
		if !bytes.Equal(got, payload) {
			t.Fatalf("n=%d payload mismatch", n)
		}
	}
}

func TestWebsocketWriteMatchesGobwasUnmasked(t *testing.T) {
	for _, n := range []int{0, 1, 125, 126, 127, 256} {
		payload := testPayload(n)
		var ours, theirs bytes.Buffer
		wsc := newWebsocketConn(&bufferConn{Buffer: &ours}, ws.StateServerSide)
		if _, err := wsc.Write(payload); err != nil {
			t.Fatalf("n=%d write: %v", n, err)
		}
		if err := wsutil.WriteMessage(&theirs, ws.StateServerSide, ws.OpBinary, payload); err != nil {
			t.Fatalf("n=%d gobwas: %v", n, err)
		}
		if !bytes.Equal(ours.Bytes(), theirs.Bytes()) {
			t.Fatalf("n=%d wire mismatch\nours  %x\ntheirs %x", n, ours.Bytes(), theirs.Bytes())
		}
	}
}

type bufferConn struct {
	*bytes.Buffer
	discardConn
}

func (c *bufferConn) Read(b []byte) (int, error)  { return c.Buffer.Read(b) }
func (c *bufferConn) Write(b []byte) (int, error) { return c.Buffer.Write(b) }

func BenchmarkSealVMessAEADHeader(b *testing.B) {
	var key [16]byte
	copy(key[:], bytes.Repeat([]byte{0x33}, 16))
	data := testPayload(80)
	ts := time.Unix(1_700_000_000, 0)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = sealVMessAEADHeader(key, data, ts)
	}
}

func BenchmarkWebsocketWrite8K(b *testing.B) {
	wsc := newWebsocketConn(discardConn{}, ws.StateClientSide)
	payload := testPayload(8192)
	b.SetBytes(int64(len(payload)))
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := wsc.Write(payload); err != nil {
			b.Fatal(err)
		}
	}
}
