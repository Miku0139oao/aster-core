# Aster 比 Mihomo 快多少？

## 先看結論

Aster 沒有用魔法把你的網路變快，而是把代理核心內大量重複的小工作拿掉。以下是在實際 OpenWrt 軟路由上，相同程式與測試各跑三輪的中位數：

| 你會遇到的工作 | Aster 快多少 | 為什麼會快 |
| --- | ---: | --- |
| TCP 搬運 32 KiB 資料 | **吞吐約高 2%**、處理時間約少 2% | 重複使用搬運資料的暫存空間 |
| AnyTLS 打包 1 KiB | **約快 2.0 倍** | 直接寫進可重用 buffer，不再多包一層物件 |
| AnyTLS 打包 16 KiB | **約快 1.4 倍** | 同上，並減少小型記憶體申請 |
| 準備一個 UDP 封包 | **約快 5.4 倍** | 封包資料結構用完收回，下次直接重用 |
| 忽略一行已關閉的 debug log | **約快 101 倍** | 在組字串、建立 log 前就直接跳過 |

> [!IMPORTANT]
> **最接近「實際搬資料」的是 TCP 的約 2% 改善。** 5.4 倍和 101 倍只代表核心裡某個很小、但會執行非常多次的步驟，不代表下載速度會直接變成 5.4 倍或 101 倍。

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

## 2026-08-24 review wave 本機驗證

這一輪以 `8462a265` 為基底的未發布工作樹，對 Mihomo `v1.19.30`（`ac017cdd`）重新建立獨立 Windows amd64 test binary。每個樣本都是新程序，固定 `GOMAXPROCS=1`、`-test.cpu=1`、`GOAMD64=v1`、Go 1.26.3、每輪 2 秒；Aster／Mihomo 交錯 7 輪。這是 5900X 開發機的 regression validation，不取代下方 OpenWrt 主要結果，也不是 WAN 吞吐量。

| 核心工作 | Mihomo 1.19.30 中位數（範圍） | Aster 中位數（範圍） | 配置量 |
| --- | ---: | ---: | ---: |
| UDP metadata | 54.44 ns（49.80–71.77） | 12.22 ns（11.76–12.48），**4.45×** | 416 B／1 → 0 B／0 |
| 停用 debug log | 206.3 ns（204.3–245.5） | 2.132 ns（2.096–3.063），**96.8×** | 24 B／1 → 0 B／0 |
| AnyTLS frame 1 KiB | 72.55 ns（66.80–82.65） | 31.67 ns（30.93–33.76），**2.29×** | 64 B／1 → 0 B／0 |
| AnyTLS frame 16 KiB | 322.8 ns（320.4–354.0） | 244.3 ns（218.4–277.3），**1.32×** | 64 B／1 → 0 B／0 |
| 共用 relay 32 KiB | 4.038 µs（4.005–4.157） | 3.916 µs（3.763–4.477） | 64 B／1 → 0 B／0 |
| tunnel TCP relay 32 KiB | 4.079 µs（3.892–4.666） | 4.016 µs（3.840–4.198） | 64 B／1 → 0 B／0 |

兩個 TCP relay 的範圍重疊，所以這輪只確認 **Aster 維持零配置，沒有宣稱桌面 TCP 顯著加速**。UDP、log 與 AnyTLS 的配置量和方向則與 OpenWrt 結果一致。

同一工作樹內、同一台機器的 review 前後回歸 benchmark 顯示：

| Aster-only hot path | Review 前 | Review 後 | 結果 |
| --- | ---: | ---: | ---: |
| Kernel DIRECT 既有 flow refresh | 15.286 µs；64 B／1 alloc | 270.8 ns；0 B／0 alloc | **56.4×**；未到期時不再全表掃描 |
| 合併後 100k 個不連續 CIDR 的 miss | 2.385 ms；0 alloc | 114.7 ns；0 alloc | **約 20,790×**；恢復二分搜尋 |
| UDP association 新增 1,000 個 mapping | 41.31 ms；46.37 MB | 486.3 µs；542.6 KiB | **84.9×**；移除每次完整 map copy |

Windows 完整核心、相同最小設定、啟動後 15 秒的五輪中位數：Aster working set 19.00 MiB、Mihomo 18.07 MiB（Aster +0.93 MiB／+5.2%）；private bytes 52.52 MiB 對 51.29 MiB（+1.23 MiB／+2.4%）。這些是 Windows 指標，不能與 Linux RSS/PSS 混用；也再次證明不能宣稱 Aster 空載一定比較省 RAM。

## OpenWrt 實機比較（主要結果）

比較基線為 Mihomo `v1.19.30`、commit `ac017cdd246ce8bd547653d927e7bf77d7ee73d5`。Aster 為 `0590d3a4` 當時的 `main`（含這波 review／修正與 128 KiB frame pool）。兩個版本各自以相同 Go 版本、target、flags 與 benchmark harness 建立 `go test -c` Linux amd64 binary，依序執行三輪，每輪至少 2 秒；表格採三輪中位數。2026-08-19 在同一台 OpenWrt 軟路由、於修正落地後重跑。

| 環境項目 | 實際值 |
| --- | --- |
| 系統 | OpenWrt，Linux 6.6.86，x86-64 |
| CPU | Ryzen 7 5825U 宿主，VM 配置 12 vCPU |
| CPU 頻率 | 測試前客體取樣平均 2.342 GHz。這是 `/proc/cpuinfo` 頻率取樣，Hypervisor 未保證恆定 |
| 記憶體 | VM 配置約 6 GB（`MemTotal` 6081752 kB） |
| DRAM 頻率 | Hypervisor 未提供，**無法取得** |
| Go | 1.26.3，`GOAMD64=v1` 交叉編譯的 Linux amd64 test binary |
| 測試前 load average | 0.07／0.03／0.00（1／5／15 分鐘） |
| 測試後 load average | 0.92／0.37／0.14（1／5／15 分鐘） |

| 核心工作 | Mihomo 1.19.30 中位數 | Aster 中位數 | Aster 相對結果 |
| --- | ---: | ---: | ---: |
| UDP packet metadata | 70.00 ns；416 B／1 alloc | 12.89 ns；0 B／0 alloc | **5.43× faster**；消除 416 B 配置 |
| 停用的 debug log | 228.0 ns；24 B／1 alloc | 2.254 ns；0 B／0 alloc | **101× faster**；消除 event 配置 |
| AnyTLS frame（1 KiB） | 71.54 ns；64 B／1 alloc | 35.98 ns；0 B／0 alloc | **1.99× faster**；延遲降低 50% |
| AnyTLS frame（16 KiB） | 267.1 ns；64 B／1 alloc | 196.8 ns；0 B／0 alloc | **1.36× faster**；延遲降低 26% |
| AnyTLS frame（64 KiB） | 無同名 bench | 1.148 µs；0 B／0 alloc | 獨立 `WriteDataFrame/65536` fixture 在 128 KiB pool 後為零配置 |
| TCP relay（32 KiB） | 4.546 µs；7.21 GB/s；64 B／1 alloc | 4.449 µs；7.37 GB/s；0 B／0 alloc | 延遲降低 **2.1%**；吞吐提高 **2.2%** |

### 三輪測量範圍

| Benchmark | Mihomo 1.19.30 範圍 | Aster 範圍 |
| --- | ---: | ---: |
| UDP packet metadata | 69.74–72.12 ns/op | 12.84–13.00 ns/op |
| 停用的 debug log | 223.6–231.5 ns/op | 2.230–2.270 ns/op |
| AnyTLS frame（1 KiB） | 71.35–71.69 ns/op | 35.76–36.89 ns/op |
| AnyTLS frame（16 KiB） | 264.9–271.8 ns/op | 195.2–216.4 ns/op |
| TCP relay（32 KiB） | 4.538–4.576 µs/op | 4.423–4.505 µs/op |

這台 5825U 軟路由仍比許多 MT7621、低階 ARM 或廉價 VPS 強。這組結果只能證明優化在實際 OpenWrt 環境仍有效；越弱的裝置，絕對 GB/s 一定更低，改善比例也必須在該裝置重新測量。

## Hyper-V 低階資源模擬（次要結果）

為了更接近大部分使用者的弱硬體，我們在同一台 Hyper-V OpenWrt VM 裡再加一層資源限制。Benchmark 只准使用 **1 顆 vCPU**，而且每 100 ms 最多只能執行 25 ms，相當於 **單核 25% CPU 時間**；記憶體上限設為 **512 MiB**，並停用 swap。2026-08-19 用 Mihomo `v1.19.30` 與同一組 Linux amd64 test binary 重跑。

這是資源不足情境的模擬，不是 MT7621 或 ARM 指令集模擬。它能回答「CPU 很慢、記憶體很少時，Aster 的優化是否仍存在」，但不能取代真實低階路由器測試。

| 環境項目 | 實際值 |
| --- | --- |
| 系統 | Hyper-V 上的 OpenWrt，Linux 6.6.86，x86-64 |
| 宿主 CPU | Ryzen 7 5825U |
| CPU 限制 | 綁定 vCPU 0；cgroup `cpu.max = 25000 100000`，即單核 25% CPU 時間 |
| 客體回報 CPU 頻率 | 未限速段開始前平均 2.342 GHz。這是頻率取樣，**不包含 25% CPU 時間限制** |
| 記憶體限制 | 512 MiB、無 swap |
| DRAM 頻率 | Hypervisor 未提供，**無法取得** |
| Go | 1.26.3，Linux amd64 test binary |
| 測試前 load average | 0.07／0.03／0.00（與未限速段同一輪開始前） |
| 測試後 load average | 0.31／0.31／0.14（1／5／15 分鐘） |
| 節流確認 | 3,709 個 CPU quota periods 中有 3,660 個發生 throttling |

兩個版本依序跑相同 benchmark，各三輪、每輪至少 2 秒。以下採三輪中位數：

| 核心工作 | Mihomo 1.19.30 中位數 | Aster 中位數 | Aster 相對結果 |
| --- | ---: | ---: | ---: |
| UDP packet metadata | 251.5 ns；416 B／1 alloc | 48.01 ns；0 B／0 alloc | **5.24× faster**；消除 416 B 配置 |
| 停用的 debug log | 854.3 ns；24 B／1 alloc | 8.054 ns；0 B／0 alloc | **約 106× faster**；消除 event 配置 |
| AnyTLS frame（1 KiB） | 331.0 ns；64 B／1 alloc | 139.6 ns；0 B／0 alloc | **2.37× faster**；延遲降低 58% |
| AnyTLS frame（16 KiB） | 1.080 µs；64 B／1 alloc | 790.8 ns；0 B／0 alloc | **1.37× faster**；延遲降低 27% |
| TCP relay（32 KiB） | 17.644 µs；1.86 GB/s；64 B／1 alloc | 16.812 µs；1.95 GB/s；0 B／0 alloc | 延遲降低 **4.7%**；吞吐提高 **5.0%** |

### 峰值記憶體佔用

先把兩種「記憶體佔用」分開看：

| 情境 | Mihomo 1.19.30 | Aster | 結論 |
| --- | ---: | ---: | --- |
| 完整核心、最小設定、空載 15 秒的 RSS | 34.7 MiB | 39.3 MiB | Aster 多 4.6 MiB，約 **+13%** |

完整核心測試使用相同設定：Direct 模式、無代理、無規則、靜默 log、IPv6 關閉、TUN 關閉、只綁 127.0.0.1 未使用埠；Aster 與 Mihomo 各三輪，每輪啟動後等待 15 秒。RSS 三輪範圍為 Aster 38.5–39.3 MiB、Mihomo 34.7–35.0 MiB。OpenWrt 核心沒有提供 `smaps_rollup`，因此沒有 PSS。只報 `/proc/<pid>/status` 的 `VmRSS`。

**所以不能說 Aster 空載比較省 RAM。** Aster 加入更多功能與程式碼，最小設定的常駐底座目前比 Mihomo 1.19.30 多約 4.6 MiB。下面的獨立程序峰值只代表「重複執行某個 hot path 的 Go 測試程式」當時的 Max RSS，不是完整代理掛著規則、GeoIP、DNS cache 和連線時的總 RAM。

### 核心 hot path 的獨立程序峰值

每個案例改用獨立程序重跑三輪，透過 Linux `/usr/bin/time -v` 記錄 Max RSS。表格採三輪中位數；兩邊都套用相同的單核 25% CPU 時間與 512 MiB 記憶體限制。

| 核心工作 | Mihomo 1.19.30 峰值 RSS | Aster 峰值 RSS | 差異 |
| --- | ---: | ---: | ---: |
| TCP relay（32 KiB） | 40.5 MiB | 51.8 MiB | Aster 多 **28%** |
| 停用的 debug log | 33.5 MiB | 23.0 MiB | **少 31%** |
| UDP packet metadata | 53.0 MiB | 53.4 MiB | 持平 |
| AnyTLS frame（1 KiB） | 35.4 MiB | 26.0 MiB | **少 27%** |
| AnyTLS frame（16 KiB） | 34.8 MiB | 25.5 MiB | **少 27%** |

對 1.19.30 重跑後，**不能再寫「五個案例都少 30–49%」**。停用 log 與 AnyTLS 仍明顯較低；UDP 持平；TCP 這個獨立測試程序的峰值反而較高。這比較的是該 package 測試程式的暫存壓力，不能外推成「Aster 在任何設定下整體 RAM 都比較小」。Aster 測試程式比 Mihomo 大（含更多套件），RSS 比較的是整個 Go test binary，不是單一 frame。

三輪範圍分別為：TCP Mihomo 39.5–40.5 MiB、Aster 51.5–52.0 MiB；log Mihomo 32.5–33.5 MiB、Aster 固定 23.0 MiB；UDP Mihomo 50.8–53.9 MiB、Aster 53.0–54.9 MiB；AnyTLS 1 KiB Mihomo 35.0–35.5 MiB、Aster 固定 26.0 MiB；AnyTLS 16 KiB Mihomo 34.7–35.1 MiB、Aster 25.5–26.0 MiB。所有測試皆為 0 swap。

### 受限環境三輪範圍

| Benchmark | Mihomo 1.19.30 範圍 | Aster 範圍 |
| --- | ---: | ---: |
| UDP packet metadata | 244.3–256.4 ns/op | 47.85–48.21 ns/op |
| 停用的 debug log | 854.1–856.9 ns/op | 7.984–8.157 ns/op |
| AnyTLS frame（1 KiB） | 323.4–335.5 ns/op | 135.5–155.2 ns/op |
| AnyTLS frame（16 KiB） | 1.075–1.092 µs/op | 772.3–801.6 ns/op |
| TCP relay（32 KiB） | 17.598–17.756 µs/op | 16.771–16.841 µs/op |

受限後的絕對處理時間約變成原本的四倍，證明 CPU quota 確實生效；Aster 在五項延遲工作中仍全部較快。**在這個相同 x86 VM 的 CPU quota 實驗裡**，TCP 改善由未限速的 2.1% 增至 4.7%，AnyTLS 1 KiB 由 1.99 倍增至 2.37 倍。UDP 倍率則由 5.43 倍略降到 5.24 倍；這不能外推成「所有較弱硬體都會有更大提升」，ARM／MIPS、cache 與記憶體頻寬仍須實機重測。首頁仍採用未限速 OpenWrt 對 Mihomo 1.19.30 的 **TCP 約 2%**，不使用這組較好看的 4.7% 當主要宣傳數字。

## 各協議 loopback 測試

核心 microbenchmark 變快，不等於每一種協議的端到端吞吐都會同步增加。為了驗證這點，我們先前在 Linux Docker host network 上，讓 Aster 與 Mihomo 連到相同的本機服務端，以 16 KiB chunk 分別測量讀寫。下表仍是對 Mihomo `v1.19.29` 的結果；這次沒有用 `v1.19.30` 重跑協議服務端。

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

這組協議測試仍是先前對 Mihomo `v1.19.29` 的 Docker loopback，**沒有**用 `v1.19.30` 重跑。這次 5900X 主機有 Docker Engine，但沒有當時釘死的協議服務端映像，補拉映像再比一次端到端吞吐，無法對上原 SHA。這些協議的加密與搬運路徑兩邊共用，1.19.30 的 microbenchmark 已覆蓋有改動的 relay／UDP／log／AnyTLS frame；協議表只用來說明「沒有讓所有加密協議一起大幅變快」，**不作為首頁宣傳數字**。

服務端映像固定為 shadowsocks-rust `sha256:85d01d…e1359`、V2Fly `sha256:e81a07…de78c`、Trojan `sha256:5b36c2…b98b5f7`、OpenSnell `sha256:70053f…345467`、Hysteria v1.3.5 `sha256:4c8c92…f1e35`。

### UDP metadata 配置前後

同一支 benchmark 同時量測 Mihomo 的直接建立 metadata 路徑與 Aster 的物件池路徑，因此可以做同機比較：

| 路徑 | 時間 | 記憶體配置 |
| --- | ---: | ---: |
| Mihomo 1.19.30 每個封包直接建立 | 69.74–72.12 ns/op | 416 B/op、1 alloc/op |
| Aster metadata pool | 12.84–13.00 ns/op | 0 B/op、0 allocs/op |

物件池路徑以三輪中位數計算約快 **5.43 倍**，並消除每封包 416 bytes 的 heap allocation。

## 高階開發機結果（附錄）

最初的 Aster 絕對數據是在高階桌機上取得，只適合做開發期回歸檢查，不應拿來代表一般路由器。2026-08-19 在同一台 5900X 上，對 Mihomo `v1.19.30` 與 Aster `main` 交錯重跑同一組 microbenchmark（各三輪、每輪至少 2 秒）：

| 環境項目 | 實際值 |
| --- | --- |
| 系統 | Windows 11 amd64；平衡電源模式 |
| CPU | Ryzen 9 5900X，12 cores／24 logical CPUs |
| 回報頻率 | Windows `\Processor Frequency` 計數器固定 3701 MHz（標稱時脈，**不是**實際 boost） |
| DRAM | 64 GB（4×16 GB）G.Skill DDR4-3600，configured 3600 MT/s |
| Go | 1.26.3 windows/amd64 |
| 測試前 CPU load | 各項開始前取樣平均約 30%（17.6–60.9%） |
| 測試後 CPU load | 各項結束後取樣平均約 34%（20.3–54.8%） |
| processor queue | 全程 0 |

背景負載仍然不低，所以 5900X **不是**首頁的主要比較；它只用來確認桌面開發機沒有明顯回退。同一天稍早一輪因背景約 42–47% 且未交錯，TCP 數字互相打架，已丟棄。修正落地後的交錯重跑：

| 核心工作 | Mihomo 1.19.30 中位數 | Aster 中位數 | Aster 相對結果 |
| --- | ---: | ---: | ---: |
| UDP packet metadata | 152.4 ns；416 B／1 alloc | 11.91 ns；0 B／0 alloc | **12.8× faster**；消除 416 B 配置 |
| 停用的 debug log | 455.3 ns；24 B／1 alloc | 2.268 ns；0 B／0 alloc | **201× faster**；消除 event 配置 |
| AnyTLS frame（1 KiB） | 74.99 ns；64 B／1 alloc | 34.70 ns；0 B／0 alloc | **2.16× faster** |
| AnyTLS frame（16 KiB） | 260.0 ns；64 B／1 alloc | 184.0 ns；0 B／0 alloc | **1.41× faster** |
| AnyTLS frame（64 KiB） | 無同名 bench | 1.056 µs；0 B／0 alloc | 獨立 `WriteDataFrame/65536` fixture 為零配置 |
| TCP relay（32 KiB） | 10.044 µs；3.26 GB/s；64 B／1 alloc | 11.436 µs；2.87 GB/s；0 B／0 alloc | 範圍重疊；此輪 Aster 中位數較慢，**不以 5900X TCP 下加速結論** |

三輪範圍：UDP Mihomo 145.8–155.1 ns、Aster pool 11.69–12.21 ns；log Mihomo 442.9–456.5 ns、Aster 2.227–2.352 ns；AnyTLS 1 KiB Mihomo 74.12–77.73 ns、Aster 33.21–36.36 ns；AnyTLS 16 KiB Mihomo 257.9–271.6 ns、Aster 182.9–200.4 ns；TCP Mihomo 9.963–11.705 µs、Aster 10.394–11.947 µs。32 KiB `Relay32KiBComparison` 中位數 Aster 9.332 µs 對 Mihomo 10.775 µs。桌面 TCP 在背景負載下波動大，首頁仍用 OpenWrt。

## 如何重跑

從 repository 根目錄執行：

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

對照 Mihomo 時請使用 tag `v1.19.30`（`ac017cdd`）建置相同的 Linux amd64 test binary，在同一台機器依序跑三輪。不要用不同機器的單次結果推算百分比。

## 數據如何解讀

- 這些是 in-process microbenchmarks，主要量測 Aster Core 自身的額外開銷。
- TCP 與 AnyTLS 的 GB/s 是記憶體／`net.Pipe` 路徑，不是 WAN 實際吞吐量。
- 真實代理速度還會受到加密、RTT、丟包、MTU、NIC、作業系統、CPU 架構與服務端影響。
- 獨立 `WriteDataFrame` fixture 的 1 KiB、16 KiB 與 64 KiB case 為零配置（64 KiB 靠 128 KiB 物件池）；這不代表完整 AnyTLS session 的所有路徑都零配置。更大或不對齊的 buffer 仍可能配置。
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
