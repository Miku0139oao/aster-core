# Downloads

[繁體中文](/downloads)

Aster Core currently ships multi-platform binaries on the rolling GitHub prerelease [`Prerelease-main`](https://github.com/Miku0139oao/aster-core/releases/tag/Prerelease-main). **No official numbered `v*` Aster release has been published yet.** Do not wait for a `v1.x.x` tag, and do not substitute a third-party mirror or Tencent Cloud dump of Mihomo for Aster.

The upstream baseline is Mihomo `v1.19.29`. Each `Prerelease-main` asset name includes a short commit, such as `alpha-main-98cb11f`. That suffix changes as `main` moves; use the filename listed on the release page.

## Current release status

| Item | Status |
| --- | --- |
| Recommended source | [`Prerelease-main`](https://github.com/Miku0139oao/aster-core/releases/tag/Prerelease-main) |
| Checksums | [`checksums.txt`](https://github.com/Miku0139oao/aster-core/releases/download/Prerelease-main/checksums.txt) on the same tag |
| All releases | [Releases](https://github.com/Miku0139oao/aster-core/releases) |
| Official `v*` | Not published yet |
| Upstream baseline | Mihomo `v1.19.29` |

The rolling-tag URLs stay stable; the payload is replaced by newer `main` builds:

```text
https://github.com/Miku0139oao/aster-core/releases/download/Prerelease-main/<asset-name>
https://github.com/Miku0139oao/aster-core/releases/download/Prerelease-main/checksums.txt
```

Filename pattern:

```text
aster-core-<os>-<arch>[-flavor]-alpha-main-<sha>.<ext>
```

Examples:

```text
aster-core-linux-amd64-v1-alpha-main-<sha>.gz
aster-core-linux-amd64-v1-alpha-main-<sha>.deb
aster-core-windows-amd64-v1-alpha-main-<sha>.zip
aster-core-darwin-arm64-alpha-main-<sha>.gz
aster-core-android-arm64-v8-alpha-main-<sha>.gz
```

The same release also publishes `version.txt`, `vendor.tar.gz`, and `toolchain.tar.gz`. Everyday installs only need the binary or package plus `checksums.txt`.

## How to pick OS and architecture

Match `<os>` first, then the CPU `<arch>`. When unsure, pick the most compatible flavor. Do not treat a bare `amd64` token as “generic x86-64”.

### Operating system

| Your system | Asset `<os>` | Typical extension |
| --- | --- | --- |
| Linux desktop, VPS, most soft routers | `linux` | `.gz`, `.deb`, `.rpm`, `.pkg.tar.zst` |
| Windows | `windows` | `.zip` |
| macOS | `darwin` | `.gz` |
| Android | `android` | `.gz` (standalone binary, not a Play Store APK) |
| FreeBSD | `freebsd` | `.gz` |

### CPU architecture

| `uname -m` / typical device | Asset arch | Notes |
| --- | --- | --- |
| `x86_64` / `amd64` | `amd64` family | Still choose v1 / v2 / v3 below |
| `aarch64` / `arm64` | `arm64` | Apple Silicon, most ARM routers and phones |
| Android 64-bit ARM | `arm64-v8` | Android assets only |
| `i386` / `i686` | `386` | 32-bit x86 |
| `armv7l` / `armhf` | `armv7` | 32-bit ARMv7 |
| `armv6l` | `armv6` | Older 32-bit ARM |
| Other | `riscv64`, `loong64`, `mips*`, `ppc64le`, `s390x` | Download only if the release lists that name |

On Linux, confirm both kernel and package architecture:

```sh
uname -m
uname -s
dpkg --print-architecture   # Debian / Ubuntu
```

Windows PowerShell:

```powershell
[System.Runtime.InteropServices.RuntimeInformation]::OSArchitecture
```

macOS:

```sh
uname -m
# arm64 = Apple Silicon; x86_64 = Intel
```

### amd64 v1 / v2 / v3

x86-64 assets are built with Go `GOAMD64` levels. A higher level needs a newer instruction set; the wrong one dies at startup with an illegal instruction.

| Asset suffix | Meaning | When to use it |
| --- | --- | --- |
| `amd64-v1` | Baseline x86-64, widest compatibility | **Default.** Unknown CPUs, older hosts, most OpenWrt x86 boxes |
| `amd64-compatible` | Same as `amd64-v1` (`GOAMD64=v1`) | Legacy name, planned for removal; new downloads should use `amd64-v1` |
| `amd64-v2` | Needs SSE4.2, POPCNT, and related features | Only after you confirm the CPU |
| `amd64-v3` | Needs AVX / AVX2 / BMI and related features | Only on confirmed newer desktop or server CPUs |
| Bare `amd64` (no suffix) | **Also v3, not generic x86-64** | Only when you intentionally want v3 |

For OpenWrt x86_64, prefer `linux-amd64-v1`. Do not pick unsuffixed `linux-amd64`.

## Linux

| Distro / use | Choose | After install |
| --- | --- | --- |
| Generic, OpenWrt drop-in, self-managed VPS | `aster-core-linux-<arch>-alpha-main-<sha>.gz` | Single binary |
| Debian / Ubuntu | Matching `.deb` | `/usr/bin/aster-core` |
| Fedora / RHEL / openSUSE | Matching `.rpm` | `/usr/bin/aster-core` |
| Arch | Matching `.pkg.tar.zst` when published | `/usr/bin/aster-core` |

A `.gz` asset is a compressed binary, not a source tarball:

```sh
gzip --decompress --stdout aster-core-linux-amd64-v1-alpha-main-<sha>.gz > aster-core
chmod 0755 aster-core
./aster-core -v
```

Packages install:

```text
/usr/bin/aster-core
/usr/bin/mihomo          # compatibility alternative
/etc/mihomo/config.yaml
aster-core.service
aster-core@.service
```

Package and systemd details: [Linux packages and systemd](/deployment/linux). Building a production unit from a raw binary: [Linux VPS deployment](/tutorials/linux-production). That tutorial still shows a `vX.Y.Z` placeholder; until an official `v*` exists, substitute the `alpha-main-<sha>` asset name from `Prerelease-main`.

## Windows

Download a `.zip` such as `aster-core-windows-amd64-v1-alpha-main-<sha>.zip` or `aster-core-windows-arm64-alpha-main-<sha>.zip`. The archive contains `aster-core.exe`:

```powershell
Expand-Archive .\aster-core-windows-amd64-v1-alpha-main-<sha>.zip -DestinationPath .
.\aster-core.exe -v
```

Use `windows-amd64-v1` on older Intel/AMD PCs. Use `windows-arm64` on Windows on ARM.

## macOS

| Machine | Asset |
| --- | --- |
| Apple Silicon (M series) | `aster-core-darwin-arm64-alpha-main-<sha>.gz` |
| Intel | `aster-core-darwin-amd64-v1-alpha-main-<sha>.gz` |

```sh
gzip --decompress --stdout aster-core-darwin-arm64-alpha-main-<sha>.gz > aster-core
chmod 0755 aster-core
xattr -d com.apple.quarantine aster-core 2>/dev/null || true
./aster-core -v
```

Browser-downloaded binaries may be quarantined by Gatekeeper. A local `go build` or a copy with quarantine removed will run.

## Android

Android assets are standalone binaries for Termux, Magisk modules, or embedding. They are not store APKs.

| Device | Asset |
| --- | --- |
| 64-bit ARM phones / tablets (most devices) | `aster-core-android-arm64-v8-alpha-main-<sha>.gz` |
| 32-bit ARMv7 | `aster-core-android-armv7-alpha-main-<sha>.gz` |
| x86_64 emulator or some tablets | `aster-core-android-amd64-alpha-main-<sha>.gz` |
| 32-bit x86 | `aster-core-android-386-alpha-main-<sha>.gz` |

Decompress, mark the file executable, and start it the way that platform expects. Desktops and routers should use the Linux, Windows, or macOS assets instead.

## FreeBSD

```text
aster-core-freebsd-amd64-v1-alpha-main-<sha>.gz
aster-core-freebsd-arm64-alpha-main-<sha>.gz
aster-core-freebsd-386-alpha-main-<sha>.gz
```

Treat these like the Linux `.gz` assets: decompress, `chmod +x`, then run `-v`.

## OpenWrt and Nikki

OpenWrt users have two official paths. Both start from this repository’s package or a `linux-*` asset on `Prerelease-main`. **Do not drop in a Tencent Cloud or other third-party Mihomo dump.**

### Package (preferred)

Build [`openwrt/aster-core`](https://github.com/Miku0139oao/aster-core/tree/main/openwrt/aster-core) with the OpenWrt SDK or image builder and install `aster-core`. The package:

- installs the binary at `/usr/libexec/aster-core`
- registers `/usr/bin/mihomo` through alternatives (priority 400)
- provides the virtual `mihomo` package, so Nikki needs no init or LuCI changes

### Manual drop-in

On x86_64 soft routers take `aster-core-linux-amd64-v1-*.gz` from `Prerelease-main`. On ARM64 routers take `aster-core-linux-arm64-*.gz`. Do not use unsuffixed `linux-amd64`.

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

`readlink` should resolve to `/usr/libexec/aster-core`. `-v` may still print `Mihomo Meta`; Nikki’s LuCI backend parses that compatibility string.

Full integration, Kernel DIRECT, and switching from an older Mihomo package: [OpenWrt and Nikki](/deployment/openwrt).

## Docker

CI is intended to publish `docker.io/miku0139oao/aster-core`, but Docker Hub credentials are not configured, so that image is not public yet. Build `aster-core:local` from [Docker](/en/deployment/docker).

## Verify checksums

Download [`checksums.txt`](https://github.com/Miku0139oao/aster-core/releases/download/Prerelease-main/checksums.txt) from the **same** `Prerelease-main` release, then check the file you just fetched. Stop if the digest does not match; do not retry from an unknown mirror.

Linux:

```sh
sha256sum --check --ignore-missing checksums.txt
```

macOS if `sha256sum` is missing:

```sh
shasum -a 256 aster-core-darwin-arm64-alpha-main-<sha>.gz
# Compare with the matching line in checksums.txt
```

Windows PowerShell:

```powershell
Get-FileHash .\aster-core-windows-amd64-v1-alpha-main-<sha>.zip -Algorithm SHA256
```

The list format is `SHA256  ./<filename>`. The target name must appear exactly once.

## After download

Unix:

```sh
chmod +x ./aster-core
./aster-core -v
```

Windows:

```powershell
.\aster-core.exe -v
```

Next:

- Minimal start: [Getting started](/guide/getting-started)
- Node setup through `curl` verification: [First proxy](/tutorials/first-proxy)
- Field reference: [Configuration](/reference/configuration)

## Build from source

Release builds include `with_gvisor`. A local equivalent:

```sh
git clone https://github.com/Miku0139oao/aster-core.git
cd aster-core
go mod download
CGO_ENABLED=0 go build -tags with_gvisor -trimpath -o aster-core .
./aster-core -v
```

More targets: [Build and test](/development/build-test). A plain `go build` does not inject the release version and is not a substitute for a published asset.
