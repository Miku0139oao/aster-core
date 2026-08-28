package net

import (
	"crypto/sha1"
	"encoding/base64"
	"encoding/binary"
	"math/bits"
	"unsafe"
)

// nativeLittleEndian is true on amd64/arm64/386 and most OpenWrt targets.
// The word-XOR fast path is only valid on little-endian machines.
var nativeLittleEndian = func() bool {
	x := uint16(1)
	return *(*byte)(unsafe.Pointer(&x)) == 1
}()

// kanged from https://github.com/nhooyr/websocket/blob/master/frame.go
// License: MIT

// MaskWebSocket applies the WebSocket masking algorithm to p
// with the given key.
// See https://tools.ietf.org/html/rfc6455#section-5.3
//
// The returned value is the correctly rotated key to
// to continue to mask/unmask the message.
//
// It is optimized for LittleEndian and expects the key
// to be in little endian.
//
// See https://github.com/golang/go/issues/31586
func MaskWebSocket(key uint32, b []byte) uint32 {
	// Word XOR after 8-byte alignment beats encoding/binary on data-sized
	// frames. Tiny control frames stay on the generic path to avoid the
	// alignment prologue. Payload in WriteBuffer is typically offset by
	// FrontHeadroom (14), so the aligned path is the real WS data plane.
	if nativeLittleEndian && len(b) >= 32 {
		return maskWebSocketAligned(key, b)
	}
	return maskWebSocketGeneric(key, b)
}

func maskWebSocketAligned(key uint32, b []byte) uint32 {
	for len(b) > 0 && uintptr(unsafe.Pointer(unsafe.SliceData(b)))&7 != 0 {
		b[0] ^= byte(key)
		key = bits.RotateLeft32(key, -8)
		b = b[1:]
	}
	if len(b) >= 8 {
		key64 := uint64(key)<<32 | uint64(key)
		p := unsafe.Pointer(unsafe.SliceData(b))
		n := len(b)
		i := 0
		for n-i >= 64 {
			*(*uint64)(unsafe.Add(p, i)) ^= key64
			*(*uint64)(unsafe.Add(p, i+8)) ^= key64
			*(*uint64)(unsafe.Add(p, i+16)) ^= key64
			*(*uint64)(unsafe.Add(p, i+24)) ^= key64
			*(*uint64)(unsafe.Add(p, i+32)) ^= key64
			*(*uint64)(unsafe.Add(p, i+40)) ^= key64
			*(*uint64)(unsafe.Add(p, i+48)) ^= key64
			*(*uint64)(unsafe.Add(p, i+56)) ^= key64
			i += 64
		}
		for n-i >= 8 {
			*(*uint64)(unsafe.Add(p, i)) ^= key64
			i += 8
		}
		b = b[i:]
	}
	return maskWebSocketGeneric(key, b)
}

func maskWebSocketGeneric(key uint32, b []byte) uint32 {
	if len(b) >= 8 {
		key64 := uint64(key)<<32 | uint64(key)

		// At some point in the future we can clean these unrolled loops up.
		// See https://github.com/golang/go/issues/31586#issuecomment-487436401

		// Then we xor until b is less than 128 bytes.
		for len(b) >= 128 {
			v := binary.LittleEndian.Uint64(b)
			binary.LittleEndian.PutUint64(b, v^key64)
			v = binary.LittleEndian.Uint64(b[8:16])
			binary.LittleEndian.PutUint64(b[8:16], v^key64)
			v = binary.LittleEndian.Uint64(b[16:24])
			binary.LittleEndian.PutUint64(b[16:24], v^key64)
			v = binary.LittleEndian.Uint64(b[24:32])
			binary.LittleEndian.PutUint64(b[24:32], v^key64)
			v = binary.LittleEndian.Uint64(b[32:40])
			binary.LittleEndian.PutUint64(b[32:40], v^key64)
			v = binary.LittleEndian.Uint64(b[40:48])
			binary.LittleEndian.PutUint64(b[40:48], v^key64)
			v = binary.LittleEndian.Uint64(b[48:56])
			binary.LittleEndian.PutUint64(b[48:56], v^key64)
			v = binary.LittleEndian.Uint64(b[56:64])
			binary.LittleEndian.PutUint64(b[56:64], v^key64)
			v = binary.LittleEndian.Uint64(b[64:72])
			binary.LittleEndian.PutUint64(b[64:72], v^key64)
			v = binary.LittleEndian.Uint64(b[72:80])
			binary.LittleEndian.PutUint64(b[72:80], v^key64)
			v = binary.LittleEndian.Uint64(b[80:88])
			binary.LittleEndian.PutUint64(b[80:88], v^key64)
			v = binary.LittleEndian.Uint64(b[88:96])
			binary.LittleEndian.PutUint64(b[88:96], v^key64)
			v = binary.LittleEndian.Uint64(b[96:104])
			binary.LittleEndian.PutUint64(b[96:104], v^key64)
			v = binary.LittleEndian.Uint64(b[104:112])
			binary.LittleEndian.PutUint64(b[104:112], v^key64)
			v = binary.LittleEndian.Uint64(b[112:120])
			binary.LittleEndian.PutUint64(b[112:120], v^key64)
			v = binary.LittleEndian.Uint64(b[120:128])
			binary.LittleEndian.PutUint64(b[120:128], v^key64)
			b = b[128:]
		}

		// Then we xor until b is less than 64 bytes.
		for len(b) >= 64 {
			v := binary.LittleEndian.Uint64(b)
			binary.LittleEndian.PutUint64(b, v^key64)
			v = binary.LittleEndian.Uint64(b[8:16])
			binary.LittleEndian.PutUint64(b[8:16], v^key64)
			v = binary.LittleEndian.Uint64(b[16:24])
			binary.LittleEndian.PutUint64(b[16:24], v^key64)
			v = binary.LittleEndian.Uint64(b[24:32])
			binary.LittleEndian.PutUint64(b[24:32], v^key64)
			v = binary.LittleEndian.Uint64(b[32:40])
			binary.LittleEndian.PutUint64(b[32:40], v^key64)
			v = binary.LittleEndian.Uint64(b[40:48])
			binary.LittleEndian.PutUint64(b[40:48], v^key64)
			v = binary.LittleEndian.Uint64(b[48:56])
			binary.LittleEndian.PutUint64(b[48:56], v^key64)
			v = binary.LittleEndian.Uint64(b[56:64])
			binary.LittleEndian.PutUint64(b[56:64], v^key64)
			b = b[64:]
		}

		// Then we xor until b is less than 32 bytes.
		for len(b) >= 32 {
			v := binary.LittleEndian.Uint64(b)
			binary.LittleEndian.PutUint64(b, v^key64)
			v = binary.LittleEndian.Uint64(b[8:16])
			binary.LittleEndian.PutUint64(b[8:16], v^key64)
			v = binary.LittleEndian.Uint64(b[16:24])
			binary.LittleEndian.PutUint64(b[16:24], v^key64)
			v = binary.LittleEndian.Uint64(b[24:32])
			binary.LittleEndian.PutUint64(b[24:32], v^key64)
			b = b[32:]
		}

		// Then we xor until b is less than 16 bytes.
		for len(b) >= 16 {
			v := binary.LittleEndian.Uint64(b)
			binary.LittleEndian.PutUint64(b, v^key64)
			v = binary.LittleEndian.Uint64(b[8:16])
			binary.LittleEndian.PutUint64(b[8:16], v^key64)
			b = b[16:]
		}

		// Then we xor until b is less than 8 bytes.
		for len(b) >= 8 {
			v := binary.LittleEndian.Uint64(b)
			binary.LittleEndian.PutUint64(b, v^key64)
			b = b[8:]
		}
	}

	// Then we xor until b is less than 4 bytes.
	for len(b) >= 4 {
		v := binary.LittleEndian.Uint32(b)
		binary.LittleEndian.PutUint32(b, v^key)
		b = b[4:]
	}

	// xor remaining bytes.
	for i := range b {
		b[i] ^= byte(key)
		key = bits.RotateLeft32(key, -8)
	}

	return key
}

func GetWebSocketSecAccept(secKey string) string {
	const magic = "258EAFA5-E914-47DA-95CA-C5AB0DC85B11"
	const nonceSize = 24 // base64.StdEncoding.EncodedLen(nonceKeySize)
	p := make([]byte, nonceSize+len(magic))
	copy(p[:nonceSize], secKey)
	copy(p[nonceSize:], magic)
	sum := sha1.Sum(p)
	return base64.StdEncoding.EncodeToString(sum[:])
}
