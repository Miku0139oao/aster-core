# 流量治理與報表

Aster 可以針對所有經核心處理的流量、LAN 裝置、命中的路由規則、代理節點與策略組，統計上下行、限制聚合頻寬，並套用滾動流量配額。Nikki 使用者應優先從 LuCI 的「Traffic Control」頁管理；Nikki 會把 UCI 設定注入 runtime profile，並依 MAC 從 neighbor table 更新來源位址。

不經 Aster 的 Nikki firewall bypass 流量不會被計算或限速。

## 最小設定

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

速率單位是 bit/s，流量單位是 bytes；`0` 或省略代表不限制。配額窗口支援 `m`、`h`、`d`、`w`，範圍為一小時至 365 天。

## 裝置、規則與代理

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

規則以類型、正規化 matcher 與目標策略產生內容簽名，因此重排規則不會丟失歷史。修改規則內容或配額窗口會建立新的 generation。

`mac` 可作為 Nikki／管理端的穩定裝置識別，但核心目前不會在每條 flow 執行 neighbor lookup，也沒有 ingress MAC attribution。每個 device policy 都必須帶 `source-cidrs`；MAC-only 設定會被拒絕，避免看似成功卻永遠不命中。

## 持久化與壓縮

治理狀態與報表保存在 bbolt。一般流量每五分鐘 checkpoint；超額、重置、設定變更與正常關閉會立即提交。每個 policy／report series 以帶原始長度與 CRC32C 的 Zstandard blob 保存；啟動時會載入並解碼目前資料，報表查詢再從記憶體中的 hour/day/month map 篩選。這不是 query-time lazy segment storage。

主資料庫會建立驗證備份；損壞時可自動恢復。Nikki package 會把資料庫與備份加入 sysupgrade 保留清單。

## Controller API

所有端點使用標準 Controller Bearer secret：

- `GET /api/aster/capabilities`
- `GET /api/aster/kernel-direct/status`
- `GET /api/aster/traffic-control/status`
- `GET/PUT /api/aster/traffic-control/policies`
- `GET /api/aster/traffic-control/rules`
- `POST /api/aster/traffic-control/policies/{id}/reset`
- `GET /api/aster/traffic-control/reports`
- `GET /api/aster/traffic-control/reports/summary`
- `GET /api/aster/traffic-control/reports/export.csv`

`GET /api/aster/capabilities` 的 `kernel_direct` 目前 schema version 為 `4`；`deprecated_fields` 含 `proxy_traffic`。

`GET /api/aster/kernel-direct/status` 除既有的 `backend`／`fast_paths` 外還回傳：

- `learned_sets`：各 kernel-direct consumer 的 snapshot，含 `max_entries`、`max_records`（domain budget，通常為 `max_entries × 4`）、`learned_addresses`、`direct_addresses`、`proxy_addresses`、`learned_domains`、`evictions`。
- `process`：controller 行程 `pid` 與 `started_at`（Unix 秒）。
- `aster_traffic`：目前所有由 Aster 處理的流量估計，包含 TUN DIRECT／default-tun fallback，不含已被 kernel-direct 繞過的 DIRECT。
- `proxy_traffic`：已棄用別名，內容與 `aster_traffic` 完全相同，不是「僅代理流量」。

`learned_sets[].evictions` 是行程啟動以來 address LRU 因容量上限淘汰的次數；TTL 到期、規則 reload flush 或 set collapse 不計入。

`GET /api/aster/traffic-control/policies` 回傳 `{revision, config}`；PUT 必須把同一 envelope 送回，revision 過期時回 409。Controller JSON body 上限為 1 MiB。失敗的 Configure／PUT 會保留先前的 runtime、revision、store、flusher 與 portal；相同 store path 與 portal listen 會被重用，活躍 session 會切到新 generation。政策數量上限 256，每個 session 最多 128 個 report key，全域 report series 上限 4,096。

報表查詢接受 `key`、`granularity=hour|day|month`、`from` 與 `to` Unix timestamp；範圍必須為正且最多 400 天。未知 key 回傳 `buckets: []`，無效 granularity 即使 key 不存在也會回 400。交叉維度 key 使用 `|` 連接，例如 `device:phone|rule:video-rule`。
