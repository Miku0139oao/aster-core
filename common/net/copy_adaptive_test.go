package net

import (
	"bytes"
	"errors"
	"io"
	stdnet "net"
	"runtime"
	"sync"
	"testing"
	"time"

	"github.com/Miku0139oao/aster-core/common/pool"

	"github.com/metacubex/sing/common/buf"
	"github.com/metacubex/sing/common/network"
)

type sizeSpyReader struct {
	r     io.Reader
	sizes []int
}

func (s *sizeSpyReader) Read(p []byte) (int, error) {
	s.sizes = append(s.sizes, len(p))
	return s.r.Read(p)
}

type protocolChunkReader struct {
	r io.Reader
}

func (p *protocolChunkReader) Read(b []byte) (int, error) {
	return p.r.Read(b)
}

func (p *protocolChunkReader) ReadBuffer(buffer *buf.Buffer) error {
	if buffer.FreeLen() < pool.RelayBufferSize {
		return errors.New("protocol chunk needs RelayBufferSize")
	}
	n, err := p.r.Read(buffer.FreeBytes())
	buffer.Truncate(n)
	if n > 0 && err == io.EOF {
		return nil
	}
	return err
}

type headroomWriter struct {
	out bytes.Buffer
}

func (w *headroomWriter) Write(p []byte) (int, error) {
	return w.out.Write(p)
}

func (w *headroomWriter) WriteBuffer(buffer *buf.Buffer) error {
	_, err := w.out.Write(buffer.Bytes())
	buffer.Release()
	return err
}

func (w *headroomWriter) FrontHeadroom() int { return 8 }

// mtuWriter mimics a chunked writer such as shadowaead: payloads above
// WriterMTU still work but take a chunk-and-copy slow path.
type mtuWriter struct {
	headroomWriter
	mtu   int
	sizes []int
}

func (w *mtuWriter) WriteBuffer(buffer *buf.Buffer) error {
	w.sizes = append(w.sizes, buffer.Len())
	return w.headroomWriter.WriteBuffer(buffer)
}

func (w *mtuWriter) RearHeadroom() int { return 16 }

func (w *mtuWriter) WriterMTU() int { return w.mtu }

// slabSpyWriter records the allocator slab behind every buffer it receives
// (buffer.data is pool.Get(size)[:size], so cap(Bytes())+Start() is the
// slab) alongside the payload length.
type slabSpyWriter struct {
	headroomWriter
	front, rear int
	slabs       []int
	lens        []int
}

func (w *slabSpyWriter) WriteBuffer(buffer *buf.Buffer) error {
	w.slabs = append(w.slabs, cap(buffer.Bytes())+buffer.Start())
	w.lens = append(w.lens, buffer.Len())
	return w.headroomWriter.WriteBuffer(buffer)
}

func (w *slabSpyWriter) FrontHeadroom() int { return w.front }

func (w *slabSpyWriter) RearHeadroom() int { return w.rear }

func TestNextCopySizeGrowsAndShrinks(t *testing.T) {
	var full, small int
	size := copyMinBuffer
	size = nextCopySize(size, size, size, copyMinBuffer, pool.RelayBufferSize, &full, &small)
	size = nextCopySize(size, size, size, copyMinBuffer, pool.RelayBufferSize, &full, &small)
	if size != copyMinBuffer*2 {
		t.Fatalf("after two full reads size=%d", size)
	}
	full, small = 0, 0
	size = copyMinBuffer * 4
	for i := 0; i < copyShrinkAfter; i++ {
		size = nextCopySize(size, size, 1, copyMinBuffer, pool.RelayBufferSize, &full, &small)
	}
	if size != copyMinBuffer*2 {
		t.Fatalf("after small reads size=%d", size)
	}
	// Growth is judged against capacity (size minus headroom), and a
	// shrink from an MTU-derived size lands back on an allocator class.
	full, small = 0, 0
	const headroom = 24
	size = copyMinBuffer
	size = nextCopySize(size, size-headroom, size-headroom, copyMinBuffer, 16383+headroom, &full, &small)
	size = nextCopySize(size, size-headroom, size-headroom, copyMinBuffer, 16383+headroom, &full, &small)
	if size != copyMinBuffer*2 {
		t.Fatalf("full reads of capacity should grow, size=%d", size)
	}
	full, small = 0, 0
	size = 16383 + headroom
	for i := 0; i < copyShrinkAfter; i++ {
		size = nextCopySize(size, 16383, 1, copyMinBuffer, 16383+headroom, &full, &small)
	}
	if size != 8<<10 {
		t.Fatalf("shrink from MTU size should land on 8 KiB, got %d", size)
	}
}

func TestAdaptiveSizeRange(t *testing.T) {
	cases := []struct {
		name                  string
		opts                  network.ReadWaitOptions
		byteStream            bool
		wantMin, wantMax      int
		wantStartsAtMinBuffer bool
	}{
		{"plain byte stream", network.ReadWaitOptions{}, true, copyMinBuffer, pool.RelayBufferSize, true},
		{"plain chunked", network.ReadWaitOptions{}, false, pool.RelayBufferSize, pool.RelayBufferSize, false},
		{"small headroom", network.ReadWaitOptions{FrontHeadroom: 14}, true, copyMinBuffer, pool.RelayBufferSize, true},
		{"vision-like headroom", network.ReadWaitOptions{FrontHeadroom: 1024, RearHeadroom: 1400}, true, copyMinBuffer * 2, pool.RelayBufferSize, false},
		{"mtu byte stream", network.ReadWaitOptions{MTU: 16383, FrontHeadroom: 34, RearHeadroom: 16}, true, copyMinBuffer, 16383 + 50, true},
		{"mtu chunked", network.ReadWaitOptions{MTU: 16383, FrontHeadroom: 34, RearHeadroom: 16}, false, 16383 + 50, 16383 + 50, false},
		{"mtu below min buffer", network.ReadWaitOptions{MTU: 1000, RearHeadroom: 24}, true, 1024, 1024, false},
	}
	for _, tc := range cases {
		size, minSize, maxSize := adaptiveSizeRange(tc.opts, tc.byteStream)
		if size != minSize {
			t.Errorf("%s: start %d != min %d", tc.name, size, minSize)
		}
		if minSize != tc.wantMin || maxSize != tc.wantMax {
			t.Errorf("%s: min/max = %d/%d, want %d/%d", tc.name, minSize, maxSize, tc.wantMin, tc.wantMax)
		}
		if tc.wantStartsAtMinBuffer && size != copyMinBuffer {
			t.Errorf("%s: start %d, want %d", tc.name, size, copyMinBuffer)
		}
		headroom := tc.opts.FrontHeadroom + tc.opts.RearHeadroom
		if minSize-headroom < copyMinPayload && minSize != maxSize {
			t.Errorf("%s: min payload %d below %d", tc.name, minSize-headroom, copyMinPayload)
		}
	}
}

func TestCopyExtendedHeadroomStaysInSizeClass(t *testing.T) {
	for _, tc := range []struct {
		name        string
		front, rear int
		wantFirst   int
	}{
		{"websocket-like", 14, 0, copyMinBuffer},
		{"vision-like", 1024, 1400, copyMinBuffer * 2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			payload := bytes.Repeat([]byte("s"), 8*pool.RelayBufferSize)
			src := bytes.NewReader(payload)
			dst := &slabSpyWriter{front: tc.front, rear: tc.rear}
			n, err := copyExtendedAdaptive(src, dst, NewExtendedReader(src), nil, nil)
			if err != nil {
				t.Fatal(err)
			}
			if n != int64(len(payload)) || !bytes.Equal(dst.out.Bytes(), payload) {
				t.Fatalf("n=%d dest=%d want %d", n, dst.out.Len(), len(payload))
			}
			t.Logf("slabs=%v lens=%v", dst.slabs, dst.lens)
			if dst.slabs[0] != tc.wantFirst {
				t.Fatalf("first slab %d, want %d (slabs %v)", dst.slabs[0], tc.wantFirst, dst.slabs)
			}
			reachedMax := false
			for i, slab := range dst.slabs {
				if slab > pool.RelayBufferSize {
					t.Fatalf("slab %d exceeds RelayBufferSize %d: %v", slab, pool.RelayBufferSize, dst.slabs)
				}
				if slab&(slab-1) != 0 {
					t.Fatalf("slab %d is not an allocator class: %v", slab, dst.slabs)
				}
				if dst.lens[i] > slab-tc.front-tc.rear {
					t.Fatalf("payload %d does not fit slab %d minus headroom %d", dst.lens[i], slab, tc.front+tc.rear)
				}
				if slab == pool.RelayBufferSize {
					reachedMax = true
				}
			}
			if !reachedMax {
				t.Fatalf("never reached RelayBufferSize: %v", dst.slabs)
			}
		})
	}
}

func TestCopyConnStartsSmallThenGrows(t *testing.T) {
	payload := bytes.Repeat([]byte("x"), pool.RelayBufferSize*2)
	spy := &sizeSpyReader{r: bytes.NewReader(payload)}
	var dest bytes.Buffer
	n, err := copyConn(&dest, spy)
	if err != nil {
		t.Fatal(err)
	}
	if n != int64(len(payload)) || dest.Len() != len(payload) {
		t.Fatalf("copied %d dest %d want %d", n, dest.Len(), len(payload))
	}
	if len(spy.sizes) == 0 || spy.sizes[0] != copyMinBuffer {
		t.Fatalf("first read size = %v, want %d", spy.sizes, copyMinBuffer)
	}
	grew := false
	for _, s := range spy.sizes {
		if s > copyMinBuffer {
			grew = true
			break
		}
	}
	if !grew {
		t.Fatalf("copy never grew from %d: %v", copyMinBuffer, spy.sizes)
	}
}

func TestCopyConnSmallTrafficDoesNotGrow(t *testing.T) {
	payload := []byte("hello")
	spy := &sizeSpyReader{r: bytes.NewReader(payload)}
	var dest bytes.Buffer
	if _, err := copyConn(&dest, spy); err != nil {
		t.Fatal(err)
	}
	for _, s := range spy.sizes {
		if s != copyMinBuffer {
			t.Fatalf("small payload used size %d, want %d", s, copyMinBuffer)
		}
	}
}

func TestCopyExtendedKeepsProtocolBuffer(t *testing.T) {
	payload := bytes.Repeat([]byte("p"), 64)
	src := &protocolChunkReader{r: bytes.NewReader(payload)}
	var dest bytes.Buffer
	n, err := copyConn(&dest, src)
	if err != nil {
		t.Fatal(err)
	}
	if n != int64(len(payload)) || !bytes.Equal(dest.Bytes(), payload) {
		t.Fatalf("protocol copy n=%d dest=%q", n, dest.Bytes())
	}
}

func TestCopyExtendedPreservesFrontHeadroom(t *testing.T) {
	payload := bytes.Repeat([]byte("h"), 32)
	src := bytes.NewReader(payload)
	dst := &headroomWriter{}
	n, err := copyExtendedAdaptive(src, dst, NewExtendedReader(src), nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if n != int64(len(payload)) {
		t.Fatalf("n=%d", n)
	}
	if !bytes.Equal(dst.out.Bytes(), payload) {
		t.Fatalf("headroom writer payload = %q", dst.out.Bytes())
	}
}

func TestCopyExtendedHonoursWriterMTU(t *testing.T) {
	const mtu = copyMinBuffer + copyMinBuffer/2
	payload := bytes.Repeat([]byte("m"), 8*mtu)
	src := bytes.NewReader(payload)
	dst := &mtuWriter{mtu: mtu}
	n, err := copyExtendedAdaptive(src, dst, NewExtendedReader(src), nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if n != int64(len(payload)) || !bytes.Equal(dst.out.Bytes(), payload) {
		t.Fatalf("n=%d dest=%d want %d", n, dst.out.Len(), len(payload))
	}
	// The first buffer is one copyMinBuffer slab with headroom carved out.
	if len(dst.sizes) == 0 || dst.sizes[0] != copyMinBuffer-dst.FrontHeadroom()-dst.RearHeadroom() {
		t.Fatalf("first payload = %v, want %d", dst.sizes, copyMinBuffer-dst.FrontHeadroom()-dst.RearHeadroom())
	}
	reachedMTU := false
	for _, s := range dst.sizes {
		if s > mtu {
			t.Fatalf("payload %d exceeds writer MTU %d: %v", s, mtu, dst.sizes)
		}
		if s == mtu {
			reachedMTU = true
		}
	}
	if !reachedMTU {
		t.Fatalf("byte stream never grew to MTU %d: %v", mtu, dst.sizes)
	}
}

func TestCopyExtendedProtocolSourceUsesWriterMTU(t *testing.T) {
	const mtu = 1000
	payload := bytes.Repeat([]byte("q"), 5*mtu)
	src := &chunkReader{r: bytes.NewReader(payload)}
	dst := &mtuWriter{mtu: mtu}
	n, err := copyExtendedAdaptive(src, dst, src, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if n != int64(len(payload)) || !bytes.Equal(dst.out.Bytes(), payload) {
		t.Fatalf("n=%d dest=%d want %d", n, dst.out.Len(), len(payload))
	}
	for _, s := range dst.sizes {
		if s != mtu {
			t.Fatalf("protocol source should read exactly MTU chunks, got %v", dst.sizes)
		}
	}
}

// chunkReader is an ExtendedReader with no minimum buffer demand; unlike
// protocolChunkReader it accepts whatever capacity the copier hands it.
type chunkReader struct {
	r io.Reader
}

func (c *chunkReader) Read(b []byte) (int, error) { return c.r.Read(b) }

func (c *chunkReader) ReadBuffer(buffer *buf.Buffer) error {
	n, err := c.r.Read(buffer.FreeBytes())
	buffer.Truncate(n)
	if n > 0 && err == io.EOF {
		return nil
	}
	return err
}

// readGate reports the first Read so the test knows the copier has taken
// its buffer and is parked on the pipe. It stays a plain io.Reader, so
// copyConn keeps the same fallback path as a bare net.Pipe.
type readGate struct {
	io.Reader
	once  sync.Once
	ready chan<- struct{}
}

func (g *readGate) Read(p []byte) (int, error) {
	g.once.Do(func() { g.ready <- struct{}{} })
	return g.Reader.Read(p)
}

func TestIdlePipeRelayHeapPerConn(t *testing.T) {
	const conns = 256
	runtime.GC()
	runtime.GC()
	var before runtime.MemStats
	runtime.ReadMemStats(&before)

	keep := make([]stdnet.Conn, 0, conns*2)
	ready := make(chan struct{}, conns*2)
	for i := 0; i < conns; i++ {
		leftA, leftB := stdnet.Pipe()
		rightA, rightB := stdnet.Pipe()
		keep = append(keep, leftB, rightA)
		go func(dst io.Writer, src io.Reader) {
			_, _ = copyConn(dst, &readGate{Reader: src, ready: ready})
		}(leftA, rightB)
		go func(dst io.Writer, src io.Reader) {
			_, _ = copyConn(dst, &readGate{Reader: src, ready: ready})
		}(rightA, leftB)
	}
	deadline := time.After(5 * time.Second)
	for i := 0; i < conns*2; i++ {
		select {
		case <-ready:
		case <-deadline:
			t.Fatalf("only %d of %d copiers reached Read", i, conns*2)
		}
	}
	runtime.GC()
	var after runtime.MemStats
	runtime.ReadMemStats(&after)
	runtime.KeepAlive(keep)

	delta := int64(after.HeapInuse) - int64(before.HeapInuse)
	if delta < 0 {
		delta = 0
	}
	per := delta / conns
	t.Logf("idle copyConn heap %d KiB / %d conns = %d bytes/conn", delta>>10, conns, per)
	if per > 16*1024 {
		t.Fatalf("idle heap %d bytes/conn, want <= 16 KiB (two 4 KiB buffers plus overhead)", per)
	}
}

func TestCopySourceIsByteStreamClassification(t *testing.T) {
	if !copySourceIsByteStream(bytes.NewReader(nil)) {
		t.Fatal("plain reader should be a byte stream")
	}
	if !copySourceIsByteStream(NewExtendedReader(bytes.NewReader(nil))) {
		t.Fatal("generic extended wrapper should be a byte stream")
	}
	if copySourceIsByteStream(&protocolChunkReader{r: bytes.NewReader(nil)}) {
		t.Fatal("protocol ReadBuffer must keep RelayBufferSize")
	}
	var _ network.ExtendedReader = &protocolChunkReader{}
}
