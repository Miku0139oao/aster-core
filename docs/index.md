---
layout: home

hero:
  name: Aster Core
  text: AnyTLS + REALITY，建立在 Mihomo 之上
  tagline: 從協定設定到即時使用者管理、訂閱、API、安全與部署的繁體中文參考文件。
  image:
    src: /logo.png
    alt: Aster Core
  actions:
    - theme: brand
      text: 快速開始
      link: /guide/getting-started
    - theme: alt
      text: AnyTLS + REALITY
      link: /reference/anytls-reality
    - theme: alt
      text: Aster 與 Mihomo 差異
      link: /reference/mihomo-differences
    - theme: alt
      text: 設定參考
      link: /reference/configuration

features:
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
| 第一次執行 | [快速開始](/guide/getting-started) |
| 部署 AnyTLS + REALITY | [AnyTLS + REALITY](/reference/anytls-reality) |
| 從 Mihomo 遷移 | [Aster 與 Mihomo 差異](/reference/mihomo-differences) |
| 查 YAML 欄位 | [設定總覽](/reference/configuration) |
| 建立 VLESS/AnyTLS 管理服務 | [Aster 管理功能](/aster/overview) |
| 串接後台或面板 | [Admin API](/aster/api) |
| 上正式環境前安全檢查 | [安全與持久化](/aster/security) |
| 容器或路由器部署 | [部署文件](/deployment/docker) |

::: warning 文件範圍
本文件描述 Aster Core repository 目前的實作。Mihomo 上游選項非常多，完整 YAML 範例仍保留在 [`config.yaml`](/config.yaml)；敘述式頁面著重常用欄位、Aster 變動、限制與容易踩雷的行為。
:::

## 版本基線

- Aster module：`github.com/Miku0139oao/aster-core`
- Mihomo 基線：`v1.19.29`
- 上游 commit：`e26714a181ac0e2fa803453c0a8e9a9ce94e31cb`
- 最低 Go：1.20
- 授權：GPL-3.0

來源與同步政策請參閱 repository 的 [NOTICE.md](https://github.com/Miku0139oao/aster-core/blob/main/NOTICE.md) 與 [UPSTREAM.md](https://github.com/Miku0139oao/aster-core/blob/main/UPSTREAM.md)。
