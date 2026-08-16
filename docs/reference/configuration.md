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
  auto-redirect: true
  kernel-direct: true
  kernel-direct-max-entries: 4096
  # Experimental; keep disabled until a same-server A/B test proves a benefit.
  kernel-direct-ebpf: false
  kernel-direct-ebpf-interfaces:
    - br-lan
  kernel-direct-ebpf-mark: 1073741824 # 0x40000000
  kernel-direct-ebpf-max-entries: 65536
  kernel-direct-ebpf-proxy: false
  kernel-direct-ebpf-proxy-redirect: false
  kernel-direct-ebpf-proxy-mark: 536870912 # 0x20000000
  kernel-direct-ebpf-flow-entries: 65536
  kernel-direct-ebpf-direct-prefixes: []
  kernel-direct-ebpf-proxy-prefixes: []
  auto-detect-interface: true
  dns-hijack:
    - any:53
```

Release build 已包含 `with_gvisor`。TUN 的 route、auto-redirect、UID/package include/exclude 及平台差異很多，請先在目標平台用最小設定驗證。

`kernel-direct` 是 Linux/OpenWrt 專用的 kernel forwarding 模式。Aster 觀察自己處理的真實 A/AAAA 回應，使用目前路由規則做保守分類，將可由目的網域/IP 單獨確定為 `DIRECT` 的位址放進 nftables auto-redirect exclude set；之後的新連線留在 Linux forwarding/NAT path，不再建立 Aster `DIRECT` socket。它要求 `auto-route: true`、`auto-redirect: true`、可用的 nftables，以及 client DNS 經過 Aster。共享 IP 出現任何代理判定時採 proxy-wins；source/process/inbound/port 等無法以目的 IP 等價表達的規則不會 bypass。規則、proxy、mode 或 provider 更新會立即清空 learned set，等待新的 DNS 回應重新學習。`backend: nftables` 是此功能的推薦正常狀態，不是降級錯誤。

fake-IP 回應不會加入 kernel set；因此要取得完整收益，建議使用 redir-host/mapping DNS，或把希望 kernel-direct 的網域放入 `fake-ip-filter`。繞過流量不會出現在 Aster connection/traffic 統計中。

`kernel-direct-ebpf` 是預設關閉的實驗性 TC classifier，會在列出的 Linux interface ingress 對每個封包執行。若列出的是 Linux bridge（OpenWrt 通常為 `br-lan`），Aster 會自動解析並掛到目前的 bridge ports（例如 `eth0`/WLAN）。IPv4 與 IPv6 使用 LPM trie，因此 `/0`、網段與 DNS 學到的 `/32`／`/128` 可以按 longest-prefix 決策；相同 prefix 仍採 proxy-wins。它可能使 OpenWrt software/hardware flow offload 無法生效，不能因為使用 eBPF 就假定吞吐量較高。

開啟 `kernel-direct-ebpf-proxy` 後，安全的全域位址先以 PROXY `/0` 作保守 fallback，已確認的 DIRECT prefix 再以較長 prefix 覆蓋。每個 TCP／UDP／ICMP flow 的 family、protocol、來源/目的位址與 port 會存入 LRU 5-tuple cache。TC 命中 DIRECT 時由 nft mark-return 回 Linux forwarding。未開啟 `kernel-direct-ebpf-proxy-redirect` 時，PROXY 仍使用相容的 nftables TCP redirect／UDP-ICMP mark shim。

開啟 `kernel-direct-ebpf-proxy-redirect` 後，IPv4 與 IPv6 的 PROXY TCP／UDP／ICMP 封包會由 TC eBPF 直接 `bpf_redirect()` 至 Aster TUN，不再建立兩條 PROXY nftables shim。sing-tun 原本的 auto-redirect 規則仍完整保留：generation 更新期間、DNS、fragment/extension header、無法分類的封包，或 TC 被 fail-open 卸載後，都會繼續走原路徑。Aster 也會把啟動時所有本機 IPv4 `/32` 與 IPv6 `/128` 位址放入最高優先 bypass，避免回程或送往路由器本身的封包被 PROXY `/0` 再導回 TUN。

Map 更新先發布奇數 generation，使 classifier 暫時 fail-open，再同步 IPv4/IPv6 LPM，最後用偶數 generation 原子啟用；舊 flow cache entry 因 generation 不符會立即失效。TCP/UDP 53 不會被標記，確保 client DNS 仍先經過 sing-tun DNS hijack；private、loopback、link-local、multicast、TUN fake-IP、IPv4 fragments、IPv6 extension headers、載入/同步失敗、非 Ethernet/單層 VLAN 封包及路由器本機輸出都使用原 nftables fallback。任何 map 同步錯誤會先留下奇數 generation 並卸載 TC filter，再關閉 eBPF backend。

- `kernel-direct-ebpf-required: true`：TC、BPF map 或 nft mark rule 任一建立失敗時拒絕啟動 TUN；預設為 `false`，失敗會記錄警告並自動降級為 nftables。
- `kernel-direct-ebpf-interfaces`：必填，OpenWrt 通常填 `br-lan`；多個 LAN/guest bridge 應全部列出。狀態 API 的 `requested-interfaces` 是設定值，`interfaces` 是實際掛載的 bridge ports。
- `kernel-direct-ebpf-mark`：預設 `0x40000000`。Aster 使用 bit mask 比對，不覆寫其他 mark bits。
- `kernel-direct-max-entries`：learned address set 容量上限，預設 4096，最大 65536；`0` 代表使用預設值，YAML 解析與 `PATCH /configs` 都會把 `0` 寫成 4096，超過上限則拒絕（PATCH 回 400）。
- `kernel-direct-ebpf-max-entries`：IPv4/IPv6 LPM prefix 總安全上限，預設 65536。
- `kernel-direct-ebpf-proxy`：啟用雙向 DIRECT/PROXY steering 與安全的 PROXY `/0` fallback；預設關閉。
- `kernel-direct-ebpf-proxy-redirect`：把 PROXY 決策由 TC 直接送入目前的 Aster TUN，移除 PROXY nftables shim；需要 `kernel-direct-ebpf-proxy`，預設關閉。
- `kernel-direct-ebpf-proxy-mark`：PROXY classifier bit，預設 `0x20000000`，不可與 DIRECT 或 auto-redirect marks 重疊。
- `kernel-direct-ebpf-flow-entries`：5-tuple LRU 容量，預設 65536。
- `kernel-direct-ebpf-direct-prefixes`／`kernel-direct-ebpf-proxy-prefixes`：可選靜態 CIDR；longest-prefix 優先，相同 prefix 時 PROXY 優先。

狀態可從 `GET /api/aster/kernel-direct/status` 取得；未啟用 TC 時 backend 為推薦的 `nftables`，相容 mark 模式為 `ebpf-tc-lpm-lru`，TC 直接導入 TUN 時為 `ebpf-tc-lpm-lru-redirect`。`packets`／`bytes` 等同 DIRECT 計數，PROXY 使用獨立的 `proxy-packets`／`proxy-bytes`；狀態也會回傳 redirect interface、direct/proxy/bypass prefix、flow hits、LRU 容量與最後同步錯誤。

同一個回應還包含 `learned_sets`、`process`、`aster_traffic` 與相容欄位 `proxy_traffic`：

- `learned_sets`：各 kernel-direct consumer 的 snapshot，含 `max_entries`、`max_records`（domain budget，通常為 `max_entries × 4`）、`learned_addresses`、`direct_addresses`、`proxy_addresses`、`learned_domains`、`evictions`。
- `process`：controller 行程 `pid` 與 `started_at`（Unix 秒）。
- `aster_traffic`：目前所有由 Aster 處理的流量估計，包含 TUN DIRECT／default-tun fallback，不含已被 kernel-direct 繞過的 DIRECT。
- `proxy_traffic`：已棄用別名，內容與 `aster_traffic` 完全相同，不是「僅代理流量」。`GET /api/aster/capabilities` 的 `kernel_direct.deprecated_fields` 會列出此欄位。
- `learned_sets[].evictions`：行程啟動以來 address LRU 因容量上限淘汰的次數；TTL 到期、規則 reload flush 或 set collapse 不計入。

實機吞吐量必須以同一 server A/B。曾有一台 OpenWrt 路由器在 TC eBPF 開啟時約 692 Mbps，卸載後約 1,647 Mbps，持久關閉重啟後約 1,644 Mbps；原因是逐封包 TC 工作與 flow offload 互動，而不是「eBPF 天生較慢」。詳見 [OpenWrt 與 Nikki](/deployment/openwrt)及 [效能優化與基準](/reference/performance)。

## Profile 與 cache

Profile cache 可保存 selected proxy、fake-IP 與 provider subscription information。預設 cache file 位於 home directory 的 `cache.db`。

不要把 Aster state 與 profile cache 混為一談：

- `cache.db`：Mihomo runtime/profile cache。
- `aster-state.json`：Aster managed users、traffic、revision 與 subscriptions。
