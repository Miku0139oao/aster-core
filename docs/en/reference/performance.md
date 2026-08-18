# How much faster is Aster than Mihomo?

## The short version

Aster does not magically make your network faster. It removes a lot of repeated small work inside the proxy core. The medians below come from three identical rounds of the same program and tests on a real OpenWrt soft router:

| Workload you actually hit | How much faster | Why |
| --- | ---: | --- |
| Moving 32 KiB over TCP | **about 4% more throughput**, about 4% less processing time | Reuse the scratch buffer that moves data |
| Packing a 1 KiB AnyTLS frame | **about 2.0× faster** | Write straight into a reusable buffer; no extra object wrapper |
| Packing a 16 KiB AnyTLS frame | **about 1.3× faster** | Same as above, plus fewer small allocations |
| Preparing one UDP packet | **about 5.7× faster** | Recycle the packet data structure and reuse it next time |
| Ignoring one disabled debug log | **about 98× faster** | Return before the string or log event is built |

> [!IMPORTANT]
> **The closest “actually moving data” number is the TCP improvement of about 4%.** 5.7× and 98× are tiny core steps that run many times. They do not mean download speed becomes 5.7× or 98×.

## What actually changed?

### 1. Stop allocating new memory for every packet

TCP, UDP, and AnyTLS used to create new scratch objects for each piece of data. Those objects are now recycled and reused. The CPU spends less time on garbage, and busy loads are less likely to hitch.

### 2. Connections do not fight over one lock

Looking up rules or a proxy used to read shared data under a lock. Now a complete snapshot is published when configuration updates. Packet processing only reads the snapshot and does not queue on one another.

### 3. UDP no longer repeats the same conversions

Source addresses stay in a form the computer can compare directly, instead of being converted to a string on every packet. Socket idle timers are refreshed in batches instead of being reset on every received packet.

### 4. AnyTLS rules are computed once

Padding rules are parsed when the configuration loads. When data is sent, Aster looks up the result and builds the frame directly. It no longer splits strings and recalculates while transmitting.

### 5. Logs that will not be shown do no work

If debug logging is off and no dashboard is listening, Aster returns before formatting the message. Previously it still built a full log and then discarded it.

### 6. Traffic numbers increment in place

Upload, download, and connection counts use cheap incremental stats. Each traffic update only changes the numbers. It no longer rescans every active connection to get a total.

## Full test data

## OpenWrt on real hardware (primary result)

The comparison baseline is Mihomo `v1.19.30`, commit `ac017cdd246ce8bd547653d927e7bf77d7ee73d5`. Aster was `main` at `dd750849`. Both sides used the same `go test -c` Linux amd64 binaries and parameters, three sequential rounds, at least 2 seconds each. The table uses the three-round median. Rerun on 2026-08-19 on the same OpenWrt soft router.

| Environment | Actual value |
| --- | --- |
| System | OpenWrt, Linux 6.6.86, x86-64 |
| CPU | Ryzen 7 5825U host, VM configured with 12 vCPU |
| CPU frequency | Hypervisor did not expose it, **unavailable** |
| Memory | VM configured with about 6 GB (`MemTotal` 6081752 kB) |
| DRAM frequency | Hypervisor did not expose it, **unavailable** |
| Go | 1.26.3, `GOAMD64=v1` cross-compiled Linux amd64 test binary |
| Load average before the test | 0.02／0.05／0.05 (1／5／15 minutes) |
| Load average after the test | 1.03／0.43／0.19 (1／5／15 minutes) |

| Core work | Mihomo 1.19.30 median | Aster median | Aster relative result |
| --- | ---: | ---: | ---: |
| UDP packet metadata | 73.74 ns; 416 B／1 alloc | 13.04 ns; 0 B／0 alloc | **5.65× faster**; removed the 416 B allocation |
| Disabled debug log | 225.3 ns; 24 B／1 alloc | 2.289 ns; 0 B／0 alloc | **98× faster**; removed the event allocation |
| AnyTLS frame (1 KiB) | 72.12 ns; 64 B／1 alloc | 36.34 ns; 0 B／0 alloc | **1.98× faster**; latency down 50% |
| AnyTLS frame (16 KiB) | 270.8 ns; 64 B／1 alloc | 205.0 ns; 0 B／0 alloc | **1.32× faster**; latency down 24% |
| TCP relay (32 KiB) | 4.607 µs; 7.11 GB/s; 64 B／1 alloc | 4.433 µs; 7.39 GB/s; 0 B／0 alloc | Latency down **3.8%**; throughput up **3.9%** |

### Three-round ranges

| Benchmark | Mihomo 1.19.30 range | Aster range |
| --- | ---: | ---: |
| UDP packet metadata | 71.66–75.71 ns/op | 12.97–13.04 ns/op |
| Disabled debug log | 224.1–225.3 ns/op | 2.278–2.295 ns/op |
| AnyTLS frame (1 KiB) | 71.09–72.40 ns/op | 35.71–36.81 ns/op |
| AnyTLS frame (16 KiB) | 260.1–273.7 ns/op | 195.8–211.6 ns/op |
| TCP relay (32 KiB) | 4.510–4.738 µs/op | 4.373–4.452 µs/op |

This 5825U soft router is still stronger than many MT7621, low-end ARM, or cheap VPS hosts. These results only prove the optimizations still work on real OpenWrt. Weaker devices will have lower absolute GB/s, and the improvement ratio must be remeasured on that device.

## Hyper-V low-resource simulation (secondary result)

To get closer to weaker hardware most users have, we added another resource limit on the same Hyper-V OpenWrt VM. The benchmark was allowed **1 vCPU**, and at most 25 ms of execution every 100 ms, which is **single-core 25% CPU time**. Memory was capped at **512 MiB**, with swap disabled. Rerun on 2026-08-19 against Mihomo `v1.19.30` with the same Linux amd64 test binaries.

This simulates a resource-starved situation. It is not an MT7621 or ARM ISA simulation. It answers “do Aster’s optimizations still exist when CPU is slow and RAM is scarce,” but it does not replace a test on a real low-end router.

| Environment | Actual value |
| --- | --- |
| System | OpenWrt on Hyper-V, Linux 6.6.86, x86-64 |
| Host CPU | Ryzen 7 5825U |
| CPU limit | Pinned to vCPU 0; cgroup `cpu.max = 25000 100000`, i.e. single-core 25% CPU time |
| Guest-reported CPU frequency | Average 2.304 GHz before the test; average 2.002 GHz after. This is a frequency sample and **does not include the 25% CPU-time limit** |
| Memory limit | 512 MiB, no swap |
| DRAM frequency | Hypervisor did not expose it, **unavailable** |
| Go | 1.26.3, Linux amd64 test binary |
| Load average before the test | 0.11／0.17／0.16 (1／5／15 minutes) |
| Load average after the test | 0.26／0.20／0.17 (1／5／15 minutes) |
| Throttle confirmation | 1,121 of 1,122 CPU quota periods were throttled |

Both versions ran the same benchmarks sequentially, three rounds, at least 2 seconds each. The table uses the three-round median:

| Core work | Mihomo 1.19.30 median | Aster median | Aster relative result |
| --- | ---: | ---: | ---: |
| UDP packet metadata | 240.5 ns; 416 B／1 alloc | 50.97 ns; 0 B／0 alloc | **4.72× faster**; removed the 416 B allocation |
| Disabled debug log | 846.6 ns; 24 B／1 alloc | 8.314 ns; 0 B／0 alloc | **about 102× faster**; removed the event allocation |
| AnyTLS frame (1 KiB) | 328.9 ns; 64 B／1 alloc | 135.5 ns; 0 B／0 alloc | **2.43× faster**; latency down 59% |
| AnyTLS frame (16 KiB) | 1.126 µs; 64 B／1 alloc | 753.7 ns; 0 B／0 alloc | **1.49× faster**; latency down 33% |
| TCP relay (32 KiB) | 17.829 µs; 1.84 GB/s; 64 B／1 alloc | 16.879 µs; 1.94 GB/s; 0 B／0 alloc | Latency down **5.3%**; throughput up **5.6%** |

### Peak memory

Keep two “memory” numbers separate:

| Situation | Mihomo 1.19.30 | Aster | Conclusion |
| --- | ---: | ---: | --- |
| Full core, minimal profile, idle 15 s RSS | 34.8 MiB | 39.3 MiB | Aster is 4.5 MiB larger, about **+13%** |

The full-core test used the same profile: Direct mode, no proxies, no rules, silent logs, IPv6 off, TUN off, bound only to unused 127.0.0.1 ports. Aster and Mihomo each ran three rounds, waiting 15 seconds after each start. RSS three-round ranges were Aster 39.0–39.5 MiB and Mihomo 34.6–35.4 MiB. The OpenWrt kernel did not provide `smaps_rollup`, so there is no PSS. This run also could not read a per-process `memory.current` from a child cgroup (controller delegation was denied), so only `/proc/<pid>/status` `VmRSS` is reported.

**So you cannot say Aster uses less RAM when idle.** Aster ships more features and code, and the minimal idle base is currently about 4.5 MiB larger than Mihomo 1.19.30. The isolated-process peaks below are Max RSS of a Go test program that only loaded that package. They are not the total RAM of a full proxy with rules, GeoIP, DNS cache, and live connections.

### Isolated-process peaks for core hot paths

Each case was rerun as an isolated process for three rounds. Max RSS was recorded with Linux `/usr/bin/time -v`. The table uses the three-round median. Both sides used the same single-core 25% CPU time and 512 MiB memory limit.

| Core work | Mihomo 1.19.30 peak RSS | Aster peak RSS | Difference |
| --- | ---: | ---: | ---: |
| TCP relay (32 KiB) | 39.5 MiB | 50.5 MiB | Aster is **28% larger** |
| Disabled debug log | 33.4 MiB | 23.0 MiB | **31% less** |
| UDP packet metadata | 56.0 MiB | 53.4 MiB | Roughly even (about 5% less) |
| AnyTLS frame (1 KiB) | 34.7 MiB | 19.5 MiB | **44% less** |
| AnyTLS frame (16 KiB) | 34.0 MiB | 19.5 MiB | **43% less** |

After the 1.19.30 rerun, **do not keep saying all five cases used 30–49% less RAM**. Disabled log and AnyTLS are still clearly lower. UDP is roughly even. The isolated TCP test process peaked higher for Aster. This compares temporary pressure from that package test, not Aster’s RAM under every configuration.

Three-round ranges: TCP Mihomo 38.5–40.0 MiB, Aster 50.5–50.8 MiB; log Mihomo 33.0–33.4 MiB, Aster fixed 23.0 MiB; UDP Mihomo 43.1–58.0 MiB, Aster 51.8–57.0 MiB; AnyTLS 1 KiB Mihomo 34.3–34.8 MiB, Aster fixed 19.5 MiB; AnyTLS 16 KiB Mihomo 33.8–34.9 MiB, Aster fixed 19.5 MiB. All tests used 0 swap.

### Constrained three-round ranges

| Benchmark | Mihomo 1.19.30 range | Aster range |
| --- | ---: | ---: |
| UDP packet metadata | 235.1–240.6 ns/op | 50.83–50.99 ns/op |
| Disabled debug log | 826.0–873.5 ns/op | 8.052–8.401 ns/op |
| AnyTLS frame (1 KiB) | 318.7–333.8 ns/op | 131.8–136.1 ns/op |
| AnyTLS frame (16 KiB) | 1.123–1.205 µs/op | 723.6–803.6 ns/op |
| TCP relay (32 KiB) | 17.702–18.418 µs/op | 16.240–17.239 µs/op |

Absolute processing time became about four times the unrestricted run, which shows the CPU quota was real. Aster was still faster on all five latency jobs. In plain language, **weaker hardware usually feels the saved CPU work more**: TCP improved from 3.8% unrestricted to 5.3%, AnyTLS 1 KiB from 1.98× to 2.43×, and AnyTLS 16 KiB from 1.32× to 1.49×. The UDP ratio eased from 5.65× to 4.72×, so you cannot assume every optimization scales the same way as hardware gets weaker. The homepage still uses the unrestricted OpenWrt **TCP about 4%** versus Mihomo 1.19.30, not this more flattering 5.3%, as the main advertised number.

## Per-protocol loopback tests

A faster core microbenchmark does not mean every protocol’s end-to-end throughput rises in lockstep. To check that, we previously connected Aster and Mihomo to the same local server on a Linux Docker host network and measured read/write with 16 KiB chunks. The table below is still versus Mihomo `v1.19.29`; the protocol servers were not rerun against `v1.19.30`.

| Protocol | Server | Aster vs Mihomo |
| --- | --- | --- |
| Direct (control) | none | Read/write gap within about 3%, treated as even |
| Shadowsocks AES-256-GCM | shadowsocks-rust | Same allocation count; write samples were too noisy, **no speedup claim** |
| VMess | V2Fly 4.45.2 | Write about 645 vs 647 MB/s; read about 488 vs 488 MB/s, even |
| VLESS + TLS | V2Fly 4.45.2 | Typical interleaved three-round gap about -2% to +3%, even |
| Trojan + TLS | trojan 1.16 | Interleaved five-round median: write 335 vs 341 MB/s, read 313 vs 314 MB/s, even |
| Snell v3 + HTTP obfs | OpenSnell 3.0.1 | Interleaved three-round median: write 429 vs 430 MB/s, read 763 vs 774 MB/s, even |
| Hysteria v1 | Hysteria 1.3.5, 100 Mbps | Both about 12.25–12.28 MB/s, limited by the server bandwidth cap |
| AnyTLS | No same-condition loopback server today | Do not mix in an external node; only the earlier frame microbenchmark is listed |

Numbers in the table are **Aster vs Mihomo**. VMess, VLESS, Trojan, Snell, and Shadowsocks B/op and allocs/op are also roughly the same. This round of work mainly improved shared relay, UDP metadata, logging, and AnyTLS frame hot paths. It did not magically make every encrypted protocol much faster.

These protocol tests are still the earlier Docker loopback against Mihomo `v1.19.29`. They were **not** rerun against `v1.19.30`. This 5900X host has Docker Engine, but not the original pinned protocol server images, and pulling new tags would not match those SHAs. The encryption and copy paths are shared; the 1.19.30 microbenchmarks already cover the changed relay, UDP, log, and AnyTLS frame work. The table is only here to show that encrypted protocols did not all jump together. **They are not homepage advertising numbers.**

Server images were pinned to shadowsocks-rust `sha256:85d01d…e1359`, V2Fly `sha256:e81a07…de78c`, Trojan `sha256:5b36c2…b98b5f7`, OpenSnell `sha256:70053f…345467`, and Hysteria v1.3.5 `sha256:4c8c92…f1e35`.

### UDP metadata allocation before and after

The same benchmark measures Mihomo’s direct metadata-construction path and Aster’s object-pool path, so they can be compared on the same machine:

| Path | Time | Memory allocation |
| --- | ---: | ---: |
| Mihomo 1.19.30 constructs per packet | 71.66–75.71 ns/op | 416 B/op, 1 alloc/op |
| Aster metadata pool | 12.97–13.04 ns/op | 0 B/op, 0 allocs/op |

The object-pool path is about **5.65× faster** on the three-round median and removes a 416-byte heap allocation per packet.

## High-end development machine (appendix)

The original Aster absolute numbers were taken on a high-end desktop. They are only useful as a development-time regression check and should not represent an ordinary router. On 2026-08-19 the same 5900X reran the same microbenchmarks against Mihomo `v1.19.30` and Aster `main`, interleaved, three rounds, at least 2 seconds each:

| Environment | Actual value |
| --- | --- |
| System | Windows 11 amd64; balanced power mode |
| CPU | Ryzen 9 5900X, 12 cores／24 logical CPUs |
| Reported frequency | Windows `\Processor Frequency` stayed at 3701 MHz (advertised clock, **not** actual boost) |
| DRAM | 64 GB (4×16 GB) G.Skill DDR4-3600, configured 3600 MT/s |
| Go | 1.26.3 windows/amd64 |
| CPU load before each case | About 30% average across start samples (17.6–60.9%) |
| CPU load after each case | About 34% average across end samples (20.3–54.8%) |
| Processor queue | 0 throughout |

Background load is still not low, so the 5900X run is **not** the homepage’s primary comparison. It is only here to confirm the desktop development machine did not regress. An earlier same-day pass with about 42–47% background load and no interleaving produced contradictory TCP numbers and was discarded.

| Core work | Mihomo 1.19.30 median | Aster median | Aster relative result |
| --- | ---: | ---: | ---: |
| UDP packet metadata | 150.8 ns; 416 B／1 alloc | 11.66 ns; 0 B／0 alloc | **12.9× faster**; removed the 416 B allocation |
| Disabled debug log | 607.8 ns; 24 B／1 alloc | 2.820 ns; 0 B／0 alloc | **215× faster**; removed the event allocation |
| AnyTLS frame (1 KiB) | 69.60 ns; 64 B／1 alloc | 34.35 ns; 0 B／0 alloc | **2.03× faster** |
| AnyTLS frame (16 KiB) | 245.7 ns; 64 B／1 alloc | 199.8 ns; 0 B／0 alloc | **1.23× faster** |
| TCP relay (32 KiB) | 10.607 µs; 3.09 GB/s; 64 B／1 alloc | 9.871 µs; 3.32 GB/s; 0 B／0 alloc | Latency down **6.9%**; throughput up **7.5%** |

Three-round ranges: UDP Mihomo 139.3–173.9 ns, Aster pool 11.41–11.77 ns; log Mihomo 421.8–617.4 ns, Aster 2.277–2.996 ns; AnyTLS 1 KiB Mihomo 69.42–71.19 ns, Aster 32.85–38.97 ns; AnyTLS 16 KiB Mihomo 243.4–267.1 ns, Aster 190.3–213.6 ns; TCP Mihomo 10.108–11.045 µs, Aster 9.258–10.064 µs. A second interleaved TCP trio had Aster at 9.418 µs versus Mihomo 10.269 µs, same direction.

Aster standalone regression (same pass, not a comparison):

| Aster standalone benchmark | Payload | Three-round result | Memory allocation |
| --- | ---: | ---: | ---: |
| UDPFlowSteadyState | 1,200 B | 1.02–1.13 µs/op | 0 B/op, 0 allocs/op |
| TCPRelayThroughput | 32 KiB | 9.26–10.06 µs/op | 0 B/op, 0 allocs/op |
| SessionUpload | 1 KiB | 4.81–5.48 µs/op | 0 B/op, 0 allocs/op |
| SessionUpload | 16 KiB | 9.05–10.74 µs/op | 0 B/op, 0 allocs/op |

## How to rerun

From the repository root:

```sh
go test \
  ./component/nat ./constant ./listener/sing ./log \
  ./transport/anytls/padding ./transport/anytls/session \
  ./tunnel ./tunnel/statistic \
  -run '^$' \
  -bench 'Benchmark' \
  -benchmem \
  -count=3
```

When comparing against Mihomo, build the same Linux amd64 test binaries from tag `v1.19.30` (`ac017cdd`) and run three sequential rounds on the same machine. Do not compute a percentage from a single run on different machines.

## How to read the numbers

- These are in-process microbenchmarks. They mainly measure Aster Core’s own extra cost.
- TCP and AnyTLS GB/s numbers are memory / `net.Pipe` paths, not real WAN throughput.
- Real proxy speed is still limited by encryption, RTT, loss, MTU, NIC, OS, CPU architecture, and the server.
- 64 KiB AnyTLS frames still need a large buffer allocation. The common 1 KiB and 16 KiB paths are already zero-alloc.
- Microbenchmarks are best at catching regressions. They do not promise the same network speed on every device.

## A real-hardware counter-example for TC eBPF

Kernel DIRECT itself does not require TC eBPF. The recommended OpenWrt path is the nftables learned exclude set handing DIRECT back to Linux forwarding/NAT, while keeping flow offload. Experimental `kernel-direct-ebpf` looks up generation, IPv4/IPv6 LPM, a 40-byte 5-tuple LRU, and per-CPU counters on every LAN ingress packet.

On the same router and the same Speedtest server ID `37639` A/B:

| State | Download |
| --- | ---: |
| TC eBPF on | 692,335,768 bps |
| TC filters temporarily unloaded | 1,647,299,448 bps |
| TC persistently off, after restart | 1,643,651,288 bps |

After unload it is about **2.37×** the enabled result, and it returns to that network’s original ~1.7 Gbps class. The main reason is that this TC ingress hook interferes with OpenWrt flow offload, plus the classifier itself runs per packet. It does not mean every other eBPF program or every other piece of hardware is slower.

This is also why microbenchmarks and real network results must be kept separate: a fast map lookup or data structure does not mean putting it on every ingress packet, and changing the kernel offload path, will raise end-to-end throughput. Before and after enabling TC, keep the client, server ID, protocol, and time window fixed and run multiple times. If there is no clear gain, leave `kernel-direct-ebpf: false`.
