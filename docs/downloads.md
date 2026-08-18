# 下載

[English](/en/downloads)

Aster Core 目前以 GitHub 滾動預發行 [`Prerelease-main`](https://github.com/Miku0139oao/aster-core/releases/tag/Prerelease-main) 提供多平台二進位檔。**官方編號 `v*` 發行尚未發布**；請不要等待 `v1.x.x` tag，也不要從第三方鏡像或騰訊雲 dump 抓 Mihomo 充當 Aster。

上游基線是 Mihomo `v1.19.29`。每個 `Prerelease-main` 資產檔名含短 commit，例如 `alpha-main-98cb11f`；該字串會隨 `main` 更新，下載時以 Release 頁實際列出的檔名為準。

## 目前發行狀態

| 項目 | 現況 |
| --- | --- |
| 建議來源 | [`Prerelease-main`](https://github.com/Miku0139oao/aster-core/releases/tag/Prerelease-main) |
| 校驗清單 | 同一 tag 的 [`checksums.txt`](https://github.com/Miku0139oao/aster-core/releases/download/Prerelease-main/checksums.txt) |
| 全部發行 | [Releases](https://github.com/Miku0139oao/aster-core/releases) |
| 官方 `v*` | 尚未發布 |
| 上游基線 | Mihomo `v1.19.29` |

滾動 tag 的下載網址固定，內容會被新的 `main` 建置覆蓋：

```text
https://github.com/Miku0139oao/aster-core/releases/download/Prerelease-main/<資產檔名>
https://github.com/Miku0139oao/aster-core/releases/download/Prerelease-main/checksums.txt
```

檔名格式：

```text
aster-core-<os>-<arch>[-flavor]-alpha-main-<sha>.<ext>
```

例：

```text
aster-core-linux-amd64-v1-alpha-main-<sha>.gz
aster-core-linux-amd64-v1-alpha-main-<sha>.deb
aster-core-windows-amd64-v1-alpha-main-<sha>.zip
aster-core-darwin-arm64-alpha-main-<sha>.gz
aster-core-android-arm64-v8-alpha-main-<sha>.gz
```

同一個 Release 還會附上 `version.txt`、`vendor.tar.gz` 與 `toolchain.tar.gz`。一般使用者只需二進位檔或套件，以及 `checksums.txt`。

## 怎麼選作業系統與架構

先對 `<os>`，再對 CPU `<arch>`。不確定時優先選相容範圍最廣的組合，不要只看「amd64」三個字。

### 作業系統

| 你的系統 | 資產 `<os>` | 常見副檔名 |
| --- | --- | --- |
| Linux 桌面、VPS、大多數軟路由 | `linux` | `.gz`、`.deb`、`.rpm`、`.pkg.tar.zst` |
| Windows | `windows` | `.zip` |
| macOS | `darwin` | `.gz` |
| Android | `android` | `.gz`（獨立執行檔，不是 Play 商店 APK） |
| FreeBSD | `freebsd` | `.gz` |

### CPU 架構

| `uname -m` / 常見裝置 | 資產架構 | 備註 |
| --- | --- | --- |
| `x86_64` / `amd64` | `amd64` 系列 | 還要再選 v1／v2／v3，見下一節 |
| `aarch64` / `arm64` | `arm64` | Apple Silicon、多數 ARM 路由器與手機 |
| Android 64-bit ARM | `arm64-v8` | 只有 Android 資產用這個名字 |
| `i386` / `i686` | `386` | 32-bit x86 |
| `armv7l` / `armhf` | `armv7` | 32-bit ARMv7 |
| `armv6l` | `armv6` | 更舊的 32-bit ARM |
| 其他 | `riscv64`、`loong64`、`mips*`、`ppc64le`、`s390x` | Release 頁有對應檔名再下載 |

Linux 可再對一次套件架構：

```sh
uname -m
uname -s
dpkg --print-architecture   # Debian／Ubuntu
```

Windows PowerShell：

```powershell
[System.Runtime.InteropServices.RuntimeInformation]::OSArchitecture
```

macOS：

```sh
uname -m
# arm64 = Apple Silicon；x86_64 = Intel
```

### amd64 的 v1／v2／v3

x86-64 資產用 Go 的 `GOAMD64` 等級。等級愈高，指令集要求愈新；選錯會在啟動時直接 illegal instruction。

| 資產後綴 | 意義 | 什麼時候選 |
| --- | --- | --- |
| `amd64-v1` | 基準 x86-64，相容範圍最廣 | **預設。** 不確定 CPU、舊款主機、多數 OpenWrt x86 |
| `amd64-compatible` | 與 `amd64-v1` 相同（`GOAMD64=v1`） | 目前仍會產生；新下載優先使用較明確的 `amd64-v1` |
| `amd64-v2` | 需要 SSE4.2、POPCNT 等 | 已確認 CPU 支援時 |
| `amd64-v3` | 需要 AVX／AVX2／BMI 等 | 已確認較新桌面或伺服器 CPU 時 |
| 不帶後綴的 `amd64` | **同樣是 v3，不是通用 x86-64** | 只有確定要 v3 時才選 |

OpenWrt x86_64 軟路由優先 `linux-amd64-v1`，不要選不帶後綴的 `linux-amd64`。

## Linux

| 發行版／用途 | 選什麼 | 解壓或安裝後 |
| --- | --- | --- |
| 通用、OpenWrt 手動放入、VPS 自管 | `aster-core-linux-<arch>-alpha-main-<sha>.gz` | 單一執行檔 |
| Debian／Ubuntu | 同架構的 `.deb` | `/usr/bin/aster-core` |
| Fedora／RHEL／openSUSE | 同架構的 `.rpm` | `/usr/bin/aster-core` |
| Arch | 同架構的 `.pkg.tar.zst`（有提供才用） | `/usr/bin/aster-core` |

`.gz` 是壓縮後的單一 binary，不是原始碼 tarball：

```sh
gzip --decompress --stdout aster-core-linux-amd64-v1-alpha-main-<sha>.gz > aster-core
chmod 0755 aster-core
./aster-core -v
```

套件會安裝：

```text
/usr/bin/aster-core
/usr/bin/mihomo          # 相容用 alternatives
/etc/mihomo/config.yaml
aster-core.service
aster-core@.service
```

套件與 systemd 細節見 [Linux 套件與 systemd](/deployment/linux)；從 binary 做到正式服務見 [Linux VPS 部署](/tutorials/linux-production)。目前沒有官方編號版；該教學使用 `Prerelease-main` 與其 `version.txt` 所列的 `alpha-main-<sha>` 資產 build ID。

## Windows

下載 `.zip`，例如 `aster-core-windows-amd64-v1-alpha-main-<sha>.zip` 或 `aster-core-windows-arm64-alpha-main-<sha>.zip`。解壓後是 `aster-core.exe`：

```powershell
Expand-Archive .\aster-core-windows-amd64-v1-alpha-main-<sha>.zip -DestinationPath .
.\aster-core.exe -v
```

舊款 Intel／AMD 電腦用 `windows-amd64-v1`。Windows on ARM 用 `windows-arm64`。

## macOS

| 機器 | 資產 |
| --- | --- |
| Apple Silicon（M 系列） | `aster-core-darwin-arm64-alpha-main-<sha>.gz` |
| Intel | `aster-core-darwin-amd64-v1-alpha-main-<sha>.gz` |

```sh
gzip --decompress --stdout aster-core-darwin-arm64-alpha-main-<sha>.gz > aster-core
chmod 0755 aster-core
xattr -d com.apple.quarantine aster-core 2>/dev/null || true
./aster-core -v
```

瀏覽器下載的執行檔可能被 Gatekeeper 隔離；本機 `go build` 或已移除 quarantine 的檔案才可直接執行。

## Android

Android 資產是給 Termux、Magisk 模組或自行嵌入的獨立執行檔，不是應用程式商店安裝包。

| 裝置 | 資產 |
| --- | --- |
| 64-bit ARM 手機／平板（大多數） | `aster-core-android-arm64-v8-alpha-main-<sha>.gz` |
| 32-bit ARMv7 | `aster-core-android-armv7-alpha-main-<sha>.gz` |
| x86_64 模擬器或部分平板 | `aster-core-android-amd64-alpha-main-<sha>.gz` |
| 32-bit x86 | `aster-core-android-386-alpha-main-<sha>.gz` |

解壓後賦予執行權限，再以該平台的方式啟動。一般桌面或路由器請改下 Linux／Windows／macOS 資產。

## FreeBSD

```text
aster-core-freebsd-amd64-v1-alpha-main-<sha>.gz
aster-core-freebsd-arm64-alpha-main-<sha>.gz
aster-core-freebsd-386-alpha-main-<sha>.gz
```

用法與 Linux `.gz` 相同：解壓、`chmod +x`、執行 `-v`。

## OpenWrt 與 Nikki

OpenWrt 使用者有兩種官方做法，都從本專案的套件或 `Prerelease-main` 的 `linux-*` 資產取得 Aster，**不要用騰訊雲或其他第三方 dump 的 Mihomo 二進位檔**。

### 套件（建議）

把倉庫裡的 [`openwrt/aster-core`](https://github.com/Miku0139oao/aster-core/tree/main/openwrt/aster-core) 編進 OpenWrt 映像或 SDK，再安裝 `aster-core`。套件會：

- 把執行檔放到 `/usr/libexec/aster-core`
- 以 alternatives 提供 `/usr/bin/mihomo`（priority 400）
- 提供 virtual package `mihomo`，Nikki 不必改 init 或 LuCI

### 手動放入

x86_64 軟路由從 `Prerelease-main` 取 `aster-core-linux-amd64-v1-*.gz`；ARM64 路由器取 `aster-core-linux-arm64-*.gz`。不要選不帶 `v1`／`v2`／`v3` 後綴的 `linux-amd64`。

```sh
gzip --decompress --stdout aster-core-linux-amd64-v1-alpha-main-<sha>.gz > aster-core
chmod 0755 aster-core
/etc/init.d/nikki stop
cp /usr/libexec/aster-core /usr/libexec/aster-core.bak 2>/dev/null || true
cp aster-core /usr/libexec/aster-core
/usr/libexec/aster-core -v
/etc/init.d/nikki start
readlink -f /usr/bin/mihomo
```

`readlink` 應指向 `/usr/libexec/aster-core`。`-v` 仍可能顯示 `Mihomo Meta`，這是給 Nikki LuCI 解析的相容字串。

完整整合、Kernel DIRECT 與切換舊 Mihomo 套件見 [OpenWrt 與 Nikki](/deployment/openwrt)。

## Docker

CI 預定發布 `docker.io/miku0139oao/aster-core`，但目前沒有 Docker Hub credentials，映像尚未公開。請依 [Docker](/deployment/docker) 在本機建 `aster-core:local`。

## 驗證 checksum

先下載**同一個** `Prerelease-main` 的 [`checksums.txt`](https://github.com/Miku0139oao/aster-core/releases/download/Prerelease-main/checksums.txt)，再核對剛下載的檔案。checksum 不符就停止，不要改用不明鏡像重試。

Linux：

```sh
sha256sum --check --ignore-missing checksums.txt
```

macOS 若沒有 `sha256sum`：

```sh
shasum -a 256 aster-core-darwin-arm64-alpha-main-<sha>.gz
# 對照 checksums.txt 裡同一檔名那一行
```

Windows PowerShell：

```powershell
Get-FileHash .\aster-core-windows-amd64-v1-alpha-main-<sha>.zip -Algorithm SHA256
```

清單格式是 `SHA256  ./<檔名>`。目標檔名必須剛好出現一次。

## 下載之後

Unix：

```sh
chmod +x ./aster-core
./aster-core -v
```

Windows：

```powershell
.\aster-core.exe -v
```

接下來：

- 最小啟動：[快速開始](/guide/getting-started)
- 從節點做到 `curl` 驗證：[第一個代理設定](/tutorials/first-proxy)
- 設定欄位：[設定參考](/reference/configuration)

## 從原始碼建置

Release 建置已帶 `with_gvisor`。本機等價指令：

```sh
git clone https://github.com/Miku0139oao/aster-core.git
cd aster-core
go mod download
CGO_ENABLED=0 go build -tags with_gvisor -trimpath -o aster-core .
./aster-core -v
```

更多 target 見 [建置與測試](/development/build-test)。直接 `go build` 不會注入正式版號，不能拿來當 Release 替代品。
