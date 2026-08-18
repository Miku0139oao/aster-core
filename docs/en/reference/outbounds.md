# Outbounds and groups

## Supported outbounds

| `type` | Protocol |
| --- | --- |
| `ss` | Shadowsocks |
| `ssr` | ShadowsocksR |
| `socks5` | SOCKS5 upstream |
| `http` | HTTP CONNECT upstream |
| `vmess` | VMess |
| `vless` | VLESS |
| `snell` | Snell |
| `trojan` | Trojan |
| `hysteria` | Hysteria 1 |
| `hysteria2` | Hysteria 2 |
| `wireguard` | WireGuard |
| `tuic` | TUIC |
| `shadowquic` | ShadowQUIC |
| `gost-relay` | Gost relay |
| `ssh` | SSH |
| `mieru` | Mieru |
| `anytls` | AnyTLS |
| `sudoku` | Sudoku |
| `masque` | MASQUE |
| `trusttunnel` | TrustTunnel |
| `openvpn` | OpenVPN |
| `tailscale` | Tailscale |
| `direct` | Direct with options |
| `dns` | Send into internal DNS |
| `reject` | Reject |
| `rematch` | Match again |

## Shared fields

Most remote proxies have:

```yaml
- name: edge
  type: <type>
  server: proxy.example.com
  port: 443
  udp: true
  interface-name: eth0
  routing-mark: 6666
  dialer-proxy: upstream-hop
```

`dialer-proxy` builds a chain. The old `relay` proxy group was removed.

## VLESS

```yaml
proxies:
  - name: edge-vless
    type: vless
    server: proxy.example.com
    port: 443
    uuid: 00000000-0000-0000-0000-000000000000
    udp: true
    tls: true
    servername: proxy.example.com
    client-fingerprint: chrome
    network: tcp
```

Common networks:

- `tcp`
- `ws`
- `grpc`
- `xhttp`

### XHTTP

```yaml
network: xhttp
xhttp-opts:
  path: /xhttp
  mode: auto
```

Supports `auto`, `stream-one`, `stream-up`, `packet-up`, plus HTTP/1.1, HTTP/2, HTTP/3, XMUX reuse, split download, and advanced placement/padding.

### VLESS + REALITY

```yaml
tls: true
servername: www.example.com
reality-opts:
  public-key: <server-public-key>
  short-id: 0123456789abcdef
client-fingerprint: chrome
```

## AnyTLS + REALITY

```yaml
proxies:
  - name: edge-anytls-reality
    type: anytls
    server: proxy.example.com
    port: 443
    password: replace-with-a-long-random-password
    sni: www.microsoft.com
    client-fingerprint: chrome
    reality-opts:
      public-key: <server-public-key>
      short-id: 0123456789abcdef
    udp: true
```

This is a security mode Aster added on top of the Mihomo `v1.19.29` AnyTLS outbound. `public-key` must be the public key of the server REALITY key pair. `sni` and `short-id` must match the listener. REALITY uses uTLS, so set `client-fingerprint` explicitly.

`reality-opts` cannot be used together with `shadow-tls-opts`, `restls-opts`, or `jls-opts`. Full notes are in [AnyTLS + REALITY](/en/reference/anytls-reality).

## Proxy groups

### Select

```yaml
proxy-groups:
  - name: PROXY
    type: select
    proxies:
      - edge-a
      - edge-b
      - DIRECT
```

### URL test

```yaml
proxy-groups:
  - name: AUTO
    type: url-test
    proxies: [edge-a, edge-b]
    url: https://www.gstatic.com/generate_204
    interval: 300
    tolerance: 50
```

### Fallback

Pick the first healthy proxy:

```yaml
proxy-groups:
  - name: FALLBACK
    type: fallback
    proxies: [edge-a, edge-b]
    url: https://www.gstatic.com/generate_204
    interval: 300
```

### Load balance

```yaml
proxy-groups:
  - name: BALANCE
    type: load-balance
    strategy: consistent-hashing
    proxies: [edge-a, edge-b]
```

### Relay is not supported

```yaml
# Not available
type: relay
```

Use this instead:

```yaml
proxies:
  - name: hop-2
    type: vless
    dialer-proxy: hop-1
```

## Proxy providers

```yaml
proxy-providers:
  remote:
    type: http
    url: https://example.com/nodes.yaml
    path: ./providers/remote.yaml
    interval: 3600
    override:
      udp: true
    health-check:
      enable: true
      url: https://www.gstatic.com/generate_204
      interval: 300
```

Use them in a group:

```yaml
proxy-groups:
  - name: PROVIDER-AUTO
    type: url-test
    use:
      - remote
```

Providers also support filter, exclude-filter, exclude-type, expected-status, and similar fields. Regex/filter on a large provider can affect load time. Test with representative data first.

## Build-tag notes

Full Tailscale support depends on `with_gvisor`, which official releases already include. A plain `go build` may use a stub or miss the complete outbound.
