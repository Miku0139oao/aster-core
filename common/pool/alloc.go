package pool

// Inspired by https://github.com/xtaci/smux/blob/master/alloc.go

import (
	"errors"
	"math/bits"
	"runtime"
	"sync"
)

var DefaultAllocator = NewAllocator()

type Allocator interface {
	Get(size int) []byte
	Put(buf []byte) error
}

// defaultAllocator for incoming frames, optimized to prevent overwriting after zeroing.
// Size classes 64 B–8 KiB stay on unbounded sync.Pool (cheap, high churn).
// 16 KiB–128 KiB use bounded channels so a connection burst cannot pin tens of
// megabytes after the sockets close. Excess Puts are dropped and become GC-able.
type defaultAllocator struct {
	buffers [8]sync.Pool // 64 B .. 8 KiB
	large14 chan *[1 << 14]byte
	large15 chan *[1 << 15]byte
	large16 chan *[1 << 16]byte
	large17 chan *[1 << 17]byte
}

func largePoolCap(bufSize int) int {
	n := largePoolProcFactor * runtime.GOMAXPROCS(0)
	if n < largePoolMin {
		n = largePoolMin
	}
	maxN := largePoolBudget / bufSize
	if maxN < 2 {
		maxN = 2
	}
	if n > maxN {
		n = maxN
	}
	return n
}

// NewAllocator initiates a []byte allocator for frames up to 128 KiB,
// the waste(memory fragmentation) of space allocation is guaranteed to be
// no more than 50%.
func NewAllocator() Allocator {
	return newDefaultAllocator()
}

func newDefaultAllocator() *defaultAllocator {
	return &defaultAllocator{
		buffers: [...]sync.Pool{ // 64B -> 8K
			{New: func() any { return new([1 << 6]byte) }},
			{New: func() any { return new([1 << 7]byte) }},
			{New: func() any { return new([1 << 8]byte) }},
			{New: func() any { return new([1 << 9]byte) }},
			{New: func() any { return new([1 << 10]byte) }},
			{New: func() any { return new([1 << 11]byte) }},
			{New: func() any { return new([1 << 12]byte) }},
			{New: func() any { return new([1 << 13]byte) }},
		},
		large14: make(chan *[1 << 14]byte, largePoolCap(1<<14)),
		large15: make(chan *[1 << 15]byte, largePoolCap(1<<15)),
		large16: make(chan *[1 << 16]byte, largePoolCap(1<<16)),
		large17: make(chan *[1 << 17]byte, largePoolCap(1<<17)),
	}
}

// Get a []byte from pool with most appropriate cap
func (alloc *defaultAllocator) Get(size int) []byte {
	switch {
	case size < 0:
		panic("alloc.Get: len out of range")
	case size == 0:
		return nil
	case size > 1<<17:
		return make([]byte, size)
	default:
		var index uint16
		if size > 64 {
			index = msb(size)
			if size != 1<<index {
				index += 1
			}
			index -= 6
		}
		switch index {
		case 0:
			return alloc.buffers[0].Get().(*[1 << 6]byte)[:size]
		case 1:
			return alloc.buffers[1].Get().(*[1 << 7]byte)[:size]
		case 2:
			return alloc.buffers[2].Get().(*[1 << 8]byte)[:size]
		case 3:
			return alloc.buffers[3].Get().(*[1 << 9]byte)[:size]
		case 4:
			return alloc.buffers[4].Get().(*[1 << 10]byte)[:size]
		case 5:
			return alloc.buffers[5].Get().(*[1 << 11]byte)[:size]
		case 6:
			return alloc.buffers[6].Get().(*[1 << 12]byte)[:size]
		case 7:
			return alloc.buffers[7].Get().(*[1 << 13]byte)[:size]
		case 8:
			return getLarge14(alloc.large14, size)
		case 9:
			return getLarge15(alloc.large15, size)
		case 10:
			return getLarge16(alloc.large16, size)
		case 11:
			return getLarge17(alloc.large17, size)
		default:
			panic("invalid pool index")
		}
	}
}

func getLarge14(ch chan *[1 << 14]byte, size int) []byte {
	select {
	case buf := <-ch:
		return buf[:size]
	default:
		return new([1 << 14]byte)[:size]
	}
}

func getLarge15(ch chan *[1 << 15]byte, size int) []byte {
	select {
	case buf := <-ch:
		return buf[:size]
	default:
		return new([1 << 15]byte)[:size]
	}
}

func getLarge16(ch chan *[1 << 16]byte, size int) []byte {
	select {
	case buf := <-ch:
		return buf[:size]
	default:
		return new([1 << 16]byte)[:size]
	}
}

func getLarge17(ch chan *[1 << 17]byte, size int) []byte {
	select {
	case buf := <-ch:
		return buf[:size]
	default:
		return new([1 << 17]byte)[:size]
	}
}

// Put returns a []byte to pool for future use,
// which the cap must be exactly 2^n
func (alloc *defaultAllocator) Put(buf []byte) error {
	if cap(buf) == 0 || cap(buf) > 1<<17 {
		return nil
	}

	bits := msb(cap(buf))
	if cap(buf) != 1<<bits {
		if cap(buf) > 1<<16 {
			return nil
		}
		return errors.New("allocator Put() incorrect buffer size")
	}
	if cap(buf) < 1<<6 {
		return nil
	}
	bits -= 6
	buf = buf[:cap(buf)]

	//nolint
	//lint:ignore SA6002 ignore temporarily
	switch bits {
	case 0:
		alloc.buffers[0].Put((*[1 << 6]byte)(buf))
	case 1:
		alloc.buffers[1].Put((*[1 << 7]byte)(buf))
	case 2:
		alloc.buffers[2].Put((*[1 << 8]byte)(buf))
	case 3:
		alloc.buffers[3].Put((*[1 << 9]byte)(buf))
	case 4:
		alloc.buffers[4].Put((*[1 << 10]byte)(buf))
	case 5:
		alloc.buffers[5].Put((*[1 << 11]byte)(buf))
	case 6:
		alloc.buffers[6].Put((*[1 << 12]byte)(buf))
	case 7:
		alloc.buffers[7].Put((*[1 << 13]byte)(buf))
	case 8:
		putLarge14(alloc.large14, (*[1 << 14]byte)(buf))
	case 9:
		putLarge15(alloc.large15, (*[1 << 15]byte)(buf))
	case 10:
		putLarge16(alloc.large16, (*[1 << 16]byte)(buf))
	case 11:
		putLarge17(alloc.large17, (*[1 << 17]byte)(buf))
	default:
		panic("invalid pool index")
	}
	return nil
}

func putLarge14(ch chan *[1 << 14]byte, buf *[1 << 14]byte) {
	select {
	case ch <- buf:
	default:
	}
}

func putLarge15(ch chan *[1 << 15]byte, buf *[1 << 15]byte) {
	select {
	case ch <- buf:
	default:
	}
}

func putLarge16(ch chan *[1 << 16]byte, buf *[1 << 16]byte) {
	select {
	case ch <- buf:
	default:
	}
}

func putLarge17(ch chan *[1 << 17]byte, buf *[1 << 17]byte) {
	select {
	case ch <- buf:
	default:
	}
}

// msb return the pos of most significant bit
func msb(size int) uint16 {
	return uint16(bits.Len32(uint32(size)) - 1)
}
