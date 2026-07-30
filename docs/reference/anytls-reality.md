# AnyTLS + REALITY

Aster 通常作為 AnyTLS + REALITY 客戶端，連接 Xray、sing-box、SideraCore、Aster 或其他相容服務端。完整部署流程見[實戰教學](/tutorials/anytls-reality)。

```text
你的 App → Aster Core → AnyTLS + REALITY 節點 → 網際網路
```

服務端需提供節點網址、連接埠、密碼、SNI、公開金鑰和 short ID。

## 客戶端設定

```yaml
proxies:
  - name: anytls-reality
    type: anytls
    server: proxy.example.com
    port: 443
    password: "replace-with-your-password"
    sni: www.microsoft.com
    client-fingerprint: chrome
    reality-opts:
      public-key: <server-public-key>
      short-id: 0123456789abcdef
    udp: true
```

### 欄位說明

| 欄位 | 要填的內容 |
| --- | --- |
| `name` | 自己看得懂的節點名稱 |
| `server` | 真正要連線的節點 IP 或網域，不是偽裝網站 |
| `port` | 服務端提供的連接埠 |
| `password` | 服務端提供的 AnyTLS 密碼 |
| `sni` | 服務端提供的偽裝網站名稱 |
| `client-fingerprint` | 模擬的瀏覽器類型；不確定時使用 `chrome` |
| `public-key` | 服務端提供的 REALITY 公開金鑰 |
| `short-id` | 服務端提供的一小段識別碼；沒有提供時才省略 |
| `udp` | 設為 `true`，讓需要 UDP 的程式也能使用 |

::: danger 金鑰方向
客戶端只需要 public key（公開金鑰）。private key（私鑰）只能留在服務端。
:::

## 分享連結

Aster 支援這種格式：

```text
anytls://<password>@proxy.example.com:443?security=reality&sni=www.microsoft.com&fp=chrome&pbk=<public-key>&sid=<short-id>#AnyTLS-REALITY
```

| 分享連結內容 | 代表什麼 |
| --- | --- |
| `<password>` | AnyTLS 密碼 |
| `proxy.example.com:443` | 節點網址和連接埠 |
| `sni` | 偽裝網站 |
| `fp` | 瀏覽器指紋 |
| `pbk` | REALITY 公開金鑰 |
| `sid` | short ID |
| `#AnyTLS-REALITY` | 顯示名稱，可自行修改 |

如果使用 `username:password@host` 的寫法，Aster 會使用冒號後面的內容當作密碼。

## 常見錯誤

| 看到的情況 | 先檢查 |
| --- | --- |
| 立即顯示連線被拒絕 | `server`、`port`、防火牆，以及服務端是否啟動 |
| 一直等待後逾時 | DNS 是否正確、服務端是否可從外網連線 |
| 可以連到 port，但代理仍失敗 | `password`、`sni`、`public-key`、`short-id` |
| 網頁可開，但遊戲或語音失敗 | 是否設定 `udp: true`，以及服務端是否支援 |
| 匯入分享連結後不能用 | 核對 `pbk`、`sid`、`sni` 和 `fp` 是否完整 |

不要用 `skip-cert-verify` 掩蓋錯誤的公開金鑰、SNI 或 short ID。

## Aster 服務端

Aster 也能直接接收 AnyTLS + REALITY 連線：

先產生金鑰：

```sh
aster-core generate reality-keypair
```

你會得到：

```text
PrivateKey: <server-private-key>
PublicKey: <server-public-key>
```

- PrivateKey 只放在服務端。
- PublicKey 提供給客戶端。

服務端設定：

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

| 服務端欄位 | 說明 |
| --- | --- |
| `listen` | `0.0.0.0` 代表接收所有 IPv4 網路介面的連線 |
| `port` | 對外開放的 TCP 連接埠 |
| `users` | 使用者名稱和密碼 |
| `dest` | 驗證失敗時轉往的正常 HTTPS 網站 |
| `private-key` | 服務端私鑰，不可外流 |
| `short-id` | 服務端允許的 short ID |
| `server-names` | 客戶端可以使用的 SNI |

同一個 AnyTLS 服務不能同時使用 REALITY、一般憑證 TLS、ShadowTLS、ResTLS 或 JLS；只能選一種。

## 即時管理使用者

把 AnyTLS 服務加入 `managed-listeners` 後，可以在不重新啟動整個服務的情況下新增、修改、停用或刪除密碼。

```yaml
external-controller: 127.0.0.1:9090

aster:
  secret: "replace-with-at-least-32-random-bytes"
  state-file: ./aster-state.json
  public-base-url: https://proxy.example.com
  managed-listeners:
    - edge-anytls
```

完整操作見[使用者管理教學](/tutorials/user-management)。

## 相關文件

- [AnyTLS + REALITY 逐步教學](/tutorials/anytls-reality)
- [第一個代理設定](/tutorials/first-proxy)
- [遠端節點設定](/reference/outbounds)
- [故障排查手冊](/tutorials/troubleshooting)
