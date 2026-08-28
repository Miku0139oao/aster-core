package vmess

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"hash/crc32"
	"time"
)

const (
	kdfSaltConstAuthIDEncryptionKey             = "AES Auth ID Encryption"
	kdfSaltConstAEADRespHeaderLenKey            = "AEAD Resp Header Len Key"
	kdfSaltConstAEADRespHeaderLenIV             = "AEAD Resp Header Len IV"
	kdfSaltConstAEADRespHeaderPayloadKey        = "AEAD Resp Header Key"
	kdfSaltConstAEADRespHeaderPayloadIV         = "AEAD Resp Header IV"
	kdfSaltConstVMessAEADKDF                    = "VMess AEAD KDF"
	kdfSaltConstVMessHeaderPayloadAEADKey       = "VMess Header AEAD Key"
	kdfSaltConstVMessHeaderPayloadAEADIV        = "VMess Header AEAD Nonce"
	kdfSaltConstVMessHeaderPayloadLengthAEADKey = "VMess Header AEAD Key_Length"
	kdfSaltConstVMessHeaderPayloadLengthAEADIV  = "VMess Header AEAD Nonce_Length"
)

var (
	kdfSaltVMessAEADKDF                    = []byte(kdfSaltConstVMessAEADKDF)
	kdfSaltAuthIDEncryptionKey             = []byte(kdfSaltConstAuthIDEncryptionKey)
	kdfSaltAEADRespHeaderLenKey            = []byte(kdfSaltConstAEADRespHeaderLenKey)
	kdfSaltAEADRespHeaderLenIV             = []byte(kdfSaltConstAEADRespHeaderLenIV)
	kdfSaltAEADRespHeaderPayloadKey        = []byte(kdfSaltConstAEADRespHeaderPayloadKey)
	kdfSaltAEADRespHeaderPayloadIV         = []byte(kdfSaltConstAEADRespHeaderPayloadIV)
	kdfSaltVMessHeaderPayloadAEADKey       = []byte(kdfSaltConstVMessHeaderPayloadAEADKey)
	kdfSaltVMessHeaderPayloadAEADIV        = []byte(kdfSaltConstVMessHeaderPayloadAEADIV)
	kdfSaltVMessHeaderPayloadLengthAEADKey = []byte(kdfSaltConstVMessHeaderPayloadLengthAEADKey)
	kdfSaltVMessHeaderPayloadLengthAEADIV  = []byte(kdfSaltConstVMessHeaderPayloadLengthAEADIV)
)

const kdfMaxPath = 8

// kdf implements VMess AEAD KDF (nested HMAC-SHA256) without the exponential
// hmac.New(parent.Create) allocations. Output matches the historical nested
// hmac.New construction byte-for-byte.
func kdf(key []byte, path ...[]byte) []byte {
	out := make([]byte, sha256.Size)
	kdfTo(out, key, path...)
	return out
}

func kdfTo(dst, key []byte, path ...[]byte) {
	n := 1 + len(path)
	if n > kdfMaxPath {
		panic("vmess: kdf path too long")
	}
	var keys [kdfMaxPath][]byte
	keys[0] = kdfSaltVMessAEADKDF
	for i, p := range path {
		keys[i+1] = p
	}
	kdfEval(n, keys[:n], key, dst)
}

func kdfEval(level int, keys [][]byte, msg, dst []byte) {
	if level == 0 {
		sum := sha256.Sum256(msg)
		copy(dst, sum[:])
		return
	}

	macKey := keys[level-1]
	var hashedKey [sha256.Size]byte
	if len(macKey) > sha256.BlockSize {
		kdfEval(level-1, keys, macKey, hashedKey[:])
		macKey = hashedKey[:]
	}
	var ipad, opad [sha256.BlockSize]byte
	copy(ipad[:], macKey)
	copy(opad[:], macKey)
	for i := 0; i < sha256.BlockSize; i++ {
		ipad[i] ^= 0x36
		opad[i] ^= 0x5c
	}

	var innerBuf [512]byte
	innerLen := sha256.BlockSize + len(msg)
	var innerIn []byte
	if innerLen <= len(innerBuf) {
		innerIn = innerBuf[:innerLen]
	} else {
		innerIn = make([]byte, innerLen)
	}
	copy(innerIn, ipad[:])
	copy(innerIn[sha256.BlockSize:], msg)

	var innerSum [sha256.Size]byte
	kdfEval(level-1, keys, innerIn, innerSum[:])

	var outerBuf [sha256.BlockSize + sha256.Size]byte
	copy(outerBuf[:], opad[:])
	copy(outerBuf[sha256.BlockSize:], innerSum[:])
	kdfEval(level-1, keys, outerBuf[:], dst)
}

func createAuthID(cmdKey []byte, time int64) [16]byte {
	var buf [16]byte
	binary.BigEndian.PutUint64(buf[:8], uint64(time))
	rand.Read(buf[8:12])
	binary.BigEndian.PutUint32(buf[12:], crc32.ChecksumIEEE(buf[:12]))

	var derived [sha256.Size]byte
	kdfTo(derived[:], cmdKey, kdfSaltAuthIDEncryptionKey)
	aesBlock, _ := aes.NewCipher(derived[:16])
	var result [16]byte
	aesBlock.Encrypt(result[:], buf[:])
	return result
}

func sealVMessAEADHeader(key [16]byte, data []byte, t time.Time) []byte {
	generatedAuthID := createAuthID(key[:], t.Unix())
	var connectionNonce [8]byte
	rand.Read(connectionNonce[:])

	const (
		authIDLen = 16
		nonceLen  = 8
		gcmTag    = 16
		lenPlain  = 2
		headerLen = authIDLen + lenPlain + gcmTag + nonceLen
	)
	out := make([]byte, headerLen+len(data)+gcmTag)
	copy(out[:authIDLen], generatedAuthID[:])
	copy(out[authIDLen+lenPlain+gcmTag:headerLen], connectionNonce[:])

	var payloadLen [2]byte
	binary.BigEndian.PutUint16(payloadLen[:], uint16(len(data)))

	var derived [sha256.Size]byte
	kdfTo(derived[:], key[:], kdfSaltVMessHeaderPayloadLengthAEADKey, generatedAuthID[:], connectionNonce[:])
	lengthBlock, _ := aes.NewCipher(derived[:16])
	kdfTo(derived[:], key[:], kdfSaltVMessHeaderPayloadLengthAEADIV, generatedAuthID[:], connectionNonce[:])
	lengthAEAD, _ := cipher.NewGCM(lengthBlock)
	lengthAEAD.Seal(out[authIDLen:authIDLen], derived[:12], payloadLen[:], generatedAuthID[:])

	kdfTo(derived[:], key[:], kdfSaltVMessHeaderPayloadAEADKey, generatedAuthID[:], connectionNonce[:])
	payloadBlock, _ := aes.NewCipher(derived[:16])
	kdfTo(derived[:], key[:], kdfSaltVMessHeaderPayloadAEADIV, generatedAuthID[:], connectionNonce[:])
	payloadAEAD, _ := cipher.NewGCM(payloadBlock)
	payloadAEAD.Seal(out[headerLen:headerLen], derived[:12], data, generatedAuthID[:])

	return out
}
