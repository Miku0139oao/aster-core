package observable

import (
	"errors"
	"sync"
	"sync/atomic"
)

type Observable[T any] struct {
	iterable    Iterable[T]
	listener    map[Subscription[T]]*Subscriber[T]
	mux         sync.Mutex
	subscribers atomic.Int64
	done        bool
	stopCh      chan struct{}
}

func (o *Observable[T]) process() {
	for item := range o.iterable {
		o.mux.Lock()
		for _, sub := range o.listener {
			sub.Emit(item)
		}
		o.mux.Unlock()
	}
	o.close()
}

func (o *Observable[T]) close() {
	o.mux.Lock()
	defer o.mux.Unlock()

	o.done = true
	for _, sub := range o.listener {
		sub.Close()
	}
	o.listener = nil
	o.subscribers.Store(0)
	close(o.stopCh)
}

func (o *Observable[T]) Subscribe() (Subscription[T], error) {
	o.mux.Lock()
	defer o.mux.Unlock()
	if o.done {
		return nil, errors.New("observable is closed")
	}
	subscriber := newSubscriber[T]()
	o.listener[subscriber.Out()] = subscriber
	o.subscribers.Add(1)
	return subscriber.Out(), nil
}

func (o *Observable[T]) UnSubscribe(sub Subscription[T]) {
	o.mux.Lock()
	defer o.mux.Unlock()
	subscriber, exist := o.listener[sub]
	if !exist {
		return
	}
	delete(o.listener, sub)
	o.subscribers.Add(-1)
	subscriber.Close()
}

// HasSubscribers reports whether emitting an item can reach at least one
// subscriber. It is safe to call from hot paths without taking the listener
// mutex.
func (o *Observable[T]) HasSubscribers() bool {
	return o.subscribers.Load() > 0
}

func NewObservable[T any](iter Iterable[T]) *Observable[T] {
	observable := &Observable[T]{
		iterable: iter,
		listener: map[Subscription[T]]*Subscriber[T]{},
		stopCh:   make(chan struct{}),
	}
	go observable.process()
	return observable
}
