package openvpn

import (
	"bytes"
	"context"
	"errors"
	"testing"
	"time"
)

// TestDataChannelKeyIDFollowsControlChannel pins the data-channel key epoch
// against the control channel. OpenVPN labels every data packet with the key
// epoch its keys were derived under; after a soft reset the control channel
// advances to the next epoch, so a data channel that keeps hard-coding 0 is
// silently dropped by the server once it rekeys.
func TestDataChannelKeyIDFollowsControlChannel(t *testing.T) {
	clientIO, serverIO := newMemoryPacketPair()
	clientCrypt, err := NewTLSCrypt(testStaticKey(), true)
	if err != nil {
		t.Fatal(err)
	}
	var clientID SessionID
	copy(clientID[:], []byte("client01"))
	var serverID SessionID
	copy(serverID[:], []byte("server01"))

	control := NewControlChannel(clientIO, clientCrypt, clientID)
	control.SetRemoteSessionID(serverID)
	control.clock = func() time.Time { return time.Unix(1714567890, 0) }

	if got := control.KeyID(); got != 0 {
		t.Fatalf("initial control key id = %d, want 0", got)
	}

	keys := &KeyMaterial{
		SendCipherKey: bytes.Repeat([]byte{0x11}, 32),
		SendHMACKey:   bytes.Repeat([]byte{0x22}, maxHMACKeyLength),
		RecvCipherKey: bytes.Repeat([]byte{0x33}, 32),
		RecvHMACKey:   bytes.Repeat([]byte{0x44}, maxHMACKeyLength),
	}

	// Initial epoch: the data channel must be labeled with key id 0.
	first, err := NewDataChannel(keys, CipherAES256GCM, AuthSHA1, 7, control.KeyID())
	if err != nil {
		t.Fatal(err)
	}
	enc, err := first.Encrypt(v1TestIPPacket)
	if err != nil {
		t.Fatal(err)
	}
	if opcode, keyID := parseOpcodeKeyID(enc[0]); opcode != PDataV2 || keyID != 0 {
		t.Fatalf("initial data header = %s/%d, want P_DATA_V2/0", opcode, keyID)
	}

	// Simulate the rekey: the control channel toggles to the next epoch.
	if err := control.SendSoftReset(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := control.KeyID(); got != 1 {
		t.Fatalf("control key id after soft reset = %d, want 1", got)
	}

	// The rebuilt data channel must carry the new epoch in its header, both for
	// P_DATA_V2 (opcode|peer-id) and P_DATA_V1 (opcode only).
	second, err := NewDataChannel(keys, CipherAES256GCM, AuthSHA1, 7, control.KeyID())
	if err != nil {
		t.Fatal(err)
	}
	if second.header[0] != opcodeKeyID(PDataV2, 1) {
		t.Fatalf("P_DATA_V2 header first byte = %#02x, want %#02x", second.header[0], opcodeKeyID(PDataV2, 1))
	}
	enc, err = second.Encrypt(v1TestIPPacket)
	if err != nil {
		t.Fatal(err)
	}
	if opcode, keyID := parseOpcodeKeyID(enc[0]); opcode != PDataV2 || keyID != 1 {
		t.Fatalf("post-rekey data header = %s/%d, want P_DATA_V2/1", opcode, keyID)
	}

	v1, err := NewDataChannel(keys, CipherAES256GCM, AuthSHA1, PeerIDUnset, control.KeyID())
	if err != nil {
		t.Fatal(err)
	}
	if want := opcodeKeyID(PDataV1, 1); v1.header[0] != want || len(v1.header) != 1 {
		t.Fatalf("post-rekey P_DATA_V1 header = %x, want [%#02x]", v1.header, want)
	}

	// The key id is masked to the 3 bits the wire format carries.
	masked, err := NewDataChannel(keys, CipherAES256GCM, AuthSHA1, PeerIDUnset, 0xFF)
	if err != nil {
		t.Fatal(err)
	}
	if _, keyID := parseOpcodeKeyID(masked.header[0]); keyID != KeyIDMask {
		t.Fatalf("key id %d not masked to KeyIDMask", keyID)
	}

	// Drain the packets the control channel wrote so the channel buffers do not
	// leak between runs of this test.
	for {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
		_, err := serverIO.ReadPacket(ctx)
		cancel()
		if err != nil {
			break
		}
	}
}

// TestDataChannelPacketIDExhaustionGate makes sure the 32-bit send counter can
// never wrap: OpenVPN starts a rekey at 0xFF000000 and a wrapped counter would
// reuse GCM nonces.
func TestDataChannelPacketIDExhaustionGate(t *testing.T) {
	keys := &KeyMaterial{
		SendCipherKey: bytes.Repeat([]byte{0x11}, 32),
		SendHMACKey:   bytes.Repeat([]byte{0x22}, maxHMACKeyLength),
		RecvCipherKey: bytes.Repeat([]byte{0x33}, 32),
		RecvHMACKey:   bytes.Repeat([]byte{0x44}, maxHMACKeyLength),
	}
	channel, err := NewDataChannel(keys, CipherAES256GCM, AuthSHA1, PeerIDUnset, 0)
	if err != nil {
		t.Fatal(err)
	}

	// One below the threshold still encrypts; the threshold itself does not.
	channel.sendPacketID = dataPacketIDRekeyThreshold - 1
	if _, err := channel.Encrypt(v1TestIPPacket); err != nil {
		t.Fatalf("packet id below threshold must still encrypt: %v", err)
	}
	if _, err := channel.Encrypt(v1TestIPPacket); !errors.Is(err, errDataPacketIDExhausted) {
		t.Fatalf("expected errDataPacketIDExhausted at threshold, got %v", err)
	}

	// The gate also covers CBC channels, which have no nonce to reuse but must
	// still not wrap the on-wire packet id.
	cbcKeys := &KeyMaterial{
		SendCipherKey: bytes.Repeat([]byte{0x11}, 16),
		SendHMACKey:   bytes.Repeat([]byte{0x22}, maxHMACKeyLength),
		RecvCipherKey: bytes.Repeat([]byte{0x33}, 16),
		RecvHMACKey:   bytes.Repeat([]byte{0x44}, maxHMACKeyLength),
	}
	cbc, err := NewDataChannel(cbcKeys, CipherAES128CBC, AuthSHA1, PeerIDUnset, 0)
	if err != nil {
		t.Fatal(err)
	}
	cbc.sendPacketID = dataPacketIDRekeyThreshold
	if _, err := cbc.Encrypt(v1TestIPPacket); !errors.Is(err, errDataPacketIDExhausted) {
		t.Fatalf("expected errDataPacketIDExhausted from CBC channel, got %v", err)
	}
}
