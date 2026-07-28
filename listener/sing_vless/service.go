package sing_vless

// copy and modify from https://github.com/SagerNet/sing-vmess/tree/3c1cf255413250b09a57e4ecdf1def1fa505e3cc/vless

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"sync"
	"sync/atomic"

	"github.com/Miku0139oao/aster-core/common/utils"
	"github.com/Miku0139oao/aster-core/transport/vless"
	"github.com/Miku0139oao/aster-core/transport/vless/vision"

	"github.com/gofrs/uuid/v5"
	"github.com/metacubex/sing-vmess"
	"github.com/metacubex/sing/common/auth"
	"github.com/metacubex/sing/common/buf"
	"github.com/metacubex/sing/common/bufio"
	E "github.com/metacubex/sing/common/exceptions"
	M "github.com/metacubex/sing/common/metadata"
	N "github.com/metacubex/sing/common/network"
	"google.golang.org/protobuf/proto"
)

type Service[T comparable] struct {
	users       atomic.Pointer[userSnapshot[T]]
	usersMu     sync.Mutex
	closed      atomic.Bool
	pending     sync.Map
	connections sync.Map
	handler     Handler
}

type userSnapshot[T comparable] struct {
	byUUID map[[16]byte]userCredential[T]
}

type userCredential[T comparable] struct {
	user T
	flow string
}

type Handler interface {
	N.TCPConnectionHandler
	N.UDPConnectionHandler
	E.Handler
}

func NewService[T comparable](handler Handler) *Service[T] {
	service := &Service[T]{
		handler: handler,
	}
	service.users.Store(&userSnapshot[T]{byUUID: map[[16]byte]userCredential[T]{}})
	return service
}

func (s *Service[T]) UpdateUsers(userList []T, userUUIDList []string, userFlowList []string) error {
	if len(userList) != len(userUUIDList) || len(userList) != len(userFlowList) {
		return errors.New("vless user, UUID, and flow lists must have equal lengths")
	}
	userMap := make(map[[16]byte]userCredential[T], len(userList))
	for i, userName := range userList {
		userID := utils.UUIDMap(userUUIDList[i])
		if _, exists := userMap[userID]; exists {
			return fmt.Errorf("duplicate VLESS UUID: %s", userUUIDList[i])
		}
		userMap[userID] = userCredential[T]{user: userName, flow: userFlowList[i]}
	}
	s.usersMu.Lock()
	if s.closed.Load() {
		s.usersMu.Unlock()
		return net.ErrClosed
	}
	snapshot := &userSnapshot[T]{byUUID: userMap}
	s.users.Store(snapshot)
	pendingConnections := s.removePendingConnectionsLocked()
	invalidConnections := s.removeInvalidConnectionsLocked(snapshot)
	s.usersMu.Unlock()
	closeNetConnections(pendingConnections)
	closeNetConnections(invalidConnections)
	return nil
}

func (s *Service[T]) Close() {
	s.usersMu.Lock()
	s.closed.Store(true)
	snapshot := &userSnapshot[T]{byUUID: map[[16]byte]userCredential[T]{}}
	s.users.Store(snapshot)
	pendingConnections := s.removePendingConnectionsLocked()
	activeConnections := s.removeInvalidConnectionsLocked(snapshot)
	s.usersMu.Unlock()
	closeNetConnections(pendingConnections)
	closeNetConnections(activeConnections)
}

type pendingConnection struct {
	conn net.Conn
}

type activeConnection[T comparable] struct {
	conn       net.Conn
	uuid       [16]byte
	credential userCredential[T]
}

func (s *Service[T]) CloseConnections() {
	s.usersMu.Lock()
	connections := s.removeInvalidConnectionsLocked(&userSnapshot[T]{byUUID: map[[16]byte]userCredential[T]{}})
	s.usersMu.Unlock()
	closeNetConnections(connections)
}

func (s *Service[T]) removePendingConnectionsLocked() (connections []net.Conn) {
	s.pending.Range(func(key, _ any) bool {
		s.pending.Delete(key)
		connections = append(connections, key.(*pendingConnection).conn)
		return true
	})
	return connections
}

func (s *Service[T]) removeInvalidConnectionsLocked(snapshot *userSnapshot[T]) (connections []net.Conn) {
	s.connections.Range(func(key, _ any) bool {
		active := key.(*activeConnection[T])
		credential, valid := snapshot.byUUID[active.uuid]
		if valid && credential == active.credential {
			return true
		}
		s.connections.Delete(key)
		connections = append(connections, active.conn)
		return true
	})
	return connections
}

func closeNetConnections(connections []net.Conn) {
	for _, conn := range connections {
		_ = conn.Close()
	}
}

func (s *Service[T]) trackPendingConnection(conn net.Conn) (*pendingConnection, error) {
	s.usersMu.Lock()
	defer s.usersMu.Unlock()
	if s.closed.Load() {
		return nil, net.ErrClosed
	}
	pending := &pendingConnection{conn: conn}
	s.pending.Store(pending, struct{}{})
	return pending, nil
}

func (s *Service[T]) untrackPendingConnection(pending *pendingConnection) {
	s.pending.Delete(pending)
}

var _ N.TCPConnectionHandler = (*Service[int])(nil)

func (s *Service[T]) NewConnection(ctx context.Context, conn net.Conn, metadata M.Metadata) error {
	pending, err := s.trackPendingConnection(conn)
	if err != nil {
		return err
	}
	defer s.untrackPendingConnection(pending)
	return s.newConnection(ctx, conn, metadata, pending)
}

func (s *Service[T]) newConnection(ctx context.Context, conn net.Conn, metadata M.Metadata, pending *pendingConnection) error {
	var version uint8
	err := binary.Read(conn, binary.BigEndian, &version)
	if err != nil {
		return err
	}
	if version != vless.Version {
		return E.New("unknown version: ", version)
	}

	var requestUUID [16]byte
	_, err = io.ReadFull(conn, requestUUID[:])
	if err != nil {
		return err
	}

	var addonsLen uint8
	err = binary.Read(conn, binary.BigEndian, &addonsLen)
	if err != nil {
		return err
	}

	var addons vless.Addons
	if addonsLen > 0 {
		addonsBytes := make([]byte, addonsLen)
		_, err = io.ReadFull(conn, addonsBytes)
		if err != nil {
			return err
		}

		err = proto.Unmarshal(addonsBytes, &addons)
		if err != nil {
			return err
		}
	}

	var command byte
	err = binary.Read(conn, binary.BigEndian, &command)
	if err != nil {
		return err
	}

	var destination M.Socksaddr
	if command != vless.CommandMux {
		destination, err = vmess.AddressSerializer.ReadAddrPort(conn)
		if err != nil {
			return err
		}
	}

	snapshot := s.users.Load()
	credential, loaded := snapshot.byUUID[requestUUID]
	if !loaded {
		return E.New("unknown UUID: ", uuid.FromBytesOrNil(requestUUID[:]))
	}
	ctx = auth.ContextWithUser(ctx, credential.user)
	metadata.Destination = destination

	userFlow := credential.flow
	requestFlow := addons.Flow
	if requestFlow != userFlow && requestFlow != "" {
		return E.New("flow mismatch: expected ", flowName(userFlow), ", but got ", flowName(requestFlow))
	}

	responseConn := &serverConn{ExtendedConn: bufio.NewExtendedConn(conn)}
	switch requestFlow {
	case vless.XRV:
		conn, err = vision.NewConn(responseConn, conn, requestUUID)
		if err != nil {
			return E.Cause(err, "initialize vision")
		}
	case "":
		conn = responseConn
	default:
		return E.New("unknown flow: ", requestFlow)
	}
	s.usersMu.Lock()
	currentCredential, valid := s.users.Load().byUUID[requestUUID]
	_, pendingActive := s.pending.Load(pending)
	if s.closed.Load() || !pendingActive || !valid || currentCredential != credential {
		s.usersMu.Unlock()
		_ = pending.conn.Close()
		return errors.New("vless credentials changed during authentication")
	}
	s.pending.Delete(pending)
	active := &activeConnection[T]{conn: pending.conn, uuid: requestUUID, credential: credential}
	s.connections.Store(active, struct{}{})
	s.usersMu.Unlock()
	defer s.connections.Delete(active)
	switch command {
	case vless.CommandTCP:
		return s.handler.NewConnection(ctx, conn, metadata)
	case vless.CommandUDP:
		if requestFlow == vless.XRV {
			return E.New(vless.XRV, " flow does not support UDP")
		}
		return s.handler.NewPacketConnection(ctx, &serverPacketConn{ExtendedConn: bufio.NewExtendedConn(conn), destination: destination}, metadata)
	case vless.CommandMux:
		return vmess.HandleMuxConnection(ctx, conn, metadata, s.handler)
	default:
		return E.New("unknown command: ", command)
	}
}

func flowName(value string) string {
	if value == "" {
		return "none"
	}
	return value
}

type serverConn struct {
	N.ExtendedConn
	responseWritten bool
}

func (c *serverConn) Write(b []byte) (n int, err error) {
	if !c.responseWritten {
		buffer := buf.NewSize(2 + len(b))
		buffer.WriteByte(vless.Version)
		buffer.WriteByte(0)
		buffer.Write(b)
		_, err = c.ExtendedConn.Write(buffer.Bytes())
		buffer.Release()
		if err == nil {
			n = len(b)
		}
		c.responseWritten = true
		return
	}
	return c.ExtendedConn.Write(b)
}

func (c *serverConn) WriteBuffer(buffer *buf.Buffer) error {
	if !c.responseWritten {
		header := buffer.ExtendHeader(2)
		header[0] = vless.Version
		header[1] = 0
		c.responseWritten = true
	}
	return c.ExtendedConn.WriteBuffer(buffer)
}

func (c *serverConn) FrontHeadroom() int {
	if c.responseWritten {
		return 0
	}
	return 2
}

func (c *serverConn) ReaderReplaceable() bool {
	return true
}

func (c *serverConn) WriterReplaceable() bool {
	return c.responseWritten
}

func (c *serverConn) NeedAdditionalReadDeadline() bool {
	return true
}

func (c *serverConn) Upstream() any {
	return c.ExtendedConn
}

type serverPacketConn struct {
	N.ExtendedConn
	destination     M.Socksaddr
	readWaitOptions N.ReadWaitOptions
}

func (c *serverPacketConn) InitializeReadWaiter(options N.ReadWaitOptions) (needCopy bool) {
	c.readWaitOptions = options
	return false
}

func (c *serverPacketConn) WaitReadPacket() (buffer *buf.Buffer, destination M.Socksaddr, err error) {
	var packetLen uint16
	err = binary.Read(c.ExtendedConn, binary.BigEndian, &packetLen)
	if err != nil {
		return
	}

	buffer = c.readWaitOptions.NewPacketBuffer()
	_, err = buffer.ReadFullFrom(c.ExtendedConn, int(packetLen))
	if err != nil {
		buffer.Release()
		return
	}
	c.readWaitOptions.PostReturn(buffer)

	destination = c.destination
	return
}

func (c *serverPacketConn) ReadFrom(p []byte) (n int, addr net.Addr, err error) {
	var packetLen uint16
	err = binary.Read(c.ExtendedConn, binary.BigEndian, &packetLen)
	if err != nil {
		return
	}
	if len(p) < int(packetLen) {
		err = io.ErrShortBuffer
		return
	}
	n, err = io.ReadFull(c.ExtendedConn, p[:packetLen])
	if err != nil {
		return
	}
	if c.destination.IsFqdn() {
		addr = c.destination
	} else {
		addr = c.destination.UDPAddr()
	}
	return
}

func (c *serverPacketConn) WriteTo(p []byte, addr net.Addr) (n int, err error) {
	err = binary.Write(c.ExtendedConn, binary.BigEndian, uint16(len(p)))
	if err != nil {
		return
	}
	return c.ExtendedConn.Write(p)
}

func (c *serverPacketConn) ReadPacket(buffer *buf.Buffer) (destination M.Socksaddr, err error) {
	var packetLen uint16
	err = binary.Read(c.ExtendedConn, binary.BigEndian, &packetLen)
	if err != nil {
		return
	}

	_, err = buffer.ReadFullFrom(c.ExtendedConn, int(packetLen))
	if err != nil {
		return
	}

	destination = c.destination
	return
}

func (c *serverPacketConn) WritePacket(buffer *buf.Buffer, destination M.Socksaddr) error {
	packetLen := buffer.Len()
	binary.BigEndian.PutUint16(buffer.ExtendHeader(2), uint16(packetLen))
	return c.ExtendedConn.WriteBuffer(buffer)
}

func (c *serverPacketConn) FrontHeadroom() int {
	return 2
}

func (c *serverPacketConn) NeedAdditionalReadDeadline() bool {
	return true
}

func (c *serverPacketConn) Upstream() any {
	return c.ExtendedConn
}
