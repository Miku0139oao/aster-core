package shadowquic

import "testing"

func TestJLSMonitorAuthEarlyHostnameFallback(t *testing.T) {
	if jlsMonitorAuthEarly("example.com:443", true) {
		t.Fatal("hostname candidates must authenticate before the dialer selects them")
	}
	if jlsMonitorAuthEarly("example.com:443", false) {
		t.Fatal("hostname candidates must authenticate even for non-early dials")
	}
	if !jlsMonitorAuthEarly("127.0.0.1:443", true) {
		t.Fatal("literal addresses should keep early auth monitoring")
	}
	if jlsMonitorAuthEarly("127.0.0.1:443", false) {
		t.Fatal("non-early literal dials should wait for handshake auth")
	}
	if !jlsMonitorAuthEarly("[::1]:443", true) {
		t.Fatal("IPv6 literals should keep early auth monitoring")
	}
}
