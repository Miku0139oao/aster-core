package shadowaead

import (
	"bytes"
	"crypto/rand"
	"io"
	"net"
	"testing"
)

func TestConnRoundTrip(t *testing.T) {
	psk := bytes.Repeat([]byte{0x09}, 16)
	ciph, err := AESGCM(psk)
	if err != nil {
		t.Fatal(err)
	}
	c1, c2 := net.Pipe()
	defer c1.Close()
	defer c2.Close()

	client := NewConn(c1, ciph)
	server := NewConn(c2, ciph)

	payload := bytes.Repeat([]byte("ss-aead"), 2048)
	errCh := make(chan error, 1)
	go func() {
		buf := make([]byte, len(payload))
		_, err := io.ReadFull(server, buf)
		if err != nil {
			errCh <- err
			return
		}
		if !bytes.Equal(buf, payload) {
			errCh <- io.ErrUnexpectedEOF
			return
		}
		errCh <- nil
	}()
	if _, err := client.Write(payload); err != nil {
		t.Fatal(err)
	}
	if err := <-errCh; err != nil {
		t.Fatal(err)
	}
}

func BenchmarkStreamWrite(b *testing.B) {
	psk := bytes.Repeat([]byte{0x09}, 16)
	ciph, err := AESGCM(psk)
	if err != nil {
		b.Fatal(err)
	}
	salt := make([]byte, ciph.SaltSize())
	if _, err := rand.Read(salt); err != nil {
		b.Fatal(err)
	}
	aead, err := ciph.Encrypter(salt)
	if err != nil {
		b.Fatal(err)
	}
	w := NewWriter(io.Discard, aead)
	p := make([]byte, 16*1024)
	b.ReportAllocs()
	b.SetBytes(int64(len(p)))
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := w.Write(p); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkStreamRead(b *testing.B) {
	psk := bytes.Repeat([]byte{0x09}, 16)
	ciph, err := AESGCM(psk)
	if err != nil {
		b.Fatal(err)
	}
	salt := make([]byte, ciph.SaltSize())
	if _, err := rand.Read(salt); err != nil {
		b.Fatal(err)
	}
	aead, err := ciph.Encrypter(salt)
	if err != nil {
		b.Fatal(err)
	}
	payload := bytes.Repeat([]byte{0x5a}, 16*1024)
	var enc bytes.Buffer
	if _, err := NewWriter(&enc, aead).Write(payload); err != nil {
		b.Fatal(err)
	}
	wire := enc.Bytes()
	decAEAD, err := ciph.Decrypter(salt)
	if err != nil {
		b.Fatal(err)
	}
	dst := make([]byte, len(payload))
	b.ReportAllocs()
	b.SetBytes(int64(len(payload)))
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		r := NewReader(bytes.NewReader(wire), decAEAD)
		if _, err := io.ReadFull(r, dst); err != nil {
			b.Fatal(err)
		}
	}
}
