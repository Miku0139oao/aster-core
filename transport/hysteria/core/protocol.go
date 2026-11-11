package core

import (
	"encoding/binary"
	"errors"
	"io"
	"time"
)

const (
	protocolVersion = uint8(3)
	protocolTimeout = 10 * time.Second

	closeErrorCodeGeneric  = 0
	closeErrorCodeProtocol = 1
	closeErrorCodeAuth     = 2
)

type ClientHello struct {
	SendBPS uint64
	RecvBPS uint64
	Auth    []byte
}

func WriteClientHello(stream io.Writer, hello ClientHello) error {
	var requestLen int
	requestLen += 1 // version
	requestLen += 8 // sendBPS
	requestLen += 8 // recvBPS
	requestLen += 2 // auth len
	requestLen += len(hello.Auth)
	request := make([]byte, requestLen)
	request[0] = protocolVersion
	binary.BigEndian.PutUint64(request[1:9], hello.SendBPS)
	binary.BigEndian.PutUint64(request[9:17], hello.RecvBPS)
	binary.BigEndian.PutUint16(request[17:19], uint16(len(hello.Auth)))
	copy(request[19:], hello.Auth)
	_, err := stream.Write(request)
	return err
}

func ReadClientHello(stream io.Reader) (*ClientHello, error) {
	var responseLen int
	responseLen += 1 // ok
	responseLen += 8 // sendBPS
	responseLen += 8 // recvBPS
	responseLen += 2 // auth len
	response := make([]byte, responseLen)
	_, err := io.ReadFull(stream, response)
	if err != nil {
		return nil, err
	}

	if response[0] != protocolVersion {
		return nil, errors.New("unsupported client version")
	}
	var clientHello ClientHello
	clientHello.SendBPS = binary.BigEndian.Uint64(response[1:9])
	clientHello.RecvBPS = binary.BigEndian.Uint64(response[9:17])
	authLen := binary.BigEndian.Uint16(response[17:19])

	if clientHello.SendBPS == 0 || clientHello.RecvBPS == 0 {
		return nil, errors.New("invalid rate from client")
	}

	authBytes := make([]byte, authLen)
	_, err = io.ReadFull(stream, authBytes)
	if err != nil {
		return nil, err
	}
	clientHello.Auth = authBytes
	return &clientHello, nil
}

type ServerHello struct {
	OK      bool
	SendBPS uint64
	RecvBPS uint64
	Message string
}

func ReadServerHello(stream io.Reader) (*ServerHello, error) {
	var responseLen int
	responseLen += 1 // ok
	responseLen += 8 // sendBPS
	responseLen += 8 // recvBPS
	responseLen += 2 // message len
	response := make([]byte, responseLen)
	_, err := io.ReadFull(stream, response)
	if err != nil {
		return nil, err
	}
	var serverHello ServerHello
	serverHello.OK = response[0] == 1
	serverHello.SendBPS = binary.BigEndian.Uint64(response[1:9])
	serverHello.RecvBPS = binary.BigEndian.Uint64(response[9:17])
	messageLen := binary.BigEndian.Uint16(response[17:19])
	if messageLen == 0 {
		return &serverHello, nil
	}
	message := make([]byte, messageLen)
	_, err = io.ReadFull(stream, message)
	if err != nil {
		return nil, err
	}
	serverHello.Message = string(message)
	return &serverHello, nil
}

func WriteServerHello(stream io.Writer, hello ServerHello) error {
	var responseLen int
	responseLen += 1 // ok
	responseLen += 8 // sendBPS
	responseLen += 8 // recvBPS
	responseLen += 2 // message len
	responseLen += len(hello.Message)
	response := make([]byte, responseLen)
	if hello.OK {
		response[0] = 1
	} else {
		response[0] = 0
	}
	binary.BigEndian.PutUint64(response[1:9], hello.SendBPS)
	binary.BigEndian.PutUint64(response[9:17], hello.RecvBPS)
	binary.BigEndian.PutUint16(response[17:19], uint16(len(hello.Message)))
	copy(response[19:], hello.Message)
	_, err := stream.Write(response)
	return err
}

type ClientRequest struct {
	UDP  bool
	Host string
	Port uint16
}

func ReadClientRequest(stream io.Reader) (*ClientRequest, error) {
	var clientRequest ClientRequest
	err := binary.Read(stream, binary.BigEndian, &clientRequest.UDP)
	if err != nil {
		return nil, err
	}
	var hostLen uint16
	err = binary.Read(stream, binary.BigEndian, &hostLen)
	if err != nil {
		return nil, err
	}
	host := make([]byte, hostLen)
	_, err = io.ReadFull(stream, host)
	if err != nil {
		return nil, err
	}
	clientRequest.Host = string(host)
	err = binary.Read(stream, binary.BigEndian, &clientRequest.Port)
	if err != nil {
		return nil, err
	}
	return &clientRequest, nil
}

func WriteClientRequest(stream io.Writer, request ClientRequest) error {
	var requestLen int
	requestLen += 1 // udp
	requestLen += 2 // host len
	requestLen += len(request.Host)
	requestLen += 2 // port
	buffer := make([]byte, requestLen)
	if request.UDP {
		buffer[0] = 1
	} else {
		buffer[0] = 0
	}
	binary.BigEndian.PutUint16(buffer[1:3], uint16(len(request.Host)))
	n := copy(buffer[3:], request.Host)
	binary.BigEndian.PutUint16(buffer[3+n:3+n+2], request.Port)
	_, err := stream.Write(buffer)
	return err
}

type ServerResponse struct {
	OK           bool
	UDPSessionID uint32
	Message      string
}

func ReadServerResponse(stream io.Reader) (*ServerResponse, error) {
	var responseLen int
	responseLen += 1 // ok
	responseLen += 4 // udp session id
	responseLen += 2 // message len
	response := make([]byte, responseLen)
	_, err := io.ReadFull(stream, response)
	if err != nil {
		return nil, err
	}
	var serverResponse ServerResponse
	serverResponse.OK = response[0] == 1
	serverResponse.UDPSessionID = binary.BigEndian.Uint32(response[1:5])
	messageLen := binary.BigEndian.Uint16(response[5:7])
	if messageLen == 0 {
		return &serverResponse, nil
	}
	message := make([]byte, messageLen)
	_, err = io.ReadFull(stream, message)
	if err != nil {
		return nil, err
	}
	serverResponse.Message = string(message)
	return &serverResponse, nil
}

func WriteServerResponse(stream io.Writer, response ServerResponse) error {
	var responseLen int
	responseLen += 1 // ok
	responseLen += 4 // udp session id
	responseLen += 2 // message len
	responseLen += len(response.Message)
	buffer := make([]byte, responseLen)
	if response.OK {
		buffer[0] = 1
	} else {
		buffer[0] = 0
	}
	binary.BigEndian.PutUint32(buffer[1:5], response.UDPSessionID)
	binary.BigEndian.PutUint16(buffer[5:7], uint16(len(response.Message)))
	copy(buffer[7:], response.Message)
	_, err := stream.Write(buffer)
	return err
}

type udpMessage struct {
	SessionID uint32
	Host      string
	Port      uint16
	MsgID     uint16 // doesn't matter when not fragmented, but must not be 0 when fragmented
	FragID    uint8  // doesn't matter when not fragmented, starts at 0 when fragmented
	FragCount uint8  // must be 1 when not fragmented
	Data      []byte
}

func (m udpMessage) HeaderSize() int {
	return 4 + 2 + len(m.Host) + 2 + 2 + 1 + 1 + 2
}

func (m udpMessage) Size() int {
	return m.HeaderSize() + len(m.Data)
}

func (m udpMessage) Pack() []byte {
	data := make([]byte, m.Size())
	binary.BigEndian.PutUint32(data[0:4], m.SessionID)
	binary.BigEndian.PutUint16(data[4:6], uint16(len(m.Host)))
	offset := 6 + copy(data[6:], m.Host)
	binary.BigEndian.PutUint16(data[offset:offset+2], m.Port)
	offset += 2
	binary.BigEndian.PutUint16(data[offset:offset+2], m.MsgID)
	offset += 2
	data[offset] = m.FragID
	offset++
	data[offset] = m.FragCount
	offset++
	binary.BigEndian.PutUint16(data[offset:offset+2], uint16(len(m.Data)))
	copy(data[offset+2:], m.Data)
	return data
}

// Unpack parses a datagram in place. It used to run bytes.Reader plus nine
// binary.Read calls, each of which allocates a temporary for the reflected
// value; that is the per-datagram cost on the hysteria v1 UDP receive path.
//
// The wire layout is unchanged, and so is the error behaviour, down to the
// exact EOF reported for each truncation point (binary.Read reports io.EOF
// when its field starts at end-of-input but ErrUnexpectedEOF when only part of
// the field is present; io.ReadFull reports io.EOF when the length is zero and
// ErrUnexpectedEOF otherwise). TestUDPMessageUnpackMatchesReference differentials
// every truncation and mutation against the previous implementation, so this
// equivalence is enforced rather than asserted.
func (m *udpMessage) Unpack(data []byte) error {
	if len(data) < 4 {
		return readFieldEOF(len(data), 4)
	}
	m.SessionID = binary.BigEndian.Uint32(data[0:4])
	if len(data) < 6 {
		return readFieldEOF(len(data)-4, 2)
	}
	hostLen := int(binary.BigEndian.Uint16(data[4:6]))
	offset := 6
	if remaining := len(data) - offset; remaining < hostLen {
		// io.ReadFull over a zero-length host returns nil, matching this branch.
		return readFieldEOF(remaining, hostLen)
	}
	// Converting a sub-slice of the input is a copy, exactly as string(hostBytes)
	// was before; the bytes do not alias the caller's buffer.
	m.Host = string(data[offset : offset+hostLen])
	offset += hostLen
	if len(data)-offset < 2 {
		return readFieldEOF(len(data)-offset, 2)
	}
	m.Port = binary.BigEndian.Uint16(data[offset : offset+2])
	offset += 2
	if len(data)-offset < 2 {
		return readFieldEOF(len(data)-offset, 2)
	}
	m.MsgID = binary.BigEndian.Uint16(data[offset : offset+2])
	offset += 2
	if len(data)-offset < 1 {
		return readFieldEOF(0, 1)
	}
	m.FragID = data[offset]
	offset++
	if len(data)-offset < 1 {
		return readFieldEOF(0, 1)
	}
	m.FragCount = data[offset]
	offset++
	if len(data)-offset < 2 {
		return readFieldEOF(len(data)-offset, 2)
	}
	dataLen := int(binary.BigEndian.Uint16(data[offset : offset+2]))
	offset += 2
	if len(data)-offset != dataLen {
		return errors.New("invalid data length")
	}
	m.Data = data[offset:]
	return nil
}

// readFieldEOF reproduces the error binary.Read / io.ReadFull return when a
// field of want bytes starts with have bytes left: io.EOF when nothing of the
// field is present, io.ErrUnexpectedEOF when it is partially present.
func readFieldEOF(have, want int) error {
	if have >= want {
		return nil
	}
	if have == 0 {
		return io.EOF
	}
	return io.ErrUnexpectedEOF
}
