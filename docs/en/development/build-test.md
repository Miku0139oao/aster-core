# Build and test

## Go version

`go.mod` declares a minimum of Go 1.20. CI covers 1.20–1.26 so older compatibility and newer behavior are both checked.

```sh
go version
go mod download
```

## Local build

```sh
# Fast build
go build -o aster-core .

# Release-equivalent features
CGO_ENABLED=0 go build \
  -tags with_gvisor \
  -trimpath \
  -o aster-core \
  .
```

The official Makefile also injects through ldflags:

- `constant.Version`
- `constant.BuildTime`
- `constant.ReleaseAsset`

A plain `go build` therefore prints the development default version. Do not treat that as an official release.

## Build tags

| Tag | Effect |
| --- | --- |
| `with_gvisor` | gVisor TUN stack and full Tailscale |
| `with_low_memory` | Lower-memory buffer strategy |
| `no_tailscale` | Exclude Tailscale |
| `no_fake_tcp` | Exclude Hysteria fake TCP |
| `cmfa` | CMFA integration mode |

Inspect the binary:

```sh
./aster-core -v
```

The output lists the actual build tags.

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

- `all`: main desktop/server targets.
- `all-arch`: all uncompressed targets.
- `releases`: Unix `.gz` and Windows `.zip`.
- `make vet`: historical name; it actually runs `go test ./...`, not `go vet`.

## Root-module tests

Skip downloading an external V2Ray interop binary:

```sh
SKIP_INTEROP_TEST=1 go test ./... -count=1
```

GVisor:

```sh
SKIP_INTEROP_TEST=1 go test ./... -count=1 -tags with_gvisor
```

To run the VMess interoperability test, unset `SKIP_INTEROP_TEST`. The test downloads and builds an external dependency in a temp directory and takes longer.

## Race tests

CI’s targeted set:

```sh
CGO_ENABLED=1 SKIP_INTEROP_TEST=1 go test -race \
  ./common/net \
  ./component/aster \
  ./hub/route \
  ./listener/anytls \
  ./listener/sing_vless \
  ./tunnel/statistic
```

These packages cover high-concurrency data structures, Aster state/API, managed credentials, and traffic.

## Lint and format

```sh
go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.12.2
golangci-lint run --timeout=10m
```

The configuration enables:

- `govet`
- `staticcheck`
- `gci`
- `gofumpt`

Before you commit, also run:

```sh
go mod tidy -diff
git diff --check
```

## Docker interoperability module

`test/` has its own `go.mod` and is **not** run by root `go test ./...`.

Requirements:

- Docker daemon
- Ability to download several proxy-server images
- Enough network/port privileges

```sh
cd test
make test
make benchmark
```

Coverage includes:

- Shadowsocks + obfs/plugin
- VMess normal/AEAD/HTTP/H2/TLS/WebSocket/gRPC
- Trojan
- Snell
- VLESS
- DNS/fake-IP/hosts
- TCP/UDP ping-pong and large payloads

Benchmarks are useful for relative change. They are not absolute production throughput.

## Documentation site

```sh
cd docs
npm install
npm run dev
npm run build
```

A GitHub Pages subpath can be set with:

```sh
DOCS_BASE=/aster-core/ npm run build
```

Documentation changes should confirm:

- Local search can find the new page.
- Sidebar links are not broken.
- Code examples parse.
- The `config.yaml` raw link works.
- Mobile tables can scroll horizontally.
- Both the Traditional Chinese (`/`) and English (`/en/`) locales build.
