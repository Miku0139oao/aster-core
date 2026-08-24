# OpenWrt and Nikki

## How the integration works

The Aster OpenWrt package:

- Package name: `aster-core`
- Provides virtual package: `mihomo`
- Binary: `/usr/libexec/aster-core`
- Compatibility alternative: `/usr/bin/mihomo`
- Alternatives priority: 400

Nikki therefore does not need init-script or LuCI code changes.

## Requirements

- OpenWrt 24.10 or newer.
- Packages feed.
- `golang/host`
- `upx/host`
- Target dependencies: CA bundle, `ip-full`, `kmod-inet-diag`, `kmod-tun`, `kmod-sched-bpf`

## Build

```sh
cp -r openwrt/aster-core /path/to/openwrt/package/aster-core
cd /path/to/openwrt
./scripts/feeds update packages
./scripts/feeds install golang upx
make menuconfig
make package/aster-core/compile V=s
```

In menuconfig choose:

```text
Network -> aster-core
```

The recipe uses `with_gvisor`, then strip + UPX to reduce router overlay usage.

## Use a local source tree

```sh
make package/aster-core/compile V=s \
  ASTER_CORE_LOCAL_SOURCE=/absolute/linux/path/to/aster-core
```

It must be an absolute Linux path the OpenWrt build environment can read.

## Before publishing a feed

The repository recipe’s `PKG_SOURCE_VERSION:=main` and `PKG_MIRROR_HASH:=skip` are only for development.

For a production feed:

1. Pin `PKG_SOURCE_VERSION` to a release tag or full commit.
2. Run the download.
3. Get the source archive hash.
4. Replace `skip` with the real hash.
5. Update the package version/release.

## Switching from an existing Mihomo package

Older Nikki:

```sh
opkg remove mihomo-meta mihomo-alpha --force-depends
opkg install ./aster-core_*.ipk
opkg install nikki luci-app-nikki
```

`--force-depends` is only for briefly removing a concrete provider. You must install Aster immediately so the virtual `mihomo` dependency is restored.

Newer Nikki may ship the core inside its own package and also provide `mihomo`. In that case do not delete Nikki. After Aster is installed, the higher alternatives priority selects Aster. Removing Aster restores Nikki’s bundled core.

APK-based snapshots should use the matching `apk add/del`.

## Verify

```sh
readlink -f /usr/bin/mihomo
/usr/bin/mihomo -v
/usr/bin/mihomo -d /etc/nikki/run -t
/etc/init.d/nikki restart
```

Expected:

```text
/usr/libexec/aster-core
Mihomo Meta ...
```

`Mihomo Meta` is an intentional compatibility string. Nikki’s LuCI backend parses it.

## dae-like Kernel DIRECT (recommended)

Aster can keep safely classified DIRECT connections on the Linux kernel forwarding/NAT path, like dae. Proxied traffic still goes through TUN. The recommended combination is **Kernel DIRECT + nftables + OpenWrt flow offload**. TC eBPF is a separate experimental classifier. It is not required to enable Kernel DIRECT, and it does not guarantee more speed.

The following `tun_kernel_direct*` UCI keys work only in a custom Nikki build with the matching mixin. The public stock Nikki package and `openwrt/aster-core` recipe do not create or consume these keys. With stock Nikki, inject the `tun.kernel-direct` YAML through a supported profile/mixin path and verify the generated `/etc/nikki/run/config.yaml`. Only a custom build should use the commands below to disable Nikki's own transparent proxy and let Aster manage TUN route/auto-redirect:

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

On dual-WAN / macvlan / mwan3 keep `tun.auto-detect-interface: false`. When it is `true`, `FindInterfaceName` returns `<invalid>`, and delay tests plus nodes that are not bound to an interface fail. Nikki’s `mixin.uc` must not write it back to `true` when kernel-direct is on. `nikki.init` should also force `false` again after mixin, or the next restart can break delay tests.

Confirm the nftables learned exclude set and controller status:

```sh
nft -a list chain inet mihomo prerouting
curl -H "Authorization: Bearer $SECRET" \
  http://192.168.1.1:9090/api/aster/kernel-direct/status
```

`backend: nftables` in status is the recommended and normal working state. It does not mean eBPF failed to load. This mode has no TC filter, so it is easier to keep OpenWrt software/hardware flow offload. The same response also includes `learned_sets` (with `max_entries` and `evictions`), `process`, and `aster_traffic`. `proxy_traffic` is a deprecated alias equal to `aster_traffic` (all traffic Aster handled, not proxy-only; DIRECT bypassed by the kernel is not counted). `evictions` only counts address-LRU drops from the capacity cap, not TTL, flush, or collapse.

Every client that uses Kernel DIRECT must send DNS through Aster. Unobserved DoH/DoT or stale DNS cache cannot provide a domain decision, especially on shared CDN IPs that may not match a domain rule. Live flows Aster already classified as `DIRECT` / `Compatible` can also teach later connections a pure-IP destination, **including the case where a selector / URLTest / fallback currently picks DIRECT** (for example `漏網之魚` → `DIRECT`). Looking only at the outer group type would leave those destinations in TUN forever. fake-IP, private, loopback, link-local, and other non-global addresses are not learned. Prefer redir-host/mapping DNS, or put domains you want bypassed into `fake-ip-filter`. When rules, proxies, mode, or providers update, the learned set is cleared first, then rebuilt conservatively from new DNS or live flows.

## Loop protection (do not drop inbound SYNs)

`auto-redirect` can send Aster’s own outgoing DIRECT SYN back into REDIR / TUN. At that point only one copy of the packet remains:

- **Do not** return immediately in `handleTCPConn` just because a REDIR / TUN SYN came from a local source. That blackholes DIRECT and nodes that are not bound to an interface (every delay test fails, web pages do not open).
- Loop protection lives in three places: `DIRECT.CheckConn` rejects only **registered outbound AddrPorts** (connMap); `ObserveFlow` writes safely classified DIRECT destinations into the nftables exclude before dial; a five-second scan reaps transparent-path (REDIR / TPROXY / TUN) TCP trackers that remain at zero bytes for 30 seconds.
- The reaper **only closes transparent-proxy TCP**. Ordinary HTTP / SOCKS / VLESS server-first connections, UDP (including ePDG / Wi‑Fi calling `500` / `4500`), and connections with traffic are left alone. Bulk `DELETE /connections` has been removed; do not replace normal idle cleanup with controller mass-close.

For games or port-level traffic that must stay on one WAN, a mark itself cannot stop auto-redirect. Either learn the destination into the exclude set, or do identity DNAT / `exclude-dst-port` before Aster’s `dstnat + 1` so the packet never enters TUN.

## Experimental TC eBPF classifier

TC eBPF can pre-classify DIRECT / PROXY on LAN ingress with IPv4/IPv6 LPM and a 5-tuple LRU, closer to dae’s packet hook. It is a per-packet path, and ingress TC can block or bypass OpenWrt flow offload. On one real OpenWrt router and the same Speedtest server, TC on was about **692 Mbps**, after unload **1,647 Mbps**, and after a persistent-off restart still **1,644 Mbps**. That is a measured counter-example on that hardware. Do not generalize it to “all eBPF is slower.”

Consider enabling it only after a same-server A/B:

```sh
uci set nikki.mixin.tun_kernel_direct_ebpf='1'
uci -q delete nikki.mixin.tun_kernel_direct_ebpf_interfaces
uci add_list nikki.mixin.tun_kernel_direct_ebpf_interfaces='br-lan'
uci set nikki.mixin.tun_kernel_direct_ebpf_proxy='1'
uci set nikki.mixin.tun_kernel_direct_ebpf_proxy_redirect='1'
uci commit nikki
/etc/init.d/nikki restart
```

Status `fast_paths[].interfaces` are the ports actually attached after bridge resolution. `packets` / `bytes` are DIRECT counters; PROXY is in `proxy-packets` / `proxy-bytes`. To return quickly to the recommended mode:

```sh
uci set nikki.mixin.tun_kernel_direct_ebpf='0'
uci set nikki.mixin.tun_kernel_direct_ebpf_proxy='0'
uci set nikki.mixin.tun_kernel_direct_ebpf_proxy_redirect='0'
uci commit nikki
/etc/init.d/nikki restart
```

For an emergency A/B you can run `tc filter del dev <interface> ingress` on each interface listed in status. That is only a temporary test; restarting Nikki remounts according to the configuration. If the kernel has no BPF/TC classifier, nftables Kernel DIRECT is kept by default. Do not turn on `tun_kernel_direct_ebpf_required` unless you really want a TC failure to prevent TUN from starting.

## Split-WAN IPv6 nodes

Some OpenWrt WAN6 setups only have `default from <delegated-prefix> via <gateway>`. An IPv6 socket created by the proxy core has not chosen a source yet, so route lookup can fall back to TUN, or auto-detect can pick the IPv4 WAN device. The usual symptom is that router IPv6 works, but IPv6 literals / IPv6-only nodes time out.

“Split-WAN IPv6 Outbound Fix” is also a custom Nikki extension that this repository does not ship; stock Nikki does not consume the UCI keys below. Enable it only when that extension is installed and this topology is confirmed. It takes the real device from the selected WAN6, adds a generic IPv6 default route, and binds IPv6-only proxy endpoints to that device:

```sh
uci set nikki.mixin.ipv6_outbound_fix='1'
uci set nikki.mixin.ipv6_outbound_interface='wan6'
uci set nikki.mixin.ipv6_outbound_route_metric='512'
uci commit nikki
/etc/init.d/nikki restart
```

Verify first with `ubus call network.interface.wan6 status`, `ip -6 route show table main default`, and a node delay test. Do not apply this directly on multi-WAN / custom policy routing. Confirm the new generic default will not override existing policy.

## Profile compatibility

Kept:

- Redirect
- TProxy
- TUN
- DNS hijack
- Controller API
- SIGHUP reload
- Mihomo VLESS XHTTP format

Not supported:

- `type: relay` proxy group

Use a `dialer-proxy` chain instead.

## How Kernel DIRECT is configured

On OpenWrt 24.10+ with firewall4/nftables, Aster can keep destinations its rules safely classify as `DIRECT` on the kernel forwarding path:

```yaml
tun:
  enable: true
  auto-route: true
  auto-redirect: true
  auto-detect-interface: false
  kernel-direct: true
  kernel-direct-max-entries: 4096
  dns-hijack:
    - any:53
```

`kernel-direct-max-entries` is the learned address-set capacity, default 4096, maximum 65536. `0` means the default.

Do not set `DISABLE_NFTABLES=1`. Before enabling it, clear client DNS cache, disable flow offload first to verify routing, DNS, TCP, UDP, IPv4/IPv6, and reload, then re-enable software/hardware flow offload and confirm DIRECT throughput. `nft list table inet mihomo` can inspect `inet4_route_exclude_address_set` / `inet6_route_exclude_address_set`. Bypassed DIRECT traffic does not appear in Aster connection / traffic statistics.

## XHTTP example

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

## Router test list

- [ ] Compatibility path is correct.
- [ ] Profile `-t` passes.
- [ ] Redirect TCP.
- [ ] TProxy TCP.
- [ ] TProxy UDP.
- [ ] TUN.
- [ ] DNS hijack.
- [ ] `kernel-direct` DIRECT does not appear in Aster connections, and proxied domains still enter Aster.
- [ ] After a rule reload, the learned DIRECT set is cleared and then relearned.
- [ ] IPv4.
- [ ] IPv6.
- [ ] LuCI Controller.
- [ ] Rules/providers work after restart/reload.
- [ ] Overlay space is enough.

More complete package-level notes are still in the repository [`openwrt/README.md`](https://github.com/Miku0139oao/aster-core/blob/main/openwrt/README.md).
