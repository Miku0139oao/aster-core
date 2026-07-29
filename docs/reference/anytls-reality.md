# AnyTLS + REALITY

## 這是 Aster 新增的能力

Mihomo `v1.19.29` 已包含 AnyTLS，但該基線的 AnyTLS security 只有憑證 TLS、ShadowTLS、ResTLS 與 JLS。Aster 在此基礎上補齊：

- AnyTLS listener 的 `reality-config`。
- AnyTLS outbound 的 `reality-opts` 與 uTLS `client-fingerprint`。
- `anytls://` REALITY 分享連結匯入。
- Aster managed user 訂閱的 AnyTLS + REALITY 連結輸出。
- REALITY listener 與 AnyTLS 即時使用者更新、連線追蹤及生命週期處理整合。

因此 Aster 可同時作為 AnyTLS + REALITY 伺服器與用戶端，不需要在前方另放 TLS 憑證終結器。

## 產生 REALITY 金鑰

```sh
aster-core generate reality-keypair
```

輸出包含：

```text
PrivateKey: <server-private-key>
PublicKey: <server-public-key>
```

- Private key 只放在伺服器的 `reality-config.private-key`。
- Public key 提供給用戶端的 `reality-opts.public-key` 或分享連結 `pbk`。
- 不要把正式環境的 private key、AnyTLS password 或 Aster secret 提交到 Git。

## 伺服器設定

```yaml
listeners:
  - name: edge-anytls
    type: anytls
    listen: 0.0.0.0
    port: 443
    users:
      alice: "replace-with-a-long-random-password"
    reality-config:
      dest: www.microsoft.com:443
      private-key: <server-private-key>
      short-id:
        - 0123456789abcdef
      server-names:
        - www.microsoft.com
```

### `reality-config` 欄位

| 欄位 | 必要 | 說明 |
| --- | --- | --- |
| `dest` | 是 | REALITY 驗證失敗時的 fallback TLS 目的地，格式為 `host:port` |
| `private-key` | 是 | `generate reality-keypair` 產生的 X25519 private key |
| `short-id` | 建議 | Hex 字串；每個值解碼後最多 8 bytes |
| `server-names` | 是 | 接受的 SNI 清單；用戶端 `sni` 必須匹配其中一個 |
| `max-time-difference` | 否 | 可接受時間差，單位為 microseconds；`0` 使用實作預設行為 |
| `proxy` | 否 | REALITY fallback 連線使用的指定 outbound |
| `limit-fallback-upload` | 否 | Fallback upload 限速 |
| `limit-fallback-download` | 否 | Fallback download 限速 |

`certificate`/`private-key` 憑證 TLS、`shadow-tls`、`res-tls`、`jls-config` 與 `reality-config` 是互斥 security mode。一個 AnyTLS listener 只能選一種。

## 用戶端設定

```yaml
proxies:
  - name: edge-anytls-reality
    type: anytls
    server: proxy.example.com
    port: 443
    password: "replace-with-a-long-random-password"
    sni: www.microsoft.com
    client-fingerprint: chrome
    reality-opts:
      public-key: <server-public-key>
      short-id: 0123456789abcdef
    udp: true
```

### 用戶端欄位

| 欄位 | 必要 | 說明 |
| --- | --- | --- |
| `server` | 是 | Aster 伺服器 IP 或網域；不是偽裝站 |
| `port` | 是 | AnyTLS listener port |
| `password` | 是 | `users` 中對應的 AnyTLS password |
| `sni` | 是 | 必須匹配伺服器 `server-names` |
| `client-fingerprint` | 是 | REALITY 使用 uTLS；一般使用 `chrome` |
| `reality-opts.public-key` | 是 | 伺服器 public key |
| `reality-opts.short-id` | 視伺服器而定 | 必須是伺服器允許的 short ID |
| `reality-opts.support-x25519mlkem768` | 否 | 啟用 X25519MLKEM768；須確認對端相容性 |
| `udp` | 否 | 透過 AnyTLS 的 UoT 支援 UDP |

AnyTLS outbound 的 `reality-opts`、`shadow-tls-opts`、`restls-opts` 與 `jls-opts` 同樣互斥。

## 分享連結

Aster 可匯入下列格式：

```text
anytls://<password>@proxy.example.com:443?security=reality&sni=www.microsoft.com&fp=chrome&pbk=<server-public-key>&sid=0123456789abcdef#Aster-AnyTLS-REALITY
```

REALITY query：

| Query | 對應 YAML |
| --- | --- |
| `security=reality` | 啟用 AnyTLS REALITY |
| `sni` | `sni` |
| `fp` | `client-fingerprint`；省略時 Aster 匯入預設為 `chrome` |
| `pbk` | `reality-opts.public-key`，不可省略 |
| `sid` | `reality-opts.short-id` |

一般 AnyTLS URI 可把 password 放在 userinfo 的 username 部分。若使用 `username:password@host` 形式，Aster 取 password 部分作為 credential。

## 搭配 Aster 即時管理與訂閱

```yaml
external-controller: 127.0.0.1:9090

aster:
  secret: "replace-with-at-least-32-random-bytes"
  state-file: ./aster-state.json
  public-base-url: https://proxy.example.com
  managed-listeners:
    - edge-anytls
```

加入 `managed-listeners` 後：

1. YAML `users` 會在首次啟動時匯入 Aster state。
2. Admin API 可即時新增、修改、停用或刪除 AnyTLS password，不重建 listener。
3. 每個啟用的使用者都有獨立流量與活動連線統計。
4. 使用者可取得可輪替 `/sub/aster/{token}`。
5. 訂閱內容是 Base64 編碼的單一 `anytls://` REALITY 連結。

Aster 會由 server private key 推導 public key，並從設定中選擇排序後第一個 `server-names` 與 `short-id` 寫入訂閱。因此若希望輸出的分享連結固定，應把預期使用的 SNI 與 short ID 明確放入設定。

## 部署檢查

- `dest` 應為伺服器可連線、TLS 行為穩定且與偽裝 SNI 合理對應的站點。
- `server` 是 Aster 主機；`sni` 才是偽裝名稱，兩者不要混淆。
- Port 443 若已由 Caddy、Nginx 或其他服務占用，需要先設計 TCP/SNI 分流。
- 時鐘偏差可能造成 REALITY 驗證失敗；確認 NTP 正常。
- 不要搭配 `skip-cert-verify` 來掩蓋錯誤的 public key、SNI 或 short ID。
- 修改 REALITY private key、SNI 或 short ID 後，舊訂閱／用戶端資料也必須同步更新。
- 只有 password 是即時 managed credential；REALITY transport 設定仍由 YAML 與設定 reload 管理。

## 驗證

先檢查設定：

```sh
aster-core -d /etc/aster-core -t
```

再確認：

1. 伺服器 log 顯示 AnyTLS listener 已綁定預期 port。
2. 用戶端使用正確 `pbk`、`sid`、`sni` 與 `fp`。
3. TCP 與 UDP/UoT 分別測試。
4. Admin API 更新 password 後，新 credential 可立即使用，已撤銷 credential 無法建立新連線。
5. 訂閱回傳的 `anytls://` URL 可被 Aster 重新匯入。

相關頁面：

- [入站設定](/reference/inbounds)
- [出站與代理群組](/reference/outbounds)
- [Aster 管理功能](/aster/overview)
- [Admin API](/aster/api)
- [Aster 與 Mihomo 差異](/reference/mihomo-differences)
