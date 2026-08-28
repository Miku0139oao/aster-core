package nat

import (
	"net"
	"sync/atomic"

	C "github.com/Miku0139oao/aster-core/constant"
)

// writeBackProxy is a per-flow UDP write-back handle. Process updates the
// target from the inbound packet goroutine while handleUDPToLocal calls
// WriteBack from the reverse-path goroutine, so this is on the per-packet
// path. atomic.Value publishes the interface without a mutex; Store panics
// if the concrete type changes, which does not happen for a single NAT flow.
type writeBackProxy struct {
	wb atomic.Value // C.WriteBack
}

func (w *writeBackProxy) WriteBack(b []byte, addr net.Addr) (n int, err error) {
	return w.wb.Load().(C.WriteBack).WriteBack(b, addr)
}

func (w *writeBackProxy) UpdateWriteBack(wb C.WriteBack) {
	if wb == nil {
		return
	}
	w.wb.Store(wb)
}

func NewWriteBackProxy(wb C.WriteBack) C.WriteBackProxy {
	w := &writeBackProxy{}
	w.UpdateWriteBack(wb)
	return w
}
