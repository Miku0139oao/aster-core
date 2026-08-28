package http

import (
	"context"
	"fmt"
	"io"
	"net"
	"sync"

	"github.com/Miku0139oao/aster-core/adapter/inbound"
	N "github.com/Miku0139oao/aster-core/common/net"
	"github.com/Miku0139oao/aster-core/component/auth"
	C "github.com/Miku0139oao/aster-core/constant"
	"github.com/Miku0139oao/aster-core/log"

	"github.com/metacubex/http"
)

type bodyWrapper struct {
	io.ReadCloser
	once     sync.Once
	onHitEOF func()
}

func (b *bodyWrapper) Read(p []byte) (n int, err error) {
	n, err = b.ReadCloser.Read(p)
	if err == io.EOF && b.onHitEOF != nil {
		b.once.Do(b.onHitEOF)
	}
	return n, err
}

var (
	connectEstablishedHTTP10 = []byte("HTTP/1.0 200 Connection established\r\n\r\n")
	connectEstablishedHTTP11 = []byte("HTTP/1.1 200 Connection established\r\n\r\n")
)

func writeConnectEstablished(w io.Writer, protoMajor, protoMinor int) error {
	var err error
	switch {
	case protoMajor == 1 && protoMinor == 1:
		_, err = w.Write(connectEstablishedHTTP11)
	case protoMajor == 1 && protoMinor == 0:
		_, err = w.Write(connectEstablishedHTTP10)
	default:
		_, err = fmt.Fprintf(w, "HTTP/%d.%d 200 Connection established\r\n\r\n", protoMajor, protoMinor)
	}
	return err
}

func HandleConn(c net.Conn, tunnel C.Tunnel, store auth.AuthStore, additions ...inbound.Addition) {
	conn := N.NewBufferedConn(c)

	authenticator := store.Authenticator()
	trusted := authenticator == nil // disable authenticate if lru is nil
	lastUser := ""
	inUserIdx := -1

	var (
		client    *http.Client
		ctx       context.Context
		cancel    context.CancelFunc
		peekMutex sync.Mutex
	)
	defer func() {
		if client != nil {
			client.CloseIdleConnections()
		}
		if cancel != nil {
			cancel()
		}
	}()

	for {
		if client != nil {
			peekMutex.Lock()
		}
		request, err := ReadRequest(conn.Reader())
		if client != nil {
			peekMutex.Unlock()
		}
		if err != nil {
			break
		}

		request.RemoteAddr = conn.RemoteAddr().String()

		resp, user := authenticate(request, authenticator) // always call authenticate function to get user
		if resp == nil {
			trusted = true
		}

		if trusted {
			if request.Method == http.MethodConnect {
				// Manual writing to support CONNECT for http 1.0 (workaround for uplay client)
				if err = writeConnectEstablished(conn, request.ProtoMajor, request.ProtoMinor); err != nil {
					break // close connection
				}

				hijacked, metadata := inbound.NewHTTPS(request, conn, additions...)
				metadata.InUser = user
				tunnel.HandleTCPConn(hijacked, metadata)

				return // hijack connection
			}

			if inUserIdx < 0 {
				additions = append(additions, inbound.Placeholder) // Add a placeholder for InUser
				inUserIdx = len(additions) - 1
			}
			additions[inUserIdx] = inbound.WithInUser(user)

			if client == nil {
				client = newClient(c, tunnel, additions)
				ctx, cancel = context.WithCancel(context.Background())
			}

			host := request.Header.Get("Host")
			if host != "" {
				request.Host = host
			}

			request.RequestURI = ""

			if isUpgradeRequest(request) {
				handleUpgrade(conn, request, tunnel, additions...)

				return // hijack connection
			}

			// ensure there is a client with correct additions
			// when the authenticated user changed, outbound client should close idle connections
			if user != lastUser {
				client.CloseIdleConnections()
				lastUser = user
			}

			removeHopByHopHeaders(request.Header)
			removeExtraHTTPHostPort(request)

			if request.URL.Scheme == "" || request.URL.Host == "" {
				resp = responseWith(request, http.StatusBadRequest)
			} else {
				request = request.WithContext(ctx)

				startBackgroundRead := func() {
					go func() {
						peekMutex.Lock()
						defer peekMutex.Unlock()
						_, err := conn.Peek(1)
						if err != nil {
							cancel()
						}
					}()
				}
				if request.Body == nil || request.Body == http.NoBody {
					startBackgroundRead()
				} else {
					request.Body = &bodyWrapper{ReadCloser: request.Body, onHitEOF: startBackgroundRead}
				}
				resp, err = client.Do(request)
				if err != nil {
					resp = responseWith(request, http.StatusBadGateway)
				}
			}

			removeHopByHopHeaders(resp.Header)
		}

		keepAlive := isProxyKeepAlive(request.Header.Get("Proxy-Connection"))
		if !keepAlive {
			resp.Close = true // close connection if keep-alive is not set
		}
		if keepAlive && resp.ContentLength > 0 {
			resp.Close = false // don't need to close connection if content length is positive numbers
		}

		if !resp.Close {
			resp.Header.Set("Proxy-Connection", "keep-alive")
			resp.Header.Set("Connection", "keep-alive")
			resp.Header.Set("Keep-Alive", "timeout=4")
		}

		err = resp.Write(conn)
		if err != nil || resp.Close {
			break // close connection
		}
	}

	_ = conn.Close()
}

func authenticate(request *http.Request, authenticator auth.Authenticator) (resp *http.Response, user string) {
	credential := parseBasicProxyAuthorization(request)
	if credential == "" {
		if authenticator != nil {
			resp = responseWith(request, http.StatusProxyAuthRequired)
			resp.Header.Set("Proxy-Authenticate", "Basic")
			return
		}
		log.Debugln("Auth success from %s -> %s", request.RemoteAddr, user)
		return
	}
	user, pass, err := decodeBasicProxyAuthorization(credential)
	authed := authenticator == nil || (err == nil && authenticator.Verify(user, pass))
	if !authed {
		log.Infoln("Auth failed from %s", request.RemoteAddr)
		return responseWith(request, http.StatusForbidden), user
	}
	log.Debugln("Auth success from %s -> %s", request.RemoteAddr, user)
	return
}

func responseWith(request *http.Request, statusCode int) *http.Response {
	return &http.Response{
		StatusCode: statusCode,
		Status:     http.StatusText(statusCode),
		Proto:      request.Proto,
		ProtoMajor: request.ProtoMajor,
		ProtoMinor: request.ProtoMinor,
		Header:     http.Header{},
	}
}
