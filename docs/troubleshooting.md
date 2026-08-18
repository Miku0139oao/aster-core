# 疑難排解

::: tip 依症狀執行的 runbook
需要一條從 `-t`、socket、journal、DNS、REALITY、Controller、Aster state 到 UDP 的實際排查順序，請使用[故障排查手冊](/tutorials/troubleshooting)。
:::

## 找不到設定或讀到錯誤檔案

先顯式指定：

```sh
aster-core -d /absolute/home -f /absolute/config.yaml -t
```

相對 `-f` 以目前工作目錄解析，不是以 `-d` 解析。檢查：

```sh
pwd
ls -l /absolute/config.yaml
```

若未指定 `-f`，才會讀 `<home>/config.yaml`。

## `path is not subpath of home directory or SAFE_PATHS`

設定引用的 certificate、provider 或 store 位於 home 外。

首選：

1. 把檔案移到 home directory。
2. 或將可信根目錄加入 `SAFE_PATHS`。

不要為了解決單一 path 就長期設定：

```sh
SKIP_SAFE_PATH_CHECK=true
```

## Aster Admin 回傳 404

可能原因：

- 沒有 `aster` block。
- Controller 是明文 TCP 且綁定非 loopback。
- Route/host 指到錯誤 Controller。
- Managed listener 初始化失敗。

先確認：

```yaml
external-controller: 127.0.0.1:9090
aster:
  secret: "at-least-32-bytes..."
```

再從同一 host：

```sh
curl -v \
  -H "Authorization: Bearer $ASTER_TOKEN" \
  http://127.0.0.1:9090/api/admin/status
```

## Aster Admin 回傳 401

- 使用了 Controller secret，而不是 Aster secret。
- `Bearer` 大小寫或空格錯誤。
- Secret 有複製換行。

正確格式：

```http
Authorization: Bearer actual-aster-secret
```

## Aster Admin 回傳 403

Same-origin 失敗。檢查：

- Browser `Origin` 是否等於 request scheme/host。
- Reverse proxy 是否正確覆寫 `Host`。
- `X-Forwarded-Proto` 是否為 `https`。
- Request 是否帶 `Sec-Fetch-Site: cross-site`。

面板建議透過同 origin backend/BFF，不要讓 browser 直接跨站呼叫。

## Mutation 回傳 409

Revision 已過期：

1. 重新 GET `/api/admin/inbounds`。
2. 重新 GET user。
3. 比較另一個管理者的變更。
4. 使用新 revision 重新提交。

不要只把 revision 加一後重試。

## Subscription 回傳 404

檢查：

- `public-base-url` 是否設定。
- User 是否 enabled。
- Token 是否已 rotate。
- Listener 是否仍在 `managed-listeners`。
- Listener 是否有可判定的 port。
- VLESS/AnyTLS security 是否可輸出。
- 是否用了 ShadowTLS、ResTLS、JLS 或 advanced XHTTP。

## Store 無法載入

常見原因：

- Parent directory 權限過寬。
- State file 不是 `0600`。
- Owner 錯誤。
- 檔案是 symlink。
- JSON 損壞。
- Primary 與 backup 都無效。
- State version 不支援。

不要立刻刪除兩份 state。先離線備份：

```sh
cp -a aster-state.json aster-state.json.forensics
cp -a aster-state.json.bak aster-state.json.bak.forensics
```

再檢查 log 判斷哪份有效。State 內有 credentials，forensics 檔同樣要保護。

## Docker publish port 無法連線

Bridge mode 需要：

```yaml
allow-lan: true
bind-address: "*"
```

並確認：

```sh
docker port aster-core
docker logs aster-core
```

Host network 與 Docker Desktop 行為不同；不要假設 `--network host` 跨平台一致。

## TUN 無法建立

檢查：

- Binary 是否帶 `with_gvisor`。
- `/dev/net/tun` 是否存在。
- Container 是否傳入 device。
- 是否有 `CAP_NET_ADMIN`。
- TUN name 是否衝突。
- Auto-route table/rule 是否衝突。
- 另一個 VPN 是否已接管 route。

```sh
aster-core -v
ip tuntap
ip rule
ip route show table all
```

## Kernel DIRECT 把所有節點與 DIRECT 一起打掛

OpenWrt 上 `auto-redirect` 會把 Aster 自己的 SYN 再導回來。若 core 在 `handleTCPConn` 把本機來源的 REDIR／TUN SYN 直接丟掉，DIRECT 和未綁定介面的節點會同時 timeout。正確行為是讓封包走完規則，由 `DIRECT.CheckConn`、kernel-direct exclude set 與 30 秒零位元組 TCP reaper 處理迴圈。

```sh
/usr/bin/mihomo -v
curl -sS -H "Authorization: Bearer $SECRET" \
  http://127.0.0.1:9090/proxies/DIRECT/delay?timeout=8000\&url=http://www.gstatic.com/generate_204
```

`-v` 應為不含 inbound-drop 實驗的發行；delay 應回數字而不是空或 timeout。

## Dual-WAN 延遲測試全部失敗

`tun.auto-detect-interface: true` 在 ECMP／macvlan／mwan3 上常讓 `FindInterfaceName` 回 `<invalid>`，socket bind 失敗。設成 `false`，並確認 Nikki `mixin.uc` 在 `tun_kernel_direct` 時沒有再寫回 `true`。

```sh
yq -M '.tun["auto-detect-interface"]' /etc/nikki/run/config.yaml
```

## 連線數與記憶體暴漲（大量零位元組 TCP）

Aster 的 DIRECT SYN 被 auto-redirect 再抓回來時，會留下沒有 payload 的 TCP tracker。30 秒 reaper 會關這些 TCP；UDP（含 Wi‑Fi 通話 `500`／`4500`）不會動。不要用 `DELETE /connections` 當清理手段。

先看 `GET /connections` 的 `upload`／`download` 是否為 0，以及來源是否為路由器自己的 WAN IP。接著確認 dest 有沒有進 `inet4_route_exclude_address_set`，以及 `GET /api/aster/kernel-direct/status` 的 `learned_sets`。

## Iptables 與 TUN 衝突

自動 iptables management 與 TUN 不能同時啟用。決定由誰負責透明攔截：

- TUN auto-route/auto-redirect，或
- External iptables/TProxy/Redir。

不要同時讓兩者修改相同流量。

## Proxy group `relay` 無法解析

`relay` 已移除。把 chain 移到 outbound：

```yaml
proxies:
  - name: hop-2
    type: vless
    dialer-proxy: hop-1
```

## SIGHUP 沒讀到新內容

若啟動方式是：

```sh
aster-core --config '<base64>'
aster-core -f -
```

SIGHUP 只會重新套用原始 bytes。要從磁碟重讀，使用正常 file mode。

## Provider 或 geodata 下載失敗

檢查：

- System time。
- CA bundle。
- DNS。
- Proxy chain 是否 circular。
- Safe path。
- URL/ETag。
- 執行環境是否可連 GitHub/API。

離線環境應預先放入 provider/geodata，不要依賴首次啟動下載。

## Windows named pipe

Pipe 必須以：

```text
\\.\pipe\
```

開頭。自訂 ACL 使用 `LISTEN_NAMEDPIPE_SDDL` 前，先理解 SDDL；過寬 ACL 會讓 Controller 暴露給其他 local users。

## 還是無法定位

收集：

- `aster-core -v`
- 作業系統與架構
- 已去除 secrets 的最小 config
- `-t` 完整輸出
- Runtime log
- 問題是否只在 TCP、UDP、DNS、IPv4、IPv6 或特定 listener

不要公開：

- UUID/password
- Private keys
- Aster/Controller secrets
- Subscription URLs/tokens
- 完整 state file
