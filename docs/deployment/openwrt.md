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
- [ ] IPv4。
- [ ] IPv6。
- [ ] LuCI Controller。
- [ ] Restart/reload 後 rules/providers 正常。
- [ ] Overlay 空間足夠。

更完整的 package-level 說明仍可參閱 repository 的 [`openwrt/README.md`](https://github.com/Miku0139oao/aster-core/blob/main/openwrt/README.md)。
