# Traffic control and reports

Aster can account upload and download for traffic the core handled, LAN devices, matched routing rules, proxy nodes, and policy groups, limit aggregate bandwidth, and apply rolling quotas. Nikki users should prefer the LuCI “Traffic Control” page. Nikki injects UCI settings into the runtime profile and updates source addresses from the neighbor table by MAC.

Nikki firewall-bypass traffic that never enters Aster is not counted or rate-limited.

## Minimal configuration

```yaml
traffic-control:
  enabled: true
  store: traffic-control.db
  checkpoint-interval: 5m
  max-store-size: 67108864
  portal:
    listen: 0.0.0.0:7893
    url: http://192.168.1.1:7893
  reports:
    enabled: true
    hourly-retention: 31d
    daily-retention: 397d
    monthly-retention: 397d
    orphan-retention: 90d
  global:
    id: global
    enabled: true
    download-bps: 100000000
    quota:
      total-bytes: 107374182400
      window: 30d
      overage-upload-bps: 64000
      overage-download-bps: 256000
      portal: true
```

Rate units are bit/s and traffic units are bytes. `0` or omitting a field means no limit. Quota windows support `m`, `h`, `d`, and `w`, from one hour to 365 days.

## Devices, rules, and proxies

```yaml
traffic-control:
  enabled: true
  devices:
    - id: phone
      name: Phone
      mac: aa:bb:cc:dd:ee:ff
      source-cidrs: [192.168.1.20/32]
      download-bps: 20000000
  rules:
    - id: video-rule
      type: DOMAIN-SUFFIX
      payload: example.video
      target: Proxy
      quota:
        total-bytes: 10737418240
        window: 7d
  targets:
    - id: proxy-group
      kind: group
      target: Proxy
      download-bps: 50000000
```

Rules produce a content signature from type, normalized matcher, and target policy, so reordering rules does not lose history. Changing rule content or a quota window creates a new generation.

`mac` can remain a stable device identity for Nikki or another manager, but the core does not perform a neighbor lookup for every flow and currently has no ingress MAC attribution. Every device policy must include `source-cidrs`; MAC-only configuration is rejected instead of being accepted but never matching.

## Persistence and compression

Governance state and reports live in bbolt. Ordinary traffic is checkpointed every five minutes. Overage, reset, configuration changes, and a clean shutdown commit immediately. Each policy or report series is stored as a Zstandard blob with original length and CRC32C. Startup loads and decodes the current data, and report queries filter the in-memory hour/day/month maps. This is not query-time lazy segmented storage.

The primary database creates a verified backup and can recover automatically when it is damaged. The Nikki package adds the database and backup to the sysupgrade keep list.

## Controller API

Every endpoint uses the standard Controller Bearer secret:

- `GET /api/aster/capabilities`
- `GET /api/aster/kernel-direct/status`
- `GET /api/aster/traffic-control/status`
- `GET/PUT /api/aster/traffic-control/policies`
- `GET /api/aster/traffic-control/rules`
- `POST /api/aster/traffic-control/policies/{id}/reset`
- `GET /api/aster/traffic-control/reports`
- `GET /api/aster/traffic-control/reports/summary`
- `GET /api/aster/traffic-control/reports/export.csv`

The `kernel_direct` object from `GET /api/aster/capabilities` currently uses schema version `4`. `deprecated_fields` includes `proxy_traffic`.

Besides the existing `backend` / `fast_paths`, `GET /api/aster/kernel-direct/status` also returns:

- `learned_sets`: snapshot per kernel-direct consumer, including `max_entries`, `max_records` (domain budget, usually `max_entries × 4`), `learned_addresses`, `direct_addresses`, `proxy_addresses`, `learned_domains`, `evictions`.
- `process`: controller process `pid` and `started_at` (Unix seconds).
- `aster_traffic`: estimate of all traffic Aster currently handles, including TUN DIRECT / default-tun fallback, excluding DIRECT already bypassed by kernel-direct.
- `proxy_traffic`: deprecated alias with the same content as `aster_traffic`. It is not “proxy-only traffic.”

`learned_sets[].evictions` is how many address-LRU entries were dropped because of the capacity cap since process start. TTL expiry, rule-reload flush, or set collapse are not counted.

`GET /api/aster/traffic-control/policies` returns `{revision, config}`. PUT must send the same envelope; a stale revision returns 409. Controller JSON bodies are limited to 1 MiB. A failed Configure/PUT leaves the previous runtime, revision, store, flusher, and portal in place. The same store path and portal listen address are reused, and live sessions rebind to the new generation. Policy count is capped at 256, each session at 128 report keys, and the global report-series table at 4,096.

Report queries accept `key`, `granularity=hour|day|month`, and `from` / `to` Unix timestamps. The range must be positive and no longer than 400 days. An unknown key returns `buckets: []`; an invalid granularity returns 400 even when the key is absent. Cross-dimension keys are joined with `|`, for example `device:phone|rule:video-rule`.
