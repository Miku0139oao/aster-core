package v5

import (
	"bytes"
	"net/netip"
	"testing"
)

func TestPacketAndDissociateBytesLenMatchWireEncoding(t *testing.T) {
	packet := NewPacket(
		1,
		2,
		1,
		0,
		3,
		NewAddressAddrPort(netip.MustParseAddrPort("[2001:db8::1]:443")),
		[]byte{1, 2, 3},
	)
	var packetWire bytes.Buffer
	if err := packet.WriteTo(&packetWire); err != nil {
		t.Fatal(err)
	}
	if packetWire.Len() != packet.BytesLen() {
		t.Fatalf("packet wire length=%d, BytesLen=%d", packetWire.Len(), packet.BytesLen())
	}

	dissociate := NewDissociate(9)
	var dissociateWire bytes.Buffer
	if err := dissociate.WriteTo(&dissociateWire); err != nil {
		t.Fatal(err)
	}
	if dissociateWire.Len() != dissociate.BytesLen() {
		t.Fatalf("dissociate wire length=%d, BytesLen=%d", dissociateWire.Len(), dissociate.BytesLen())
	}
}

func TestFragWriteNativeRejectsInvalidFragmentGeometry(t *testing.T) {
	packet := NewPacket(1, 2, 1, 0, 1, Address{TYPE: AtypNone}, []byte{1})
	if err := fragWriteNative(nil, packet, new(bytes.Buffer), 0); err == nil {
		t.Fatal("expected zero fragment size to fail")
	}
	if err := fragWriteNative(nil, packet, new(bytes.Buffer), -1); err == nil {
		t.Fatal("expected negative fragment size to fail")
	}

	packet.DATA = make([]byte, 256)
	if err := fragWriteNative(nil, packet, new(bytes.Buffer), 1); err == nil {
		t.Fatal("expected more than 255 fragments to fail")
	}
}
