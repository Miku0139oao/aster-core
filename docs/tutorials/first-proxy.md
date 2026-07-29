# 第一個代理設定

這篇教學會從下載開始，帶你完成一個可以使用的 Aster Core 客戶端。完成後，瀏覽器或其他程式可以透過 `127.0.0.1:7890` 使用代理。

你會完成：

- 填入一個 AnyTLS + REALITY 遠端節點。
- 啟動本機 HTTP／SOCKS5 代理。
- 在「使用代理」和「直接連線」之間切換。
- 設定基本路由規則和 DNS。
- 用一條指令確認代理真的有作用。

::: warning 範例不附可用節點
本文中的主機、密碼、REALITY public key、SNI 與 short ID 全是 placeholder。Aster Core 是客戶端，不會替你提供代理伺服器；必須先取得 Xray、sing-box、SideraCore、自建服務端或服務供應者提供的完整連線資料，逐項替換後才能連線。
:::

## 前置條件

你需要：

- Linux、macOS 或 Windows 主機。
- 對應作業系統與 CPU 架構的 Aster Core release。
- 一組可用的 AnyTLS + REALITY 用戶端資料：

| 本文 placeholder | 應填內容 |
| --- | --- |
| `<ASTER_SERVER_HOST_OR_IP>` | AnyTLS 節點／服務端的 IP 或網域；不是偽裝站網域 |
| `443` | 節點連接埠；服務端提供的不是 443 就要修改 |
| `<ANYTLS_PASSWORD>` | 服務端提供的 AnyTLS 密碼 |
| `<REALITY_SNI_FROM_SERVER>` | 服務端提供的偽裝網站名稱 |
| `<REALITY_PUBLIC_KEY>` | 服務端提供的 REALITY 公開金鑰 |
| `<REALITY_SHORT_ID>` | 服務端提供的 short ID；沒有提供時才省略 |
| `<CONTROLLER_SECRET>` | 你自己設定的本機控制密碼 |

若服務提供者給的是 `anytls://` URI，可依下列方式映射：

```text
anytls://<password>@<server>:<port>?security=reality&sni=<sni>&fp=chrome&pbk=<public-key>&sid=<short-id>
```

不要把正式密碼、private key（私鑰）或本機控制密碼貼到公開 issue、聊天記錄或 Git repository。

## 1. 下載與安裝 Aster Core

前往 [GitHub Releases](https://github.com/Miku0139oao/aster-core/releases)，選擇符合系統及 CPU 架構的檔案。舊款 x86-64 CPU 可優先選擇名稱含 `amd64-v1` 或 `amd64-compatible` 的版本。

下載後，把檔案的 SHA-256 與同一個 release 公布的 checksum 比對：

Linux／macOS：

```sh
sha256sum ./<downloaded-release-file>
```

macOS 如果沒有 `sha256sum`，可用：

```sh
shasum -a 256 ./<downloaded-release-file>
```

Windows PowerShell：

```powershell
Get-FileHash .\<downloaded-release-file> -Algorithm SHA256
```

解壓後可把執行檔命名為 `aster-core`；Windows 使用 `aster-core.exe`。在 Unix 系統加上執行權限並確認版本：

```sh
chmod +x ./aster-core
./aster-core -v
```

PowerShell：

```powershell
.\aster-core.exe -v
```

如果使用 `.deb`、`.rpm`、Arch package 或 OpenWrt package，執行檔通常已安裝為 `/usr/bin/aster-core`。Linux 套件的完整操作方式見[Linux 套件與 systemd](/deployment/linux)。

### 從原始碼建置

Release 是較省事的選擇；若確實需要自行建置：

```sh
git clone https://github.com/Miku0139oao/aster-core.git
cd aster-core
go mod download
CGO_ENABLED=0 go build -tags with_gvisor -trimpath -o aster-core .
./aster-core -v
```

正式 release 使用 `with_gvisor` build tag。自行建置卻省略此 tag，TUN 與部分功能可能和 release 不同。

## 2. 建立設定目錄

以下範例假設執行檔在目前目錄，設定放在 `./config/config.yaml`：

```sh
mkdir -p ./config
```

PowerShell：

```powershell
New-Item -ItemType Directory -Force .\config
```

`-d ./config` 會把這個目錄設為 Aster home。`cache.db`、provider cache 與其他相對路徑都會以它為基準，因此正式使用後不要只備份 YAML。

## 3. 準備 secret 與節點資料

Controller secret 可用密碼管理器產生，或在有 OpenSSL 的系統執行：

```sh
openssl rand -base64 32
```

這個 secret 只用來保護本機 Controller API，和 AnyTLS password 不同。

逐一核對服務端資料：

1. `server` 是實際連線的 Aster 主機。
2. `sni` 是 REALITY 偽裝名稱，必須出現在伺服器允許清單。
3. `public-key` 是伺服器 public key，不能填 private key。
4. `short-id` 必須與伺服器相同。
5. `password` 必須是該 AnyTLS 使用者的密碼。

任何一項不匹配，都可能表現為 TLS／REALITY handshake 失敗。

## 4. 寫入完整設定

建立 `config/config.yaml`，貼上以下完整內容，再替換所有以 `<...>` 標示的值：

```yaml
# 本機 HTTP 與 SOCKS5 共用的入口。
mixed-port: 7890
allow-lan: false
mode: rule
log-level: info
ipv6: false

# Controller 只綁 loopback，不直接暴露到 LAN／Internet。
external-controller: 127.0.0.1:9090
secret: "<CONTROLLER_SECRET>"

profile:
  store-selected: true
  store-fake-ip: true

dns:
  enable: true
  listen: 127.0.0.1:1053
  ipv6: false
  enhanced-mode: fake-ip
  fake-ip-range: 198.18.0.1/16

  # 解析 DoH hostname 或其他 DNS 上游時的 bootstrap resolver。
  # 此欄只應使用純 IP resolver 或 system。
  default-nameserver:
    - 1.1.1.1
    - 8.8.8.8

  nameserver:
    - https://1.1.1.1/dns-query
    - https://8.8.8.8/dns-query

  # 專門解析 proxy 的 server 網域，避免節點解析依賴尚未建立的代理。
  proxy-server-nameserver:
    - https://1.1.1.1/dns-query
    - https://8.8.8.8/dns-query

  # LAN、mDNS 與時間同步常不適合 fake-IP。
  fake-ip-filter:
    - "*.lan"
    - "*.local"
    - "time.*.com"

proxies:
  - name: Edge-AnyTLS-REALITY
    type: anytls
    server: <ASTER_SERVER_HOST_OR_IP>
    port: 443 # 若伺服器不是 443，請改成實際 port。
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

rules:
  # 私有位址不送到遠端代理，也不為 IP 規則額外觸發 DNS。
  - IP-CIDR,10.0.0.0/8,DIRECT,no-resolve
  - IP-CIDR,172.16.0.0/12,DIRECT,no-resolve
  - IP-CIDR,192.168.0.0/16,DIRECT,no-resolve
  - IP-CIDR,127.0.0.0/8,DIRECT,no-resolve

  # 教學驗證用：example.com 直連，ipify.org 經 PROXY。
  - DOMAIN-SUFFIX,example.com,DIRECT
  - DOMAIN-SUFFIX,ipify.org,PROXY

  # 所有未命中項目都經過可切換的 PROXY 群組。
  - MATCH,PROXY
```

這份 YAML 的幾個重點：

- `allow-lan: false` 讓 Mixed proxy 只服務本機。要分享給 LAN 時，必須另外設計防火牆與驗證，不能只把它改成 `true` 就直接暴露。
- `mode: rule` 讓 `rules` 由上往下比對，第一個命中就停止。
- `PROXY` 是 `select` group。第一個選項是 AnyTLS，`DIRECT` 只用於除錯或明確切換。
- `profile.store-selected` 會記住 group 選擇；曾切到 `DIRECT` 時，重啟後也可能繼續直連。
- fake-IP 讓 Aster 保留原始網域供 domain rule 比對。`198.18.0.0/16` 是回給 DNS client 的保留範圍，不是遠端網站的真實 IP。
- `proxy-server-nameserver` 解決「要先解析 proxy hostname，才能建立 proxy」的 bootstrap 問題。

::: danger 不要用 `skip-cert-verify` 掩蓋 REALITY 錯誤
public key、SNI 或 short ID 錯誤時，應修正資料。加入 `skip-cert-verify: true` 不會把錯誤的 REALITY 身分變成正確設定，還會削弱其他 TLS 驗證。
:::

### 如果你的 AnyTLS 使用一般憑證 TLS

只有在服務端不是 REALITY、而是具有可信任 TLS 憑證時，才把 outbound 改成：

```yaml
proxies:
  - name: Edge-AnyTLS-TLS
    type: anytls
    server: <ASTER_SERVER_HOST_OR_IP>
    port: 443
    password: "<ANYTLS_PASSWORD>"
    sni: <CERTIFICATE_HOSTNAME>
    udp: true
```

刪除 `reality-opts`，並讓 `sni` 符合憑證名稱。不要為了省事停用憑證驗證。

如果使用 VLESS、Trojan、Hysteria 2 或其他協定，只需依[出站與代理群組](/reference/outbounds)替換 `proxies` 項目；`PROXY`、DNS 與 rules 的結構可保留。

## 5. 先驗證設定

Linux／macOS：

```sh
./aster-core -d ./config -f ./config/config.yaml -t
```

PowerShell：

```powershell
.\aster-core.exe -d .\config -f .\config\config.yaml -t
```

成功時會看到類似：

```text
configuration file ... test is successful
```

`-t` 會解析整份 YAML、建立 proxy/group 模型並檢查規則引用，但不會證明遠端密碼或 REALITY 資料可以完成握手。遠端可用性要在下一步實際連線驗證。

常見的設定檢查錯誤：

- `proxy [PROXY] not found`：rule 引用了不存在或拼字不同的 group。
- REALITY public key 解析失敗：填到 private key、複製不完整或仍是 placeholder。
- `short-id` 無效：應是伺服器提供的 hex 字串。
- YAML parse error：縮排錯誤、tab 混入或含特殊字元的密碼未加引號。

## 6. 啟動並觀察 log

前景啟動最適合第一次除錯：

```sh
./aster-core -d ./config -f ./config/config.yaml
```

PowerShell：

```powershell
.\aster-core.exe -d .\config -f .\config\config.yaml
```

啟動後應確認：

- Mixed listener 綁定 `127.0.0.1:7890`。
- DNS UDP/TCP listener 綁定 `127.0.0.1:1053`。
- Controller 綁定 `127.0.0.1:9090`。
- 沒有 port already in use、provider 載入或設定錯誤。

請保留這個終端機，另開一個終端機執行以下驗證。

## 7. 驗證 Controller 與群組選擇

先確認 Controller：

```sh
curl -fsS \
  -H 'Authorization: Bearer <CONTROLLER_SECRET>' \
  http://127.0.0.1:9090/version
```

Windows PowerShell 請使用 `curl.exe`，避免舊版 PowerShell 把 `curl` 解讀為其他命令：

```powershell
curl.exe -fsS -H "Authorization: Bearer <CONTROLLER_SECRET>" http://127.0.0.1:9090/version
```

若先前選過 `DIRECT`，可明確把 `PROXY` 切回 AnyTLS：

```sh
curl -fsS -X PUT \
  -H 'Authorization: Bearer <CONTROLLER_SECRET>' \
  -H 'Content-Type: application/json' \
  --data '{"name":"Edge-AnyTLS-REALITY"}' \
  http://127.0.0.1:9090/proxies/PROXY
```

成功會回傳 HTTP `204 No Content`。讀回 group 狀態：

```sh
curl -fsS \
  -H 'Authorization: Bearer <CONTROLLER_SECRET>' \
  http://127.0.0.1:9090/proxies/PROXY
```

## 8. 用 `curl` 驗證第一條代理連線

先取得不經 Aster 的目前出口 IP，作為比較基準：

```sh
curl -fsS https://api.ipify.org
```

再經過 Aster 的 HTTP proxy：

```sh
curl -fsS \
  --proxy http://127.0.0.1:7890 \
  https://api.ipify.org
```

也可測試同一個 Mixed port 的 SOCKS5 模式：

```sh
curl -fsS \
  --proxy socks5h://127.0.0.1:7890 \
  https://api.ipify.org
```

`socks5h://` 中的 `h` 表示把 hostname 交給 proxy，不先用作業系統 DNS 解析。代理後的 IP 通常應是代理伺服器出口，而不是本機出口。

接著驗證教學中的直連規則：

```sh
curl -I \
  --proxy http://127.0.0.1:7890 \
  https://example.com/
```

Aster 終端機會顯示類似以下規則命中資訊：

```text
[TCP] ... --> api.ipify.org:443 match DomainSuffix(ipify.org) using PROXY[Edge-AnyTLS-REALITY]
[TCP] ... --> example.com:443 match DomainSuffix(example.com) using DIRECT
```

實際來源位址、chain 顯示與大小寫可能不同；關鍵是 `ipify.org` 使用 `PROXY` chain，而 `example.com` 使用 `DIRECT`。

## 9. 驗證內建 DNS

如果系統有 `dig`：

```sh
dig @127.0.0.1 -p 1053 example.org A +short
```

在 fake-IP 模式下，一般網域預期回傳 `198.18.0.0/16` 內的位址。也可透過 Controller 查詢：

```sh
curl -fsS \
  -H 'Authorization: Bearer <CONTROLLER_SECRET>' \
  'http://127.0.0.1:9090/dns/query?name=example.org&type=A'
```

這只能證明 Aster DNS 可查詢。一般 HTTP proxy request 已把 hostname 交給 Aster，不需要先把整台電腦的系統 DNS 改成 `127.0.0.1`。若要讓所有應用都使用 fake-IP，還必須設定系統 DNS、TUN 與 DNS hijack；只取得 fake-IP 卻讓流量繞過 Aster，連線不會成功。完整做法見[分流與 DNS 實戰](/tutorials/routing-dns)。

## 故障排查

### `connection refused` 到 `127.0.0.1:7890`

- Aster 尚未啟動或已因設定錯誤退出。
- `mixed-port` 被修改，測試命令仍使用 7890。
- 另一個程式已占用 7890；查看啟動 log。
- Docker 部署時，container port 沒有 publish 到 host。見 [Docker](/deployment/docker)。

### Controller 回傳 `401 Unauthorized`

- `<CONTROLLER_SECRET>` 沒有替換。
- request 的 Bearer token 與 YAML `secret` 不一致。
- 修改 YAML 後尚未 reload／重啟。

### `PROXY` 實際選到 `DIRECT`

`profile.store-selected: true` 會持久化選擇。使用上方 `PUT /proxies/PROXY` 切回 AnyTLS，或在相容 Dashboard 中檢查選擇。

### REALITY handshake／EOF／TLS 錯誤

依序核對：

1. `server` 與 `port` 是 AnyTLS 節點／服務端，不是偽裝站。
2. `sni` 完全匹配服務端 `server-names`。
3. `public-key` 是對應 private key 的 public key。
4. `short-id` 是服務端允許值。
5. `client-fingerprint` 已設定為對端支援的值；一般可先用 `chrome`。
6. 用戶端與伺服器時間已透過 NTP 校準。
7. 伺服器防火牆及前置 Caddy／Nginx 沒有占用或錯誤轉送該 port。

詳細欄位見 [AnyTLS + REALITY](/reference/anytls-reality)。

### AnyTLS 認證失敗

- password 大小寫、空白或引號內容不一致。
- 服務端使用者已停用、刪除或輪替密碼。
- 把 URI percent-encoding 後的文字直接貼入 YAML，沒有正確解碼。

### 網頁能開，但 DNS 或部分程式繞過代理

- HTTP proxy 只影響明確設定使用它的程式。
- SOCKS5 應使用 remote DNS；`curl` 使用 `socks5h://`，不是 `socks5://`。
- 瀏覽器可能啟用自己的 DoH。
- QUIC／UDP 不會由一般 HTTP CONNECT 自動接管。
- 要接管整機流量需另外部署 TUN、route 與 DNS hijack。

### 修改設定後行為沒變

先重新檢查，再 reload：

```sh
./aster-core -d ./config -f ./config/config.yaml -t
```

Unix 前景程序可接收 `SIGHUP` 重新讀取檔案：

```sh
kill -HUP <aster-core-pid>
```

Windows 可正常停止後重新啟動。若變更的是 DNS policy 或 fake-IP filter，還可能需要清除 DNS／fake-IP cache，操作見[分流與 DNS 實戰](/tutorials/routing-dns)。

## 下一步

- [分流與 DNS 實戰：fake-IP、policy、rule providers 與防泄漏](/tutorials/routing-dns)
- [規則與 DNS 參考](/reference/routing-dns)
- [出站與代理群組](/reference/outbounds)
- [AnyTLS + REALITY 完整參考](/reference/anytls-reality)
- [設定總覽](/reference/configuration)
- [疑難排解](/troubleshooting)
