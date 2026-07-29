# 實戰教學

不要先讀完整設定手冊。選擇你現在要做的事，照著步驟完成即可。

::: warning 範例不能直接使用
教學中的 `<SERVER_IP>`、`<PASSWORD>`、`<PUBLIC_KEY>` 等文字都要換成你自己的節點資料。不要把正式密碼、私鑰或訂閱網址貼到公開網站。
:::

## 第一次使用

請依序閱讀：

1. [第一個代理設定](/tutorials/first-proxy)
2. [路由與 DNS 分流](/tutorials/routing-dns)
3. 遇到問題時看[故障排查手冊](/tutorials/troubleshooting)

第一篇會帶你完成下載、建立設定、填入節點、啟動 Aster 和測試連線。

## 已經有 AnyTLS + REALITY 節點

直接閱讀[AnyTLS + REALITY 客戶端教學](/tutorials/anytls-reality)。

你需要向服務端或服務商取得：

- 節點網址和連接埠。
- 密碼。
- SNI。
- REALITY public key。
- short ID。

服務端通常使用 Xray、sing-box 或 SideraCore。客戶端與服務端不必都安裝 Aster。

## 想設定哪些網站走代理

閱讀[路由與 DNS 分流](/tutorials/routing-dns)。這篇會教你：

- 指定網站走代理或直接連線。
- 避免 DNS 走錯路線。
- 檢查規則是否真的生效。

## 連不上或網站打不開

閱讀[故障排查手冊](/tutorials/troubleshooting)。它會依你看到的錯誤，告訴你下一步要檢查什麼。

## 進階：讓 Aster 當服務端

一般使用者不需要閱讀以下內容。服務端通常使用 Xray、sing-box 或 SideraCore。

只有在你確定要讓 Aster 接收連線時，再閱讀：

1. [用 Aster 架設 AnyTLS + REALITY 測試節點](/tutorials/anytls-reality#可選服務端路線)
2. [管理 VLESS／AnyTLS 使用者](/tutorials/user-management)
3. [在 Debian／Ubuntu VPS 長期執行 Aster](/tutorials/linux-production)
