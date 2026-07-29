---
layout: home

hero:
  name: Aster Core
  text: AnyTLS + REALITY，建立在 Mihomo 之上
  tagline: 從零實戰教學、Mihomo 問題修正、效能優化、協定設定、使用者管理與正式部署的繁體中文文件。
  image:
    src: /logo.png
    alt: Aster Core
  actions:
    - theme: brand
      text: 實戰教學
      link: /tutorials/
    - theme: alt
      text: 完整變更與優化
      link: /reference/aster-changes
    - theme: alt
      text: AnyTLS + REALITY
      link: /tutorials/anytls-reality
    - theme: alt
      text: 設定參考
      link: /reference/configuration

features:
  - icon: ✓
    title: 從零實戰
    details: 六篇可照做的教學，包含完整 YAML、指令、預期結果、驗證、回退與症狀導向排錯。
    link: /tutorials/
  - icon: ⚡
    title: 問題修正與效能
    details: 逐項列出相對 Mihomo 最新基線的 listener、Hysteria、VLESS、XHTTP、DNS、updater 修正與 benchmark。
    link: /reference/aster-changes
  - icon: ↗
    title: Mihomo 相容
    details: 沿用 Mihomo YAML、規則、providers、DNS、TUN 與 Clash-compatible Controller API。
    link: /guide/introduction
  - icon: ◎
    title: Aster 管理平面
    details: 對 VLESS 與 AnyTLS 提供即時使用者管理、revision、流量、持久化與訂閱。
    link: /aster/overview
  - icon: ◈
    title: AnyTLS + REALITY
    details: Aster 新增 AnyTLS 用戶端／伺服器雙向 REALITY、分享連結匯入與受管訂閱輸出。
    link: /reference/anytls-reality
  - icon: ◫
    title: 完整參考
    details: 依入站、出站、群組、規則、DNS、CLI 與環境變數分頁整理。
    link: /reference/configuration
  - icon: ⬡
    title: 多平台部署
    details: 涵蓋 Docker、Linux 套件、systemd、Nix、OpenWrt 與 Nikki。
    link: /deployment/docker
---

## 先了解這個專案

Aster Core 是 [MetaCubeX/mihomo](https://github.com/MetaCubeX/mihomo) 的修改分支，而不是重新實作所有代理協定。Mihomo 提供成熟的資料平面、設定格式、規則、DNS、TUN 與協定支援；Aster 在此基礎上新增 AnyTLS + REALITY 用戶端／伺服器，並加入面向伺服器營運者的使用者管理與安全持久化能力。

| 需求 | 建議入口 |
| --- | --- |
| 第一次建立可用代理 | [第一個代理設定實戰](/tutorials/first-proxy) |
| 從零部署 AnyTLS + REALITY | [AnyTLS + REALITY 實戰](/tutorials/anytls-reality) |
| 管理使用者與訂閱 | [受管使用者實戰](/tutorials/user-management) |
| 在 VPS 正式上線 | [Linux 正式部署](/tutorials/linux-production) |
| 依症狀排查問題 | [故障排查手冊](/tutorials/troubleshooting) |
| 查看修正與效能證據 | [完整變更與優化](/reference/aster-changes) |
| 從 Mihomo 遷移 | [Aster 與 Mihomo 差異](/reference/mihomo-differences) |
| 查 YAML 欄位 | [設定總覽](/reference/configuration) |
| 建立 VLESS/AnyTLS 管理服務 | [Aster 管理功能](/aster/overview) |
| 串接後台或面板 | [Admin API](/aster/api) |
| 上正式環境前安全檢查 | [安全與持久化](/aster/security) |
| 容器或路由器部署 | [部署文件](/deployment/docker) |

::: warning 文件範圍
本文件描述 Aster Core repository 目前的實作。Mihomo 上游選項非常多，完整 YAML 範例仍保留在 [`config.yaml`](/config.yaml)；敘述式頁面著重常用欄位、Aster 變動、限制與容易踩雷的行為。
:::

## 教學和參考現在分開

- **想完成一個任務**：從[實戰教學](/tutorials/)開始，跟著前置條件、完整設定、操作、驗證與排錯走。
- **想查一個欄位**：使用[設定總覽](/reference/configuration)、[入站](/reference/inbounds)、[出站](/reference/outbounds)及[規則與 DNS](/reference/routing-dns)。
- **想知道 Aster 到底改了什麼**：閱讀[完整變更、問題修正與效能優化](/reference/aster-changes)，其中包含基線 SHA、實作機制、測試名稱與 benchmark。

## 版本基線

- Aster module：`github.com/Miku0139oao/aster-core`
- Mihomo 基線：`v1.19.29`
- 上游 commit：`e26714a181ac0e2fa803453c0a8e9a9ce94e31cb`
- 最低 Go：1.20
- 授權：GPL-3.0

來源與同步政策請參閱 repository 的 [NOTICE.md](https://github.com/Miku0139oao/aster-core/blob/main/NOTICE.md) 與 [UPSTREAM.md](https://github.com/Miku0139oao/aster-core/blob/main/UPSTREAM.md)。
