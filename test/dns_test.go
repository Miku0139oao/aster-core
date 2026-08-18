package main

import (
	"fmt"
	"net"
	"testing"
	"time"

	"github.com/miekg/dns"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func startTestDNSServer(t *testing.T) string {
	t.Helper()

	conn, err := net.ListenPacket("udp", "127.0.0.1:0")
	require.NoError(t, err)

	server := &dns.Server{
		PacketConn: conn,
		Handler: dns.HandlerFunc(func(w dns.ResponseWriter, r *dns.Msg) {
			response := new(dns.Msg)
			response.SetReply(r)

			for _, question := range r.Question {
				header := dns.RR_Header{
					Name:   question.Name,
					Rrtype: question.Qtype,
					Class:  dns.ClassINET,
					Ttl:    60,
				}

				switch {
				case question.Name == "1.1.1.1.nip.io." && question.Qtype == dns.TypeA:
					response.Answer = append(response.Answer, &dns.A{
						Hdr: header,
						A:   net.IPv4(1, 1, 1, 1),
					})
				case question.Name == "2606-4700-4700--1111.sslip.io." && question.Qtype == dns.TypeAAAA:
					response.Answer = append(response.Answer, &dns.AAAA{
						Hdr:  header,
						AAAA: net.ParseIP("2606:4700:4700::1111"),
					})
				}
			}

			_ = w.WriteMsg(response)
		}),
	}

	serveErr := make(chan error, 1)
	go func() {
		serveErr <- server.ActivateAndServe()
	}()

	t.Cleanup(func() {
		require.NoError(t, server.Shutdown())
		require.NoError(t, <-serveErr)
	})

	return conn.LocalAddr().String()
}

func exchange(address, domain string, tp uint16) ([]dns.RR, error) {
	client := dns.Client{}
	query := &dns.Msg{}
	query.SetQuestion(dns.Fqdn(domain), tp)

	r, _, err := client.Exchange(query, address)
	if err != nil {
		return nil, err
	}
	return r.Answer, nil
}

func TestMihomo_DNS(t *testing.T) {
	upstream := startTestDNSServer(t)
	basic := fmt.Sprintf(`
log-level: silent
dns:
  enable: true
  listen: 0.0.0.0:8553
  nameserver:
    - %s
`, upstream)

	err := parseAndApply(basic)
	require.NoError(t, err)
	defer cleanup()

	time.Sleep(waitTime)

	rr, err := exchange("127.0.0.1:8553", "1.1.1.1.nip.io", dns.TypeA)
	require.NoError(t, err)
	require.NotEmptyf(t, rr, "record empty")

	record, ok := rr[0].(*dns.A)
	require.True(t, ok)
	assert.Equal(t, "1.1.1.1", record.A.String())

	rr, err = exchange("127.0.0.1:8553", "2606-4700-4700--1111.sslip.io", dns.TypeAAAA)
	assert.NoError(t, err)
	assert.Empty(t, rr)
}

func TestMihomo_DNSHostAndFakeIP(t *testing.T) {
	upstream := startTestDNSServer(t)
	basic := fmt.Sprintf(`
log-level: silent
hosts:
  foo.mihomo.dev: 1.1.1.1
dns:
  enable: true
  listen: 0.0.0.0:8553
  ipv6: true
  enhanced-mode: fake-ip
  fake-ip-range: 198.18.0.1/16
  fake-ip-filter:
    - .sslip.io
  nameserver:
    - %s
`, upstream)

	err := parseAndApply(basic)
	require.NoError(t, err)
	defer cleanup()

	time.Sleep(waitTime)

	type domainPair struct {
		domain string
		ip     string
	}

	list := []domainPair{
		{"foo.org", "198.18.0.4"},
		{"bar.org", "198.18.0.5"},
		{"foo.org", "198.18.0.4"},
		{"foo.mihomo.dev", "1.1.1.1"},
	}

	for _, pair := range list {
		rr, err := exchange("127.0.0.1:8553", pair.domain, dns.TypeA)
		require.NoError(t, err)
		require.NotEmpty(t, rr)

		record, ok := rr[0].(*dns.A)
		require.True(t, ok)
		assert.Equal(t, pair.ip, record.A.String())
	}

	rr, err := exchange("127.0.0.1:8553", "2606-4700-4700--1111.sslip.io", dns.TypeAAAA)
	require.NoError(t, err)
	require.NotEmpty(t, rr)
	record, ok := rr[0].(*dns.AAAA)
	require.True(t, ok)
	assert.Equal(t, "2606:4700:4700::1111", record.AAAA.String())
}
