# 建置與測試

## Go 版本

`go.mod` 宣告最低 Go 1.20。CI 涵蓋 1.20–1.26，以確保舊版相容與新版本行為。

```sh
go version
go mod download
```

## 本機建置

```sh
# 快速建置
go build -o aster-core .

# Release-equivalent features
CGO_ENABLED=0 go build \
  -tags with_gvisor \
  -trimpath \
  -o aster-core \
  .
```

正式 Makefile 還會透過 ldflags 注入：

- `constant.Version`
- `constant.BuildTime`
- `constant.ReleaseAsset`

因此直接 `go build` 顯示的版本是開發預設值，不應拿來當正式 release。

## Build tags

| Tag | 作用 |
| --- | --- |
| `with_gvisor` | gVisor TUN stack 與完整 Tailscale |
| `with_low_memory` | 低記憶體 buffer 策略 |
| `no_tailscale` | 排除 Tailscale |
| `no_fake_tcp` | 排除 Hysteria fake TCP |
| `cmfa` | CMFA integration mode |

檢查 binary：

```sh
./aster-core -v
```

輸出會列出實際 build tags。

## Make targets

```sh
make linux-amd64-v1
make linux-arm64
make windows-amd64-v1
make darwin-arm64
make all
make all-arch
make releases
```

- `all`：主要桌面/伺服器 targets。
- `all-arch`：全部未壓縮 targets。
- `releases`：Unix `.gz` 與 Windows `.zip`。
- `make vet`：歷史命名，實際執行 `go test ./...`，不是 `go vet`。

## Root module tests

避免下載外部 V2Ray interop binary：

```sh
SKIP_INTEROP_TEST=1 go test ./... -count=1
```

GVisor：

```sh
SKIP_INTEROP_TEST=1 go test ./... -count=1 -tags with_gvisor
```

要執行 VMess interoperability test，移除 `SKIP_INTEROP_TEST`。測試會在 temp directory 下載並建置外部依賴，所需時間較長。

## Race tests

CI 的 targeted set：

```sh
CGO_ENABLED=1 SKIP_INTEROP_TEST=1 go test -race \
  ./common/net \
  ./component/aster \
  ./hub/route \
  ./listener/anytls \
  ./listener/sing_vless \
  ./tunnel/statistic
```

這些 package 涵蓋高併發資料結構、Aster state/API、managed credentials 與 traffic。

## Lint 與 format

```sh
go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.12.2
golangci-lint run --timeout=10m
```

設定啟用：

- `govet`
- `staticcheck`
- `gci`
- `gofumpt`

提交前也執行：

```sh
go mod tidy -diff
git diff --check
```

## Docker interoperability module

`test/` 有自己的 `go.mod`，**不會**被 root `go test ./...` 執行。

需求：

- Docker daemon
- 可下載多個 proxy server images
- 足夠的 network/port 權限

```sh
cd test
make test
make benchmark
```

涵蓋：

- Shadowsocks + obfs/plugin
- VMess normal/AEAD/HTTP/H2/TLS/WebSocket/gRPC
- Trojan
- Snell
- VLESS
- DNS/fake-IP/hosts
- TCP/UDP ping-pong 與大資料

Benchmark 適合比較相對變動，不代表正式環境絕對 throughput。

## 文件站

```sh
cd docs
npm install
npm run dev
npm run build
```

GitHub Pages 子路徑可指定：

```sh
DOCS_BASE=/aster-core/ npm run build
```

文件變更應確認：

- Local search 能找到新頁。
- Sidebar links 不失效。
- Code examples 可解析。
- `config.yaml` raw link 正常。
- Mobile table 可水平捲動。
