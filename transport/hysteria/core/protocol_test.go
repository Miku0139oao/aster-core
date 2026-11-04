package core

import (
	"bytes"
	"encoding/binary"
	"errors"
	"io"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestUDPMessagePackRoundTrip(t *testing.T) {
	want := udpMessage{
		SessionID: 0x10203040,
		Host:      "example.com",
		Port:      5353,
		MsgID:     42,
		FragID:    1,
		FragCount: 2,
		Data:      []byte("payload"),
	}

	packed := want.Pack()
	require.Len(t, packed, want.Size())

	var got udpMessage
	require.NoError(t, got.Unpack(packed))
	require.Equal(t, want, got)
}

func TestWriteClientRequestUDPFlag(t *testing.T) {
	var wire bytes.Buffer
	require.NoError(t, WriteClientRequest(&wire, ClientRequest{UDP: true}))

	request, err := ReadClientRequest(&wire)
	require.NoError(t, err)
	require.True(t, request.UDP)
}

// TestUDPMessageUnpackMalformed pins the error behaviour of the hand-rolled
// parser: short headers must still report io.ErrUnexpectedEOF, and a length
// field that disagrees with the body must still be rejected.
func TestUDPMessageUnpackMalformed(t *testing.T) {
	valid := udpMessage{
		SessionID: 7, Host: "example.com", Port: 53,
		MsgID: 1, FragID: 0, FragCount: 1,
		Data: []byte("payload"),
	}
	packed := valid.Pack()

	tests := []struct {
		name         string
		data         []byte
		wantEOF      bool
		wantPlainEOF bool
		wantErr      bool
	}{
		{name: "empty", data: nil, wantPlainEOF: true},
		{name: "truncated fixed prefix", data: packed[:5], wantEOF: true},
		{name: "truncated in host", data: packed[:8], wantEOF: true},
		{name: "truncated in trailer", data: packed[:len(packed)-5], wantErr: true},
		{name: "length field larger than body", data: append(append([]byte{}, packed...), 0x00), wantErr: true},
		{name: "length field smaller than body", data: packed[:len(packed)-1], wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var got udpMessage
			err := got.Unpack(tt.data)
			if err == nil {
				t.Fatalf("Unpack(%v) = nil error, want failure", tt.data)
			}
			if tt.wantPlainEOF && !errors.Is(err, io.EOF) {
				t.Fatalf("err = %v, want io.EOF", err)
			}
			if tt.wantEOF && !errors.Is(err, io.ErrUnexpectedEOF) {
				t.Fatalf("err = %v, want io.ErrUnexpectedEOF", err)
			}
			if tt.wantErr && errors.Is(err, io.ErrUnexpectedEOF) {
				t.Fatalf("err = %v, want the invalid-length error", err)
			}
		})
	}
}

// TestUDPMessageUnpackBoundsChecksHostLength ensures a host length pointing
// past the end of the datagram cannot read out of bounds. The old parser
// reported io.ReadFull zero-prefix EOF here, and so does this one.
func TestUDPMessageUnpackBoundsChecksHostLength(t *testing.T) {
	// SessionID=0, HostLen=65535, then nothing.
	data := []byte{0, 0, 0, 0, 0xff, 0xff}
	var got udpMessage
	if err := got.Unpack(data); !errors.Is(err, io.EOF) {
		t.Fatalf("err = %v, want io.EOF", err)
	}
}

func BenchmarkUDPMessageUnpack(b *testing.B) {
	message := udpMessage{
		SessionID: 1, Host: "example.com", Port: 443,
		MsgID: 2, FragID: 0, FragCount: 1,
		Data: make([]byte, 1200),
	}
	packed := message.Pack()
	b.ReportAllocs()
	b.SetBytes(int64(len(packed)))
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		var got udpMessage
		if err := got.Unpack(packed); err != nil {
			b.Fatal(err)
		}
	}
}

// unpackReference is the previous bytes.Reader + binary.Read implementation,
// kept only so the differential test can prove the hand-rolled parser did not
// change any accept/reject decision or parsed value.
func unpackReference(m *udpMessage, data []byte) error {
	reader := bytes.NewReader(data)
	err := binary.Read(reader, binary.BigEndian, &m.SessionID)
	if err != nil {
		return err
	}
	var hostLen uint16
	err = binary.Read(reader, binary.BigEndian, &hostLen)
	if err != nil {
		return err
	}
	hostBytes := make([]byte, hostLen)
	_, err = io.ReadFull(reader, hostBytes)
	if err != nil {
		return err
	}
	m.Host = string(hostBytes)
	err = binary.Read(reader, binary.BigEndian, &m.Port)
	if err != nil {
		return err
	}
	err = binary.Read(reader, binary.BigEndian, &m.MsgID)
	if err != nil {
		return err
	}
	err = binary.Read(reader, binary.BigEndian, &m.FragID)
	if err != nil {
		return err
	}
	err = binary.Read(reader, binary.BigEndian, &m.FragCount)
	if err != nil {
		return err
	}
	var dataLen uint16
	err = binary.Read(reader, binary.BigEndian, &dataLen)
	if err != nil {
		return err
	}
	if reader.Len() != int(dataLen) {
		return errors.New("invalid data length")
	}
	m.Data = data[len(data)-reader.Len():]
	return nil
}

// TestUDPMessageUnpackMatchesReference feeds both parsers every byte prefix,
// several single-byte mutations and random bodies, and requires the same
// verdict (and same parsed fields when accepted).
func TestUDPMessageUnpackMatchesReference(t *testing.T) {
	seed := udpMessage{
		SessionID: 0x10203040, Host: "example.com", Port: 443,
		MsgID: 9, FragID: 1, FragCount: 2, Data: []byte("hello world"),
	}
	packed := seed.Pack()

	cases := [][]byte{nil, {}, packed}
	for i := 1; i < len(packed); i++ {
		cases = append(cases, packed[:i])
	}
	for i := 4; i < len(packed); i += 3 {
		mutated := append([]byte{}, packed...)
		mutated[i] ^= 0xff
		cases = append(cases, mutated)
	}
	// Random-ish bodies driven deterministically from the packed bytes.
	rng := uint32(0x12345678)
	for i := 0; i < 200; i++ {
		n := int(rng % 64)
		body := make([]byte, n)
		for j := range body {
			rng = rng*1664525 + 1013904223
			body[j] = byte(rng >> 24)
		}
		cases = append(cases, body)
	}

	for i, data := range cases {
		var got, want udpMessage
		gotErr := got.Unpack(data)
		wantErr := unpackReference(&want, data)

		if (gotErr == nil) != (wantErr == nil) {
			t.Fatalf("case %d (% x): acceptance differs: new=%v ref=%v", i, data, gotErr, wantErr)
		}
		if gotErr != nil {
			if gotErr.Error() != wantErr.Error() {
				t.Fatalf("case %d: error text differs: %q vs %q", i, gotErr, wantErr)
			}
			continue
		}
		gotData := string(got.Data)
		wantData := string(want.Data)
		got.Data, want.Data = nil, nil
		if got.SessionID != want.SessionID || got.Host != want.Host || got.Port != want.Port ||
			got.MsgID != want.MsgID || got.FragID != want.FragID || got.FragCount != want.FragCount ||
			gotData != wantData {
			t.Fatalf("case %d: parsed differs: got=%+v/%q want=%+v/%q", i, got, gotData, want, wantData)
		}
	}
}
