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

## 持久化與壓縮

治理狀態與報表保存在 bbolt。一般流量每五分鐘 checkpoint；超額、重置、設定變更與正常關閉會立即提交。封存時間桶使用 Zstandard 分區壓縮，每個區塊有原始長度與 CRC32C，查詢時只解壓需要的區段。

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

`GET /api/aster/capabilities` 的 `kernel_direct` 物件為 version `4`（v4 未發布過，此次直接採用，沒有跳到 5）。`deprecated_fields` 含 `proxy_traffic`。

`GET /api/aster/kernel-direct/status` 除既有的 `backend`／`fast_paths` 外還回傳：

- `learned_sets`：各 kernel-direct consumer 的 snapshot，含 `max_entries`、`max_records`（domain budget，通常為 `max_entries × 4`）、`learned_addresses`、`direct_addresses`、`proxy_addresses`、`learned_domains`、`evictions`。
- `process`：controller 行程 `pid` 與 `started_at`（Unix 秒）。
- `aster_traffic`：目前所有由 Aster 處理的流量估計，包含 TUN DIRECT／default-tun fallback，不含已被 kernel-direct 繞過的 DIRECT。
- `proxy_traffic`：已棄用別名，內容與 `aster_traffic` 完全相同，不是「僅代理流量」。

`learned_sets[].evictions` 是行程啟動以來 address LRU 因容量上限淘汰的次數；TTL 到期、規則 reload flush 或 set collapse 不計入。

報表查詢接受 `key`、`granularity=hour|day|month`、`from` 與 `to` Unix timestamp。交叉維度 key 使用 `|` 連接，例如 `device:phone|rule:video-rule`。
