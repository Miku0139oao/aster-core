# Configuration overview

## Minimal skeleton

```yaml
mixed-port: 7890
allow-lan: false
mode: rule
log-level: info

dns:
  enable: true
  enhanced-mode: fake-ip
  nameserver:
    - system

proxies: []
proxy-groups: []
rules:
  - MATCH,DIRECT
```

Full annotated example: [`config.yaml`](/config.yaml).

## General

| Field | Purpose |
| --- | --- |
| `port` | HTTP proxy port |
| `socks-port` | SOCKS5 port |
| `mixed-port` | Shared HTTP/SOCKS port |
| `redir-port` | Redir transparent-proxy port |
| `tproxy-port` | Linux TProxy TCP/UDP port |
| `allow-lan` | Whether non-loopback clients are allowed |
| `bind-address` | LAN listener bind address |
| `mode` | `rule`, `global`, or `direct` |
| `log-level` | `silent`, `error`, `warning`, `info`, `debug` |
| `ipv6` | Enable IPv6 resolution/routing |
| `interface-name` | Default outbound interface |
| `routing-mark` | Linux socket mark |
| `find-process-mode` | `always`, `strict`, `off` |
| `tcp-concurrent` | Dial TCP in parallel to resolved IPs |
| `unified-delay` | Unify delay-test calculation |

### Mode

- `rule`: match `rules` in order.
- `global`: send all traffic to the `GLOBAL` group.
- `direct`: send all traffic directly.

Rule mode falls back to `DIRECT` when nothing matches. You should still add an explicit:

```yaml
rules:
  - MATCH,FINAL
```

and make sure `FINAL` is an existing proxy/group name.

## Controller

```yaml
external-controller: 127.0.0.1:9090
external-controller-tls: 127.0.0.1:9443
external-controller-unix: mihomo.sock
external-controller-pipe: \\.\pipe\mihomo
secret: "replace-with-a-strong-secret"
```

| Field | Meaning |
| --- | --- |
| `external-controller` | HTTP Controller |
| `external-controller-tls` | HTTPS Controller, using `tls` certificates |
| `external-controller-unix` | Unix socket; newer Windows may also support it |
| `external-controller-pipe` | Windows named pipe |
| `external-controller-routing-mark` | Linux listener routing mark |
| `external-controller-cors` | Origin and private-network CORS |
| `external-ui` | UI directory |
| `external-ui-url` | UI archive URL |
| `external-doh-server` | DoH path on the Controller |

A non-empty `secret` protects the ordinary Controller API. Static `/ui`, Aster subscriptions, and the optional DoH route are not in the same authentication group.

## TLS

Controller TLS and some shared certificate settings use:

```yaml
tls:
  certificate: ./server.crt
  private-key: ./server.key
  client-auth-type: ""
  client-auth-cert: ""
```

A certificate can be PEM content or a file inside the safe path. Enabling mTLS verification requires a client CA/cert.

## Proxies and groups

```yaml
proxies:
  - name: edge
    type: vless
    server: proxy.example.com
    port: 443
    uuid: 00000000-0000-0000-0000-000000000000
    tls: true
    servername: proxy.example.com

proxy-groups:
  - name: PROXY
    type: select
    proxies:
      - edge
      - DIRECT
```

Detailed fields are in [Outbounds and groups](/en/reference/outbounds).

## Providers

```yaml
proxy-providers:
  remote:
    type: http
    url: https://example.com/subscription.yaml
    path: ./providers/remote.yaml
    interval: 3600
    health-check:
      enable: true
      url: https://www.gstatic.com/generate_204
      interval: 300
```

Provider paths are also subject to the safe-path check. Treat remote content as untrusted configuration: limit the source and use HTTPS.

## Listeners

`listeners` are extra server inbounds:

```yaml
listeners:
  - name: local-socks
    type: socks
    listen: 127.0.0.1
    port: 1080
```

Each listener must have a unique `name`. Details are in [Inbounds](/en/reference/inbounds).

## Aster

```yaml
aster:
  secret: "replace-with-at-least-32-random-bytes"
  public-base-url: https://proxy.example.com
  store: aster-state.json
  managed-listeners:
    - edge-vless
```

The presence of an `aster` block enables the Aster manager. The full description is in [Aster management overview](/en/aster/overview).

## TUN

```yaml
tun:
  enable: true
  stack: gvisor
  auto-route: true
  auto-redirect: true
  kernel-direct: true
  kernel-direct-max-entries: 4096
  # Experimental; keep disabled until a same-server A/B test proves a benefit.
  kernel-direct-ebpf: false
  kernel-direct-ebpf-interfaces:
    - br-lan
  kernel-direct-ebpf-mark: 1073741824 # 0x40000000
  kernel-direct-ebpf-max-entries: 65536
  kernel-direct-ebpf-proxy: false
  kernel-direct-ebpf-proxy-redirect: false
  kernel-direct-ebpf-proxy-mark: 536870912 # 0x20000000
  kernel-direct-ebpf-flow-entries: 65536
  kernel-direct-ebpf-direct-prefixes: []
  kernel-direct-ebpf-proxy-prefixes: []
  auto-detect-interface: false # dual-WAN / macvlan / mwan3 must be false; true can make FindInterfaceName return <invalid>
  dns-hijack:
    - any:53
```

Release builds already include `with_gvisor`. TUN routes, auto-redirect, UID/package include/exclude, and platform differences are large. Validate a minimal profile on the target platform first.

`kernel-direct` is a Linux/OpenWrt kernel-forwarding mode. Aster observes real A/AAAA answers it handled, and can learn pure-IP destinations from live flows already classified as `DIRECT` / `Compatible` (it unwraps selector / URLTest / fallback, so `漏網之魚` → `DIRECT` is learned too). It classifies conservatively with the current routing rules and puts addresses that can be decided as `DIRECT` from destination domain/IP alone into the nftables auto-redirect exclude set. Later new connections stay on the Linux forwarding/NAT path and no longer create an Aster `DIRECT` socket. It requires `auto-route: true`, `auto-redirect: true`, usable nftables, and client DNS going through Aster. Any proxy classification on a shared IP is proxy-wins. Rules that cannot be expressed as a destination IP (source/process/inbound/port and similar) are not bypassed. Updating rules, proxies, mode, or providers immediately clears the learned set until new DNS or live flows relearn it. fake-IP, private, loopback, link-local, and other non-global addresses are never learned. `backend: nftables` is the recommended normal state for this feature, not a downgrade error.

Do not drop locally sourced REDIR / TUN SYNs in userspace to “prevent loops”: `auto-redirect` already took the only copy of the packet, and a direct return blackholes DIRECT plus nodes that are not bound to an interface. Loop protection is `DIRECT.CheckConn` (reject only registered outbound AddrPorts), `ObserveFlow`, and the 30-second zero-byte TCP reaper (it skips UDP, including `500` / `4500`).

fake-IP answers are not added to the kernel set. For the full benefit, prefer redir-host/mapping DNS, or put domains you want kernel-direct for into `fake-ip-filter`. Bypassed traffic does not appear in Aster connection/traffic statistics.

`kernel-direct-ebpf` is an experimental TC classifier that is off by default. It runs on ingress of the listed Linux interfaces for every packet. If the listed interface is a Linux bridge (usually `br-lan` on OpenWrt), Aster automatically resolves and attaches to the current bridge ports (for example `eth0`/WLAN). IPv4 and IPv6 use an LPM trie, so `/0`, prefixes, and DNS-learned `/32` / `/128` can be decided by longest prefix. The same prefix is still proxy-wins. It can prevent OpenWrt software/hardware flow offload from working. Do not assume higher throughput just because eBPF is in use.

With `kernel-direct-ebpf-proxy` on, safe global addresses first get a conservative PROXY `/0` fallback, then confirmed DIRECT prefixes override with a longer prefix. Each TCP / UDP / ICMP flow’s family, protocol, source/destination address, and port is stored in an LRU 5-tuple cache. A TC DIRECT hit returns to Linux forwarding via nft mark-return. Without `kernel-direct-ebpf-proxy-redirect`, PROXY still uses the compatible nftables TCP redirect / UDP-ICMP mark shim.

With `kernel-direct-ebpf-proxy-redirect` on, IPv4 and IPv6 PROXY TCP / UDP / ICMP packets are `bpf_redirect()`’d by TC eBPF straight into the current Aster TUN. The two PROXY nftables shims are not created. sing-tun’s original auto-redirect rules stay complete: during a generation update, DNS, fragment/extension headers, unclassifiable packets, or after TC is fail-open unloaded, traffic still takes the original path. Aster also puts every local IPv4 `/32` and IPv6 `/128` address present at startup into the highest-priority bypass so return traffic or packets destined for the router itself are not sent back into TUN by PROXY `/0`.

Map updates first publish an odd generation so the classifier is temporarily fail-open, then sync IPv4/IPv6 LPM, then atomically enable with an even generation. Old flow-cache entries become invalid immediately because the generation no longer matches. TCP/UDP 53 is not marked, so client DNS still goes through sing-tun DNS hijack first. Private, loopback, link-local, multicast, TUN fake-IP, IPv4 fragments, IPv6 extension headers, load/sync failures, non-Ethernet/single-layer VLAN packets, and the router’s own output all use the original nftables fallback. Any map-sync error leaves an odd generation, unloads the TC filter, then shuts the eBPF backend down.

- `kernel-direct-ebpf-required: true`: refuse to start TUN if TC, a BPF map, or an nft mark rule fails to create. Default is `false`; a failure is logged as a warning and automatically falls back to nftables.
- `kernel-direct-ebpf-interfaces`: required. OpenWrt is usually `br-lan`. List every LAN/guest bridge. Status API `requested-interfaces` is the configured value; `interfaces` are the actually attached bridge ports.
- `kernel-direct-ebpf-mark`: default `0x40000000`. Aster matches a bit mask and does not overwrite other mark bits.
- `kernel-direct-max-entries`: learned address-set capacity, default 4096, maximum 65536. `0` means the default. YAML parse and `PATCH /configs` both write `0` as 4096; values above the cap are rejected (PATCH returns 400).
- `kernel-direct-ebpf-max-entries`: combined IPv4/IPv6 LPM prefix safety cap, default 65536.
- `kernel-direct-ebpf-proxy`: enable bidirectional DIRECT/PROXY steering and a safe PROXY `/0` fallback. Off by default.
- `kernel-direct-ebpf-proxy-redirect`: send PROXY decisions from TC directly into the current Aster TUN and remove the PROXY nftables shim. Requires `kernel-direct-ebpf-proxy`. Off by default.
- `kernel-direct-ebpf-proxy-mark`: PROXY classifier bit, default `0x20000000`. Must not overlap DIRECT or auto-redirect marks.
- `kernel-direct-ebpf-flow-entries`: 5-tuple LRU capacity, default 65536.
- `kernel-direct-ebpf-direct-prefixes` / `kernel-direct-ebpf-proxy-prefixes`: optional static CIDRs. Longest prefix wins; PROXY wins on the same prefix.

Status is available from `GET /api/aster/kernel-direct/status`. When TC is not enabled the backend is the recommended `nftables`. Compatible mark mode is `ebpf-tc-lpm-lru`. When TC redirects straight into TUN it is `ebpf-tc-lpm-lru-redirect`. `packets` / `bytes` are the DIRECT counters. PROXY uses separate `proxy-packets` / `proxy-bytes`. Status also returns the redirect interface, direct/proxy/bypass prefixes, flow hits, LRU capacity, and the last sync error.

The same response also includes `learned_sets`, `process`, `aster_traffic`, and the compatibility field `proxy_traffic`:

- `learned_sets`: snapshot per kernel-direct consumer, including `max_entries`, `max_records` (domain budget, usually `max_entries × 4`), `learned_addresses`, `direct_addresses`, `proxy_addresses`, `learned_domains`, `evictions`.
- `process`: controller process `pid` and `started_at` (Unix seconds).
- `aster_traffic`: estimate of all traffic Aster currently handles, including TUN DIRECT / default-tun fallback, excluding DIRECT already bypassed by kernel-direct.
- `proxy_traffic`: deprecated alias with the same content as `aster_traffic`. It is not “proxy-only traffic.” `GET /api/aster/capabilities` lists this field under `kernel_direct.deprecated_fields`.
- `learned_sets[].evictions`: address-LRU evictions since process start because of the capacity cap. TTL expiry, rule-reload flush, or set collapse are not counted.

Real-hardware throughput must be A/B’d against the same server. One OpenWrt router was about 692 Mbps with TC eBPF on, about 1,647 Mbps after unload, and about 1,644 Mbps after a persistent-off restart. The cause is per-packet TC work interacting with flow offload, not “eBPF is inherently slower.” See [OpenWrt and Nikki](/en/deployment/openwrt) and [Performance and benchmarks](/en/reference/performance).

## Profile and cache

The profile cache can store the selected proxy, fake-IP, and provider subscription information. The default cache file is `cache.db` in the home directory.

Do not confuse Aster state with the profile cache:

- `cache.db`: Mihomo runtime/profile cache.
- `aster-state.json`: Aster managed users, traffic, revision, and subscriptions.
