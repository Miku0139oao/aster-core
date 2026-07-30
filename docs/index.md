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
      text: AnyTLS + REALITY
      link: /tutorials/anytls-reality
    - theme: alt
      text: 與 Mihomo 的差異
      link: /reference/mihomo-differences
    - theme: alt
      text: 設定參考
      link: /reference/configuration

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
    title: 修正與優化
    details: 改善重新載入、斷線重連、UDP、DNS、核心更新與高負載下的資源使用。
    link: /reference/mihomo-differences
  - icon: ◎
    title: 客戶端優先
    details: 適合桌面、路由器與閘道，服務端可搭配 Xray、sing-box 或 SideraCore。
    link: /tutorials/first-proxy
  - icon: ⬡
    title: 多平台
    details: 提供 Linux、Windows、macOS、Android、FreeBSD、Docker 與 OpenWrt 建置。
    link: /deployment/docker
---

## 關於 Aster Core

Aster Core 是 [MetaCubeX/mihomo](https://github.com/MetaCubeX/mihomo) 的衍生專案，主要用於客戶端。它通常裝在電腦、路由器或閘道上，連接由 Xray、sing-box、SideraCore 或其他相容實作提供的節點。

專案保留 Mihomo 的設定格式、規則、DNS、TUN 與 Clash 相容控制介面，另外加入 AnyTLS + REALITY，並修正上游仍存在的連線、重新載入及更新問題。Aster 也能提供 VLESS／AnyTLS 入站與使用者管理，但這部分屬於選用功能。

## 從這裡開始

- 第一次使用：[建立第一份客戶端設定](/tutorials/first-proxy)
- 已有 AnyTLS + REALITY 節點：[AnyTLS + REALITY 設定](/tutorials/anytls-reality)
- 設定分流與 DNS：[路由與 DNS](/tutorials/routing-dns)
- 從 Mihomo 遷移：[Aster 與 Mihomo 的差異](/reference/mihomo-differences)
- 遇到連線問題：[故障排查](/tutorials/troubleshooting)

欄位說明集中在[設定參考](/reference/configuration)，部署方式則整理在 [Docker](/deployment/docker)、[Linux](/deployment/linux) 與 [OpenWrt／Nikki](/deployment/openwrt) 頁面。
