package core

import "bytes"

const (
	maxPendingUDPMessages = 64
	maxUDPMessageSize     = 65535
)

func fragUDPMessage(m udpMessage, maxSize int) []udpMessage {
	if m.Size() <= maxSize {
		return []udpMessage{m}
	}
	fullPayload := m.Data
	maxPayloadSize := maxSize - m.HeaderSize()
	if maxPayloadSize <= 0 {
		return nil
	}
	fragCountValue := (len(fullPayload) + maxPayloadSize - 1) / maxPayloadSize
	if fragCountValue > 255 {
		return nil
	}
	off := 0
	fragID := uint8(0)
	fragCount := uint8(fragCountValue)
	var frags []udpMessage
	for off < len(fullPayload) {
		payloadSize := len(fullPayload) - off
		if payloadSize > maxPayloadSize {
			payloadSize = maxPayloadSize
		}
		frag := m
		frag.FragID = fragID
		frag.FragCount = fragCount
		frag.Data = fullPayload[off : off+payloadSize]
		frags = append(frags, frag)
		off += payloadSize
		fragID++
	}
	return frags
}

type defragKey struct {
	sessionID uint32
	msgID     uint16
}

type fragmentedUDPMessage struct {
	host      string
	port      uint16
	fragCount uint8
	frags     [][]byte
	received  int
	size      int
}

type defragger struct {
	pending map[defragKey]*fragmentedUDPMessage
}

func (d *defragger) Feed(m udpMessage) *udpMessage {
	if m.FragCount == 1 {
		return &m
	}
	if m.FragCount == 0 || m.MsgID == 0 || m.FragID >= m.FragCount || len(m.Data) > maxUDPMessageSize {
		return nil
	}
	if d.pending == nil {
		d.pending = make(map[defragKey]*fragmentedUDPMessage)
	}

	key := defragKey{sessionID: m.SessionID, msgID: m.MsgID}
	pending := d.pending[key]
	if pending == nil {
		if len(d.pending) >= maxPendingUDPMessages {
			for evicted := range d.pending {
				delete(d.pending, evicted)
				break
			}
		}
		pending = &fragmentedUDPMessage{
			host:      m.Host,
			port:      m.Port,
			fragCount: m.FragCount,
			frags:     make([][]byte, m.FragCount),
		}
		d.pending[key] = pending
	} else if pending.fragCount != m.FragCount || pending.host != m.Host || pending.port != m.Port {
		delete(d.pending, key)
		return nil
	}

	if previous := pending.frags[m.FragID]; previous != nil {
		if !bytes.Equal(previous, m.Data) {
			delete(d.pending, key)
		}
		return nil
	}
	if pending.size > maxUDPMessageSize-len(m.Data) {
		delete(d.pending, key)
		return nil
	}
	pending.frags[m.FragID] = m.Data
	pending.received++
	pending.size += len(m.Data)
	if pending.received != len(pending.frags) {
		return nil
	}

	data := make([]byte, 0, pending.size)
	for _, fragment := range pending.frags {
		data = append(data, fragment...)
	}
	delete(d.pending, key)
	m.Data = data
	m.FragID = 0
	m.FragCount = 1
	return &m
}
