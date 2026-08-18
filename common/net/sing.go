package net

import (
	"errors"
	"io"
	"net"
	"syscall"

	"github.com/Miku0139oao/aster-core/common/net/deadline"
	"github.com/Miku0139oao/aster-core/common/pool"

	"github.com/metacubex/sing/common"
	"github.com/metacubex/sing/common/bufio"
	"github.com/metacubex/sing/common/network"
)

var (
	NewExtendedConn   = bufio.NewExtendedConn
	NewExtendedWriter = bufio.NewExtendedWriter
	NewExtendedReader = bufio.NewExtendedReader
)

type (
	ExtendedConn   = network.ExtendedConn
	ExtendedWriter = network.ExtendedWriter
	ExtendedReader = network.ExtendedReader
)

var WriteBuffer = bufio.WriteBuffer

type ReadWaitOptions = network.ReadWaitOptions

var (
	NewReadWaitOptions     = network.NewReadWaitOptions
	CalculateFrontHeadroom = network.CalculateFrontHeadroom
	CalculateRearHeadroom  = network.CalculateRearHeadroom
)

type (
	ReaderWithUpstream = network.ReaderWithUpstream
	WithUpstreamReader = network.WithUpstreamReader
	WriterWithUpstream = network.WriterWithUpstream
	WithUpstreamWriter = network.WithUpstreamWriter
	WithUpstream       = common.WithUpstream
)

var (
	UnwrapReader = network.UnwrapReader
	UnwrapWriter = network.UnwrapWriter
)

func NewDeadlineConn(conn net.Conn) ExtendedConn {
	if deadline.IsPipe(conn) || deadline.IsPipe(UnwrapReader(conn)) {
		return NewExtendedConn(conn) // pipe always have correctly deadline implement
	}
	if deadline.IsConn(conn) || deadline.IsConn(UnwrapReader(conn)) {
		return NewExtendedConn(conn) // was a *deadline.Conn
	}
	return deadline.NewConn(conn)
}

func NeedHandshake(conn any) bool {
	if earlyConn, isEarlyConn := common.Cast[network.EarlyConn](conn); isEarlyConn && earlyConn.NeedHandshake() {
		return true
	}
	return false
}

type CountFunc = network.CountFunc

var Pipe = deadline.Pipe

func closeWrite(writer io.Closer) error {
	if c, ok := common.Cast[network.WriteCloser](writer); ok {
		return c.CloseWrite()
	}
	return writer.Close()
}

// copyConn keeps the feature-aware sing copy path for protocol-specific and
// zero-copy-capable connections. Once both sides unwrap to ordinary streams,
// a reusable byte slice avoids allocating a buf.Buffer wrapper for every read.
func copyConn(destination io.Writer, source io.Reader) (n int64, err error) {
	originDestination, originSource := destination, source
	source, readCounters := network.UnwrapCountReader(source, nil)
	destination, writeCounters := network.UnwrapCountWriter(destination, nil)

	_, sourceExtended := source.(network.ExtendedReader)
	_, destinationExtended := destination.(network.ExtendedWriter)
	_, sourceCached := source.(network.CachedReader)
	_, sourceReplaceable := source.(network.ReaderPossiblyReplaceable)
	_, destinationReplaceable := destination.(network.WriterPossiblyReplaceable)
	_, sourceSyscall := source.(syscall.Conn)
	_, destinationSyscall := destination.(syscall.Conn)
	if sourceExtended || destinationExtended || sourceCached || sourceReplaceable || destinationReplaceable || (sourceSyscall && destinationSyscall) {
		return bufio.Copy(originDestination, originSource)
	}

	buffer := pool.Get(pool.RelayBufferSize)
	defer func() { _ = pool.Put(buffer) }()
	firstWrite := true
	for {
		readN, readErr := source.Read(buffer)
		if readN > 0 {
			writeN, writeErr := destination.Write(buffer[:readN])
			if writeN != readN && writeErr == nil {
				writeErr = io.ErrShortWrite
			}
			if writeErr != nil {
				if firstWrite {
					writeErr = network.ReportHandshakeFailure(originSource, writeErr)
				}
				return n, writeErr
			}
			transferred := int64(readN)
			n += transferred
			for _, counter := range readCounters {
				counter(transferred)
			}
			for _, counter := range writeCounters {
				counter(transferred)
			}
			firstWrite = false
		}
		if readErr != nil {
			if errors.Is(readErr, io.EOF) {
				return n, nil
			}
			return n, readErr
		}
	}
}

// Relay copies between left and right bidirectionally.
// like [bufio.CopyConn] but remove unneeded [context.Context] handle and the cost of [task.Group]
func Relay(leftConn, rightConn net.Conn) {
	defer func() {
		_ = leftConn.Close()
		_ = rightConn.Close()
	}()

	ch := make(chan struct{})
	go func() {
		_, err := copyConn(leftConn, rightConn)
		if err == nil {
			_ = closeWrite(leftConn)
		} else {
			_ = leftConn.Close()
		}
		close(ch)
	}()

	_, err := copyConn(rightConn, leftConn)
	if err == nil {
		_ = closeWrite(rightConn)
	} else {
		_ = rightConn.Close()
	}
	<-ch
}
