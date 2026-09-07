# 記憶體優化規劃書（交接用）

> 本文件是給接手 agent 的工作規劃。每一項都附上檔案位置、目前的證據、建議作法與驗收標準。
> 接手時請依優先序逐項開獨立 PR，不要把多個項目混在一個 PR 內。

## 0. 現況與已完成事項

| 項目 | 狀態 | 位置 |
| --- | --- | --- |
| 16 KiB–128 KiB 配置器 slab 改為有上限的 `sync.Pool`（先前 channel 的 Get/Put 回退已修正） | 已完成，[PR #4](https://github.com/Miku0139oao/aster-core/pull/4) 與後續修正 | `common/pool/alloc.go`、`common/pool/alloc_cap*.go` |
| DNS cache 純 A/AAAA 回應改存 `[]netip.Addr`；compact hit 只 expand 一次 | 已完成，[PR #4](https://github.com/Miku0139oao/aster-core/pull/4) 與後續修正 | `dns/msg_cache.go`、`dns/resolver.go`、`dns/util.go` |
| UDP NAT 表上限（8192／低記憶體 2048） | 既有 | `component/nat/table.go` |
| `bytes.Buffer` 超過 128 KiB 不回全域 pool | 既有 | `common/pool/buffer.go` |
| 低記憶體 build tag `with_low_memory` 縮小 relay／UDP buffer | 既有 | `common/pool/buffer_low_memory.go` |
| 閒置後 opt-in `runtime.GC()`（P0-0 (B)，預設關閉；不強制 FreeOSMemory） | 已完成 | `tunnel/statistic/scavenge.go`、`experimental.idle-memory-scavenge` |

`docs/reference/performance.md` 的 2026-09-05 整程序 A/B 已指出目前最大的缺口：

- 空載 working set 約 19 MiB；**持有 1,000 條 TCP 時為 113–115 MiB**，釋放後 60 秒仍為 122 MiB。
- 亦即每條閒置 TCP 連線約佔 **85–95 KiB**，而且釋放後不回落。

本輪在 `common/net` 用 1,000 對 `net.Pipe` 走 `N.Relay` 的臨時探針（探針檔已移除，未提交）量到：

```
goroutines=2002
HeapInuse: 640 -> 68832 KiB (+68192 KiB, 68.2 KiB/conn)
StackInuse: 544 -> 8544 KiB (8.0 KiB/conn)
```

`net.Pipe` 不是 `ExtendedConn` 也不是 `syscall.Conn`，所以這個探針只量到 `copyConn` 的**備援路徑**：兩個方向各持有一塊 32 KiB 的 `pool.RelayBufferSize` buffer，並在整條連線閒置期間不放手。實際 tunnel 路徑走的是 sing 的 `bufio.Copy`，各分支的閒置成本不同，詳見 P0-1。**歸因並削減每條連線的常駐成本是最高優先級。**

## 1. 量測方法（每個項目改動前後都要跑）

1. **整程序情境**：沿用 `docs/reference/performance.md` 的最小設定（Rule 模式 + `MATCH,DIRECT`、DNS／TUN 關閉、loopback SOCKS），情境至少包含：空載、持有 100／1,000 條 TCP、釋放後 60 秒、1,000 個 UDP flow 各 queue 若干封包。每情境七輪、新程序、前後交錯。Linux 取 `/proc/<pid>/status` 的 `VmRSS`／`VmHWM`，Windows 取 working set 與 private bytes；兩者不可互換比較。
2. **Heap 歸因**：用 `hub/route` 的 `/debug/pprof/heap`（需 `external-controller` 開 debug）在「持有 1,000 條 TCP」時抓 `inuse_space`，以 `go tool pprof -top` 確認 top 3 符號。這是驗收「錢花在哪」的依據，不是用來取代整程序數字。
3. **Microbenchmark**：`go test -run '^$' -bench ... -benchmem -count=7 -cpu=1`，每個 case 用 `go test -c` 建 test binary、新程序執行，見 `docs/reference/performance.md` 的「如何重跑」。
4. **禁止事項**：不設定 `GOGC`／`GOMEMLIMIT`、不為了美化數字而呼叫 `runtime.GC()`／`debug.FreeOSMemory()`。唯一例外：(1) 既有手動 `PUT /debug/gc`；(2) 預設關閉的 `experimental.idle-memory-scavenge`，且只跑一次 `runtime.GC()`，不強制 `FreeOSMemory`。不能拿 B/op 的下降直接宣稱 RSS 下降。

## 2. 工作項目

### P0-0　決策項：閒置後 heap 不回落是 Go runtime 行為 — **已裁決 (B)，已實作**

- **裁決**：使用者接受並授權 opt-in 閒置回收器（2026-09-06）。
- **實作**：`experimental.idle-memory-scavenge`（預設 `false`）+ `experimental.idle-memory-scavenge-idle`（秒，`0` 視為 300）。所有 tracker 關閉並閒置期滿後跑一次 `runtime.GC()`，把死物件變成 idle span，由背景 scavenger 還給 OS。**不**呼叫 `debug.FreeOSMemory()`，避免 heap lock 造成數毫秒的配置停頓。每個忙碌週期最多一次；啟動時空載不觸發。`handle()` 以 goroutine 呼叫。程式在 `tunnel/statistic/scavenge.go`。
- **GC 安全性**：只回收不可達物件。Go 做不到零 STW；剩下的是一般 GC 的極短暫停，不是「立刻還完全部頁面」。若要立刻還給 OS，用手動 `PUT /debug/gc`。
- **未採用**：HeapInuse < RSS/2 作為觸發條件。關閉連線後若尚未 GC，HeapInuse 仍高，該條件會剛好跳過需要回收的情境。
- **未採用**：預設開啟或 `with_low_memory` 自動開啟。維持預設關閉。
- **未重跑**：2026-09-05 七輪整程序 working-set A/B。

### P0-1　每條 TCP 連線的常駐成本：先歸因，再削減閒置時持有的 relay buffer

- **檔案**：`common/net/sing.go`（`copyConn`、`Relay`）、`context/conn.go`（inbound 一律包成 `N.BufferedConn`）、`constant/adapters.go`（`C.Conn` 內嵌 `N.ExtendedConn`）、`tunnel/statistic/tracker.go`、`common/pool/buffer_*.go`；相依：`github.com/metacubex/sing/common/bufio/copy.go`、`copy_direct*.go`、`common/buf/buffer_standard.go`（sing `BufferSize = 32 KiB`）。
- **實際路徑分析**（接手者請先讀懂再動手）：
  1. tunnel 內 inbound 是 `*N.BufferedConn`（`ExtendedConn` + `CachedReader` + `ReaderPossiblyReplaceable`），outbound 是 `C.Conn`（`ExtendedConn`），所以 `copyConn` 的 `if sourceExtended || ...` 幾乎必定成立，走 sing `bufio.Copy`；`pool.Get(pool.RelayBufferSize)` 的備援迴圈只有在兩端都是裸 `net.Conn` 時才會用到（例如測試、少數 listener 直接 relay）。
  2. sing `bufio.Copy` 分三支：(a) 兩端解包後都是 `syscall.Conn`（DIRECT TCP↔TCP）→ `copyDirect` 在 Linux 用 splice，**不持有使用者空間 buffer**；(b) 來源是 `syscall.Conn` 但目的不是（DIRECT 回程寫進 TLS 等）→ `syscallReadWaiter`，用 `RawConn.Read` 等可讀後才 `NewBuffer()`，**閒置時 heap 接近 0**，posix 與 Windows 都有實作；(c) 來源不是 `syscall.Conn`（TLS、VMess、Trojan、Hysteria、AnyTLS 等所有代理 outbound 的下行方向；以及 inbound 若是 TLS listener）→ `CopyExtendedWithPool`，每圈 `options.NewBuffer()` 後**在 `ReadBuffer` 阻塞期間持有一塊 32 KiB sing buffer**（低記憶體 tag 為 16 KiB）。
  3. 因此「代理節點下行方向」這個最主要的使用情境，每條閒置連線至少持有 32 KiB sing buffer，再加上 `crypto/tls` 自身每條連線的讀寫 record buffer（約 16–34 KiB）。1,000 條走 TLS 代理的閒置連線就是 32–66 MiB。這與 `docs/reference/performance.md` 的 SOCKS→DIRECT 情境不同；該情境的 85–95 KiB/conn 來源尚未歸因（見下方第一個任務）。
  4. sing 的 `buf` pool 與 Aster 的 `common/pool` 一樣是無上限 `sync.Pool`；[PR #4](https://github.com/Miku0139oao/aster-core/pull/4) 只替 Aster 自己的 16 KiB+ 級距加了上限，sing 那份沒有。
- **任務**：
  1. **歸因**：在 Linux 建兩個 1,000 條閒置連線情境並抓 `inuse_space` heap profile：(i) SOCKS→DIRECT loopback echo（對應現有文件數字），(ii) SOCKS→loopback TLS 代理（trojan 或 vmess+tls 服務端）。把 top 5 符號與每條連線的 bytes 寫進 PR，確認 (i) 的 85–95 KiB 究竟是 goroutine stack、`BufferedConn` 的 bufio、tracker、sync.Pool 殘留還是 heap 碎片。**沒有這步不得宣稱任何項目「省了多少」。仍未完成。**
  2. **備援路徑（已完成）：** `copyConn` 從 4 KiB 起跳，連續 2 次滿讀升級、連續 4 次 `readN < len/4` 降級。256 對閒置 `net.Pipe` heap 約 12 KiB/conn（先前約 68 KiB）。見 `common/net/copy_adaptive.go`。
  3. **sing (c) 分支（部分完成）：** 來源若是 generic byte stream（TLS／pipe／`ExtendedReaderWrapper`），`CopyExtendedWithPool` 同等路徑改為自適應並保留 headroom。VMess 等自訂 `ReadBuffer`（可能先讀 length 再要求整塊）仍用 `RelayBufferSize`，避免 `ErrShortBuffer` 已消耗 prefix。splice 與 `syscall.Conn` readWaiter 仍走 sing。整程序「1,000 條閒置 TLS 代理」尚未重跑。
  4. 保留現有的 `readCounters`／`writeCounters`、`ReportHandshakeFailure`、`closeWrite`、`ReadCached` 語意；splice 與 readWaiter 分支不要動。
- **驗收標準**：
  - 歸因報告（任務 1）附在 PR 內，兩個情境都有 heap top 5 與 per-conn bytes。
  - `net.Pipe` 探針（1,000 對閒置 relay）heap 由 68 KiB/conn 降到 **≤ 16 KiB/conn**（任務 2）。
  - 新增「1,000 條閒置 TLS 代理連線」整程序情境；任務 3 完成後 working set 中位數下降且七輪範圍不重疊。
  - 既有 `Relay32KiBComparison`（`common/net/relay_comparison_test.go`）與 tunnel relay benchmark 吞吐回退不超過 3%，維持 0 allocs/op 的 case 仍為 0。
  - 新增測試：小流量連線不升級、持續滿讀連線在數次 read 內升級、降級後 buffer 正確歸還、headroom 協定（至少 VMess 與 Shadowsocks）在小 buffer 下 round-trip 正確。
- **風險**：升級門檻太保守會讓下載初期吞吐下降，須以持續 32 KiB 寫入的 benchmark 佐證升級在少數幾圈內完成；任務 3 若某協定的 `ReadBuffer` 假設 buffer 容量 ≥ MTU，會直接讀壞資料，必須有 round-trip 測試。

### P0-2　每個排隊中的 UDP 封包都釘住一塊 16 KiB slab

- **檔案**：`common/net/packet/packet.go`（`waitReadFrom`、`enhanceUDPConn.WaitReadFrom`）、`common/net/packet/packet_posix.go`、`listener/tproxy/udp.go`、`listener/tproxy/packet.go`、`listener/tunnel/udp.go`、`tunnel/tunnel.go`（`queueCapacity=64`、`senderCapacity=128`）、`tunnel/connection.go`（`packetSender.ch`）。
- **問題**：讀取 UDP 時固定 `pool.Get(pool.UDPBufferSize)`（16 KiB，低記憶體 8 KiB），封包實際大小常見只有 60–1,400 bytes。封包進入 `packetSender.ch`（每個 NAT entry 128 格）與 worker queue（每 worker 64 格）排隊期間，整塊 16 KiB 都被保留。理論上限是 NAT 8192 entries × 128 × 16 KiB，遠超任何實機 RAM；實務上一個目的端卡住的 UDP flow 就能釘住 2 MiB。
- **建議作法**：
  1. 讀完後若 `readN <= 某門檻（例如 2 KiB）`，`pool.Get(readN)` 複製一份、立刻歸還 16 KiB 讀取 buffer；`put` 改釘小塊。複製幾百 bytes 的成本遠低於釘住 16 KiB。門檻與是否複製要用 `BenchmarkPacketMetadata`／`handlePacket` 類 benchmark 確認 ns/op 與 allocs 不惡化。
  2. 另行評估全域「在途 UDP bytes 上限」（例如 4 MiB／低記憶體 1 MiB），超限時在 `Send` 直接 `Drop()`，並在 `udpNATAdmissionDrops` 之外增加計數器暴露到 `/memory` 或 log。
- **驗收標準**：
  - 新增 benchmark／test：1,000 個 flow、每 flow 排隊 32 個 200-byte 封包時，heap inuse 由約 500 MiB 級降到 ≤ 20 MiB 級（實際數字在實作時量測寫入 PR）。
  - `listener/sing` 的 `handlePacket` 與 `tunnel` 的 UDP benchmark ns/op 變動在 ±5% 內。
- **風險**：`sing` 路徑（`listener/sing/sing.go` 的 `WaitReadPacket`）用的是 sing 的 `buf.Buffer` pool，不在此改動範圍；改動前先確認哪些 listener 真的走 `common/net/packet`。

### P1-3　QUIC 接收視窗預設值未隨 `with_low_memory` 縮小

- **檔案**：`transport/tuic/common/congestion.go`（`DefaultStreamReceiveWindow = 15 MiB`、`DefaultConnectionReceiveWindow = 64 MiB`）、`adapter/outbound/tuic.go`、`adapter/outbound/hysteria.go`、`adapter/outbound/shadowquic.go`、`listener/tuic/server.go`、`listener/shadowquic/server.go`、`adapter/outbound/hysteria2.go`。
- **問題**：這些預設值決定 quic-go 每條連線最多可緩衝多少未讀資料。單一 TUIC／Hysteria 連線在高 RTT、接收端消費慢時最多可佔 64 MiB，且完全不受 `with_low_memory` 影響。Hysteria2 在未設定時交給 sing-quic 的預設，行為需查證。
- **建議作法**：
  1. 先量測：在 loopback 上對 TUIC 與 Hysteria 做「接收端刻意慢讀」情境，抓 heap 確認 quic-go 緩衝真的成長到接近視窗。
  2. 若確認，為 `with_low_memory` 提供較小預設（例如 stream 2 MiB／connection 8 MiB），一般 build 維持不變；設定檔已提供 `recv-window`／`recv-window-conn` 覆寫。
  3. 在 `docs/reference/performance.md` 與 `docs/tutorials` 說明低記憶體 build 的預設變更與吞吐取捨。
- **驗收標準**：低記憶體 build 的慢讀情境 heap 上限明顯下降；一般 build 的吞吐 benchmark 無變化（因為未改動）。
- **風險**：縮小視窗會直接限制高 RTT 下的單流吞吐，所以只做在 `with_low_memory`，且必須寫進文件。

### P1-4　連線面板 WebSocket 每秒完整序列化所有連線

- **檔案**：`hub/route/connections.go`（`getConnections`、`sendSnapshot`）、`tunnel/statistic/manager.go`（`Snapshot`）、`tunnel/statistic/tracker.go`（`TrackerInfo`）。
- **問題**：每個 WS 客戶端每 `interval`（預設 1 秒）呼叫 `Manager.Snapshot()`，`connections` slice 從 nil 開始 `append` 成長，再用 `json.NewEncoder` 序列化每條連線的完整 `Metadata`（20 多個欄位，含 `SrcGeoIP`／`DstGeoIP` slice 與多個字串）。10,000 條連線時每秒產生數 MB 垃圾，推高 GC 頻率與 heap 峰值；`bytes.Buffer` 會長到最大快照大小並在 WS 存活期間保留。
- **建議作法**：
  1. `Snapshot()` 用 `m.connections.Size()` 預先配置 slice 容量（一行改動）。
  2. 為 `TrackerInfo` 實作 `MarshalJSON` 或改用 `json.Encoder` 直接寫入，避免中間 `Snapshot` 結構；評估 `jsoniter`／`encoding/json/v2` 是否已在依賴中。
  3. 可選：多個 WS 客戶端共用同一份每秒快照（單一 ticker 產生、廣播給所有訂閱者）。
- **驗收標準**：新增 `BenchmarkConnectionsSnapshotJSON`（1,000／10,000 條假 tracker），B/op 與 allocs/op 下降 ≥ 50%；輸出 JSON 與現行完全相容（用 golden test 比對）。

### P2-5　`LruCache` 每筆額外開銷與 stale 模式只靠容量淘汰

- **檔案**：`common/lru/lrucache.go`、使用者：`dns/resolver.go`（`lru.WithStale(true)`）、`dns/enhancer.go`（4096 筆 fake-IP 反查）、`component/fakeip/memory.go`、`component/sniffer/dispatcher.go`。
- **問題**：每筆項目 = `entry{key, value, expires}` + `list.Element`（4 個指標）+ map slot，約 80–100 bytes 額外開銷，且 Go map 刪除後不縮小。DNS 走 `lru` 演算法時 `WithStale(true)` 讓 `maybeDeleteOldest` 完全不做事，過期項目只在達到 `CacheMaxSize` 時才淘汰；這是刻意設計（stale-while-revalidate），但意味 cache 幾乎永遠滿載。
- **建議作法**：先量測 4,096／65,536 筆時的實際 bytes/entry；若開銷 > 30%，評估以索引型 intrusive list（`[]entry` + `int32` prev/next）取代 `generic-list-go`。**不要**改動 stale 語意或降低容量上限（見第 3 節約束）。
- **驗收標準**：`common/lru` benchmark 的 bytes/entry 下降且 `Get`／`Set` ns/op 不變差；所有既有 lru 測試通過。

### P2-6　`memconservative` 每載入一個 GeoIP／GeoSite 清單就強制 `runtime.GC()`

- **檔案**：`component/geodata/memconservative/memc.go`（`defer runtime.GC()` ×2）、`component/geodata/utils.go`。
- **問題**：設定檔有 N 條 `GEOSITE`／`GEOIP` 規則就在啟動時觸發 N 次 full GC。這是 CPU／啟動時間問題而不是常駐記憶體問題，但與專案「不做 GC 調參」的立場衝突，且 `loadGeoSiteMatcherListSF` 已把原始 list 設為不保留，強制 GC 的原始理由可能已不存在。
- **建議作法**：量測 50 條 geosite 規則的啟動時間與 `VmHWM`，比較保留／移除 `runtime.GC()` 兩版；只有在峰值不惡化時才移除。若峰值惡化，改為「整批規則載入完成後 GC 一次」而非每筆一次。
- **驗收標準**：啟動時間下降、`VmHWM` 不上升，兩者皆需七輪中位數。

### P2-7　`TrackerInfo.Metadata` 保留整個 `Metadata` 直到連線結束

- **檔案**：`tunnel/statistic/tracker.go`、`constant/metadata.go`。
- **問題**：每條連線的 `TrackerInfo` 持有 `*C.Metadata`，其中 `RawSrcAddr`／`RawDstAddr net.Addr`、`SrcGeoIP`／`DstGeoIP []string`、`Process`／`ProcessPath` 等在 dial 完成後只剩面板顯示用途。單筆不大（約 300–400 bytes + 字串），但乘以連線數後是固定成本；同時 `Metadata` 是 sync.Pool 物件，被 tracker 長期引用會讓 pool 無法回收。
- **建議作法**：確認 `Metadata` pool 與 tracker 引用關係（是否 tracker 持有的就不歸還 pool），評估在 `NewTCPTracker` 時只複製面板需要的欄位到精簡結構。這項收益較小，排在 P0／P1 之後。
- **驗收標準**：`BenchmarkTCPTrackerLifecycle` B/op 不上升；`/connections` JSON 欄位不變。

### P3-8　log observable 的 `Emit` 在鎖內阻塞

- **檔案**：`common/observable/observable.go`（`process` 在 `mux` 內呼叫 `sub.Emit`）、`common/observable/subscriber.go`（`buffer <- item` 無 `default`）、`log/log.go`（`logCh` 無緩衝）。
- **問題**：任何一個訂閱者 200 格 buffer 滿了，`Emit` 就阻塞並持鎖，連帶 `Subscribe`／`UnSubscribe` 與所有呼叫 `log.*` 的 goroutine 停住。`hub/route/server.go` 的 `getLogs` 已用另一個 goroutine 快速搬到會丟棄的 channel 緩解，但 `observable` 本身仍是阻塞語意。這是健壯性議題，不是記憶體議題。
- **建議作法**：`Emit` 改為非阻塞（滿了就丟並累計 dropped 計數）。需確認沒有測試依賴阻塞語意。
- **驗收標準**：新增測試「慢訂閱者不阻塞 `log.Infoln`」；既有 `log`／`observable` 測試通過。

### P3-9　Provider 載入時的瞬時峰值

- **檔案**：`component/resource/fetcher.go`（`os.ReadFile` → `parser` → `vehicle.Write`）、`rules/provider/mrs_reader.go`、`component/geodata/standard/standard.go`。
- **問題**：載入時同時存在原始 bytes、解碼後結構與寫回檔案的 buffer；`standard` loader 更是把整個 `geosite.dat` 讀進來並 `proto.Unmarshal` 成完整 list。`memconservative` 是預設，`standard` 需使用者自選。這是峰值而非常駐，僅列入觀察。
- **建議作法**：只量測，不改動；在 `docs/reference/performance.md` 記錄「100k 條 MRS／GeoSite 載入時 `VmHWM` 與穩態差距」，供使用者判斷 `standard` 與 `memconservative` 的取捨。

## 3. 約束（接手者不得違反）

1. 不使用 `GOGC`／`GOMEMLIMIT`／`debug.SetMemoryLimit`／忙碌時定時 `FreeOSMemory` 讓數字好看。P0-0 的 opt-in idle scavenge 只允許一次 `runtime.GC()`，且必須預設關閉。
2. 不降低 queue／cache／NAT 上限、不縮短 TTL、不移除功能來換記憶體；`with_low_memory` 可以有更小的預設，一般 build 的預設要有數據支持才能改。
3. 保留 stale DNS、fake-IP 反查、面板 JSON 欄位等對外語意。
4. 每個項目獨立 PR，PR 描述必須附：改動前後整程序數字（含範圍）、heap top 3、相關 benchmark 前後值、`go test ./...`、`go vet`、`gofmt`、`golangci-lint` 結果。
5. 文件數字只能寫實際量到的，不可由 B/op 外推 RSS，也不可拿桌機結果冒充 OpenWrt 結果（見 `docs/reference/performance.md` 既有寫法）。

## 4. 建議執行順序與相依

```
P0-0 已裁決 (B)，idle scavenge 預設關閉 ── 不阻塞 P0-1
P0-1.1 heap 歸因（DIRECT 與 TLS 代理兩情境）──> 決定 P0-1.2／P0-1.3 與 P2-7 是否值得做
P0-1.2 備援路徑自適應 buffer ──┐
P0-1.3 sing (c) 分支自適應    ──┼──> 重跑整程序 A/B，更新 docs/reference/performance.md
P0-2 UDP slab 右尺寸化        ──┤
P1-4 面板快照                 ──┘
P1-3 QUIC 視窗（獨立，可平行）
P2-5 / P2-6 / P2-7（各自獨立，收益需先量測再決定是否做）
P3-8 / P3-9（健壯性與觀察，不影響前面項目）
```

P0-1 完成後，「持有 1,000 條 TCP」與「釋放後 60 秒」兩個情境的數字會改變，P1-4 與 P2-7 的收益要在 P0-1 之後重新量測，避免把 P0-1 的收益算到別的項目上。

## 5. 快速檢查清單（每個 PR 完成前）

- [ ] `go build ./...` 與 `go build -tags with_low_memory ./...` 通過
- [ ] `go test ./...`；改動 package 加跑 `-race`
- [ ] `gofmt -l .` 為空；`go vet ./...`；`golangci-lint run`
- [ ] 相關 benchmark 前後 `-count=7 -cpu=1` 並附 benchstat
- [ ] 整程序情境七輪中位數與範圍寫進 PR 與 `docs/reference/performance.md`
- [ ] 臨時探針、debug log、`runtime.GC()` 呼叫已移除
