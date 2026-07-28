# Upstream Policy

The Aster Core baseline is MetaCubeX/mihomo `v1.19.29` at commit `e26714a181ac0e2fa803453c0a8e9a9ce94e31cb`.

The `upstream` Git remote must point to `https://github.com/MetaCubeX/mihomo.git` without embedded credentials. Upstream updates are merged deliberately and must preserve Aster-specific management, security, and interoperability tests.

Protocol behavior may be compared against sing-box and Xray-core, but Aster Core exposes Mihomo YAML and the Clash-compatible controller API only. Code copied from another project must retain its applicable license and source revision.
