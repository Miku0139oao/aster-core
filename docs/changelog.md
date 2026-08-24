---
title: 更新紀錄
description: Aster Core 自 Mihomo v1.19.29 基準點以來的繁體中文變更紀錄
---

# 更新紀錄

> [!NOTE]
> 本頁是 Aster Core 的 rolling snapshot changelog，整理至 `2026-08-24`。內容只涵蓋基準提交 `e26714a1`（Mihomo `v1.19.29`）之後的 Aster 時代變更；日期依提交／review 日期整理，不代表正式版本號。最上方的「未發布」項目在合併進 `main` 且 Prerelease-main 更新前，不應視為已下載版本。

Aster Core 目前尚未發布正式的 Aster `v*` 版本。GitHub 上的 `Prerelease-main` 是持續更新中的預發布版本，使用前請留意它可能隨 `main` 改變。

- [查看 `e26714a1..main` 的完整 GitHub 比較](https://github.com/Miku0139oao/aster-core/compare/e26714a1..main)
- [取得 GitHub `Prerelease-main`](https://github.com/Miku0139oao/aster-core/releases/tag/Prerelease-main)
- [English version](/en/changelog)

完整的功能差異與相容性說明，請看[Aster 與 Mihomo 的差異](/reference/mihomo-differences)；本頁只整理有日期的 Aster 變更重點。

## 未發布｜2026-08-24 效能、記憶體與可靠性 review wave

- **Kernel DIRECT hot path：** 未到下一筆 TTL 前跳過全表 expiry scan，共用同 generation 的 apply barrier，並讓常見 1–4 筆 observation 使用 stack buffer。既有 flow refresh 從 15.286 µs／64 B／1 alloc 降至 270.8 ns／0 alloc（同機中位數，約 **56.4×**）。
- **規則與 UDP mapping：** 合併後 100k 個不連續 CIDR 的 miss 恢復二分搜尋，從 2.385 ms 降至 114.7 ns；UDP association 不再每新增一個目的地就複製完整 reverse map，1,000 筆新增從 41.31 ms／46.37 MB 降至 486.3 µs／542.6 KiB，並加入每 association 4,096 筆上限。
- **低記憶體保護：** 大於 128 KiB 的 `bytes.Buffer` 不再回到全域 pool；`with_low_memory` 的每個 DNS cache 預設 1,024 筆，負值設定會被拒絕；TUIC fragment bag、Kernel DIRECT waiter 與 eBPF prefix budget 增加邊界。已設定的 provider／rule-provider `size-limit` 會以 `limit+1` 檢查並拒絕超限內容，不再把截斷資料當成功。
- **DNS 與路由安全：** Kernel DIRECT 只信任 Answer 內從原查詢出發的單一 CNAME／DNAME chain，只接受 terminal owner 的 A/AAAA，alias TTL 會限制地址 TTL，並拒絕 cycle、歧義、錯誤 class／family、截斷與非成功回應。Fake-IP flush、sniffer config publication 與 UDP 二次 fake-IP lookup 的 race／TOCTOU 也已修正。
- **TUN／API：** PATCH 的 TUN snapshot→merge→validate→activate 與 reload 共用鎖；TUN 先於其他 PATCH side effects 套用，activation/rollback 失敗回 5xx，舊 listener close error 不再被忽略。補齊 loopback、IPv4、port exclusion 與 ICMP forwarding 欄位的 equality/schema。
- **協定與生命週期：** 修正 deadline-wrapped sing packet panic、阻塞 Go finalizer 的 pool drain、XTLS Vision 未清零 padding、REALITY + Vision unwrap、VLESS first-buffer ownership、TUIC v5 wire size/fragment cap、kcptun session close、TrustTunnel health-loop close，以及 OpenVPN `IV_VER` override。QUIC CRYPTO coverage 改用有界 bitmap，對齊 Mihomo 1.19.30 的資源修正。
- **Traffic Control：** 修正 compound duration overflow、預設 store path 外洩、ancestor symlink／既有超大資料庫檢查、低速 UDP 永久拒絕、stacked limiter token rollback、status slice alias、無效 granularity 與每次 record 重建 report pair keys。MAC-only policy 目前明確拒絕，必須提供 `source-cidrs`。
- **Benchmark 對照：** Windows 5900X 單核、新程序、交錯 7 輪對 Mihomo 1.19.30：UDP metadata 4.45×、停用 log 96.8×、AnyTLS 1 KiB 2.29×、16 KiB 1.32×；兩項 TCP relay 範圍重疊，只確認 0 allocation，不宣稱顯著桌面加速。完整方法、範圍與 Windows working-set 反例見[效能優化與基準](/reference/performance)。
- **Residual-risk 收尾：** Kernel DIRECT classifier 在 Close 前維持序列化與 quiescent；UDP NAT 以 inbound namespace 分桶，TPROXY local socket 改用 close-once promise，並加上全域 flow 上限。MRS／DomainSet 會拒絕惡意長度與畸形 trie；geosite AC/MPH 不再把未支援位元組當成 `A`。Traffic Control 失敗的 Configure 會保留舊 runtime／store／portal，活躍 session 會重新綁定新 generation。Mekya、XHTTP、TUIC pool 與 rule-provider reload 加上 session／queue／goroutine 邊界，退役 provider 會被關閉。

### 相容性注意

- Traffic Control 的 MAC 欄位可保留作裝置識別，但核心目前沒有 ingress MAC attribution；`devices` 必須提供 `source-cidrs`，不再接受實際上永遠不會命中的 MAC-only policy。
- `PATCH /configs` 的 Kernel DIRECT dependency／容量驗證錯誤維持 400；進入 TUN listener 建立後的裝置、權限、route/nftables、activation 或 restore 錯誤回 500。
- TC eBPF 仍預設關閉；這輪沒有把實驗 backend 改成推薦路徑。

## 已合併 main｜2026-08-19 後續可靠性修正

- **Controller 相容性：** 移除一次關閉全部連線的 `DELETE /connections`；單筆 `DELETE /connections/{id}` 保留。舊客戶端若依賴 mass close，必須改為列出後逐筆關閉。
- **AnyTLS／REALITY：** 修正 frame alignment、wire length、authentication preamble、idle pool、shutdown 後 session、v2 handshake 同步，以及 REALITY unwrap 後的 close semantics。
- **DNS／規則：** 保留 fake-IP clone offset/cycle，補上 AAAA fallback、Hosts alias cycle、IP4P 誤判、rcode request mutation、DIRECT case folding、wildcard 驗證與單行 provider YAML。
- **Listener／封包路徑：** redir/TPROXY UDP 可獨立重試；修正 TPROXY NAT waiter、ancillary TOS、`RawConn.Control` error、TUN close 後 route update 與 packet-sender close 後殘留封包。
- **Controller／release：** 強化 WebSocket interval/upgrade/streaming 與 empty-array schema，限制 storage body，加入 revision validation、release checksum self-check 與 prerelease note/build 修正。

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
