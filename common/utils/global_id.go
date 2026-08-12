package utils

import (
	"unsafe"

	"github.com/Miku0139oao/aster-core/common/maphash"
)

var globalSeed = maphash.MakeSeed()

func GlobalID(material string) (id [8]byte) {
	*(*uint64)(unsafe.Pointer(&id[0])) = maphash.String(globalSeed, material)
	return
}

func MapHash(material string) uint64 {
	return maphash.String(globalSeed, material)
}

func MapHashComparable[T comparable](material T) uint64 {
	return maphash.Comparable(globalSeed, material)
}
