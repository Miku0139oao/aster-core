# Documentation accuracy audit

The 2026-08-19 audit compared the docs against `Prerelease-main` (`98cb11f8`), `constant/version.go`, OpenWrt packaging, and kernel-direct code.

Confirmed:

- Only `Prerelease-main` exists; there is no official Aster `v*` release.
- Mihomo `v1.19.29` / `e26714a1` is the upstream baseline, not an Aster version.
- Docker Hub `miku0139oao/aster-core` is not published yet.
- OpenWrt installs `/usr/libexec/aster-core` and the `mihomo` alternative.

The full findings table is in the [Traditional Chinese accuracy record](/development/docs-accuracy).
