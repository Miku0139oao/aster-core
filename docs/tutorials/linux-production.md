# Debian／Ubuntu VPS 生產部署

::: warning 可選的 Aster 服務端部署
Aster Core 的主要用途是客戶端；一般節點服務端使用 Xray、sing-box 或 SideraCore。只有在測試、全 Aster 環境或確實需要 Aster listener／管理 API 時，才需要本篇 VPS 流程。一般桌面、路由器或閘道客戶端請先看[第一個代理設定](/tutorials/first-proxy)。
:::

本篇從 GitHub Release 的 Linux binary asset 開始，建立一套可驗證、使用專用帳號、由 systemd 管理，而且能安全更新與回退的可選 Aster Core 服務端。範例以 AnyTLS + REALITY listener 為主。

以下有兩種安裝模式，請選一種，不要交叉混用路徑或 unit：

| 模式 | Binary | Home／state | 設定 | 執行身分 |
| --- | --- | --- | --- | --- |
| 本篇主要流程：原始 `.gz` | `/opt/aster-core/current/aster-core` | `/var/lib/aster-core` | `/etc/aster-core/config.yaml` | 專用 `aster-core` 使用者 |
| 官方 `.deb` 套件 | `/usr/bin/aster-core` | `/etc/mihomo` | `/etc/mihomo/config.yaml` | 目前隨附 unit 以 root 執行 |

::: warning 套件 unit 的實際狀態
Repository 目前的 `.github/release/aster-core.service` 沒有 `User=` 或 `Group=`，並授予多項 capabilities。若使用 `.deb`，應以該 unit 的實際行為為準；不要假設它會以本篇建立的專用帳號執行。本篇稍後另列出套件安裝方式。
:::

## 1. 上線前準備

準備以下資訊：

- VPS 的 Debian 或 Ubuntu 版本與 CPU 架構。
- Release tag `Prerelease-main`，以及該 release 的 `version.txt` 所列 asset build ID。
- AnyTLS 公開連接埠；範例使用 TCP `443`。
- 指向 VPS 的 DNS A／AAAA record。
- REALITY `dest`、允許的 SNI、server key pair、short ID。
- 一條仍可使用的 SSH 工作階段。調整 firewall 前不要關閉它。

先更新 CA 與下載工具：

```sh
sudo apt update
sudo apt install --yes ca-certificates curl gzip openssl
```

確認平台：

```sh
uname -m
dpkg --print-architecture
lscpu
```

常見 release flavor：

| 主機 | 建議 flavor | 備註 |
| --- | --- | --- |
| 一般 x86-64 | `amd64-v1` | 相容範圍最廣，適合不確定 CPU 指令集時 |
| 已確認較新 x86-64 | `amd64-v2`／`amd64-v3` | 只有確認 CPU 支援時才選 |
| ARM64 | `arm64` | `uname -m` 通常是 `aarch64` |
| 32-bit ARMv7 | `armv7` | `dpkg --print-architecture` 通常是 `armhf` |

Release workflow 中不帶版本尾碼的 `amd64` 目前是 GOAMD64 v3，不是通用 x86-64。`amd64-compatible` 與 `amd64-v1` 目前都對應 GOAMD64 v1；新部署優先使用較明確的 `amd64-v1`。

::: warning 目前只有 rolling prerelease
Aster 尚未發布帶版本號的正式版。請到[下載](/downloads)使用 [`Prerelease-main`](https://github.com/Miku0139oao/aster-core/releases/tag/Prerelease-main)。該 tag 的資產名是 `aster-core-linux-amd64-v1-alpha-main-<sha>.gz`；下方所有 build ID 請以同一個 release 的 `version.txt` 為準。
:::

## 2. 下載並驗證 release

目前沒有 Aster `v*` 正式 release；公開 asset 位於會隨 `main` 更新的 `Prerelease-main`。先讀取同一 release 的 `version.txt`，把下列 `SHA7` 改成其中的七碼 commit 後綴。檔名格式來自 repository 的 release workflow：

```sh
ASTER_RELEASE_TAG='Prerelease-main'
ASTER_BUILD_ID='alpha-main-SHA7'
ASTER_ASSET_FLAVOR='amd64-v1'
ASTER_ASSET_NAME="aster-core-linux-${ASTER_ASSET_FLAVOR}-${ASTER_BUILD_ID}.gz"
ASTER_DOWNLOAD_DIR="$(mktemp -d)"
```

下載 binary 與同一個 Release 的 `checksums.txt`：

```sh
curl --proto '=https' --tlsv1.2 --fail --location \
  --output "${ASTER_DOWNLOAD_DIR}/${ASTER_ASSET_NAME}" \
  "https://github.com/Miku0139oao/aster-core/releases/download/${ASTER_RELEASE_TAG}/${ASTER_ASSET_NAME}"

curl --proto '=https' --tlsv1.2 --fail --location \
  --output "${ASTER_DOWNLOAD_DIR}/checksums.txt" \
  "https://github.com/Miku0139oao/aster-core/releases/download/${ASTER_RELEASE_TAG}/checksums.txt"
```

確認 checksum 清單中剛好有一筆目標 asset，再驗證內容與 gzip：

```sh
test "$(grep -Fc "  ./${ASTER_ASSET_NAME}" "${ASTER_DOWNLOAD_DIR}/checksums.txt")" -eq 1
(
  cd "${ASTER_DOWNLOAD_DIR}"
  sha256sum --check --ignore-missing checksums.txt
)
gzip --test "${ASTER_DOWNLOAD_DIR}/${ASTER_ASSET_NAME}"
```

任何一步失敗都先停止。不要在 checksum 不符時改用 `--insecure`、略過驗證或繼續安裝。

解壓到只有目前使用者可存取的暫存目錄，並確認版本：

```sh
umask 077
gzip --decompress --stdout "${ASTER_DOWNLOAD_DIR}/${ASTER_ASSET_NAME}" \
  > "${ASTER_DOWNLOAD_DIR}/aster-core"
chmod 0755 "${ASTER_DOWNLOAD_DIR}/aster-core"
"${ASTER_DOWNLOAD_DIR}/aster-core" -v
```

輸出的版本應與 `version.txt` 及 asset 檔名中的 `${ASTER_BUILD_ID}` 相同；它不會顯示可變的 release tag `Prerelease-main`。若 build ID 或架構不符，回到 Release 頁重新選擇 asset。

## 3. 以版本目錄安裝 binary

使用版本目錄可讓回退只需切換一個 symlink，不必覆寫唯一一份 binary：

```sh
sudo install -d -o root -g root -m 0755 /opt/aster-core
sudo install -d -o root -g root -m 0755 /opt/aster-core/releases
sudo install -d -o root -g root -m 0755 \
  "/opt/aster-core/releases/${ASTER_BUILD_ID}"
sudo install -o root -g root -m 0755 \
  "${ASTER_DOWNLOAD_DIR}/aster-core" \
  "/opt/aster-core/releases/${ASTER_BUILD_ID}/aster-core"
sudo ln -sfn \
  "/opt/aster-core/releases/${ASTER_BUILD_ID}" \
  /opt/aster-core/current
```

再次從正式路徑確認：

```sh
readlink -f /opt/aster-core/current
/opt/aster-core/current/aster-core -v
```

暫存目錄可先保留到服務通過端到端測試；它不應成為 systemd 的執行來源。

## 4. 建立專用帳號與目錄

先確認帳號是否已存在；若已存在，檢查它是否真的是預期的 system account，不要直接改寫既有帳號：

```sh
getent passwd aster-core || true
```

全新主機可建立不可登入的 service account：

```sh
sudo useradd \
  --system \
  --user-group \
  --home-dir /var/lib/aster-core \
  --create-home \
  --shell /usr/sbin/nologin \
  aster-core
```

建立設定與 state 目錄：

```sh
sudo install -d -o root -g aster-core -m 0750 /etc/aster-core
sudo install -d -o aster-core -g aster-core -m 0700 /var/lib/aster-core
```

這個分工刻意讓：

- root 可修改 `/etc/aster-core/config.yaml`。
- `aster-core` 可讀設定，但不能覆寫它。
- `aster-core` 可原子寫入 `/var/lib/aster-core/aster-state.json*`。
- 其他本機使用者不能讀取 credentials。

Aster 的 state parent 必須由目前執行服務的帳號擁有，且不可被 group／other 寫入；state files 必須由該帳號擁有且不得有任何 group／other permissions。程式也會拒絕 symlink state。

## 5. 產生 secrets 與 REALITY key

在可信終端產生資料，立即存入 password manager；不要貼到 issue、聊天或 shell script：

```sh
openssl rand -base64 48
openssl rand -base64 48
openssl rand -base64 32
openssl rand -hex 8
/opt/aster-core/current/aster-core generate reality-keypair
```

依序可用作：

1. Controller `secret`。
2. `aster.secret`，至少 32 bytes，且不可與 Controller secret 相同。
3. 第一個 AnyTLS 使用者 password。
4. REALITY short ID；8 bytes 會輸出 16 個 hex 字元。
5. REALITY key pair：private key 只留在 server，public key交給 client。

終端輸出仍是敏感資料。若有 session recording、共用終端或會保存 scrollback 的系統，改在離線可信裝置產生。

## 6. 建立 AnyTLS + REALITY 設定

使用 `sudoedit`，避免用 world-readable 暫存檔：

```sh
sudoedit /etc/aster-core/config.yaml
```

可從以下最小 server 設定開始，所有 `<...>` 都必須替換：

```yaml
log-level: info
mode: rule

external-controller: 127.0.0.1:9090
secret: "<獨立的-controller-secret>"

listeners:
  - name: edge-anytls
    type: anytls
    listen: 0.0.0.0
    port: 443
    users:
      first-user: "<長且隨機的-anytls-password>"
    reality-config:
      dest: www.microsoft.com:443
      private-key: "<server-private-key>"
      short-id:
        - "<16-hex-short-id>"
      server-names:
        - www.microsoft.com

aster:
  secret: "<獨立且至少-32-bytes-的-aster-secret>"
  store: /var/lib/aster-core/aster-state.json
  managed-listeners:
    - edge-anytls

rules:
  - MATCH,DIRECT
```

設定檔權限：

```sh
sudo chown root:aster-core /etc/aster-core/config.yaml
sudo chmod 0640 /etc/aster-core/config.yaml
sudo stat -c '%U:%G %a %n' /etc/aster-core/config.yaml
```

REALITY 注意事項：

- `dest` 是驗證失敗時 server 會連線的 fallback TLS 目的地。
- Client 的 `server` 是你的 VPS IP／網域；`sni` 才要匹配 `server-names`。
- Client 使用 server 的 **public key**；不可把 private key 當成 `pbk`。
- Client 的 `sid` 必須完全匹配其中一個 short ID。
- `certificate`／`private-key` 憑證 TLS、`shadow-tls`、`res-tls`、`jls-config` 與 `reality-config` 是互斥模式。
- 若要輸出受管訂閱，再加入 `public-base-url: https://你的公開網域`。它必須是絕對 HTTPS URL，不能有 query、fragment 或 user information。

完整欄位與 client 範例見 [AnyTLS + REALITY](/reference/anytls-reality)。

## 7. 以服務帳號驗證設定

`-t` 要使用與正式服務相同的使用者、home、設定路徑與 safe paths。否則 root 測試成功，不代表 service account 有權讀取設定或引用的檔案。

```sh
sudo -u aster-core \
  env SAFE_PATHS=/etc/aster-core \
  /opt/aster-core/current/aster-core \
  -d /var/lib/aster-core \
  -f /etc/aster-core/config.yaml \
  -t
```

預期看到 `configuration ... test is successful`。`-t` 只證明解析與靜態驗證成功，不保證：

- TCP 443 尚未被其他程式占用。
- Firewall 與雲端 security group 已放行。
- DNS 指向正確主機。
- REALITY `dest` 可由 VPS 連線。
- Client 的 SNI、public key、short ID 與時鐘正確。
- Server 的 UDP egress 可用。

若憑證、provider 或其他引用檔案放在 `/etc/aster-core` 以外，優先把它們移入受控目錄；確實需要其他可信根目錄時，再把該根目錄加入 `SAFE_PATHS`。不要用 `SKIP_SAFE_PATH_CHECK=true` 當成長期解法。

## 8. 建立 systemd service

建立 `/etc/systemd/system/aster-core.service`：

```sh
sudoedit /etc/systemd/system/aster-core.service
```

內容：

```ini
[Unit]
Description=Aster Core
Documentation=https://astercore.fubukishop.app/
Wants=network-online.target
After=network-online.target nss-lookup.target

[Service]
Type=simple
User=aster-core
Group=aster-core
UMask=0077
Environment=SAFE_PATHS=/etc/aster-core
ExecStart=/opt/aster-core/current/aster-core -d /var/lib/aster-core -f /etc/aster-core/config.yaml
ExecReload=/bin/kill -HUP $MAINPID
Restart=on-failure
RestartSec=5s
LimitNOFILE=infinity

StateDirectory=aster-core
StateDirectoryMode=0700
NoNewPrivileges=true
PrivateTmp=true
ProtectSystem=strict
ProtectHome=true
ProtectControlGroups=true
ProtectKernelModules=true
ProtectKernelTunables=true
ReadWritePaths=/var/lib/aster-core

# 範例 listener 使用 TCP 443；高於 1023 的 port 可移除這兩行。
AmbientCapabilities=CAP_NET_BIND_SERVICE
CapabilityBoundingSet=CAP_NET_BIND_SERVICE

[Install]
WantedBy=multi-user.target
```

這個 unit 適用於一般 server listener、Controller 與 outbound。若啟用 TUN、TProxy、透明路由或自動 firewall 管理，還需要額外權限與裝置存取；先閱讀 [Linux 套件與 systemd](/deployment/linux) 與 [路由、DNS、TUN](/reference/routing-dns)，只加入實際需要的 capability。不要為了省事直接授予無關權限。

檢查 unit 並啟動：

```sh
sudo systemd-analyze verify /etc/systemd/system/aster-core.service
sudo systemctl daemon-reload
sudo systemctl enable --now aster-core.service
sudo systemctl status aster-core.service --no-pager
```

查看本次啟動 log：

```sh
sudo journalctl -u aster-core.service -b --no-pager
```

不要只看 `active (running)`；接著確認 socket 與端到端流量。

## 9. Port、DNS 與 firewall

先確認誰占用 443：

```sh
sudo ss -lntp 'sport = :443'
```

預期看到 `aster-core`。若是 Caddy、Nginx、HAProxy 或另一個代理，先設計 TCP／SNI 分流或改用不同 port；兩個程序不能直接監聽相同 address:port。

確認 DNS：

```sh
getent ahosts proxy.example.com
```

把 `proxy.example.com` 換成實際 server 網域。若 DNS provider 有一般 HTTP reverse proxy／CDN 開關，AnyTLS + REALITY 的 raw TCP 通常必須使用 DNS-only；一般 CDN proxy 會終止或拒絕這個非 HTTP 協定。

先檢查目前使用的 firewall，不要同時用多套工具建立互相矛盾的規則：

```sh
sudo ufw status verbose
sudo nft list ruleset
```

若主機確實由 UFW 管理，保留現有 SSH 規則後才加入：

```sh
sudo ufw allow 443/tcp comment 'Aster AnyTLS'
sudo ufw status numbered
```

在遠端 VPS 上不要未確認 SSH allow rule 就執行 `ufw enable`。若主機由 nftables、雲端 security group 或供應商 firewall 管理，請在那一層加入等價的 TCP 443 allow rule，不要用會清空整份 ruleset 的指令。

AnyTLS transport 本身使用 TCP。Client 的 `udp: true` 是把 UDP payload 透過 AnyTLS/UoT 傳輸，不代表 server 必須另外監聽 `443/udp`；除非你同時部署了另一個使用 UDP listener 的協定，否則不要因為 UoT 盲目開放 UDP 443。

## 10. 上線驗收

在 server 端：

```sh
sudo systemctl is-active aster-core.service
sudo systemctl show aster-core.service \
  -p User -p Group -p ExecStart -p AmbientCapabilities
sudo ss -lntp 'sport = :443'
sudo ss -lntp 'sport = :9090'
sudo journalctl -u aster-core.service --since '-10 minutes' --no-pager
timedatectl status
```

Controller 應只在 `127.0.0.1:9090`。Aster Admin 使用 `aster.secret`，不是 Controller `secret`。測試時不要加 `curl -v`，它可能把 Authorization header 顯示在終端：

```sh
read -r -s ASTER_ADMIN_TOKEN
printf '\n'
printf 'header = "Authorization: Bearer %s"\n' "${ASTER_ADMIN_TOKEN}" |
  curl --config - \
    --silent --show-error --fail-with-body \
    http://127.0.0.1:9090/api/admin/status
unset ASTER_ADMIN_TOKEN
```

再由另一個網路的 Aster 相容 client 驗證：

1. `server` 指向 VPS 網域或 IP，而不是 REALITY 偽裝站。
2. `sni` 匹配 `server-names`。
3. `pbk` 使用 server public key。
4. `sid` 匹配 server short ID。
5. `client-fingerprint: chrome`。
6. 先測 TCP 網頁，再以 `udp: true` 測 DNS／其他 UDP 流量。
7. 修改 AnyTLS password 後，確認新 credential 可用、舊 credential 無法建立新連線。

## 11. 更新流程

更新前閱讀 release notes，下載新 asset 並重做 checksum 驗證。不要直接覆寫 `/opt/aster-core/current/aster-core`。

先把新 binary 安裝到新版本目錄，但暫時不要切換 `current`：

```sh
ASTER_NEW_BUILD_ID='alpha-main-NEW_SHA7'
sudo install -d -o root -g root -m 0755 \
  "/opt/aster-core/releases/${ASTER_NEW_BUILD_ID}"
sudo install -o root -g root -m 0755 \
  "${ASTER_DOWNLOAD_DIR}/aster-core" \
  "/opt/aster-core/releases/${ASTER_NEW_BUILD_ID}/aster-core"
```

用新 binary、正式 service account 與正式設定做靜態驗證：

```sh
sudo -u aster-core \
  env SAFE_PATHS=/etc/aster-core \
  "/opt/aster-core/releases/${ASTER_NEW_BUILD_ID}/aster-core" \
  -d /var/lib/aster-core \
  -f /etc/aster-core/config.yaml \
  -t
```

記錄目前版本並建立只限 root 的一致性備份。為避免 state 在複製途中繼續變更，這個流程包含短暫停機：

```sh
ASTER_PREVIOUS_TARGET="$(readlink -f /opt/aster-core/current)"
ASTER_BACKUP_STAMP="$(date -u +%Y%m%dT%H%M%SZ)"
sudo systemctl stop aster-core.service
sudo install -d -o root -g root -m 0700 \
  "/var/backups/aster-core/${ASTER_BACKUP_STAMP}"
sudo cp -a /etc/aster-core \
  "/var/backups/aster-core/${ASTER_BACKUP_STAMP}/etc-aster-core"
sudo cp -a /var/lib/aster-core \
  "/var/backups/aster-core/${ASTER_BACKUP_STAMP}/var-lib-aster-core"
```

切換並啟動：

```sh
sudo ln -sfn \
  "/opt/aster-core/releases/${ASTER_NEW_BUILD_ID}" \
  /opt/aster-core/current
sudo systemctl start aster-core.service
sudo systemctl status aster-core.service --no-pager
```

完整重做「上線驗收」，特別檢查：

- AnyTLS TCP 與 UDP/UoT。
- REALITY handshake。
- Controller 與 Aster Admin。
- Managed users、revision 與 subscription。
- DNS、IPv4、IPv6。
- Journal 是否有 state recovery、permission 或 runtime apply error。

備份含有 private key、使用者 credential 與 subscription token，必須保留 `0700` 存取控制並納入加密備份政策。

## 12. 回退流程

若新版本無法啟動或端到端驗收失敗，先保留 journal，再回到剛才記錄的完整舊 target：

```sh
sudo journalctl -u aster-core.service --since '-30 minutes' --no-pager
sudo systemctl stop aster-core.service
sudo ln -sfn "${ASTER_PREVIOUS_TARGET}" /opt/aster-core/current
sudo systemctl start aster-core.service
sudo systemctl status aster-core.service --no-pager
```

先嘗試只回退 binary。只有舊 binary 明確拒絕新 config／state schema，或 release notes 要求配對回復時，才在服務停止狀態下從同一個 upgrade backup 回復 config 與 state。回復舊 state 會遺失備份之後建立的帳號、流量與 token 變更，不能當成無代價操作。

絕對不要手動修改 `aster-state.json` 的 `version` 來繞過相容性檢查，也不要同時刪除 primary 與 `.bak`。需要深入定位時請依 [生產環境故障排除](/tutorials/troubleshooting) 收集已遮罩的診斷資料。

## 13. 使用官方 `.deb` 的差異

若要使用 Release 中的 Debian package，下載與驗證方式相同，只把 asset 名稱改成 Release 頁實際存在的 `.deb`：

```sh
ASTER_DEB_NAME='aster-core-linux-amd64-v1-alpha-main-SHA7.deb'
sudo apt install "${ASTER_DOWNLOAD_DIR}/${ASTER_DEB_NAME}"
```

目前 package 會安裝：

```text
/usr/bin/aster-core
/usr/bin/mihomo -> aster-core
/etc/mihomo/config.yaml
/usr/lib/systemd/system/aster-core.service
/usr/lib/systemd/system/aster-core@.service
```

隨附主 unit 的實際啟動命令是：

```text
/usr/bin/aster-core -d /etc/mihomo
```

它目前以 root 執行，並保留：

```text
CAP_NET_ADMIN CAP_NET_RAW CAP_NET_BIND_SERVICE CAP_SYS_TIME
CAP_SYS_PTRACE CAP_DAC_READ_SEARCH CAP_DAC_OVERRIDE
```

套件模式的正確驗證與啟動命令：

```sh
sudo /usr/bin/aster-core -d /etc/mihomo -t
sudo systemctl enable --now aster-core.service
sudo systemctl status aster-core.service --no-pager
```

若要使用專用帳號與最小 capability，採用本篇的原始 `.gz`／自訂 unit 流程會更清楚。不要只對 package 目錄執行 `chown aster-core`，卻仍讓隨附 unit 以 root 執行；那會形成與實際 threat model 不一致的半套配置。
