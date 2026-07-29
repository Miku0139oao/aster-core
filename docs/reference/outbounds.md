# 出站與代理群組

## 支援的出站

| `type` | 協定 |
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
| `dns` | 送入內部 DNS |
| `reject` | 拒絕 |
| `rematch` | 重新比對 |

## 共用欄位

多數遠端 proxy 具有：

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

`dialer-proxy` 用來建立 chain。舊 `relay` proxy group 已移除。

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

常用 network：

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

支援 `auto`、`stream-one`、`stream-up`、`packet-up`，以及 HTTP/1.1、HTTP/2、HTTP/3、XMUX reuse、split download 與進階 placement/padding。

### REALITY

```yaml
tls: true
servername: www.example.com
reality-opts:
  public-key: <server-public-key>
  short-id: 0123456789abcdef
client-fingerprint: chrome
```

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

依健康狀態選擇第一個可用 proxy：

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

### 不支援 Relay

```yaml
# 不可用
type: relay
```

改用：

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

在 group 中使用：

```yaml
proxy-groups:
  - name: PROVIDER-AUTO
    type: url-test
    use:
      - remote
```

Provider 可使用 filter、exclude-filter、exclude-type、expected-status 等欄位。Regex/filter 在大型 provider 上可能影響載入時間，應先用代表性資料測試。

## Build tag 注意

Tailscale 完整支援依賴 `with_gvisor`，正式 release 已包含。若用一般 `go build`，相關 outbound 可能使用 stub 或缺少完整功能。
