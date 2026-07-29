# 即時管理 VLESS、AnyTLS 使用者與訂閱

::: warning 可選的進階服務端功能
本篇只適用於你明確選擇用 Aster 提供 VLESS／AnyTLS listener 的情境。Aster 的主要用途是客戶端；一般服務端可使用 Xray、sing-box 或 SideraCore，且不需要啟用本篇管理平面。
:::

本篇會把兩個 Aster listener 納入管理平面，然後用完整 `curl` 流程完成：

- 啟用 `aster.managed-listeners`、state store 與 Admin API。
- 建立、列出、查詢、更新、停用、重新啟用及刪除 AnyTLS/VLESS user。
- 正確使用每個 listener 的 revision。
- 重現並處理 HTTP `409 Conflict`。
- 取得及輪替單一使用者的訂閱 URL。
- 備份、回復與保護 state 中的 credential/token。

使用者變更會即時套用到執行中的 VLESS/AnyTLS listener，不必重建 listener。REALITY key、port、SNI、transport 等 listener 設定仍由 YAML 與設定 reload 管理，不屬於 user CRUD。

## 架構與安全邊界

本篇採用：

```text
管理者
  └─ SSH / localhost ──> 127.0.0.1:9090/api/admin/*
                              │ Aster Bearer token
                              ▼
                         Aster manager
                           ├─ live listeners
                           └─ state/aster-state.json + .bak

一般使用者
  └─ HTTPS ──> subscription.example.com/sub/aster/{token}
                              │
                              └─ 單一 Base64 proxy link
```

Admin API 維持 loopback；public reverse proxy 只轉送 `/sub/aster/*`。不要把 `/api/admin/*`、一般 Clash Controller 或 state file 暴露到 Internet。

## 前置條件

- Aster Core 已安裝在 VPS，並能以 `-d /etc/mihomo` 執行。
- 已準備兩組 REALITY key pair 與 short ID；可參考[從零部署 AnyTLS + REALITY](/tutorials/anytls-reality)。
- `subscription.example.com` 指向 VPS，且有可用的 HTTPS reverse proxy。
- VPS 有 `curl` 與 `jq`。
- AnyTLS/VLESS 對外 ports 已在雲端與本機防火牆開放。

本篇 listener ports 如下：

| Listener | Port | Credential |
| --- | --- | --- |
| `edge-anytls` | TCP 8443 | password |
| `edge-vless` | TCP 8444 | UUID，可選 `xtls-rprx-vision` flow |
| Loopback Controller | TCP 9090 | `aster.secret` 保護 Admin API |
| Public subscription HTTPS | TCP 443 | 每位 user 的 subscription token |

使用 `8443/8444` 是為了讓 Caddy/Nginx 可獨占 `443` 提供訂閱 HTTPS。若自行改 port，DNS、防火牆、YAML 與測試要一起修改。

### 管理範圍

- 只有 `listeners` 下具名的 VLESS 與 AnyTLS 可加入 `managed-listeners`。
- Admin API 即時修改 user credential、flow、enabled、流量與 subscription token；REALITY/TLS/transport/port 仍由 YAML 管理。
- Quota 與 expiration capability 目前是 `false`，不能只靠 Aster Core 實作流量配額或到期停用。
- Revision 屬於整個 listener，不是單一 user；同 listener 任一 user mutation 都會推進 revision。
- 訂閱只輸出 eligible 的 VLESS/AnyTLS 設定。ShadowTLS、ResTLS、JLS 與進階 XHTTP 不會產生受管分享連結。
- 其他繼承自 Mihomo 的 listener/protocol 仍照原本方式設定，不能交給這組 Admin API 管理。

## 1. 產生獨立 secret 與 listener 材料

Controller secret、Aster Admin secret、使用者 credential 與 subscription token 是四種不同的秘密，不可重用。

在 VPS 產生兩個管理 secret：

```sh
umask 077
openssl rand -hex 32
openssl rand -hex 32
```

依序作為：

- `<CONTROLLER_SECRET>`：一般 `/version`、`/configs` 等 Controller API。
- `<ASTER_SECRET>`：只有 `/api/admin/*` 使用，至少 32 bytes。

為兩個 listener 分別產生 REALITY key pair 與 short ID：

```sh
/usr/bin/aster-core generate reality-keypair
openssl rand -hex 8

/usr/bin/aster-core generate reality-keypair
openssl rand -hex 8
```

記為：

- `<ANYTLS_REALITY_PRIVATE_KEY>` / `<ANYTLS_REALITY_PUBLIC_KEY>` / `<ANYTLS_SHORT_ID>`
- `<VLESS_REALITY_PRIVATE_KEY>` / `<VLESS_REALITY_PUBLIC_KEY>` / `<VLESS_SHORT_ID>`

Private keys 只會出現在 server YAML。Admin API 管理 AnyTLS password 與 VLESS UUID，不會回傳或修改 REALITY private key。

## 2. 寫入完整 Aster server YAML

建立受保護的設定目錄：

```sh
sudo install -d -m 700 /etc/mihomo
sudoedit /etc/mihomo/config.yaml
```

填入以下設定並替換所有 placeholder：

```yaml
mode: rule
log-level: info
ipv6: true

# 明文 Controller 只綁 loopback，才會掛載 Aster Admin routes。
external-controller: 127.0.0.1:9090
secret: "<CONTROLLER_SECRET>"

listeners:
  - name: edge-anytls
    type: anytls
    listen: 0.0.0.0
    port: 8443
    users: {}
    reality-config:
      dest: www.microsoft.com:443
      private-key: "<ANYTLS_REALITY_PRIVATE_KEY>"
      short-id:
        - "<ANYTLS_SHORT_ID>"
      server-names:
        - www.microsoft.com

  - name: edge-vless
    type: vless
    listen: 0.0.0.0
    port: 8444
    users: []
    reality-config:
      dest: www.microsoft.com:443
      private-key: "<VLESS_REALITY_PRIVATE_KEY>"
      short-id:
        - "<VLESS_SHORT_ID>"
      server-names:
        - www.microsoft.com

aster:
  secret: "<ASTER_SECRET>"
  public-base-url: "https://subscription.example.com"
  store: "state/aster-state.json"
  managed-listeners:
    - edge-anytls
    - edge-vless

rules:
  - MATCH,DIRECT
```

注意欄位名稱是 `aster.store`。相對路徑會以 Aster home（本篇是 `/etc/mihomo`）解析，因此 state 位於：

```text
/etc/mihomo/state/aster-state.json
/etc/mihomo/state/aster-state.json.bak
/etc/mihomo/state/aster-state.json.lock
```

`public-base-url` 必須是絕對 HTTPS URL，不能包含 userinfo、query 或 fragment。Aster 會使用它的 hostname 產生 proxy link 的 server，並使用 listener 實際 port。

### YAML users 與 state 的關係

首次啟動、state 尚無對應 listener 時，Aster 會把 YAML `users` 匯入 state。之後 state 才是受管 user 的持久來源；不要一邊用 API 管理，一邊把 YAML 當作即時 user database。

本篇以空的 `{}`/`[]` 起步，所有帳號都由 API 建立。若刪除 state 後重啟，YAML user 可能再次被匯入，所以不要把刪除 state 當成「清空帳號」的方法。

## 3. 驗證、啟動與確認 state

```sh
sudo chmod 600 /etc/mihomo/config.yaml
sudo /usr/bin/aster-core -d /etc/mihomo -t
sudo systemctl restart aster-core
sudo systemctl status --no-pager aster-core
sudo journalctl -u aster-core -n 80 --no-pager
```

確認 listeners 與 Controller：

```sh
sudo ss -ltnp | grep -E ':(8443|8444|9090)[[:space:]]'
```

預期：

- `0.0.0.0:8443`：AnyTLS。
- `0.0.0.0:8444`：VLESS。
- `127.0.0.1:9090`：Controller。

確認 state 權限：

```sh
sudo stat \
  -c '%A %a %U:%G %n' \
  /etc/mihomo/state \
  /etc/mihomo/state/aster-state.json \
  /etc/mihomo/state/aster-state.json.bak
```

使用 package 的 root service 時，預期 directory 是 `700`，state files 是 `600` 且 owner 為 `root`。若 service 使用專用 account，owner 必須改為該 account；Aster 會拒絕不安全的 directory、symlink、非 regular file 或寬鬆 state file。

## 4. 只公開 subscription route

例如 Caddy 可只轉送 public subscription：

```caddy
subscription.example.com {
	route {
		@asterSubscription path /sub/aster/*
		reverse_proxy @asterSubscription 127.0.0.1:9090
		respond 404
	}

	header {
		Referrer-Policy "no-referrer"
	}
}
```

其他 path 會是 404，`/api/admin/*` 不會被這個 matcher 轉送。Aster 已對訂閱回應設定 `Cache-Control: no-store`，reverse proxy 不應改成可快取。

Admin 操作從 VPS localhost 執行，或透過 SSH tunnel：

```sh
ssh -N -L 9090:127.0.0.1:9090 root@<VPS_HOST>
```

然後管理端仍呼叫 `http://127.0.0.1:9090`。若把明文 `external-controller` 綁到非 loopback，Aster 會刻意不掛載 Admin routes，請勿用 `0.0.0.0:9090` 解決遠端存取。

## 5. 準備安全的 curl 環境

在可信任的管理 terminal 設定 base URL，並互動輸入 Aster secret，避免 secret 直接留在 shell history：

```sh
export ASTER_API='http://127.0.0.1:9090'
read -r -s -p 'Aster secret: ' ASTER_TOKEN
echo
export ASTER_TOKEN

acurl() {
  curl \
    --silent \
    --show-error \
    --fail \
    -H "Authorization: Bearer ${ASTER_TOKEN}" \
    "$@"
}
```

CLI request 通常不帶 `Origin`，因此可通過 same-origin 檢查；Bearer token 仍為必要。`aster.secret` 不是 root-level Controller `secret`，用錯會回 `401`。

先測 overview：

```sh
acurl "${ASTER_API}/api/admin/overview" |
  jq '{status, api_version, authentication_enabled, users}'
```

預期：

```json
{
  "status": "running",
  "api_version": 1,
  "authentication_enabled": true,
  "users": {
    "total": 0,
    "enabled": 0,
    "disabled": 0
  }
}
```

重點是 status 為 `running` 且 authentication enabled。Listener summary 會在下一步單獨檢查。

## 6. 讀取 listeners 與 revision

```sh
acurl "${ASTER_API}/api/admin/inbounds" |
  jq '.inbounds[] | {
    tag, type, managed, credential, flow, traffic,
    user_count, enabled_user_count,
    revision, applied_revision, pending
  }'
```

預期 `edge-anytls` 與 `edge-vless` 都有：

```json
{
  "managed": true,
  "revision": 1785312000000,
  "applied_revision": 1785312000000
}
```

Revision 是 **每個 listener 各自獨立** 的正整數，而且不保證只加一。每次 mutation 前都應讀取當下 revision；`edge-anytls` 的 mutation 不會改變 `edge-vless` revision。

把目前值存入 shell：

```sh
ANYTLS_REV=$(
  acurl "${ASTER_API}/api/admin/inbounds" |
    jq -er '.inbounds[] | select(.tag == "edge-anytls") | .revision'
)

VLESS_REV=$(
  acurl "${ASTER_API}/api/admin/inbounds" |
    jq -er '.inbounds[] | select(.tag == "edge-vless") | .revision'
)
```

## 7. 建立 AnyTLS user

`password` 可以自行提供；省略或留空時，Aster 會產生 32-byte Base64URL password。本篇讓 server 產生，避免弱密碼：

```sh
ANYTLS_CREATE=$(
  jq -n \
    --arg inbound 'edge-anytls' \
    --arg name 'alice-phone' \
    --argjson revision "${ANYTLS_REV}" \
    '{
      inbound: $inbound,
      name: $name,
      enabled: true,
      revision: $revision
    }' |
  acurl \
    -X POST \
    -H 'Content-Type: application/json' \
    --data-binary @- \
    "${ASTER_API}/api/admin/users"
)
```

只在可信 terminal 查看建立結果：

```sh
printf '%s\n' "${ANYTLS_CREATE}" |
  jq '{id, inbound, type, name, password, enabled, revision, subscription_url}'
```

成功是 HTTP `201 Created`，response 的 `type` 是 `anytls`，並含自動產生的 `password`、新 revision 與 eligible subscription URL。

保存後續操作所需欄位：

```sh
ANYTLS_ID=$(printf '%s\n' "${ANYTLS_CREATE}" | jq -er '.id')
ANYTLS_REV=$(printf '%s\n' "${ANYTLS_CREATE}" | jq -er '.revision')
ANYTLS_SUB=$(printf '%s\n' "${ANYTLS_CREATE}" | jq -er '.subscription_url')
unset ANYTLS_CREATE
```

## 8. 建立 VLESS user

VLESS `uuid` 也可省略，Aster 會產生 UUID。`flow` 只接受空字串或 `xtls-rprx-vision`：

```sh
VLESS_CREATE=$(
  jq -n \
    --arg inbound 'edge-vless' \
    --arg name 'bob-laptop' \
    --arg flow 'xtls-rprx-vision' \
    --argjson revision "${VLESS_REV}" \
    '{
      inbound: $inbound,
      name: $name,
      flow: $flow,
      enabled: true,
      revision: $revision
    }' |
  acurl \
    -X POST \
    -H 'Content-Type: application/json' \
    --data-binary @- \
    "${ASTER_API}/api/admin/users"
)
```

```sh
printf '%s\n' "${VLESS_CREATE}" |
  jq '{id, inbound, type, name, uuid, flow, enabled, revision, subscription_url}'

VLESS_ID=$(printf '%s\n' "${VLESS_CREATE}" | jq -er '.id')
VLESS_REV=$(printf '%s\n' "${VLESS_CREATE}" | jq -er '.revision')
VLESS_SUB=$(printf '%s\n' "${VLESS_CREATE}" | jq -er '.subscription_url')
unset VLESS_CREATE
```

成功 response 的 `type` 是 `vless`，並含產生的 UUID。不要對 VLESS 傳 `password`，也不要對 AnyTLS 傳 `uuid` 或 `flow`；這些錯誤會回 HTTP `400`。

## 9. 列出與查詢單一 user

列出全部：

```sh
acurl "${ASTER_API}/api/admin/users" |
  jq '.users[] | {
    id, inbound, type, name, flow, enabled,
    upload_bytes, download_bytes, active_connections,
    revision, applied_revision
  }'
```

只列 AnyTLS：

```sh
acurl "${ASTER_API}/api/admin/users?inbound=edge-anytls" |
  jq '.users'
```

List response 刻意不含 UUID/password。需要查 credential 時，讀單一 user：

```sh
acurl "${ASTER_API}/api/admin/users/${ANYTLS_ID}" |
  jq '{id, inbound, type, name, password, enabled, revision, subscription_url}'

acurl "${ASTER_API}/api/admin/users/${VLESS_ID}" |
  jq '{id, inbound, type, name, uuid, flow, enabled, revision, subscription_url}'
```

單一 user response、建立/更新 response 及 subscription URL 都是敏感資料，不應寫入一般 application log 或 analytics。

## 10. 更新 AnyTLS password 與名稱

先讀最新 revision，再產生新 password：

```sh
ANYTLS_REV=$(
  acurl "${ASTER_API}/api/admin/users/${ANYTLS_ID}" |
    jq -er '.revision'
)
NEW_ANYTLS_PASSWORD=$(openssl rand -hex 32)
```

送出 partial update；未出現的欄位保持原值：

```sh
ANYTLS_UPDATE=$(
  jq -n \
    --arg name 'alice-tablet' \
    --arg password "${NEW_ANYTLS_PASSWORD}" \
    --argjson revision "${ANYTLS_REV}" \
    '{
      name: $name,
      password: $password,
      revision: $revision
    }' |
  acurl \
    -X PUT \
    -H 'Content-Type: application/json' \
    --data-binary @- \
    "${ASTER_API}/api/admin/users/${ANYTLS_ID}"
)

printf '%s\n' "${ANYTLS_UPDATE}" |
  jq '{id, name, enabled, revision, applied_revision}'
ANYTLS_REV=$(printf '%s\n' "${ANYTLS_UPDATE}" | jq -er '.revision')
unset ANYTLS_UPDATE NEW_ANYTLS_PASSWORD
```

預期 `revision == applied_revision`。新 password 立即用於 authentication；舊 password 無法建立新 session。Aster 也會關閉 credential 已失效的 active/pending connections，不受這次變更影響的其他 user 連線則保留。仍應用全新的 client session 驗證新舊 password。

## 11. 更新 VLESS UUID、flow 或名稱

用 Aster CLI 產生新 UUID：

```sh
NEW_VLESS_UUID=$(/usr/bin/aster-core generate uuid)
VLESS_REV=$(
  acurl "${ASTER_API}/api/admin/users/${VLESS_ID}" |
    jq -er '.revision'
)
```

```sh
VLESS_UPDATE=$(
  jq -n \
    --arg name 'bob-desktop' \
    --arg uuid "${NEW_VLESS_UUID}" \
    --arg flow 'xtls-rprx-vision' \
    --argjson revision "${VLESS_REV}" \
    '{
      name: $name,
      uuid: $uuid,
      flow: $flow,
      revision: $revision
    }' |
  acurl \
    -X PUT \
    -H 'Content-Type: application/json' \
    --data-binary @- \
    "${ASTER_API}/api/admin/users/${VLESS_ID}"
)

printf '%s\n' "${VLESS_UPDATE}" |
  jq '{id, name, flow, enabled, revision, applied_revision}'
VLESS_REV=$(printf '%s\n' "${VLESS_UPDATE}" | jq -er '.revision')
unset VLESS_UPDATE NEW_VLESS_UUID
```

若要清除 flow，明確傳 `"flow": ""`。其他 flow 會回 `400`。

## 12. 停用與重新啟用 user

停用會把 user 從 live credential 集合移除，但保留 state、流量與 user ID，適合可回復的撤銷。先停用 AnyTLS：

```sh
ANYTLS_REV=$(
  acurl "${ASTER_API}/api/admin/users/${ANYTLS_ID}" |
    jq -er '.revision'
)

ANYTLS_DISABLE=$(
  jq -n \
    --argjson revision "${ANYTLS_REV}" \
    '{enabled: false, revision: $revision}' |
  acurl \
    -X PUT \
    -H 'Content-Type: application/json' \
    --data-binary @- \
    "${ASTER_API}/api/admin/users/${ANYTLS_ID}"
)
ANYTLS_REV=$(printf '%s\n' "${ANYTLS_DISABLE}" | jq -er '.revision')
printf '%s\n' "${ANYTLS_DISABLE}" |
  jq '{id, enabled, active_connections, revision}'
unset ANYTLS_DISABLE
```

再停用 VLESS：

```sh
VLESS_REV=$(
  acurl "${ASTER_API}/api/admin/users/${VLESS_ID}" |
    jq -er '.revision'
)

VLESS_DISABLE=$(
  jq -n \
    --argjson revision "${VLESS_REV}" \
    '{enabled: false, revision: $revision}' |
  acurl \
    -X PUT \
    -H 'Content-Type: application/json' \
    --data-binary @- \
    "${ASTER_API}/api/admin/users/${VLESS_ID}"
)
VLESS_REV=$(printf '%s\n' "${VLESS_DISABLE}" | jq -er '.revision')
printf '%s\n' "${VLESS_DISABLE}" |
  jq '{id, enabled, active_connections, revision}'
unset VLESS_DISABLE
```

停用後，兩個 subscription URL 都應回 `404`：

```sh
curl -sS -o /dev/null -w 'AnyTLS subscription: HTTP %{http_code}\n' "${ANYTLS_SUB}"
curl -sS -o /dev/null -w 'VLESS subscription: HTTP %{http_code}\n' "${VLESS_SUB}"
```

也要用已完全重啟的新 client session 確認 credential 無法再登入。

重新啟用時，各自使用最新 revision：

```sh
ANYTLS_ENABLE=$(
  jq -n \
    --argjson revision "${ANYTLS_REV}" \
    '{enabled: true, revision: $revision}' |
  acurl \
    -X PUT \
    -H 'Content-Type: application/json' \
    --data-binary @- \
    "${ASTER_API}/api/admin/users/${ANYTLS_ID}"
)
ANYTLS_REV=$(printf '%s\n' "${ANYTLS_ENABLE}" | jq -er '.revision')
unset ANYTLS_ENABLE

VLESS_ENABLE=$(
  jq -n \
    --argjson revision "${VLESS_REV}" \
    '{enabled: true, revision: $revision}' |
  acurl \
    -X PUT \
    -H 'Content-Type: application/json' \
    --data-binary @- \
    "${ASTER_API}/api/admin/users/${VLESS_ID}"
)
VLESS_REV=$(printf '%s\n' "${VLESS_ENABLE}" | jq -er '.revision')
unset VLESS_ENABLE
```

## 13. 取得、解碼與輪替訂閱

啟用 user 後，單一 user response 會帶 subscription URL：

```sh
ANYTLS_SUB=$(
  acurl "${ASTER_API}/api/admin/users/${ANYTLS_ID}" |
    jq -er '.subscription_url'
)
VLESS_SUB=$(
  acurl "${ASTER_API}/api/admin/users/${VLESS_ID}" |
    jq -er '.subscription_url'
)
```

Subscription endpoint 不使用 Bearer token。Body 是 Base64 編碼的一條 proxy URI：

```sh
curl --fail --silent --show-error "${ANYTLS_SUB}" | base64 -d
echo
curl --fail --silent --show-error "${VLESS_SUB}" | base64 -d
echo
```

預期分別以 `anytls://` 與 `vless://` 開頭。macOS 的 `base64` 若不接受 `-d`，改用 `base64 -D`。

AnyTLS + REALITY link 應包含：

```text
security=reality
type=tcp
sni=...
fp=chrome
pbk=...
sid=...
```

輪替 AnyTLS subscription 前先保存當下 revision 與舊 URL：

```sh
STALE_REV=$(
  acurl "${ASTER_API}/api/admin/users/${ANYTLS_ID}" |
    jq -er '.revision'
)
OLD_ANYTLS_SUB="${ANYTLS_SUB}"
```

```sh
ANYTLS_ROTATE=$(
  jq -n \
    --argjson revision "${STALE_REV}" \
    '{revision: $revision}' |
  acurl \
    -X POST \
    -H 'Content-Type: application/json' \
    --data-binary @- \
    "${ASTER_API}/api/admin/users/${ANYTLS_ID}/rotate-subscription"
)

printf '%s\n' "${ANYTLS_ROTATE}" | jq .
ANYTLS_REV=$(printf '%s\n' "${ANYTLS_ROTATE}" | jq -er '.revision')
ANYTLS_SUB=$(printf '%s\n' "${ANYTLS_ROTATE}" | jq -er '.subscription_url')
unset ANYTLS_ROTATE
```

舊 token 立即失效，新 token 可用：

```sh
curl -sS -o /dev/null -w 'old: HTTP %{http_code}\n' "${OLD_ANYTLS_SUB}"
curl -sS -o /dev/null -w 'new: HTTP %{http_code}\n' "${ANYTLS_SUB}"
```

預期是 `old: HTTP 404`、`new: HTTP 200`。輪替 VLESS 使用相同 endpoint，只要把 ID、revision 與變數換成 VLESS：

```sh
VLESS_REV=$(
  acurl "${ASTER_API}/api/admin/users/${VLESS_ID}" |
    jq -er '.revision'
)
OLD_VLESS_SUB="${VLESS_SUB}"

VLESS_ROTATE=$(
  jq -n \
    --argjson revision "${VLESS_REV}" \
    '{revision: $revision}' |
  acurl \
    -X POST \
    -H 'Content-Type: application/json' \
    --data-binary @- \
    "${ASTER_API}/api/admin/users/${VLESS_ID}/rotate-subscription"
)
VLESS_REV=$(printf '%s\n' "${VLESS_ROTATE}" | jq -er '.revision')
VLESS_SUB=$(printf '%s\n' "${VLESS_ROTATE}" | jq -er '.subscription_url')
unset VLESS_ROTATE
```

輪替 URL 不會自動變更 AnyTLS password 或 VLESS UUID；它只撤銷舊 subscription token。若懷疑 credential 也外洩，必須另外更新 credential。

## 14. 正確處理 revision `409 Conflict`

上一節保存的 `STALE_REV` 是 AnyTLS 輪替前的 revision；輪替已把 listener 推進到 `ANYTLS_REV`。故意用舊值送一次更新：

```sh
CONFLICT_BODY=$(mktemp)
chmod 600 "${CONFLICT_BODY}"

CONFLICT_STATUS=$(
  jq -n \
    --arg name 'must-not-apply' \
    --argjson revision "${STALE_REV}" \
    '{name: $name, revision: $revision}' |
  curl \
    --silent \
    --show-error \
    -o "${CONFLICT_BODY}" \
    -w '%{http_code}' \
    -X PUT \
    -H "Authorization: Bearer ${ASTER_TOKEN}" \
    -H 'Content-Type: application/json' \
    --data-binary @- \
    "${ASTER_API}/api/admin/users/${ANYTLS_ID}"
)

printf 'HTTP %s\n' "${CONFLICT_STATUS}"
jq . "${CONFLICT_BODY}"
rm -f "${CONFLICT_BODY}"
```

預期：

```text
HTTP 409
```

錯誤訊息會包含 expected/current revision。正確恢復流程：

1. 重新 GET listener 或單一 user。
2. 比對 server 現況與自己尚未送出的修改。
3. 合併後，用重新讀到的 current revision 建立新 request。
4. 如果又收到 409，再重讀；不要猜 revision。

```sh
CURRENT_ANYTLS=$(
  acurl "${ASTER_API}/api/admin/users/${ANYTLS_ID}"
)
printf '%s\n' "${CURRENT_ANYTLS}" |
  jq '{id, name, enabled, revision, applied_revision}'

CURRENT_ANYTLS_REV=$(
  printf '%s\n' "${CURRENT_ANYTLS}" |
    jq -er '.revision'
)

jq -n \
  --arg name 'alice-final' \
  --argjson revision "${CURRENT_ANYTLS_REV}" \
  '{name: $name, revision: $revision}' |
acurl \
  -X PUT \
  -H 'Content-Type: application/json' \
  --data-binary @- \
  "${ASTER_API}/api/admin/users/${ANYTLS_ID}" |
jq '{id, name, revision, applied_revision}'

unset CURRENT_ANYTLS CURRENT_ANYTLS_REV STALE_REV
```

不要把舊 revision 手動加一。Aster revision 可能以目前 Unix milliseconds 推進，而且同 listener 內另一位 user 的 mutation 也會使舊 revision 過期。

## 15. 刪除 AnyTLS 與 VLESS user

刪除不可像 disable 一樣直接復原，會同時移除 user 與 subscription token。正式操作通常先 disable、觀察，再 delete。

先讀最新 revision，刪除 AnyTLS：

```sh
ANYTLS_REV=$(
  acurl "${ASTER_API}/api/admin/users/${ANYTLS_ID}" |
    jq -er '.revision'
)

curl \
  --silent \
  --show-error \
  --fail \
  -o /dev/null \
  -w 'AnyTLS delete: HTTP %{http_code}\n' \
  -X DELETE \
  -H "Authorization: Bearer ${ASTER_TOKEN}" \
  "${ASTER_API}/api/admin/users/${ANYTLS_ID}?revision=${ANYTLS_REV}"
```

刪除 VLESS：

```sh
VLESS_REV=$(
  acurl "${ASTER_API}/api/admin/users/${VLESS_ID}" |
    jq -er '.revision'
)

curl \
  --silent \
  --show-error \
  --fail \
  -o /dev/null \
  -w 'VLESS delete: HTTP %{http_code}\n' \
  -X DELETE \
  -H "Authorization: Bearer ${ASTER_TOKEN}" \
  "${ASTER_API}/api/admin/users/${VLESS_ID}?revision=${VLESS_REV}"
```

兩個成功 response 都是 HTTP `204 No Content`。驗證：

```sh
acurl "${ASTER_API}/api/admin/users" |
  jq --arg anytls "${ANYTLS_ID}" --arg vless "${VLESS_ID}" \
    '[.users[] | select(.id == $anytls or .id == $vless)]'

curl -sS -o /dev/null -w 'AnyTLS sub: HTTP %{http_code}\n' "${ANYTLS_SUB}"
curl -sS -o /dev/null -w 'VLESS sub: HTTP %{http_code}\n' "${VLESS_SUB}"
```

預期 user 陣列是 `[]`，兩個 subscription 都是 `404`。

## 16. 備份 state 與敏感資料

State 包含：

- VLESS UUID。
- AnyTLS password。
- Subscription token。
- User ID/name、流量與 revision。

`config.yaml` 還包含 REALITY private keys、Controller secret 與 Aster secret。兩者都必須視為 credential database。

最一致的備份方式是短暫停止單一 Aster instance：

```sh
sudo systemctl stop aster-core

BACKUP_STAMP=$(date -u +%Y%m%dT%H%M%SZ)
sudo install -d -m 700 "/root/aster-backups/${BACKUP_STAMP}"
sudo cp --preserve=mode,ownership,timestamps \
  /etc/mihomo/config.yaml \
  /etc/mihomo/state/aster-state.json \
  /etc/mihomo/state/aster-state.json.bak \
  "/root/aster-backups/${BACKUP_STAMP}/"

sudo systemctl start aster-core
sudo systemctl is-active aster-core
```

不需要備份 `.lock`。確認服務已重新啟動後，再把 backup 用 age、離線媒體或受控 backup system 加密。不要把明文 tar 上傳到一般 object storage。

若不能停機，primary 與 `.bak` 都採 atomic replace，仍應在很短的同一時段複製兩份，並保留 generation 較新且可驗證的版本；離線備份仍較容易推理。

### 回復 state

回復舊 state 會一起回復當時所有 user、credential、token、流量與 revision，可能重新啟用已撤銷的資料。只有在理解影響後才做：

1. 停止 Aster。
2. 先把目前 state 複製到另一個 root-only forensic 目錄。
3. 確認 backup 的 listener names 與目前 YAML 完全一致。
4. 回復 primary 與 `.bak`，權限設為 `0600`。
5. 先執行 `aster-core -t`，再啟動並檢查 log/Admin snapshot。
6. 驗證所有應撤銷的 credential/token 沒被意外恢復。

不要手動修改 JSON 的 `generation`、`revision`、token 或 `version` 來繞過驗證。Primary 損壞時 Aster 會嘗試有效的 `.bak`；兩份都失敗時應保留現場並從受控 backup 回復。

## 17. 常見問題與回復方式

### Admin API 回 404

- 沒有 `aster` block。
- 明文 Controller 綁到非 loopback，所以 Admin routes 未掛載。
- Listener 名稱不在 `managed-listeners`。
- Reverse proxy/SSH tunnel 指錯 Controller。

先在 VPS 執行：

```sh
curl -v \
  -H "Authorization: Bearer ${ASTER_TOKEN}" \
  http://127.0.0.1:9090/api/admin/status
```

### 回 401 或 403

- `401`：用了 root-level Controller secret，而不是 `aster.secret`。
- `403`：Browser `Origin`/`Sec-Fetch-Site` 或 reverse proxy scheme/host 不符合 same-origin。

CLI 不需要自行添加 `Origin`。面板應透過同-origin backend/BFF，不要把 Aster token放在前端 bundle、URL 或 localStorage。

### Mutation 回 400

- `revision` 缺少、不是正整數。
- User name 空白、前後有空白、超過 256 字元或與同 listener 名稱重複（不分大小寫）。
- 同 listener credential 重複。
- AnyTLS 缺 password，或夾帶 UUID/flow。
- VLESS UUID 無效，或 flow 不是空字串/`xtls-rprx-vision`。
- JSON 超過 1 MiB 時會是 `413`。

### `revision != applied_revision` 或 `pending: true`

不要繼續批次 mutation。先查看 Aster log、listener socket 與目前 user snapshot。Aster 啟動/套用受管 credential 失敗時以 fail-closed 為優先，應先修復 runtime/store 問題。

### Subscription 回 404

- User disabled、deleted 或 token 已 rotate。
- `public-base-url` 未設定。
- Listener 不再受管或尚未有有效 port。
- Security mode 無法輸出；ShadowTLS、ResTLS、JLS 不會產生受管分享連結。
- VLESS 使用進階 XHTTP placement/padding。

### 誤更新或誤停用

如果 user 尚未刪除，優先用 Admin API 以最新 revision 改回 name/credential/enabled；這只影響目標 listener。不要為單一欄位錯誤直接回復整份 state。

如果已刪除：

- 可建立新 user，發放新 credential 與 subscription URL；這是較安全的作法。
- 只有必須恢復原 ID/token 時才考慮整份 state rollback，並評估其他使用者會一起倒退。

## 18. 結束管理 session

清除包含 token、ID 與 URL 的 shell 變數：

```sh
unset \
  ASTER_TOKEN ASTER_API \
  ANYTLS_ID ANYTLS_REV ANYTLS_SUB OLD_ANYTLS_SUB \
  VLESS_ID VLESS_REV VLESS_SUB OLD_VLESS_SUB
unset -f acurl
```

關閉 SSH tunnel，並確認沒有把 terminal scrollback、`jq` response 或 subscription URL 上傳到工單系統。

## 下一步

- [Aster Admin API 欄位與 status codes](/aster/api)
- [安全、state store 與 reverse proxy](/aster/security)
- [AnyTLS + REALITY 詳細部署](/tutorials/anytls-reality)
- [Linux 正式部署](/tutorials/linux-production)
- [疑難排解](/troubleshooting)
