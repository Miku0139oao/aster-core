package pool

// Inspired by https://github.com/xtaci/smux/blob/master/alloc.go

import (
	"errors"
	"math/bits"
	"runtime"
	"sync"
	"sync/atomic"
)

var DefaultAllocator = NewAllocator()

type Allocator interface {
	Get(size int) []byte
	Put(buf []byte) error
}

// slabPool is a sync.Pool with an occupancy cap. Get/Put stay on the
// per-P cache; excess Puts are dropped instead of pinning every idle slab.
// Occupancy is approximate: runtime may evict Pool items, in which case the
// next miss resets the counter so Puts are accepted again.
type slabPool[T any] struct {
	pool sync.Pool
	n    atomic.Int32
	max  int32
}

func (p *slabPool[T]) get() *T {
	if v, _ := p.pool.Get().(*T); v != nil {
		if n := p.n.Add(-1); n < 0 {
			p.n.Store(0)
		}
		return v
	}
	if p.n.Load() > 0 {
		p.n.Store(0)
	}
	return new(T)
}

func (p *slabPool[T]) put(v *T) {
	for {
		n := p.n.Load()
		if n >= p.max {
			return
		}
		if p.n.CompareAndSwap(n, n+1) {
			p.pool.Put(v)
			return
		}
	}
}

func (p *slabPool[T]) retained() int32 {
	n := p.n.Load()
	if n < 0 {
		return 0
	}
	return n
}

// defaultAllocator for incoming frames, optimized to prevent overwriting after zeroing.
// Size classes 64 B–8 KiB stay on unbounded sync.Pool (cheap, high churn).
// 16 KiB–128 KiB use the same Pool with a cap so a connection burst cannot pin
// tens of megabytes after the sockets close. Excess Puts are dropped and become GC-able.
type defaultAllocator struct {
	buffers [8]sync.Pool // 64 B .. 8 KiB
	large14 slabPool[[1 << 14]byte]
	large15 slabPool[[1 << 15]byte]
	large16 slabPool[[1 << 16]byte]
	large17 slabPool[[1 << 17]byte]
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
		large14: slabPool[[1 << 14]byte]{max: int32(largePoolCap(1 << 14))},
		large15: slabPool[[1 << 15]byte]{max: int32(largePoolCap(1 << 15))},
		large16: slabPool[[1 << 16]byte]{max: int32(largePoolCap(1 << 16))},
		large17: slabPool[[1 << 17]byte]{max: int32(largePoolCap(1 << 17))},
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
			return alloc.large14.get()[:size]
		case 9:
			return alloc.large15.get()[:size]
		case 10:
			return alloc.large16.get()[:size]
		case 11:
			return alloc.large17.get()[:size]
		default:
			panic("invalid pool index")
		}
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
		alloc.large14.put((*[1 << 14]byte)(buf))
	case 9:
		alloc.large15.put((*[1 << 15]byte)(buf))
	case 10:
		alloc.large16.put((*[1 << 16]byte)(buf))
	case 11:
		alloc.large17.put((*[1 << 17]byte)(buf))
	default:
		panic("invalid pool index")
	}
	return nil
}

// msb return the pos of most significant bit
func msb(size int) uint16 {
	return uint16(bits.Len32(uint32(size)) - 1)
}
