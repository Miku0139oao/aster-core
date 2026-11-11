# 效能稽核與實作紀錄（2026-11-05）

基準：`main` @ `81cc83f6` 起（稽核期間 main 推進到 `47961b16`），實作分支
`perf/audit-tier-a-20261105`，以 `main` 為基底。完整候選清單與否決理由見
`docs/reference/perf-audit-20261105.md`。

## 已實作（每個項目一個 commit，均可獨立回退）

| Commit | 變更 | 量測 |
| --- | --- | --- |
| `4417bab2` | NAT last-hit 快取 64→4096 槽、改 heap slice；`canonicalRuleType` 的 `strings.NewReplacer` 提升為套件變數 | NAT 256 流 67→19 ns、1024 流 81→21 ns；SessionOpen/reports 252→196 ns、7→3 allocs |
| `ada9fd58` | tproxy 每 datagram 只建一次 `*net.UDPAddr`（原本 4 次） | 4 次查詢 0.25–0.43 ns、0 allocs |
| `f4cd4970` | UDP rawRead 把 `UDPAddr` 與 IP 合併為單一配置 | 4→3 allocs/op |
| `bf4c8ab1` | fake-IP 反查先比對網段，避免每封包取 pool mutex | 並行真實位址 163–178→1.3 ns |
| `da627db3` | VLESS UDP 每 datagram 單次寫入（原本 2 次） | 真實 TLS loopback 51–73 µs→25–37 µs |
| `8cecb230` | Hysteria v1 `Unpack` 改為邊界檢查直讀 | 201–310 ns/10 allocs → 27–34 ns/1 alloc |
| `a03d50cb` | UDP worker 分片只 hash 來源 `AddrPort`（Raw 路徑維持全鍵） | 37.8→12.1 ns |
| `aec9aca0` | Aster 管理快照預先配置 Users 容量 | 10.94 MB→3.44 MB/op（10k 使用者） |
| `535430dd` | `/connections` 葉狀欄位改用 `AppendText` | 10k 連線 120,074→110,074 allocs/op |

行為不變的保證方式：每個改動都有釘住語意的測試。Hysteria 的
`TestUDPMessageUnpackMatchesReference` 以舊實作為對照，對每個截斷前綴、
單一位元組變異與 200 組決定性偽隨機輸入逐項比對錯誤文字與解析結果；VLESS 以
`TestServerPacketConnWriteToSingleWrite` 斷言每次 datagram 恰好一次寫入；
JSON 以 `TestAppendTextMatchesMarshalJSON` 與 `TestSnapshotJSONShape` 釘住
wire 格式（含 `dnsMode` 現存的雙重引號行為，刻意保留）。

## 量測到但**未採用**（重要，避免重做）

- **kerneldirect 跳過 IPSet 重建**：報告聲稱「新 host 帶相同 decision 位可跳過
  全表重建，4096 筆 2.63 ms→1.7 µs」。實測不成立：單一位址產生的是 /32，
  新位址必然產生新前綴，重建無法避免。加入覆蓋範圍快取後
  seed256 從 144–204 µs 退化為 259–499 µs，且記憶體增加。已完全回退。
- **VLESS 單次寫入在純記憶體 conn 上較慢**（12–17 ns→125–150 ns），因為多了
  一次複製。只有在 TLS 這類第二次寫入真正昂貴的傳輸上才划算；已於 commit
  訊息中明確記錄此代價。
- **tunnel 統計 `Snapshot()` 預配置切片**：`xsync.Map` 沒有 `Size()`，可靠的
  大小取得需要全表掃描，成本高於 append 成長。未採用。

## 驗證

- `go test -count=1` 涵蓋所有修改套件，全數通過。
- `go build ./...`、`-tags with_low_memory`、`CGO_ENABLED=0 GOOS=linux` 均通過。
- `go vet` 涵蓋所有修改套件；`gofmt -l` 對所有變更 .go 檔為空。
- `-race` 在 Windows 本機不可用（缺 gcc），需在 Linux CI 執行。
