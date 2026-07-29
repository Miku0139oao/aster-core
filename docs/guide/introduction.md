# Aster Core 是什麼？

Aster Core 是一個**代理客戶端核心**。它不是有按鈕和視窗的完整 App，而是負責在背景處理連線的程式。

它通常安裝在：

- 電腦。
- 路由器。
- 家用伺服器或網路閘道。
- 把 Aster 包裝成圖形介面的 App 裡。

## 它怎麼工作？

```text
瀏覽器或 App
      ↓
  Aster Core
      ↓
遠端代理節點
      ↓
    網際網路
```

遠端代理節點一般由 Xray、sing-box、SideraCore 或服務商的系統提供。你需要先取得節點資料，例如網址、連接埠、密碼和金鑰，再交給 Aster 使用。

## Aster 可以做什麼？

- 連接 VLESS、VMess、Trojan、AnyTLS、Hysteria 等節點。
- 讓瀏覽器或其他程式使用本機 HTTP／SOCKS5 代理。
- 使用 TUN 接管不支援手動代理的程式。
- 依網站、IP、程式或地區決定是否使用代理。
- 處理 DNS，避免查到錯誤位址或走錯路線。
- 自動測速，或讓你手動切換節點。
- 讓相容 Clash 的控制面板查看狀態。

## Aster 比 Mihomo 多了什麼？

Aster 建立在 Mihomo 之上，大部分設定和功能都相同。Aster 另外加入：

- AnyTLS + REALITY 客戶端支援。
- AnyTLS 分享連結匯入。
- 修正設定重新載入、斷線重連、UDP、DNS、更新等問題。
- 減少大量連線或使用者時不必要的 CPU、記憶體和磁碟使用。

詳細但仍然白話的說明請看[Aster 跟 Mihomo 有什麼不同](/reference/mihomo-differences)。

## 我需要在服務端安裝 Aster 嗎？

**通常不需要。**

一般服務端使用 Xray、sing-box 或 SideraCore。Aster 只要拿到正確的連線資料，就可以作為客戶端連線。

Aster 也能自己接收 VLESS 或 AnyTLS 連線，但這是給測試、全 Aster 環境或特殊需求使用的進階功能。第一次使用時可以完全忽略。

## 設定格式要注意什麼？

Aster 使用 Mihomo 的 YAML 設定，不能直接讀取 Xray 或 sing-box 的 JSON。

你不是要複製整份服務端設定，只需要拿出這些連線資料：

- 節點網址。
- 連接埠。
- 協定類型。
- 密碼或 UUID。
- SNI。
- REALITY public key 和 short ID。
- 傳輸方式，例如 TCP、WebSocket 或 gRPC。

把資料填入對應的 Aster 範例即可。

## 下一步

第一次使用請直接前往[第一個代理設定](/tutorials/first-proxy)。如果你已經拿到 AnyTLS + REALITY 節點，請看[AnyTLS + REALITY 客戶端教學](/tutorials/anytls-reality)。
