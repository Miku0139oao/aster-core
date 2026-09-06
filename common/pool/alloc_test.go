package pool

import (
	"testing"

	"github.com/metacubex/randv2"
	"github.com/stretchr/testify/assert"
)

func TestAllocGet(t *testing.T) {
	alloc := NewAllocator()
	assert.Nil(t, alloc.Get(0))
	assert.Equal(t, 1, len(alloc.Get(1)))
	assert.Equal(t, 2, len(alloc.Get(2)))
	assert.Equal(t, 3, len(alloc.Get(3)))
	assert.Equal(t, 64, cap(alloc.Get(3)))
	assert.Equal(t, 64, cap(alloc.Get(4)))
	assert.Equal(t, 1023, len(alloc.Get(1023)))
	assert.Equal(t, 1024, cap(alloc.Get(1023)))
	assert.Equal(t, 1024, len(alloc.Get(1024)))
	assert.Equal(t, 65536, len(alloc.Get(65536)))
	assert.Equal(t, 65537, len(alloc.Get(65537)))
}

func TestAllocPut(t *testing.T) {
	alloc := NewAllocator()
	assert.Nil(t, alloc.Put(nil), "put nil misbehavior")
	assert.NotNil(t, alloc.Put(make([]byte, 3)), "put elem:3 []bytes misbehavior")
	assert.Nil(t, alloc.Put(make([]byte, 4)), "put elem:4 []bytes misbehavior")
	assert.Nil(t, alloc.Put(make([]byte, 1023, 1024)), "put elem:1024 []bytes misbehavior")
	assert.Nil(t, alloc.Put(make([]byte, 65536)), "put elem:65536 []bytes misbehavior")
	assert.Nil(t, alloc.Put(make([]byte, 65537)), "put elem:65537 []bytes misbehavior")
}

func TestAllocPutThenGet(t *testing.T) {
	alloc := NewAllocator()
	data := alloc.Get(4)
	alloc.Put(data)
	newData := alloc.Get(4)

	assert.Equal(t, cap(data), cap(newData), "different cap while alloc.Get()")
}

func TestLargePoolDropsExcessSlabs(t *testing.T) {
	alloc := newDefaultAllocator()
	ch := alloc.large15
	if cap(ch) < 2 {
		t.Fatalf("32 KiB pool cap = %d", cap(ch))
	}

	n := cap(ch)*3 + 7
	bufs := make([][]byte, n)
	for i := range bufs {
		bufs[i] = alloc.Get(1 << 15)
		if cap(bufs[i]) != 1<<15 {
			t.Fatalf("Get(32KiB) cap = %d", cap(bufs[i]))
		}
	}
	for i := range bufs {
		if err := alloc.Put(bufs[i]); err != nil {
			t.Fatal(err)
		}
		bufs[i] = nil
	}
	if got := len(ch); got != cap(ch) {
		t.Fatalf("retained 32 KiB slabs = %d, want bounded cap %d", got, cap(ch))
	}

	// The next Get must still succeed from the bounded pool.
	got := alloc.Get(1 << 15)
	if cap(got) != 1<<15 {
		t.Fatalf("Get after overflow cap = %d", cap(got))
	}
}

func TestLargePoolCapsStayWithinBudget(t *testing.T) {
	for _, size := range []int{1 << 14, 1 << 15, 1 << 16, 1 << 17} {
		n := largePoolCap(size)
		if n < 2 {
			t.Fatalf("size %d cap %d < 2", size, n)
		}
		if n*size > largePoolBudget {
			t.Fatalf("size %d cap %d exceeds budget %d", size, n, largePoolBudget)
		}
	}
}

func BenchmarkMSB(b *testing.B) {
	for i := 0; i < b.N; i++ {
		msb(randv2.Int())
	}
}
