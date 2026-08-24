package pool

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func lg() Factory[int] {
	initial := -1
	return func(context.Context) (int, error) {
		initial++
		return initial, nil
	}
}

func TestPool_Basic(t *testing.T) {
	g := lg()
	pool := New[int](g)

	elm, _ := pool.Get()
	assert.Equal(t, 0, elm)
	pool.Put(elm)
	elm, _ = pool.Get()
	assert.Equal(t, 0, elm)
	elm, _ = pool.Get()
	assert.Equal(t, 1, elm)
}

func TestPool_MaxSize(t *testing.T) {
	g := lg()
	size := 5
	pool := New[int](g, WithSize[int](size))

	var items []int

	for i := 0; i < size; i++ {
		item, _ := pool.Get()
		items = append(items, item)
	}

	extra, _ := pool.Get()
	assert.Equal(t, size, extra)

	for _, item := range items {
		pool.Put(item)
	}

	pool.Put(extra)

	for _, item := range items {
		elm, _ := pool.Get()
		assert.Equal(t, item, elm)
	}
}

func TestPool_MaxAge(t *testing.T) {
	g := lg()
	pool := New[int](g, WithAge[int](20))

	elm, _ := pool.Get()
	pool.Put(elm)

	elm, _ = pool.Get()
	assert.Equal(t, 0, elm)
	pool.Put(elm)

	time.Sleep(time.Millisecond * 22)
	elm, _ = pool.Get()
	assert.Equal(t, 1, elm)
}

func TestRecycleDrainsWithoutWaitingForChannelClose(t *testing.T) {
	evicted := make(chan int, 2)
	p := &Pool[int]{pool: &pool[int]{
		ch: make(chan *entry[int], 2),
		evict: func(value int) {
			evicted <- value
		},
	}}
	p.pool.ch <- &entry[int]{elm: 1}
	p.pool.ch <- &entry[int]{elm: 2}

	done := make(chan struct{})
	go func() {
		recycle(p)
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("recycle blocked on an open, empty pool channel")
	}
	close(evicted)
	var got []int
	for value := range evicted {
		got = append(got, value)
	}
	assert.ElementsMatch(t, []int{1, 2}, got)
}
