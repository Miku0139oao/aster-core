# Aster 完整變更、修正與效能優化

這一頁列出 Aster Core 相對上游 Mihomo 的**可驗證變更**。重點不是只說「更快」或「修了很多 bug」，而是交代問題、影響、Aster 的處理方式，以及可以在哪個 commit、檔案或測試找到證據。

## 比較基準

截至 **2026-07-29**，重新抓取 `MetaCubeX/mihomo` 的 `Meta` 分支後，最新 commit 與 Aster 的上游基線相同：

```text
Mihomo tag:    v1.19.29
Mihomo commit: e26714a181ac0e2fa803453c0a8e9a9ce94e31cb
Aster fork:    35f45516941f9ae8040734f27acabbe93364fb0d
Aster optimize:676c4e7290b15b1ca9e426411ab2a52ea820ae2a
```

因此下面標示「上游仍存在」的項目，是指在上述 Mihomo `Meta` 最新 commit 中仍可看到原始行為，而 Aster 已帶有對應 patch。多數項目附有直接 regression test；尚無直接測試者會明確標示程式入口或間接驗證，不把「有實作」誇大成「每個分支都已測」。未來上游若再更新，應重新比較，不能把這份清單當成永久不變的結論。

可自行重現基線確認：

```sh
git fetch upstream Meta
git rev-parse upstream/Meta
git diff --stat e26714a1..35f45516
git show --stat 676c4e72
```

::: info 如何閱讀這個 fork 的大型 diff
Go module 從 `github.com/metacubex/mihomo` 改為 `github.com/Miku0139oao/aster-core`，因此很多檔案只有 import path 或格式變動。以下清單刻意只收錄具功能、正確性、安全、生命週期或效能影響的差異，不把機械式 rename 算成「修正」。
:::

## 摘要

| 類別 | Aster 的主要變更 |
| --- | --- |
| 新資料平面能力 | AnyTLS listener/outbound 的 REALITY、URI 匯入與訂閱輸出 |
| 管理能力 | VLESS/AnyTLS 即時使用者 CRUD、revision、流量、訂閱與安全 state |
| 一般問題修正 | Listener close/reload、Controller lifecycle、安全 transport、DNS relay、Hysteria UDP、VLESS packet、XHTTP、TrustTunnel、跨協定 cleanup、Core updater |
| 效能 | O(1) user lookup、目標 listener 局部 clone、原子流量計數、批次持久化、增量連線統計、精簡 JSON、較少封包組裝開銷 |
| 安全與一致性 | Store lock/generation/backup/ACL、checksum、降版防護、same-origin、fail-closed、敏感欄位 redaction |
| 品質閘門 | Race、lifecycle、rollback、security、benchmark、跨 Go 版本與 interoperability tests |

## Aster 新增的功能

### AnyTLS + REALITY

Mihomo `v1.19.29` 的 AnyTLS 已支援憑證 TLS、ShadowTLS、ResTLS 與 JLS，但沒有把 REALITY 接到 AnyTLS。Aster 新增：

- AnyTLS listener `reality-config`。
- AnyTLS outbound `reality-opts` 與 `client-fingerprint`。
- `anytls://` 的 `security=reality`、`pbk`、`sid`、`sni`、`fp`。
- 從 server private key 推導 public key，再輸出受管使用者訂閱。
- REALITY handshake 與動態 password 撤銷、pending connection、listener close 的生命週期整合。

設定與端到端操作請看[從零部署 AnyTLS + REALITY](/tutorials/anytls-reality)，欄位定義請看[AnyTLS + REALITY 參考](/reference/anytls-reality)。

### VLESS 與 AnyTLS 即時使用者管理

Aster 為具名 listener 增加 `ManagedUserListener` 邊界，目前支援：

- VLESS：UUID、flow、enabled。
- AnyTLS：password、enabled。
- 不重建 listener 的 create、update、disable、delete。
- Credential 改變時撤銷受影響的 pending/active connection，但保留未受影響的連線。
- 每 listener revision 與 applied revision。
- 每 user upload、download、active connection 與 traffic generation。

管理平面刻意與 Clash Controller API 分開，使用自己的 `/api/admin`、Bearer secret 與權限條件。

### 持久化與訂閱

Aster 新增 `aster-state.json`：

- 跨程序 exclusive lock。
- generation compare-and-swap，拒絕 stale writer。
- temp file、fsync、atomic replace。
- primary 與 `.bak` 交叉恢復。
- 16 MiB 讀寫上限。
- 拒絕 symlink、非 regular file 與不安全父目錄。
- Unix `0600` 與 owner-only 檢查；Windows DACL 驗證與修正。

每個 eligible user 可取得可輪替 `/sub/aster/{token}`。Token 有反向索引，舊 token 在輪替後立即失效，response 使用 `Cache-Control: no-store`。

## Mihomo 最新基線仍存在、Aster 已修正的問題

### 1. Context listener 關閉時可能留下 in-flight connection

**上游行為**

`common/net/listener.go` 的 handler goroutine 可能仍阻塞在原始連線或準備把結果送入 channel。只 cancel context 與關閉底層 listener，不能保證：

- handler 忽略 context 時 `Accept` 仍能退出；
- 已 accept、尚未完成 handshake 的 raw connection 被關閉；
- cancel 後才成功產生的 wrapped connection 被關閉；
- 非 comparable `net.Conn` 不因 interface 比較而 panic。

**Aster 修正**

- 明確追蹤 raw in-flight connections 與 handler `WaitGroup`。
- `Close` 會 cancel、關閉 raw connection 並讓 `Accept` 立即回傳 `net.ErrClosed`。
- cancel 後產生的 wrapped/raw connection 都會安全清理。
- 只在 concrete type 可比較時判斷兩個 connection 是否相同。
- `ConnectionTrackingListener` 支援關閉已接收連線，並處理 Accept/Close race。

**回歸測試**

`common/net/listener_test.go`：

- `TestHandleContextListenerCloseClosesInFlightConnection`
- `TestHandleContextListenerCloseUnblocksAcceptWhenHandlerIgnoresCancellation`
- `TestHandleContextListenerClosesSuccessfulResultAfterCancellation`
- `TestHandleContextListenerAcceptsNonComparableConnection`
- `TestConnectionTrackingListenerClosesAcceptThatLosesCloseRace`

### 2. Inbound reload 缺少確定性 rollback

**上游行為**

舊 `PatchInboundListeners` 逐一關閉舊 listener、啟動新 listener；錯誤只寫 log。當 port 需要從舊 listener 轉移給新 listener、其中一個 bind 失敗，或 close 本身失敗時，runtime map 可能與實際 socket 狀態不一致。

**Aster 修正**

- 依名稱排序，先決定需要停止與替換的集合。
- 確定性 pre-close，讓 address transfer 不依賴 map iteration 順序。
- 任一 close/listen 失敗時，反向關閉已啟動 replacement，再恢復已乾淨關閉的舊 listener。
- 聚合 close、restore 與 rollback error，不吞錯。
- 若 replacement cleanup 失敗，保留可追蹤的 runtime entry，避免遺失仍在監聽的 socket。

這是具錯誤回報的 staged replacement 與 **best-effort rollback**，不是保證完全原子的 transaction。若 replacement close 或舊 listener restore 本身失敗，Aster 會保留實際 runtime entry 並回報聚合錯誤，不會假裝已恢復原狀；pre-close 已關閉的既有連線也不可能復原。

**回歸測試**

`listener/listener_test.go`：

- `TestPatchInboundListenersPreclosesInDeterministicOrderForAddressTransfer`
- `TestPatchInboundListenersFailedListenRestoresOnlyCleanedReplacements`
- `TestPatchInboundListenersAggregatesPrecloseErrors`
- `TestPatchInboundListenersKeepsReplacementWhenRollbackCloseFails`

### 3. 跨 listener/service 的 constructor 與 close lifecycle 問題

**上游行為**

多地址 listener 若第一個 bind 成功、後續 bind 或 post-bind security validation 失敗，已建立的 TCP/UDP listener 不一定被回收；部分 legacy close 也不是 idempotent。

**Aster 修正**

- Trojan、Shadowsocks、VMess、Hysteria2、Hysteria2 Realm、ShadowQUIC、TUIC、TrustTunnel 及 slice-backed inbound 在 constructor error 時關閉先前資源。
- 關閉同時回收 TCP、UDP、HTTP/H3 server、TLS/raw listener 與 accept loop，並聚合所有 close error。
- 使用 `sync.Once`／atomic closed state，重複 close 不重複釋放。
- sing Hysteria2 的 `service.Start` 改為同步取得錯誤並 rollback，不再從 goroutine 忽略啟動失敗。
- TrustTunnel graceful shutdown 失敗時再 force close，並清理 HTTP/H3/TCP/UDP reference。

**回歸測試**

- `listener/trojan/server_cleanup_test.go`
- `listener/shadowsocks/tcp_test.go`
- `listener/inbound/lifecycle_test.go`
- `listener/inbound/common_test.go`

其他協定的實作入口包括 `listener/sing_hysteria2/server.go`、`listener/hysteria2_realm/server.go`、`listener/sing_vmess/server.go`、`listener/shadowquic/server.go`、`listener/tuic/server.go`、`listener/sing_shadowsocks/server.go` 與 `listener/trusttunnel/server.go`；不是每一個 cleanup error branch 都有獨立 regression test。

### 4. Hysteria UDP 分片會混淆交錯 session

**上游行為**

Defragger 只保留一個 `msgID` 與單一 fragment slice，沒有把 `SessionID` 納入 key。不同 UDP session 的同號訊息交錯時，fragment 可能被跨 session 合併、誤當重複而丟棄；另一個 message ID 到達時，也會清掉上一組尚未完成的重組狀態。衝突 fragment、無效 count 與過大組合亦缺少完整保護。

**Aster 修正**

- 以 `(sessionID, msgID)` 作為重組 key。
- 同時追蹤最多 64 個 pending UDP message。
- 驗證 fragment count、index、host、port、重複 fragment 內容與總大小。
- 拒絕無法以 8-bit fragment count 表示的分片。
- 完成後才組裝，衝突時丟棄整組 pending state。

64 筆上限使 pending state 有界，但目前沒有 TTL；到達上限時會淘汰其中一筆，不能視為完整的 timeout/reassembly policy。

**回歸測試**

- `TestDefraggerHandlesInterleavedSessions`
- `TestDefraggerRejectsConflictingFragments`
- `TestFragUDPMessageRejectsUnrepresentableFragments`

### 5. Hysteria client UDP、reconnect 與 port hopping correctness

**上游行為**

- `DialUDP` 寫出的 `ClientRequest` 把 `UDP` 留成 `false`。
- Reconnect 前後共用 session map 與 defragger 時，舊 QUIC session 的 handler/callback 可能操作 replacement generation。
- Fragmented message ID 使用隨機值，並行時缺少 per-session sequence。
- Port hopping 啟用時，程式先呼叫一次 `ListenPacket`，之後才建立 hopping packet connection；前一個 socket 沒有成為實際 hopping connection 的一部分。

**Aster 修正**

- `DialUDP` 明確送出 `UDP: true`。
- 每一代 QUIC session 使用自己的 session map 與 defragger，handler/Close callback 捕捉同一代資料。
- Fragment message ID 使用 per-session atomic sequence 並跳過 0。
- 無法以 8-bit count 表示的分片保留原始 `DatagramTooLargeError`。
- 有 `serverPorts` 時直接建立 `NewObfsUDPHopClientPacketConn`，不先開一次普通 socket。

**回歸測試**

- `TestQUICPacketConnPortHoppingOpensOneInitialSocket`
- `TestWriteClientRequestUDPFlag`

Reconnect generation 與 atomic message ID 目前沒有直接 regression test；程式入口位於 `transport/hysteria/core/client.go`。

### 6. VLESS packet stream 的 frame 邊界可能被破壞

**上游行為**

- 呼叫端 buffer 太小時直接回傳 `io.ErrShortBuffer`，但沒有排掉該 frame 的剩餘 payload，下一次讀取會把 payload 當成 length header。
- 超過 `uint16` 的 packet length 會截斷。
- 同一 connection 的 concurrent read/write 沒有 frame-level serialization，header 與 payload 可能交錯。
- header/payload 分兩次一般 write，錯誤或 short write 後沒有 fail-closed。

**Aster 修正**

- Short buffer 時完整 drain 當前 frame，再讓下一次 read 從正確 header 開始。
- 明確拒絕大於 65535 bytes 的 packet。
- read/write 各自以 mutex 維護 frame 原子性。
- 使用 `net.Buffers` 寫入 header + payload，檢查 short write，錯誤時關閉 connection。
- partial header/body read 失敗後關閉 connection，避免繼續解析失步 stream。

**回歸測試**

- `TestPacketConnReadFromDrainsShortBufferFrame`
- `TestPacketConnWriteToRejectsOversizedPacket`
- `TestPacketConnSerializesConcurrentWrites`
- `TestPacketConnSerializesConcurrentReads`

### 7. XHTTP close 可能卡在 blocked write，舊 session 也可能刪掉 replacement

**上游行為**

- Server connection 的 `Close` 先等寫入 mutex；若 `ResponseWriter.Write` 正在阻塞，close 也會永久等待。
- Client connection 的 close、deadline timer 與 callback 不是 idempotent/race-safe。
- 舊 connection 延遲觸發 close callback 時，只按 session ID 刪除，可能把已用同 ID 建立的新 session 一起刪掉。

**Aster 修正**

- Close 先標記 closed、關閉 done，並透過 `ResponseController.SetWriteDeadline` 要求支援該能力的 HTTP writer 中斷 blocked write，再等待 writer 離開。
- `sync.Once`、atomic closed 與 deadline mutex 保證只釋放一次。
- 刪除 session 時同時比對 expected session pointer，只能刪除原本那一代。

**回歸測試**

- `TestHTTPServerConnCloseInterruptsBlockedWrite`
- `TestConnCloseIsIdempotentAndDoesNotDeleteReplacementSession`

### 8. TrustTunnel HTTP connection 重複 close 與 timer race

**上游行為**

重複 close 可能多次關閉 writer/body、重複 cancel/callback；deadline timer 與 close 同時執行時缺少同步，close 後仍可設定 deadline。

**Aster 修正**

使用 `sync.Once`、atomic closed 與 deadline mutex；close 停止 timer，close 後 `SetDeadline` 回傳 `net.ErrClosed`。

**回歸測試**

`TestHTTPConnCloseIsIdempotentAndStopsDeadline`。

### 9. DNS relay 壓縮後可能沒有寫回 caller 的 target buffer

**上游行為**

`dns.Msg.PackBuffer(target)` 可能因未壓縮大小判斷而另行配置，即使最終壓縮 response 可放入 target。UDP hijack caller 依賴原 target backing array，會看到未更新或錯誤資料。

**Aster 修正**

如果 pack 回傳了另一個 backing array，而壓縮結果能放入 target，明確 copy 回 target 並回傳 target slice。

**回歸測試**

`TestRelayDnsPacketCopiesCompressedReplyIntoTarget` 以 100 筆可壓縮 A record 驗證 backing array 與內容。

### 10. Core updater 缺少完整 release 與下載驗證

**上游行為**

- metadata/package HTTP status 沒有統一要求 2xx。
- metadata 沒有大小上限。
- 下載只靠 size limit，沒有驗證 release SHA-256。
- stable channel version 沒有 canonical/prerelease 驗證。
- Stable/release channel 的非 force 更新可接受降版。
- 剛好等於大小上限的合法檔案可能被誤判。

**Aster 修正**

- 只接受 2xx，限制 metadata 與 package 大小。
- 從 checksum metadata 精確匹配 package name，下載後驗證 SHA-256。
- Stable channel 只接受 canonical、非 prerelease semver。
- 除非 force，拒絕 downgrade。
- 使用 `limit + 1` 判定真正超限，允許剛好位於上限的檔案。
- Release build 嵌入正確 asset 名稱，避免架構／variant 推測錯誤。

**直接回歸測試**

- `TestCoreBaseNameUsesEmbeddedReleaseAsset`
- `TestParsePackageChecksum`
- `TestValidateReleaseUpdate`

HTTP status、metadata/package size、實際 SHA mismatch 與剛好位於 size limit 的分支目前沒有各自的直接 regression test；行為入口位於 `component/updater/update_core.go`。這些限制只描述 core binary updater，不代表 geo/UI downloader 已套用同一套驗證。

### 11. Controller reload 與本機 transport 的安全生命週期

**上游行為**

- 舊 controller goroutine 可能在新設定套用後回寫或關閉 replacement server。
- Async server goroutine 直接讀取可被後續修改的 config/CORS slice。
- `/debug` 掛在 authentication group 外。
- Unix controller 沒有使用 Controller secret，socket 權限為 `0666`；Windows named pipe 也未使用 secret，預設 ACL 允許 Builtin Users 讀寫。
- `SetUIPath("")` 不能真正停用既有 UI。

**Aster 修正**

- 使用 `serverGen` 隔離 reload generation，並追蹤、關閉實際 HTTP/TLS/Unix/pipe listener。
- 啟動前 clone config 與 CORS slice，避免 async data race。
- 把 `/debug` 移到 Controller secret authentication group 內。
- Unix controller 目錄使用 `0700`、socket 使用 `0600` 並套用 Controller secret。
- Windows pipe 套用 Controller secret，預設 ACL 收緊為 owner、Administrators 與 SYSTEM。
- 空 UI path 明確移除 UI route。

`TestSetUIPathEmptyDisablesUI` 直接驗證 UI disable；其餘 reload/security 分支目前以 `hub/route/server.go`、`adapter/inbound/listen_windows.go` 為主要證據入口。

### 12. 一般 connection statistic 的 memory data race

上游 `tunnel/statistic.Manager.memory` 使用普通 `uint64`，背景 `updateMemory()` 與 Snapshot/Memory consumer 可能同時讀寫。Aster 改為 `atomic.Uint64`，讓這個一般 Mihomo statistic 路徑不再依賴未同步讀寫。實作入口位於 `tunnel/statistic/manager.go`。

### 13. Managed VLESS/AnyTLS credential 更新的 race 與鎖定

這部分是 Aster 管理功能所需，但修正發生在實際 listener/authentication 路徑：

- 完整建立 immutable credential snapshot 後才一次 publish，失敗不暴露半套 user map。
- 拒絕 duplicate UUID/password。
- Handshake 開始時記錄 credential generation，完成前再次確認仍有效。
- 更新或 close 會撤銷 pending handshake 與已被移除／換 credential 的 active connection。
- 不在持有 `usersMu` 時呼叫可能阻塞的 connection close，避免管理操作互相卡死。
- 未受變更的 credential 與 active connection 保留。
- Listener close 後不能被 `UpdateUsers` 重新開啟。

**回歸測試**

- `listener/anytls/server_test.go`
- `listener/anytls/server_lifecycle_test.go`
- `listener/sing_vless/service_test.go`
- `listener/sing_vless/server_lifecycle_test.go`

### 14. 流量計數可能把失敗的 buffered write 算成成功

**上游行為**

TCP tracker 的 `WriteBuffer` 在底層 write 回傳錯誤後仍會增加 upload total。

**Aster 修正**

只有底層 `WriteBuffer` 成功才增加全域與 per-principal upload；失敗直接回傳，不製造不存在的流量。

## 效能優化

### 1. User lookup：線性掃描改為索引

原本 `GetUser`、update、delete、reset traffic、subscription 等操作會跨 listener/user slice 尋找 ID。Aster 建立：

```text
user ID -> { inbound, index }
subscription token -> user ID
```

每次 listener mutation commit 後只重建該 listener 的 index。常見單一 user lookup 從隨 user 數增加的掃描，改為平均 O(1) map lookup。

一次本機 benchmark sample（2026-07-29、Windows amd64、Ryzen 9 5900X、`-benchtime=300ms`；不同 run 會浮動）：

| User 數 | `Manager.GetUser` | Allocation |
| ---: | ---: | ---: |
| 100 | 約 52.4 ns/op | 0 |
| 1,000 | 約 55.6 ns/op | 0 |
| 10,000 | 約 53.0 ns/op | 0 |

User 數增加 100 倍時，lookup 延遲仍維持同一量級。Benchmark 位於 `component/aster/manager_test.go`。

### 2. Mutation：完整 store deep clone 改為目標 listener clone

每次只修改一個 managed listener。Aster 的 `cloneStoreForListener`：

- 複製頂層 maps。
- 其他 listener 沿用 immutable pointer。
- 只 deep-copy 被修改 listener 與其 users。
- Subscription map 保持獨立 copy，避免 candidate 變更污染目前 store。

這減少「修改一名使用者」時複製所有其他 listener/user object 的 CPU 與 allocation。Commit 前仍會驗證 candidate、套用 runtime、持久化；任何一步失敗會 rollback/fail-closed。

### 3. 流量熱路徑：atomic counter，不同步寫磁碟

每次 tracker upload/download：

1. 取得 immutable runtime pointer。
2. 以 atomic recorder admission 避免與 runtime retire 交錯。
3. 更新 user 專屬 atomic counter。
4. 設 dirty flag。

不會在每個 packet：

- deep-copy store；
- marshal JSON；
-取得全域 manager mutex；
- fsync state file。

流量預設每 5 分鐘批次 flush。Runtime swap、reconfigure 與 disable 會先停止舊 runtime 接受新 recorder、等待已進入者完成，再同步計數；程序 shutdown 則先解除 statistic observer，再執行 final Flush。

本機 benchmark：

| 路徑 | 結果 | Allocation |
| --- | ---: | ---: |
| `Manager.RecordTraffic` | 約 23.6 ns/op | 0 |
| Parallel `RecordTraffic` | 約 79.7 ns/op | 0 |
| 一般全域 upload counter | 約 3.55 ns/op | 0 |
| `PushUploadedFor` unauthenticated fast path | 約 3.79 ns/op | 0 |

這些是 microbenchmark，不等同真實網路 throughput；它們用來防止管理 accounting 熱路徑日後退化成有 allocation 或同步 I/O。

### 4. 一次取得一致的管理 snapshot

Overview/list users 原本會分別讀 listener、user、traffic，再建立多份資料。Aster 新增 `ManagementSnapshot`：

- 一次鎖定與 traffic sync。
- 同一 revision 下取得 listeners 與 users。
- API 不再重複掃描與 clone。

這同時改善效能與一致性，避免 response 中 user 與 inbound revision 來自不同瞬間。

### 5. Active connection 改為增量 per-principal 計數

舊管理 API 為顯示每位使用者活動連線，會抓完整 connection snapshot，再逐筆掃描 metadata。

Aster 在 tracker `Join`/`Leave` 時維護：

```text
{ inbound, user ID } -> active connection count
```

查詢只複製小型 principal count map，不需要建立完整 connection view。`LoadOrStore`/`LoadAndDelete` 也避免重複 Join/Leave 造成 double count。

### 6. Store 寫入使用 compact JSON

State file 從 `json.MarshalIndent` 改為 `json.Marshal`：

- 減少 encode CPU 與中間輸出大小。
- 降低 state file 寫入量與 16 MiB 上限壓力。
- 檔案仍是一般 JSON，可用 `jq` pretty-print 查閱。

### 7. Hysteria UDP serialization 減少額外工作

`udpMessage.Pack` 預先配置最終大小，直接用 `binary.BigEndian.PutUint*` 與 `copy` 填入，不再在已配置 slice 上建立 `bytes.Buffer` 再逐欄 `binary.Write`。

Port hopping 的 pool/queue buffer 也從 `[]byte` 改成 `*[udpBufferSize]byte`，減少 slice header/interface boxing 的熱路徑負擔。這是 allocation-shape 微優化，尚無獨立 benchmark，不能推導固定百分比加速。

### 8. VLESS packet 使用 serialized vectored-write path

VLESS packet header 與 payload 使用 `net.Buffers.WriteTo` 組成同一 frame write path。對支援 vectored I/O 的底層連線，可合併 header/payload 的系統呼叫；所有底層連線都由 `writeMu` 保證 frame 不與其他 writer 交錯，並保留 short-write 檢查與 fail-closed。

## 安全與可靠性強化

| 項目 | Aster 行為 |
| --- | --- |
| Admin secret | 與 Controller secret 分離，至少 32 bytes |
| Admin transport | 明文 TCP 只允許 loopback 掛載；HTTPS/Unix/pipe 可用 |
| Controller debug | `/debug` 位於 Controller secret authentication 內 |
| Local controller | Unix `0700`/`0600`；Windows pipe owner/admin/SYSTEM ACL |
| Browser request | Admin mutation 做 same-origin 驗證 |
| Request size | JSON body 上限 1 MiB |
| Credential response | List 不輸出 UUID/password；single-user detail 才輸出 |
| State path | Lock、generation、regular-file、symlink、owner/ACL 檢查 |
| Persistence | Atomic replacement、backup recovery、fsync、大小上限 |
| Runtime apply | Staged apply、rollback；rollback 失敗時 fail-closed |
| Subscription | 高熵 token、輪替、`no-store`、不存在時統一 404 |
| Updater | 2xx、size、SHA-256、stable semver、downgrade guard |

## 發行、相容與 CI 變更

- Go module、binary、Docker image、native packages 改為 `aster-core`。
- `-v` 保留 `Mihomo Meta` 前綴，Linux/OpenWrt 保留 `mihomo` compatibility path。
- 多平台 release、deb/rpm/pkg.tar.zst、Docker multi-arch、Nix 與 OpenWrt/Nikki package。
- Go 1.20–1.26 測試矩陣。
- 管理、store security、Windows ACL、listener lifecycle、rollback、race 與 benchmark tests。
- 固定 lint 工具版本並執行 `govet`、`staticcheck`、`gci`、`gofumpt`。
- 正式 build 與 test 覆蓋 `with_gvisor`。

## 不是所有差異都代表上游 bug

為避免混淆，以下屬於產品選擇或 Aster 專用能力：

- `/api/admin`、`aster-state.json`、managed users 與 subscription 是 Aster 新功能，不是上游「漏修」。
- `type: relay` 移除是 Mihomo/Aster 相容方向的一部分，不應描述成效能修正。
- Module/import path、release name、logo、README 與 package metadata 是專案識別。
- 大量 `gofumpt`、error string 與 lint 修正是品質維護，不應各自宣稱為 runtime bug。
- Microbenchmark 只證明特定函式的複雜度、allocation 與回歸門檻，不代表所有協定在所有網路都會有固定百分比加速。

## Commit 與驗證入口

| Commit | 內容 |
| --- | --- |
| [`35f45516`](https://github.com/Miku0139oao/aster-core/commit/35f45516941f9ae8040734f27acabbe93364fb0d) | Fork、一般問題修正、AnyTLS/VLESS 管理、store、安全、發行與 tests |
| [`676c4e72`](https://github.com/Miku0139oao/aster-core/commit/676c4e7290b15b1ca9e426411ab2a52ea820ae2a) | User index、局部 clone、traffic/runtime、snapshot、connection count 與 lifecycle 優化 |
| [`8d0d1d90`](https://github.com/Miku0139oao/aster-core/commit/8d0d1d9076f4b36bb6cb181e8b9ee3b3f60fd879) | CI、lint、race 與 code-quality gate |

執行重點驗證：

```sh
# 一般修正與 Aster tests
SKIP_INTEROP_TEST=1 go test ./... -count=1

# Race
SKIP_INTEROP_TEST=1 go test -race ./component/aster ./hub/route \
  ./listener/anytls ./listener/sing_vless ./tunnel/statistic

# Management microbenchmarks
go test ./component/aster -run '^$' \
  -bench 'Benchmark(ManagerGetUser|CloneStoreForListener|ManagerRecordTraffic)' \
  -benchmem -benchtime=300ms

# Tracker hot path
go test ./tunnel/statistic -run '^$' \
  -bench 'BenchmarkManagerPushUploaded' \
  -benchmem
```

如果只想先理解使用上的差異，回到[Aster 與 Mihomo 差異](/reference/mihomo-differences)；若要開始實際部署，從[實戰教學](/tutorials/)進入。
