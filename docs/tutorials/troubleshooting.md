# 生產環境故障排除

本篇是症狀導向的 runbook。先保留證據，再做最小變更；每次只改一件事並記錄結果。不要從「刪除 state」、「清空 firewall」、「關閉 safe path」或「重新安裝整台 VPS」開始。

## 先確認你使用哪一種部署

後續命令同時涵蓋兩種常見 layout：

| 項目 | 專用帳號教學 | 官方 `.deb` package |
| --- | --- | --- |
| Unit | `aster-core.service` | `aster-core.service` |
| Binary | `/opt/aster-core/current/aster-core` | `/usr/bin/aster-core` |
| Home | `/var/lib/aster-core` | `/etc/mihomo` |
| Config | `/etc/aster-core/config.yaml` | `/etc/mihomo/config.yaml` |
| 預期使用者 | `aster-core` | `root` |

先讓 systemd 告訴你實際值，不要只靠記憶：

```sh
sudo systemctl cat aster-core.service
sudo systemctl show aster-core.service \
  -p FragmentPath -p DropInPaths -p User -p Group -p ExecStart -p Environment
readlink -f /opt/aster-core/current 2>/dev/null || true
```

若 `ExecStart`、使用者或路徑與預期不同，先釐清 package unit、手動 unit 與 drop-in 的優先順序。

## 前十分鐘：固定順序檢查

### 1. 先做設定驗證

專用帳號部署：

```sh
sudo -u aster-core \
  env SAFE_PATHS=/etc/aster-core \
  /opt/aster-core/current/aster-core \
  -d /var/lib/aster-core \
  -f /etc/aster-core/config.yaml \
  -t
```

官方 package：

```sh
sudo /usr/bin/aster-core -d /etc/mihomo -t
```

不要只用 root 測試專用帳號部署。root 能讀檔不代表 `aster-core` 使用者能讀 certificate、provider、config 或 state parent。

`-t` 成功只代表靜態解析成功；port 衝突、網路、DNS、REALITY 對端、TUN capability 與 firewall 都仍可能在 runtime 失敗。

### 2. 確認 service 與最近 log

```sh
sudo systemctl status aster-core.service --no-pager
sudo systemctl show aster-core.service \
  -p ActiveState -p SubState -p Result -p ExecMainStatus -p NRestarts
sudo journalctl -u aster-core.service -b --no-pager
```

若 log 太多，縮小到事故時間：

```sh
sudo journalctl -u aster-core.service \
  --since '2026-01-01 12:00:00' \
  --until '2026-01-01 12:15:00' \
  --no-pager
```

把時間換成實際事故區間。不要一開始就用無限 `-f`，否則容易漏掉服務啟動前的第一個錯誤。

### 3. 確認 process 與 socket

```sh
sudo systemctl show aster-core.service -p MainPID
sudo ss -lntup
```

針對常見 port：

```sh
sudo ss -lntp 'sport = :443'
sudo ss -lntp 'sport = :9090'
sudo ss -lnup
```

### 4. 確認 DNS、時間與磁碟

```sh
getent ahosts proxy.example.com
timedatectl status
df -h
df -i
```

把網域換成實際節點／服務端 hostname。磁碟空間或 inode 用盡會讓 state、cache、provider 與 log 寫入失敗。

## 症狀：`-t` 失敗

### 找不到設定或讀錯檔案

先看 `ExecStart`，再用絕對路徑重現：

```sh
pwd
sudo systemctl show aster-core.service -p ExecStart
sudo ls -l /etc/aster-core/config.yaml
```

`-f` 的相對路徑以 process 當下 working directory 解析，不是以 `-d` 解析；未指定 `-f` 時才讀 `<home>/config.yaml`。生產 unit 建議同時使用絕對 `-d` 與絕對 `-f`。

### `path is not subpath of home directory or SAFE_PATHS`

設定引用了 home 以外的 certificate、private key、provider 或 store。處理順序：

1. 把敏感檔案放進受控的 home 或 `/etc/aster-core`。
2. 確認 service account 有讀取權。
3. 確實需要其他可信目錄時，將精確根目錄加入 `SAFE_PATHS`。

檢查 unit 的 environment：

```sh
sudo systemctl show aster-core.service -p Environment
```

不要用 `SKIP_SAFE_PATH_CHECK=true` 掩蓋路徑設計問題。

### YAML 或欄位錯誤

常見來源：

- Tab、縮排或引號錯誤。
- Listener name 重複。
- `aster.managed-listeners` 指向不存在或不支援管理的 listener。
- `aster.secret` 少於 32 bytes，或有前後空白。
- `public-base-url` 不是絕對 HTTPS URL，或含 query／fragment。
- `store` 指向 safe path 以外。
- 同一 listener 同時設定 REALITY 與另一種互斥 security mode。
- REALITY private key 不是 32-byte X25519 base64url key。
- Short ID 不是 hex、解碼後超過 8 bytes。

只修正錯誤訊息指出的最小範圍，再重跑與正式服務完全相同的 `-t`。

## 症狀：service 一直重啟或 `status=1/FAILURE`

先取得第一個失敗與 restart count：

```sh
sudo systemctl show aster-core.service \
  -p Result -p ExecMainCode -p ExecMainStatus -p NRestarts
sudo journalctl -u aster-core.service -b -n 200 --no-pager
```

常見判斷：

| Log／狀態 | 優先檢查 |
| --- | --- |
| `permission denied` | unit 使用者、config／certificate／state owner 與 mode |
| `address already in use` | port 已被另一個 process 占用 |
| `not subpath`／`SAFE_PATHS` | home、引用路徑與 unit environment |
| `load Aster state` | state owner、mode、symlink、JSON、version |
| `operation not permitted` | 低 port、TUN、routing 所需 capability |
| `no such file or directory` | symlink target、ExecStart、certificate/provider path |

若 restart loop 讓 log 很吵，可先停止服務進行離線檢查：

```sh
sudo systemctl stop aster-core.service
```

完成 `-t` 與權限檢查後再啟動，不要用不斷 restart 代替診斷。

## 症狀：`address already in use` 或外部連不上

找出精確 owner：

```sh
sudo ss -lntp 'sport = :443'
sudo ss -lntp 'sport = :9090'
sudo systemctl list-units --type=service --state=running
```

若 443 已被 Caddy、Nginx、HAProxy 或另一個代理使用：

- 改用另一個 listener port；或
- 先設計支援該協定的 TCP／SNI 分流。

一般 HTTP reverse proxy 不能把 AnyTLS + REALITY 當成普通網站路徑轉發。不要停止未知服務或殺掉 PID；先用 `systemctl status <unit>` 確認它的用途與 owner。

若 server 已監聽但外部仍 timeout，依序檢查：

1. VPS provider security group／cloud firewall。
2. Host firewall。
3. DNS 是否指向這台 VPS。
4. AAAA 是否指向未正常配置的 IPv6。
5. Client 所在網路是否封鎖該 port。

檢查目前規則，不要清空 ruleset：

```sh
sudo ufw status verbose
sudo nft list ruleset
```

從另一個網路測 TCP reachability：

```sh
nc -vz proxy.example.com 443
```

`nc` 成功只證明 TCP 可建立，不代表 REALITY authentication 成功。

## 症狀：DNS 解析錯誤、時好時壞或只在部分網路失敗

由 client 與 server 分別查：

```sh
getent ahosts proxy.example.com
dig +short A proxy.example.com
dig +short AAAA proxy.example.com
dig @1.1.1.1 +short A proxy.example.com
dig @1.1.1.1 +short AAAA proxy.example.com
```

若未安裝 `dig`：

```sh
sudo apt install --yes dnsutils
```

判讀：

- Public resolver 與本機結果不同：可能是 cache、split DNS 或 hosts override。
- A 正確、AAAA 錯誤：支援 IPv6 的 client 可能優先走壞的 AAAA。
- 回傳 CDN address 而非 VPS：DNS record 可能開了 HTTP proxy。
- NXDOMAIN：record 名稱、zone 或 nameserver delegation 錯誤。

AnyTLS + REALITY 是 raw TCP；Cloudflare 等一般橘雲／HTTP CDN proxy 通常不會直接轉發。沒有對應的 L4 產品時，將 proxy server record 設為 DNS-only。

Server 內部 DNS 問題再查：

```sh
cat /etc/resolv.conf
resolvectl status 2>/dev/null || true
resolvectl query www.microsoft.com 2>/dev/null || true
```

若 Aster 使用 `nameserver: system`，system resolver 本身必須能用。TUN DNS hijack 環境也要確認 Aster 的 upstream query 沒有再次被自己攔截。

## 症狀：REALITY handshake 失敗

先確認四個最容易混淆的值：

| Client 欄位 | 正確來源 |
| --- | --- |
| `server` | Aster VPS 的 IP／網域 |
| `sni` | Server `reality-config.server-names` 其中一個值 |
| `reality-opts.public-key`／`pbk` | Server key pair 的 **PublicKey** |
| `reality-opts.short-id`／`sid` | Server `short-id` 其中一個值 |

Client 還應明確設定：

```yaml
client-fingerprint: chrome
```

### 時鐘

REALITY 對時間敏感。Server：

```sh
date -u
timedatectl status
timedatectl show -p NTPSynchronized -p TimeUSec
systemctl status systemd-timesyncd.service --no-pager 2>/dev/null || true
chronyc tracking 2>/dev/null || true
```

Client 也必須同步時間。不要靠把 `max-time-difference` 設成極大值長期掩蓋 NTP 故障；該欄位單位是 microseconds。

### SNI 與 fallback destination

確認 VPS 可解析並連到 `dest`：

```sh
getent ahosts www.microsoft.com
timeout 10 openssl s_client \
  -connect www.microsoft.com:443 \
  -servername www.microsoft.com \
  </dev/null
```

把 host 換成實際 `dest`／SNI。`dest` 應是行為穩定且與 SNI 合理對應的 TLS 站點。

直接用普通 `openssl s_client` 連 Aster 的 REALITY port 只會走未通過驗證的 fallback 行為，不能證明 REALITY client authentication 成功。最終測試必須使用支援相同 REALITY 欄位的 Aster client。

### Public key 與 short ID

- X25519 public／private key 是 raw URL-safe base64，兩者不可互換。
- `sid` 是 hex 字串，必須為偶數長度，解碼後最多 8 bytes。
- Client `sid` 必須完全匹配 server 清單；server 若配置空 short ID，client 也要依實際設定處理。
- 更換 server private key 後，所有 client 的 public key 與既有訂閱都必須更新。
- 更換 SNI／short ID 後，client 與訂閱也必須同步。

不要用 `skip-cert-verify` 嘗試修復錯誤的 `pbk`、SNI 或 `sid`；REALITY 問題不會因此正確解決。

### 只看到 timeout

同時檢查：

```sh
sudo ss -lntp 'sport = :443'
sudo journalctl -u aster-core.service --since '-10 minutes' --no-pager
sudo ufw status verbose
```

若 TCP 根本沒到 server，先處理 DNS／firewall／port；只有 TCP 已到且 log 顯示 handshake 問題時，才集中比對 REALITY 欄位。

## 症狀：Controller 或 Aster Admin 回傳 401／403／404

### 先分清兩個 token

```yaml
secret: "<controller-secret>"

aster:
  secret: "<另一個至少 32 bytes 的 aster-secret>"
```

- `/version`、`/configs`、`/proxies` 等 Clash-compatible API 使用 Controller `secret`。
- `/api/admin/*` 使用 `aster.secret`。
- `/sub/aster/{token}` 使用每個 user 的 subscription token，不使用 Bearer token。

不要在終端使用 `curl -v` 測帶 token 的 request。它可能顯示 Authorization header。可從隱藏輸入傳給 curl stdin config：

```sh
read -r -s ASTER_ADMIN_TOKEN
printf '\n'
printf 'header = "Authorization: Bearer %s"\n' "${ASTER_ADMIN_TOKEN}" |
  curl --config - \
    --silent --show-error \
    --output /dev/null \
    --write-out '%{http_code}\n' \
    http://127.0.0.1:9090/api/admin/status
unset ASTER_ADMIN_TOKEN
```

一般 Controller 可用相同方式單獨驗證；這次輸入的是頂層 `secret`：

```sh
read -r -s ASTER_CONTROLLER_TOKEN
printf '\n'
printf 'header = "Authorization: Bearer %s"\n' "${ASTER_CONTROLLER_TOKEN}" |
  curl --config - \
    --silent --show-error \
    --output /dev/null \
    --write-out '%{http_code}\n' \
    http://127.0.0.1:9090/version
unset ASTER_CONTROLLER_TOKEN
```

若 `/version` 成功而 `/api/admin/status` 是 401，transport 與 Controller secret 沒問題，應集中檢查 `aster.secret`。反過來也不要把 Aster token 拿去呼叫一般 Controller API。

### 401 Unauthorized

優先檢查：

- 把 Controller secret 當成 Aster secret，或反過來。
- `Bearer` 拼字、大小寫或空格錯誤。
- Secret 複製到額外換行或前後空白。
- Reverse proxy／BFF 沒有送出 Authorization header。

不要把 token 放進 URL query、shell history、前端 bundle 或 issue。

### 403 Forbidden

Aster Admin 執行 same-origin 防護。檢查：

- Browser `Origin` 是否與 request scheme／host 相同。
- `Sec-Fetch-Site` 是否是 `cross-site`。
- Reverse proxy 是否覆寫正確的 `Host`。
- HTTPS proxy 是否傳遞正確 `X-Forwarded-Proto: https`。
- Proxy upstream 是否是 loopback Controller。

只有 request 來自 loopback 時，Aster 才採信第一個 `X-Forwarded-Proto`。建議面板透過同 origin backend／BFF 呼叫，不要讓 browser 直接跨站帶 Aster token。

### 404 Not Found

檢查：

- Config 是否真的有 `aster` block。
- `managed-listeners` 是否初始化成功。
- Request 是否打到正確 Controller port／socket。
- 明文 `external-controller` 是否綁在非 loopback。

基於安全設計，Aster Admin 只掛載於：

- Loopback 明文 Controller。
- HTTPS Controller。
- Unix socket。
- Windows named pipe。

若把明文 Controller 綁到 `0.0.0.0`，Admin route 不會掛載；不要為了得到 route 而把整個 Controller 暴露到 Internet。

## 症狀：Aster mutation 回傳 409

409 代表 listener revision 或 store generation conflict，不是一般網路重試。

正確處理：

1. 重新 GET `/api/admin/inbounds`。
2. 若修改既有 user，重新 GET `/api/admin/users/{id}`。
3. 比較另一個管理者或程序已提交的變更。
4. 合併使用者意圖。
5. 使用最新 listener revision 重新送出 mutation。

不要只把舊 revision 加一後重送，也不要無限自動重試；那可能覆蓋另一位管理者的更新。

如果單一管理者也持續 409，查是否有兩個 Aster process 共用 state：

```sh
sudo systemctl list-units 'aster-core*' --all
sudo systemctl list-unit-files 'aster-core*'
sudo fuser -v /var/lib/aster-core/aster-state.json.lock 2>/dev/null || true
sudo fuser -v /etc/mihomo/aster-state.json.lock 2>/dev/null || true
```

`fuser` 由 Debian／Ubuntu 的 `psmisc` package 提供；若未安裝，可先用 `sudo apt install --yes psmisc`。它只用來識別 owner，不要直接對結果加 `-k`。

多實例必須各自使用不同：

- Store path。
- Listener／proxy ports。
- Controller address／Unix socket。
- TUN device 與 routing resources。

不要讓兩個 instance 輪流寫同一份 state，也不要用刪除 `.lock` 來繞過仍在執行的 owner。

## 症狀：Store permission、owner 或 JSON 錯誤

### 只做 metadata 檢查

專用帳號 layout：

```sh
sudo namei -l /var/lib/aster-core/aster-state.json
sudo stat -c '%U:%G %a %F %n' /var/lib/aster-core
sudo find /var/lib/aster-core \
  -maxdepth 1 \
  -name 'aster-state.json*' \
  -printf '%u:%g %m %y %p\n'
```

Package layout：

```sh
sudo namei -l /etc/mihomo/aster-state.json
sudo stat -c '%U:%G %a %F %n' /etc/mihomo
sudo find /etc/mihomo \
  -maxdepth 1 \
  -name 'aster-state.json*' \
  -printf '%u:%g %m %y %p\n'
```

預期：

- Parent 是 real directory，不是 symlink。
- Parent owner 等於 service user。
- Parent 不可被 group／other 寫入；專用帳號教學使用 `0700`。
- State 是 regular file，不是 symlink。
- State owner 等於 service user。
- State 不得有任何 group／other permission；通常是 `0600`。

### 在離線狀態修正權限

先停止服務並建立受保護的 forensic copy，不要先刪檔：

```sh
ASTER_FORENSIC_STAMP="$(date -u +%Y%m%dT%H%M%SZ)"
sudo systemctl stop aster-core.service
sudo install -d -o root -g root -m 0700 \
  "/var/backups/aster-core-forensics/${ASTER_FORENSIC_STAMP}"
sudo cp -a /var/lib/aster-core \
  "/var/backups/aster-core-forensics/${ASTER_FORENSIC_STAMP}/var-lib-aster-core"
```

專用帳號 layout 可修正：

```sh
sudo chown aster-core:aster-core /var/lib/aster-core
sudo chmod 0700 /var/lib/aster-core
sudo find /var/lib/aster-core \
  -maxdepth 1 \
  -type f \
  -name 'aster-state.json*' \
  -exec chown aster-core:aster-core {} +
sudo find /var/lib/aster-core \
  -maxdepth 1 \
  -type f \
  -name 'aster-state.json*' \
  -exec chmod 0600 {} +
```

再以 service account 跑 `-t`，啟動並讀 journal。Package layout 的預期 owner 是 root，不要照抄 `aster-core:aster-core`。

### Primary／backup 損壞

Aster 會驗證：

```text
aster-state.json
aster-state.json.bak
```

若兩份都有效，使用 generation 較新者；若只有一份有效，使用有效者，後續 commit 可修復 redundancy。

不要：

- 同時刪除 primary 與 `.bak`。
- 手動把 `.bak` 隨意覆蓋到 primary 而不保留證據。
- 修改 JSON `version` 繞過相容性檢查。
- 把 state 上傳到 issue；它含 UUID、password、token 與流量資料。

若錯誤是 `unsupported Aster state version`，使用能讀取該 state 的 binary，或從同一 upgrade 前的配對備份回復 binary、config 與 state。

### Mutation 回傳 500

查看事故時間 journal，再檢查：

```sh
df -h /var/lib/aster-core
df -i /var/lib/aster-core
findmnt -no TARGET,OPTIONS /var/lib/aster-core
```

常見原因是唯讀 filesystem、空間／inode 用盡、owner／mode 被部署工具改掉，或 runtime apply 失敗。不要把 500 當成可安全無限重試。

## 症狀：TCP 正常，但 UDP、DNS 或遊戲流量失敗

先理解 AnyTLS 的 UDP 模型：

- AnyTLS listener 是 TCP。
- Client `udp: true` 啟用 UoT，把 UDP payload 放進 AnyTLS transport。
- Server 不一定會出現 `443/udp` socket；只有 `443/tcp` 是正常的。

因此「`ss -lnup` 看不到 443」不是 AnyTLS UDP 失敗的證據。

依序分層：

1. Client node 是否有 `udp: true`。
2. TCP 經同一 proxy 是否正常。
3. Server 能否直接進行 UDP DNS query。
4. Server egress firewall／provider 是否擋 UDP。
5. Aster route／rule 是否把 UDP 送到不支援 UDP 的 outbound。
6. TUN／MTU／IPv6 是否只影響較大的 datagram。

Server 比較 UDP 與 TCP DNS：

```sh
dig @1.1.1.1 example.com
dig @1.1.1.1 +tcp example.com
```

若 TCP DNS 成功、UDP DNS timeout，先查 server egress firewall 與供應商限制。若兩者都成功但 client UoT 失敗，查 client log、node `udp`、rule 命中與 outbound 能力。

若只有 TUN 模式失敗：

```sh
ls -l /dev/net/tun
sudo systemctl show aster-core.service \
  -p User -p AmbientCapabilities -p CapabilityBoundingSet
ip tuntap
ip rule
ip route show table all
```

TUN／transparent routing 通常需要 `CAP_NET_ADMIN` 與適當裝置存取。不要同時讓 TUN auto-route／auto-redirect 和外部 iptables 規則接管相同流量。

OpenWrt + Nikki + `kernel-direct` 另見 [OpenWrt 與 Nikki](/deployment/openwrt) 與[疑難排解](/troubleshooting)：

- 延遲測試全失敗：`auto-detect-interface` 在雙 WAN 上必須是 `false`。
- DIRECT 與未綁定節點同時掛掉：不要丟 inbound REDIR／TUN SYN。
- 連線數／記憶體暴漲：查零位元組 TCP 與 `inet4_route_exclude_address_set`，不要 `DELETE /connections`。

Packet capture 可能暴露目的 IP、DNS 名稱與使用模式。只有在取得授權且了解隱私影響時才擷取；不要直接把原始 pcap 上傳到公開 issue。

## 症狀：設定 reload 後沒有生效

先驗證，再 reload：

```sh
sudo -u aster-core \
  env SAFE_PATHS=/etc/aster-core \
  /opt/aster-core/current/aster-core \
  -d /var/lib/aster-core \
  -f /etc/aster-core/config.yaml \
  -t
sudo systemctl reload aster-core.service
sudo journalctl -u aster-core.service --since '-5 minutes' --no-pager
```

Package 模式把第一段換成 `/usr/bin/aster-core -d /etc/mihomo -t`。

若啟動來源是 `--config '<base64>'` 或 `-f -`，SIGHUP 只能重新套用原始 bytes，不能從磁碟取得新內容。生產部署要支援檔案 reload，應使用正常 `-f /absolute/config.yaml`。

只有 AnyTLS password 等 managed credential 可由 Aster API 即時更新。REALITY private key、SNI、short ID、listener port 等 transport 設定仍由 YAML reload 管理。

## 症狀：升級後才出現問題

先回答四個問題：

1. `aster-core -v` 現在是哪個版本與架構？
2. systemd 實際執行哪個 binary？
3. Upgrade 前備份與舊 binary 是否仍在？
4. 問題是啟動失敗，還是只有特定協定／UDP／API 失敗？

```sh
/opt/aster-core/current/aster-core -v 2>/dev/null || /usr/bin/aster-core -v
sudo systemctl show aster-core.service -p ExecStart
readlink -f /opt/aster-core/current 2>/dev/null || true
sudo journalctl -u aster-core.service --since '-30 minutes' --no-pager
```

版本目錄部署可先只回退 binary：

```sh
sudo systemctl stop aster-core.service
sudo ln -sfn /opt/aster-core/releases/<上一個已驗證版本> /opt/aster-core/current
sudo systemctl start aster-core.service
sudo systemctl status aster-core.service --no-pager
```

`<上一個已驗證版本>` 必須換成 `readlink`／部署紀錄中已確認存在的精確目錄；不要猜版本。

如果舊 binary 明確拒絕目前 state version，再停止服務並使用同一次 upgrade 前的配對備份。回復 state 會回退帳號、token、流量與 revision；先保留目前 state 的 forensic copy。

Package downgrade 應使用已驗證、仍保留 checksum 的舊 `.deb`，並先閱讀 release notes。不要用 `apt remove --purge` 當作回退，它可能讓設定與 package 狀態更難還原。

## 症狀：Subscription 404 或內容不可用

檢查：

- `aster.public-base-url` 是否存在且為 HTTPS。
- User 是否 enabled。
- Subscription token 是否剛 rotate；舊 token 會立即失效。
- Listener 是否仍在 `managed-listeners`。
- Listener 是否有可輸出的 port 與 security。
- VLESS／AnyTLS REALITY 欄位是否完整。
- 是否使用不能輸出的 ShadowTLS、ResTLS、JLS 或 advanced XHTTP 組合。

Subscription URL 本身就是 access capability。不要把完整 URL放進 access log、analytics、Referer、截圖或 issue。Reverse proxy 不應覆寫 Aster 的 `Cache-Control: no-store`。

## 收集可分享的診斷資料

先建立 owner-only 目錄：

```sh
umask 077
ASTER_DIAG_DIR="$(mktemp -d)"
printf '%s\n' "${ASTER_DIAG_DIR}"
```

收集不直接包含 config／state 的基礎資料：

```sh
uname -a > "${ASTER_DIAG_DIR}/uname.txt"
(
  /opt/aster-core/current/aster-core -v 2>/dev/null ||
  /usr/bin/aster-core -v
) > "${ASTER_DIAG_DIR}/version.txt"
sudo systemctl show aster-core.service \
  -p FragmentPath -p DropInPaths -p User -p Group \
  -p ExecStart -p ActiveState -p SubState -p Result \
  > "${ASTER_DIAG_DIR}/systemd-show.txt"
sudo systemctl cat aster-core.service \
  > "${ASTER_DIAG_DIR}/systemd-unit.txt"
sudo journalctl -u aster-core.service \
  --since '-30 minutes' \
  --no-pager \
  > "${ASTER_DIAG_DIR}/journal.txt"
sudo ss -lntup > "${ASTER_DIAG_DIR}/sockets.txt"
ip address show > "${ASTER_DIAG_DIR}/ip-address.txt"
ip route show table all > "${ASTER_DIAG_DIR}/ip-route.txt"
```

即使是這些檔案，也要人工逐行檢查後才分享。Journal、unit environment、IP 與 route 可能含內部拓撲或 token。

另外提供一份**人工建立的最小 config**，不要直接複製 production config 再假設自動遮罩完整。至少移除或替換：

- Controller `secret`。
- `aster.secret`。
- UUID 與 AnyTLS password。
- REALITY／WireGuard／certificate private keys。
- Age secret key。
- Subscription token 與完整 `/sub/aster/...` URL。
- Provider URL 中的 userinfo、query token 或 signed URL。
- 不希望公開的 server hostname／IP。

不要收集或公開：

- `aster-state.json`、`.bak` 或 `.lock`。
- 未遮罩的 config。
- Authorization header。
- Browser local storage／password manager export。
- 原始 pcap。
- 包含訂閱 URL 的 reverse-proxy access log。

Issue 中應清楚寫出：

1. 作業系統、架構與 Aster 完整 `-v`。
2. 部署方式與 systemd 實際 `ExecStart`。
3. 去除秘密的最小 config。
4. 相同使用者／路徑執行 `-t` 的完整結果。
5. 問題只影響 TCP、UDP、DNS、IPv4、IPv6、REALITY、Controller 或特定 listener 的哪一部分。
6. 可重現步驟、預期結果、實際結果與精確事故時間。
