package core

import (
	"reflect"
	"testing"

	"github.com/stretchr/testify/require"
)

func Test_fragUDPMessage(t *testing.T) {
	type args struct {
		m       udpMessage
		maxSize int
	}
	tests := []struct {
		name string
		args args
		want []udpMessage
	}{
		{
			"no frag",
			args{
				udpMessage{
					SessionID: 123,
					Host:      "test",
					Port:      123,
					MsgID:     123,
					FragID:    0,
					FragCount: 1,
					Data:      []byte("hello"),
				},
				100,
			},
			[]udpMessage{
				udpMessage{
					SessionID: 123,
					Host:      "test",
					Port:      123,
					MsgID:     123,
					FragID:    0,
					FragCount: 1,
					Data:      []byte("hello"),
				},
			},
		},
		{
			"2 frags",
			args{
				udpMessage{
					SessionID: 123,
					Host:      "test",
					Port:      123,
					MsgID:     123,
					FragID:    0,
					FragCount: 1,
					Data:      []byte("hello"),
				},
				22,
			},
			[]udpMessage{
				udpMessage{
					SessionID: 123,
					Host:      "test",
					Port:      123,
					MsgID:     123,
					FragID:    0,
					FragCount: 2,
					Data:      []byte("hell"),
				},
				udpMessage{
					SessionID: 123,
					Host:      "test",
					Port:      123,
					MsgID:     123,
					FragID:    1,
					FragCount: 2,
					Data:      []byte("o"),
				},
			},
		},
		{
			"4 frags",
			args{
				udpMessage{
					SessionID: 123,
					Host:      "test",
					Port:      123,
					MsgID:     123,
					FragID:    0,
					FragCount: 1,
					Data:      []byte("wow wow wow lol lmao"),
				},
				23,
			},
			[]udpMessage{
				udpMessage{
					SessionID: 123,
					Host:      "test",
					Port:      123,
					MsgID:     123,
					FragID:    0,
					FragCount: 4,
					Data:      []byte("wow w"),
				},
				udpMessage{
					SessionID: 123,
					Host:      "test",
					Port:      123,
					MsgID:     123,
					FragID:    1,
					FragCount: 4,
					Data:      []byte("ow wo"),
				},
				udpMessage{
					SessionID: 123,
					Host:      "test",
					Port:      123,
					MsgID:     123,
					FragID:    2,
					FragCount: 4,
					Data:      []byte("w lol"),
				},
				udpMessage{
					SessionID: 123,
					Host:      "test",
					Port:      123,
					MsgID:     123,
					FragID:    3,
					FragCount: 4,
					Data:      []byte(" lmao"),
				},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := fragUDPMessage(tt.args.m, tt.args.maxSize); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("fragUDPMessage() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestFragUDPMessageRejectsUnrepresentableFragments(t *testing.T) {
	message := udpMessage{Host: "example.com", FragCount: 1, Data: make([]byte, 256)}
	require.Nil(t, fragUDPMessage(message, message.HeaderSize()))
	require.Nil(t, fragUDPMessage(message, message.HeaderSize()+1))
}

func Test_defragger_Feed(t *testing.T) {
	d := &defragger{}
	type args struct {
		m udpMessage
	}
	tests := []struct {
		name string
		args args
		want *udpMessage
	}{
		{
			"no frag",
			args{
				udpMessage{
					SessionID: 123,
					Host:      "test",
					Port:      123,
					MsgID:     123,
					FragID:    0,
					FragCount: 1,
					Data:      []byte("hello"),
				},
			},
			&udpMessage{
				SessionID: 123,
				Host:      "test",
				Port:      123,
				MsgID:     123,
				FragID:    0,
				FragCount: 1,
				Data:      []byte("hello"),
			},
		},
		{
			"frag 1 - 1/3",
			args{
				udpMessage{
					SessionID: 123,
					Host:      "test",
					Port:      123,
					MsgID:     666,
					FragID:    0,
					FragCount: 3,
					Data:      []byte("hello"),
				},
			},
			nil,
		},
		{
			"frag 1 - 2/3",
			args{
				udpMessage{
					SessionID: 123,
					Host:      "test",
					Port:      123,
					MsgID:     666,
					FragID:    1,
					FragCount: 3,
					Data:      []byte(" shitty "),
				},
			},
			nil,
		},
		{
			"frag 1 - 3/3",
			args{
				udpMessage{
					SessionID: 123,
					Host:      "test",
					Port:      123,
					MsgID:     666,
					FragID:    2,
					FragCount: 3,
					Data:      []byte("world!!"),
				},
			},
			&udpMessage{
				SessionID: 123,
				Host:      "test",
				Port:      123,
				MsgID:     666,
				FragID:    0,
				FragCount: 1,
				Data:      []byte("hello shitty world!!"),
			},
		},
		{
			"frag 2 - 1/2",
			args{
				udpMessage{
					SessionID: 123,
					Host:      "test",
					Port:      123,
					MsgID:     777,
					FragID:    0,
					FragCount: 2,
					Data:      []byte("hello"),
				},
			},
			nil,
		},
		{
			"frag 3 - 2/2",
			args{
				udpMessage{
					SessionID: 123,
					Host:      "test",
					Port:      123,
					MsgID:     778,
					FragID:    1,
					FragCount: 2,
					Data:      []byte(" moto"),
				},
			},
			nil,
		},
		{
			"frag 2 - 2/2",
			args{
				udpMessage{
					SessionID: 123,
					Host:      "test",
					Port:      123,
					MsgID:     777,
					FragID:    1,
					FragCount: 2,
					Data:      []byte(" moto"),
				},
			},
			&udpMessage{
				SessionID: 123,
				Host:      "test",
				Port:      123,
				MsgID:     777,
				FragID:    0,
				FragCount: 1,
				Data:      []byte("hello moto"),
			},
		},
		{
			"frag 2 - 1/2 re",
			args{
				udpMessage{
					SessionID: 123,
					Host:      "test",
					Port:      123,
					MsgID:     777,
					FragID:    0,
					FragCount: 2,
					Data:      []byte("hello"),
				},
			},
			nil,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := d.Feed(tt.args.m); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("Feed() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestDefraggerHandlesInterleavedSessions(t *testing.T) {
	d := new(defragger)
	first := udpMessage{SessionID: 1, Host: "first", Port: 1, MsgID: 7, FragCount: 2}
	second := udpMessage{SessionID: 2, Host: "second", Port: 2, MsgID: 7, FragCount: 2}

	first.Data = []byte("first-")
	require.Nil(t, d.Feed(first))
	second.Data = []byte("second-")
	require.Nil(t, d.Feed(second))

	first.FragID = 1
	first.Data = []byte("message")
	require.Equal(t, []byte("first-message"), d.Feed(first).Data)
	second.FragID = 1
	second.Data = []byte("message")
	require.Equal(t, []byte("second-message"), d.Feed(second).Data)
}

func TestDefraggerRejectsConflictingFragments(t *testing.T) {
	d := new(defragger)
	require.Nil(t, d.Feed(udpMessage{SessionID: 1, MsgID: 7, FragCount: 2, Data: []byte("first")}))
	require.NotPanics(t, func() {
		require.Nil(t, d.Feed(udpMessage{SessionID: 1, MsgID: 7, FragID: 2, FragCount: 3, Data: []byte("last")}))
	})
	require.Nil(t, d.Feed(udpMessage{SessionID: 1, MsgID: 0, FragCount: 2, Data: []byte("invalid")}))
}
