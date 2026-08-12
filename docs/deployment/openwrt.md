# OpenWrt 與 Nikki

## 整合方式

Aster OpenWrt package：

- Package name：`aster-core`
- 提供 virtual package：`mihomo`
- Binary：`/usr/libexec/aster-core`
- Compatibility alternative：`/usr/bin/mihomo`
- Alternatives priority：400

因此 Nikki 不需修改 init script 或 LuCI code。

## 需求

- OpenWrt 24.10 或更新版本。
- Packages feed。
- `golang/host`
- `upx/host`
- Target dependencies：CA bundle、`ip-full`、`kmod-inet-diag`、`kmod-tun`

## Build

```sh
cp -r openwrt/aster-core /path/to/openwrt/package/aster-core
cd /path/to/openwrt
./scripts/feeds update packages
./scripts/feeds install golang upx
make menuconfig
make package/aster-core/compile V=s
```

在 menuconfig 選擇：

```text
Network -> aster-core
```

Recipe 使用 `with_gvisor`，並 strip + UPX 以降低 router overlay 使用量。

## 使用本機 source

```sh
make package/aster-core/compile V=s \
  ASTER_CORE_LOCAL_SOURCE=/absolute/linux/path/to/aster-core
```

必須是 OpenWrt build environment 可讀取的絕對 Linux path。

## Feed 發布前

Repository recipe 的 `PKG_SOURCE_VERSION:=main` 與 `PKG_MIRROR_HASH:=skip` 只適合開發。

正式 feed：

1. 將 `PKG_SOURCE_VERSION` 固定到 release tag 或完整 commit。
2. 執行 download。
3. 取得 source archive hash。
4. 用實際 hash 取代 `skip`。
5. 更新 package version/release。

## 與既有 Mihomo package 切換

舊型 Nikki：

```sh
opkg remove mihomo-meta mihomo-alpha --force-depends
opkg install ./aster-core_*.ipk
opkg install nikki luci-app-nikki
```

`--force-depends` 只用於短暫移除 concrete provider；必須立即安裝 Aster，恢復 virtual `mihomo` dependency。

新型 Nikki 可能把 core 打包在自身 package，並同時提供 `mihomo`。此時不要刪除 Nikki；安裝 Aster 後由較高 alternatives priority 選擇 Aster，移除 Aster 則恢復 Nikki bundled core。

APK-based snapshot 請使用對應 `apk add/del`。

## 驗證

```sh
readlink -f /usr/bin/mihomo
/usr/bin/mihomo -v
/usr/bin/mihomo -d /etc/nikki/run -t
/etc/init.d/nikki restart
```

預期：

```text
/usr/libexec/aster-core
Mihomo Meta ...
```

`Mihomo Meta` 是刻意保留的相容字串，Nikki LuCI backend 會解析它。

## 類 dae 的 Kernel DIRECT（推薦）

Aster 可以像 dae 一樣，讓判定安全的 DIRECT 連線留在 Linux kernel forwarding/NAT path；代理流量仍由 TUN 處理。推薦組合是 **Kernel DIRECT + nftables + OpenWrt flow offload**。TC eBPF 是另一層實驗性 classifier，不是啟用 Kernel DIRECT 的必要條件，也不保證更快。

在 Nikki 的 TUN mixin 開啟 Kernel DIRECT，關閉 Nikki 自己的 transparent proxy，讓 Aster 單獨管理 TUN route/auto-redirect：

```sh
uci set nikki.proxy.enabled='0'
uci set nikki.mixin.dns_mode='redir-host'
uci set nikki.mixin.tun_kernel_direct='1'
uci set nikki.mixin.tun_kernel_direct_ebpf='0'
uci set nikki.mixin.tun_kernel_direct_ebpf_proxy='0'
uci set nikki.mixin.tun_kernel_direct_ebpf_proxy_redirect='0'
uci commit nikki
/etc/init.d/nikki restart
```

確認 nftables learned exclude set 與 controller status：

```sh
nft -a list chain inet mihomo prerouting
curl -H "Authorization: Bearer $SECRET" \
  http://192.168.1.1:9090/api/aster/kernel-direct/status
```

status 顯示 `backend: nftables` 是推薦且正常的工作狀態，不代表 eBPF 載入失敗。此模式沒有 TC filter，較容易保留 OpenWrt software/hardware flow offload 的收益。

所有使用 Kernel DIRECT 的 client DNS 都必須經過 Aster；未被觀察的 DoH/DoT 或舊 DNS cache 無法提供網域判定，尤其共享 CDN IP 不能保證符合 domain rule。建議使用 redir-host/mapping DNS，或把希望 bypass 的網域放進 `fake-ip-filter`。規則、proxy、mode 或 provider 更新時 learned set 會先清空，再由新 DNS 回應保守重建。

## 實驗性 TC eBPF classifier

TC eBPF 可在 LAN ingress 以 IPv4/IPv6 LPM 和 5-tuple LRU 預先分類 DIRECT／PROXY，更接近 dae 的封包掛鉤方式；但它是逐封包路徑，而且 ingress TC 可能阻止或繞過 OpenWrt flow offload。在一台實際 OpenWrt 路由器、同一 Speedtest server 的測試中，TC 開啟只有約 **692 Mbps**，卸載後為 **1,647 Mbps**，持久關閉並重啟後仍為 **1,644 Mbps**。這是該硬體的實測反例，不應推廣成所有 eBPF 都較慢。

只有在同一伺服器做過 A/B 後才考慮啟用：

```sh
uci set nikki.mixin.tun_kernel_direct_ebpf='1'
uci -q delete nikki.mixin.tun_kernel_direct_ebpf_interfaces
uci add_list nikki.mixin.tun_kernel_direct_ebpf_interfaces='br-lan'
uci set nikki.mixin.tun_kernel_direct_ebpf_proxy='1'
uci set nikki.mixin.tun_kernel_direct_ebpf_proxy_redirect='1'
uci commit nikki
/etc/init.d/nikki restart
```

status 的 `fast_paths[].interfaces` 是 bridge 解析後實際掛載的 ports。`packets`／`bytes` 是 DIRECT 計數；PROXY 另見 `proxy-packets`／`proxy-bytes`。要快速回到推薦模式：

```sh
uci set nikki.mixin.tun_kernel_direct_ebpf='0'
uci set nikki.mixin.tun_kernel_direct_ebpf_proxy='0'
uci set nikki.mixin.tun_kernel_direct_ebpf_proxy_redirect='0'
uci commit nikki
/etc/init.d/nikki restart
```

緊急 A/B 可對 status 列出的每個 interface 執行 `tc filter del dev <interface> ingress`；這只適合暫時測試，重啟 Nikki 會依設定重新掛載。若 kernel 沒有 BPF/TC classifier 能力，預設自動保留 nftables Kernel DIRECT；不要開啟 `tun_kernel_direct_ebpf_required`，除非你確實希望 TC 失敗時阻止 TUN 啟動。

## split-WAN IPv6 節點

有些 OpenWrt WAN6 只有 `default from <delegated-prefix> via <gateway>`，而代理 core 建立的 IPv6 socket 尚未選定 source，route lookup 可能掉回 TUN，或 auto-detect 誤選 IPv4 WAN device。症狀通常是路由器 IPv6 正常，但 IPv6 literal／IPv6-only 節點 timeout。

Nikki 的「Split-WAN IPv6 Outbound Fix」預設關閉。只在上述拓撲啟用，它會從選定 WAN6 取得實際 device、補一條 generic IPv6 default route，並把 IPv6-only proxy endpoint 綁到該 device：

```sh
uci set nikki.mixin.ipv6_outbound_fix='1'
uci set nikki.mixin.ipv6_outbound_interface='wan6'
uci set nikki.mixin.ipv6_outbound_route_metric='512'
uci commit nikki
/etc/init.d/nikki restart
```

先用 `ubus call network.interface.wan6 status`、`ip -6 route show table main default` 和節點 delay test 驗證。multi-WAN／自訂 policy routing 不應直接套用；請先確認新增 generic default 不會越過既有策略。

## Profile 相容

保留：

- Redirect
- TProxy
- TUN
- DNS hijack
- Controller API
- SIGHUP reload
- Mihomo VLESS XHTTP 格式

不支援：

- `type: relay` proxy group

改用 `dialer-proxy` chain。

## Kernel DIRECT 設定原理

OpenWrt 24.10+ 使用 firewall4/nftables 時，可以讓 Aster 規則判定安全的 `DIRECT` 目的位址保留在 kernel forwarding path：

```yaml
tun:
  enable: true
  auto-route: true
  auto-redirect: true
  kernel-direct: true
  dns-hijack:
    - any:53
```

不要設定 `DISABLE_NFTABLES=1`。建議啟用前清除 client DNS cache，先停用 flow offload 驗證分流、DNS、TCP、UDP、IPv4/IPv6 與 reload，再重新啟用 software/hardware flow offload 並確認 DIRECT throughput。`nft list table inet mihomo` 可檢查 `inet4_route_exclude_address_set`／`inet6_route_exclude_address_set`。繞過的 DIRECT 流量不會出現在 Aster connection／traffic 統計中。

## XHTTP 範例

```yaml
proxies:
  - name: vless-xhttp
    type: vless
    server: proxy.example.com
    port: 443
    uuid: 00000000-0000-0000-0000-000000000000
    udp: true
    tls: true
    servername: proxy.example.com
    client-fingerprint: chrome
    network: xhttp
    xhttp-opts:
      path: /xhttp
      mode: auto
```

## Router 測試清單

- [ ] Compatibility path 正確。
- [ ] Profile `-t` 通過。
- [ ] Redirect TCP。
- [ ] TProxy TCP。
- [ ] TProxy UDP。
- [ ] TUN。
- [ ] DNS hijack。
- [ ] `kernel-direct` 的 DIRECT 不出現在 Aster connections，代理網域仍正常進入 Aster。
- [ ] 規則 reload 後 learned DIRECT set 先清空再重新學習。
- [ ] IPv4。
- [ ] IPv6。
- [ ] LuCI Controller。
- [ ] Restart/reload 後 rules/providers 正常。
- [ ] Overlay 空間足夠。

更完整的 package-level 說明仍可參閱 repository 的 [`openwrt/README.md`](https://github.com/Miku0139oao/aster-core/blob/main/openwrt/README.md)。
