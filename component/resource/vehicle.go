package resource

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/Miku0139oao/aster-core/common/utils"
	mihomoHttp "github.com/Miku0139oao/aster-core/component/http"
	"github.com/Miku0139oao/aster-core/component/profile/cachefile"
	P "github.com/Miku0139oao/aster-core/constant/provider"

	"github.com/metacubex/http"
)

const (
	DefaultHttpTimeout = time.Second * 20

	fileMode os.FileMode = 0o666
	dirMode  os.FileMode = 0o755
)

var ErrResourceTooLarge = errors.New("resource exceeds size limit")

var etag = false

func ETag() bool {
	return etag
}

func SetETag(b bool) {
	etag = b
}

func safeWrite(path string, buf []byte) error {
	dir := filepath.Dir(path)

	if _, err := os.Stat(dir); os.IsNotExist(err) {
		if err := os.MkdirAll(dir, dirMode); err != nil {
			return err
		}
	}

	return os.WriteFile(path, buf, fileMode)
}

type FileVehicle struct {
	path string
}

func (f *FileVehicle) Type() P.VehicleType {
	return P.File
}

func (f *FileVehicle) Path() string {
	return f.path
}

func (f *FileVehicle) Url() string {
	return "file://" + f.path
}

func (f *FileVehicle) Read(ctx context.Context, oldHash utils.HashType) (buf []byte, hash utils.HashType, err error) {
	buf, err = os.ReadFile(f.path)
	if err != nil {
		return
	}
	hash = utils.MakeHash(buf)
	return
}

func (f *FileVehicle) Proxy() string {
	return ""
}

func (f *FileVehicle) Write(buf []byte) error {
	return safeWrite(f.path, buf)
}

func NewFileVehicle(path string) *FileVehicle {
	return &FileVehicle{path: path}
}

type HTTPVehicle struct {
	url       string
	path      string
	proxy     string
	header    http.Header
	timeout   time.Duration
	sizeLimit int64
	inRead    func(response *http.Response)
	provider  P.ProxyProvider
}

func (h *HTTPVehicle) Url() string {
	return h.url
}

func (h *HTTPVehicle) Type() P.VehicleType {
	return P.HTTP
}

func (h *HTTPVehicle) Path() string {
	return h.path
}

func (h *HTTPVehicle) Proxy() string {
	return h.proxy
}

func (h *HTTPVehicle) Write(buf []byte) error {
	return safeWrite(h.path, buf)
}

func (h *HTTPVehicle) SetInRead(fn func(response *http.Response)) {
	h.inRead = fn
}

func (h *HTTPVehicle) Read(ctx context.Context, oldHash utils.HashType) (buf []byte, hash utils.HashType, err error) {
	ctx, cancel := context.WithTimeout(ctx, h.timeout)
	defer cancel()
	header := h.header
	setIfNoneMatch := false
	if etag && oldHash.IsValid() {
		etagWithHash := cachefile.Cache().GetETagWithHash(h.url)
		if oldHash.Equal(etagWithHash.Hash) && etagWithHash.ETag != "" {
			if header == nil {
				header = http.Header{}
			} else {
				header = header.Clone()
			}
			header.Set("If-None-Match", etagWithHash.ETag)
			setIfNoneMatch = true
		}
	}
	resp, err := mihomoHttp.HttpRequest(ctx, h.url, http.MethodGet, header, nil, mihomoHttp.WithSpecialProxy(h.proxy))
	if err != nil {
		return
	}
	defer resp.Body.Close()

	if h.inRead != nil {
		h.inRead(resp)
	}

	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		if setIfNoneMatch && resp.StatusCode == http.StatusNotModified {
			return nil, oldHash, nil
		}
		err = errors.New(resp.Status)
		return
	}
	buf, err = readWithLimit(resp.Body, h.sizeLimit)
	if err != nil {
		return
	}
	hash = utils.MakeHash(buf)
	if etag {
		cachefile.Cache().SetETagWithHash(h.url, cachefile.EtagWithHash{
			Hash: hash,
			ETag: resp.Header.Get("ETag"),
			Time: time.Now(),
		})
	}
	return
}

func readWithLimit(reader io.Reader, limit int64) ([]byte, error) {
	if limit <= 0 || limit == int64(^uint64(0)>>1) {
		return io.ReadAll(reader)
	}
	limited := &io.LimitedReader{R: reader, N: limit + 1}
	buf, err := io.ReadAll(limited)
	if err != nil {
		return nil, err
	}
	if int64(len(buf)) > limit {
		return nil, ErrResourceTooLarge
	}
	return buf, nil
}

func NewHTTPVehicle(url string, path string, proxy string, header http.Header, timeout time.Duration, sizeLimit int64) *HTTPVehicle {
	return &HTTPVehicle{
		url:       url,
		path:      path,
		proxy:     proxy,
		header:    header,
		timeout:   timeout,
		sizeLimit: sizeLimit,
	}
}
