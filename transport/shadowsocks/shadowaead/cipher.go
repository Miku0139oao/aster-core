package shadowaead

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/sha1"
	"hash"
	"strconv"
	"sync"

	"github.com/metacubex/chacha"
	"gitlab.com/go-extension/aes-ccm"
	"golang.org/x/crypto/chacha20poly1305"
)

type Cipher interface {
	KeySize() int
	SaltSize() int
	Encrypter(salt []byte) (cipher.AEAD, error)
	Decrypter(salt []byte) (cipher.AEAD, error)
}

type KeySizeError int

func (e KeySizeError) Error() string {
	return "key size error: need " + strconv.Itoa(int(e)) + " bytes"
}

// ssSubkeyInfo is the Shadowsocks AEAD HKDF info string. Kept as a package
// variable so Encrypter/Decrypter do not allocate []byte("ss-subkey") per packet.
var ssSubkeyInfo = []byte("ss-subkey")

// hkdfScratch holds reusable HKDF-SHA1 state. hash.Write/Sum make stack arrays
// escape, so per-packet Encrypter would otherwise heap-allocate every buffer.
type hkdfScratch struct {
	h      hash.Hash
	prk    [sha1.Size]byte
	t      [sha1.Size]byte
	block  [sha1.BlockSize]byte
	ipad   [sha1.BlockSize]byte
	opad   [sha1.BlockSize]byte
	inner  [sha1.Size]byte
	msg    [sha1.Size + 64 + 1]byte
	subkey [32]byte
}

var hkdfScratchPool = sync.Pool{
	New: func() any {
		return &hkdfScratch{h: sha1.New()}
	},
}

func putHkdfScratch(s *hkdfScratch) {
	s.h.Reset()
	s.prk = [sha1.Size]byte{}
	s.t = [sha1.Size]byte{}
	s.block = [sha1.BlockSize]byte{}
	s.ipad = [sha1.BlockSize]byte{}
	s.opad = [sha1.BlockSize]byte{}
	s.inner = [sha1.Size]byte{}
	s.msg = [sha1.Size + 64 + 1]byte{}
	s.subkey = [32]byte{}
	hkdfScratchPool.Put(s)
}

func hkdfSHA1(secret, salt, info, outkey []byte) {
	s := hkdfScratchPool.Get().(*hkdfScratch)
	hkdfSHA1Into(s, secret, salt, info, outkey)
	putHkdfScratch(s)
}

func hkdfSHA1Into(s *hkdfScratch, secret, salt, info, outkey []byte) {
	hmacSHA1Into(s, s.prk[:], salt, secret)

	var prev []byte
	counter := byte(1)
	dst := outkey
	for len(dst) > 0 {
		if len(prev)+len(info)+1 > len(s.msg) {
			panic("hkdf-sha1: info too long")
		}
		n := copy(s.msg[:], prev)
		n += copy(s.msg[n:], info)
		s.msg[n] = counter
		n++
		hmacSHA1Into(s, s.t[:], s.prk[:], s.msg[:n])
		copied := copy(dst, s.t[:])
		dst = dst[copied:]
		prev = s.t[:]
		counter++
		if counter == 0 {
			panic("hkdf-sha1: output too long")
		}
	}
}

func hmacSHA1Into(s *hkdfScratch, out, key, data []byte) {
	if cap(out) < sha1.Size {
		panic("hmac-sha1: short output")
	}
	s.block = [sha1.BlockSize]byte{}
	if len(key) > sha1.BlockSize {
		s.h.Reset()
		s.h.Write(key)
		hashed := s.h.Sum(s.block[:0])
		copy(s.block[:sha1.Size], hashed)
		for i := sha1.Size; i < len(s.block); i++ {
			s.block[i] = 0
		}
	} else {
		copy(s.block[:], key)
	}

	for i := 0; i < sha1.BlockSize; i++ {
		s.ipad[i] = s.block[i] ^ 0x36
		s.opad[i] = s.block[i] ^ 0x5c
	}

	s.h.Reset()
	s.h.Write(s.ipad[:])
	s.h.Write(data)
	inner := s.h.Sum(s.inner[:0])
	s.h.Reset()
	s.h.Write(s.opad[:])
	s.h.Write(inner)
	sum := s.h.Sum(out[:0])
	if copy(out, sum) < sha1.Size {
		panic("hmac-sha1: short output")
	}
}

type metaCipher struct {
	psk      []byte
	makeAEAD func(key []byte) (cipher.AEAD, error)
}

func (a *metaCipher) KeySize() int { return len(a.psk) }
func (a *metaCipher) SaltSize() int {
	if ks := a.KeySize(); ks > 16 {
		return ks
	}
	return 16
}

func (a *metaCipher) Encrypter(salt []byte) (cipher.AEAD, error) {
	return a.aeadFromSalt(salt)
}

func (a *metaCipher) Decrypter(salt []byte) (cipher.AEAD, error) {
	return a.aeadFromSalt(salt)
}

func (a *metaCipher) aeadFromSalt(salt []byte) (cipher.AEAD, error) {
	s := hkdfScratchPool.Get().(*hkdfScratch)
	ks := a.KeySize()
	if ks > len(s.subkey) {
		putHkdfScratch(s)
		return nil, KeySizeError(ks)
	}
	subkey := s.subkey[:ks]
	hkdfSHA1Into(s, a.psk, salt, ssSubkeyInfo, subkey)
	aead, err := a.makeAEAD(subkey)
	putHkdfScratch(s)
	return aead, err
}

func aesGCM(key []byte) (cipher.AEAD, error) {
	blk, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(blk)
}

// AESGCM creates a new Cipher with a pre-shared key. len(psk) must be
// one of 16, 24, or 32 to select AES-128/196/256-GCM.
func AESGCM(psk []byte) (Cipher, error) {
	switch l := len(psk); l {
	case 16, 24, 32: // AES 128/196/256
	default:
		return nil, aes.KeySizeError(l)
	}
	return &metaCipher{psk: psk, makeAEAD: aesGCM}, nil
}

func aesCCM(key []byte) (cipher.AEAD, error) {
	blk, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	return ccm.NewCCM(blk)
}

// AESCCM creates a new Cipher with a pre-shared key. len(psk) must be
// one of 16, 24, or 32 to select AES-128/196/256-GCM.
func AESCCM(psk []byte) (Cipher, error) {
	switch l := len(psk); l {
	case 16, 24, 32: // AES 128/196/256
	default:
		return nil, aes.KeySizeError(l)
	}
	return &metaCipher{psk: psk, makeAEAD: aesCCM}, nil
}

// Chacha20Poly1305 creates a new Cipher with a pre-shared key. len(psk)
// must be 32.
func Chacha20Poly1305(psk []byte) (Cipher, error) {
	if len(psk) != chacha20poly1305.KeySize {
		return nil, KeySizeError(chacha20poly1305.KeySize)
	}
	return &metaCipher{psk: psk, makeAEAD: chacha20poly1305.New}, nil
}

// XChacha20Poly1305 creates a new Cipher with a pre-shared key. len(psk)
// must be 32.
func XChacha20Poly1305(psk []byte) (Cipher, error) {
	if len(psk) != chacha20poly1305.KeySize {
		return nil, KeySizeError(chacha20poly1305.KeySize)
	}
	return &metaCipher{psk: psk, makeAEAD: chacha20poly1305.NewX}, nil
}

// Chacha8Poly1305 creates a new Cipher with a pre-shared key. len(psk)
// must be 32.
func Chacha8Poly1305(psk []byte) (Cipher, error) {
	if len(psk) != chacha.KeySize {
		return nil, KeySizeError(chacha.KeySize)
	}
	return &metaCipher{psk: psk, makeAEAD: chacha.NewChaCha8IETFPoly1305}, nil
}

// XChacha8Poly1305 creates a new Cipher with a pre-shared key. len(psk)
// must be 32.
func XChacha8Poly1305(psk []byte) (Cipher, error) {
	if len(psk) != chacha.KeySize {
		return nil, KeySizeError(chacha.KeySize)
	}
	return &metaCipher{psk: psk, makeAEAD: chacha.NewXChaCha20IETFPoly1305}, nil
}
