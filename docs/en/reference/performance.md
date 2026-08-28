# How much faster is Aster than Mihomo?

## The short version

Aster does not magically make your network faster. It removes a lot of repeated small work inside the proxy core. The medians below come from three identical rounds of the same program and tests on a real OpenWrt soft router:

| Workload you actually hit | How much faster | Why |
| --- | ---: | --- |
| Moving 32 KiB over TCP | **about 2% more throughput**, about 2% less processing time | Reuse the scratch buffer that moves data |
| Packing a 1 KiB AnyTLS frame | **about 2.0× faster** | Write straight into a reusable buffer; no extra object wrapper |
| Packing a 16 KiB AnyTLS frame | **about 1.4× faster** | Same as above, plus fewer small allocations |
| Preparing one UDP packet | **about 5.4× faster** | Recycle the packet data structure and reuse it next time |
| Ignoring one disabled debug log | **about 101× faster** | Return before the string or log event is built |

> [!IMPORTANT]
> **The closest “actually moving data” number is the TCP improvement of about 2%.** 5.4× and 101× are tiny core steps that run many times. They do not mean download speed becomes 5.4× or 101×.

> [!NOTE]
> The latest 2026-08-28 results compare **Aster before and after this optimization wave**. They are not Aster-versus-Mihomo numbers. They prove that core CPU and allocation costs fell, but they do not turn the OpenWrt TCP result below into a larger network-speed claim.

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

## 2026-08-28 core hot-path optimization A/B

This run compares Aster `90f0e4ee` (before this wave) with `72048c8a` (performance commit `9824ccdb` plus its lint fix). Each revision was built as separate Windows amd64 test binaries. Every sample used a fresh process, `GOMAXPROCS=1`, `-test.cpu=1`, `GOAMD64=v1`, Go 1.26.3, and a two-second run. Before and after were interleaved for seven rounds, alternating which revision ran first.

The main matrix covered 11 packages and 24 identically named cases per revision, for 168 samples per revision. The table reports the seven-round median and full range. Every improvement listed below had `p=0.001` in benchstat's Mann–Whitney U test. The machine was Windows 11, a Ryzen 9 5900X (12C/24T), and 64 GB DDR4. Total CPU samples were 9.85–17.22% before the run and 7.19–15.71% after it. This is still a development-machine microbenchmark, not WAN throughput.

| Aster core work | Before median (range) | After median (range) | Result | Allocations |
| --- | ---: | ---: | ---: | ---: |
| Existing Kernel DIRECT flow refresh | 221.0 ns (208.2–243.9) | 182.0 ns (179.8–185.9) | **1.21×**; time down 17.6% | 0 B／0 → 0 B／0 |
| Existing NAT flow lookup | 64.26 ns (63.99–65.38) | 11.67 ns (11.58–12.09) | **5.51×**; time down 81.8% | 0 B／0 → 0 B／0 |
| UDP WriteBack target update | 7.515 ns (7.491–7.802) | 3.918 ns (3.908–4.084) | **1.92×**; time down 47.9% | 0 B／0 → 0 B／0 |
| `DomainSet.Has` (short) | 166.7 ns (165.0–170.5) | 56.32 ns (54.96–56.96) | **2.96×**; time down 66.2% | 0 B／0 → 0 B／0 |
| Miss in 100k merged CIDRs | 84.63 ns (83.60–90.87) | 12.93 ns (12.57–15.84) | **6.55×**; time down 84.7% | 0 B／0 → 0 B／0 |
| `GetUser` (10,000 users) | 56.67 ns (55.23–61.04) | 40.26 ns (37.26–47.98) | **1.41×**; time down 29.0% | 0 B／0 → 0 B／0 |
| Upload traffic increment | 3.561 ns (3.533–3.652) | 1.742 ns (1.717–1.852) | **2.04×**; time down 51.1% | 0 B／0 → 0 B／0 |
| TCP tracker lifecycle | 574.7 ns (561.3–592.6) | 257.6 ns (253.5–314.1) | **2.23×**; time down 55.2% | 528 B／8 → 352 B／3 |
| Default-rule match | 22.97 ns (22.78–23.30) | 20.13 ns (20.03–20.53) | **1.14×**; time down 12.4% | 0 B／0 → 0 B／0 |
| UDP `handlePacket` (same benchmark-only harness) | 299.8 ns (259.1–353.4) | 187.4 ns (170.5–230.0) | **1.60×**; time down 37.5% | 280 B／8 → 208 B／2 |

### Controls and results that are not advertised as wins

- The production UDP metadata-pool code is identical in both revisions. The ordinary test binaries initially showed 11.50 → 13.18 ns. With one minimal, identical harness on both revisions it measured 12.84 → 12.73 ns (`p=0.874`), confirming a test-layout artifact rather than a pool regression. The complete `handlePacket` path did remove 37.5% of the time and 75% of allocation count.
- Inserting 100 or 1,000 UDP mappings did not change significantly (1,000: 348.2 → 331.1 µs, `p=0.097`); allocation stayed at 615,856 B／2,049 allocs.
- The 1 KiB, 16 KiB, and 64 KiB AnyTLS `WriteDataFrame` cases all stayed zero-allocation. The 1 KiB and 16 KiB medians were flat; the 64 KiB change was not significant (`p=0.073`).
- The shared 32 KiB relay median moved from 3.766 to 3.959 µs (+5.1%), and the tunnel TCP relay from 3.899 to 4.023 µs (+3.2%). Both before/after ranges overlap. This run does not claim a desktop TCP speedup and does not overwrite the OpenWrt result below. Determining whether 3–5% is a real regression requires a fixed-frequency CPU or another OpenWrt hardware rerun.

This A/B validates Aster's internal hot paths in `9824ccdb`. It did not run a Mihomo binary or use an external endpoint, so it answers “did this Aster commit lower core overhead,” not “how much faster will a download be.”

## 2026-08-24 review-wave validation

The unreleased working tree based on `8462a265` was rebuilt against Mihomo `v1.19.30` (`ac017cdd`) as separate Windows amd64 test binaries. Every sample used a fresh process, `GOMAXPROCS=1`, `-test.cpu=1`, `GOAMD64=v1`, Go 1.26.3, and a two-second run; Aster and Mihomo were interleaved for seven rounds. This is regression validation on the 5900X development machine. It does not replace the OpenWrt primary result below and is not WAN throughput.

| Core work | Mihomo 1.19.30 median (range) | Aster median (range) | Allocations |
| --- | ---: | ---: | ---: |
| UDP metadata | 54.44 ns (49.80–71.77) | 12.22 ns (11.76–12.48), **4.45×** | 416 B／1 → 0 B／0 |
| Disabled debug log | 206.3 ns (204.3–245.5) | 2.132 ns (2.096–3.063), **96.8×** | 24 B／1 → 0 B／0 |
| AnyTLS frame, 1 KiB | 72.55 ns (66.80–82.65) | 31.67 ns (30.93–33.76), **2.29×** | 64 B／1 → 0 B／0 |
| AnyTLS frame, 16 KiB | 322.8 ns (320.4–354.0) | 244.3 ns (218.4–277.3), **1.32×** | 64 B／1 → 0 B／0 |
| Shared 32 KiB relay | 4.038 µs (4.005–4.157) | 3.916 µs (3.763–4.477) | 64 B／1 → 0 B／0 |
| Tunnel 32 KiB TCP relay | 4.079 µs (3.892–4.666) | 4.016 µs (3.840–4.198) | 64 B／1 → 0 B／0 |

The two TCP relay ranges overlap, so this run establishes that **Aster stayed zero-allocation, not a statistically significant desktop TCP speedup**. UDP, logging, and AnyTLS allocation classes and direction agree with the OpenWrt result.

Same-host before/after regression benchmarks for this review wave showed:

| Aster-only hot path | Before review | After review | Result |
| --- | ---: | ---: | ---: |
| Existing Kernel DIRECT flow refresh | 15.286 µs; 64 B／1 alloc | 270.8 ns; 0 B／0 alloc | **56.4×**; no full scan before the next expiry |
| Miss in 100k merged, disjoint CIDRs | 2.385 ms; 0 alloc | 114.7 ns; 0 alloc | **about 20,790×**; restored binary search |
| Insert 1,000 UDP-association mappings | 41.31 ms; 46.37 MB | 486.3 µs; 542.6 KiB | **84.9×**; removed whole-map copying |

For five full-core runs with the same minimal config, sampled 15 seconds after startup, Windows working-set medians were 19.00 MiB for Aster and 18.07 MiB for Mihomo (Aster +0.93 MiB / +5.2%). Private bytes were 52.52 MiB versus 51.29 MiB (+1.23 MiB / +2.4%). These Windows counters cannot be mixed with Linux RSS/PSS and again do not support a general claim that Aster uses less RAM when idle.

## OpenWrt on real hardware (primary result)

The comparison baseline is Mihomo `v1.19.30`, commit `ac017cdd246ce8bd547653d927e7bf77d7ee73d5`. Aster was `main` at `0590d3a4` (this review/fix wave plus the 128 KiB frame pool). Each side was built separately with the same Go version, target, flags, and benchmark harness as a Linux amd64 `go test -c` binary, then run for three sequential rounds, at least 2 seconds each. The table uses the three-round median. Rerun on 2026-08-19 on the same OpenWrt soft router after the fixes landed.

| Environment | Actual value |
| --- | --- |
| System | OpenWrt, Linux 6.6.86, x86-64 |
| CPU | Ryzen 7 5825U host, VM configured with 12 vCPU |
| CPU frequency | Guest sample averaged 2.342 GHz before the unrestricted run. This is `/proc/cpuinfo`; the hypervisor does not guarantee a constant clock |
| Memory | VM configured with about 6 GB (`MemTotal` 6081752 kB) |
| DRAM frequency | Hypervisor did not expose it, **unavailable** |
| Go | 1.26.3, `GOAMD64=v1` cross-compiled Linux amd64 test binary |
| Load average before the test | 0.07／0.03／0.00 (1／5／15 minutes) |
| Load average after the test | 0.92／0.37／0.14 (1／5／15 minutes) |

| Core work | Mihomo 1.19.30 median | Aster median | Aster relative result |
| --- | ---: | ---: | ---: |
| UDP packet metadata | 70.00 ns; 416 B／1 alloc | 12.89 ns; 0 B／0 alloc | **5.43× faster**; removed the 416 B allocation |
| Disabled debug log | 228.0 ns; 24 B／1 alloc | 2.254 ns; 0 B／0 alloc | **101× faster**; removed the event allocation |
| AnyTLS frame (1 KiB) | 71.54 ns; 64 B／1 alloc | 35.98 ns; 0 B／0 alloc | **1.99× faster**; latency down 50% |
| AnyTLS frame (16 KiB) | 267.1 ns; 64 B／1 alloc | 196.8 ns; 0 B／0 alloc | **1.36× faster**; latency down 26% |
| AnyTLS frame (64 KiB) | no matching bench | 1.148 µs; 0 B／0 alloc | The isolated `WriteDataFrame/65536` fixture is zero-alloc with the 128 KiB pool |
| TCP relay (32 KiB) | 4.546 µs; 7.21 GB/s; 64 B／1 alloc | 4.449 µs; 7.37 GB/s; 0 B／0 alloc | Latency down **2.1%**; throughput up **2.2%** |

### Three-round ranges

| Benchmark | Mihomo 1.19.30 range | Aster range |
| --- | ---: | ---: |
| UDP packet metadata | 69.74–72.12 ns/op | 12.84–13.00 ns/op |
| Disabled debug log | 223.6–231.5 ns/op | 2.230–2.270 ns/op |
| AnyTLS frame (1 KiB) | 71.35–71.69 ns/op | 35.76–36.89 ns/op |
| AnyTLS frame (16 KiB) | 264.9–271.8 ns/op | 195.2–216.4 ns/op |
| TCP relay (32 KiB) | 4.538–4.576 µs/op | 4.423–4.505 µs/op |

This 5825U soft router is still stronger than many MT7621, low-end ARM, or cheap VPS hosts. These results only prove the optimizations still work on real OpenWrt. Weaker devices will have lower absolute GB/s, and the improvement ratio must be remeasured on that device.

## Hyper-V low-resource simulation (secondary result)

To get closer to weaker hardware most users have, we added another resource limit on the same Hyper-V OpenWrt VM. The benchmark was allowed **1 vCPU**, and at most 25 ms of execution every 100 ms, which is **single-core 25% CPU time**. Memory was capped at **512 MiB**, with swap disabled. Rerun on 2026-08-19 against Mihomo `v1.19.30` with the same Linux amd64 test binaries.

This simulates a resource-starved situation. It is not an MT7621 or ARM ISA simulation. It answers “do Aster’s optimizations still exist when CPU is slow and RAM is scarce,” but it does not replace a test on a real low-end router.

| Environment | Actual value |
| --- | --- |
| System | OpenWrt on Hyper-V, Linux 6.6.86, x86-64 |
| Host CPU | Ryzen 7 5825U |
| CPU limit | Pinned to vCPU 0; cgroup `cpu.max = 25000 100000`, i.e. single-core 25% CPU time |
| Guest-reported CPU frequency | Average 2.342 GHz before the unrestricted segment. This is a frequency sample and **does not include the 25% CPU-time limit** |
| Memory limit | 512 MiB, no swap |
| DRAM frequency | Hypervisor did not expose it, **unavailable** |
| Go | 1.26.3, Linux amd64 test binary |
| Load average before the test | 0.07／0.03／0.00 (same start as the unrestricted segment) |
| Load average after the test | 0.31／0.31／0.14 (1／5／15 minutes) |
| Throttle confirmation | 3,660 of 3,709 CPU quota periods were throttled |

Both versions ran the same benchmarks sequentially, three rounds, at least 2 seconds each. The table uses the three-round median:

| Core work | Mihomo 1.19.30 median | Aster median | Aster relative result |
| --- | ---: | ---: | ---: |
| UDP packet metadata | 251.5 ns; 416 B／1 alloc | 48.01 ns; 0 B／0 alloc | **5.24× faster**; removed the 416 B allocation |
| Disabled debug log | 854.3 ns; 24 B／1 alloc | 8.054 ns; 0 B／0 alloc | **about 106× faster**; removed the event allocation |
| AnyTLS frame (1 KiB) | 331.0 ns; 64 B／1 alloc | 139.6 ns; 0 B／0 alloc | **2.37× faster**; latency down 58% |
| AnyTLS frame (16 KiB) | 1.080 µs; 64 B／1 alloc | 790.8 ns; 0 B／0 alloc | **1.37× faster**; latency down 27% |
| TCP relay (32 KiB) | 17.644 µs; 1.86 GB/s; 64 B／1 alloc | 16.812 µs; 1.95 GB/s; 0 B／0 alloc | Latency down **4.7%**; throughput up **5.0%** |

### Peak memory

Keep two “memory” numbers separate:

| Situation | Mihomo 1.19.30 | Aster | Conclusion |
| --- | ---: | ---: | --- |
| Full core, minimal profile, idle 15 s RSS | 34.7 MiB | 39.3 MiB | Aster is 4.6 MiB larger, about **+13%** |

The full-core test used the same profile: Direct mode, no proxies, no rules, silent logs, IPv6 off, TUN off, bound only to unused 127.0.0.1 ports. Aster and Mihomo each ran three rounds, waiting 15 seconds after each start. RSS three-round ranges were Aster 38.5–39.3 MiB and Mihomo 34.7–35.0 MiB. The OpenWrt kernel did not provide `smaps_rollup`, so there is no PSS. Only `/proc/<pid>/status` `VmRSS` is reported.

**So you cannot say Aster uses less RAM when idle.** Aster ships more features and code, and the minimal idle base is currently about 4.6 MiB larger than Mihomo 1.19.30. The isolated-process peaks below are Max RSS of a Go test program that only loaded that package. They are not the total RAM of a full proxy with rules, GeoIP, DNS cache, and live connections.

### Isolated-process peaks for core hot paths

Each case was rerun as an isolated process for three rounds. Max RSS was recorded with Linux `/usr/bin/time -v`. The table uses the three-round median. Both sides used the same single-core 25% CPU time and 512 MiB memory limit.

| Core work | Mihomo 1.19.30 peak RSS | Aster peak RSS | Difference |
| --- | ---: | ---: | ---: |
| TCP relay (32 KiB) | 40.5 MiB | 51.8 MiB | Aster is **28% larger** |
| Disabled debug log | 33.5 MiB | 23.0 MiB | **31% less** |
| UDP packet metadata | 53.0 MiB | 53.4 MiB | Even |
| AnyTLS frame (1 KiB) | 35.4 MiB | 26.0 MiB | **27% less** |
| AnyTLS frame (16 KiB) | 34.8 MiB | 25.5 MiB | **27% less** |

After the 1.19.30 rerun, **do not keep saying all five cases used 30–49% less RAM**. Disabled log and AnyTLS are still clearly lower. UDP is even. The isolated TCP test process peaked higher for Aster. This compares temporary pressure from that package test, not Aster’s RAM under every configuration. The Aster test binary is larger than Mihomo’s, so RSS includes the whole Go test process.

Three-round ranges: TCP Mihomo 39.5–40.5 MiB, Aster 51.5–52.0 MiB; log Mihomo 32.5–33.5 MiB, Aster fixed 23.0 MiB; UDP Mihomo 50.8–53.9 MiB, Aster 53.0–54.9 MiB; AnyTLS 1 KiB Mihomo 35.0–35.5 MiB, Aster fixed 26.0 MiB; AnyTLS 16 KiB Mihomo 34.7–35.1 MiB, Aster 25.5–26.0 MiB. All tests used 0 swap.

### Constrained three-round ranges

| Benchmark | Mihomo 1.19.30 range | Aster range |
| --- | ---: | ---: |
| UDP packet metadata | 244.3–256.4 ns/op | 47.85–48.21 ns/op |
| Disabled debug log | 854.1–856.9 ns/op | 7.984–8.157 ns/op |
| AnyTLS frame (1 KiB) | 323.4–335.5 ns/op | 135.5–155.2 ns/op |
| AnyTLS frame (16 KiB) | 1.075–1.092 µs/op | 772.3–801.6 ns/op |
| TCP relay (32 KiB) | 17.598–17.756 µs/op | 16.771–16.841 µs/op |

Absolute processing time became about four times the unrestricted run, which shows the CPU quota was real. Aster was still faster on all five latency jobs. **In this CPU-quota experiment on the same x86 VM**, TCP improved from 2.1% unrestricted to 4.7%, and AnyTLS 1 KiB from 1.99× to 2.37×. The UDP ratio eased from 5.43× to 5.24×. This cannot be generalized into “every weaker machine improves more”; ARM/MIPS, cache, and memory-bandwidth effects still require native testing. The homepage still uses the unrestricted OpenWrt **TCP about 2%** versus Mihomo 1.19.30, not this more flattering 4.7%, as the main advertised number.

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
| Mihomo 1.19.30 constructs per packet | 69.74–72.12 ns/op | 416 B/op, 1 alloc/op |
| Aster metadata pool | 12.84–13.00 ns/op | 0 B/op, 0 allocs/op |

The object-pool path is about **5.43× faster** on the three-round median and removes a 416-byte heap allocation per packet.

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

Background load is still not low, so the 5900X run is **not** the homepage’s primary comparison. It is only here to confirm the desktop development machine did not regress. An earlier same-day pass with about 42–47% background load and no interleaving produced contradictory TCP numbers and was discarded. After the fixes landed, an interleaved rerun showed:

| Core work | Mihomo 1.19.30 median | Aster median | Aster relative result |
| --- | ---: | ---: | ---: |
| UDP packet metadata | 152.4 ns; 416 B／1 alloc | 11.91 ns; 0 B／0 alloc | **12.8× faster**; removed the 416 B allocation |
| Disabled debug log | 455.3 ns; 24 B／1 alloc | 2.268 ns; 0 B／0 alloc | **201× faster**; removed the event allocation |
| AnyTLS frame (1 KiB) | 74.99 ns; 64 B／1 alloc | 34.70 ns; 0 B／0 alloc | **2.16× faster** |
| AnyTLS frame (16 KiB) | 260.0 ns; 64 B／1 alloc | 184.0 ns; 0 B／0 alloc | **1.41× faster** |
| AnyTLS frame (64 KiB) | no matching bench | 1.056 µs; 0 B／0 alloc | The isolated `WriteDataFrame/65536` fixture is zero-alloc |
| TCP relay (32 KiB) | 10.044 µs; 3.26 GB/s; 64 B／1 alloc | 11.436 µs; 2.87 GB/s; 0 B／0 alloc | Ranges overlap; Aster’s median was slower this pass, **no 5900X TCP speedup claim** |

Three-round ranges: UDP Mihomo 145.8–155.1 ns, Aster pool 11.69–12.21 ns; log Mihomo 442.9–456.5 ns, Aster 2.227–2.352 ns; AnyTLS 1 KiB Mihomo 74.12–77.73 ns, Aster 33.21–36.36 ns; AnyTLS 16 KiB Mihomo 257.9–271.6 ns, Aster 182.9–200.4 ns; TCP Mihomo 9.963–11.705 µs, Aster 10.394–11.947 µs. The 32 KiB `Relay32KiBComparison` median was Aster 9.332 µs versus Mihomo 10.775 µs. Desktop TCP is noisy under background load; the homepage still uses OpenWrt.

## How to rerun

Run the current Aster hot-path suite from the repository root:

```sh
GOAMD64=v1 GOMAXPROCS=1 go test \
  ./component/kerneldirect ./component/nat ./component/trie ./component/cidr \
  ./component/aster ./listener/sing ./tunnel ./tunnel/statistic \
  -run '^$' \
  -bench 'Benchmark(ObserveFlowRefresh|WriteBackProxyUpdate|TableExistingFlow|DomainSetHas|IpCidrSetMergedMiss|ManagerGetUser|ManagerPushUploaded|TCPTrackerLifecycle|MatchDefaultRule|PacketMetadata)$' \
  -benchmem \
  -benchtime=2s \
  -count=7 \
  -cpu=1
```

For a commit-to-commit A/B, build each revision with `go test -c`, launch a fresh test process for every sample, and alternate whether before or after runs first. Do not mix compilation into a still-rebuilding `go test` timing, and do not compare one-off samples from different machines.

The older Aster-versus-Mihomo suite is:

```sh
go test \
  ./common/net ./component/nat ./constant ./listener/sing ./log \
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
- The isolated `WriteDataFrame` fixture is zero-allocation for its 1 KiB, 16 KiB, and 64 KiB cases (64 KiB uses the 128 KiB pool). This does not mean every complete AnyTLS-session path is zero-allocation. Larger or misaligned buffers may still allocate.
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
