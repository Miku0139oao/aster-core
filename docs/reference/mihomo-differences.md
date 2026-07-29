# Aster 與 Mihomo 差異

## 基線與定位

Aster Core 的上游基線是 MetaCubeX/mihomo `v1.19.29`：

```text
e26714a181ac0e2fa803453c0a8e9a9ce94e31cb
```

Fork 建立後的主要變更分為三組：

1. `bootstrap Aster core fork`：建立獨立 module、命名、Aster 管理、發行與相容封裝。
2. `optimize Aster runtime and management paths`：改善查找、流量、snapshot、listener 更新及連線生命週期。
3. `harden CI and enforce code quality`：固定 lint、race 與多版本測試，修正 fork 內品質問題。

## 能力邊界

| 能力 | 來源 | 說明 |
| --- | --- | --- |
| Mihomo YAML | Mihomo | Aster 沿用設定模型與預設值 |
| Clash Controller API | Mihomo | Dashboard 與既有工具可繼續使用 |
| DNS、fake-IP、TUN、rules | Mihomo | Aster 沒有改成另一套資料平面 |
| 各種 inbound/outbound | Mihomo 為主 | VLESS、AnyTLS 另加入 managed-user hooks |
| AnyTLS + REALITY | Aster | 相對 `v1.19.29` 基線新增 listener、outbound、URI 匯入及受管訂閱輸出 |
| `/api/admin` | Aster | 專用管理 API |
| `/sub/aster/{token}` | Aster | 單一 managed user 訂閱 |
| 逐使用者持久化流量 | Aster | 由 connection tracker principal 回報 |
| `aster-state.json` | Aster | 獨立安全 state store |
| OpenWrt/Nikki alternative | Aster | 保留 Mihomo compatibility path |

## 1. 獨立專案識別

Aster 將 Go module 改為：

```text
github.com/Miku0139oao/aster-core
```

發行物、Docker image、systemd units 與套件名稱使用 `aster-core`。為了既有工具相容：

- `-v` 仍輸出 `Mihomo Meta` 前綴。
- Linux 套件提供 `/usr/bin/mihomo` 相容連結。
- OpenWrt package 提供 virtual `mihomo` 及 alternatives。

## 2. AnyTLS + REALITY

Mihomo `v1.19.29` 基線已有 AnyTLS 與憑證 TLS、ShadowTLS、ResTLS、JLS，但 AnyTLS 尚未接上 REALITY。Aster 新增：

- Listener `reality-config`，讓 Aster 作為 AnyTLS + REALITY server。
- Outbound `reality-opts` 與 `client-fingerprint`，讓 Aster 作為 AnyTLS + REALITY client。
- `anytls://` URI 的 `security=reality`、`pbk`、`sid`、`sni`、`fp` 轉換。
- Managed user 訂閱自動輸出帶 REALITY public key 的 AnyTLS 分享連結。
- REALITY transport 與動態 password、撤銷連線及 listener close lifecycle 的整合。

這是一項 Aster 資料平面擴充，不只是管理 API 包裝。完整設定見 [AnyTLS + REALITY](/reference/anytls-reality)。

## 3. Managed VLESS 與 AnyTLS

只有實作 `ManagedUserListener` 的 listener 才能加入：

```yaml
aster:
  managed-listeners:
    - edge-vless
    - edge-anytls
```

目前只有：

- VLESS：以 UUID 為 credential，可選 flow。
- AnyTLS：以 password 為 credential。

管理變更直接更新執行中的 authentication state，不需要關閉並重開 listener。未列入 `managed-listeners` 的 listener 仍由一般 YAML 設定管理。

## 4. 獨立 Admin API

Aster API 使用 `/api/admin`，不共用 Controller `secret`：

```http
Authorization: Bearer <aster-secret>
```

它提供：

- Overview、protocol capability、listener 狀態。
- User list、create、read、update、delete。
- 流量重設。
- 訂閱 token 輪替。

此 API 不替代 Clash API。兩者同時存在、權限分離，適合讓 Dashboard 與管理後台使用不同 credentials。

## 5. Revision 與衝突控制

每個 managed listener 有：

- `revision`：持久化狀態的目前版本。
- `applied_revision`：runtime 已套用版本。

Mutation 必須帶目前 revision。若另一個管理者已先更新，舊 request 會收到：

```text
409 Conflict
```

這可防止面板、CLI 或多個後台彼此覆蓋變更。

## 6. Per-principal 流量

Aster 在 connection tracker 加入 principal：

```text
Inbound + UserID
```

統計包含：

- `upload_bytes`
- `download_bytes`
- `active_connections`
- `traffic_generation`

流量不是每個 packet 都同步寫入磁碟，而是先在記憶體聚合，再由 manager 批次 flush，降低熱路徑鎖定與 I/O。

## 7. 安全持久化

預設 store：

```text
<home>/aster-state.json
<home>/aster-state.json.bak
```

與一般單檔 JSON 不同，Aster store 加入：

- 跨程序 lock。
- Generation conflict check。
- Temp file + atomic replacement。
- Primary/backup 交叉恢復。
- 16 MiB 大小上限。
- 拒絕 symlink 與非 regular file。
- Unix owner-only 目錄及 `0600`。
- Windows ACL 驗證與修正。

Store 仍是**明文 JSON**。其安全性依賴檔案系統權限，不是內容加密。

## 8. 訂閱

啟用 `public-base-url` 後，每個 eligible user 可取得可輪替 token：

```text
https://proxy.example.com/sub/aster/<token>
```

回應是 Base64 編碼的單一分享連結。支援範圍：

- VLESS TCP、WebSocket、gRPC、基本 XHTTP。
- VLESS/AnyTLS 的 TLS 或 REALITY。

不輸出：

- ShadowTLS
- ResTLS
- JLS
- 進階 XHTTP placement、padding 等欄位

## 9. 管理路徑效能與生命週期

Fork 的管理熱路徑加入：

- ID 與 subscription token 索引，避免反覆線性掃描。
- 只複製目標 listener，而非每次深複製完整 store。
- Management snapshot 一次取得一致的 listener/user view。
- 活動連線按 principal 聚合。
- Credential 更新期間追蹤 handshake 與連線歸屬。
- Revoked credential 關閉與既有連線保留規則。
- AnyTLS/VLESS 更新失敗時的 fail-closed 行為。

這些是 Aster 管理層優化，不代表整個 Mihomo 資料平面被改寫。

## 10. 發行與 CI

Aster 新增：

- `aster-core` 多平台 release asset。
- Docker Hub multi-arch image。
- deb、rpm、pacman package 與 systemd units。
- OpenWrt 24.10+ recipe。
- Nix flake。
- Go 1.20–1.26 CI。
- 管理、安全、store、lifecycle、performance 與 race tests。
- 固定 `golangci-lint v2.12.2`、`govet`、`staticcheck`、`gci`、`gofumpt`。

## 遷移自 Mihomo

一般 client profile 通常可直接驗證：

```sh
aster-core -d /path/to/mihomo-home -t
```

需要留意：

1. `type: relay` 已移除，改用 `dialer-proxy`。
2. 自行建置要加 `with_gvisor` 才接近正式 release。
3. Aster 管理不會自動接管所有 listeners，必須明確列出。
4. Aster secret 與 Controller secret 不同。
5. Aster store 含敏感資料，需要正確 owner 與 permissions。
6. 使用訂閱前要設定 HTTPS `public-base-url` 並設計 reverse proxy。
7. AnyTLS + REALITY 是 Aster 擴充；需要使用支援相同 URI 與 REALITY 欄位的 client。

## 上游同步政策

上游變更不是自動覆蓋。更新 Mihomo 基線時必須保留：

- Aster management API。
- Managed listener hooks。
- Principal traffic accounting。
- Store security 與 recovery。
- Subscription compatibility。
- OpenWrt/Nikki compatibility。
- Fork-specific tests。

詳細政策見 [UPSTREAM.md](https://github.com/Miku0139oao/aster-core/blob/main/UPSTREAM.md)。
