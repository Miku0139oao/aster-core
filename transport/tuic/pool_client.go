package tuic

import (
	"context"
	"errors"
	"net"
	"sync"
	"sync/atomic"
	"time"

	N "github.com/Miku0139oao/aster-core/common/net"
	C "github.com/Miku0139oao/aster-core/constant"

	list "github.com/bahlo/generic-list-go"
)

type PoolClient struct {
	newClientOptionV4 *ClientOptionV4
	newClientOptionV5 *ClientOptionV5

	dialFn          DialFunc
	tcpClients      list.List[Client]
	tcpClientsMutex sync.Mutex
	udpClients      list.List[Client]
	udpClientsMutex sync.Mutex
	closed          atomic.Bool
	closeOnce       sync.Once
}

func (t *PoolClient) DialContext(ctx context.Context, metadata *C.Metadata) (net.Conn, error) {
	client, err := t.getClient(false)
	if err != nil {
		return nil, err
	}
	conn, err := client.DialContext(ctx, metadata)
	if errors.Is(err, TooManyOpenStreams) {
		client, err = t.newClient(false)
		if err == nil {
			conn, err = client.DialContext(ctx, metadata)
		}
	}
	if err != nil {
		return nil, err
	}
	return N.NewRefConn(conn, t), err
}

func (t *PoolClient) ListenPacket(ctx context.Context, metadata *C.Metadata) (net.PacketConn, error) {
	client, err := t.getClient(true)
	if err != nil {
		return nil, err
	}
	pc, err := client.ListenPacket(ctx, metadata)
	if errors.Is(err, TooManyOpenStreams) {
		client, err = t.newClient(true)
		if err == nil {
			pc, err = client.ListenPacket(ctx, metadata)
		}
	}
	if err != nil {
		return nil, err
	}
	return N.NewRefPacketConn(pc, t), nil
}

func (t *PoolClient) newClient(udp bool) (client Client, err error) {
	if t.closed.Load() {
		return nil, net.ErrClosed
	}
	clients := &t.tcpClients
	clientsMutex := &t.tcpClientsMutex
	if udp {
		clients = &t.udpClients
		clientsMutex = &t.udpClientsMutex
	}

	clientsMutex.Lock()
	defer clientsMutex.Unlock()
	if t.closed.Load() {
		return nil, net.ErrClosed
	}

	if t.newClientOptionV4 != nil {
		client = NewClientV4(t.newClientOptionV4, udp, t.dialFn)
	} else {
		client = NewClientV5(t.newClientOptionV5, udp, t.dialFn)
	}

	client.SetLastVisited(time.Now())

	clients.PushFront(client)
	return client, nil
}

func (t *PoolClient) getClient(udp bool) (Client, error) {
	if t.closed.Load() {
		return nil, net.ErrClosed
	}
	clients := &t.tcpClients
	clientsMutex := &t.tcpClientsMutex
	if udp {
		clients = &t.udpClients
		clientsMutex = &t.udpClientsMutex
	}
	var bestClient Client

	func() {
		clientsMutex.Lock()
		defer clientsMutex.Unlock()
		for it := clients.Front(); it != nil; {
			client := it.Value
			if client == nil {
				next := it.Next()
				clients.Remove(it)
				it = next
				continue
			}
			if bestClient == nil {
				bestClient = client
			} else {
				if client.OpenStreams() < bestClient.OpenStreams() {
					bestClient = client
				}
			}
			it = it.Next()
		}
		for it := clients.Front(); it != nil; {
			client := it.Value
			if client != bestClient && client.OpenStreams() == 0 && time.Now().Sub(client.LastVisited()) > 30*time.Minute {
				client.Close()
				next := it.Next()
				clients.Remove(it)
				it = next
				continue
			}
			it = it.Next()
		}
	}()

	if bestClient == nil {
		return t.newClient(udp)
	}
	if t.closed.Load() {
		return nil, net.ErrClosed
	}
	bestClient.SetLastVisited(time.Now())
	return bestClient, nil
}

func (t *PoolClient) Close() {
	t.closeOnce.Do(func() {
		t.closed.Store(true)
		clients := make([]Client, 0)
		t.tcpClientsMutex.Lock()
		for item := t.tcpClients.Front(); item != nil; {
			next := item.Next()
			if item.Value != nil {
				clients = append(clients, item.Value)
			}
			t.tcpClients.Remove(item)
			item = next
		}
		t.tcpClientsMutex.Unlock()
		t.udpClientsMutex.Lock()
		for item := t.udpClients.Front(); item != nil; {
			next := item.Next()
			if item.Value != nil {
				clients = append(clients, item.Value)
			}
			t.udpClients.Remove(item)
			item = next
		}
		t.udpClientsMutex.Unlock()
		for _, client := range clients {
			client.Close()
		}
	})
}

func NewPoolClientV4(clientOption *ClientOptionV4, dialFn DialFunc) *PoolClient {
	p := &PoolClient{
		dialFn: dialFn,
	}
	newClientOption := *clientOption
	p.newClientOptionV4 = &newClientOption
	return p
}

func NewPoolClientV5(clientOption *ClientOptionV5, dialFn DialFunc) *PoolClient {
	p := &PoolClient{
		dialFn: dialFn,
	}
	newClientOption := *clientOption
	p.newClientOptionV5 = &newClientOption
	return p
}
