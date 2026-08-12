package net

import (
	"io"
	stdnet "net"
	"testing"
)

func BenchmarkRelay32KiBComparison(b *testing.B) {
	sourceWriter, sourceRelay := stdnet.Pipe()
	destinationRelay, destinationReader := stdnet.Pipe()
	payload := make([]byte, 32*1024)
	total := int64(b.N) * int64(len(payload))

	copyDone := make(chan error, 1)
	go func() {
		_, err := io.CopyN(io.Discard, destinationReader, total)
		copyDone <- err
	}()
	relayDone := make(chan struct{})
	go func() {
		Relay(sourceRelay, destinationRelay)
		close(relayDone)
	}()

	b.ReportAllocs()
	b.SetBytes(int64(len(payload)))
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := sourceWriter.Write(payload); err != nil {
			b.Fatal(err)
		}
	}
	b.StopTimer()
	_ = sourceWriter.Close()
	if err := <-copyDone; err != nil {
		b.Fatal(err)
	}
	_ = destinationReader.Close()
	<-relayDone
}
