---
title: Changelog
description: Aster Core changes since the Mihomo v1.19.29 baseline
---

# Changelog

> [!NOTE]
> This is the Aster Core rolling snapshot changelog, current through `HEAD` on `2026-08-19`. It covers only Aster-era changes after baseline commit `e26714a1` (Mihomo `v1.19.29`). Dates follow commit dates and do not represent release versions.

Aster Core does not have an official Aster `v*` release yet. GitHub's `Prerelease-main` is a continuously updated prerelease, so its contents may change with `main`.

- [View the full GitHub comparison for `e26714a1..main`](https://github.com/Miku0139oao/aster-core/compare/e26714a1..main)
- [Get the GitHub `Prerelease-main`](https://github.com/Miku0139oao/aster-core/releases/tag/Prerelease-main)
- [繁體中文版](/changelog)

For the full feature and compatibility overview, see [Aster vs. Mihomo](/en/reference/mihomo-differences). This page keeps the dated Aster highlights without reproducing the upstream Mihomo history.

## Core and Kernel DIRECT

- **2026-07-29 | Aster foundation:** Created Aster Core from the Mihomo baseline and positioned it as a client-first core, with optional Aster management, user, subscription, and traffic-accounting capabilities.
- **2026-08-12 | Kernel DIRECT and traffic governance:** Added a Linux/OpenWrt Kernel DIRECT path and traffic governance so safely identified `DIRECT` flows can stay on the kernel forwarding/NAT path. nftables plus flow offload is the recommended backend; the TC eBPF classifier remains disabled by default as an experimental option.
- **2026-08-17–18 | DIRECT safety boundaries:** Bounded the kernel-direct cache and added loop-safety tests and verification commands. TUN no longer drops hijacked SYNs and can learn `DIRECT` selected by a proxy group. See [OpenWrt and Nikki](/en/deployment/openwrt) for configuration, risks, and measurements.

## Protocols and connections

- **2026-07-29 | AnyTLS + REALITY:** Added AnyTLS client support and REALITY configuration for Aster inbound listeners, including uTLS fingerprints, `anytls://` imports, and share-link output for managed users.
- **2026-08-15–16 | Connection lifecycle:** Fixed close, bidirectional-state, and concurrent lifecycle issues across AnyTLS, VLESS, XTLS Vision, and the Shadowsocks fallback listener, making reloads and abnormal shutdowns more reliable.
- **2026-08-18 | AnyTLS metadata:** Added AnyTLS client metadata and stopped sending version information by default; empty metadata is covered by session tests.
- **2026-08-18 | XHTTP:** Changed the default `uplink-chunk-size` to `0`, accepted `uplinkHTTPMethod`, and retained the existing `uplinkHttpMethod` alias.
- **2026-08-18 | Sniffing:** Added H2C QUICv2 and multi-round sniffing, with fragmented TLS, QUIC, and HTTP/2 handshake assembly. Sniff failures keep the connection open while bounding reads.
- **2026-08-18 | Other transports:** Added a Hysteria2 handshake timeout and a RESTLS listener rate limit, fixed MASQUE QUIC `ConnectionIDLength` at 20, and added AmneziaWG v3.0 and v3.1 support.

## DNS, routing, and matching

- **2026-08-18 | DNS initialization:** `ApplyConfig` now initializes DNS before NTP, avoiding reload ordering issues where DNS was not ready.
- **2026-08-18 | EDNS0 and UDP responses:** DNS replies echo the EDNS0 OPT record and are truncated to the client-advertised size. The internal DNS server handler was split while preserving DNS relay copy-back coverage.
- **2026-08-18 | Errors and performance:** DNS dialer UDP errors now use the adapter `Name()`, and `DomainSet.Has` avoids unnecessary string reversal.

## TLS, JLS, and security

- **2026-08-18 | TLS CVE:** Updated `metacubex/tls` to `v0.1.8` for the CVE-2026-56862 fix.
- **2026-08-18 | JLS / ShadowQUIC:** JLS FakeRandom now rejects reserved TLS suffixes. ShadowQUIC enforces JLS authentication at the QUIC layer and hardens camouflage forwarding.
- **2026-07-29–08-16 | Core hardening:** Bounded updater request bodies, kept unpacked update files inside the update directory, and improved listener-handle, shutdown-flag, and race safety.

## Aster management, reliability, and performance

- **2026-07-29 | Management layer:** Added the optional Aster state store, listener versioning, user/subscription management, permissions, and backup/locking protections. Client-only deployments do not need `aster:` enabled.
- **2026-08-15 | Reload handling:** Applying inbound management failures now applies the full configuration; unusable listeners left by a failed patch are rebuilt; listeners without stored state no longer break user listings.
- **2026-08-15 | Statistics and API work:** Batched per-user traffic accounting per connection, removed repeated expensive work from Aster API requests, and stopped cloning all users for overview summaries.
- **2026-08-15–18 | Long-running stability:** Strengthened idempotent listener close, atomic state, and request-size limits, with real-tracker, race, and transport-lifecycle coverage for the critical paths.

See [Performance and benchmarks](/en/reference/performance) and [Aster vs. Mihomo](/en/reference/mihomo-differences) for detailed measurements and compatibility differences.

## CI, builds, and documentation

- **2026-07-29 | Aster engineering foundation:** Reworked the Aster Build/Test workflows, Docker and systemd service names, and release-note grouping, and created the Traditional Chinese VitePress documentation site.
- **2026-07-29–30 | Documentation site:** Added AnyTLS + REALITY, architecture, tutorials, and the client-first project positioning, then published the site at its custom domain.
- **2026-08-12–18 | Verification and toolchain:** Added Kernel DIRECT, OpenWrt trade-off, traffic-governance, and performance documentation; added race/interop coverage and gci/gofumpt cleanup; changed Build to official Go and removed the obsolete Mihomo patched-toolchain matrix.
- **2026-08-18–19 | Prerelease flow:** Restored the Build workflow and generated Aster prerelease notes from `v1.19.29`; fixed artifact checkout, Pacman/apt hangs, and the no-Docker path when Hub credentials are unavailable.

This page records only Aster changes after the baseline. For later `main` updates, use the [GitHub comparison](https://github.com/Miku0139oao/aster-core/compare/e26714a1..main) and [Prerelease-main](https://github.com/Miku0139oao/aster-core/releases/tag/Prerelease-main) as the source of truth.
