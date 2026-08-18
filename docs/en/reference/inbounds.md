# Inbounds

## Two inbound styles

### General ports

The most common local and transparent proxies can be set directly:

```yaml
port: 7890
socks-port: 7891
mixed-port: 7892
redir-port: 7893
tproxy-port: 7894
```

These listeners are managed by general config and have no custom names.

### Named listeners

`listeners` can create multiple named servers:

```yaml
listeners:
  - name: office-socks
    type: socks
    listen: 127.0.0.1
    port: 1080
```

Only named listeners can be targeted by inbound rules or a special proxy/rule. Aster also only manages named listeners.

## Shared fields

| Field | Meaning |
| --- | --- |
| `name` | Unique name |
| `type` | Listener type |
| `listen` | Bind IP, default `0.0.0.0` |
| `port` | A single port, comma list, or range |
| `routing-mark` | Linux socket mark |
| `rule` | Use a sub-rule |
| `proxy` | Skip ordinary rules and send traffic to a specified proxy |

Port forms:

```yaml
port: 443
port: 200,302
port: 401-429,501-503
```

## Supported types

| Type | Purpose | Main transport |
| --- | --- | --- |
| `socks` | SOCKS server | TCP/UDP |
| `http` | HTTP CONNECT proxy | TCP |
| `mixed` | HTTP + SOCKS | TCP/UDP |
| `tunnel` | Fixed-target forward | TCP/UDP |
| `tun` | Named TUN | IP |
| `redir` | NAT redirect | TCP |
| `tproxy` | Transparent proxy | TCP/UDP |
| `shadowsocks` | Shadowsocks server | TCP/UDP |
| `snell` | Snell server | TCP/UDP |
| `vmess` | VMess server | Multiple transports |
| `vless` | VLESS server | TCP/WS/gRPC/XHTTP |
| `trojan` | Trojan server | TCP/WS/gRPC |
| `hysteria2` | Hysteria 2 server | QUIC |
| `hysteria2-realm` | Realm server | QUIC |
| `tuic` | TUIC server | QUIC |
| `shadowquic` | ShadowQUIC server | QUIC |
| `anytls` | AnyTLS server | TLS/REALITY and others |
| `mieru` | Mieru server | TCP/UDP |
| `sudoku` | Sudoku server | TCP |
| `trusttunnel` | TrustTunnel server | QUIC |

## VLESS listener

```yaml
listeners:
  - name: edge-vless
    type: vless
    listen: 0.0.0.0
    port: 8443
    users:
      - username: alice
        uuid: 00000000-0000-0000-0000-000000000000
        flow: xtls-rprx-vision
    certificate: ./server.crt
    private-key: ./server.key
```

Transport options:

- `ws-path`
- `grpc-service-name`
- `xhttp-config`
- `mux-option`

Security options:

- `certificate` + `private-key`
- `reality-config`
- `shadow-tls`
- `res-tls`
- `jls-config`
- VLESS `decryption`

If `allow-insecure` is not `true`, at least one valid security/decryption must be configured.

### REALITY

```yaml
reality-config:
  dest: www.example.com:443
  private-key: <server-private-key>
  short-id:
    - 0123456789abcdef
  server-names:
    - www.example.com
```

Generate a key:

```sh
aster-core generate reality-keypair
```

## AnyTLS listener

Certificate TLS:

```yaml
listeners:
  - name: edge-anytls
    type: anytls
    listen: 0.0.0.0
    port: 9443
    users:
      alice: replace-with-a-random-password
    certificate: ./server.crt
    private-key: ./server.key
```

### AnyTLS + REALITY

```yaml
listeners:
  - name: edge-anytls-reality
    type: anytls
    listen: 0.0.0.0
    port: 443
    users:
      alice: replace-with-a-long-random-password
    reality-config:
      dest: www.microsoft.com:443
      private-key: <server-private-key>
      short-id:
        - 0123456789abcdef
      server-names:
        - www.microsoft.com
```

AnyTLS + REALITY listeners are an Aster addition relative to Mihomo `v1.19.29`. AnyTLS supports certificate/private key, REALITY, ShadowTLS, ResTLS, JLS, and a padding scheme. Those security modes are mutually exclusive. If no security is enabled and `allow-insecure` is not `true`, configuration validation fails.

For complete server/client pairing, share links, and deployment checks, see [AnyTLS + REALITY](/en/reference/anytls-reality).

## Aster managed listener

To let Aster manage VLESS or AnyTLS:

```yaml
aster:
  secret: "replace-with-at-least-32-random-bytes"
  managed-listeners:
    - edge-vless
    - edge-anytls
```

At startup, YAML users are taken into Aster state. Later API changes are persisted by the state store. If a managed listener name is missing, duplicated, or an unsupported type, the whole configuration fails validation.

## TUN, Redir, TProxy

| Type | Platform notes |
| --- | --- |
| TUN | Needs device and route privileges; implementations differ by platform |
| Redir | Linux, macOS, and FreeBSD support differ |
| TProxy TCP | Mainly Linux |
| TProxy UDP | Linux |
| Automatic iptables | Linux; cannot be enabled together with TUN |

Always test TCP, UDP, DNS hijack, IPv4, IPv6, and local traffic loops separately for transparent proxy.
