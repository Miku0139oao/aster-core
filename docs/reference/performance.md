# Aster 比 Mihomo 快多少？

## 先看結論

Aster 沒有用魔法把你的網路變快，而是把代理核心內大量重複的小工作拿掉。以下是在實際 OpenWrt 軟路由上，相同程式與測試各跑三輪的中位數：

| 你會遇到的工作 | Aster 快多少 | 為什麼會快 |
| --- | ---: | --- |
| TCP 搬運 32 KiB 資料 | **吞吐約高 3%**、處理時間約少 3% | 重複使用搬運資料的暫存空間 |
| AnyTLS 打包 1 KiB | **約快 1.9 倍** | 直接寫進可重用 buffer，不再多包一層物件 |
| AnyTLS 打包 16 KiB | **約快 1.3 倍** | 同上，並減少小型記憶體申請 |
| 準備一個 UDP 封包 | **約快 5.1 倍** | 封包資料結構用完收回，下次直接重用 |
| 忽略一行已關閉的 debug log | **約快 97 倍** | 在組字串、建立 log 前就直接跳過 |

> [!IMPORTANT]
> **最接近「實際搬資料」的是 TCP 的約 3% 改善。** 5.1 倍和 97 倍只代表核心裡某個很小、但會執行非常多次的步驟，不代表下載速度會直接變成 5.1 倍或 97 倍。

## 到底改了什麼？

### 1. 不再每個封包都申請新記憶體

以前 TCP、UDP、AnyTLS 處理一次資料，就可能建立新的暫存物件。現在用完會收回，下個封包直接重用。結果是 CPU 少花時間整理垃圾記憶體，高負載時也比較不容易突然抖一下。

### 2. 多條連線不用搶同一把鎖

以前封包查規則、找代理時，要讀取大家共用的資料並拿鎖。現在設定更新時先做好一份完整快照，處理封包只讀快照，不必互相排隊。

### 3. UDP 不再一直重做相同轉換

來源位址直接用電腦容易比較的格式保存，不再每個封包都轉成字串。socket 的逾時計時也改成合併刷新，而不是每收到一包就重設一次。

### 4. AnyTLS 的規則先算一次

Padding 規則在載入設定時就先解析好。真正傳資料時，Aster 只要查結果並直接組 frame，不再邊傳邊拆字串、重新計算。

### 5. 不會顯示的 log 就完全不做

如果 debug log 已關閉，而且也沒有面板在監聽，Aster 會在格式化訊息前返回。以前即使最後不顯示，仍會先建立完整 log 再丟掉。

### 6. 流量數字直接累加

上傳、下載與連線數改用便宜的增量統計。每次有流量時只更新數字，不再為了得到總數反覆掃描所有活動連線。

## 以下是完整測試數據

## OpenWrt 實機比較（主要結果）

比較基線為 Aster 上游的 Mihomo `v1.19.29`、commit `e26714a181ac0e2fa803453c0a8e9a9ce94e31cb`。兩個版本使用同一組 benchmark 程式與參數，依序執行三輪，每輪至少 2 秒；表格採三輪中位數。

| 環境項目 | 實際值 |
| --- | --- |
| 系統 | OpenWrt，Linux 6.6.86，x86-64 |
| CPU | Ryzen 7 5825U 宿主，VM 配置 12 vCPU |
| CPU 頻率 | Hypervisor 未提供，**無法取得** |
| 記憶體 | VM 配置約 6 GB |
| DRAM 頻率 | Hypervisor 未提供，**無法取得** |
| Go | 1.26.3，Linux amd64 test binary |
| 測試前 load average | 0.19／0.15／0.10（1／5／15 分鐘） |
| 測試後 load average | 0.94／0.41／0.20（1／5／15 分鐘） |

| 核心工作 | Mihomo 中位數 | Aster 中位數 | Aster 相對結果 |
| --- | ---: | ---: | ---: |
| UDP packet metadata | 68.45 ns；416 B／1 alloc | 13.38 ns；0 B／0 alloc | **5.1× faster**；消除 416 B 配置 |
| 停用的 debug log | 222.3 ns；24 B／1 alloc | 2.291 ns；0 B／0 alloc | **97× faster**；消除 event 配置 |
| AnyTLS frame（1 KiB） | 71.03 ns；64 B／1 alloc | 36.65 ns；0 B／0 alloc | **1.94× faster**；延遲降低 48% |
| AnyTLS frame（16 KiB） | 270.3 ns；64 B／1 alloc | 207.1 ns；0 B／0 alloc | **1.31× faster**；延遲降低 23% |
| TCP relay（32 KiB） | 4.397 µs；7.45 GB/s；64 B／1 alloc | 4.271 µs；7.67 GB/s；0 B／0 alloc | 延遲降低 **2.9%**；吞吐提高 **3.0%** |

### 三輪測量範圍

| Benchmark | Mihomo 範圍 | Aster 範圍 |
| --- | ---: | ---: |
| UDP packet metadata | 67.50–68.91 ns/op | 13.02–13.38 ns/op |
| 停用的 debug log | 221.1–225.4 ns/op | 2.245–2.297 ns/op |
| AnyTLS frame（1 KiB） | 70.78–71.09 ns/op | 35.71–36.67 ns/op |
| AnyTLS frame（16 KiB） | 269.7–275.1 ns/op | 204.5–211.8 ns/op |
| TCP relay（32 KiB） | 4.396–4.418 µs/op | 4.227–4.337 µs/op |

這台 5825U 軟路由仍比許多 MT7621、低階 ARM 或廉價 VPS 強。這組結果只能證明優化在實際 OpenWrt 環境仍有效；越弱的裝置，絕對 GB/s 一定更低，改善比例也必須在該裝置重新測量。

## Hyper-V 低階資源模擬（次要結果）

為了更接近大部分使用者的弱硬體，我們在同一台 Hyper-V OpenWrt VM 裡再加一層資源限制。Benchmark 只准使用 **1 顆 vCPU**，而且每 100 ms 最多只能執行 25 ms，相當於 **單核 25% CPU 時間**；記憶體上限設為 **512 MiB**，並停用 swap。

這是資源不足情境的模擬，不是 MT7621 或 ARM 指令集模擬。它能回答「CPU 很慢、記憶體很少時，Aster 的優化是否仍存在」，但不能取代真實低階路由器測試。

| 環境項目 | 實際值 |
| --- | --- |
| 系統 | Hyper-V 上的 OpenWrt，Linux 6.6.86，x86-64 |
| 宿主 CPU | Ryzen 7 5825U |
| CPU 限制 | 綁定 vCPU 0；cgroup `cpu.max = 25000 100000`，即單核 25% CPU 時間 |
| 客體回報 CPU 頻率 | 測試前平均 1.998 GHz；測試後平均 2.080 GHz。這是頻率取樣，**不包含 25% CPU 時間限制** |
| 記憶體限制 | 512 MiB、無 swap；測試程序峰值約 23.2 MiB |
| DRAM 頻率 | Hypervisor 未提供，**無法取得** |
| Go | 1.26.3，Linux amd64 test binary |
| 測試前 load average | 0.00／0.11／0.12（1／5／15 分鐘） |
| 測試後 load average | 0.29／0.19／0.15（1／5／15 分鐘） |
| 節流確認 | 1,200 個 CPU quota periods 中有 1,197 個發生 throttling |

兩個版本依序跑相同 benchmark，各三輪、每輪至少 2 秒。以下採三輪中位數：

| 核心工作 | Mihomo 中位數 | Aster 中位數 | Aster 相對結果 |
| --- | ---: | ---: | ---: |
| UDP packet metadata | 239.0 ns；416 B／1 alloc | 47.60 ns；0 B／0 alloc | **5.0× faster**；消除 416 B 配置 |
| 停用的 debug log | 842.6 ns；24 B／1 alloc | 8.044 ns；0 B／0 alloc | **約 105× faster**；消除 event 配置 |
| AnyTLS frame（1 KiB） | 315.1 ns；64 B／1 alloc | 128.0 ns；0 B／0 alloc | **2.46× faster**；延遲降低 59% |
| AnyTLS frame（16 KiB） | 1.087 µs；64 B／1 alloc | 835.6 ns；0 B／0 alloc | **1.30× faster**；延遲降低 23% |
| TCP relay（32 KiB） | 16.627 µs；1.97 GB/s；64 B／1 alloc | 15.918 µs；2.06 GB/s；0 B／0 alloc | 延遲降低 **4.3%**；吞吐提高 **4.5%** |

### 峰值記憶體佔用

先把兩種「記憶體佔用」分開看：

| 情境 | Mihomo | Aster | 結論 |
| --- | ---: | ---: | --- |
| 完整核心、最小設定、空載 15 秒的 RSS | 28.9 MiB | 31.6 MiB | Aster 多 2.8 MiB，約 **+9.5%** |
| 完整核心的 cgroup `memory.current` | 6.50 MiB | 6.80 MiB | Aster 多約 0.30 MiB，約 **+4.6%** |

完整核心測試使用相同設定：Direct 模式、無代理、無規則、靜默 log、IPv6 關閉；Aster 與 Mihomo 交錯三輪，每輪啟動後等待 15 秒。RSS 三輪範圍為 Aster 31.5–31.9 MiB、Mihomo 28.9–29.1 MiB。OpenWrt 核心沒有提供 `smaps_rollup`，因此沒有 PSS 數據。

**所以不能說 Aster 空載比較省 RAM。** Aster 加入更多功能與程式碼，最小設定的常駐底座目前比 Mihomo 多約 2.8 MiB。下面的 30–49% 降幅只出現在重複執行特定 hot path 時，代表忙碌時少製造臨時記憶體，不代表完整程式的 idle RSS 較小。

### 核心 hot path 的獨立程序峰值

每個案例改用獨立程序重跑三輪，透過 Linux `/usr/bin/time -v` 記錄 Max RSS。表格採三輪中位數；兩邊都套用相同的單核 25% CPU 時間與 512 MiB 記憶體限制。

| 核心工作 | Mihomo 峰值 RSS | Aster 峰值 RSS | 差異 |
| --- | ---: | ---: | ---: |
| TCP relay（32 KiB） | 39.5 MiB | 24.5 MiB | **少 38%** |
| 停用的 debug log | 33.0 MiB | 23.0 MiB | **少 30%** |
| UDP packet metadata | 50.6 MiB | 26.0 MiB | **少 49%** |
| AnyTLS frame（1 KiB） | 38.9 MiB | 23.5 MiB | **少 40%** |
| AnyTLS frame（16 KiB） | 39.6 MiB | 23.5 MiB | **少 41%** |

這裡量到的是「只載入該 package 並執行 benchmark」的 Go 測試程序峰值，不是完整代理程式掛著規則、GeoIP、DNS cache 和連線時的總 RAM。它適合比較相同小工作造成的記憶體壓力，不能宣稱 Aster 在任何設定下整體 RAM 都固定少 30–49%。

三輪範圍分別為：TCP Mihomo 39.5–40.5 MiB、Aster 24.0–24.5 MiB；log Mihomo 32.5–33.9 MiB、Aster 固定 23.0 MiB；UDP Mihomo 42.9–55.5 MiB、Aster 固定 26.0 MiB；AnyTLS Mihomo 38.0–40.0 MiB、Aster 23.5–24.0 MiB。所有測試皆為 0 swap。

### 受限環境三輪範圍

| Benchmark | Mihomo 範圍 | Aster 範圍 |
| --- | ---: | ---: |
| UDP packet metadata | 231.3–243.0 ns/op | 47.13–48.03 ns/op |
| 停用的 debug log | 825.6–921.2 ns/op | 7.958–8.066 ns/op |
| AnyTLS frame（1 KiB） | 313.3–319.2 ns/op | 126.3–129.9 ns/op |
| AnyTLS frame（16 KiB） | 1.047–1.097 µs/op | 731.3–895.3 ns/op |
| TCP relay（32 KiB） | 16.605–17.087 µs/op | 15.754–16.501 µs/op |

受限後的絕對處理時間約變成原本的四倍，證明 CPU quota 確實生效；Aster 在五項工作中仍全部較快。人話來說，**硬體越弱，Aster 省掉的 CPU 工作和記憶體配置通常越有感**：TCP 改善由約 3% 增至 4.3%，AnyTLS 1 KiB 由 1.94 倍增至 2.46 倍。不過 UDP 與 AnyTLS 16 KiB 的倍率幾乎不變，所以不能保證每項優化都會隨硬體變弱而等比例放大。首頁仍採用未限速 OpenWrt 的 **TCP 約 3%**，不使用這組較好看的 4.3% 當主要宣傳數字。

## 各協議 loopback 測試

核心 microbenchmark 變快，不等於每一種協議的端到端吞吐都會同步增加。為了驗證這點，我們在 Linux Docker host network 上，讓 Aster 與 Mihomo 連到相同的本機服務端，以 16 KiB chunk 分別測量讀寫。

| 協議 | 服務端 | Aster 與 Mihomo 的結果 |
| --- | --- | --- |
| Direct（控制組） | 無 | 讀寫差距在約 3% 內，視為持平 |
| Shadowsocks AES-256-GCM | shadowsocks-rust | 分配量相同；寫入樣本波動過大，**不下加速結論** |
| VMess | V2Fly 4.45.2 | 寫入約 645 vs 647 MB/s；讀取約 488 vs 488 MB/s，持平 |
| VLESS + TLS | V2Fly 4.45.2 | 交錯三輪的典型差距約 -2% 至 +3%，持平 |
| Trojan + TLS | trojan 1.16 | 交錯五輪中位數：寫入 335 vs 341 MB/s、讀取 313 vs 314 MB/s，持平 |
| Snell v3 + HTTP obfs | OpenSnell 3.0.1 | 交錯三輪中位數：寫入 429 vs 430 MB/s、讀取 763 vs 774 MB/s，持平 |
| Hysteria v1 | Hysteria 1.3.5，100 Mbps | 兩邊皆約 12.25–12.28 MB/s，受服務端頻寬上限限制 |
| AnyTLS | 目前沒有同條件 loopback 服務端 | 不混用外部節點；只列前面的 frame microbenchmark |

表內數字順序皆為 **Aster vs Mihomo**。VMess、VLESS、Trojan、Snell、Shadowsocks 的 B/op 與 allocs/op 也大致相同，表示這次優化主要改善共用 relay、UDP metadata、log 與 AnyTLS frame 等底層 hot path，沒有神奇地讓所有加密協議一起大幅變快。

這組協議測試使用 Ryzen 9 5900X、Docker Linux amd64、Go 1.26.5。每個一般協議至少三輪；Trojan 因第一組順序測試出現互相矛盾的 ±20% 結果，另外用固定 test binary 做 Aster／Mihomo 交錯五輪，差距收斂到約 2% 內。協議測試期間沒有同步記錄原始 host load，因此只用來判斷有沒有明顯協議回歸，**不作為首頁宣傳數字**。

服務端映像固定為 shadowsocks-rust `sha256:85d01d…e1359`、V2Fly `sha256:e81a07…de78c`、Trojan `sha256:5b36c2…b98b5f7`、OpenSnell `sha256:70053f…345467`、Hysteria v1.3.5 `sha256:4c8c92…f1e35`。

### UDP metadata 配置前後

同一支 benchmark 同時量測 Mihomo 的直接建立 metadata 路徑與 Aster 的物件池路徑，因此可以做同機比較：

| 路徑 | 時間 | 記憶體配置 |
| --- | ---: | ---: |
| Mihomo 每個封包直接建立 | 67.50–68.91 ns/op | 416 B/op、1 alloc/op |
| Aster metadata pool | 13.02–13.38 ns/op | 0 B/op、0 allocs/op |

物件池路徑以三輪中位數計算約快 **5.1 倍**，並消除每封包 416 bytes 的 heap allocation。

## 高階開發機結果（附錄）

最初的 Aster 絕對數據是在高階桌機上取得，只適合做開發期回歸檢查，不應拿來代表一般路由器：

| 環境項目 | 實際值 |
| --- | --- |
| 系統 | Windows amd64；平衡電源模式 |
| CPU | Ryzen 9 5900X，12 cores／24 logical CPUs |
| CPU 實際頻率 | 後續代表性重跑平均 4.46 GHz，範圍 4.42–4.51 GHz |
| DRAM | 64 GB（4×16 GB）G.Skill DDR4-3600，configured 3600 MT/s |
| Go | 1.26.3 |
| 原始測試 load | **未同步記錄，無法事後補值** |
| 後續代表性重跑 CPU load | 測試前平均 36.4%（24.7–58.5%）；測試中平均 37.7%（27.3–55.6%） |
| 後續 processor queue | 平均 0.1，範圍 0–1 |

因為背景負載高且原始測試沒有逐點記錄 load，5900X 數據已不再作為首頁的主要比較；它只保留用來觀察是否發生明顯效能回退。

| Aster 單體 benchmark | Payload | 三輪結果 | 記憶體配置 |
| --- | ---: | ---: | ---: |
| UDPFlowSteadyState | 1,200 B | 0.85–1.22 µs/op | 0 B/op、0 allocs/op |
| TCPRelayThroughput | 32 KiB | 9.99–10.72 µs/op | 0 B/op、0 allocs/op |
| SessionUpload | 1 KiB | 5.46–5.52 µs/op | 0 B/op、0 allocs/op |
| SessionUpload | 16 KiB | 8.02–8.46 µs/op | 0 B/op、0 allocs/op |

## 如何重跑

從 repository 根目錄執行：

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

比較兩個版本時，請在同一台機器、相同 Go 版本與電源模式下執行，並使用 `benchstat` 分析多輪輸出；不要用不同機器的單次結果推算百分比。

## 數據如何解讀

- 這些是 in-process microbenchmarks，主要量測 Aster Core 自身的額外開銷。
- TCP 與 AnyTLS 的 GB/s 是記憶體／`net.Pipe` 路徑，不是 WAN 實際吞吐量。
- 真實代理速度還會受到加密、RTT、丟包、MTU、NIC、作業系統、CPU 架構與服務端影響。
- 64 KiB AnyTLS frame 仍需大型 buffer 配置；常見的 1 KiB 與 16 KiB 路徑已達零配置。
- 微基準最適合防止效能回退，而不是承諾任何裝置都會得到相同網速。

## TC eBPF 的實機反例

Kernel DIRECT 本身不要求 TC eBPF。推薦的 OpenWrt 路徑是由 nftables learned exclude set 把 DIRECT 交回 Linux forwarding/NAT，並保留 flow offload。實驗性 `kernel-direct-ebpf` 則會在 LAN ingress 對每個封包查 generation、IPv4/IPv6 LPM、40-byte 5-tuple LRU 與 per-CPU counters。

在同一台路由器、同一 Speedtest server ID `37639` 的 A/B 中：

| 狀態 | Download |
| --- | ---: |
| TC eBPF 開啟 | 692,335,768 bps |
| 暫時卸載 TC filters | 1,647,299,448 bps |
| 持久關閉 TC、重啟後 | 1,643,651,288 bps |

卸載後約為開啟時的 **2.37 倍**，也回到該網路原本約 1.7 Gbps 的級別。主要原因是這個 TC ingress hook 妨礙 OpenWrt flow offload，加上 classifier 本身逐封包執行；它不代表其他 eBPF 程式或其他硬體一定較慢。

這也是 microbenchmark 與實際網路結果必須分開看的例子：某個 map lookup 或資料結構很快，不代表把它放進每個 ingress packet、並改變 kernel offload 路徑後，端到端吞吐量就會更高。啟用 TC 前後應固定 client、server ID、協定與時間區間各跑多次；只要沒有明確收益，就維持 `kernel-direct-ebpf: false`。
