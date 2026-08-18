package outbound

import "testing"

func TestHysteriaQUICConfigUsesIndependentReceiveWindowDefaults(t *testing.T) {
	tests := []struct {
		name                  string
		option                HysteriaOption
		wantInitialStream     uint64
		wantMaxStream         uint64
		wantInitialConnection uint64
		wantMaxConnection     uint64
	}{
		{
			name:                  "custom stream window",
			option:                HysteriaOption{ReceiveWindowConn: 1234},
			wantInitialStream:     1234,
			wantMaxStream:         1234,
			wantInitialConnection: DefaultConnectionReceiveWindow / 10,
			wantMaxConnection:     DefaultConnectionReceiveWindow,
		},
		{
			name:                  "custom connection window",
			option:                HysteriaOption{ReceiveWindow: 5678},
			wantInitialStream:     DefaultStreamReceiveWindow / 10,
			wantMaxStream:         DefaultStreamReceiveWindow,
			wantInitialConnection: 5678,
			wantMaxConnection:     5678,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			config := hysteriaQUICConfig(test.option)
			if config.InitialStreamReceiveWindow != test.wantInitialStream {
				t.Fatalf("initial stream receive window = %d, want %d", config.InitialStreamReceiveWindow, test.wantInitialStream)
			}
			if config.MaxStreamReceiveWindow != test.wantMaxStream {
				t.Fatalf("max stream receive window = %d, want %d", config.MaxStreamReceiveWindow, test.wantMaxStream)
			}
			if config.InitialConnectionReceiveWindow != test.wantInitialConnection {
				t.Fatalf("initial connection receive window = %d, want %d", config.InitialConnectionReceiveWindow, test.wantInitialConnection)
			}
			if config.MaxConnectionReceiveWindow != test.wantMaxConnection {
				t.Fatalf("max connection receive window = %d, want %d", config.MaxConnectionReceiveWindow, test.wantMaxConnection)
			}
		})
	}
}
