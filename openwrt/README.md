# OpenWrt package

The `aster-core` package provides the virtual `mihomo` package and registers
`/usr/libexec/aster-core` as `/usr/bin/mihomo` through OpenWrt alternatives.
Nikki can therefore use Aster Core without changes to its init script or LuCI
application.

## Build

Use OpenWrt 24.10 or newer with the packages feed enabled:

```sh
cp -r openwrt/aster-core /path/to/openwrt/package/aster-core
cd /path/to/openwrt
./scripts/feeds update packages
./scripts/feeds install golang upx
make menuconfig
make package/aster-core/compile V=s
```

Select `Network -> aster-core` in `menuconfig`. The package uses the
`with_gvisor` build tag to match the TUN feature set expected by Nikki. It
strips and UPX-packs the binary to avoid exhausting small router overlays.

The in-tree recipe follows `main` and sets `PKG_MIRROR_HASH:=skip` for
development. Before publishing it in a feed, set `PKG_SOURCE_VERSION` to a
released tag or full commit ID, run the download step, and replace `skip` with
the generated source archive hash.

To package the current working tree without fetching GitHub, pass its absolute
Linux path to the SDK build:

```sh
make package/aster-core/compile V=s ASTER_CORE_LOCAL_SOURCE=/path/to/aster-core
```

## Install with Nikki

On Nikki releases that use a separate Mihomo package, remove that concrete
package, install Aster Core, and keep `nikki` and `luci-app-nikki` installed:

```sh
opkg remove mihomo-meta mihomo-alpha --force-depends
opkg install ./aster-core_*.ipk
opkg install nikki luci-app-nikki
```

`--force-depends` only bridges the package replacement: install Aster Core
immediately afterward so Nikki's virtual `mihomo` dependency is satisfied
again.

Newer Nikki packages may bundle their core and provide `mihomo` themselves.
Do not remove Nikki in that case. Install `aster-core` alongside it; Aster's
higher alternatives priority selects `/usr/libexec/aster-core`, and removing
`aster-core` restores Nikki's bundled core.

On apk-based OpenWrt snapshots, use the equivalent `apk del` and `apk add`
commands.

Verify the compatibility path and the active profile before enabling traffic
hijacking:

```sh
readlink -f /usr/bin/mihomo
/usr/bin/mihomo -v
/usr/bin/mihomo -d /etc/nikki/run -t
/etc/init.d/nikki restart
```

`readlink` should resolve to `/usr/libexec/aster-core`. Aster intentionally
keeps `Mihomo Meta` in `-v` output because Nikki's LuCI backend parses that
string.

## Compatibility notes

- Nikki redirect, TPROXY, TUN, DNS hijacking, controller API, and SIGHUP reload
  use interfaces retained from Mihomo.
- VLESS XHTTP is included in the normal `with_gvisor` build. It supports
  `auto`, `stream-one`, `stream-up`, and `packet-up`, HTTP/1.1, HTTP/2,
  HTTP/3, XMUX reuse, split download settings, REALITY, and VLESS Encryption.
- Proxy groups with `type: relay` are not supported. Replace them with
  `dialer-proxy` chains before testing a profile.
- Test redirect, TPROXY TCP/UDP, TUN, IPv4, IPv6, and DNS hijacking separately
  on the target router before production rollout.

Nikki subscriptions can use the regular Mihomo XHTTP form:

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
