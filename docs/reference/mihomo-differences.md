# Aster 跟 Mihomo 有什麼不同？

簡單說：**Aster 保留 Mihomo 的用法，再加入新功能、問題修正與效能改善。**

如果你已經會用 Mihomo，大部分設定可以繼續使用。如果你是第一次接觸代理核心，也不用先研究程式碼；從[第一個代理設定](/tutorials/first-proxy)照著做即可。

## Aster 主要用來做什麼？

Aster Core 主要安裝在你的電腦、路由器或閘道上，負責：

- 連接遠端代理節點。
- 決定哪些網站要走代理。
- 處理 DNS，降低網域解析被干擾的機會。
- 提供 HTTP、SOCKS5 或 TUN 給其他程式使用。
- 讓相容 Clash 的面板查看及切換節點。

遠端服務端通常使用 Xray、sing-box、SideraCore 或服務商提供的系統。**一般使用者不需要在服務端安裝 Aster。**

## Aster 多了什麼？

### 1. AnyTLS + REALITY

Aster 可以連接 AnyTLS + REALITY 節點，也可以匯入相容的 `anytls://` 分享連結。

服務端或服務商需要提供：

- 節點網址與連接埠。
- 密碼。
- SNI，也就是用來偽裝的網站名稱。
- REALITY public key，也就是公開金鑰。
- short ID，也就是服務端指定的一小段識別碼。

拿到這些資料後，照[AnyTLS + REALITY 教學](/tutorials/anytls-reality)填入即可。

### 2. 連線更穩定

Aster 修正了 Mihomo 目前版本仍可能遇到的幾類問題：

| 原本可能看到的情況 | Aster 的改善 |
| --- | --- |
| 重新載入設定後，舊連接埠沒有正常釋放 | 改善服務關閉及重新載入流程 |
| Hysteria 使用 UDP 時偶爾收錯資料或重連異常 | 修正 UDP 分段、重連及連接埠切換問題 |
| VLESS 在少數封包情況下斷線 | 修正封包讀寫及大小檢查 |
| XHTTP 關閉時卡住或留下舊連線 | 改善關閉及連線清理 |
| DNS 偶爾回傳損壞或不完整的內容 | 修正 DNS 回應資料處理 |
| 更新下載不完整或誤裝較舊版本 | 加入檔案檢查及版本保護 |

你不需要理解內部名稱。實際效果就是：更新設定、斷線重連、UDP、DNS 與自動更新時比較不容易出現奇怪問題。

### 3. 使用較少的額外資源

Aster 減少了幾種不必要的重複工作：

- 使用者很多時，不必每次從頭尋找。
- 流量統計先在記憶體整理，再分批儲存。
- 更新一名使用者時，不必複製全部資料。
- 查詢目前連線數時，不必每次重新掃描所有連線。
- 部分 VLESS 與 Hysteria 資料可用更少的暫存空間處理。

這些改善不代表所有網路都會固定快多少。它們主要降低大量連線或大量使用者時的 CPU、記憶體及磁碟負擔。

### 4. 可選的服務端功能

Aster 也能接收 VLESS 或 AnyTLS 連線，並可在不中斷整個服務的情況下新增、停用或修改使用者。

這是進階功能，只適合：

- 測試環境。
- 客戶端與服務端都想使用 Aster。
- 確實需要 Aster 的使用者管理介面。

一般服務端仍建議使用你熟悉的 Xray、sing-box 或 SideraCore。

## 哪些東西跟 Mihomo 一樣？

Aster 繼續使用：

- Mihomo 的 YAML 設定格式。
- 規則、代理群組及訂閱提供者。
- DNS、fake-IP 與 TUN。
- Clash 相容控制介面及面板。
- Mihomo 支援的大部分連線協定。

注意：Aster 不能直接讀取 Xray 或 sing-box 的 JSON 設定。你需要把服務端提供的「網址、連接埠、密碼、金鑰」填入 Aster 設定，或匯入支援的分享連結。

## 我該從哪裡開始？

| 你的情況 | 請看 |
| --- | --- |
| 第一次使用 | [第一個代理設定](/tutorials/first-proxy) |
| 已有 AnyTLS + REALITY 節點 | [AnyTLS + REALITY 教學](/tutorials/anytls-reality) |
| 網站打不開或連不上 | [故障排查手冊](/tutorials/troubleshooting) |
| 從 Mihomo 換過來 | 先備份設定，再執行 `aster-core -d <設定資料夾> -t` 檢查 |
