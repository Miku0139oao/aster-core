package session

import (
	"crypto/md5"
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"runtime/debug"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"github.com/Miku0139oao/aster-core/common/pool"
	"github.com/Miku0139oao/aster-core/log"
	"github.com/Miku0139oao/aster-core/transport/anytls/padding"
	"github.com/Miku0139oao/aster-core/transport/anytls/util"
)

type Session struct {
	conn     net.Conn
	connLock sync.Mutex

	streams    map[uint32]*Stream
	streamId   atomic.Uint32
	streamLock sync.RWMutex

	dieOnce sync.Once
	die     chan struct{}
	dieHook func()

	synDone     map[uint32]func()
	synDoneLock sync.Mutex

	// pool
	seq       uint64
	idleSince time.Time
	padding   *atomic.Pointer[padding.PaddingFactory]

	peerVersion atomic.Uint32

	// client
	isClient       bool
	sendPadding    bool
	buffering      bool
	buffer         []byte
	padBuf         []byte
	pktSizes       []int
	pktCounter     atomic.Uint32
	clientMetadata string

	// server
	onNewStream func(stream *Stream)
}

func NewClientSession(conn net.Conn, _padding *atomic.Pointer[padding.PaddingFactory], clientMetadata string) *Session {
	s := &Session{
		conn:           conn,
		isClient:       true,
		sendPadding:    true,
		padding:        _padding,
		clientMetadata: clientMetadata,
	}
	s.die = make(chan struct{})
	s.streams = make(map[uint32]*Stream)
	return s
}

func NewServerSession(conn net.Conn, onNewStream func(stream *Stream), _padding *atomic.Pointer[padding.PaddingFactory]) *Session {
	s := &Session{
		conn:        conn,
		onNewStream: onNewStream,
		padding:     _padding,
	}
	s.die = make(chan struct{})
	s.streams = make(map[uint32]*Stream)
	return s
}

func (s *Session) Run() {
	if !s.isClient {
		s.recvLoop()
		return
	}

	settings := util.StringMap{
		"v":           "2",
		"client":      s.clientMetadata,
		"padding-md5": s.padding.Load().Md5,
	}
	f := newFrame(cmdSettings, 0)
	f.data = settings.ToBytes()
	s.buffering = true
	s.writeControlFrame(f)

	go s.recvLoop()
}

// IsClosed does a safe check to see if we have shutdown
func (s *Session) IsClosed() bool {
	select {
	case <-s.die:
		return true
	default:
		return false
	}
}

// Close is used to close the session and all streams.
func (s *Session) Close() error {
	var once bool
	s.dieOnce.Do(func() {
		_ = s.conn.SetDeadline(time.Now())
		close(s.die)
		once = true
	})
	if once {
		s.cancelAllSynDone()
		if s.dieHook != nil {
			s.dieHook()
			s.dieHook = nil
		}
		s.streamLock.Lock()
		for _, stream := range s.streams {
			stream.closeLocally()
		}
		s.streams = make(map[uint32]*Stream)
		s.streamLock.Unlock()
		return s.conn.Close()
	} else {
		return io.ErrClosedPipe
	}
}

// OpenStream is used to create a new stream for CLIENT
func (s *Session) OpenStream() (*Stream, error) {
	if s.IsClosed() {
		return nil, io.ErrClosedPipe
	}

	sid := s.streamId.Add(1)
	stream := newStream(sid, s)

	if sid >= 2 && s.peerVersion.Load() >= 2 {
		s.synDoneLock.Lock()
		if s.synDone == nil {
			s.synDone = make(map[uint32]func())
		}
		s.synDone[sid] = util.NewDeadlineWatcher(time.Second*3, func() {
			s.Close()
		})
		s.synDoneLock.Unlock()
	}

	if _, err := s.writeControlFrame(newFrame(cmdSYN, sid)); err != nil {
		s.cancelSynDone(sid)
		return nil, err
	}

	s.buffering = false // proxy Write it's SocksAddr to flush the buffer

	s.streamLock.Lock()
	defer s.streamLock.Unlock()
	select {
	case <-s.die:
		s.cancelSynDone(sid)
		return nil, io.ErrClosedPipe
	default:
		s.streams[sid] = stream
		return stream, nil
	}
}

func (s *Session) recvLoop() error {
	defer func() {
		if r := recover(); r != nil {
			log.Errorln("[BUG] %v %s", r, string(debug.Stack()))
		}
	}()
	defer s.Close()

	var receivedSettingsFromClient bool
	var hdr rawHeader

	for {
		if s.IsClosed() {
			return io.ErrClosedPipe
		}
		// read header first
		if _, err := io.ReadFull(s.conn, hdr[:]); err == nil {
			sid := hdr.StreamID()
			switch hdr.Cmd() {
			case cmdPSH:
				if hdr.Length() > 0 {
					buffer := pool.Get(int(hdr.Length()))
					if _, err := io.ReadFull(s.conn, buffer); err == nil {
						s.streamLock.RLock()
						stream, ok := s.streams[sid]
						s.streamLock.RUnlock()
						if ok {
							stream.pipeW.Write(buffer)
						}
						pool.Put(buffer)
					} else {
						pool.Put(buffer)
						return err
					}
				}
			case cmdSYN: // should be server only
				if err := s.discardFramePayload(hdr.Length()); err != nil {
					return err
				}
				if !s.isClient && !receivedSettingsFromClient {
					f := newFrame(cmdAlert, 0)
					f.data = []byte("client did not send its settings")
					s.writeControlFrame(f)
					return nil
				}
				s.streamLock.Lock()
				if _, ok := s.streams[sid]; !ok {
					stream := newStream(sid, s)
					s.streams[sid] = stream
					go func() {
						if s.onNewStream != nil {
							s.onNewStream(stream)
						} else {
							stream.Close()
						}
					}()
				}
				s.streamLock.Unlock()
			case cmdSYNACK: // should be client only
				s.synDoneLock.Lock()
				if done, ok := s.synDone[sid]; ok {
					done()
					delete(s.synDone, sid)
				}
				s.synDoneLock.Unlock()
				if hdr.Length() > 0 {
					buffer := pool.Get(int(hdr.Length()))
					if _, err := io.ReadFull(s.conn, buffer); err != nil {
						pool.Put(buffer)
						return err
					}
					// report error
					s.streamLock.RLock()
					stream, ok := s.streams[sid]
					s.streamLock.RUnlock()
					if ok {
						stream.closeWithError(fmt.Errorf("remote: %s", string(buffer)))
					}
					pool.Put(buffer)
				}
			case cmdFIN:
				if err := s.discardFramePayload(hdr.Length()); err != nil {
					return err
				}
				s.streamLock.Lock()
				stream, ok := s.streams[sid]
				delete(s.streams, sid)
				s.streamLock.Unlock()
				if ok {
					stream.closeLocally()
				}
			case cmdWaste:
				if hdr.Length() > 0 {
					buffer := pool.Get(int(hdr.Length()))
					if _, err := io.ReadFull(s.conn, buffer); err != nil {
						pool.Put(buffer)
						return err
					}
					pool.Put(buffer)
				}
			case cmdSettings:
				if hdr.Length() > 0 {
					buffer := pool.Get(int(hdr.Length()))
					if _, err := io.ReadFull(s.conn, buffer); err != nil {
						pool.Put(buffer)
						return err
					}
					if !s.isClient {
						receivedSettingsFromClient = true
						m := util.StringMapFromBytes(buffer)
						paddingF := s.padding.Load()
						if m["padding-md5"] != paddingF.Md5 {
							f := newFrame(cmdUpdatePaddingScheme, 0)
							f.data = paddingF.RawScheme
							_, err = s.writeControlFrame(f)
							if err != nil {
								pool.Put(buffer)
								return err
							}
						}
						// check client's version
						if v, err := strconv.Atoi(m["v"]); err == nil && v >= 2 {
							s.peerVersion.Store(uint32(v))
							// send cmdServerSettings
							f := newFrame(cmdServerSettings, 0)
							f.data = util.StringMap{
								"v": "2",
							}.ToBytes()
							_, err = s.writeControlFrame(f)
							if err != nil {
								pool.Put(buffer)
								return err
							}
						}
					}
					pool.Put(buffer)
				}
			case cmdAlert:
				if hdr.Length() > 0 {
					buffer := pool.Get(int(hdr.Length()))
					if _, err := io.ReadFull(s.conn, buffer); err != nil {
						pool.Put(buffer)
						return err
					}
					if s.isClient {
						log.Errorln("[Alert from server] %s", string(buffer))
					}
					pool.Put(buffer)
				}
				return nil
			case cmdUpdatePaddingScheme:
				if hdr.Length() > 0 {
					// `rawScheme` Do not use buffer to prevent subsequent misuse
					rawScheme := make([]byte, int(hdr.Length()))
					if _, err := io.ReadFull(s.conn, rawScheme); err != nil {
						return err
					}
					if s.isClient {
						if padding.UpdatePaddingScheme(rawScheme, s.padding) {
							log.Debugln("[Update padding succeed] %x\n", md5.Sum(rawScheme))
						} else {
							log.Warnln("[Update padding failed] %x\n", md5.Sum(rawScheme))
						}
					}
				}
			case cmdHeartRequest:
				if err := s.discardFramePayload(hdr.Length()); err != nil {
					return err
				}
				if _, err := s.writeControlFrame(newFrame(cmdHeartResponse, sid)); err != nil {
					return err
				}
			case cmdHeartResponse:
				// Active keepalive checking is not implemented yet
				if err := s.discardFramePayload(hdr.Length()); err != nil {
					return err
				}
			case cmdServerSettings:
				if hdr.Length() > 0 {
					buffer := pool.Get(int(hdr.Length()))
					if _, err := io.ReadFull(s.conn, buffer); err != nil {
						pool.Put(buffer)
						return err
					}
					if s.isClient {
						// check server's version
						m := util.StringMapFromBytes(buffer)
						if v, err := strconv.Atoi(m["v"]); err == nil && v >= 0 {
							s.peerVersion.Store(uint32(v))
						}
					}
					pool.Put(buffer)
				}
			default:
				// Preserve framing when an extension or malformed peer sends a
				// payload with a command this implementation does not know.
				if err := s.discardFramePayload(hdr.Length()); err != nil {
					return err
				}
			}
		} else {
			return err
		}
	}
}

func (s *Session) cancelSynDone(sid uint32) {
	s.synDoneLock.Lock()
	if done, ok := s.synDone[sid]; ok {
		done()
		delete(s.synDone, sid)
	}
	s.synDoneLock.Unlock()
}

func (s *Session) cancelAllSynDone() {
	s.synDoneLock.Lock()
	done := s.synDone
	s.synDone = nil
	s.synDoneLock.Unlock()

	for _, cancel := range done {
		cancel()
	}
}

func (s *Session) discardFramePayload(length uint16) error {
	if length == 0 {
		return nil
	}

	buffer := pool.Get(int(length))
	defer pool.Put(buffer)
	_, err := io.ReadFull(s.conn, buffer)
	return err
}

func (s *Session) streamClosed(sid uint32) error {
	if s.IsClosed() {
		return io.ErrClosedPipe
	}
	_, err := s.writeControlFrame(newFrame(cmdFIN, sid))
	s.streamLock.Lock()
	delete(s.streams, sid)
	s.streamLock.Unlock()
	return err
}

// maxFrameDataLen is the maximum payload bytes per data frame. The wire
// format encodes payload length as a uint16, so a single frame cannot
// carry more than 65535 bytes. writeDataFrame splits larger writes into
// multiple frames and sends the whole frame sequence in one writeConn call.
// That keeps one Stream.Write contiguous relative to other stream/control
// frame writes.
const maxFrameDataLen = 0xFFFF

func (s *Session) writeDataFrame(sid uint32, data []byte) (int, error) {
	dataLen := len(data)
	if dataLen == 0 {
		return 0, nil
	}
	if dataLen <= maxFrameDataLen {
		buffer := pool.Get(dataLen + headerOverHeadSize)
		buffer[0] = cmdPSH
		binary.BigEndian.PutUint32(buffer[1:5], sid)
		binary.BigEndian.PutUint16(buffer[5:7], uint16(dataLen))
		copy(buffer[headerOverHeadSize:], data)
		_, err := s.writeConn(buffer)
		_ = pool.Put(buffer)
		if err != nil {
			return 0, err
		}
		return dataLen, nil
	}

	frameCount := (dataLen + maxFrameDataLen - 1) / maxFrameDataLen
	buffer := pool.Get(dataLen + frameCount*headerOverHeadSize)
	defer pool.Put(buffer)

	for written, offset := 0, 0; written < dataLen; {
		chunk := dataLen - written
		if chunk > maxFrameDataLen {
			chunk = maxFrameDataLen
		}
		buffer[offset] = cmdPSH
		binary.BigEndian.PutUint32(buffer[offset+1:offset+5], sid)
		binary.BigEndian.PutUint16(buffer[offset+5:offset+7], uint16(chunk))
		copy(buffer[offset+headerOverHeadSize:offset+headerOverHeadSize+chunk], data[written:written+chunk])
		written += chunk
		offset += headerOverHeadSize + chunk
	}

	if _, err := s.writeConn(buffer); err != nil {
		return 0, err
	}
	return dataLen, nil
}

func (s *Session) writeControlFrame(frame frame) (int, error) {
	dataLen := len(frame.data)
	if dataLen > maxFrameDataLen {
		err := fmt.Errorf("AnyTLS control frame payload too large: %d", dataLen)
		_ = s.Close()
		return 0, err
	}

	buffer := pool.Get(dataLen + headerOverHeadSize)
	buffer[0] = frame.cmd
	binary.BigEndian.PutUint32(buffer[1:5], frame.sid)
	binary.BigEndian.PutUint16(buffer[5:7], uint16(dataLen))
	copy(buffer[headerOverHeadSize:], frame.data)

	s.conn.SetWriteDeadline(time.Now().Add(time.Second * 5))

	_, err := s.writeConn(buffer)
	_ = pool.Put(buffer)
	if err != nil {
		s.Close()
		return 0, err
	}

	s.conn.SetWriteDeadline(time.Time{})

	return dataLen, nil
}

func (s *Session) writeConn(b []byte) (n int, err error) {
	s.connLock.Lock()
	defer s.connLock.Unlock()

	if s.buffering {
		s.buffer = append(s.buffer, b...)
		return len(b), nil
	} else if len(s.buffer) > 0 {
		b = append(s.buffer, b...)
		s.buffer = nil
	}

	// calulate & send padding
	if s.sendPadding {
		pkt := s.pktCounter.Add(1)
		paddingF := s.padding.Load()
		if pkt < paddingF.Stop {
			s.pktSizes = paddingF.AppendRecordPayloadSizes(pkt, s.pktSizes[:0])
			for _, l := range s.pktSizes {
				remainPayloadLen := len(b)
				if l == padding.CheckMark {
					if remainPayloadLen == 0 {
						break
					} else {
						continue
					}
				}
				if remainPayloadLen > l { // this packet is all payload
					_, err = s.conn.Write(b[:l])
					if err != nil {
						return 0, err
					}
					n += l
					b = b[l:]
				} else if remainPayloadLen > 0 { // this packet contains padding and the last part of payload
					paddingLen := l - remainPayloadLen - headerOverHeadSize
					if paddingLen > 0 {
						b = s.appendWastePadding(b, paddingLen)
					}
					_, err = s.conn.Write(b)
					if err != nil {
						return 0, err
					}
					n += remainPayloadLen
					b = nil
				} else { // this packet is all padding
					padding := s.wastePadding(l)
					_, err = s.conn.Write(padding)
					if err != nil {
						return 0, err
					}
					b = nil
				}
			}
			// maybe still remain payload to write
			if len(b) == 0 {
				return
			} else {
				n2, err := s.conn.Write(b)
				return n + n2, err
			}
		} else {
			s.sendPadding = false
		}
	}

	return s.conn.Write(b)
}

func (s *Session) growPadBuf(n int) []byte {
	if cap(s.padBuf) < n {
		s.padBuf = make([]byte, n)
		return s.padBuf
	}
	s.padBuf = s.padBuf[:n]
	for i := range s.padBuf {
		s.padBuf[i] = 0
	}
	return s.padBuf
}

func writeWasteHeader(b []byte, payloadLen int) {
	b[0] = cmdWaste
	binary.BigEndian.PutUint32(b[1:5], 0)
	binary.BigEndian.PutUint16(b[5:7], uint16(payloadLen))
}

func (s *Session) wastePadding(payloadLen int) []byte {
	b := s.growPadBuf(headerOverHeadSize + payloadLen)
	writeWasteHeader(b, payloadLen)
	return b
}

func (s *Session) appendWastePadding(payload []byte, paddingLen int) []byte {
	need := len(payload) + headerOverHeadSize + paddingLen
	if cap(payload) >= need {
		off := len(payload)
		payload = payload[:need]
		writeWasteHeader(payload[off:], paddingLen)
		for i := off + headerOverHeadSize; i < need; i++ {
			payload[i] = 0
		}
		return payload
	}
	out := s.growPadBuf(need)
	copy(out, payload)
	writeWasteHeader(out[len(payload):], paddingLen)
	return out
}
