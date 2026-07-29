# 分流與 DNS 實戰：fake-IP、policy、rule providers 與防泄漏

這篇教學在一份完整設定中組合：

- fake-IP DNS。
- `nameserver-policy`。
- `domain`、`ipcidr`、`classical` 三種 rule provider。
- 由精確例外到最終 `MATCH` 的 rules。
- Controller 的 DNS 查詢、rule hit counter 與 provider 狀態。
- DNS、IPv6、UDP 與 TUN 常見泄漏排查。

建議先完成[第一個代理設定](/tutorials/first-proxy)，確認單一 proxy 可以連線，再加入複雜分流。否則「節點壞了」與「規則／DNS 錯了」會很難區分。

## 目標

完成後的流量方向如下：

| 類型 | 結果 |
| --- | --- |
| 廣告／追蹤測試網域 | `REJECT` |
| LAN／內部網域與 RFC 1918 位址 | `DIRECT` |
| `example.com` | `DIRECT`，作為可重現的規則測試 |
| `ipify.org` | `PROXY`，作為可重現的代理測試 |
| 其他流量 | `PROXY` |
| 一般 DNS 名稱 | 回傳 fake-IP，再由 Aster 還原網域 |
| 私有、mDNS、時間同步名稱 | 不使用 fake-IP |

## 前置條件

需要：

- 可正常啟動的 Aster Core。
- 一個已確認可用的 proxy。本篇完整 YAML 使用 AnyTLS + REALITY，所有 `<...>` 都必須換成你的資料。
- 未被其他程式占用的 `127.0.0.1:7890`、`:1053` 與 `:9090`。
- 選用：`dig`、`jq` 與 `tcpdump`，方便觀察結果。

::: warning 先在 loopback 測試
本文讓 DNS、Mixed proxy 與 Controller 都只監聽 `127.0.0.1`。不要為了讓 LAN 能連線就直接改成 `0.0.0.0`；DNS recursion、無驗證 proxy 及 Controller 都需要獨立的 firewall、ACL 與 authentication 設計。
:::

## 1. 先理解 fake-IP 的資料路徑

一般 DNS 模式會把上游回覆的真實 IP 交給應用程式；fake-IP 模式則是：

1. 應用程式向 Aster DNS 查詢 `www.example.org`。
2. Aster 回傳 `198.18.0.0/16` 中的一個保留位址。
3. 流量進入 Aster 的 Mixed／TUN／透明代理入口。
4. Aster 由 fake-IP mapping 還原 `www.example.org`。
5. domain rule 使用原始網域比對，再選擇 `DIRECT`、`PROXY` 或 `REJECT`。

fake-IP 的優點是 domain rule 不依賴可能受污染的本機解析結果；但它有一個重要前提：**DNS 與後續流量都必須交給同一個 Aster runtime**。如果應用取得 `198.18.x.x` 後直接從實體網卡連線，網路上並不存在那台主機，連線必然失敗。

`fake-ip-filter` 用於不適合 fake-IP 的名稱，例如：

- `.lan`、`.local` 與 mDNS。
- 需要真實 IP 的印表機、NAS、遊戲主機或 captive portal。
- NTP、連線偵測或部分 P2P／VoIP 服務。

## 2. 寫入完整 cookbook 設定

建立或覆核 Aster home 下的 `config.yaml`。以下設定是完整結構；先替換所有 `<...>`：

```yaml
mixed-port: 7890
allow-lan: false
mode: rule

# 教學期間使用 debug，確認無誤後改回 info。
log-level: debug
ipv6: false

external-controller: 127.0.0.1:9090
secret: "<CONTROLLER_SECRET>"

profile:
  store-selected: true
  store-fake-ip: true

dns:
  enable: true
  listen: 127.0.0.1:1053
  ipv6: false
  cache-algorithm: arc
  enhanced-mode: fake-ip
  fake-ip-range: 198.18.0.1/16

  # Bootstrap resolver 只可使用純 IP 或 system。
  default-nameserver:
    - 1.1.1.1
    - 8.8.8.8

  # 一般查詢使用加密 DNS。IP literal 避免額外 hostname bootstrap。
  nameserver:
    - https://1.1.1.1/dns-query
    - https://8.8.8.8/dns-query

  # 解析代理節點 hostname，避免形成「先有代理才能解析代理」的循環。
  proxy-server-nameserver:
    - https://1.1.1.1/dns-query
    - https://8.8.8.8/dns-query

  # 預設 false：nameserver／fallback 的連線不再次進入 rules。
  respect-rules: false

  # blacklist 模式下，命中者使用真實 IP，未命中者使用 fake-IP。
  fake-ip-filter-mode: blacklist
  fake-ip-filter:
    - "*.lan"
    - "*.local"
    - "time.*.com"
    - "rule-set:private-domains"

  # 可使用一般 domain matcher、GEOSITE 或 domain/classical rule set。
  nameserver-policy:
    "rule-set:private-domains":
      - system
    "+.example.com":
      - https://1.1.1.1/dns-query

proxies:
  - name: Edge-AnyTLS-REALITY
    type: anytls
    server: <ASTER_SERVER_HOST_OR_IP>
    port: 443
    password: "<ANYTLS_PASSWORD>"
    sni: <REALITY_SNI_FROM_SERVER>
    client-fingerprint: chrome
    reality-opts:
      public-key: <REALITY_PUBLIC_KEY>
      short-id: <REALITY_SHORT_ID>
    udp: true

proxy-groups:
  - name: PROXY
    type: select
    proxies:
      - Edge-AnyTLS-REALITY
      - DIRECT

rule-providers:
  # domain behavior 的 payload 只放網域 matcher。
  private-domains:
    type: inline
    behavior: domain
    payload:
      - "+.lan"
      - "+.local"
      - "+.internal.example"

  # ipcidr behavior 的 payload 只放 IP prefix。
  private-cidrs:
    type: inline
    behavior: ipcidr
    payload:
      - 10.0.0.0/8
      - 172.16.0.0/12
      - 192.168.0.0/16
      - 127.0.0.0/8
      - 169.254.0.0/16

  # classical behavior 放完整規則，但不附最終 target。
  tutorial-ads:
    type: inline
    behavior: classical
    payload:
      - DOMAIN-SUFFIX,ads.example
      - DOMAIN-KEYWORD,tracker

rules:
  # 由最具體、最不應外送的規則開始。
  - RULE-SET,tutorial-ads,REJECT
  - RULE-SET,private-domains,DIRECT
  - RULE-SET,private-cidrs,DIRECT,no-resolve

  # 可重現的直連與代理測試。
  - DOMAIN-SUFFIX,example.com,DIRECT
  - DOMAIN-SUFFIX,ipify.org,PROXY

  # 最終規則一定放最後。
  - MATCH,PROXY
```

如果不是 AnyTLS，依[出站與代理群組](/reference/outbounds)替換 `proxies` block，並保留名稱 `Edge-AnyTLS-REALITY` 或同步修改 `PROXY.proxies` 的引用。

## 3. 理解 `nameserver-policy`

`nameserver-policy` 會依查詢名稱選擇不同 resolver。上例有兩條：

```yaml
nameserver-policy:
  "rule-set:private-domains":
    - system
  "+.example.com":
    - https://1.1.1.1/dns-query
```

- 命中 `private-domains` 的名稱交給系統 resolver，適合由 DHCP／VPN 提供的內部 DNS。
- `example.com` 及其子網域固定交給 Cloudflare DoH。
- 未命中 policy 的名稱使用 `nameserver`。

可用的 key 形式包括：

```yaml
nameserver-policy:
  "www.example.com": 1.1.1.1
  "+.corp.example": 10.0.0.53
  "geosite:private": system
  "rule-set:internal-domains":
    - 10.0.0.53
    - 10.0.0.54
```

`default-nameserver` 不是一般 domain policy。它用來 bootstrap 含 hostname 的 DoH／DoT 上游，所以程式會要求純 IP resolver 或 `system`；不要在其中填 `https://dns.example/dns-query` 這類仍需先解析 hostname 的值。

`rule-set:` policy 只能引用：

- `behavior: domain`。
- `behavior: classical`，但其中只有 domain 類規則參與 DNS policy。

`behavior: ipcidr` 沒有 domain 可比對，不能用於 `nameserver-policy` 或 `fake-ip-filter`。

### 如何確認實際選到哪個上游

教學 YAML 使用 `log-level: debug`。查詢時可在 log 找到：

```text
[DNS] resolve <domain> A from <upstream>
```

其中 `<upstream>` 會顯示被選中的 DNS address。確認完成後把 `log-level` 改回 `info`，避免正式環境產生大量 log。

## 4. 設計 rules 的順序

`rules` 從上往下比對，第一個命中就決定 target。實用順序通常是：

1. 安全阻擋與非常精確的例外。
2. LAN／內部網域。
3. 私有 IP 與不應解析的 IP rule。
4. 明確直連或代理的服務。
5. 較大型的 GEOIP／GEOSITE／rule set。
6. 最後的 `MATCH`。

以下是常用 cookbook，可依需求插入完整 YAML 的 `MATCH` 之前：

```yaml
rules:
  # 單一 host 與 suffix。
  - DOMAIN,api.example.com,PROXY
  - DOMAIN-SUFFIX,example.net,DIRECT

  # 關鍵字與正規表示式應盡量具體，避免誤傷。
  - DOMAIN-KEYWORD,tracker,REJECT
  - DOMAIN-REGEX,^ads[0-9]*\.,REJECT

  # IP 規則已有目標 IP 時可加 no-resolve，避免為了比對反而觸發解析。
  - IP-CIDR,203.0.113.0/24,REJECT,no-resolve
  - IP-CIDR6,2001:db8::/32,REJECT,no-resolve

  # Port／network 條件。
  - DST-PORT,22,DIRECT
  - NETWORK,UDP,PROXY

  - MATCH,PROXY
```

`203.0.113.0/24` 與 `2001:db8::/32` 是文件示例保留網段，不代表應在正式設定阻擋它們。

::: tip Domain rule 應優先於會觸發解析的 IP rule
當 metadata 還保有 hostname 時，domain rule 可直接比對。把大量需要解析的 IP/GEOIP 規則放在前面，可能增加 DNS latency；可使用 `no-resolve` 的規則則應明確加上。
:::

## 5. 選擇 rule provider behavior

### `domain`

只包含 domain matcher，適合網域封鎖、直連與 DNS policy：

```yaml
rule-providers:
  streaming-domains:
    type: inline
    behavior: domain
    payload:
      - "+.video.example"
      - "api.media.example"
```

引用：

```yaml
rules:
  - RULE-SET,streaming-domains,PROXY
```

### `ipcidr`

只包含 IPv4／IPv6 CIDR，適合私有網路或已整理的 IP 清單：

```yaml
rule-providers:
  office-networks:
    type: inline
    behavior: ipcidr
    payload:
      - 10.20.0.0/16
      - 2001:db8:20::/48

rules:
  - RULE-SET,office-networks,DIRECT,no-resolve
```

### `classical`

每一項是完整 rule 語法，但 provider 本身不寫 target；target 由外層 `RULE-SET` 指定：

```yaml
rule-providers:
  mixed-blocklist:
    type: inline
    behavior: classical
    payload:
      - DOMAIN-SUFFIX,ads.example
      - DOMAIN-KEYWORD,telemetry
      - IP-CIDR,203.0.113.0/24,no-resolve

rules:
  - RULE-SET,mixed-blocklist,REJECT
```

classical 最靈活，但大型清單的解析與記憶體成本通常高於專用 domain／ipcidr set。

## 6. 改用本機或遠端 provider

`type: inline` 最適合自包含教學。正式部署可換成 `file` 或 `http`。

### 本機 YAML provider

設定：

```yaml
rule-providers:
  local-direct:
    type: file
    behavior: domain
    format: yaml
    path: ./rules/local-direct.yaml
```

`./rules/local-direct.yaml`：

```yaml
payload:
  - "+.example.com"
  - "updates.example.net"
```

相對 `path` 以 Aster home 為基準。預設只允許 home directory 內的安全路徑；需要其他根目錄時，應明確設定 `SAFE_PATHS`，不要直接停用 safe-path check。

### 遠端 MRS provider

```yaml
rule-providers:
  remote-ads:
    type: http
    behavior: domain
    format: mrs
    url: https://<TRUSTED_PROVIDER_HOST>/rules/ads.mrs
    path: ./rules/remote-ads.mrs
    interval: 86400
    proxy: DIRECT
    size-limit: 5242880
```

注意：

- `<TRUSTED_PROVIDER_HOST>` 必須替換成你信任的 HTTPS 來源。
- `path` 應使用 home 下的獨立檔案，避免 providers 互相覆寫。
- `interval` 單位是秒。
- `proxy` 控制 provider 下載走哪個 outbound／group；若設成 `PROXY`，必須確保啟動時 proxy 已可用。
- `size-limit` 可限制不可信或異常回應的最大尺寸。
- MRS 只支援 `domain` 與 `ipcidr`，不支援 `classical`。

YAML provider 需要頂層 `payload:`（也支援 `rules:`）；text provider 則是一行一項。MRS 是二進位格式，可用 Aster 轉換：

```sh
aster-core convert-ruleset domain yaml source.yaml target.mrs
aster-core convert-ruleset ipcidr text source.txt target.mrs
```

不要把任意訂閱內容當成可信設定。Provider 可以改變大量流量方向，上線前應固定來源、使用 HTTPS、限制大小並保留上一版供 rollback。

## 7. 驗證設定與啟動

Linux／macOS：

```sh
./aster-core -d ./config -f ./config/config.yaml -t
./aster-core -d ./config -f ./config/config.yaml
```

PowerShell：

```powershell
.\aster-core.exe -d .\config -f .\config\config.yaml -t
.\aster-core.exe -d .\config -f .\config\config.yaml
```

設定驗證會檢查：

- rule target 是否是存在的 proxy／group。
- `RULE-SET` 名稱是否存在。
- nameserver policy 是否引用可用的 domain/classical provider。
- fake-IP range、filter 與 DNS 上游格式。
- provider behavior／format／path。

`-t` 不會保證遠端 provider URL、內部 DNS 或代理節點可連線，仍要做執行期測試。

## 8. 驗證 DNS 與 fake-IP

### 直接查 DNS listener

```sh
dig @127.0.0.1 -p 1053 example.org A +short
```

一般網域應回傳 `198.18.0.0/16` 中的 fake-IP。

查詢 filter 中的時間同步網域：

```sh
dig @127.0.0.1 -p 1053 time.apple.com A +short
```

只要上游可解析，它應回傳真實 IP，而不是 `198.18.x.x`。

沒有 `dig` 時，可使用 Controller：

```sh
curl -fsS \
  -H 'Authorization: Bearer <CONTROLLER_SECRET>' \
  'http://127.0.0.1:9090/dns/query?name=example.org&type=A'
```

### 清除 DNS 與 fake-IP cache

修改 policy、filter 或 provider 後，舊 cache 可能讓結果看起來沒變：

```sh
curl -fsS -X POST \
  -H 'Authorization: Bearer <CONTROLLER_SECRET>' \
  http://127.0.0.1:9090/cache/dns/flush

curl -fsS -X POST \
  -H 'Authorization: Bearer <CONTROLLER_SECRET>' \
  http://127.0.0.1:9090/cache/fakeip/flush
```

兩個 endpoint 成功時回傳 HTTP `204 No Content`。作業系統、瀏覽器與應用程式可能還有自己的 DNS cache，也要分別清除或重啟。

## 9. 驗證規則命中

先產生三種流量：

```sh
# 明確 DIRECT。
curl -I --proxy http://127.0.0.1:7890 https://example.com/

# 明確 PROXY。
curl -fsS --proxy http://127.0.0.1:7890 https://api.ipify.org

# 命中 tutorial-ads 後應快速被 REJECT。
curl --max-time 3 --proxy http://127.0.0.1:7890 http://ads.example/
```

終端機 log 應顯示 `match ... using DIRECT`、`using PROXY[...]` 或 `using REJECT`。

Aster 會為頂層 rules 維護 hit／miss counter。讀取 Controller：

```sh
curl -fsS \
  -H 'Authorization: Bearer <CONTROLLER_SECRET>' \
  http://127.0.0.1:9090/rules
```

有 `jq` 時可簡化：

```sh
curl -fsS \
  -H 'Authorization: Bearer <CONTROLLER_SECRET>' \
  http://127.0.0.1:9090/rules |
  jq '.rules[] | {
    index,
    type,
    payload,
    proxy,
    hitCount: .extra.hitCount,
    missCount: .extra.missCount
  }'
```

如果流量成功，但預期規則的 `hitCount` 沒增加：

- 更前面的規則已先命中。
- 應用程式沒有使用 Aster proxy／TUN。
- SOCKS client 在本機先解析，metadata 只剩 IP，domain rule 無法比對。
- 瀏覽器 reuse 既有 connection，沒有建立新連線。

### 查看 provider 載入狀態

```sh
curl -fsS \
  -H 'Authorization: Bearer <CONTROLLER_SECRET>' \
  http://127.0.0.1:9090/providers/rules
```

手動要求 HTTP／file provider 更新：

```sh
curl -fsS -X PUT \
  -H 'Authorization: Bearer <CONTROLLER_SECRET>' \
  http://127.0.0.1:9090/providers/rules/remote-ads
```

Inline provider 不需要下載；update 只會更新其時間資訊。

## 10. DNS 防泄漏策略

「DNS 不泄漏」必須先定義威脅模型。常見需求有兩層：

1. 不送出明文 UDP/TCP 53：使用 DoH／DoT／DoQ 可達成。
2. DNS 上游本身也必須經代理：只用 DoH 不代表它走代理；上例的 `respect-rules: false` 是刻意讓 DNS 連線直連，以簡化 bootstrap。

### 讓一般 DNS upstream 經過代理

先確認 proxy hostname 可由 `proxy-server-nameserver` 直接解析，再考慮：

```yaml
dns:
  respect-rules: true
  proxy-server-nameserver:
    - https://1.1.1.1/dns-query
  nameserver:
    - https://1.1.1.1/dns-query
```

程式要求 `respect-rules: true` 時，`proxy-server-nameserver` 不可為空。一般 `nameserver`／`fallback`／policy 上游連線會依 `rules` 選路；代理節點本身則由專用 resolver bootstrap，避免遞迴。

也能對單一 DNS 上游明確指定 group：

```yaml
dns:
  nameserver:
    - "https://1.1.1.1/dns-query#PROXY"
```

先在 `respect-rules: false` 的簡單模式驗證，再逐步切換。DNS 經代理若設定成「解析代理需要代理」，會形成死循環。

### 應用程式繞過系統 DNS

瀏覽器、Android Private DNS、VPN client 與部分程式會自行使用 DoH／DoT。處理方式：

- 關閉應用程式自訂 DNS，改用系統／Aster DNS。
- 或把該 DoH host 以 rules 明確管理。
- 在可控網路中阻擋未授權的 UDP/TCP 53 與 853；但 DoH 使用 443，不能只靠 port 完整識別。
- 不要假設設定了 `dns.listen` 就會自動攔截所有程式。

### SOCKS5 本機解析

以下寫法可能由 `curl` 所在主機先解析 hostname：

```sh
curl --proxy socks5://127.0.0.1:7890 https://example.org/
```

改用 remote hostname resolution：

```sh
curl --proxy socks5h://127.0.0.1:7890 https://example.org/
```

不同 SOCKS client 的選項名稱不同，應確認它傳送 domain 還是 IP。

## 11. 全機接管與 TUN DNS hijack

只設定 HTTP/SOCKS proxy 不會接管沒有遵循系統 proxy 的程式。需要全機接管時，可在頂層加入：

```yaml
tun:
  enable: true
  stack: mixed
  auto-route: true
  auto-detect-interface: true
  strict-route: true
  dns-hijack:
    - any:53
```

啟用前必須了解：

- Linux 通常需要 `CAP_NET_ADMIN`／root 與 `/dev/net/tun`。
- Windows 需要對應權限及可用的 TUN driver。
- `strict-route` 用於降低繞過路由，但可能讓本機無法存取 LAN 或其他 VPN；先保留 out-of-band 管理方式。
- DNS hijack 只處理命中的 DNS traffic；應用自帶 DoH 仍是一般 HTTPS 連線。
- Aster 自己的 upstream 不可被 hijack 後再次送回自己，否則會循環。
- 若使用 fake-IP，TUN 必須持續運作以攔截 `198.18.0.0/16` 的後續連線。
- Docker Desktop、WSL、路由器與原生 Linux 的 route 行為不同。

第一次設定 TUN 時，先保留 `allow-lan: false`、不要同時加入複雜 firewall rewrite，逐步驗證 DNS、TCP、UDP 與 LAN。

## 12. 用封包觀察排查泄漏

Linux 可在測試期間觀察傳統 DNS／DoT：

```sh
sudo tcpdump -ni any '(udp port 53 or tcp port 53 or tcp port 853)'
```

另開終端機產生測試流量後檢查：

- 是否有應用程式直接詢問路由器、ISP 或未知 resolver。
- 封包來源 process／network namespace 是否其實不是 Aster。
- `proxy-server-nameserver` 的 bootstrap 查詢是否符合預期。
- IPv6 上是否出現未納入設計的 DNS 或直連。

DoH 使用 TCP/UDP 443，僅靠 port 無法判斷；要同時核對目的 IP、Aster debug log、browser policy 與防火牆 log。Windows 可使用 Wireshark／pktmon，macOS 可使用 `tcpdump`。

## 故障排查

### DNS listener 啟動失敗

- `127.0.0.1:1053` 已被其他 Aster instance 或 DNS 程式占用。
- 改成 53 後缺少綁定低連接埠權限。
- Docker 沒有 publish UDP 與 TCP port。
- YAML 中 `dns.enable` 未啟用。

### 查詢只得到 timeout

- DoH／DoT 上游被網路阻擋。
- `default-nameserver` 無法 bootstrap 上游 hostname。
- `respect-rules` 造成 DNS 經過尚未可用的 proxy。
- firewall 只允許 UDP，卻漏了 TCP DNS；或只允許 TCP 443，卻使用 DoQ。
- `system` resolver 又指回 `127.0.0.1:1053`，形成循環。

### `nameserver-policy` 沒有效果

- key 的 domain matcher 拼字錯誤。
- `rule-set:` 引用了不存在的 provider。
- provider 是 `behavior: ipcidr`，不能比對 DNS domain。
- 舊 DNS cache 尚未清除。
- 實際查詢由瀏覽器內建 DoH 直接送出，根本沒有進入 Aster DNS。

### Domain rule 不命中，只命中 IP／MATCH

- SOCKS client 在本機解析；改用 remote DNS 模式。
- 應用直接連 IP，Aster 沒有 hostname metadata。
- fake-IP DNS 與接收流量的不是同一個 Aster instance／profile。
- 規則順序錯誤，更前方已命中。
- 既有長連線未重建。

### fake-IP 查詢正常，但所有連線失敗

這通常代表 DNS 已使用 Aster，後續流量卻沒有進入 Aster。確認：

- 應用有設定 HTTP/SOCKS proxy；或
- TUN 已啟用且 route 包含 `198.18.0.0/16`；以及
- 沒有另一個 VPN／route table 把 fake-IP 流量搶走。

### HTTP 正常，QUIC／遊戲／語音仍直連

HTTP CONNECT 主要處理 TCP。UDP 應用需要：

- 使用 SOCKS5 UDP／支援 UDP 的 client，或
- 由 TUN／TProxy 接管；以及
- proxy outbound 本身支援 UDP。本文 AnyTLS 設定使用 `udp: true`，透過 UoT 支援 UDP，但 client 到 Aster 的入口仍必須正確接管該流量。

### IPv6 泄漏或 IPv6 網站行為不同

本文同時設定頂層 `ipv6: false` 與 `dns.ipv6: false`，適合先建立可控的 IPv4 baseline。若要啟用 IPv6：

1. 確認 proxy 與 DNS 上游支援 IPv6。
2. 加入 IPv6 rules 與 route。
3. TUN／防火牆同時處理 `::/0` 與 AAAA。
4. 分別測試直連及代理出口 IPv6。

只關閉 Aster 的 AAAA 回覆，不能阻止完全繞過 Aster 的應用自行直連 IPv6；真正的全機防泄漏仍依賴 route／firewall。

### Provider 更新後規則沒有改變

- 檢查 `/providers/rules` 的 `updatedAt`、`ruleCount` 與 format。
- HTTP 回應可能不是預期 YAML/text/MRS。
- YAML 缺少 `payload:`／`rules:`。
- `behavior` 與資料內容不符。
- `path` 不在允許的 safe path。
- 清除 DNS cache，並為既有連線建立新 request。

## 上線前檢查清單

- 把 `log-level: debug` 改回 `info`。
- Controller、DNS 與 Mixed proxy 維持 loopback，或已配置必要 ACL／authentication。
- `PROXY` 沒有因 `store-selected` 保留在 `DIRECT`。
- fake-IP filter 包含實際 LAN、mDNS、NTP、遊戲與 captive portal 需求。
- Provider 使用 HTTPS、受信任來源、合理 `size-limit` 與可回復的 cache。
- DNS bootstrap 不會依賴尚未建立的 proxy。
- 使用封包觀察確認沒有未預期的 UDP/TCP 53、853、IPv6 或旁路流量。
- TUN 環境已分別測試 TCP、UDP、DNS、LAN、休眠喚醒與網路切換。

## 下一步

- [第一個代理設定](/tutorials/first-proxy)
- [規則與 DNS 參考](/reference/routing-dns)
- [完整設定欄位](/reference/configuration)
- [出站與代理群組](/reference/outbounds)
- [AnyTLS + REALITY](/reference/anytls-reality)
- [TUN 與平台部署注意事項](/reference/inbounds)
- [疑難排解](/troubleshooting)
