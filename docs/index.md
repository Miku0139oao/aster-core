---
layout: home

hero:
  name: Aster Core
  text: 客戶端優先，建立在 Mihomo 之上
  tagline: 連接 Xray、sing-box、SideraCore 節點，並提供 AnyTLS + REALITY、Mihomo 問題修正、效能優化、DNS、規則分流與 TUN。
  image:
    src: /logo.png
    alt: Aster Core
  actions:
    - theme: brand
      text: 第一次使用
      link: /tutorials/first-proxy
    - theme: alt
      text: Aster 改了什麼
      link: /reference/mihomo-differences
    - theme: alt
      text: AnyTLS + REALITY
      link: /tutorials/anytls-reality
    - theme: alt
      text: 設定參考
      link: /reference/configuration

features:
  - icon: ✓
    title: 照著做就能用
    details: 從下載、填入節點資料到測試連線，每一步都有可以直接複製的設定與指令。
    link: /tutorials/
  - icon: ⚡
    title: 更穩、更省資源
    details: 改善重新載入、斷線重連、UDP、DNS 與更新問題，也減少不必要的資料複製和掃描。
    link: /reference/mihomo-differences
  - icon: ↗
    title: Mihomo 相容
    details: 繼續使用 Mihomo 設定、規則、DNS、TUN，以及相容 Clash 的控制面板。
    link: /guide/introduction
  - icon: ◎
    title: 客戶端優先
    details: 安裝在電腦或路由器上，連接 Xray、sing-box、SideraCore 或服務商提供的節點。
    link: /tutorials/first-proxy
  - icon: ◈
    title: AnyTLS + REALITY
    details: 填入服務端提供的網址、密碼和金鑰即可連線，也能匯入相容的分享連結。
    link: /reference/anytls-reality
  - icon: ◫
    title: 設定查詢
    details: 不知道某個設定要填什麼時，可以依連線、節點、規則和 DNS 分類查找。
    link: /reference/configuration
  - icon: ⬡
    title: 多平台使用
    details: 支援 Linux、Windows、macOS、Android、FreeBSD、Docker、OpenWrt 與 Nikki。
    link: /deployment/docker
---

## 先了解這個專案

Aster Core 主要安裝在你的電腦、路由器或閘道上。它連接 Xray、sing-box、SideraCore 或服務商提供的遠端節點，再按照你的規則決定流量怎麼走。

它以 Mihomo 為基礎，所以可以繼續使用 Mihomo 設定和 Clash 相容面板。Aster 另外加入 AnyTLS + REALITY，並改善重新載入、斷線重連、UDP、DNS、更新和資源使用問題。Aster 也有可選的服務端功能，但一般使用者不需要啟用。

| 需求 | 建議入口 |
| --- | --- |
| 第一次建立可用客戶端 | [第一個代理設定實戰](/tutorials/first-proxy) |
| 連接 AnyTLS + REALITY 節點 | [AnyTLS + REALITY 客戶端實戰](/tutorials/anytls-reality) |
| 設定分流、DNS 與 TUN | [路由與 DNS 實戰](/tutorials/routing-dns) |
| 依症狀排查問題 | [故障排查手冊](/tutorials/troubleshooting) |
| 看 Aster 改善了什麼 | [Aster 跟 Mihomo 有什麼不同](/reference/mihomo-differences) |
| 從 Mihomo 換過來 | [Aster 跟 Mihomo 有什麼不同](/reference/mihomo-differences) |
| 查 YAML 欄位 | [設定總覽](/reference/configuration) |
| 容器或路由器部署 | [部署文件](/deployment/docker) |
| 可選：用 Aster 架設測試節點 | [AnyTLS + REALITY 進階路線](/tutorials/anytls-reality#可選服務端路線) |
| 可選：管理 VLESS/AnyTLS 帳號 | [使用者管理教學](/tutorials/user-management) |
| 可選：在 VPS 長期執行 Aster | [Linux 正式部署](/tutorials/linux-production) |

::: tip 看不懂術語也沒關係
先從[第一個代理設定](/tutorials/first-proxy)開始。遇到不懂的欄位再查參考頁，不需要先讀完全部文件。
:::

## 教學和參考現在分開

- **想直接用起來**：從[實戰教學](/tutorials/)開始，照著範例填入自己的節點資料。
- **不知道某個欄位是什麼**：使用[設定總覽](/reference/configuration)、[接收連線](/reference/inbounds)、[遠端節點](/reference/outbounds)及[規則與 DNS](/reference/routing-dns)。
- **想知道為什麼選 Aster**：閱讀[Aster 跟 Mihomo 有什麼不同](/reference/mihomo-differences)。

## 版本基線

- Aster module：`github.com/Miku0139oao/aster-core`
- Mihomo 基線：`v1.19.29`
- 上游 commit：`e26714a181ac0e2fa803453c0a8e9a9ce94e31cb`
- 最低 Go：1.20
- 授權：GPL-3.0

來源與同步政策請參閱 repository 的 [NOTICE.md](https://github.com/Miku0139oao/aster-core/blob/main/NOTICE.md) 與 [UPSTREAM.md](https://github.com/Miku0139oao/aster-core/blob/main/UPSTREAM.md)。
