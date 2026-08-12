package utils

import (
	"bufio"
	"crypto/rand"
	"sync"
)

// cryptoRandomReaders amortizes the comparatively expensive operating-system
// random read while preserving crypto/rand as the only entropy source.
var cryptoRandomReaders = sync.Pool{New: func() any {
	return bufio.NewReaderSize(rand.Reader, 4096)
}}

// CryptoRandomUint64n returns an unbiased cryptographically random value in
// [0, max). It uses rejection sampling to avoid modulo bias.
func CryptoRandomUint64n(max uint64) (uint64, error) {
	if max == 0 {
		return 0, nil
	}
	reader := cryptoRandomReaders.Get().(*bufio.Reader)
	defer cryptoRandomReaders.Put(reader)

	threshold := -max % max
	for {
		var value uint64
		for shift := 0; shift < 64; shift += 8 {
			part, err := reader.ReadByte()
			if err != nil {
				return 0, err
			}
			value |= uint64(part) << shift
		}
		if value >= threshold {
			return value % max, nil
		}
	}
}
