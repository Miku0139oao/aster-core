---
title: Changelog
description: Aster Core changes since the Mihomo v1.19.29 baseline
---

# Changelog

> [!NOTE]
> This is the Aster Core rolling snapshot changelog, current through `2026-08-24`. It covers only Aster-era changes after baseline commit `e26714a1` (Mihomo `v1.19.29`). Dates follow commit/review dates and do not represent release versions. The Unreleased section is not a downloadable release until it lands on `main` and Prerelease-main is refreshed.

Aster Core does not have an official Aster `v*` release yet. GitHub's `Prerelease-main` is a continuously updated prerelease, so its contents may change with `main`.

- [View the full GitHub comparison for `e26714a1..main`](https://github.com/Miku0139oao/aster-core/compare/e26714a1..main)
- [Get the GitHub `Prerelease-main`](https://github.com/Miku0139oao/aster-core/releases/tag/Prerelease-main)
- [繁體中文版](/changelog)

For the full feature and compatibility overview, see [Aster vs. Mihomo](/en/reference/mihomo-differences). This page keeps the dated Aster highlights without reproducing the upstream Mihomo history.

## Unreleased | 2026-08-24 performance, memory, and reliability review

- **Kernel DIRECT hot path:** Skip full expiry scans until the next TTL, share one apply barrier per generation, and use a stack buffer for the common 1–4 observations. Existing-flow refresh fell from 15.286 µs / 64 B / 1 alloc to 270.8 ns / 0 alloc on the same host (**56.4×** median).
- **Rules and UDP mappings:** Restored binary lookup for finalized IP sets: a miss across 100k disjoint ranges fell from 2.385 ms to 114.7 ns. UDP associations no longer copy the complete reverse map for every destination; inserting 1,000 mappings fell from 41.31 ms / 46.37 MB to 486.3 µs / 542.6 KiB, with a 4,096-destination bound per association.
- **Low-memory bounds:** `bytes.Buffer` values above 128 KiB are not retained globally; `with_low_memory` defaults each DNS cache to 1,024 entries and negative sizes are rejected. TUIC fragment bags, Kernel DIRECT waiters, and eBPF prefix budgeting gained explicit bounds. Configured provider/rule-provider `size-limit` values now use a `limit+1` check and reject overflow instead of accepting truncated data.
- **DNS and routing safety:** Kernel DIRECT now trusts only one Answer-authorized CNAME/DNAME chain rooted at the query, accepts A/AAAA only for its terminal owner, and bounds addresses by alias TTL. Cycles, ambiguity, wrong class/family, truncated responses, and failed responses are rejected. Fake-IP flush, sniffer publication, and the second UDP fake-IP lookup also lost race/TOCTOU windows.
- **TUN and API:** PATCH snapshot→merge→validate→activate is serialized with reload. TUN activation runs before unrelated PATCH side effects; activation/rollback failures return 5xx; old-listener close errors are preserved. Equality/schema now cover loopback, IPv4, port exclusions, and ICMP-forwarding fields.
- **Protocols and lifecycle:** Fixed the deadline-wrapped sing-packet panic, a pool finalizer that blocked Go finalization, uninitialized XTLS Vision padding, REALITY + Vision unwrapping, first-buffer VLESS ownership, TUIC v5 wire sizing/fragment bounds, kcptun session shutdown, TrustTunnel health-loop shutdown, and OpenVPN `IV_VER` override. QUIC CRYPTO coverage now uses the bounded bitmap fix from Mihomo 1.19.30.
- **Traffic Control:** Fixed compound-duration overflow, default-store path disclosure, ancestor-symlink/existing-oversized-store checks, permanent low-rate UDP rejection, stacked-limiter token rollback, status slice aliasing, invalid granularity, and per-record report-key rebuilding. MAC-only policies are now rejected; `source-cidrs` is required.
- **Mihomo 1.19.30 benchmark:** Seven interleaved, fresh-process, single-core Windows rounds measured UDP metadata at 4.45×, disabled logging at 96.8×, AnyTLS 1 KiB at 2.29×, and 16 KiB at 1.32×. Both TCP relay ranges overlap; only zero allocation is claimed, not a significant desktop speedup. See [Performance and benchmarks](/en/reference/performance) for ranges, method, and the Windows working-set counter-example.
- **Residual-risk follow-up:** Kernel DIRECT classifiers stay serialized and quiescent through `Close`. UDP NAT keys now include an inbound namespace, TPROXY local sockets use a close-once promise, and the global NAT table has an admission cap. MRS/DomainSet reject hostile lengths and malformed tries; geosite AC/MPH no longer aliases unsupported bytes to `A`. Failed Traffic Control `Configure` keeps the previous runtime/store/portal, and live sessions rebind to the new generation. Mekya, XHTTP, TUIC pools, and rule-provider reloads gained session/queue/goroutine bounds; retired providers are closed.

### Compatibility notes

- Traffic Control may retain a MAC as device identity, but the core currently has no ingress MAC attribution. Every `devices` policy must provide `source-cidrs`; a MAC-only policy that could never match is no longer accepted.
- Kernel DIRECT dependency/capacity validation in `PATCH /configs` remains HTTP 400. Errors reached while constructing the TUN listener—device, permission, route/nftables, activation, or restoration—return HTTP 500.
- TC eBPF remains disabled by default and is not promoted to the recommended backend.

## Landed on main | follow-up reliability fixes after 2026-08-19

- **Controller compatibility:** Removed bulk `DELETE /connections`; per-ID `DELETE /connections/{id}` remains. Clients that relied on mass close must enumerate and close entries individually.
- **AnyTLS / REALITY:** Fixed frame alignment, wire lengths, authentication preamble handling, idle-pool state, sessions created after shutdown, v2 handshake synchronization, and REALITY close semantics through unwrap.
- **DNS and rules:** Preserved fake-IP clone offset/cycle and fixed AAAA fallback, Hosts alias cycles, IP4P misclassification, rcode request mutation, DIRECT case folding, wildcard validation, and single-line provider YAML.
- **Listeners and packet paths:** Redir/TPROXY UDP can retry independently; fixed TPROXY NAT waiters, ancillary TOS parsing, `RawConn.Control` errors, TUN route updates after close, and packets stranded during sender shutdown.
- **Controller and releases:** Hardened WebSocket interval/upgrade/streaming and empty-array schemas, bounded storage bodies, added revision validation, checksum self-checks, and repaired prerelease-note/build paths.

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
