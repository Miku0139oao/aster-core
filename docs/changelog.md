---
title: 更新紀錄
description: Aster Core 自 Mihomo v1.19.29 基準點以來的繁體中文變更紀錄
---

# 更新紀錄

> [!NOTE]
> 本頁是 Aster Core 的 rolling snapshot changelog，整理至 `2026-08-19` 的目前 `HEAD`。內容只涵蓋基準提交 `e26714a1`（Mihomo `v1.19.29`）之後的 Aster 時代變更；日期依提交日期整理，不代表正式版本號。

Aster Core 目前尚未發布正式的 Aster `v*` 版本。GitHub 上的 `Prerelease-main` 是持續更新中的預發布版本，使用前請留意它可能隨 `main` 改變。

- [查看 `e26714a1..main` 的完整 GitHub 比較](https://github.com/Miku0139oao/aster-core/compare/e26714a1..main)
- [取得 GitHub `Prerelease-main`](https://github.com/Miku0139oao/aster-core/releases/tag/Prerelease-main)
- [English version](/en/changelog)

完整的功能差異與相容性說明，請看[Aster 與 Mihomo 的差異](/reference/mihomo-differences)；本頁只整理有日期的 Aster 變更重點。

## 核心與 Kernel DIRECT

- **2026-07-29｜Aster 基礎：** 從 Mihomo 基準點建立 Aster Core，定位為客戶端優先的核心，同時加入 Aster 管理、使用者、訂閱與流量統計能力。
- **2026-08-12｜Kernel DIRECT 與流量治理：** 新增 Linux/OpenWrt Kernel DIRECT 路徑與流量治理，讓可安全判定為 `DIRECT` 的流量留在 kernel forwarding/NAT path；nftables + flow offload 是推薦 backend，TC eBPF classifier 保持為預設關閉的實驗功能。
- **2026-08-17–18｜DIRECT 安全界線：** 限制 kernel-direct cache，補上 loop safety、測試與驗證指令；TUN 不再丟棄被攔截的 SYN，並能學習代理群組選出的 `DIRECT`。詳細設定、風險與 OpenWrt 實測請看[OpenWrt 與 Nikki](/deployment/openwrt)。

## 協定與連線

- **2026-07-29｜AnyTLS + REALITY：** 加入 AnyTLS 客戶端與 Aster 入站的 REALITY 設定、uTLS fingerprint、`anytls://` 匯入，以及受管使用者的分享連結輸出。
- **2026-08-15–16｜連線生命週期：** 修正 AnyTLS、VLESS、XTLS Vision 與 Shadowsocks fallback listener 的關閉、雙向狀態同步及併發生命週期問題，讓重載與異常關閉更穩定。
- **2026-08-18｜AnyTLS metadata：** 支援 AnyTLS client metadata，預設不再傳送版本資訊；空 metadata 也有明確的 session 測試覆蓋。
- **2026-08-18｜XHTTP：** `uplink-chunk-size` 預設改為 `0`，並接受 `uplinkHTTPMethod`；原有的 `uplinkHttpMethod` alias 仍可使用。
- **2026-08-18｜Sniff：** 支援 H2C QUICv2 與多輪 sniff，能跨片段組合 TLS、QUIC 和 HTTP/2 handshake；sniff 失敗時保留連線並限制讀取量。
- **2026-08-18｜其他傳輸：** Hysteria2 新增 handshake timeout；RESTLS listener 新增 rate limit；MASQUE QUIC 的 `ConnectionIDLength` 固定為 20；加入 AmneziaWG v3.0 與 v3.1 支援。

## DNS、路由與匹配

- **2026-08-18｜DNS 啟動順序：** `ApplyConfig` 先初始化 DNS，再初始化 NTP，避免重載時序造成 DNS 尚未可用。
- **2026-08-18｜EDNS0 與 UDP 回應：** DNS 回應會回傳 EDNS0 OPT，並依客戶端宣告的大小截斷 UDP response；同時拆分內部 DNS server handler，保留 relay copy-back 的測試覆蓋。
- **2026-08-18｜錯誤與效能：** dns dialer 的 UDP 錯誤改用 adapter `Name()`；`DomainSet.Has` 避免不必要的字串反轉。

## TLS、JLS 與安全

- **2026-08-18｜TLS CVE：** 將 `metacubex/tls` 更新至 `v0.1.8`，對應 CVE-2026-56862 的修正。
- **2026-08-18｜JLS / ShadowQUIC：** JLS FakeRandom 拒絕保留的 TLS suffix；ShadowQUIC 在 QUIC 層強制 JLS authentication，並收緊 camouflage forwarding。
- **2026-07-29–08-16｜核心防護：** 更新器限制 request body、確認下載檔案留在更新目錄內，並改善 listener handle、shutdown flag 與測試中的 race safety。

## Aster 管理、穩定性與效能

- **2026-07-29｜管理功能：** Aster state store、listener 版本控制、使用者/訂閱管理、權限與備份鎖定機制成為可選的服務端管理層；只當客戶端使用時不需要啟用 `aster:`。
- **2026-08-15｜管理重載：** inbound 管理失敗時套用完整設定；失敗 patch 留下不可用 listener 時會重建；沒有儲存狀態的 listener 不會讓使用者列表崩潰。
- **2026-08-15｜統計與 API：** 每條連線批次累加使用者流量，避免 Aster API 重複昂貴工作，overview 也不再為了摘要複製整份使用者資料。
- **2026-08-15–18｜長時間運行：** 強化 listener close 的冪等性、原子狀態與 request size limit，並以真實 tracker、race test 和 transport lifecycle test 覆蓋關鍵路徑。

效能數字、完整相容性差異與不適用的上游歷史，請分別查看[效能優化與基準](/reference/performance)及[Aster 與 Mihomo 的差異](/reference/mihomo-differences)。

## CI、建置與文件

- **2026-07-29｜Aster 工程基礎：** 重整 Aster 的 Build/Test workflow、Docker、systemd 服務名稱與 release note 分組，並建立繁體中文 VitePress 文件站。
- **2026-07-29–30｜文件站：** 補上 AnyTLS + REALITY、架構、教學與客戶端優先定位，並發布至自訂網域。
- **2026-08-12–18｜驗證與工具鏈：** 補充 Kernel DIRECT、OpenWrt trade-off、流量治理與效能文件；加入 race/interop 測試、gci/gofumpt 整理，Build 改用 official Go 並移除不再需要的 Mihomo patched-toolchain matrix。
- **2026-08-18–19｜預發布流程：** 恢復 Build workflow，從 `v1.19.29` 產生 Aster prerelease notes；修正 artifact checkout、Pacman/apt 卡住與沒有 Hub credentials 時跳過 Docker 的流程。

這份頁面本身只記錄上述基準點之後的 Aster 變更；後續 `main` 更新時，請以 [GitHub compare](https://github.com/Miku0139oao/aster-core/compare/e26714a1..main) 與 [Prerelease-main](https://github.com/Miku0139oao/aster-core/releases/tag/Prerelease-main) 為準。
