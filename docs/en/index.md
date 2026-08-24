---
layout: home

hero:
  name: Aster Core
  text: A client-first proxy core built on Mihomo
  tagline: Compatible with Mihomo configuration and Clash dashboards, plus AnyTLS + REALITY, upstream fixes, and performance work.
  image:
    src: /logo.png
    alt: Aster Core
  actions:
    - theme: brand
      text: Quick start
      link: /en/tutorials/first-proxy
    - theme: alt
      text: AnyTLS + REALITY
      link: /en/tutorials/anytls-reality
    - theme: alt
      text: How Aster differs from Mihomo
      link: /en/reference/mihomo-differences
    - theme: alt
      text: Configuration
      link: /en/reference/configuration

features:
  - icon: ↗
    title: Mihomo compatible
    details: Keeps Mihomo YAML, rules, DNS, TUN, proxy groups, and the Clash-compatible API.
    link: /en/guide/introduction
  - icon: ◈
    title: AnyTLS + REALITY
    details: Client connections and anytls:// share-link import.
    link: /en/tutorials/anytls-reality
  - icon: ⚡
    title: Leaner, faster core forwarding
    details: "Same-host OpenWrt tests vs Mihomo 1.19.30: TCP core forwarding about 2% faster, AnyTLS framing about 1.4–2.0× faster."
    link: /en/reference/performance
  - icon: ⇄
    title: OpenWrt Kernel DIRECT
    details: Safe DIRECT stays in the Linux kernel. Prefer nftables + flow offload; TC eBPF is an experimental layer that is off by default.
    link: /en/deployment/openwrt
  - icon: ◎
    title: Client first
    details: Built for desktops, routers, and gateways. Remote servers can be Xray, sing-box, or SideraCore.
    link: /en/tutorials/first-proxy
  - icon: ⬡
    title: Multi-platform
    details: Linux, Windows, macOS, Android, FreeBSD, Docker, and OpenWrt builds.
    link: /en/deployment/docker
---

## About Aster Core

Aster Core is a client-oriented fork of [MetaCubeX/mihomo](https://github.com/MetaCubeX/mihomo). It usually runs on a computer, router, or gateway and connects to nodes provided by Xray, sing-box, SideraCore, or another compatible implementation.

The project keeps Mihomo configuration, rules, DNS, TUN, and the Clash-compatible controller, then adds AnyTLS + REALITY and fixes upstream issues around connections, reloads, and updates. Aster can also host VLESS / AnyTLS listeners and manage users, but that path is optional.

## How much faster than Mihomo?

In plain terms: Aster removes core work that used to be redone for every packet. The medians below come from three identical rounds on a real OpenWrt soft router:

| Workload you actually hit | How much faster | What that means |
| --- | ---: | --- |
| TCP core forwarding | **about 2% faster** | Less wasted work while moving data, and no per-relay allocation |
| AnyTLS framing | **about 1.4–2.0× faster** | Lower packing cost on high-speed transfers or many small packets |
| New UDP packet setup | **about 5.4× faster** | Games, voice, and QUIC create less throwaway memory |
| Disabled debug logs | **about 101× faster** | Unused logs are skipped before the string is built |

Memory needs separate claims. These hot paths remove per-operation heap allocations, but the complete core with a minimal idle profile measured about 39.3 MiB for Aster and 34.7 MiB for Mihomo on OpenWrt, so Aster was about **4.6 MiB** larger. The five-run Windows validation on 2026-08-24 likewise put Aster's working set about 0.93 MiB higher. Aster does not claim universally lower idle RAM; the benefit is less temporary garbage on busy paths plus explicit cache, mapping, and pool bounds.

### What changed?

- **Reuse buffers:** TCP, UDP, and AnyTLS recycle scratch space instead of allocating for every packet.
- **Fewer global locks:** rules and proxy lists publish a snapshot, so connections do not queue on one lock.
- **Less repeated UDP work:** addresses stay in a comparable form, and socket timers are refreshed in batches.
- **AnyTLS is precomputed:** padding rules are parsed once, then frames are packed directly.
- **Disabled logs do no work:** messages that will not be shown are skipped before formatting.
- **Cheaper traffic stats:** upload and download counters increment in place instead of scanning every connection.

> [!IMPORTANT]
> This does not mean Speedtest becomes 5.4× faster. That figure is one small core step: preparing UDP metadata. The closest whole-path number is the TCP test, about 2%. The Ryzen 7 5825U soft router is still stronger than many home routers, so do not copy these figures to another device. The single-core 25% CPU-quota / 512 MiB run only shows that the gains persisted under throttling on the same x86 VM; it does not simulate ARM, MIPS, cache, or memory bandwidth. The homepage keeps the conservative unrestricted result.

> [!WARNING]
> “Closer to dae” is not the same as faster. On one real OpenWrt router, the experimental TC eBPF classifier dropped same-server speedtest from about 1,647 Mbps to 692 Mbps. The recommended setup is still Kernel DIRECT with the nftables backend and OpenWrt flow offload. See [OpenWrt and Nikki](/en/deployment/openwrt).

Full numbers, three-round ranges, and the test method are in [Performance and benchmarks](/en/reference/performance).

## Start here

- First time: [Build your first client profile](/en/tutorials/first-proxy)
- You already have an AnyTLS + REALITY node: [AnyTLS + REALITY setup](/en/tutorials/anytls-reality)
- Split traffic and DNS: [Routing and DNS](/en/tutorials/routing-dns)
- Migrating from Mihomo: [How Aster differs from Mihomo](/en/reference/mihomo-differences)
- Core overhead: [Performance and benchmarks](/en/reference/performance)
- Connection problems: [Troubleshooting](/en/tutorials/troubleshooting)

Field-level details live in [Configuration](/en/reference/configuration). Deployment notes are on the [Docker](/en/deployment/docker), [Linux](/en/deployment/linux), and [OpenWrt / Nikki](/en/deployment/openwrt) pages.
