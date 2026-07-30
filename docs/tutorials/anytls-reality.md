# AnyTLS + REALITY

Aster 可連接 AnyTLS + REALITY 節點。服務端可以是 Xray、sing-box、SideraCore、Aster，或其他支援相同參數的實作。

```text
瀏覽器或 App
   │
   ▼
Aster Core
   │ AnyTLS + REALITY
   ▼
遠端代理節點
   │
   ▼
網際網路
```

客戶端與服務端不必使用同一套設定格式。Aster 使用 Mihomo YAML，請依服務端提供的連線參數填寫。

## 客戶端設定

需要的節點資料：

| 你需要的資料 | 怎麼辨認 |
| --- | --- |
| 節點網址（`server`） | 你真正要連線的 IP 或網域，不是偽裝網站 |
| 連接埠（`port`） | 常見是 `443`，以服務端提供的數字為準 |
| 密碼（`password`） | 服務端分配給你的 AnyTLS 密碼 |
| 偽裝網站（`sni`） | 看起來像一般網站的網域名稱 |
| 瀏覽器指紋（`client-fingerprint`） | 不確定時先用 `chrome` |
| 公開金鑰（`public-key`） | 服務端提供的 public key；絕對不要填 private key |
| short ID | 服務端提供的一小段英數字；沒有提供時才省略 |

若服務端提供 `anytls://` 分享連結，可直接匯入支援 Aster 的 App。手動設定時，把以下內容儲存為 `config.yaml`，並替換所有 `<...>`：

```yaml
mixed-port: 7890
allow-lan: false
mode: rule
log-level: info

external-controller: 127.0.0.1:9090
secret: "<CONTROLLER_SECRET>"

proxies:
  - name: anytls-reality
    type: anytls
    server: "<SERVER_HOST_OR_IP>"
    port: 443
    password: "<ANYTLS_PASSWORD>"
    sni: "<REALITY_SNI>"
    client-fingerprint: chrome
    reality-opts:
      public-key: "<REALITY_PUBLIC_KEY>"
      short-id: "<REALITY_SHORT_ID>"
    udp: true

proxy-groups:
  - name: PROXY
    type: select
    proxies:
      - anytls-reality
      - DIRECT

rules:
  - MATCH,PROXY
```

檢查設定並啟動：

```sh
aster-core -d ./aster-client -f ./aster-client/config.yaml -t
aster-core -d ./aster-client -f ./aster-client/config.yaml
```

另開終端測試連線：

```sh
curl --proxy http://127.0.0.1:7890 https://www.cloudflare.com/cdn-cgi/trace
```

命令正常回傳內容即表示基本連線可用。

分享連結格式：

```text
anytls://<password>@proxy.example.com:443?security=reality&sni=www.microsoft.com&fp=chrome&pbk=<public-key>&sid=<short-id>#AnyTLS-REALITY
```

其中 `pbk` 對應公開金鑰、`sid` 對應 short ID、`fp` 對應瀏覽器指紋。完整的安裝與本機代理設定見[第一個代理設定](/tutorials/first-proxy)。

## Aster 服務端

以下章節示範在 VPS 上使用 Aster 自帶的 AnyTLS listener。若服務端已使用 Xray、sing-box、SideraCore 或其他實作，可跳到其對應文件設定。

```text
Aster client
   │ AnyTLS over TCP + REALITY
   ▼
VPS 上的 Aster listener
   ├─ TCP 目的地
   └─ UDP over TCP（UoT）目的地
```

### 部署內容

- 一筆 DNS-only 的節點網域，例如 `edge.example.com`。
- VPS 上監聽 TCP `443` 的 AnyTLS listener。
- 一組只留在 VPS 的 REALITY private key。
- 一組給 client 的 REALITY public key、short ID 與 AnyTLS password。
- 一份可測試 TCP 與 UDP/UoT 的 Aster client profile。
- 一條可匯入 Aster 的 `anytls://` REALITY 分享連結。

### 準備項目

- 一台有 public IPv4 的 Linux VPS，並可使用 root 或 sudo。
- 一個能編輯 DNS 的網域。
- VPS 與 client 都已安裝相容版本的 Aster Core；先用 `aster-core -v` 記下版本。
- VPS 的系統時間已由 NTP 同步。
- 已決定一個 VPS 可連線的 TLS fallback 站點。本篇以 `www.microsoft.com:443` 為例，正式使用時應自行確認可用性與適用性。

以下 placeholder 必須替換：

| Placeholder | 內容 |
| --- | --- |
| `<NODE_DOMAIN>` | 連到 VPS 的網域，例如 `edge.example.com` |
| `<SERVER_PRIVATE_KEY>` | VPS 使用的 REALITY private key |
| `<SERVER_PUBLIC_KEY>` | client 使用的同一組 REALITY public key |
| `<SHORT_ID>` | 最多 8 bytes 的十六進位 short ID |
| `<ANYTLS_PASSWORD>` | AnyTLS 使用者密碼 |
| `<CONTROLLER_SECRET>` | 一般 Controller secret，與 AnyTLS password 不同 |

::: danger 不可公開的資料
`<SERVER_PRIVATE_KEY>`、`<ANYTLS_PASSWORD>` 與 `<CONTROLLER_SECRET>` 都是敏感資料。不要貼到 issue、聊天、監控標籤或公開 Git。Public key 可提供給 client，但仍不建議連同完整節點資料公開。
:::

## 1. 建立 DNS

先只建立一筆 A record：

```text
Type: A
Name: edge
Value: <VPS_PUBLIC_IPV4>
TTL: 300
Proxy/CDN: DNS only
```

如果使用 Cloudflare，雲朵必須是灰色的 **DNS only**。一般 Cloudflare HTTP proxy 不會轉送 AnyTLS；除非另有支援任意 TCP 的產品，開橙雲會讓 client 連到 Cloudflare 而不是 Aster。

等候 DNS 生效後，在 client 與 VPS 各查一次：

```sh
dig +short A <NODE_DOMAIN>
```

預期只看到 VPS 的 public IPv4。部署初期不要急著加入 AAAA；本篇 server 設定是 `listen: 0.0.0.0`，若 DNS 先公布 IPv6，偏好 IPv6 的 client 反而可能失敗。要啟用 IPv6時，先讓 listener 確實綁定 IPv6、放行防火牆並從外網驗證，再新增 AAAA。

## 2. 檢查連接埠、防火牆與時間

### 確認 443 沒被占用

```sh
sudo ss -ltnp | grep -E '(^|[[:space:]])[^[:space:]]*:443[[:space:]]' || true
```

沒有輸出代表目前沒有 TCP listener。若 Caddy、Nginx、HAProxy 或另一個代理已占用 `443`，先設計 TCP/SNI 分流，或把本篇所有 `443` 一致改成另一個開放 port，例如 `8443`。不要直接停止未知的正式服務。

### 開放防火牆

雲端供應商的 security group 與 VPS 本機防火牆都要允許 inbound TCP `443`。保留現有 SSH 規則，並只執行實際使用的防火牆工具。

UFW：

```sh
sudo ufw allow 443/tcp
sudo ufw status verbose
```

firewalld：

```sh
sudo firewall-cmd --permanent --add-service=https
sudo firewall-cmd --reload
sudo firewall-cmd --list-all
```

AnyTLS listener 本身只監聽 **TCP**。Client 的 UDP 會封裝成 UoT 經這條 TCP 連線傳輸，所以不必為 AnyTLS 開 inbound UDP `443`；VPS 仍要允許必要的 outbound UDP。

### 確認 NTP

```sh
timedatectl status
```

預期至少看到：

```text
System clock synchronized: yes
```

如果尚未同步，先修復 chrony、systemd-timesyncd 或供應商的時間服務。REALITY 驗證依賴正確時間。

## 3. 驗證 fallback 目標

從 VPS 測試目標的 DNS、TCP 與 TLS：

```sh
getent ahosts www.microsoft.com
timeout 10 openssl s_client \
  -connect www.microsoft.com:443 \
  -servername www.microsoft.com \
  -brief </dev/null
```

預期可以建立 TLS 連線，且憑證名稱與 `www.microsoft.com` 相符。若 timeout、被 VPS 網路封鎖或 TLS 行為不穩定，先換一個你確定可用的 TLS 站點，並同時修改之後的：

- Server `reality-config.dest`
- Server `reality-config.server-names`
- Client `sni`
- 分享連結 `sni`

`dest` 是 REALITY 驗證失敗時的 fallback 目的地，不是 Aster client 要連的 VPS 位址。

## 4. 產生金鑰、short ID 與密碼

在 VPS 的 root shell 執行：

```sh
umask 077
/usr/bin/aster-core generate reality-keypair
openssl rand -hex 8
openssl rand -hex 32
openssl rand -hex 32
```

第一個命令輸出：

```text
PrivateKey: <SERVER_PRIVATE_KEY>
PublicKey: <SERVER_PUBLIC_KEY>
```

後面三行依序作為：

1. `<SHORT_ID>`：`openssl rand -hex 8` 會產生 16 個 hex 字元，也就是 8 bytes。
2. `<ANYTLS_PASSWORD>`：32 bytes 的隨機密碼。
3. `<CONTROLLER_SECRET>`：獨立的 32 bytes Controller secret。

Aster 的 server short ID 欄位是清單，名稱為 `short-id`；每個值經 hex 解碼後最多 8 bytes。Client 則填單一 `reality-opts.short-id`。Short ID 必須是有效的偶數長度 hex，不要放 UUID 或任意文字。

::: warning 金鑰方向
Private key 只填在 VPS 的 `reality-config.private-key`。Client 只拿 Public key，填入 `reality-opts.public-key`。兩者填反時設定或握手會失敗。
:::

## 5. 寫入完整 server YAML

建立 root-only 設定目錄：

```sh
sudo install -d -m 700 /etc/mihomo
sudoedit /etc/mihomo/config.yaml
```

填入以下完整設定並替換 placeholder：

```yaml
mode: rule
log-level: info
ipv6: true

# Controller 只供本機檢查。這是一般 Controller secret，
# 不是 AnyTLS password，也不是 Aster Admin secret。
external-controller: 127.0.0.1:9090
secret: "<CONTROLLER_SECRET>"

listeners:
  - name: edge-anytls
    type: anytls
    listen: 0.0.0.0
    port: 443
    users:
      alice: "<ANYTLS_PASSWORD>"
    reality-config:
      dest: www.microsoft.com:443
      private-key: "<SERVER_PRIVATE_KEY>"
      short-id:
        - "<SHORT_ID>"
      server-names:
        - www.microsoft.com

rules:
  - MATCH,DIRECT
```

再收緊權限：

```sh
sudo chmod 600 /etc/mihomo/config.yaml
sudo chown root:root /etc/mihomo/config.yaml
```

這份 YAML 的關鍵對應如下：

| Server 欄位 | Client 對應 |
| --- | --- |
| DNS `<NODE_DOMAIN>` + `port` | `server` + `port` |
| `users.alice` 的值 | `password` |
| `reality-config.private-key` 的 public half | `reality-opts.public-key` |
| `reality-config.short-id` 其中一個值 | `reality-opts.short-id` |
| `reality-config.server-names` 其中一個值 | `sni` |

同一個 AnyTLS listener 只能使用一種 security mode。使用 `reality-config` 時，不要同時加入憑證用的 `certificate`/`private-key`、`shadow-tls`、`res-tls` 或 `jls-config`。

## 6. 驗證設定並啟動

先只解析設定：

```sh
sudo /usr/bin/aster-core \
  -d /etc/mihomo \
  -f /etc/mihomo/config.yaml \
  -t
```

成功時應看到類似：

```text
configuration file ... test is successful
```

如果使用專案提供的 systemd package：

```sh
sudo systemctl enable --now aster-core
sudo systemctl status --no-pager aster-core
sudo journalctl -u aster-core -n 50 --no-pager
```

Log 應包含 `AnyTLS[edge-anytls] proxy listening at`。再檢查實際 socket：

```sh
sudo ss -ltnp | grep ':443'
```

從另一台外部主機只測 TCP 可達性：

```sh
nc -vz <NODE_DOMAIN> 443
```

`succeeded` 只代表 DNS、防火牆與 TCP listener 正常，不代表 REALITY key、SNI、short ID 或 password 正確；完整驗證要由 Aster client 完成。

若不是 systemd 安裝，可先以前景模式觀察：

```sh
sudo /usr/local/bin/aster-core \
  -d /etc/mihomo \
  -f /etc/mihomo/config.yaml
```

## 7. 寫入完整 client YAML

在 client 建立獨立目錄，例如 `./aster-client`，並將下列內容存成 `config.yaml`：

```yaml
mixed-port: 7890
allow-lan: false
mode: rule
log-level: info
ipv6: false

external-controller: 127.0.0.1:9091
secret: "<LOCAL_CONTROLLER_SECRET>"

dns:
  enable: true
  listen: 127.0.0.1:1053
  ipv6: false
  enhanced-mode: redir-host
  default-nameserver:
    - 1.1.1.1
  proxy-server-nameserver:
    - 1.1.1.1
  nameserver:
    # 明確指定由 AnyTLS proxy 發出 UDP DNS，供下一節驗證 UoT。
    - "udp://1.1.1.1#edge-anytls-reality"

proxies:
  - name: edge-anytls-reality
    type: anytls
    server: <NODE_DOMAIN>
    port: 443
    password: "<ANYTLS_PASSWORD>"
    sni: www.microsoft.com
    client-fingerprint: chrome
    reality-opts:
      public-key: "<SERVER_PUBLIC_KEY>"
      short-id: "<SHORT_ID>"
    udp: true

rules:
  - MATCH,edge-anytls-reality
```

另外產生一個獨立的 `<LOCAL_CONTROLLER_SECRET>`；它只保護 client 本機 Controller：

```sh
openssl rand -hex 32
```

Client 的 `server` 是 VPS 節點網域，`sni` 才是 fallback/偽裝名稱。不要把兩者對調。REALITY client 必須使用 `client-fingerprint`；一般從 `chrome` 開始。

以下欄位不屬於這份 Aster outbound YAML：

```text
security: reality
realitySettings:
serverName:
shortIds:
privateKey:
```

`security=reality` 只出現在分享 URI 的 query。YAML 是否啟用 REALITY，由 `reality-opts.public-key` 判定。

## 8. 啟動 client

先驗證：

```sh
./aster-core \
  -d ./aster-client \
  -f ./aster-client/config.yaml \
  -t
```

再以前景模式啟動，第一次測試可暫時把 `log-level` 改成 `debug`：

```sh
./aster-core \
  -d ./aster-client \
  -f ./aster-client/config.yaml
```

預期本機有三個 socket：

- `127.0.0.1:7890`：HTTP/SOCKS mixed proxy。
- `127.0.0.1:1053`：測試用 DNS listener。
- `127.0.0.1:9091`：client Controller。

另一個 terminal 可確認：

```sh
curl \
  -H "Authorization: Bearer <LOCAL_CONTROLLER_SECRET>" \
  http://127.0.0.1:9091/version
```

## 9. 驗證 TCP

先透過 HTTP proxy 查出口：

```sh
curl --fail --show-error \
  --proxy http://127.0.0.1:7890 \
  https://api.ipify.org
echo
```

再透過 SOCKS 測一次：

```sh
curl --fail --show-error \
  --socks5-hostname 127.0.0.1:7890 \
  https://example.com/ \
  -o /dev/null \
  -w 'HTTP %{http_code}\n'
```

預期：

- 第一個命令顯示 VPS 的出口 IP，而不是 client 的原始 IP。
- 第二個命令取得正常 HTTP status。
- Client log 顯示規則選到 `edge-anytls-reality`。
- VPS log 沒有連續的 REALITY handshake 或 password 錯誤。

為避免既有 session 掩蓋問題，修改 password/key 後應完全重啟 client，或至少關閉相關連線再測。

## 10. 驗證 UDP over TCP（UoT）

本篇 client DNS 已把 `1.1.1.1:53/udp` 明確指定到 AnyTLS proxy。先在 VPS 開一個短暫觀察視窗：

```sh
sudo tcpdump -ni any 'udp and host 1.1.1.1 and port 53'
```

然後在 client 執行：

```sh
dig @127.0.0.1 -p 1053 example.com A +short
```

預期：

1. `dig` 回傳一個或多個 A records。
2. Client log 的 DNS 連線使用 `edge-anytls-reality`。
3. VPS 的 `tcpdump` 看見由 VPS 發往 `1.1.1.1:53` 的 UDP。
4. VPS 不需要收到 inbound UDP `443`；client 到 VPS 仍是 TCP `443`。

這個結果同時驗證了：

- Client `udp: true`。
- AnyTLS outbound 的 UoT。
- Server 能向外送出 UDP。
- 回程封包能透過 AnyTLS 回到 client。

如果 TCP 成功但這個測試失敗，先檢查 `udp: true`、`nameserver` 尾端的 `#edge-anytls-reality`、VPS outbound firewall 與 client log。不要用一般 `curl` 成功就推論 UDP 也正常。

## 11. 建立與核對分享連結

使用本篇產生的 hex password 時不需要額外 URL-encode。完整 Aster AnyTLS + REALITY URI 為：

```text
anytls://<ANYTLS_PASSWORD>@<NODE_DOMAIN>:443?security=reality&type=tcp&sni=www.microsoft.com&fp=chrome&pbk=<SERVER_PUBLIC_KEY>&sid=<SHORT_ID>#Aster-AnyTLS-REALITY
```

Query 與 YAML 的對應：

| URI query | Aster YAML |
| --- | --- |
| `security=reality` | 啟用 `reality-opts` 匯入 |
| `type=tcp` | AnyTLS transport |
| `sni` | `sni` |
| `fp` | `client-fingerprint` |
| `pbk` | `reality-opts.public-key` |
| `sid` | `reality-opts.short-id` |

如果 password 不是本篇的純 hex，而包含 `@`、`:`、`/`、`?`、`#` 或 `%`，必須先做 URL userinfo percent-encoding，不能直接字串拼接。

匯入後請核對產生的 outbound 至少有：

```yaml
type: anytls
server: <NODE_DOMAIN>
port: 443
password: "<ANYTLS_PASSWORD>"
sni: www.microsoft.com
client-fingerprint: chrome
reality-opts:
  public-key: "<SERVER_PUBLIC_KEY>"
  short-id: "<SHORT_ID>"
udp: true
```

## 12. 上線安全檢查

- REALITY private key 永遠只留在 server；client、訂閱與分享連結只使用 public key。
- 每位使用者使用不同的 AnyTLS password。需要即時新增、停用與輪替時，使用[受管使用者教學](/tutorials/user-management)。
- Controller 保持 `127.0.0.1`。不要為了遠端面板把明文 Controller 改成 `0.0.0.0:9090`。
- 不要加入 `skip-cert-verify: true` 來掩蓋錯誤的 public key、SNI 或 short ID。
- 不要把 REALITY 與憑證 TLS、ShadowTLS、ResTLS、JLS 疊在同一 listener/outbound。
- 對 `config.yaml` 使用 `0600`，備份也要加密並限制存取。
- 定期檢查 NTP、DNS 是否仍指向正確 VPS、fallback 是否仍可從 VPS 連線。
- 變更 private key、SNI 或 short ID 時，所有 client 與分享連結都必須同步更新。

## 13. 排錯順序

| 症狀 | 優先檢查 |
| --- | --- |
| DNS 查不到 | A record、TTL、nameserver delegation |
| TCP timeout | 雲端 security group、本機 firewall、路由、錯誤 IP |
| TCP refused | Aster 未啟動、port 寫錯、listener 未綁定 |
| 連到 Cloudflare/CDN | DNS record 必須是 DNS only |
| `-t` 顯示 invalid private key | private key 填反、Base64URL 被截斷或多了空白 |
| `invalid short ID` | 不是偶數長度 hex，或解碼後超過 8 bytes |
| REALITY handshake 失敗 | public/private key 不成對、SNI 不在 `server-names`、short ID 不匹配、時鐘偏差 |
| AnyTLS authentication 失敗 | `password` 與 server `users` 的值不同 |
| TCP 正常、UDP 失敗 | Client `udp: true`、UoT 測試路徑、VPS outbound UDP |
| 重啟後 443 無法綁定 | Caddy/Nginx/其他程序占用；用 `ss -ltnp` 查 owner |
| Fallback 異常 | VPS 到 `dest` 的 DNS/TCP/TLS、`dest` 與 SNI 是否合理 |

逐層測試，不要一次更換 key、password、SNI、short ID、DNS 與 port。建議順序是：

1. DNS 是否解析到正確 VPS。
2. 外部 TCP 是否可到 listener。
3. Server `-t` 與 socket 是否正常。
4. Public/private key 是否成對。
5. `sni`、`server-names`、`short-id` 是否逐字匹配。
6. AnyTLS password 是否一致。
7. TCP proxy。
8. UDP/UoT。

需要分享診斷資料時，只提供版本、已遮罩設定、錯誤文字與測試層級；private key、password、完整 URI 都要移除。

## 14. 回復部署

如果上線後要回到變更前：

1. 先保留目前 log 與已遮罩的設定副本。
2. 停止 Aster，或還原先前經 `-t` 驗證的 `config.yaml`。
3. 重新啟動原服務並確認原本 socket/網站恢復。
4. 確認沒有 client 再使用後，才移除新增的 TCP firewall rule。
5. 最後刪除或改回 `<NODE_DOMAIN>` 的 DNS record。

使用 systemd 時：

```sh
sudo systemctl stop aster-core
sudo cp /etc/mihomo/config.yaml.before-anytls /etc/mihomo/config.yaml
sudo chmod 600 /etc/mihomo/config.yaml
sudo /usr/bin/aster-core -d /etc/mihomo -t
sudo systemctl start aster-core
sudo systemctl status --no-pager aster-core
```

只有在你事前確實建立了 `config.yaml.before-anytls` 時才執行上述 `cp`。不要用空白檔或不確定來源覆蓋正式設定。

## 下一步

- [即時管理 VLESS/AnyTLS 使用者與訂閱](/tutorials/user-management)
- [AnyTLS + REALITY 欄位參考](/reference/anytls-reality)
- [Aster 與 Mihomo 差異](/reference/mihomo-differences)
- [Linux 與 systemd](/deployment/linux)
- [疑難排解](/troubleshooting)
