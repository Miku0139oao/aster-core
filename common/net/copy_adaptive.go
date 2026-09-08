package net

import (
	"errors"
	"io"
	"math/bits"
	"syscall"

	"github.com/Miku0139oao/aster-core/common/net/deadline"
	"github.com/Miku0139oao/aster-core/common/pool"

	"github.com/metacubex/sing/common/buf"
	"github.com/metacubex/sing/common/bufio"
	"github.com/metacubex/sing/common/network"
)

const (
	copyMinBuffer   = 4 << 10
	copyMinPayload  = copyMinBuffer / 2
	copyGrowAfter   = 2
	copyShrinkAfter = 4
)

func copyFeatureAware(originDestination io.Writer, originSource io.Reader) (n int64, err error) {
	source, destination := originSource, originDestination
	var readCounters, writeCounters []network.CountFunc
	possibly := bufio.MaxCopyExtendedOnceTimes
	for {
		source, readCounters = network.UnwrapCountReader(source, readCounters)
		destination, writeCounters = network.UnwrapCountWriter(destination, writeCounters)
		if cachedSrc, isCached := source.(network.CachedReader); isCached {
			cachedBuffer := cachedSrc.ReadCached()
			if cachedBuffer != nil {
				dataLen := cachedBuffer.Len()
				_, err = destination.Write(cachedBuffer.Bytes())
				cachedBuffer.Release()
				if err != nil {
					return
				}
				for _, counter := range readCounters {
					counter(int64(dataLen))
				}
				for _, counter := range writeCounters {
					counter(int64(dataLen))
				}
				n += int64(dataLen)
				continue
			}
		}
		replaceableReader, isReaderPossiblyReplaceable := source.(network.ReaderPossiblyReplaceable)
		replaceableWriter, isWriterPossiblyReplaceable := destination.(network.WriterPossiblyReplaceable)
		if possibly != 0 &&
			(isReaderPossiblyReplaceable && replaceableReader.ReaderPossiblyReplaceable()) ||
			(isWriterPossiblyReplaceable && replaceableWriter.WriterPossiblyReplaceable()) {
			possibly--
			var onceN int64
			onceN, err = bufio.CopyExtendedOnce(destination, source, readCounters, writeCounters)
			n += onceN
			if err != nil {
				if n == onceN {
					err = network.ReportHandshakeFailure(originSource, err)
				}
				if errors.Is(err, io.EOF) {
					err = nil
				}
				return
			}
			continue
		}
		break
	}

	if _, srcSyscall := source.(syscall.Conn); srcSyscall {
		var rest int64
		rest, err = bufio.CopyWithCounters(destination, source, originSource, readCounters, writeCounters)
		n += rest
		return
	}

	rest, copyErr := copyExtendedAdaptive(originSource, NewExtendedWriter(destination), NewExtendedReader(source), readCounters, writeCounters)
	n += rest
	return n, copyErr
}

func copyAdaptive(destination io.Writer, source io.Reader, originSource io.Reader, readCounters, writeCounters []network.CountFunc) (n int64, err error) {
	size := copyMinBuffer
	maxSize := pool.RelayBufferSize
	if size > maxSize {
		size = maxSize
	}
	buffer := pool.Get(size)
	defer func() { _ = pool.Put(buffer) }()
	firstWrite := true
	var full, small int
	for {
		readN, readErr := source.Read(buffer)
		if readN > 0 {
			writeN, writeErr := destination.Write(buffer[:readN])
			if writeN != readN && writeErr == nil {
				writeErr = io.ErrShortWrite
			}
			transferred := int64(writeN)
			n += transferred
			for _, counter := range readCounters {
				counter(transferred)
			}
			for _, counter := range writeCounters {
				counter(transferred)
			}
			if writeErr != nil {
				if firstWrite {
					writeErr = network.ReportHandshakeFailure(originSource, writeErr)
				}
				return n, writeErr
			}
			firstWrite = false
			if next := nextCopySize(size, size, readN, copyMinBuffer, maxSize, &full, &small); next != size {
				_ = pool.Put(buffer)
				size = next
				buffer = pool.Get(size)
			}
		}
		if readErr != nil {
			if errors.Is(readErr, io.EOF) {
				return n, nil
			}
			return n, readErr
		}
	}
}

func copyExtendedAdaptive(originSource io.Reader, destination network.ExtendedWriter, source network.ExtendedReader, readCounters, writeCounters []network.CountFunc) (n int64, err error) {
	options := network.NewReadWaitOptions(source, destination)
	size, minSize, maxSize := adaptiveSizeRange(options, copySourceIsByteStream(source))
	var notFirstTime bool
	var full, small int
	for {
		buffer := newAdaptiveReadBuffer(options, size)
		capacity := buffer.FreeLen()
		err = source.ReadBuffer(buffer)
		if err != nil {
			buffer.Release()
			if errors.Is(err, io.EOF) {
				err = nil
				return
			}
			return
		}
		dataLen := buffer.Len()
		options.PostReturn(buffer)
		err = destination.WriteBuffer(buffer)
		if err != nil {
			buffer.Leak()
			if !notFirstTime {
				err = network.ReportHandshakeFailure(originSource, err)
			}
			return
		}
		n += int64(dataLen)
		for _, counter := range readCounters {
			counter(int64(dataLen))
		}
		for _, counter := range writeCounters {
			counter(int64(dataLen))
		}
		notFirstTime = true
		if minSize == maxSize {
			continue
		}
		size = nextCopySize(size, capacity, dataLen, minSize, maxSize, &full, &small)
	}
}

// adaptiveSizeRange returns the starting, minimum and maximum total buffer
// size (headroom included) for copyExtendedAdaptive.
//
// sing sizes the buffer to MTU + headroom when the writer declares an MTU,
// and to one whole BufferSize slab otherwise, carving the headroom out of
// the slab. Both are kept here: adding headroom on top of a power-of-two
// payload would push buf.NewSize into the next allocator class (4 KiB + 8
// bytes occupies an 8 KiB slab, 32 KiB + 8 bytes a 64 KiB slab).
//
// Byte-stream sources start at copyMinBuffer and double on full reads up
// to the maximum; chunked protocol readers always get the maximum.
func adaptiveSizeRange(options network.ReadWaitOptions, byteStream bool) (size, minSize, maxSize int) {
	headroom := 0
	if options.FrontHeadroom > 0 {
		headroom += options.FrontHeadroom
	}
	if options.RearHeadroom > 0 {
		headroom += options.RearHeadroom
	}
	if options.MTU > 0 {
		maxSize = options.MTU + headroom
	} else {
		maxSize = pool.RelayBufferSize
		if maxSize-headroom < copyMinPayload {
			// Headroom leaves no usable room in a RelayBufferSize slab; fall
			// back to payload + headroom rather than looping on empty reads.
			maxSize = pool.RelayBufferSize + headroom
		}
	}
	minSize = maxSize
	if byteStream {
		minSize = copyMinBuffer
		for minSize-headroom < copyMinPayload && minSize*2 <= maxSize {
			minSize *= 2
		}
		if minSize > maxSize {
			minSize = maxSize
		}
	}
	return minSize, minSize, maxSize
}

func newAdaptiveReadBuffer(options network.ReadWaitOptions, size int) *buf.Buffer {
	buffer := buf.NewSize(size)
	if options.FrontHeadroom > 0 {
		buffer.Resize(options.FrontHeadroom, 0)
	}
	if options.RearHeadroom > 0 {
		buffer.Reserve(options.RearHeadroom)
	}
	return buffer
}

// nextCopySize returns the next total buffer size. readN is judged against
// capacity, the part of the buffer a read may fill (size minus headroom):
// copyGrowAfter consecutive full reads double the size, copyShrinkAfter
// consecutive reads under a quarter of capacity halve it.
func nextCopySize(size, capacity, readN, minSize, maxSize int, full, small *int) int {
	if size <= 0 {
		return minSize
	}
	if readN == capacity {
		*full++
		*small = 0
		if *full >= copyGrowAfter && size < maxSize {
			*full = 0
			next := size * 2
			if next > maxSize {
				next = maxSize
			}
			return next
		}
		return size
	}
	*full = 0
	if readN > 0 && readN < capacity/4 {
		*small++
		if *small >= copyShrinkAfter && size > minSize {
			*small = 0
			// maxSize may be MTU + headroom rather than a power of two;
			// shrink back onto an allocator class.
			next := 1 << (bits.Len(uint(size/2)) - 1)
			if next < minSize {
				next = minSize
			}
			return next
		}
		return size
	}
	*small = 0
	return size
}

// copySourceIsByteStream reports whether ReadBuffer is a generic fill of
// FreeBytes (TCP/TLS/pipe). Chunked protocol readers (VMess, Shadowsocks)
// may consume a length prefix then require a buffer large enough for the
// whole chunk; those keep RelayBufferSize.
func copySourceIsByteStream(source io.Reader) bool {
	cur := source
	for i := 0; i < 16 && cur != nil; i++ {
		if _, ok := cur.(syscall.Conn); ok {
			return true
		}
		switch c := cur.(type) {
		case *bufio.ExtendedReaderWrapper:
			cur = c.Reader
			continue
		case *bufio.ExtendedConnWrapper:
			cur = c.Conn
			continue
		case *BufferedConn:
			if c.ExtendedConn != nil {
				cur = c.ExtendedConn
				continue
			}
			return true
		case *deadline.Conn:
			cur = c.ExtendedConn
			continue
		case *CachedConn:
			cur = c.ExtendedConn
			continue
		}
		if _, ok := cur.(network.ExtendedReader); ok {
			return false
		}
		return true
	}
	return false
}
