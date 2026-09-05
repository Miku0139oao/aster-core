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
	ready        []byte
	hasReady     bool
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

func (q *UploadQueue) storedPackets() int {
	n := len(q.packets)
	if q.hasReady {
		n++
	}
	return n
}

func (q *UploadQueue) hasSeq(sequence uint64) bool {
	if sequence == q.nextSeq {
		return q.hasReady
	}
	_, exists := q.packets[sequence]
	return exists
}

func (q *UploadQueue) CanPush(sequence uint64) error {
	q.mu.Lock()
	defer q.mu.Unlock()
	if q.closed {
		return io.ErrClosedPipe
	}
	if !q.hasSeq(sequence) && q.maxPackets > 0 && q.storedPackets() >= q.maxPackets {
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

	var previous []byte
	exists := false
	if p.Seq == q.nextSeq {
		previous = q.ready
		exists = q.hasReady
	} else {
		previous, exists = q.packets[p.Seq]
	}
	if !exists && q.maxPackets > 0 && q.storedPackets() >= q.maxPackets {
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
	if p.Seq == q.nextSeq {
		q.ready = p.Payload
		q.hasReady = true
	} else {
		if q.packets == nil {
			q.packets = make(map[uint64][]byte)
		}
		q.packets[p.Seq] = p.Payload
	}
	q.queuedBytes = nextBytes
	q.condPushed.Broadcast()
	return nil
}

func (q *UploadQueue) takeBuf(payload []byte) {
	if len(payload) == 0 {
		q.buf = nil
		return
	}
	q.buf = payload
}

func (q *UploadQueue) Read(b []byte) (int, error) {
	q.mu.Lock()

	for {
		if len(q.buf) > 0 {
			n := copy(b, q.buf)
			q.buf = q.buf[n:]
			if len(q.buf) == 0 {
				q.buf = nil
			}
			q.queuedBytes -= n
			if q.releaseBytes != nil {
				q.releaseBytes(n)
			}
			q.mu.Unlock()
			return n, nil
		}

		if q.hasReady {
			q.takeBuf(q.ready)
			q.ready = nil
			q.hasReady = false
			q.nextSeq++
			continue
		}

		if payload, ok := q.packets[q.nextSeq]; ok {
			delete(q.packets, q.nextSeq)
			q.nextSeq++
			q.takeBuf(payload)
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

		if q.maxPackets > 0 && q.storedPackets() >= q.maxPackets {
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
	q.ready = nil
	q.hasReady = false
	if q.queuedBytes > 0 && q.releaseBytes != nil {
		q.releaseBytes(q.queuedBytes)
	}
	q.queuedBytes = 0
	q.closed = true
	q.condPushed.Broadcast()
	return err
}
