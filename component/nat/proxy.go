package nat

import (
	"net"
	"sync"

	C "github.com/Miku0139oao/aster-core/constant"
)

type writeBackProxy struct {
	mu sync.RWMutex
	wb C.WriteBack
}

func (w *writeBackProxy) WriteBack(b []byte, addr net.Addr) (n int, err error) {
	w.mu.RLock()
	wb := w.wb
	w.mu.RUnlock()
	return wb.WriteBack(b, addr)
}

func (w *writeBackProxy) UpdateWriteBack(wb C.WriteBack) {
	w.mu.Lock()
	w.wb = wb
	w.mu.Unlock()
}

func NewWriteBackProxy(wb C.WriteBack) C.WriteBackProxy {
	w := &writeBackProxy{}
	w.UpdateWriteBack(wb)
	return w
}
