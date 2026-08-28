package vision

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"sync"
	"sync/atomic"
	"unsafe"

	"github.com/Miku0139oao/aster-core/common/buf"
	N "github.com/Miku0139oao/aster-core/common/net"
	"github.com/Miku0139oao/aster-core/log"

	"github.com/gofrs/uuid/v5"
)

var _ N.ExtendedConn = (*Conn)(nil)

// A Conn relays in both directions at once, and the two directions do not keep
// to their own state: FilterTLS mutates the sniffing fields from the read and
// the write path alike, while Upstream, FrontHeadroom and the Replaceable
// predicates are called from whichever direction the copier happens to be
// serving. The state below is therefore synchronized rather than owned.
type Conn struct {
	net.Conn // should be *vless.Conn
	N.ExtendedReader
	N.ExtendedWriter
	userUUID uuid.UUID

	// [*tls.Conn] or other tls-like [net.Conn]'s internal variables
	netConn  net.Conn      // tlsConn.NetConn()
	input    *bytes.Reader // &tlsConn.input or nil
	rawInput *bytes.Buffer // &tlsConn.rawInput or nil

	// TLS sniffing state, mutated by FilterTLS on both paths.
	filterMu             sync.Mutex
	packetsToFilter      int
	isTLS                bool
	isTLS12orAbove       bool
	enableXTLS           bool
	cipher               uint16
	remainingServerHello uint16

	// Owned by the read path.
	readRemainingBuffer  *buf.Buffer
	readRemainingContent int
	readRemainingPadding int

	// Owned by the write path.
	writeOnceUserUUID []byte

	// Read across both directions.
	readProcess                atomic.Bool
	readFilterUUID             atomic.Bool
	readLastCommand            atomic.Uint32
	writeFilterApplicationData atomic.Bool
	writeDirect                atomic.Bool
	writeHandshake             atomic.Bool
}

// filterState is a snapshot of the sniffing state, so that the write path can
// make its padding decisions from a consistent view.
type filterState struct {
	packetsToFilter int
	isTLS           bool
	isTLS12orAbove  bool
	enableXTLS      bool
}

func (vc *Conn) filterState() filterState {
	vc.filterMu.Lock()
	defer vc.filterMu.Unlock()
	return filterState{
		packetsToFilter: vc.packetsToFilter,
		isTLS:           vc.isTLS,
		isTLS12orAbove:  vc.isTLS12orAbove,
		enableXTLS:      vc.enableXTLS,
	}
}

func (vc *Conn) lastCommand() byte {
	return byte(vc.readLastCommand.Load())
}

// applyPadding also publishes that the once-only user UUID has been consumed,
// which NeedHandshake and FrontHeadroom read from the other direction.
func (vc *Conn) applyPadding(buffer *buf.Buffer, command byte, paddingTLS bool) {
	ApplyPadding(buffer, command, &vc.writeOnceUserUUID, paddingTLS)
	if vc.writeOnceUserUUID == nil {
		vc.writeHandshake.Store(false)
	}
}

func (vc *Conn) Read(b []byte) (int, error) {
	if vc.readProcess.Load() {
		buffer := buf.With(b)
		err := vc.ReadBuffer(buffer)
		if unsafe.SliceData(buffer.Bytes()) != unsafe.SliceData(b) { // buffer.Bytes() not at the beginning of b
			copy(b, buffer.Bytes())
		}
		return buffer.Len(), err
	}
	return vc.ExtendedReader.Read(b)
}

func (vc *Conn) ReadBuffer(buffer *buf.Buffer) error {
	if vc.readRemainingBuffer != nil {
		_, err := buffer.ReadOnceFrom(vc.readRemainingBuffer)
		if vc.readRemainingBuffer.IsEmpty() {
			vc.readRemainingBuffer.Release()
			vc.readRemainingBuffer = nil
		}
		return err
	}
	if vc.readRemainingContent > 0 {
		readSize := xrayBufSize          // at least read xrayBufSize
		if buffer.FreeLen() > readSize { // input buffer larger than xrayBufSize, read as much as possible
			readSize = buffer.FreeLen()
		}
		if readSize > vc.readRemainingContent { // don't read out of bounds
			readSize = vc.readRemainingContent
		}

		readBuffer := buffer
		if buffer.FreeLen() < readSize {
			readBuffer = buf.NewSize(readSize)
			vc.readRemainingBuffer = readBuffer
		}
		n, err := vc.ExtendedReader.Read(readBuffer.FreeBytes()[:readSize])
		readBuffer.Truncate(n)
		vc.readRemainingContent -= n
		vc.FilterTLS(readBuffer.Bytes())
		if vc.readRemainingBuffer != nil {
			innerErr := vc.ReadBuffer(buffer) // back to top but not losing err
			if err != nil {
				err = innerErr
			}
		}
		return err
	}
	if vc.readRemainingPadding > 0 {
		n, err := io.CopyN(io.Discard, vc.ExtendedReader, int64(vc.readRemainingPadding))
		if err != nil {
			return err
		}
		vc.readRemainingPadding -= int(n)
	}
	if vc.readProcess.Load() {
		switch vc.lastCommand() {
		case commandPaddingContinue:
			// if vc.isTLS || vc.packetsToFilter > 0 {
			need := PaddingHeaderLen
			if !vc.readFilterUUID.Load() {
				need = PaddingHeaderLen - uuid.Size
			}
			var header []byte
			if buffer.FreeLen() < need {
				header = make([]byte, need)
			} else {
				header = buffer.FreeBytes()[:need]
			}
			_, err := io.ReadFull(vc.ExtendedReader, header)
			if err != nil {
				return err
			}
			if vc.readFilterUUID.Load() {
				vc.readFilterUUID.Store(false)
				if !bytes.Equal(vc.userUUID[:], header[:uuid.Size]) {
					err = fmt.Errorf("XTLS Vision server responded unknown UUID: %s", uuid.FromBytesOrNil(header[:uuid.Size]))
					log.Errorln(err.Error())
					return err
				}
				header = header[uuid.Size:]
			}
			vc.readRemainingPadding = int(binary.BigEndian.Uint16(header[3:]))
			vc.readRemainingContent = int(binary.BigEndian.Uint16(header[1:]))
			command := header[0]
			vc.readLastCommand.Store(uint32(command))
			if log.Enabled(log.DEBUG) {
				log.Debugln("XTLS Vision read padding: command=%d, payloadLen=%d, paddingLen=%d",
					command, vc.readRemainingContent, vc.readRemainingPadding)
			}
			return vc.ReadBuffer(buffer)
			//}
		case commandPaddingEnd:
			vc.readProcess.Store(false)
			return vc.ReadBuffer(buffer)
		case commandPaddingDirect:
			needReturn := false
			if vc.input != nil {
				_, err := buffer.ReadOnceFrom(vc.input)
				if err != nil {
					if !errors.Is(err, io.EOF) {
						return err
					}
				}
				if vc.input.Len() == 0 {
					needReturn = true
					*vc.input = bytes.Reader{} // full reset
					vc.input = nil
				} else { // buffer is full
					return nil
				}
			}
			if vc.rawInput != nil {
				_, err := buffer.ReadOnceFrom(vc.rawInput)
				if err != nil {
					if !errors.Is(err, io.EOF) {
						return err
					}
				}
				needReturn = true
				if vc.rawInput.Len() == 0 {
					*vc.rawInput = bytes.Buffer{} // full reset
					vc.rawInput = nil
				}
			}
			if vc.input == nil && vc.rawInput == nil {
				vc.readProcess.Store(false)
				vc.ExtendedReader = N.NewExtendedReader(vc.netConn)
				if log.Enabled(log.DEBUG) {
					log.Debugln("XTLS Vision direct read start")
				}
			}
			if needReturn {
				return nil
			}
		default:
			err := fmt.Errorf("XTLS Vision read unknown command: %d", vc.lastCommand())
			if log.Enabled(log.DEBUG) {
				log.Debugln(err.Error())
			}
			return err
		}
	}
	return vc.ExtendedReader.ReadBuffer(buffer)
}

func (vc *Conn) Write(p []byte) (int, error) {
	if vc.writeFilterApplicationData.Load() {
		return N.WriteBuffer(vc, buf.As(p))
	}
	return vc.ExtendedWriter.Write(p)
}

func (vc *Conn) WriteBuffer(buffer *buf.Buffer) (err error) {
	if !vc.writeFilterApplicationData.Load() {
		return vc.ExtendedWriter.WriteBuffer(buffer)
	}
	if buffer.IsEmpty() {
		vc.applyPadding(buffer, commandPaddingContinue, true) // we do a long padding to hide vless header
		return vc.ExtendedWriter.WriteBuffer(buffer)
	}

	vc.FilterTLS(buffer.Bytes())
	const bufferLimit = xrayBufSize - PaddingHeaderLen
	if buffer.Len() < bufferLimit {
		return vc.writePaddedBuffer(buffer)
	}
	buffers := vc.ReshapeBuffer(buffer)
	for i, buffer := range buffers {
		err = vc.writePaddedBuffer(buffer)
		if err != nil {
			buf.ReleaseMulti(buffers[i:]) // release unwritten buffers
			return
		}
	}
	return err
}

func (vc *Conn) writePaddedBuffer(buffer *buf.Buffer) error {
	command := commandPaddingContinue
	if vc.writeFilterApplicationData.Load() {
		filter := vc.filterState()
		if filter.isTLS && buffer.Len() > 6 && bytes.Equal(tlsApplicationDataStart, buffer.To(3)) {
			command = commandPaddingEnd
			if filter.enableXTLS {
				command = commandPaddingDirect
				vc.writeDirect.Store(true)
			}
			vc.writeFilterApplicationData.Store(false)
		} else if !filter.isTLS12orAbove && filter.packetsToFilter <= 1 {
			command = commandPaddingEnd
			vc.writeFilterApplicationData.Store(false)
		}
		vc.applyPadding(buffer, command, filter.isTLS)
	}
	if err := vc.ExtendedWriter.WriteBuffer(buffer); err != nil {
		return err
	}
	if command == commandPaddingDirect {
		vc.ExtendedWriter = N.NewExtendedWriter(vc.netConn)
		if log.Enabled(log.DEBUG) {
			log.Debugln("XTLS Vision direct write start")
		}
	}
	return nil
}

func (vc *Conn) FrontHeadroom() int {
	fontHeadroom := PaddingHeaderLen - uuid.Size
	if vc.readFilterUUID.Load() || vc.writeHandshake.Load() {
		fontHeadroom = PaddingHeaderLen
	}
	if vc.writeFilterApplicationData.Load() { // The writer may be replaced, add the required value for vc.netConn
		if abs := N.CalculateFrontHeadroom(vc.netConn) - N.CalculateFrontHeadroom(vc.Conn); abs > 0 {
			fontHeadroom += abs
		}
	}
	return fontHeadroom
}

func (vc *Conn) RearHeadroom() int {
	rearHeadroom := 500 + 900
	if vc.writeFilterApplicationData.Load() { // The writer may be replaced, add the required value for vc.netConn
		if abs := N.CalculateRearHeadroom(vc.netConn) - N.CalculateRearHeadroom(vc.Conn); abs > 0 {
			rearHeadroom += abs
		}
	}
	return rearHeadroom
}

func (vc *Conn) NeedHandshake() bool {
	return vc.writeHandshake.Load()
}

func (vc *Conn) NeedAdditionalReadDeadline() bool {
	return true
}

func (vc *Conn) Upstream() any {
	if vc.writeDirect.Load() ||
		vc.lastCommand() == commandPaddingDirect {
		return vc.netConn
	}
	return vc.Conn
}

func (vc *Conn) ReaderPossiblyReplaceable() bool {
	return vc.readProcess.Load()
}

func (vc *Conn) ReaderReplaceable() bool {
	if !vc.readProcess.Load() &&
		vc.lastCommand() == commandPaddingDirect {
		return true
	}
	return false
}

func (vc *Conn) WriterPossiblyReplaceable() bool {
	return vc.writeFilterApplicationData.Load()
}

func (vc *Conn) WriterReplaceable() bool {
	return vc.writeDirect.Load()
}

func (vc *Conn) Close() error {
	if vc.ReaderReplaceable() || vc.WriterReplaceable() { // ignore send closeNotify alert in tls.Conn
		return vc.netConn.Close()
	}
	return vc.Conn.Close()
}
