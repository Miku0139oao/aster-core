package outbound

import "testing"

func TestNewOpenVPNRejectsPortOutsideUint16Range(t *testing.T) {
	for _, port := range []int{-1, 65536} {
		_, err := NewOpenVPN(OpenVPNOption{Server: "vpn.example.com", Port: port})
		if err == nil {
			t.Fatalf("port %d was accepted, want validation error", port)
		}
		if got, want := err.Error(), "openvpn port must be between 1 and 65535"; got != want {
			t.Fatalf("port %d error = %q, want %q", port, got, want)
		}
	}
}
