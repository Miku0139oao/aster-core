# 設定總覽

## 最小骨架

```yaml
mixed-port: 7890
allow-lan: false
mode: rule
log-level: info

dns:
  enable: true
  enhanced-mode: fake-ip
  nameserver:
    - system

proxies: []
proxy-groups: []
rules:
  - MATCH,DIRECT
```

完整帶註解範例：[`config.yaml`](/config.yaml)。

## General

| 欄位 | 用途 |
| --- | --- |
| `port` | HTTP proxy port |
| `socks-port` | SOCKS5 port |
| `mixed-port` | HTTP/SOCKS 共用 port |
| `redir-port` | Redir transparent proxy port |
| `tproxy-port` | Linux TProxy TCP/UDP port |
| `allow-lan` | 是否允許非 loopback client |
| `bind-address` | LAN listener bind address |
| `mode` | `rule`、`global` 或 `direct` |
| `log-level` | `silent`、`error`、`warning`、`info`、`debug` |
| `ipv6` | 啟用 IPv6 resolution/routing |
| `interface-name` | 預設出站 interface |
| `routing-mark` | Linux socket mark |
| `find-process-mode` | `always`、`strict`、`off` |
| `tcp-concurrent` | 對解析出的 IP 並行建立 TCP |
| `unified-delay` | 統一 delay test 計算 |

### Mode

- `rule`：依序比對 `rules`。
- `global`：所有流量交給 `GLOBAL` group。
- `direct`：所有流量直接送出。

Rule mode 沒有任何規則命中時會回落 `DIRECT`，仍建議明確加入：

```yaml
rules:
  - MATCH,FINAL
```

並確保 `FINAL` 是存在的 proxy/group 名稱。

## Controller

```yaml
external-controller: 127.0.0.1:9090
external-controller-tls: 127.0.0.1:9443
external-controller-unix: mihomo.sock
external-controller-pipe: \\.\pipe\mihomo
secret: "replace-with-a-strong-secret"
```

| 欄位 | 說明 |
| --- | --- |
| `external-controller` | HTTP Controller |
| `external-controller-tls` | HTTPS Controller，使用 `tls` 憑證 |
| `external-controller-unix` | Unix socket；Windows 新版也可能支援 |
| `external-controller-pipe` | Windows named pipe |
| `external-controller-routing-mark` | Linux listener routing mark |
| `external-controller-cors` | Origin 與 private-network CORS |
| `external-ui` | UI directory |
| `external-ui-url` | UI archive URL |
| `external-doh-server` | Controller 上的 DoH path |

非空白 `secret` 會保護一般 Controller API。靜態 `/ui`、Aster subscription 與可選 DoH route 不在同一 authentication group。

## TLS

Controller TLS 與部分共用憑證設定使用：

```yaml
tls:
  certificate: ./server.crt
  private-key: ./server.key
  client-auth-type: ""
  client-auth-cert: ""
```

Certificate 可是 PEM 內容或安全路徑內的檔案。啟用 mTLS verification 時必須提供 client CA/cert。

## Proxies 與 groups

```yaml
proxies:
  - name: edge
    type: vless
    server: proxy.example.com
    port: 443
    uuid: 00000000-0000-0000-0000-000000000000
    tls: true
    servername: proxy.example.com

proxy-groups:
  - name: PROXY
    type: select
    proxies:
      - edge
      - DIRECT
```

詳細欄位見[出站與代理群組](/reference/outbounds)。

## Providers

```yaml
proxy-providers:
  remote:
    type: http
    url: https://example.com/subscription.yaml
    path: ./providers/remote.yaml
    interval: 3600
    health-check:
      enable: true
      url: https://www.gstatic.com/generate_204
      interval: 300
```

Provider path 同樣受 safe-path 檢查。遠端內容應視為不可信設定，限制來源並使用 HTTPS。

## Listeners

`listeners` 用於額外 server inbound：

```yaml
listeners:
  - name: local-socks
    type: socks
    listen: 127.0.0.1
    port: 1080
```

每個 listener 必須有唯一 `name`。詳情見[入站參考](/reference/inbounds)。

## Aster

```yaml
aster:
  secret: "replace-with-at-least-32-random-bytes"
  public-base-url: https://proxy.example.com
  store: aster-state.json
  managed-listeners:
    - edge-vless
```

只要存在 `aster` block 就會啟用 Aster manager。完整說明見[Aster 管理概覽](/aster/overview)。

## TUN

```yaml
tun:
  enable: true
  stack: gvisor
  auto-route: true
  auto-detect-interface: true
  dns-hijack:
    - any:53
```

Release build 已包含 `with_gvisor`。TUN 的 route、auto-redirect、UID/package include/exclude 及平台差異很多，請先在目標平台用最小設定驗證。

## Profile 與 cache

Profile cache 可保存 selected proxy、fake-IP 與 provider subscription information。預設 cache file 位於 home directory 的 `cache.db`。

不要把 Aster state 與 profile cache 混為一談：

- `cache.db`：Mihomo runtime/profile cache。
- `aster-state.json`：Aster managed users、traffic、revision 與 subscriptions。
