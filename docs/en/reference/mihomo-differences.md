# How Aster differs from Mihomo

Aster Core is based on Mihomo `v1.19.29`. Configuration format, the rule engine, DNS, TUN, proxy groups, and the Clash-compatible API all come from Mihomo. Aster’s own work is concentrated on AnyTLS + REALITY, bug fixes, runtime efficiency, and optional server management.

## AnyTLS + REALITY

For AnyTLS, Aster adds:

- Client `reality-opts` and uTLS fingerprint
- `anytls://` REALITY share-link import
- `reality-config` on Aster inbounds
- Share-link export for managed users

The client only needs the node address, port, password, SNI, public key, and short ID from the server. A complete example is in [AnyTLS + REALITY](/en/tutorials/anytls-reality).

## Upstream fixes

Aster fixes several classes of problems that still exist on the current Mihomo baseline:

| Area | What was fixed |
| --- | --- |
| Configuration reload | Avoid leftover ports, partial updates after a later failure, and connections left behind on shutdown |
| Hysteria UDP | Fragment interleaving, reconnect state, message IDs, and port-hopping sockets |
| VLESS | Packet read/write desync, oversized frames, and concurrent read/write issues |
| XHTTP | Shutdown hangs, double close, and stale session cleanup |
| DNS | Some compressed responses were not written back into the original buffer |
| Core updates | Check HTTP status, version, file size, and checksum, and block unexpected downgrades |
| Controller | Better reload, debug validation, and local socket permissions |

These fixes matter most on long-running hosts, frequent configuration updates, UDP, or unstable networks.

## Performance work

- TCP relay reuses buffers; UDP adapters and metadata use object pools
- UDP NAT uses typed keys, immutable mapping snapshots, and batched deadline refresh
- Rules and proxy lists are published as atomic snapshots, so packet matching does not hold a global configuration read lock
- AnyTLS pre-parses the padding scheme and assembles data/control frames directly
- Disabled debug logs return before formatting and creating an event
- Users and subscription tokens are looked up by index; traffic uses atomic increments and batched writes
- Updating one listener copies only the related data; Aster state uses leaner JSON

In the latest microbenchmarks, steady-state UDP, 32 KiB TCP relay, and common-size AnyTLS uploads reach zero allocations per operation. Full numbers, test environment, and how to rerun are in [Performance and benchmarks](/en/reference/performance). Real differences still vary with configuration, connection count, protocol, OS, and hardware.

## Linux / OpenWrt Kernel DIRECT

Aster can conservatively learn DIRECT destinations from real A/AAAA answers that went through its own DNS, and from live flows already classified as `DIRECT` / `Compatible` (including the current unwrapped selector). Those addresses go into the nftables auto-redirect exclude set so later new connections stay on the Linux kernel forwarding/NAT path. Shared IPs are proxy-wins. Rules that cannot be decided from destination domain/IP alone are not bypassed. Aster **does not** drop inbound REDIR / TUN SYNs to prevent self-hijack; that blackholes DIRECT as well.

`kernel-direct` does not require eBPF. The recommended backend is nftables plus OpenWrt flow offload. There is also an experimental TC eBPF DIRECT / PROXY classifier that is off by default. Functionally it is closer to dae’s ingress hook, but the per-packet work can reduce throughput. Enable it only after a same-server A/B on the target router. Settings, safety boundaries, IPv6 split-WAN troubleshooting, and a measured counter-example are in [OpenWrt and Nikki](/en/deployment/openwrt).

## Aster server management

Aster can add, update, disable, or delete VLESS / AnyTLS users live without rebuilding the whole listener. Management data includes:

- A per-listener revision so concurrent edits do not overwrite each other
- Per-user upload, download, and active connections
- Rotatable single-user subscription links
- `aster-state.json` with backup, locking, and permission checks

This management plane is optional. If you only use Aster as a client, you do not need `aster:` or `/api/admin`.

## Compatibility

Existing Mihomo profiles usually work as-is, but you should still back up first and run:

```sh
aster-core -d <config-directory> -t
```

Watch for:

- Aster uses Mihomo YAML. It does not read Xray / sing-box JSON directly.
- `type: relay` was removed. Use `dialer-proxy`.
- AnyTLS + REALITY requires both ends to support the same parameters.
- The Aster Admin secret is separate from the ordinary Controller secret.

Provenance and the upstream sync process are in the repository [NOTICE.md](https://github.com/Miku0139oao/aster-core/blob/main/NOTICE.md) and [UPSTREAM.md](https://github.com/Miku0139oao/aster-core/blob/main/UPSTREAM.md).
