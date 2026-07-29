<p align="center">
  <img src="docs/logo.png" width="160" alt="Aster Core logo">
</p>

<h1 align="center">Aster Core</h1>

<p align="center">
  A Mihomo-compatible universal proxy core with live VLESS and AnyTLS user management.
</p>

<p align="center">
  <a href="https://github.com/Miku0139oao/aster-core/actions/workflows/test.yml">
    <img src="https://github.com/Miku0139oao/aster-core/actions/workflows/test.yml/badge.svg" alt="Test status">
  </a>
  <a href="https://goreportcard.com/report/github.com/Miku0139oao/aster-core">
    <img src="https://goreportcard.com/badge/github.com/Miku0139oao/aster-core?style=flat-square" alt="Go Report Card">
  </a>
  <img src="https://img.shields.io/github/go-mod/go-version/Miku0139oao/aster-core/main?style=flat-square" alt="Go version">
  <a href="https://github.com/Miku0139oao/aster-core/releases">
    <img src="https://img.shields.io/github/v/release/Miku0139oao/aster-core?display_name=tag&sort=semver&style=flat-square" alt="Latest release">
  </a>
  <a href="LICENSE">
    <img src="https://img.shields.io/github/license/Miku0139oao/aster-core?style=flat-square" alt="GPL-3.0 license">
  </a>
</p>

Aster Core is a security-focused fork of [MetaCubeX/mihomo](https://github.com/MetaCubeX/mihomo). It keeps the Mihomo YAML configuration format and Clash-compatible Controller API, while adding an opt-in management plane for live VLESS and AnyTLS user updates, per-user traffic accounting, persistent state, and subscription links.

The current upstream baseline is Mihomo `v1.19.29` at commit `e26714a181ac0e2fa803453c0a8e9a9ce94e31cb`. See [NOTICE.md](NOTICE.md) and [UPSTREAM.md](UPSTREAM.md) for provenance and the upstream update policy.

> [!IMPORTANT]
> Aster Core accepts Mihomo configuration, not sing-box or Xray configuration. It aims to preserve Mihomo and Clash API compatibility, but Aster-specific behavior and the compatibility notes below still apply.

## Contents

- [Highlights](#highlights)
- [Supported capabilities](#supported-capabilities)
- [Quick start](#quick-start)
- [Configuration](#configuration)
- [Aster user management](#aster-user-management)
- [Deployment](#deployment)
- [Command-line reference](#command-line-reference)
- [Architecture](#architecture)
- [Development](#development)
- [Compatibility and operational notes](#compatibility-and-operational-notes)
- [License and credits](#license-and-credits)

## Highlights

- Mihomo-compatible YAML, rule engine, proxy providers, rule providers, and Clash-compatible REST API.
- HTTP, SOCKS5, mixed, transparent proxy, TUN, and protocol-server inbounds.
- Broad outbound protocol support, including VLESS/XHTTP, VMess, Shadowsocks, Trojan, AnyTLS, Hysteria, TUIC, WireGuard, OpenVPN, Tailscale, and more.
- Integrated DNS server with fake-IP, hosts, policy routing, cache, and DoH/DoT/DoQ/DHCP upstream support.
- `select`, `url-test`, `fallback`, and `load-balance` proxy groups with health checks.
- Domain, GeoIP, GeoSite, IP/CIDR, ASN, process, port, inbound, user, network, and logical routing rules.
- Live VLESS and AnyTLS user CRUD without recreating the listener.
- Per-principal upload, download, and active-connection accounting.
- Durable Aster state with locking, generation checks, a redundant backup, and restrictive file permissions.
- Linux, Windows, macOS, Android, and FreeBSD build targets; OpenWrt/Nikki integration is included.

## Supported capabilities

| Area | Included |
| --- | --- |
| Local and transparent inbounds | HTTP, SOCKS5, mixed, tunnel, TUN, redir, TProxy |
| Protocol inbounds | Shadowsocks, Snell, VMess, VLESS, Trojan, Hysteria 2, Hysteria 2 Realm, TUIC, ShadowQUIC, AnyTLS, Mieru, Sudoku, TrustTunnel |
| Outbounds | Shadowsocks, ShadowsocksR, SOCKS5, HTTP, VMess, VLESS, Snell, Trojan, Hysteria 1/2, WireGuard, TUIC, ShadowQUIC, SSH, Mieru, AnyTLS, Sudoku, MASQUE, TrustTunnel, OpenVPN, Tailscale, Gost Relay |
| Built-in outbounds | `DIRECT`, `REJECT`, `DNS`, `REMATCH` |
| Proxy groups | `select`, `url-test`, `fallback`, `load-balance` |
| DNS | UDP/TCP DNS, DoH, DoT, DoQ, DHCP, fake-IP, fallback, nameserver policy, hosts, cache |
| Controller transports | HTTP, HTTPS, Unix socket, Windows named pipe |
| Routing inputs | Domain, suffix, keyword, regex, wildcard, GeoIP, GeoSite, IP/CIDR, IP suffix, ASN, process, UID, ports, DSCP, inbound, network type, rule sets, logical rules |

The full configuration surface is documented in [docs/config.yaml](docs/config.yaml). Mihomo-compatible options are also covered by the [Mihomo documentation](https://wiki.metacubex.one/).

## Quick start

### 1. Install

Download a binary or native package from [GitHub Releases](https://github.com/Miku0139oao/aster-core/releases), or build from source:

```sh
git clone https://github.com/Miku0139oao/aster-core.git
cd aster-core
go mod download
mkdir -p bin
CGO_ENABLED=0 go build -tags with_gvisor -trimpath -o bin/aster-core .
```

Go 1.20 or newer is required. Release builds use the `with_gvisor` tag; use the same tag when TUN or Tailscale support is required.

### 2. Create a minimal configuration

Create `config/config.yaml`:

```yaml
mixed-port: 7890
allow-lan: false
mode: rule
log-level: info

external-controller: 127.0.0.1:9090
secret: "replace-this-controller-secret"

dns:
  enable: true
  enhanced-mode: fake-ip
  nameserver:
    - system

rules:
  - MATCH,DIRECT
```

This configuration starts a local HTTP/SOCKS mixed proxy on `127.0.0.1:7890`, a Controller API on `127.0.0.1:9090`, and sends traffic directly. Add proxies, groups, and routing rules before using it as an actual proxy profile.

### 3. Validate and run

```sh
./bin/aster-core -d ./config -f ./config/config.yaml -t
./bin/aster-core -d ./config -f ./config/config.yaml
```

On Windows, use `.\bin\aster-core.exe`. Send `SIGHUP` on Unix to reload a file-backed configuration without stopping the process.

## Configuration

Without `-d` or `-f`, Aster Core uses the Mihomo-compatible paths below:

| Platform | Default configuration |
| --- | --- |
| Linux, macOS, and other Unix systems | `$HOME/.config/mihomo/config.yaml` |
| Windows | `%USERPROFILE%\.config\mihomo\config.yaml` |

If the default file does not exist, Aster Core creates it with `mixed-port: 7890`. Related assets, cache data, downloaded providers, and the default Aster state are stored under the same home directory.

Configuration can be supplied in four ways:

```sh
# Explicit file
aster-core -d /etc/mihomo -f /etc/mihomo/config.yaml

# Standard input
aster-core -d /etc/mihomo -f - < /etc/mihomo/config.yaml

# Base64-encoded YAML
aster-core --config '<base64-data>'

# Default path
aster-core
```

Relative `-d` and `-f` values are resolved from the current working directory. In particular, a relative `-f` path is not resolved from the directory passed to `-d`.

Age-armored configuration is supported through `--age-secret-key` or `CLASH_AGE_SECRET_KEY`. Generate keys and encrypt a configuration with the built-in `age` subcommand described below.

Paths referenced by configuration are restricted to the home directory by default. Additional trusted roots can be supplied through the `SAFE_PATHS` environment variable. `SKIP_SAFE_PATH_CHECK=true` disables this protection and should only be used in a controlled environment.

### Dashboard and Controller API

The standard Clash-compatible API exposes configuration, proxies, groups, rules, connections, providers, DNS, cache, storage, logs, traffic, memory, restart, and upgrade routes. Protect it with a non-empty Controller `secret`, and avoid exposing a plaintext Controller directly to an untrusted network.

[metacubexd](https://github.com/MetaCubeX/metacubexd) can be used as a web dashboard for the standard Controller API. Set `external-ui` to serve a dashboard from `/ui`.

The static `/ui` route, Aster subscription routes, and an optional Controller DoH route are outside the standard Controller authentication group. Treat each as a separately exposed service when designing firewall or reverse-proxy rules.

## Aster user management

Aster management is opt-in and currently supports named VLESS and AnyTLS entries under `listeners`. Managed user changes are persisted and applied live without recreating the listener.

### Example

```yaml
external-controller: 127.0.0.1:9090
secret: "replace-this-controller-secret"

listeners:
  - name: edge-vless
    type: vless
    listen: 0.0.0.0
    port: 8443
    users: []
    certificate: ./server.crt
    private-key: ./server.key

aster:
  # Independent from the Controller secret; minimum 32 bytes.
  secret: "replace-with-a-random-aster-secret-of-32-bytes"
  public-base-url: "https://proxy.example.com"
  store: "aster-state.json"
  managed-listeners:
    - edge-vless

rules:
  - MATCH,DIRECT
```

`public-base-url` must be an absolute HTTPS URL without user information, query parameters, or a fragment. Its hostname is used in generated proxy links, while each managed listener contributes its actual listening port. Publish `/sub/aster/*` through that HTTPS origin if subscriptions are required.

The Aster secret is not the Controller secret. Authenticate admin requests with:

```http
Authorization: Bearer <aster-secret>
```

For example:

```sh
curl \
  -H 'Authorization: Bearer replace-with-a-random-aster-secret-of-32-bytes' \
  http://127.0.0.1:9090/api/admin/inbounds
```

### API surface

| Method and path | Purpose |
| --- | --- |
| `GET /api/admin/overview` | Runtime, platform, traffic, connection, user, and inbound summary |
| `GET /api/admin/status` | Alias of the overview route |
| `GET /api/admin/protocols` | Managed protocol capabilities |
| `GET /api/admin/inbounds` | Managed listener state and revisions |
| `GET /api/admin/listeners` | Alias of the inbound route |
| `GET /api/admin/users?inbound=<name>` | List users, optionally filtered by inbound |
| `POST /api/admin/users` | Create a user |
| `GET /api/admin/users/{id}` | Read one user, including credentials and subscription URL |
| `PUT /api/admin/users/{id}` | Update credentials, flow, name, or enabled state |
| `DELETE /api/admin/users/{id}?revision=<revision>` | Delete a user |
| `POST /api/admin/users/{id}/reset-traffic` | Reset the user's traffic counters |
| `POST /api/admin/users/{id}/rotate-subscription` | Rotate the subscription token |
| `GET /sub/aster/{token}` | Return a Base64-encoded single-user proxy link |

Every mutation uses optimistic concurrency. Read the current listener `revision` first, then include it in the JSON request body; deletion supplies it as a query parameter. A stale revision returns HTTP `409 Conflict`.

### Security model and current limits

- The Aster secret must contain at least 32 bytes and cannot have leading or trailing whitespace.
- Admin routes enforce both Bearer authentication and same-origin checks.
- Admin request bodies are limited to 1 MiB.
- A plaintext TCP Controller mounts Aster admin routes only when bound to a loopback address. HTTPS, Unix socket, and Windows named-pipe Controllers may mount them normally.
- Subscription routes are token-authenticated and intentionally outside Controller and Aster Bearer authentication. Responses use `Cache-Control: no-store`.
- The state file contains credentials and subscription tokens in plaintext JSON. Keep the configuration directory private and include both `aster-state.json` and `aster-state.json.bak` in backups.
- Unix state directories must be owner-only and state files use mode `0600`; Windows applies equivalent ACL hardening.
- Generated subscription links support eligible VLESS and AnyTLS listeners. ShadowTLS, ResTLS, JLS, and advanced XHTTP options are not emitted as subscription links.
- Quotas and expiration policies are not implemented in the current API version.

## Deployment

### Docker

The published image contains the release binary and GeoIP/GeoSite data. On Linux, host networking is the simplest option for TUN, transparent proxying, and multi-protocol listeners:

```sh
docker run -d \
  --name aster-core \
  --restart unless-stopped \
  --network host \
  --cap-add NET_ADMIN \
  --device /dev/net/tun \
  -v "$PWD/config:/root/.config/mihomo" \
  miku0139oao/aster-core:latest
```

The mounted directory must contain `config.yaml`. Add only the privileges required by your configuration; ordinary HTTP/SOCKS use requires neither `NET_ADMIN` nor `/dev/net/tun`. Docker Desktop users should publish the configured TCP and UDP ports instead of relying on Linux host networking. When using `-p`, set `allow-lan: true` so the proxy listens beyond the container's loopback interface.

The repository Dockerfile packages prebuilt `bin/*.gz` release artifacts; it is not a source-compilation Dockerfile.

### Nix

Build the default package from the included flake:

```sh
nix build .#
./result/bin/aster-core -v
```

### Linux packages

Release automation produces supported `.deb`, `.rpm`, and `.pkg.tar.zst` packages with systemd units. The installed service runs:

```text
/usr/bin/aster-core -d /etc/mihomo
```

### OpenWrt and Nikki

The package recipe in [openwrt/aster-core](openwrt/aster-core) provides the virtual `mihomo` package and the `/usr/bin/mihomo` compatibility path expected by Nikki. See [openwrt/README.md](openwrt/README.md) for SDK builds, installation, alternatives behavior, and profile validation.

## Command-line reference

| Flag | Environment variable | Description |
| --- | --- | --- |
| `-d <dir>` | `CLASH_HOME_DIR` | Configuration and data directory |
| `-f <file>` | `CLASH_CONFIG_FILE` | Configuration file; use `-` for stdin |
| `--config <base64>` | `CLASH_CONFIG_STRING` | Base64-encoded configuration |
| `--age-secret-key <key>` | `CLASH_AGE_SECRET_KEY` | Decrypt age-armored configuration |
| `--ext-ui <dir>` | `CLASH_OVERRIDE_EXTERNAL_UI_DIR` | Override the external UI directory |
| `--ext-ctl <addr>` | `CLASH_OVERRIDE_EXTERNAL_CONTROLLER` | Override the HTTP Controller address |
| `--ext-ctl-tls <addr>` | `CLASH_OVERRIDE_EXTERNAL_CONTROLLER_TLS` | Override the HTTPS Controller address |
| `--ext-ctl-unix <path>` | `CLASH_OVERRIDE_EXTERNAL_CONTROLLER_UNIX` | Override the Unix socket path |
| `--ext-ctl-pipe <path>` | `CLASH_OVERRIDE_EXTERNAL_CONTROLLER_PIPE` | Override the Windows named pipe |
| `--ext-ctl-routing-mark <mark>` | `CLASH_OVERRIDE_EXTERNAL_CONTROLLER_ROUTING_MARK` | Linux routing mark for Controller sockets |
| `--secret <secret>` | `CLASH_OVERRIDE_SECRET` | Override the standard Controller secret |
| `--post-up <command>` | `CLASH_POST_UP` | Run a shell command after startup |
| `--post-down <command>` | `CLASH_POST_DOWN` | Run a shell command during shutdown |
| `-m` | — | Enable geodata mode |
| `-t` | — | Validate configuration and exit |
| `-v` | — | Print version, platform, Go version, build time, and build tags |

Built-in utility commands:

`--post-up` and `--post-down` execute their values through the system shell. Do not populate them from untrusted input.

```sh
# Credentials and protocol keys
aster-core generate uuid
aster-core generate reality-keypair
aster-core generate wg-keypair
aster-core generate ech-keypair example.com
aster-core generate vless-mlkem768
aster-core generate vless-x25519
aster-core generate sudoku-keypair

# Age configuration encryption
aster-core age keygen
aster-core age keygen-pq
aster-core age convert <secret-key>
aster-core age encrypt <public-key> <source> <target>
aster-core age decrypt <secret-key> <source> <target>

# Rule-set conversion
aster-core convert-ruleset <behavior> <format> <source> <target>
```

## Architecture

```mermaid
flowchart LR
    A[CLI / YAML / reload] --> B[config]
    B --> C[hub / executor]
    C --> D[listener inbounds]
    D --> E[tunnel]
    E --> F[sniffer / DNS / process lookup]
    F --> G[rules and providers]
    G --> H[proxy groups and outbound adapters]
    H --> I[transport implementations]
    C --> J[Controller API]
    C --> K[Aster manager and state]
    K --> D
    E --> K
```

During configuration application, the runtime suspends the tunnel, updates proxies, rules, DNS, listeners, Aster state, TUN, providers, and caches, then returns the tunnel to the running state.

| Path | Responsibility |
| --- | --- |
| `main.go` | CLI, configuration source selection, process lifecycle, and signals |
| `config/` | Defaults, YAML decoding, validation, and construction of runtime configuration |
| `hub/` | Configuration application and Controller server lifecycle |
| `listener/` | Local, transparent, TUN, and protocol-server inbounds |
| `tunnel/` | TCP/UDP data plane, metadata processing, rule matching, and connection tracking |
| `adapter/` | Outbound adapters, proxy groups, and proxy providers |
| `rules/` | Routing rules and rule providers |
| `dns/` | DNS server, clients, policies, cache, and fake-IP |
| `transport/` | Protocol, encryption, multiplexing, and obfuscation implementations |
| `component/aster/` | Managed users, subscriptions, traffic accounting, and persistent state |
| `component/` | Geodata, updater, resolver, sniffer, CA, dialer, profile, and platform services |
| `common/` | Shared collections, network wrappers, pools, synchronization, and utilities |
| `test/` | Docker-backed interoperability test suite |
| `openwrt/` | OpenWrt package recipe and Nikki integration |

## Development

### Build

```sh
# Fast local build
go build -o aster-core .

# Release-equivalent feature set
CGO_ENABLED=0 go build -tags with_gvisor -trimpath -o aster-core .

# Common release targets
make linux-amd64-v1
make linux-arm64
make windows-amd64-v1
make darwin-arm64
```

The Makefile writes platform binaries to `bin/` and injects version, build time, and release asset metadata.

Available feature tags include:

| Tag | Effect |
| --- | --- |
| `with_gvisor` | Include the gVisor TUN stack and full Tailscale integration |
| `with_low_memory` | Select lower-memory buffer behavior |
| `no_tailscale` | Exclude Tailscale support |
| `no_fake_tcp` | Exclude Hysteria fake-TCP support |
| `cmfa` | Build for the CMFA integration mode |

### Test and lint

```sh
SKIP_INTEROP_TEST=1 go test ./... -count=1
SKIP_INTEROP_TEST=1 go test ./... -count=1 -tags with_gvisor
CGO_ENABLED=1 SKIP_INTEROP_TEST=1 go test -race ./common/net ./component/aster ./hub/route ./listener/anytls ./listener/sing_vless ./tunnel/statistic
golangci-lint run --timeout=10m
```

The CI matrix covers Go 1.20 through 1.26 on Linux and Go 1.26 on Windows and macOS. It also runs gVisor builds, targeted race tests, `govet`, `staticcheck`, `gci`, and `gofumpt`. The pinned lint version is `golangci-lint v2.12.2`. Unset `SKIP_INTEROP_TEST` to run the VMess interoperability test, which downloads and builds an external V2Ray test binary.

The root command does not include the separate Go module under `test/`. That interoperability suite uses Docker:

```sh
cd test
make test
```

## Compatibility and operational notes

- Proxy groups with `type: relay` are not supported. Replace them with `dialer-proxy` chains.
- Release builds include `with_gvisor`; a plain `go build` does not provide the same TUN and Tailscale feature set.
- TProxy UDP, automatic iptables configuration, and socket routing marks are Linux-specific. Redir support is platform-dependent.
- Automatic iptables management and TUN mode cannot be enabled at the same time.
- TUN, TProxy, redir, low ports, and system routing changes may require root privileges or specific capabilities.
- `-v` intentionally retains the `Mihomo Meta` prefix because Nikki uses it for compatibility detection.
- `SIGHUP` re-reads file-backed configuration. Configurations supplied through stdin or Base64 reapply the original in-memory bytes.
- A configured Controller with an empty `secret` is unauthenticated. Bind it to loopback or set a strong secret.
- Aster admin routes use their own mandatory token even when the standard Controller is unauthenticated.
- Rule mode falls back to `DIRECT` when no rule matches, so production profiles should include an explicit final rule.
- Before production use, test TCP, UDP, DNS hijacking, IPv4, IPv6, and transparent routing separately on the target platform.

## License and credits

Aster Core is distributed under the [GNU General Public License v3.0](LICENSE). Existing upstream and third-party notices remain in effect.

Major upstream projects include:

- [MetaCubeX/mihomo](https://github.com/MetaCubeX/mihomo)
- [Dreamacro/clash](https://github.com/Dreamacro/clash)
- [SagerNet/sing-box](https://github.com/SagerNet/sing-box)
- [XTLS/Xray-core](https://github.com/XTLS/Xray-core)
- [WireGuard/wireguard-go](https://github.com/WireGuard/wireguard-go)
- [riobard/go-shadowsocks2](https://github.com/riobard/go-shadowsocks2)
