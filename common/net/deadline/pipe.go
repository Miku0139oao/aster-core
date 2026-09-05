// Copyright 2010 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package deadline

import (
	"sync"
	"sync/atomic"
	"time"
)

// PipeDeadline is an abstraction for handling timeouts.
type PipeDeadline struct {
	mu     sync.Mutex // Guards timer and cancel
	timer  *time.Timer
	cancel chan struct{} // Must be non-nil
	cached atomic.Value  // chan struct{}, lock-free Wait() after first Set/Wait
}

func MakePipeDeadline() PipeDeadline {
	return PipeDeadline{cancel: make(chan struct{})}
}

// Set sets the point in time when the deadline will time out.
// A timeout event is signaled by closing the channel returned by waiter.
// Once a timeout has occurred, the deadline can be refreshed by specifying a
// t value in the future.
//
// A zero value for t prevents timeout.
func (d *PipeDeadline) Set(t time.Time) {
	d.mu.Lock()
	defer d.mu.Unlock()

	ch := d.cancel
	oldTimer := d.timer
	stopped := true
	if oldTimer != nil {
		stopped = oldTimer.Stop()
		if !stopped {
			<-ch // Wait for the timer callback to finish and close cancel
		}
	}
	// Never retain a stopped, unfired timer across Set(zero)/Set(past).
	// A later Stop on that timer returns false while ch is still open, so
	// waiting on <-ch would deadlock.
	d.timer = nil

	// Time is zero, then there is no deadline.
	closed := isClosedChan(ch)
	if t.IsZero() {
		if closed {
			ch = make(chan struct{})
			d.cancel = ch
		}
		d.cached.Store(ch)
		return
	}

	// Time in the future, setup a timer to cancel in the future.
	if dur := time.Until(t); dur > 0 {
		if closed {
			ch = make(chan struct{})
			d.cancel = ch
		}
		d.cached.Store(ch)
		if oldTimer != nil && stopped && !closed {
			oldTimer.Reset(dur)
			d.timer = oldTimer
			return
		}
		d.timer = time.AfterFunc(dur, func() {
			close(ch)
		})
		return
	}

	// Time in the past, so close immediately.
	if !closed {
		close(ch)
	}
	d.cached.Store(ch)
}

// Wait returns a channel that is closed when the deadline is exceeded.
func (d *PipeDeadline) Wait() chan struct{} {
	if ch, ok := d.cached.Load().(chan struct{}); ok && ch != nil {
		return ch
	}
	return d.syncWait()
}

func (d *PipeDeadline) syncWait() chan struct{} {
	d.mu.Lock()
	ch := d.cancel
	if ch == nil {
		ch = make(chan struct{})
		d.cancel = ch
	}
	d.cached.Store(ch)
	d.mu.Unlock()
	return ch
}

func isClosedChan(c <-chan struct{}) bool {
	select {
	case <-c:
		return true
	default:
		return false
	}
}

func makeFilledChan() chan struct{} {
	ch := make(chan struct{}, 1)
	ch <- struct{}{}
	return ch
}
