# Aster Core 效能稽核報告（唯讀）

**稽核範圍**：`D:/vless/aster-core`。任務指定 `main @ 81cc83f6`，但實際 HEAD 為 `e9ec7ae4`（分支 `fix/sing-chunkreader-cache`）；各 lane 執行期間 HEAD 曾推進（81cc83f6 → 369d3a5d → ae889601 → e9ec7ae4），因此部分提案在現行樹上「已落地」，下文逐項標註。
**環境**：Windows 11、Ryzen 9 5900X、go1.27.3、`GOEXPERIMENT=jsonv2` 預設啟用。`ns/op` 在平行負載下不穩，**以 `B/op`、`allocs/op` 為主要訊號**；所有未直接量測的推論均標記「未量測」。
**硬性遵循**：未修改任何檔案；所有實驗在 repo 外的暫存 module 或 `go test -overlay` 完成。

---

## 1. 摘要

- **最高 ROI**：`component/kerneldirect/controller.go:449-451` 只要出現新 host 就標脏並全表重建 IPSet（`:785-796`），即使該位址已帶相同 DIRECT/PROXY 位元。4096 筆時量測 2.63 ms / 876 KB / 26 allocs → 1.72 µs / 227 B / 2 allocs（約 1500×），並縮短 DNS 執行緒的 `c.mu` 等待。
- **三個零風險的 per-packet 熱點**：tproxy `LocalAddr()` 每 datagram 重建 4 次 `*net.UDPAddr`（`listener/tproxy/packet.go:39-41`，-6 allocs / -161 B）；NAT last-hit 僅 64 槽且 sticky（`component/nat/table.go:13,81-100`，>64 流每包多付 ~35-50 ns）；shard hash 對 120 B 全鍵做 maphash（`tunnel/tunnel.go:167`，48.5→13.4 ns）。
- **兩個 per-packet 配置修正**：hysteria v1 `Unpack` 每次 datagram 10 allocs（`transport/hysteria/core/protocol.go:268-311`，可降至 1）；VLESS UDP 回應分兩次 Write，在 TLS 下等於兩個 record + 兩次 AEAD seal（`listener/sing_vless/service.go:405-411`）。
- **多項提案已落地或是錯的，不可重提**：sing context key（08db3e12）、geosite matcher per-rule cache（b1960465）、geosite regex TLD index（a9cced03）、TUIC v5 codec（08b2391b）皆已在樹上；deadline pipe/goroutine 與 outbound deadline probe 因正確性問題否決。
- **記憶體面**：P0-2 讀取緩衝區 right-size 可把在途上限從 24 MiB 降到 ~3 MiB，但每包多一次複製（+17~60 ns，未量測端到端），屬「以 CPU 換記憶體」的明確取捨，需 benchmark 把關。
- 所有建議皆不改變 config / JSON / wire 格式，未降低任何 cache / queue / NAT / TTL 上限，未做 GC 調參。

---

## 2. 已確認且行為不變的優化（依 ROI 排序）

| ID | 位置 | 問題 | 影響頻率 | 預期收益（實測） | 風險 | 建議 |
|---|---|---|---|---|---|---|
| **KD-1** | `component/kerneldirect/controller.go:449-451`、`:785-796`、`:744-764` | 新 host 帶相同 decision 位仍觸發全表 `buildSetsLocked` + `Prefixes()` + `slices.Equal`，且 publish 次數為 0 | per-DNS-answer / per-flow（非 per-packet） | 4096 筆：2.63 ms→1.72 µs、876 KB→227 B、26→2 allocs；1024 筆 323 µs→1.03 µs；MultiHostFill 41.9 MB→6.6 MB | 低；overlay A/B 證明 publish 序列、generation、`Statuses()` 完全一致 | 新增 `addressRecords.hasDecision(direct)`，在 `upsert` **之前**計算，只在 false 時設 dirty |
| **NAT-1** | `component/nat/table.go:13`、`:41-51`、`:81-100` | `natHitSlots=64` 且只按來源 port 取模；`remember` 為 sticky，槽位被佔後其他流永久 miss，每包重走 xsync | per-packet | 1024 流 76-78→18-20 ns；256 流 63-65→16-17 ns；worker-pinned 並行 5.5→2.6-3.4 ns；單流 1.06-1.11 ns 不變 | 低；需把 `hits` 改為 heap slice，避免 `Table` 內嵌 32 KiB | `natHitSlots=4096`（2048 只拿到約一半）；不動 sticky 與 `dead` 檢查 |
| **TP-1** | `listener/tproxy/packet.go:39-41`；呼叫點 `adapter/inbound/packet.go:13,15`、`listener/tproxy/udp.go:116`、`constant/adapters.go:316/391` | 同一 `netip.AddrPort` 每 datagram 轉成 4 次新的 `*net.UDPAddr`（每次 2 allocs） | per-packet（Linux TPROXY/redir only，`udp_other.go` 在 Windows 直接報錯） | 端到端 929 B/13→768 B/7 allocs（-6 allocs / -161 B）；ns 不可信（685-2125 區間） | 低；所有 consumer 只讀 `AddrPort()`，`RawSrcAddr` 為 `json:"-"` | 在 `handlePacketConn`（`udp.go:106-118`）建一次。**建議 value-embed**（`src net.UDPAddr` + `srcIP [16]byte`，回傳 `&c.src`）可再省到 0 alloc（未量測，須用 `srcIP[:4:4]` 保持 cap） |
| **FI-1** | `dns/enhancer.go:91-110`；`component/fakeip/pool.go:59-64` | 每個 UDP 包先對（可能兩個）pool 取 `p.mux`，鎖內再做兩層 LRU 鎖，最後才查 mapping LRU | per-packet（fake-ip / mapping 模式） | 單 pool 約 85 ns/包（`PoolLookBackHit` 64.5-69.9 ns，0 allocs）；並行下省的是序列化 | 中；`store-fake-ip` 跨 `fake-ip-range` 變更的殘留項會有行為差異 | 先 `pool.IPNet().Contains(ip)`（唯讀不可變前綴）再 `LookBack`；此寫法已見於同檔 `IsFakeIP`（`:57,63`） |
| **SH-1** | `tunnel/tunnel.go:167-168` | 只為選 worker queue 就對 120 B `UDPNatKey` 做 `MapHashComparable` 全鍵 hash | per-packet（20+ 個 inbound 呼叫點） | 48.5-52.7→13.4-14.2 ns、0 B/0 allocs；in-situ 約 -35 ns，約佔入站 UDP 路徑 3-5% | 低（**僅限安全變體**）；原提案的子集會讓 Raw 路徑坍縮到 1/24 個 worker | `if key.AddrPort.IsValid() { hash AddrPort } else { hash 全鍵 }`。**不可**只 hash `(AddrPort, IngressAddrPort)` |
| **MEM-1** | `common/net/packet/packet_windows.go:28`、`packet_posix.go:20`、`packet.go:173`；`listener/tproxy/udp.go:76`、`listener/tproxy/packet.go:77`、`listener/tunnel/udp.go:62` | 每個在途 datagram 釘住 16 KiB（low-memory 8 KiB）slab 直到 `Drop` | per-packet | 在途上限 24 MiB（24×64×16 KiB）+ 2 MiB/停滯關聯 → ~3 MiB + 128 KiB；成本 +17~60 ns/包（≤512 B 在 ±5% 內，1400 B 超標） | 中；**必須用 `pool.Get(n)` 保持 cap**（`shadowaead/packet.go:56` 就地解密依賴 cap），且 Windows 只能在成功 recv 後換 buffer | 只在 ≤2 KiB 複製縮小，>threshold 完全不變；先做 `packet.go` / OS rawRead 兩個 choke point |
| **HY-1** | `transport/hysteria/core/protocol.go:268-311`（RX）、`:250-266`（TX） | `Unpack` 用 `bytes.Reader` + 9 次 `binary.Read`，每次配一個 temp | per-packet（僅 legacy hysteria v1；hysteria2 走 sing-quic） | RX：474 ns/10 allocs/80 B → 30 ns/1 alloc/8 B（1200 B payload） | 低；wire 相同，且 `m.Data` **本來就是 view**（`:309`），無新增 aliasing | 改成 bounds-checked `Uint16/Uint32` 直讀；保留 `io.ErrUnexpectedEOF`。TX 的 pool 化另案 |
| **VL-1** | `listener/sing_vless/service.go:405-411` vs `:429-434` | `WriteTo` 分兩次 `Write`（2-byte 長度 + payload）；`WritePacket` 早已是單次寫 | per-packet（VLESS inbound UDP；`WriteTo` 是**唯一**實際路徑，`:429` 幾乎是死碼） | TLS loopback：512 B 85.8→36.7 µs、1024 B 72.9→42.8 µs（microbench 高估，實際為少一個 record/AEAD/syscall） | 低-中；**提案寫法會 panic**（`buf.NewSize` 後 `start=0`，`ExtendHeader(2)` 直接 panic） | `WriteTo` delegate 到 `WritePacket`：`buf.NewSize(len(p)+2)`、**`buffer.Resize(2,0)`**、`Write(p)`、`ExtendHeader(2)`；headroom 固定 2，勿用 `CalculateFrontHeadroom`（首包會算成 4，造成 wire 空隙） |
| **UDP-1** | `common/net/packet/packet_windows.go:45-46`、`packet_posix.go:37-45` | 每個 datagram 分別配置 `*net.UDPAddr` 與其 IP slice | per-packet（SOCKS/SS UDP 的 `*net.UDPConn` 快路徑） | 4→3 allocs/op（99→104 B，3 次取樣）；單獨建構 2→1 allocs（52→64 B） | 低；用 `b.ip[:4:4]`（`[:16:16]`）保持 cap，勿用提案的 `[:4]`（cap 變 16） | 合併為單一物件，**每包仍新建**，不可 pool（`listener/socks/utils.go:189-197` 依賴 addr 存活至 `put()` 之後） |
| **TC-1** | `component/trafficcontrol/model.go:398` | `canonicalRuleType` 每次呼叫都 `strings.NewReplacer(...)`（編譯 trie + 6.7 KB） | per-connection（traffic-control 啟用且含 Rule policy） | 2904→345 ns、6880→144 B、7→3 allocs；`Manager.Open` 的 ~97% 來自此處 | 無；`strings.Replacer` 併發安全且不可變 | 提升為 package-level var（放在 `:173-176`），`:397` 的 `ToUpper+TrimSpace` 不動 |
| **UI-1** | `tunnel/statistic/tracker.go:93-103`、`constant/metadata.go:79-81/185-187`、`common/atomic/type.go:140-142` | leaf 型別走 reflect + 巢狀 `json.Marshal`（jsonv2 的 Marshaler 路徑） | per-UI-update（每個 WS client 每個 tick，預設 1 s） | 1k conns 13,705→8,027 allocs；10k -28% bytes / -43% allocs（**含不可改的 uuid 第三方部分，實際較低**） | 低；但 `AppendText` 對已有 `MarshalJSON` 的型別是 **no-op**（Marshaler 優先），且 `encoding/json/jsontext` 受 `goexperiment.jsonv2` 限制 | 直接把既有 `MarshalJSON` 本體改為 `strconv.AppendInt/AppendQuote`；DNSMode **新增**（非取代）`AppendText` |
| **AS-1** | `component/aster/users.go:104-121`（Part A） | `Users` slice 無容量，10k 筆 append 成長；且對所有 listener 先 clone 再過濾 | per-request（`GET /api/admin/users`，admin） | 10.94 MB/10,026 allocs → Part A 可去 ~7.5 MB（68%）；A+B 合計 ~1.76 MB/~30 allocs | Part A 無；Part B 改 exported 回傳型別，需先補 `inbounds` golden test | **只先做 Part A**：預掃 `len(state.Users)`（套用 `:109`、`:113` 的過濾）後 `make(..., 0, total)` |

---

## 3. 不確定 / 需要先量測（未取得對抗性驗證）

| ID | 位置 | 為何不確定 | 需要什麼 |
|---|---|---|---|
| TW-1 | `listener/tproxy/packet.go:55-64` | `local := lAddr.String()` 在 NAT local-conn 命中時也被計算；量測 230.3→182.4 ns、3→2 allocs，但該數字只來自單一 lane，未經 A/B 重現 | 補 `BenchmarkWriteBack` hit-path（`-benchmem -count=7`），並保留 `packet_test.go` 的 miss-path 錯誤傳遞測試 |
| DR-1 | `tunnel/connection.go:177-186` | `time.Now()` 後又 `time.Since(...)`，等於兩次時鐘讀取；現有 `BenchmarkPacketSenderDeadlineRefresh` 僅 11.7 ns/0 alloc，理論收益 ~5 ns，落在噪音內 | 以程式碼算術確認（`now.Sub(epoch)`），不要用 ns 差值證明 |
| HS-1 | `component/resolver/host.go:37` | `moved to heap: hostValue` 造成 1 alloc/48 B，但修正需改共享 API 或新增 `Exists`；未量測實際站點收益 | `testing.AllocsPerRun` 對 miss 路徑前後比較；確認 alias 跟隨語意不變 |
| TR-1 | `component/trie/domain.go:38-39` | `strings.Split` 每此配置 48 B；但 split 同時餵給驗證（空 label、wildcard），需完整語意等價 | 新增 `BenchmarkValidAndSplitDomain`，並跑 `component/trie` 既有 wildcard / 非 ASCII 測試 |
| SN-1 | `tunnel/statistic/manager.go:213-225` | 預配置（310 KB→82 KB）與 `RangeRelaxed`（66 µs→30 µs）方向正確，但 relaxed 走訪與 `Join/Leave` 的併發語意未驗證 | `-race` + 10k 連線 bench；確認 reaper 的路徑不受影響 |
| KS-1 | `component/kerneldirect/controller.go:767-783` | `Statuses()` 在 `c.mu` 下全表掃描（1024 筆 29 µs、4096 筆 115 µs），但需改增量計數；同檔另有 agent 正在修改 | 增量計數與全表掃描的隨機操作對照測試；與 KD-1 一起排程 |

---

## 4. 已否決 / 不建議

- **geosite 每規則重載 matcher**（`rules/common/geosite.go:34`）— 已由 `b1960465` 落地（generation 守衛 + per-rule cache），185→109-146 ns、40 B/2→0 B/0 allocs。
- **geosite regex 無 prefilter 全掃**（`component/geodata/router/condition.go:48`）— 已由 `a9cced03` 的 TLD index 取代（miss 71 µs→9-20 µs）；提案的 literal 抽取規則不健全（未處理 `\\.`、漏 `+`），實測 102/80,745 mismatch。
- **TUIC v5 codec `binary.Write`**（`transport/tuic/v5/protocol.go:290-324`）— 已由 `08b2391b` 修掉；現為 48.5 ns/2 allocs（write）、83.8 ns/5 allocs（read）。
- **TUIC 每 datagram 一個 goroutine**（`transport/tuic/v5/client.go:182`、`server.go:127`）— goroutine 是**背壓機制**：`conn.Write` 進的是同步無緩衝 pipe（`pipe_sing.go:129-130`），server 端還會等 `<-s.authCh`；inline 化會讓單一 QUIC 連線上所有關聯一起 head-of-line block，並在 quic-go 128 筆接收佇列滿時轉為丟包。
- **sing context key boxing（兩個 lane 各提一次）** — 已由 `08db3e12`（PR #16）改為 pointer key；現行樹 `context.go:19-27` 已無 string-kind key，提案的 `&contextKey{"Additions"}` 甚至無法編譯。
- **deadline wrapper 每讀一個 goroutine**（`common/net/deadline/packet.go:123-124`）— 提案宣稱的 2026-2372 ns/1584 B/3 allocs 基準不存在；對照實測 1154-1253 ns/16 B/1 alloc，原型僅省 1 個 alloc，ns 在噪音內；且 `Close` 掛鉤不存在、固定 buffer slot 會截斷 UDP，sing v0.5.8 也**沒有** persistent-reader 先例。
- **deadline 零期限 fast path**（`common/net/deadline/conn.go:67-82`）— 該 wrapper 在 relay 路徑被 `UnwrapReader` 直接繞過（400 KB relay 實測 0 次進入 `Read`）；UDP 端 deadline 從第 1 包就非零，guard 不會命中。
- **packet-sing 每 datagram 新建 `*net.UDPAddr`**（`common/net/packet/packet_sing.go:73`）— 可達路徑其實在 **outbound**（UoT/hysteria2/trusttunnel/vmess），且提案的「單一可變 addr」會與 `C.WriteBack` 的保留/併發使用產生 race；安全變體（destination 不變時重用快取）另議。
- **outbound UDP deadline 探測**（`common/net/deadline/packet_enhance.go:87-90`）— `SetReadDeadline` 回 nil 不代表支援：`adapter/outbound/dns.go:148-150` 回 nil 但忽略，`transport/hysteria/core/client.go:439` 把 deadline 轉到 `Stream` 而 `ReadFrom` 等的是 `MsgCh`；eagerly `disablePipe` 會導致關聯永不返回、NAT 表無獨立回收器而洩漏。既有 `TestNetPacketConnPastDeadline` 會直接失敗。
- **Aster principal mutex 分片**（`tunnel/statistic/manager.go:106`）— 提案的 `LoadAndDelete`-on-zero 是 check-then-act race，實測 594-600 次違反不變量（key 永久殘留，污染 `/api/admin/users` 的 `active_connections`）；以 `xsync.Map.Compute` 的正確版本也只從 1253-1318→722-802 ns（~1.7×，非 5×），單執行緒還多 2 allocs。
- **queue hash 全鍵（原提案寫法）**（`tunnel/tunnel.go:167`）— 只用 `(AddrPort, IngressAddrPort)` 會讓所有非 TUN sing inbound（Raw 路徑，`constant/adapters.go:300-309`、`common/net/addr.go:13-23` 的 `customAddr` 無 `AddrPort()`）坍縮到 1/24 個 worker；安全替身見 SH-1。
- **本報告未採納的 lane 內小項**：`Process lookups`（`component/process`）、DNS LRU 侵入式串列、`aster` clone-listener、`Open` 前後的 `regexp`/`time.Now()` 掃描等，皆因頻率為 per-config-load / per-UI-update 或行為風險過高而未列入。

---

## 5. 建議實作順序

### A. 可直接實作（低風險、明確收益）
1. **KD-1** `component/kerneldirect/controller.go:449-451`（+`hasDecision`）。
2. **NAT-1** `component/nat/table.go:13`（4096 + heap slice）。
3. **TC-1** `component/trafficcontrol/model.go:398`（一行提升）。
4. **TP-1** `listener/tproxy/packet.go:39-41`（先做快取指標版；value-embed 另以 Linux 驗證）。
5. **UDP-1** `common/net/packet/packet_windows.go:45-46` / `packet_posix.go:37-45`（用 `[:4:4]`）。
6. **VL-1** `listener/sing_vless/service.go:405-411`（delegate + `Resize(2,0)`）。
7. **HY-1 RX** `transport/hysteria/core/protocol.go:268-311`。
8. **FI-1** `dns/enhancer.go:91-110`（含 persist 邊界測試）。
9. **AS-1 Part A** `component/aster/users.go:104-121`。

### B. 需先補 benchmark 才可動
- **MEM-1** 緩衝區 right-size：先建 heap-inuse 測試（1000 flow × 32 queued 200 B）與 `-benchmem` 回歸，確認 ns/op 在 ±5%。
- **SH-1** 安全變體：先建 queue 分佈測試（AddrPort 與 Raw 兩種 key 都要覆蓋 >1 個 queue）。
- **UI-1** JSON：先建 `BenchmarkConnectionsSnapshotJSON` + golden bytes 測試（現有 `connections_test.go:66` 只斷言 `"connections":[]`）。
- 第 3 節的 TW-1 / DR-1 / HS-1 / TR-1 / SN-1 / KS-1。

### C. 需設計討論
- `store-fake-ip` 跨 range 變更的殘留項（要嘛接受行為差異，要嘛在 `restoreState` 先清 out-of-prefix）。
- hysteria v1 的 **TX** pool 化（`SendDatagram` 會再複製一次，需先確認 `tracker.go:480` 的第二個 `WriteTo` 呼叫者的併發模型）。
- MEM-1 的 CPU/記憶體取捨門檻（2 KiB 與否），以及是否擴及 tproxy / tunnel listener。
- KD-1 的 `ObserveFlow`/`ObserveDNS` 缺 `HasConsumers` 提前返回（可分離的獨立小 win）。

---

## 6. 驗證計畫

每次改動後的基本三步（皆為 targeted，不跑全量）：

```bash
gofmt -l <modified files>
go vet ./<pkg>
go test -count=1 ./<pkg>
```

| 變更 | 指令 |
|---|---|
| KD-1 | `go test -count=1 -run "TestController\|TestParseTunKernelDirect\|TestKernelDirectStatus" ./component/kerneldirect ./config ./hub/route`；`go test -run '^$' -bench 'BenchmarkKernelDirect(MultiHostFill\|SteadyEvict\|SingleHostFill)' -benchmem -count=7 ./component/kerneldirect`；新增「預填 M=256/1024/4096 後反覆 observe 新 host」bench；`go vet ./component/kerneldirect ./config ./hub/route ./listener/sing_tun`；`gofmt -l component/kerneldirect/controller.go` |
| NAT-1 | `go test -count=1 ./component/nat ./tunnel`；新增 `BenchmarkTableExistingFlowK`（K∈{32,128,256,1024}，真實 ephemeral port）；`go test -run '^$' -bench 'BenchmarkTableExistingFlow' -benchmem -count=7 ./component/nat` |
| TP-1 | `go test -count=1 ./listener/tproxy`；新增 `BenchmarkTproxyInboundChain`（IPv4 / IPv4-mapped-IPv6 golden）；Linux 上跑真實 UDP redir 端到端 |
| FI-1 | `go test -count=1 ./dns ./component/fakeip`；新增 `BenchmarkResolverFindHostByIP`（v4 pool / v6 pool / 真實 IP / 無 ipv6），serial + `b.RunParallel`，看 allocs/op 與 B/op |
| SH-1 | `go test -count=1 ./tunnel`；`go test -run '^$' -bench 'BenchmarkHandleUDPPacket\|BenchmarkUDPFlowSteadyState' -benchmem -count=7 ./tunnel`；新增分佈測試（AddrPort 與 Raw 各 256 個 key，assert 覆蓋 >1 queue）與 10k 同 key 順序測試 |
| MEM-1 | `go test -count=1 ./common/net/packet ./listener/socks ./listener/shadowsocks`；`go test -run '^$' -bench 'BenchmarkEnhanceUDPConnWaitReadFrom\|BenchmarkWaitReadFromGeneric' -benchmem -count=7 ./common/net/packet`；`go test -race ./common/net/packet`（**本機 cgo/gcc 缺失，race 需在 Linux CI 執行**）；邊界測試 2047/2048/2049 B |
| HY-1 | `go test -count=1 ./transport/hysteria/core`；新增 `BenchmarkUDPMessageUnpack/Pack` 與 malformed 輸入表測試 |
| VL-1 | `go test -run 'TestServerPacketConn' -count=1 ./listener/sing_vless`；新增「counting writer + TLS-wrapped」bench，assert writes/datagram 2→1；新增首包（response header 未寫）golden |
| UDP-1 | `go test -count=1 ./common/net/packet`；`go test -run '^$' -bench 'BenchmarkEnhanceUDPConnWaitReadFrom' -benchmem -count=7 ./common/net/packet`；新增 `cap(IP)==4` 斷言 |
| TC-1 | `go test -count=1 ./component/trafficcontrol`；新增 `BenchmarkCanonicalRule` |
| UI-1 | `go test -count=1 ./hub/route ./tunnel/statistic`；golden JSON（含每個 enum 值與 int64 極值） |
| AS-1 | `go test -count=1 ./component/aster ./hub/route`；`go test -run '^$' -bench 'BenchmarkManagerOverview' -benchmem -count=7 ./component/aster` |

**共通注意事項**
- `-race` 在 Windows 本機不可用（cgo 開啟但 gcc 缺失），須在 Linux CI 執行；`go test -count=1 ./common/net/packet`、`./listener/tproxy` 的非 race 版本已可通過。
- tproxy UDP redir 端到端只能於 Linux 驗證（`listener/tproxy/udp_other.go`）。
- 所有 benchmark 以 `-count=7` 搭 `benchstat` 解讀，**B/op 與 allocs/op 為準**，勿引用單次 `ns/op`。
- 本報告未修改、建立或刪除任何 repo 檔案，也未執行任何改變狀態的 git 指令。