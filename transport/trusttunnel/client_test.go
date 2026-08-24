package trusttunnel

import (
	"context"
	"testing"
)

func TestPoolClientCloseRejectsReuse(t *testing.T) {
	pool := &PoolClient{}
	if err := pool.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.getClient(); err == nil {
		t.Fatal("closed pool created a replacement client")
	}
}

func TestClientCloseCancelsHealthLoop(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	client := &Client{ctx: ctx, cancel: cancel, healthCheck: true}
	client.start()
	client.healthCheckMu.Lock()
	done := client.healthCheckDone
	client.healthCheckMu.Unlock()
	if done == nil {
		t.Fatal("health loop did not start")
	}
	if err := client.Close(); err != nil {
		t.Fatal(err)
	}
	select {
	case <-done:
	default:
		t.Fatal("Client.Close returned before the health loop exited")
	}
}
