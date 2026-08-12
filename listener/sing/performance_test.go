package sing

import (
	"testing"

	C "github.com/Miku0139oao/aster-core/constant"
)

var packetMetadataSink *C.Metadata

func TestPacketMetadataPoolClearsState(t *testing.T) {
	metadata := acquirePacketMetadata(C.TUN)
	metadata.Host = "example.com"
	metadata.InUser = "user"
	releasePacketMetadata(metadata)

	metadata = acquirePacketMetadata(C.SOCKS5)
	defer releasePacketMetadata(metadata)
	if metadata.NetWork != C.UDP || metadata.Type != C.SOCKS5 {
		t.Fatalf("unexpected base metadata: %#v", metadata)
	}
	if metadata.Host != "" || metadata.InUser != "" {
		t.Fatalf("pooled metadata retained state: %#v", metadata)
	}
}

func BenchmarkPacketMetadata(b *testing.B) {
	b.Run("allocate", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			packetMetadataSink = &C.Metadata{NetWork: C.UDP, Type: C.TUN}
		}
	})
	b.Run("pool", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			metadata := acquirePacketMetadata(C.TUN)
			packetMetadataSink = metadata
			releasePacketMetadata(metadata)
		}
	})
}
