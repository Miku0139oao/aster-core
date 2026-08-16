# Upstream Policy

Aster Core is an independent project. MetaCubeX/mihomo is a historical starting point, not a release or CI parent.

The recorded baseline is MetaCubeX/mihomo `v1.19.29` at commit `e26714a181ac0e2fa803453c0a8e9a9ce94e31cb`. The `upstream` remote may still point at `https://github.com/MetaCubeX/mihomo.git` for occasional reference merges. Those merges are deliberate and must keep Aster-specific management, security, and interoperability tests.

CI and release binaries use official Go (`actions/setup-go`). They do not download MetaCubeX patched toolchains or ship the old-OS compatibility matrix inherited from mihomo.

Protocol behavior may be compared against sing-box and Xray-core, but Aster Core exposes Mihomo YAML and the Clash-compatible controller API only. Code copied from another project must retain its applicable license and source revision.
