package shadowaead

import (
	"bytes"
	"crypto/rand"
	"crypto/sha1"
	"io"
	"testing"

	"golang.org/x/crypto/hkdf"
)

func TestHkdfSHA1MatchesXCrypto(t *testing.T) {
	info := []byte("ss-subkey")
	cases := []struct {
		secret []byte
		salt   []byte
		info   []byte
		outLen int
	}{
		{bytes.Repeat([]byte{0x11}, 16), bytes.Repeat([]byte{0x22}, 16), info, 16},
		{bytes.Repeat([]byte{0x11}, 24), bytes.Repeat([]byte{0x22}, 16), info, 24},
		{bytes.Repeat([]byte{0x11}, 32), bytes.Repeat([]byte{0x22}, 32), info, 32},
		{bytes.Repeat([]byte{0x11}, 16), nil, info, 16},
		{bytes.Repeat([]byte{0xaa}, 32), bytes.Repeat([]byte{0xbb}, 20), []byte("x"), 1},
		{bytes.Repeat([]byte{0xaa}, 32), bytes.Repeat([]byte{0xbb}, 20), info, 20},
		{bytes.Repeat([]byte{0xaa}, 32), bytes.Repeat([]byte{0xbb}, 20), info, 40},
	}
	for i, tc := range cases {
		got := make([]byte, tc.outLen)
		hkdfSHA1(tc.secret, tc.salt, tc.info, got)

		want := make([]byte, tc.outLen)
		r := hkdf.New(sha1.New, tc.secret, tc.salt, tc.info)
		if _, err := io.ReadFull(r, want); err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(got, want) {
			t.Fatalf("case %d mismatch\n got %x\nwant %x", i, got, want)
		}
	}
}

func TestPackUnpackRoundTrip(t *testing.T) {
	plain := bytes.Repeat([]byte{0x7a}, 1250)
	for _, n := range []int{16, 24, 32} {
		psk := bytes.Repeat([]byte{byte(n)}, n)
		ciph, err := AESGCM(psk)
		if err != nil {
			t.Fatal(err)
		}
		dst := make([]byte, ciph.SaltSize()+len(plain)+32)
		packed, err := Pack(dst, plain, ciph)
		if err != nil {
			t.Fatal(err)
		}
		out := make([]byte, len(plain))
		got, err := Unpack(out, packed, ciph)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(got, plain) {
			t.Fatalf("aes-%d-gcm roundtrip mismatch", n*8)
		}
	}

	psk := bytes.Repeat([]byte{0x42}, 32)
	ciph, err := Chacha20Poly1305(psk)
	if err != nil {
		t.Fatal(err)
	}
	dst := make([]byte, ciph.SaltSize()+len(plain)+32)
	packed, err := Pack(dst, plain, ciph)
	if err != nil {
		t.Fatal(err)
	}
	out := make([]byte, len(plain))
	got, err := Unpack(out, packed, ciph)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, plain) {
		t.Fatal("chacha20-poly1305 roundtrip mismatch")
	}
}

func TestEncrypterConcurrent(t *testing.T) {
	ciph := newAES128GCM(t)
	salt := bytes.Repeat([]byte{0x33}, ciph.SaltSize())
	plain := []byte("concurrent-aead")
	const n = 64
	errCh := make(chan error, n)
	for i := 0; i < n; i++ {
		go func() {
			aead, err := ciph.Encrypter(salt)
			if err != nil {
				errCh <- err
				return
			}
			nonce := make([]byte, aead.NonceSize())
			ct := aead.Seal(nil, nonce, plain, nil)
			got, err := aead.Open(nil, nonce, ct, nil)
			if err != nil {
				errCh <- err
				return
			}
			if !bytes.Equal(got, plain) {
				errCh <- io.ErrUnexpectedEOF
				return
			}
			errCh <- nil
		}()
	}
	for i := 0; i < n; i++ {
		if err := <-errCh; err != nil {
			t.Fatal(err)
		}
	}
}

func TestEncrypterUsableAfterPoolReuse(t *testing.T) {
	for _, mk := range []struct {
		name string
		new  func() (Cipher, error)
	}{
		{"aes-128-gcm", func() (Cipher, error) {
			return AESGCM(bytes.Repeat([]byte{0x09}, 16))
		}},
		{"chacha20-poly1305", func() (Cipher, error) {
			return Chacha20Poly1305(bytes.Repeat([]byte{0x42}, 32))
		}},
	} {
		t.Run(mk.name, func(t *testing.T) {
			ciph, err := mk.new()
			if err != nil {
				t.Fatal(err)
			}
			salt := bytes.Repeat([]byte{0x33}, ciph.SaltSize())
			aead, err := ciph.Encrypter(salt)
			if err != nil {
				t.Fatal(err)
			}
			nonce := make([]byte, aead.NonceSize())
			plain := []byte("after-pool-put")
			ct := aead.Seal(nil, nonce, plain, nil)
			for i := 0; i < 32; i++ {
				if _, err := ciph.Encrypter(salt); err != nil {
					t.Fatal(err)
				}
			}
			got, err := aead.Open(nil, nonce, ct, nil)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(got, plain) {
				t.Fatalf("aead key corrupted after scratch reuse: got %q", got)
			}
		})
	}
}

func newAES128GCM(tb testing.TB) Cipher {
	tb.Helper()
	psk := bytes.Repeat([]byte{0x09}, 16)
	ciph, err := AESGCM(psk)
	if err != nil {
		tb.Fatal(err)
	}
	return ciph
}

func BenchmarkEncrypter(b *testing.B) {
	ciph := newAES128GCM(b)
	salt := make([]byte, ciph.SaltSize())
	if _, err := rand.Read(salt); err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		aead, err := ciph.Encrypter(salt)
		if err != nil {
			b.Fatal(err)
		}
		if aead == nil {
			b.Fatal("nil aead")
		}
	}
}

func BenchmarkPack(b *testing.B) {
	ciph := newAES128GCM(b)
	plain := bytes.Repeat([]byte{0x01}, 1250)
	dst := make([]byte, ciph.SaltSize()+len(plain)+32)
	b.ReportAllocs()
	b.SetBytes(int64(len(plain)))
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := Pack(dst, plain, ciph); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkUnpack(b *testing.B) {
	ciph := newAES128GCM(b)
	plain := bytes.Repeat([]byte{0x01}, 1250)
	packed, err := Pack(make([]byte, ciph.SaltSize()+len(plain)+32), plain, ciph)
	if err != nil {
		b.Fatal(err)
	}
	// Pack returns a slice of the dst buffer; copy so Unpack does not race the fixture.
	pkt := append([]byte(nil), packed...)
	out := make([]byte, len(plain))
	b.ReportAllocs()
	b.SetBytes(int64(len(plain)))
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := Unpack(out, pkt, ciph); err != nil {
			b.Fatal(err)
		}
	}
}
