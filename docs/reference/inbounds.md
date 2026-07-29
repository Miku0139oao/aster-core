# 入站

## 兩種入站設定

### General ports

最常用的本機與透明代理可直接設定：

```yaml
port: 7890
socks-port: 7891
mixed-port: 7892
redir-port: 7893
tproxy-port: 7894
```

這些 listener 由 general config 管理，沒有自訂名稱。

### Named listeners

`listeners` 可建立多個具名 server：

```yaml
listeners:
  - name: office-socks
    type: socks
    listen: 127.0.0.1
    port: 1080
```

具名 listener 才能被 inbound rules、特殊 proxy/rule 指向，Aster 也只管理具名 listener。

## 共用欄位

| 欄位 | 說明 |
| --- | --- |
| `name` | 唯一名稱 |
| `type` | Listener type |
| `listen` | Bind IP，預設 `0.0.0.0` |
| `port` | 單一 port、逗號清單或 range |
| `routing-mark` | Linux socket mark |
| `rule` | 指定 sub-rule |
| `proxy` | 跳過一般規則，直接交給指定 proxy |

Port 支援：

```yaml
port: 443
port: 200,302
port: 401-429,501-503
```

## 支援類型

| Type | 用途 | 主要 transport |
| --- | --- | --- |
| `socks` | SOCKS server | TCP/UDP |
| `http` | HTTP CONNECT proxy | TCP |
| `mixed` | HTTP + SOCKS | TCP/UDP |
| `tunnel` | 固定目標 forward | TCP/UDP |
| `tun` | 具名 TUN | IP |
| `redir` | NAT redirect | TCP |
| `tproxy` | Transparent proxy | TCP/UDP |
| `shadowsocks` | Shadowsocks server | TCP/UDP |
| `snell` | Snell server | TCP/UDP |
| `vmess` | VMess server | 多 transport |
| `vless` | VLESS server | TCP/WS/gRPC/XHTTP |
| `trojan` | Trojan server | TCP/WS/gRPC |
| `hysteria2` | Hysteria 2 server | QUIC |
| `hysteria2-realm` | Realm server | QUIC |
| `tuic` | TUIC server | QUIC |
| `shadowquic` | ShadowQUIC server | QUIC |
| `anytls` | AnyTLS server | TLS/REALITY 等 |
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

Transport 選項：

- `ws-path`
- `grpc-service-name`
- `xhttp-config`
- `mux-option`

Security 選項：

- `certificate` + `private-key`
- `reality-config`
- `shadow-tls`
- `res-tls`
- `jls-config`
- VLESS `decryption`

若 `allow-insecure` 不是 `true`，必須配置至少一種有效 security/decryption。

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

產生 key：

```sh
aster-core generate reality-keypair
```

## AnyTLS listener

憑證 TLS：

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

AnyTLS + REALITY listener 是 Aster 相對 Mihomo `v1.19.29` 新增的能力。AnyTLS 支援 certificate/private key、REALITY、ShadowTLS、ResTLS、JLS 與 padding scheme；這些 security mode 互斥。未啟用任何 security 且 `allow-insecure` 不是 `true` 時，設定驗證會失敗。

完整的 server/client 配對、分享連結與部署檢查請參閱 [AnyTLS + REALITY](/reference/anytls-reality)。

## Aster managed listener

要讓 VLESS 或 AnyTLS 由 Aster 管理：

```yaml
aster:
  secret: "replace-with-at-least-32-random-bytes"
  managed-listeners:
    - edge-vless
    - edge-anytls
```

啟動時 YAML users 會納入 Aster state；之後 API 變更由 state store 持久化。管理 listener 名稱不存在、重複或 type 不支援時，整份設定驗證會失敗。

## TUN、Redir、TProxy

| 類型 | 平台重點 |
| --- | --- |
| TUN | 需要裝置與 route 權限；各平台實作不同 |
| Redir | Linux、macOS、FreeBSD 支援程度不同 |
| TProxy TCP | 主要 Linux |
| TProxy UDP | Linux |
| 自動 iptables | Linux，不能與 TUN 同時啟用 |

透明代理務必分開測試 TCP、UDP、DNS hijack、IPv4、IPv6 與 local traffic loop。
