package resource

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Miku0139oao/aster-core/common/utils"
	P "github.com/Miku0139oao/aster-core/constant/provider"
)

type fetcherTestVehicle struct{}

func (fetcherTestVehicle) Read(context.Context, utils.HashType) ([]byte, utils.HashType, error) {
	return nil, utils.HashType{}, errors.New("not used")
}
func (fetcherTestVehicle) Write([]byte) error  { return nil }
func (fetcherTestVehicle) Path() string        { return "" }
func (fetcherTestVehicle) Url() string         { return "" }
func (fetcherTestVehicle) Proxy() string       { return "" }
func (fetcherTestVehicle) Type() P.VehicleType { return P.Compatible }

func TestFetcherCloseFencesUpdateCallback(t *testing.T) {
	callbackStarted := make(chan struct{})
	releaseCallback := make(chan struct{})
	var callbacks atomic.Int64
	fetcher := NewFetcher("test", 0, fetcherTestVehicle{}, nil, func(data []byte) ([]byte, error) {
		return append([]byte(nil), data...), nil
	}, func([]byte) {
		callbacks.Add(1)
		close(callbackStarted)
		<-releaseCallback
	})

	updateDone := make(chan error, 1)
	go func() {
		_, _, err := fetcher.SideUpdate([]byte("first"))
		updateDone <- err
	}()
	select {
	case <-callbackStarted:
	case <-time.After(2 * time.Second):
		t.Fatal("callback did not start")
	}
	closeDone := make(chan error, 1)
	go func() { closeDone <- fetcher.Close() }()
	select {
	case <-closeDone:
		t.Fatal("Close returned before the callback transaction finished")
	case <-time.After(100 * time.Millisecond):
	}
	close(releaseCallback)
	if err := <-updateDone; err != nil {
		t.Fatal(err)
	}
	if err := <-closeDone; err != nil {
		t.Fatal(err)
	}

	if _, _, err := fetcher.SideUpdate([]byte("second")); !errors.Is(err, context.Canceled) {
		t.Fatalf("post-close update error = %v", err)
	}
	if callbacks.Load() != 1 {
		t.Fatalf("post-close callback count = %d", callbacks.Load())
	}
	if err := fetcher.Close(); err != nil {
		t.Fatal(err)
	}
}
