# Memory optimization handoff plan

::: info English summary
This page summarizes the handoff plan. The detailed evidence, file locations, risks, and acceptance criteria are in the [full Traditional Chinese plan](/development/memory-optimization-plan).
:::

## Changes already merged

[PR #4](https://github.com/Miku0139oao/aster-core/pull/4) includes:

- Bounded channel pools for Aster's 16–128 KiB allocator slabs, limiting retained pool entries after connection bursts.
- Compact DNS cache entries for eligible A/AAAA-only replies using `netip.Addr` values. Replies requiring full DNS message semantics retain the full-message representation.

These changes do not by themselves establish a measured reduction in whole-process resident memory. Aster's allocator and the dependency's `sing` buffer pool are separate.

## September 7 validation update

The [performance report](/en/reference/performance) now includes Windows/Linux verification and seven-round WSL2 A/B results. Median RSS with 4,096 DNS cache entries fell 2.5%, with overlapping ranges; 1,000 TCP connections did not use less RAM. Large-pool Get/Put and full DNS-message cache hits became slower; full-message hit allocations rose from 252 B / 5 allocs to 516 B / 10 allocs. Natural-GC TCP heap profiles were insufficient to complete P0-1 attribution. The planned items below remain unaccepted.

## Priorities

1. Attribute idle TCP memory with heap profiles for SOCKS-to-DIRECT and SOCKS-to-TLS-proxy scenarios before changing relay buffers. Preserve splice, read-waiter, protocol headroom, counters, and connection lifecycle behavior.
2. Investigate right-sizing small queued UDP packets instead of retaining a full receive slab for each packet.
3. Measure QUIC receive-window pressure before considering smaller defaults for `with_low_memory`; investigate connection-panel snapshot allocations.
4. Measure LRU overhead, geodata startup GC costs, and tracker metadata retention before choosing optimizations.
5. Investigate slow log subscribers as a robustness issue; observe provider-loading memory peaks without changing behavior first.

These are planned investigations, not completed optimizations.

## Measurement and safety rules

- Use fresh processes and seven interleaved runs for before/after comparisons. Report medians and ranges.
- Measure Windows working set/private bytes or Linux RSS/high-water mark separately; do not treat them as interchangeable or extrapolate RSS from benchmark B/op.
- Collect heap profiles to establish allocation ownership. Include relevant benchmarks and correctness tests.
- Do not tune `GOGC` or `GOMEMLIMIT`, add a periodic memory reclaimer, or force GC to improve reported numbers. Without explicit user approval, retain the current no-GC-intervention policy.
- Preserve cache capacities, TTLs, stale DNS behavior, fake-IP reverse lookup, protocol semantics, and public JSON fields.
- Keep each optimization in a separate PR, with evidence and regression results.

## Verification checklist

- Build normal and `with_low_memory` variants.
- Run the full Go test suite and race tests for changed packages.
- Run formatting checks, `go vet`, and `golangci-lint`.
- Compare relevant benchmarks with seven runs and `-cpu=1`.
- Record whole-process measurements and heap attribution before claiming memory savings.

See the [performance reference](/en/reference/performance) for existing measurements and the [full plan](/development/memory-optimization-plan) for per-item acceptance criteria.
