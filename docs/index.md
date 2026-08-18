---
layout: home

hero:
  name: Aster Core
  text: 以 Mihomo 為基礎的客戶端代理核心
  tagline: 相容 Mihomo 設定與 Clash 面板，加入 AnyTLS + REALITY、上游問題修正及效能改善。
  image:
    src: /logo.png
    alt: Aster Core
  actions:
    - theme: brand
      text: 快速開始
      link: /tutorials/first-proxy
    - theme: alt
      text: 下載
      link: /downloads
    - theme: alt
      text: AnyTLS + REALITY
      link: /tutorials/anytls-reality
    - theme: alt
      text: 與 Mihomo 的差異
      link: /reference/mihomo-differences
    - theme: alt
      text: 設定參考
      link: /reference/configuration
    - theme: alt
      text: 更新紀錄
      link: /changelog

features:
  - icon: ↗
    title: Mihomo 相容
    details: 沿用 Mihomo 的 YAML、規則、DNS、TUN、代理群組與 Clash-compatible API。
    link: /guide/introduction
  - icon: ◈
    title: AnyTLS + REALITY
    details: 支援客戶端連線及 anytls:// 分享連結匯入。
    link: /tutorials/anytls-reality
  - icon: ⚡
    title: 核心轉送更省、更快
    details: 實際 OpenWrt 主機同機測試：TCP 核心轉送快約 3%，AnyTLS 封包處理快約 1.3–1.9 倍。
    link: /reference/performance
  - icon: ⇄
    title: OpenWrt Kernel DIRECT
    details: 安全的 DIRECT 留在 Linux kernel；推薦 nftables + flow offload，TC eBPF 為預設關閉的實驗層。
    link: /deployment/openwrt
  - icon: ◎
    title: 客戶端優先
    details: 適合桌面、路由器與閘道，服務端可搭配 Xray、sing-box 或 SideraCore。
    link: /tutorials/first-proxy
  - icon: ⬡
    title: 多平台
    details: 提供 Linux、Windows、macOS、Android、FreeBSD、Docker 與 OpenWrt 建置。
    link: /downloads
---

## 關於 Aster Core

Aster Core 是 [MetaCubeX/mihomo](https://github.com/MetaCubeX/mihomo) 的衍生專案，主要用於客戶端。它通常裝在電腦、路由器或閘道上，連接由 Xray、sing-box、SideraCore 或其他相容實作提供的節點。

專案保留 Mihomo 的設定格式、規則、DNS、TUN 與 Clash 相容控制介面，另外加入 AnyTLS + REALITY，並修正上游仍存在的連線、重新載入及更新問題。Aster 也能提供 VLESS／AnyTLS 入站與使用者管理，但這部分屬於選用功能。

## 比 Mihomo 快多少？

先講人話：Aster 把代理核心裡「每個封包都要重做一次」的雜工砍掉了。以下是在實際 OpenWrt 軟路由上，相同測試各跑三輪的中位數：

| 你會遇到的工作 | Aster 快多少 | 代表什麼 |
| --- | ---: | --- |
| TCP 核心轉送 | **約快 3%** | 搬資料時少做一些無用工作，並消除每次轉送的記憶體配置 |
| AnyTLS 封包打包 | **快約 1.3–1.9 倍** | 高速傳輸或很多小封包時，打包成本更低 |
| UDP 新封包準備 | **快約 5.1 倍** | 遊戲、語音、QUIC 等大量 UDP 封包較少製造垃圾記憶體 |
| 被關掉的 debug log | **快約 97 倍** | 不需要的 log 不再先做好再丟掉 |

在單核 25% CPU 時間、512 MiB RAM 的受限環境中，五個核心案例的獨立測試程序峰值記憶體比 Mihomo 少約 **30–49%**；但完整核心以最小設定空載時，Aster 約 31.6 MiB、Mihomo 約 28.9 MiB，Aster 反而多約 **2.8 MiB**。也就是忙碌時少製造臨時記憶體，空載底座則因功能較多而稍大。

### 改了什麼？

- **記憶體重複用：** TCP、UDP 和 AnyTLS 用過的暫存空間收回再用，不再每個封包都重新申請。
- **少搶同一把鎖：** 規則與代理清單改成讀取現成快照，多條連線不用排隊等全域鎖。
- **UDP 少做重複工作：** 不再反覆把位址轉成字串，也不再每個封包都重設 socket 計時器。
- **AnyTLS 先算好：** padding 規則只解析一次，傳資料時直接打包。
- **關掉的 log 真的不做：** 確定不會顯示的 log，在組字串前就跳過。
- **流量統計更輕：** 上下載數字直接累加，不為每次更新掃描所有連線。

> [!IMPORTANT]
> 這不代表 Speedtest 會快 5.1 倍。5.1 倍是「準備一個 UDP 封包」這個核心小步驟；最接近整體資料轉送的 TCP 測試約快 3%。測試用的 Ryzen 7 5825U 軟路由仍比許多家用路由器強，低階裝置的絕對速度會更低，不能直接套用這些數字。另有以單核 25% CPU 時間、512 MiB 記憶體模擬弱硬體的測試，五項優化仍全部有效。整體趨勢是硬體越弱，省掉的 CPU 工作與記憶體配置越有感；首頁仍保留較保守的未限速結果。

> [!WARNING]
> 「更接近 dae」不等於一定更快。實驗性 TC eBPF classifier 在一台實際 OpenWrt 路由器把同 server 測速由約 1,647 Mbps 降到 692 Mbps；推薦設定仍是 Kernel DIRECT 的 nftables backend 搭配 OpenWrt flow offload。詳見 [OpenWrt 與 Nikki](/deployment/openwrt)。

想看完整數字、三輪範圍與測試方法，可查看[效能優化與基準](/reference/performance)。

## 從這裡開始

- 取得二進位檔：[下載](/downloads)
- 第一次使用：[建立第一份客戶端設定](/tutorials/first-proxy)
- 已有 AnyTLS + REALITY 節點：[AnyTLS + REALITY 設定](/tutorials/anytls-reality)
- 設定分流與 DNS：[路由與 DNS](/tutorials/routing-dns)
- 從 Mihomo 遷移：[Aster 與 Mihomo 的差異](/reference/mihomo-differences)
- 想查看 Aster 最近變更：[更新紀錄](/changelog)（[English](/en/changelog)）
- 想了解核心開銷：[效能優化與基準](/reference/performance)
- 遇到連線問題：[故障排查](/tutorials/troubleshooting)

欄位說明集中在[設定參考](/reference/configuration)，部署方式則整理在 [Docker](/deployment/docker)、[Linux](/deployment/linux) 與 [OpenWrt／Nikki](/deployment/openwrt) 頁面。
