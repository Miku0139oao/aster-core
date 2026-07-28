package inbound

import (
	"net"

	C "github.com/Miku0139oao/aster-core/constant"
	"github.com/Miku0139oao/aster-core/transport/socks5"
)

// NewHTTP receive normal http request and return HTTPContext
func NewHTTP(target socks5.Addr, srcConn net.Conn, conn net.Conn, additions ...Addition) (net.Conn, *C.Metadata) {
	metadata := parseSocksAddr(target)
	metadata.NetWork = C.TCP
	metadata.Type = C.HTTP
	metadata.RawSrcAddr = srcConn.RemoteAddr()
	metadata.RawDstAddr = srcConn.LocalAddr()
	ApplyAdditions(metadata, WithSrcAddr(srcConn.RemoteAddr()), WithInAddr(srcConn.LocalAddr()))
	ApplyAdditions(metadata, additions...)
	return conn, metadata
}
