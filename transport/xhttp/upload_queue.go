package xhttp

import (
	"errors"
	"io"
	"sync"
)

var ErrQueueTooLarge = errors.New("packet queue is too large")

type Packet struct {
	Seq     uint64
	Payload []byte // UploadQueue will hold Payload, so never reuse it after UploadQueue.Push
	Reader  io.ReadCloser
}

type UploadQueue struct {
	mu           sync.Mutex
	condPushed   sync.Cond
	packets      map[uint64][]byte
	nextSeq      uint64
	buf          []byte
	closed       bool
	maxPackets   int
	maxBytes     int
	queuedBytes  int
	reserveBytes func(int) bool
	releaseBytes func(int)
	reader       io.ReadCloser
}

const defaultMaxUploadQueueBytes = 32 << 20

func NewUploadQueue(maxPackets int, maxBytes ...int) *UploadQueue {
	byteLimit := defaultMaxUploadQueueBytes
	if len(maxBytes) > 0 {
		byteLimit = maxBytes[0]
	}
	q := &UploadQueue{
		packets:    make(map[uint64][]byte, maxPackets),
		maxPackets: maxPackets,
		maxBytes:   byteLimit,
	}
	q.condPushed = sync.Cond{L: &q.mu}
	return q
}

func (q *UploadQueue) SetByteBudget(reserve func(int) bool, release func(int)) {
	q.mu.Lock()
	q.reserveBytes = reserve
	q.releaseBytes = release
	q.mu.Unlock()
}

func (q *UploadQueue) CanPush(sequence uint64) error {
	q.mu.Lock()
	defer q.mu.Unlock()
	if q.closed {
		return io.ErrClosedPipe
	}
	if _, exists := q.packets[sequence]; !exists && q.maxPackets > 0 && len(q.packets) >= q.maxPackets {
		return ErrQueueTooLarge
	}
	return nil
}

func (q *UploadQueue) Push(p Packet) error {
	q.mu.Lock()
	defer q.mu.Unlock()

	if q.closed {
		return io.ErrClosedPipe
	}

	if q.reader != nil {
		return errors.New("uploadQueue.reader already exists")
	}

	if p.Reader != nil {
		q.reader = p.Reader
		q.condPushed.Broadcast()
		return nil
	}

	previous, exists := q.packets[p.Seq]
	if !exists && q.maxPackets > 0 && len(q.packets) >= q.maxPackets {
		return ErrQueueTooLarge
	}
	delta := len(p.Payload) - len(previous)
	nextBytes := q.queuedBytes + delta
	if q.maxBytes > 0 && nextBytes > q.maxBytes {
		return ErrQueueTooLarge
	}
	if delta > 0 && q.reserveBytes != nil && !q.reserveBytes(delta) {
		return ErrQueueTooLarge
	}
	if delta < 0 && q.releaseBytes != nil {
		q.releaseBytes(-delta)
	}
	q.packets[p.Seq] = p.Payload
	q.queuedBytes = nextBytes
	q.condPushed.Broadcast()
	return nil
}

func (q *UploadQueue) Read(b []byte) (int, error) {
	q.mu.Lock()

	for {
		if len(q.buf) > 0 {
			n := copy(b, q.buf)
			q.buf = q.buf[n:]
			q.queuedBytes -= n
			if q.releaseBytes != nil {
				q.releaseBytes(n)
			}
			q.mu.Unlock()
			return n, nil
		}

		if payload, ok := q.packets[q.nextSeq]; ok {
			delete(q.packets, q.nextSeq)
			q.nextSeq++
			q.buf = payload
			continue
		}

		if reader := q.reader; reader != nil {
			q.mu.Unlock() // unlock before calling q.reader.Read
			return reader.Read(b)
		}

		if q.closed {
			q.mu.Unlock()
			return 0, io.EOF
		}

		if q.maxPackets > 0 && len(q.packets) >= q.maxPackets {
			q.mu.Unlock()
			// the "reassembly buffer" is too large, and we want to constrain memory usage somehow.
			// let's tear down the connection and hope the application retries.
			return 0, ErrQueueTooLarge
		}

		q.condPushed.Wait()
	}
}

func (q *UploadQueue) Close() error {
	q.mu.Lock()
	defer q.mu.Unlock()

	var err error
	if q.reader != nil {
		err = q.reader.Close()
	}
	q.reader = nil
	q.packets = nil
	q.buf = nil
	if q.queuedBytes > 0 && q.releaseBytes != nil {
		q.releaseBytes(q.queuedBytes)
	}
	q.queuedBytes = 0
	q.closed = true
	q.condPushed.Broadcast()
	return err
}
