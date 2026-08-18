# Aster 與 Mihomo

Aster Core 以 Mihomo `v1.19.29` 為基線。設定格式、規則引擎、DNS、TUN、代理群組和 Clash-compatible API 都沿用自 Mihomo；Aster 的改動包含 AnyTLS + REALITY、AmneziaWG v3/v3.1、H2C/HTTP2 與 QUIC v2 嗅探改善、Linux/OpenWrt Kernel DIRECT、問題修正、執行效率，以及選用的服務端管理功能。

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

- TCP relay 重用 buffer，UDP adapter 與 metadata 使用物件池
- UDP NAT 使用 typed key、不可變 mapping snapshot，並合併 deadline refresh
- 規則與代理列表以原子快照發布，封包匹配不持有全域設定讀鎖
- AnyTLS 預先解析 padding scheme，並直接組裝 data/control frame
- 關閉 debug log 時在格式化與建立 event 前返回
- 以索引查找使用者與訂閱 token；流量採原子累加與批次寫入
- 更新單一 listener 時只複製相關資料，Aster state 使用較精簡的 JSON

最新微基準中的穩態 UDP、32 KiB TCP relay 與常見大小的 AnyTLS upload 都達到每次操作零配置。完整數據、測試環境與重跑方式見[效能優化與基準](/reference/performance)。實際差異仍會隨設定、連線數、協議、作業系統和硬體而變。

## Linux／OpenWrt Kernel DIRECT

Aster 可從經過自身 DNS 的真實 A/AAAA 回應，以及已判定為 `DIRECT`／`Compatible` 的 live flow（含 unwrap 後的選擇器），保守學習 DIRECT 目的位址，將其加入 nftables auto-redirect exclude set，讓後續新連線留在 Linux kernel forwarding/NAT path。共享 IP 採 proxy-wins，無法只由目的網域/IP 等價判斷的規則不會 bypass。Aster **不會**丟 inbound REDIR／TUN SYN 來防自劫持；那會把 DIRECT 一起黑洞。

這項 `kernel-direct` 功能不要求 eBPF；推薦 backend 是 nftables 加 OpenWrt flow offload。另有預設關閉的實驗性 TC eBPF DIRECT／PROXY classifier，功能上更接近 dae 的 ingress hook，但逐封包工作可能降低吞吐量，必須在目標路由器做同 server A/B 後才啟用。設定、安全界線、IPv6 split-WAN 排查與實測反例見 [OpenWrt 與 Nikki](/deployment/openwrt)。

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
