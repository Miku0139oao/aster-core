# 專案介紹

Aster Core 是以 Mihomo 為基礎的代理核心，主要運行在客戶端。它負責接收電腦或區域網路內的流量，依規則選擇節點，再送往遠端服務端。

```text
瀏覽器／App → Aster Core → 遠端節點 → 網際網路
```

遠端服務端不必使用 Aster。常見搭配包括 Xray、sing-box、SideraCore，以及支援相同協定的其他實作。只要協定和參數相容，兩端可以各自使用不同軟體。

## 主要功能

- VLESS、VMess、Trojan、AnyTLS、Hysteria 等代理協定
- HTTP、SOCKS5、透明代理與 TUN
- 規則分流、代理群組與節點健康檢查
- DNS、fake-IP、DoH、DoT 與 DoQ
- Clash-compatible API 與面板
- Mihomo proxy provider 與 rule provider

設定仍使用 Mihomo YAML，因此現有 profile 通常只需先備份，再用 `aster-core -t` 檢查即可。Aster 不直接讀取 Xray 或 sing-box JSON；從服務端取出節點位址、連接埠、密碼、SNI、金鑰和傳輸方式，填入對應的 `proxies` 項目即可。

## Aster 增加的內容

Aster 在 Mihomo 基線上加入 AnyTLS + REALITY 客戶端及分享連結支援，並修正設定重新載入、Hysteria UDP、VLESS 封包、XHTTP、DNS 回應與核心更新等問題。高連線數和大量使用者的管理路徑也做了額外優化。

詳細差異整理在[Aster 與 Mihomo](/reference/mihomo-differences)。

## 服務端功能

Aster 內建 VLESS 與 AnyTLS 入站，也能即時管理使用者和訂閱。這些功能適合測試、全 Aster 環境，或需要 Aster 管理介面的部署；一般節點服務端仍可使用 Xray、sing-box 或 SideraCore。

接下來可從[下載](/downloads)取得執行檔，再依[快速開始](/guide/getting-started)或[第一個代理設定](/tutorials/first-proxy)建立設定。
