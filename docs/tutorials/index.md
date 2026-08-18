# 實戰教學

教學依使用情境分開，每篇都附完整設定、驗證方式和常見錯誤。

::: warning
範例中的 `<SERVER_IP>`、`<PASSWORD>`、`<PUBLIC_KEY>` 等值需要自行替換。正式密碼、私鑰和訂閱網址不要提交到 Git。
:::

## 客戶端

### [第一個代理設定](/tutorials/first-proxy)

從[下載](/downloads) Aster Core 開始，建立本機 HTTP／SOCKS5 代理，加入一個 AnyTLS + REALITY 節點，最後用 `curl` 確認連線。

### [路由與 DNS](/tutorials/routing-dns)

設定哪些流量走代理、哪些直接連線，並處理 fake-IP、DNS policy 與 rule provider。

### [AnyTLS + REALITY](/tutorials/anytls-reality)

將服務端提供的節點位址、密碼、SNI、public key 和 short ID 寫入 Aster 設定。服務端可使用 Xray、sing-box、SideraCore 或其他相容實作。

### [故障排查](/tutorials/troubleshooting)

依設定錯誤、DNS、REALITY、連接埠、UDP 和系統服務等症狀逐項檢查。

## Aster 服務端

以下內容只和 Aster 自帶的入站及管理功能有關：

- [用 Aster 建立 AnyTLS + REALITY 服務端](/tutorials/anytls-reality#aster-服務端)
- [管理 VLESS／AnyTLS 使用者與訂閱](/tutorials/user-management)
- [Debian／Ubuntu VPS 部署](/tutorials/linux-production)
