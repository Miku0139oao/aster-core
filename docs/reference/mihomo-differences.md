# Aster 與 Mihomo

Aster Core 以 Mihomo `v1.19.29` 為基線。設定格式、規則引擎、DNS、TUN、代理群組和 Clash-compatible API 都沿用自 Mihomo；Aster 的改動主要集中在 AnyTLS + REALITY、問題修正、執行效率，以及選用的服務端管理功能。

## AnyTLS + REALITY

Aster 為 AnyTLS 加入：

- 客戶端 `reality-opts` 與 uTLS fingerprint
- `anytls://` REALITY 分享連結匯入
- Aster 入站的 `reality-config`
- 受管使用者的分享連結輸出

客戶端只需要服務端提供的節點位址、連接埠、密碼、SNI、public key 和 short ID。完整範例見[AnyTLS + REALITY](/tutorials/anytls-reality)。

## 上游問題修正

Aster 修正了目前 Mihomo 基線仍存在的幾類問題：

| 範圍 | 修正內容 |
| --- | --- |
| 設定重新載入 | 避免舊連接埠未釋放、部分服務已更新但其餘失敗，以及關閉時留下連線 |
| Hysteria UDP | 修正分片交錯、重連狀態、message ID 和 port hopping socket |
| VLESS | 修正封包讀寫失步、超大 frame 及同時讀寫問題 |
| XHTTP | 修正關閉卡住、重複關閉及舊 session 清理 |
| DNS | 修正部分壓縮回應沒有正確寫回原始 buffer |
| 核心更新 | 檢查 HTTP 狀態、版本、檔案大小與 checksum，並阻止意外降版 |
| Controller | 改善 reload、debug 驗證及本機 socket 權限 |

這些修正主要影響長時間運行、頻繁更新設定、UDP 或網路不穩定的環境。

## 效能調整

- 以索引查找使用者與訂閱 token
- 更新單一 listener 時只複製相關資料
- 流量先累積後批次寫入
- 活動連線數採增量統計
- 減少 Hysteria UDP 與 VLESS 封包組裝時的配置
- Aster state 使用較精簡的 JSON

實際差異會隨設定、連線數和硬體而變；這些調整主要避免管理功能在大量使用者或連線下造成額外負擔。

## Aster 服務端管理

Aster 可即時新增、更新、停用或刪除 VLESS／AnyTLS 使用者，不需重建整個 listener。管理資料包含：

- 每個 listener 的版本號，避免同時修改時互相覆蓋
- 每名使用者的上傳、下載和活動連線
- 可輪替的單一使用者訂閱連結
- 帶備份、鎖定和權限檢查的 `aster-state.json`

這套管理功能是選用的。只把 Aster 當客戶端時，不需要設定 `aster:` 或 `/api/admin`。

## 相容性

既有 Mihomo profile 通常可直接使用，但仍建議先備份並執行：

```sh
aster-core -d <設定資料夾> -t
```

需要留意：

- Aster 使用 Mihomo YAML，不直接讀取 Xray／sing-box JSON。
- `type: relay` 已移除，請改用 `dialer-proxy`。
- AnyTLS + REALITY 必須由兩端共同支援相同參數。
- Aster Admin secret 與一般 Controller secret 分開。

來源和上游同步方式見 repository 內的 [NOTICE.md](https://github.com/Miku0139oao/aster-core/blob/main/NOTICE.md) 與 [UPSTREAM.md](https://github.com/Miku0139oao/aster-core/blob/main/UPSTREAM.md)。
