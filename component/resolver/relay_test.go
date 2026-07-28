package resolver

import (
	"bytes"
	"context"
	"net"
	"testing"

	D "github.com/miekg/dns"
	"github.com/stretchr/testify/require"
)

type relayTestService func(context.Context, *D.Msg) (*D.Msg, error)

func (service relayTestService) ServeMsg(ctx context.Context, msg *D.Msg) (*D.Msg, error) {
	return service(ctx, msg)
}

func TestRelayDnsPacketCopiesCompressedReplyIntoTarget(t *testing.T) {
	previousService := DefaultService
	DefaultService = relayTestService(func(_ context.Context, request *D.Msg) (*D.Msg, error) {
		response := new(D.Msg)
		response.SetReply(request)
		for i := 0; i < 100; i++ {
			response.Answer = append(response.Answer, &D.A{
				Hdr: D.RR_Header{Name: request.Question[0].Name, Rrtype: D.TypeA, Class: D.ClassINET, Ttl: 60},
				A:   net.IPv4(192, 0, 2, byte(i+1)),
			})
		}
		return response, nil
	})
	t.Cleanup(func() { DefaultService = previousService })

	request := new(D.Msg)
	request.SetQuestion("example.com.", D.TypeA)
	payload, err := request.Pack()
	require.NoError(t, err)

	target := bytes.Repeat([]byte{0xa5}, SafeDnsPacketSize)
	data, err := RelayDnsPacket(context.Background(), payload, target)
	require.NoError(t, err)
	require.NotEmpty(t, data)
	require.Same(t, &target[0], &data[0])
	require.Equal(t, data, target[:len(data)])

	response := new(D.Msg)
	require.NoError(t, response.Unpack(target[:len(data)]))
	require.Len(t, response.Answer, 100)
}
