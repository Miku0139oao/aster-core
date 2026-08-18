# How much faster is Aster than Mihomo?

## The short version

Aster does not magically make your network faster. It removes a lot of repeated small work inside the proxy core. The medians below come from three identical rounds of the same program and tests on a real OpenWrt soft router:

| Workload you actually hit | How much faster | Why |
| --- | ---: | --- |
| Moving 32 KiB over TCP | **about 3% more throughput**, about 3% less processing time | Reuse the scratch buffer that moves data |
| Packing a 1 KiB AnyTLS frame | **about 1.9× faster** | Write straight into a reusable buffer; no extra object wrapper |
| Packing a 16 KiB AnyTLS frame | **about 1.3× faster** | Same as above, plus fewer small allocations |
| Preparing one UDP packet | **about 5.1× faster** | Recycle the packet data structure and reuse it next time |
| Ignoring one disabled debug log | **about 97× faster** | Return before the string or log event is built |

> [!IMPORTANT]
> **The closest “actually moving data” number is the TCP improvement of about 3%.** 5.1× and 97× are tiny core steps that run many times. They do not mean download speed becomes 5.1× or 97×.

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

The comparison baseline is Aster’s upstream Mihomo `v1.19.29`, commit `e26714a181ac0e2fa803453c0a8e9a9ce94e31cb`. Both versions used the same benchmark program and parameters, three sequential rounds, at least 2 seconds each. The table uses the three-round median.

| Environment | Actual value |
| --- | --- |
| System | OpenWrt, Linux 6.6.86, x86-64 |
| CPU | Ryzen 7 5825U host, VM configured with 12 vCPU |
| CPU frequency | Hypervisor did not expose it, **unavailable** |
| Memory | VM configured with about 6 GB |
| DRAM frequency | Hypervisor did not expose it, **unavailable** |
| Go | 1.26.3, Linux amd64 test binary |
| Load average before the test | 0.19／0.15／0.10 (1／5／15 minutes) |
| Load average after the test | 0.94／0.41／0.20 (1／5／15 minutes) |

| Core work | Mihomo median | Aster median | Aster relative result |
| --- | ---: | ---: | ---: |
| UDP packet metadata | 68.45 ns; 416 B／1 alloc | 13.38 ns; 0 B／0 alloc | **5.1× faster**; removed the 416 B allocation |
| Disabled debug log | 222.3 ns; 24 B／1 alloc | 2.291 ns; 0 B／0 alloc | **97× faster**; removed the event allocation |
| AnyTLS frame (1 KiB) | 71.03 ns; 64 B／1 alloc | 36.65 ns; 0 B／0 alloc | **1.94× faster**; latency down 48% |
| AnyTLS frame (16 KiB) | 270.3 ns; 64 B／1 alloc | 207.1 ns; 0 B／0 alloc | **1.31× faster**; latency down 23% |
| TCP relay (32 KiB) | 4.397 µs; 7.45 GB/s; 64 B／1 alloc | 4.271 µs; 7.67 GB/s; 0 B／0 alloc | Latency down **2.9%**; throughput up **3.0%** |

### Three-round ranges

| Benchmark | Mihomo range | Aster range |
| --- | ---: | ---: |
| UDP packet metadata | 67.50–68.91 ns/op | 13.02–13.38 ns/op |
| Disabled debug log | 221.1–225.4 ns/op | 2.245–2.297 ns/op |
| AnyTLS frame (1 KiB) | 70.78–71.09 ns/op | 35.71–36.67 ns/op |
| AnyTLS frame (16 KiB) | 269.7–275.1 ns/op | 204.5–211.8 ns/op |
| TCP relay (32 KiB) | 4.396–4.418 µs/op | 4.227–4.337 µs/op |

This 5825U soft router is still stronger than many MT7621, low-end ARM, or cheap VPS hosts. These results only prove the optimizations still work on real OpenWrt. Weaker devices will have lower absolute GB/s, and the improvement ratio must be remeasured on that device.

## Hyper-V low-resource simulation (secondary result)

To get closer to weaker hardware most users have, we added another resource limit on the same Hyper-V OpenWrt VM. The benchmark was allowed **1 vCPU**, and at most 25 ms of execution every 100 ms, which is **single-core 25% CPU time**. Memory was capped at **512 MiB**, with swap disabled.

This simulates a resource-starved situation. It is not an MT7621 or ARM ISA simulation. It answers “do Aster’s optimizations still exist when CPU is slow and RAM is scarce,” but it does not replace a test on a real low-end router.

| Environment | Actual value |
| --- | --- |
| System | OpenWrt on Hyper-V, Linux 6.6.86, x86-64 |
| Host CPU | Ryzen 7 5825U |
| CPU limit | Pinned to vCPU 0; cgroup `cpu.max = 25000 100000`, i.e. single-core 25% CPU time |
| Guest-reported CPU frequency | Average 1.998 GHz before the test; average 2.080 GHz after. This is a frequency sample and **does not include the 25% CPU-time limit** |
| Memory limit | 512 MiB, no swap; test-process peak about 23.2 MiB |
| DRAM frequency | Hypervisor did not expose it, **unavailable** |
| Go | 1.26.3, Linux amd64 test binary |
| Load average before the test | 0.00／0.11／0.12 (1／5／15 minutes) |
| Load average after the test | 0.29／0.19／0.15 (1／5／15 minutes) |
| Throttle confirmation | 1,197 of 1,200 CPU quota periods were throttled |

Both versions ran the same benchmarks sequentially, three rounds, at least 2 seconds each. The table uses the three-round median:

| Core work | Mihomo median | Aster median | Aster relative result |
| --- | ---: | ---: | ---: |
| UDP packet metadata | 239.0 ns; 416 B／1 alloc | 47.60 ns; 0 B／0 alloc | **5.0× faster**; removed the 416 B allocation |
| Disabled debug log | 842.6 ns; 24 B／1 alloc | 8.044 ns; 0 B／0 alloc | **about 105× faster**; removed the event allocation |
| AnyTLS frame (1 KiB) | 315.1 ns; 64 B／1 alloc | 128.0 ns; 0 B／0 alloc | **2.46× faster**; latency down 59% |
| AnyTLS frame (16 KiB) | 1.087 µs; 64 B／1 alloc | 835.6 ns; 0 B／0 alloc | **1.30× faster**; latency down 23% |
| TCP relay (32 KiB) | 16.627 µs; 1.97 GB/s; 64 B／1 alloc | 15.918 µs; 2.06 GB/s; 0 B／0 alloc | Latency down **4.3%**; throughput up **4.5%** |

### Peak memory

Keep two “memory” numbers separate:

| Situation | Mihomo | Aster | Conclusion |
| --- | ---: | ---: | --- |
| Full core, minimal profile, idle 15 s RSS | 28.9 MiB | 31.6 MiB | Aster is 2.8 MiB larger, about **+9.5%** |
| Full-core cgroup `memory.current` | 6.50 MiB | 6.80 MiB | Aster is about 0.30 MiB larger, about **+4.6%** |

The full-core test used the same profile: Direct mode, no proxies, no rules, silent logs, IPv6 off. Aster and Mihomo were interleaved for three rounds, waiting 15 seconds after each start. RSS three-round ranges were Aster 31.5–31.9 MiB and Mihomo 28.9–29.1 MiB. The OpenWrt kernel did not provide `smaps_rollup`, so there is no PSS data.

**So you cannot say Aster uses less RAM when idle.** Aster ships more features and code, and the minimal idle base is currently about 2.8 MiB larger than Mihomo. The 30–49% reductions below only appear when specific hot paths are repeated. They mean less temporary memory while busy, not a smaller idle RSS for the whole program.

### Isolated-process peaks for core hot paths

Each case was rerun as an isolated process for three rounds. Max RSS was recorded with Linux `/usr/bin/time -v`. The table uses the three-round median. Both sides used the same single-core 25% CPU time and 512 MiB memory limit.

| Core work | Mihomo peak RSS | Aster peak RSS | Difference |
| --- | ---: | ---: | ---: |
| TCP relay (32 KiB) | 39.5 MiB | 24.5 MiB | **38% less** |
| Disabled debug log | 33.0 MiB | 23.0 MiB | **30% less** |
| UDP packet metadata | 50.6 MiB | 26.0 MiB | **49% less** |
| AnyTLS frame (1 KiB) | 38.9 MiB | 23.5 MiB | **40% less** |
| AnyTLS frame (16 KiB) | 39.6 MiB | 23.5 MiB | **41% less** |

These numbers are peak RSS of a Go test program that only loaded that package and ran the benchmark. They are not the total RAM of a full proxy with rules, GeoIP, DNS cache, and live connections. They are useful for comparing memory pressure from the same small job. They do not claim Aster always uses 30–49% less RAM under every configuration.

Three-round ranges: TCP Mihomo 39.5–40.5 MiB, Aster 24.0–24.5 MiB; log Mihomo 32.5–33.9 MiB, Aster fixed 23.0 MiB; UDP Mihomo 42.9–55.5 MiB, Aster fixed 26.0 MiB; AnyTLS Mihomo 38.0–40.0 MiB, Aster 23.5–24.0 MiB. All tests used 0 swap.

### Constrained three-round ranges

| Benchmark | Mihomo range | Aster range |
| --- | ---: | ---: |
| UDP packet metadata | 231.3–243.0 ns/op | 47.13–48.03 ns/op |
| Disabled debug log | 825.6–921.2 ns/op | 7.958–8.066 ns/op |
| AnyTLS frame (1 KiB) | 313.3–319.2 ns/op | 126.3–129.9 ns/op |
| AnyTLS frame (16 KiB) | 1.047–1.097 µs/op | 731.3–895.3 ns/op |
| TCP relay (32 KiB) | 16.605–17.087 µs/op | 15.754–16.501 µs/op |

Absolute processing time became about four times the unrestricted run, which shows the CPU quota was real. Aster was still faster on all five jobs. In plain language, **weaker hardware usually feels the saved CPU work and allocations more**: TCP improved from about 3% to 4.3%, and AnyTLS 1 KiB from 1.94× to 2.46×. UDP and AnyTLS 16 KiB ratios barely changed, so you cannot assume every optimization scales the same way as hardware gets weaker. The homepage still uses the unrestricted OpenWrt **TCP about 3%**, not this more flattering 4.3%, as the main advertised number.

## Per-protocol loopback tests

A faster core microbenchmark does not mean every protocol’s end-to-end throughput rises in lockstep. To check that, we connected Aster and Mihomo to the same local server on a Linux Docker host network and measured read/write with 16 KiB chunks.

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

These protocol tests used a Ryzen 9 5900X, Docker Linux amd64, Go 1.26.5. Ordinary protocols ran at least three rounds. Trojan produced contradictory ±20% results in the first sequential set, so it got an extra five interleaved Aster/Mihomo rounds with a fixed test binary; the gap converged to about 2%. Raw host load was not recorded in sync during the protocol tests, so they are only used to look for an obvious protocol regression. **They are not homepage advertising numbers.**

Server images were pinned to shadowsocks-rust `sha256:85d01d…e1359`, V2Fly `sha256:e81a07…de78c`, Trojan `sha256:5b36c2…b98b5f7`, OpenSnell `sha256:70053f…345467`, and Hysteria v1.3.5 `sha256:4c8c92…f1e35`.

### UDP metadata allocation before and after

The same benchmark measures Mihomo’s direct metadata-construction path and Aster’s object-pool path, so they can be compared on the same machine:

| Path | Time | Memory allocation |
| --- | ---: | ---: |
| Mihomo constructs per packet | 67.50–68.91 ns/op | 416 B/op, 1 alloc/op |
| Aster metadata pool | 13.02–13.38 ns/op | 0 B/op, 0 allocs/op |

The object-pool path is about **5.1× faster** on the three-round median and removes a 416-byte heap allocation per packet.

## High-end development machine (appendix)

The original Aster absolute numbers were taken on a high-end desktop. They are only useful as a development-time regression check and should not represent an ordinary router:

| Environment | Actual value |
| --- | --- |
| System | Windows amd64; balanced power mode |
| CPU | Ryzen 9 5900X, 12 cores／24 logical CPUs |
| Actual CPU frequency | Later representative rerun averaged 4.46 GHz, range 4.42–4.51 GHz |
| DRAM | 64 GB (4×16 GB) G.Skill DDR4-3600, configured 3600 MT/s |
| Go | 1.26.3 |
| Original test load | **Not recorded in sync; cannot be backfilled** |
| Later representative rerun CPU load | Average 36.4% before the test (24.7–58.5%); average 37.7% during the test (27.3–55.6%) |
| Later processor queue | Average 0.1, range 0–1 |

Because background load was high and the original test did not record load point by point, the 5900X data is no longer the homepage’s primary comparison. It is kept only to watch for an obvious performance regression.

| Aster standalone benchmark | Payload | Three-round result | Memory allocation |
| --- | ---: | ---: | ---: |
| UDPFlowSteadyState | 1,200 B | 0.85–1.22 µs/op | 0 B/op, 0 allocs/op |
| TCPRelayThroughput | 32 KiB | 9.99–10.72 µs/op | 0 B/op, 0 allocs/op |
| SessionUpload | 1 KiB | 5.46–5.52 µs/op | 0 B/op, 0 allocs/op |
| SessionUpload | 16 KiB | 8.02–8.46 µs/op | 0 B/op, 0 allocs/op |

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

When you compare two versions, run them on the same machine, same Go version, and same power mode, then analyze multiple rounds with `benchstat`. Do not compute a percentage from a single run on different machines.

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
