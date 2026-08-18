# 文件正確性稽核

> 稽核日期：2026-08-19（Asia/Shanghai）。本次對照的程式碼與 `Prerelease-main` target 為 `98cb11f8e435`；GitHub release 與線上站點是會變動的外部狀態。這是測試紀錄，不是 Aster changelog 或版本發布聲明。

## 結論

本次找到並修正會直接導致安裝失敗或版本誤讀的敘述：不存在的 Aster `vX.Y.Z` assets、`.gz` 未解壓就執行、單檔下載卻用全清單 checksum 驗證、OpenWrt binary 路徑、尚未發布的 Docker image，以及虛構的 API SemVer。

目前只有 `Prerelease-main`，沒有 Aster `v*` 正式 release。Mihomo `v1.19.29` / `e26714a1` 是上游基線，不是 Aster 版本。

## 驗收清單

- [x] 以 GitHub API 列出 `Prerelease-main` 的每個 asset，並對照 `.github/workflows/build.yml:96`、`.github/workflows/build.yml:157` 與 `.github/workflows/build.yml:274`。
- [x] 逐項核對 quick start、Linux packages、raw binary、Docker 與 OpenWrt 的下載／安裝敘述。
- [x] 核對 `constant/version.go:5`、`NOTICE.md:5`、`NOTICE.md:6` 與 release 的 `version.txt`。
- [x] 核對 OpenWrt binary、alternative、priority 與 dependencies：`openwrt/aster-core/Makefile:32`、`openwrt/aster-core/Makefile:47`、`openwrt/aster-core/Makefile:50`。
- [x] 逐欄比對 kernel-direct schema、預設值、依賴條件、fallback 與 status response。
- [x] 比對 CLI flags、environment variables、generators、Age 與 rule-set converter。
- [x] 比對 outbound parser 與文件表，另核對 AmneziaWG v3/v3.1 欄位。
- [x] 比對 sniffer config 與 H2C/HTTP2、fragmented TLS/QUIC、QUIC v2、multi-round read 行為。
- [x] 查看線上頁面與未存在路由的 HTTP status。
- [x] 文件修正後執行 `npm run build` 與 `git diff --check`。

## Release 與 assets 實測

GitHub Releases API 在稽核時只回傳一個 release：

| 項目 | 實測值 |
| --- | --- |
| Tag / name | `Prerelease-main` |
| 類型 | prerelease，非 draft |
| Target commit | `98cb11f8e435216369f638fab61cf9d4063b8cc3` |
| `version.txt` | `alpha-main-98cb11f` |
| Assets 總數 | 81 |
| Raw `.gz` | 38 |
| Windows `.zip` | 7 |
| Debian `.deb` | 13 |
| RPM `.rpm` | 13 |
| Pacman `.pkg.tar.zst` | 6 |
| 輔助檔 | `checksums.txt`、`version.txt`、`toolchain.tar.gz`、`vendor.tar.gz` |

目前 prerelease raw asset 格式與 workflow 一致：非 Windows 為 `aster-core-<os>-<arch>-alpha-main-<sha7>.gz`，Windows 為同格式的 `.zip`（`.github/workflows/build.yml:100`、`.github/workflows/build.yml:175`、`.github/workflows/build.yml:178`）。Linux package 名稱使用同一 build ID（`.github/workflows/build.yml:188`、`.github/workflows/build.yml:204`、`.github/workflows/build.yml:220`）。

實際對 `linux-amd64-v1` 的 `.gz`、`.deb`、`.pkg.tar.zst`、`windows-amd64-v1` 的 `.zip` 與 `checksums.txt` 做 HEAD request，全部為 HTTP 200。`checksums.txt` 是全 release 清單（`.github/workflows/build.yml:296`），因此只下載一個 asset 時必須使用 `sha256sum --check --ignore-missing checksums.txt`，並確認目標檔確實有被驗證。

OpenWrt `.ipk` 不在 GitHub release assets 內；本 repository 只提供 `openwrt/aster-core/Makefile` 供 OpenWrt build environment 建置。文件沒有再把 `.ipk` 說成可從 `Prerelease-main` 下載的檔案。

## 已修正的事實錯誤

| 位置 | 原問題 | 修正與依據 |
| --- | --- | --- |
| `docs/guide/getting-started.md:18` | 只連到泛用 Releases，且對 `.gz` 直接 `chmod` | 直連 `Prerelease-main`，分清上游 baseline，先 checksum 再 `gzip -dc`；見 `.github/workflows/build.yml:178`。 |
| `docs/deployment/linux.md:28` | `sha256sum -c` 會將未下載的其他 release assets 全部報錯 | 改為 `--check --ignore-missing`，並限定在 asset 目錄執行。 |
| `docs/tutorials/first-proxy.md:70` | 把 OpenWrt binary 說成 `/usr/bin/aster-core` | 改為 `/usr/libexec/aster-core`，並說明 `/usr/bin/mihomo` alternative；見 `openwrt/aster-core/Makefile:32` 與 `openwrt/aster-core/Makefile:50`。 |
| `docs/tutorials/linux-production.md:59` | 使用不存在的 `vX.Y.Z` tag 組合 raw/package 檔名 | 分開 mutable tag `Prerelease-main` 與 immutable asset build ID `alpha-main-<sha7>`，版本目錄也使用 build ID。 |
| `docs/tutorials/linux-production.md:55` | 宣稱 `amd64-compatible` 預計移除，但 workflow 沒有 deprecation 資料 | 改為可驗證的現況：`amd64-compatible` 與 `amd64-v1` 都是 GOAMD64 v1。 |
| `docs/aster/api.md:39` | Response 範例虛構 Aster `v1.0.0` | 改為 `alpha-main-<sha7>` 格式，並說明來自 `constant.Version`；見 `hub/route/aster.go:161`。 |
| `docs/deployment/docker.md:3` 與 `README.md:315` | 把 `docker.io/miku0139oao/aster-core:latest` 說成已發布 image | 最新 Build run 顯示 Docker Hub credentials 為空並略過 push；Docker Hub API 與 manifest pull 也無法匿名取得。範例改用本機 `aster-core:local`。 |
| `docs/deployment/openwrt.md:21` | Target dependency 清單少了 `kmod-sched-bpf` | 補上 recipe 實際的 dependency；見 `openwrt/aster-core/Makefile:47`。 |
| `docs/deployment/openwrt.md:101` 與 `docs/deployment/openwrt.md:170` | 把 `tun_kernel_direct*` 與 `ipv6_outbound_fix` UCI 鍵寫成原版 Nikki 可用 | 公開 `nikkinikki-org/OpenWrt-nikki` 的 `main` commit `3799926b` 沒有這些鍵，本 repo 也只有文件出現。現已標成需要自訂 mixin，原版必須驗證生成的 YAML。 |
| `docs/reference/configuration.md:211` | 把 eBPF max entries 寫成 IPv4/IPv6 合計上限 | 實作是 IPv4 與 IPv6 兩個 map 各自使用設定值；見 `component/kerneldirect/fastpath_linux.go:216` 與 `component/kerneldirect/fastpath_linux.go:227`。 |

## 欄位與行為缺口

以下是缺少詳細文件，不是本次要擴寫的功能教學。

### AmneziaWG

`docs/reference/outbounds.md:17` 現已明確 AmneziaWG 不是新 outbound `type`；parser 仍是 `wireguard`（`adapter/parser.go:100`），啟用鍵為 `amnezia-wg-option`（`adapter/outbound/wireguard.go:69`）。

參考頁仍沒有逐欄解釋 `version`、`jc`、`jmin`、`jmax`、`s1`–`s4`、`h1`–`h4`、`i1`–`i5`、`j1`–`j3`、`itime`、`header-protection-key`、`content-padding-addition`、`rekey-after-time`、`rekey-timeout`、`reject-after-time`、`keepalive-timeout`、`max-handshake-attempts`、`random-trailers`、`disable-cookies`（`adapter/outbound/wireguard.go:88`）。不過 raw sample 已列出這些欄位（`docs/config.yaml:1310`），所以不是錯誤的 type 清單。

### Sniffer

新 sniffer 工作沒有新增 YAML 欄位；仍使用 `sniffer.sniff.HTTP`、`TLS`、`QUIC`（`config/config.go:394`、`docs/config.yaml:195`）。缺的是行為說明：

- HTTP sniffer 現可解 H2C/HTTP2 preface、HEADERS 與 HPACK authority（`component/sniffer/http_sniffer.go:192`）。
- TCP sniffer 可多輪讀取，上限 64 KiB，失敗後不應關閉原連線（`component/sniffer/dispatcher.go:24`、`component/sniffer/dispatcher.go:203`）。
- QUIC sniffer 支援 draft-29、v1 與 v2，並組合分片 CRYPTO data（`component/sniffer/quic_sniffer.go:69`、`component/sniffer/quic_sniffer.go:558`、`component/sniffer/quic_sniffer.go:628`）。

`docs/reference/mihomo-differences.md:3` 只補了簡短總結；完整參數與邊界可由 owner 在 changelog 或專頁補上。

### Kernel DIRECT

Core schema 沒有遺漏：`config/config.go:300` 到 `config/config.go:312` 定義了 `kernel-direct`、`kernel-direct-max-entries`，以及 `kernel-direct-ebpf`、`required`、`interfaces`、`mark`、`max-entries`、`proxy`、`proxy-redirect`、`proxy-mark`、`flow-entries`、`direct-prefixes`、`proxy-prefixes`。`docs/reference/configuration.md:172` 與 `docs/reference/configuration.md:207` 已經覆蓋這些欄位及 dependency rules。

`docs/config.yaml:145` 這份 raw sample 只有 `kernel-direct`，沒有其他 Aster kernel-direct 欄位。因此 `docs/reference/configuration.md:23` 已改成「廣泛範例」，不再宣稱每個 Aster 新欄位都完整收錄；欄位清單以參考頁為準。

## Kernel DIRECT / OpenWrt 對照結果

- `kernel-direct` 只在 Linux auto-route + auto-redirect + nftables 成立；parser 與 listener 都會拒絕不符條件的設定（`config/config.go:1783`、`listener/sing_tun/server.go:158`）。
- Learned address 容量預設 4096，上限 65536，`0` 回到預設（`component/kerneldirect/controller.go:20`、`component/kerneldirect/options.go:5`）。
- Fake-IP、private、loopback、link-local 與非 global-unicast 不會學習（`component/kerneldirect/controller.go:240`）；共用 IP 採 proxy-wins（`component/kerneldirect/controller.go:354`）。
- DNS 與 live flow 都可學習；live flow 會 unwrap group，`Direct` 與 `Compatible` 才是 DIRECT（`tunnel/tunnel.go:785`、`tunnel/tunnel.go:804`）。
- Rule/proxy/mode/provider 變更會 flush learned set（`component/kerneldirect/controller.go:164`）。
- eBPF 不是基本 Kernel DIRECT 的必要條件；建立失敗且 `required` 為 false 時回到 nftables（`listener/sing_tun/server.go:587`）。
- Status 未有 fast path 時回 `backend: nftables`，並回傳 `learned_sets`、`process`、`aster_traffic` 與 deprecated alias `proxy_traffic`（`hub/route/traffic_control.go:55`）。
- OpenWrt recipe 安裝 `/usr/libexec/aster-core`，提供 virtual `mihomo`，並以 priority 400 建立 `/usr/bin/mihomo` alternative（`openwrt/aster-core/Makefile:32`、`openwrt/aster-core/Makefile:48`、`openwrt/aster-core/Makefile:50`）。

## 其他對照通過項目

- `docs/reference/cli.md:5` 的主 flags 與 `main.go:60` 一致；generators 與 `component/generator/cmd.go:16` 一致；Age subcommands 與 `component/age/age.go:164` 一致。
- `docs/reference/outbounds.md:5` 的 outbound `type` 清單與 `adapter/parser.go:30` 到 `adapter/parser.go:205` 完整對應。AmneziaWG 是 `wireguard` option，不應額外發明一個 type。
- `NOTICE.md:5` 與 `NOTICE.md:6` 的 baseline 對應 tag `v1.19.29` 指向的 commit `e26714a181ac0e2fa803453c0a8e9a9ce94e31cb`。
- `constant/version.go:5` 的 `1.10.0` 是未注入 ldflags 的開發預設；OpenWrt `PKG_VERSION` / `PKG_RELEASE` 也是 package build metadata。兩者都不是 GitHub 上已發布的 Aster `v*` release。

## Owner 後續項目

- 線上 `/downloads`、`/changelog`、`/en/` 在稽核時都是 HTTP 404；本分支不新增 owner 已規劃的 Downloads、Changelog 或 English 頁面。
- Downloads 不應把 `Prerelease-main` 當 immutable version。應顯示 release API 當下 target SHA / `version.txt`，再產生帶 `alpha-main-<sha7>` 的 asset URL。
- Changelog 必須把 Mihomo baseline `v1.19.29` 與 Aster build ID 分開，不得發明 Aster `v1.19.29`、`v1.0.0` 或 `vX.Y.Z`。
- 若要支援文件內的 Nikki UCI 鍵，需公開／固定實作這些鍵的 Nikki patch；否則保留現在的「自訂 mixin」警告。
- Docker Hub credentials 實際設定並成功 push 前，不得恢復 `docker pull ...:main` 或 `:latest` 的安裝文案。

## 驗證命令

```text
gh api repos/Miku0139oao/aster-core/releases/tags/Prerelease-main
gh run view 32169278767 --repo Miku0139oao/aster-core
go test ./component/kerneldirect ./config ./hub/route
cd docs && npm run build
git diff --check
```

本次結果：上述 Go targeted tests、VitePress production build 與 whitespace check 全部通過。
