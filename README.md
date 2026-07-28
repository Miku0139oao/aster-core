<h1 align="center">Aster Core</h1>

<h3 align="center">A Mihomo-based universal proxy core.</h3>

<p align="center">
  <a href="https://goreportcard.com/report/github.com/Miku0139oao/aster-core">
    <img src="https://goreportcard.com/badge/github.com/Miku0139oao/aster-core?style=flat-square">
  </a>
  <img src="https://img.shields.io/github/go-mod/go-version/Miku0139oao/aster-core/main?style=flat-square">
  <a href="https://github.com/Miku0139oao/aster-core/releases">
    <img src="https://img.shields.io/github/release/Miku0139oao/aster-core/all.svg?style=flat-square">
  </a>
  <a href="https://github.com/Miku0139oao/aster-core">
    <img src="https://img.shields.io/badge/core-Aster-5b5bd6?style=flat-square">
  </a>
</p>

## Features

- Local HTTP/HTTPS/SOCKS server with authentication support
- VMess, VLESS (including XHTTP), Shadowsocks, Trojan, Snell, TUIC, Hysteria protocol support
- Built-in DNS server that aims to minimize DNS pollution attack impact, supports DoH/DoT upstream and fake IP.
- Rules based off domains, GEOIP, IPCIDR or Process to forward packets to different nodes
- Remote groups allow users to implement powerful rules. Supports automatic fallback, load balancing or auto select node
  based off latency
- Remote providers, allowing users to get node lists remotely instead of hard-coding in config
- Netfilter TCP redirecting. Deploy Aster Core on your Internet gateway with `iptables`.
- Comprehensive HTTP RESTful API controller
- Live VLESS and AnyTLS user management with per-principal traffic accounting
- AnyTLS over REALITY in both inbound and outbound directions

## Aster management

Management is opt-in for existing VLESS and AnyTLS listeners:

```yaml
aster:
  secret: "replace-with-at-least-32-random-bytes"
  public-base-url: "https://proxy.example.com"
  store: "aster-state.json"
  managed-listeners:
    - edge-vless
    - edge-anytls
```

The Aster Bearer token is independent of the Clash controller token and must contain at least 32 bytes. Admin routes are under `/api/admin`; plaintext TCP controllers expose them only when bound to loopback, while TLS and local socket controllers may expose them normally. User mutations require the current listener `revision` and are applied without recreating the listener.

Subscription URLs use `/sub/aster/{token}`. `public-base-url` must be an absolute HTTPS URL; its hostname is also advertised as the proxy hostname, with each managed listener's actual port.

## Dashboard

A web dashboard with first-class support for this project has been created; it can be checked out at [metacubexd](https://github.com/MetaCubeX/metacubexd).

## Configration example

Configuration example is located at [`docs/config.yaml`](docs/config.yaml).

## Docs

Mihomo-compatible configuration documentation can be found in the [Mihomo docs](https://wiki.metacubex.one/).

## OpenWrt and Nikki

An OpenWrt package recipe that provides the virtual `mihomo` package and the
`/usr/bin/mihomo` compatibility path is available under [`openwrt/aster-core`](openwrt/aster-core).
See [`openwrt/README.md`](openwrt/README.md) for build, installation, and
profile validation instructions. Nikki itself does not need to be patched.

## For development

Requirements:
[Go 1.20 or newer](https://go.dev/dl/)

Build Aster Core:

```shell
git clone https://github.com/Miku0139oao/aster-core.git
cd aster-core && go mod download
go build -o aster-core
```

Set go proxy if a connection to GitHub is not possible:

```shell
go env -w GOPROXY=https://goproxy.io,direct
```

Build with gvisor tun stack:

```shell
go build -tags with_gvisor
```

### IPTABLES configuration

Work on Linux OS which supported `iptables`

```yaml
# Enable the TPROXY listener
tproxy-port: 9898

iptables:
  enable: true # default is false
  inbound-interface: eth0 # detect the inbound interface, default is 'lo'
```

## Debugging

Check [wiki](https://wiki.metacubex.one/api/#debug) to get an instruction on using debug
API.

## Credits

- [Dreamacro/clash](https://github.com/Dreamacro/clash)
- [SagerNet/sing-box](https://github.com/SagerNet/sing-box)
- [riobard/go-shadowsocks2](https://github.com/riobard/go-shadowsocks2)
- [v2ray/v2ray-core](https://github.com/v2ray/v2ray-core)
- [WireGuard/wireguard-go](https://github.com/WireGuard/wireguard-go)
- [yaling888/clash-plus-pro](https://github.com/yaling888/clash)

## License

This software is released under the GPL-3.0 license. See [`NOTICE.md`](NOTICE.md) and [`UPSTREAM.md`](UPSTREAM.md) for provenance.
