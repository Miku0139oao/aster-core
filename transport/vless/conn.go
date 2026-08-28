package vless

import (
	"encoding/binary"
	"errors"
	"io"
	"net"

	"github.com/Miku0139oao/aster-core/common/buf"
	N "github.com/Miku0139oao/aster-core/common/net"
	"github.com/Miku0139oao/aster-core/transport/vless/vision"

	"github.com/gofrs/uuid/v5"
)

type Conn struct {
	N.ExtendedConn
	dst      *DstAddr
	id       uuid.UUID
	addons   *Addons
	received bool
	sent     bool
}

func (vc *Conn) Read(b []byte) (int, error) {
	if !vc.received {
		if err := vc.recvResponse(); err != nil {
			return 0, err
		}
		vc.received = true
	}
	return vc.ExtendedConn.Read(b)
}

func (vc *Conn) ReadBuffer(buffer *buf.Buffer) error {
	if !vc.received {
		if err := vc.recvResponse(); err != nil {
			return err
		}
		vc.received = true
	}
	return vc.ExtendedConn.ReadBuffer(buffer)
}

func (vc *Conn) Write(p []byte) (int, error) {
	if !vc.sent {
		if err := vc.sendRequest(p); err != nil {
			return 0, err
		}
		vc.sent = true
		return len(p), nil
	}

	return vc.ExtendedConn.Write(p)
}

func (vc *Conn) WriteBuffer(buffer *buf.Buffer) error {
	if !vc.sent {
		// WriteBuffer transfers ownership to the receiver even on the first
		// request, where sendRequest copies the payload into its own frame.
		defer buffer.Release()
		if err := vc.sendRequest(buffer.Bytes()); err != nil {
			return err
		}
		vc.sent = true
		return nil
	}

	return vc.ExtendedConn.WriteBuffer(buffer)
}

func (vc *Conn) sendRequest(p []byte) (err error) {
	var addonsStorage [128]byte
	addonsBytes := addonsStorage[:0]
	if vc.addons != nil {
		addonsBytes = appendAddons(addonsBytes, vc.addons)
	}
	if len(addonsBytes) > 255 {
		return errors.New("vless addons exceed maximum length")
	}

	requestLen := 1  // protocol version
	requestLen += 16 // UUID
	requestLen += 1  // addons length
	requestLen += len(addonsBytes)
	requestLen += 1 // command
	if !vc.dst.Mux {
		requestLen += 2 // port
		requestLen += 1 // addr type
		requestLen += len(vc.dst.Addr)
	}
	requestLen += len(p)

	buffer := buf.NewSize(requestLen)
	defer buffer.Release()

	if err = writeRequestBytes(buffer, Version); err != nil {
		return err
	}
	if err = writeRequestSlice(buffer, vc.id[:]); err != nil {
		return err
	}
	if err = writeRequestBytes(buffer, byte(len(addonsBytes))); err != nil {
		return err
	}
	if err = writeRequestSlice(buffer, addonsBytes); err != nil {
		return err
	}

	if vc.dst.Mux {
		if err = writeRequestBytes(buffer, CommandMux); err != nil {
			return err
		}
	} else {
		command := CommandTCP
		if vc.dst.UDP {
			command = CommandUDP
		}
		if err = writeRequestBytes(buffer, command); err != nil {
			return err
		}

		binary.BigEndian.PutUint16(buffer.Extend(2), vc.dst.Port)
		if err = writeRequestBytes(buffer, vc.dst.AddrType); err != nil {
			return err
		}
		if err = writeRequestSlice(buffer, vc.dst.Addr); err != nil {
			return err
		}
	}

	if err = writeRequestSlice(buffer, p); err != nil {
		return err
	}

	_, err = vc.ExtendedConn.Write(buffer.Bytes())
	return
}

func writeRequestBytes(buffer *buf.Buffer, value byte) error {
	return buffer.WriteByte(value)
}

func writeRequestSlice(buffer *buf.Buffer, data []byte) error {
	n, err := buffer.Write(data)
	if err != nil {
		return err
	}
	if n != len(data) {
		return io.ErrShortBuffer
	}
	return nil
}

func (vc *Conn) recvResponse() (err error) {
	var buffer [2]byte
	_, err = io.ReadFull(vc.ExtendedConn, buffer[:])
	if err != nil {
		return err
	}

	if buffer[0] != Version {
		return errors.New("unexpected response version")
	}

	length := int64(buffer[1])
	if length != 0 { // addon data length > 0
		if _, err = io.CopyN(io.Discard, vc.ExtendedConn, length); err != nil {
			return err
		}
	}

	return nil
}

func (vc *Conn) Upstream() any {
	return vc.ExtendedConn
}

func (vc *Conn) ReaderReplaceable() bool {
	return vc.received
}

func (vc *Conn) WriterReplaceable() bool {
	return vc.sent
}

func (vc *Conn) NeedHandshake() bool {
	return !vc.sent
}

// newConn return a Conn instance
func newConn(conn net.Conn, client *Client, dst *DstAddr) (net.Conn, error) {
	c := &Conn{
		ExtendedConn: N.NewExtendedConn(conn),
		id:           client.uuid,
		addons:       client.Addons,
		dst:          dst,
	}

	if client.Addons != nil {
		switch client.Addons.Flow {
		case XRV:
			visionConn, err := vision.NewConn(c, conn, c.id)
			if err != nil {
				return nil, err
			}
			return visionConn, nil
		}
	}

	return c, nil
}
