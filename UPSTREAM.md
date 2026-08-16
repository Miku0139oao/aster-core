# Upstream Policy

The Aster Core baseline is MetaCubeX/mihomo `v1.19.29` at commit `e26714a181ac0e2fa803453c0a8e9a9ce94e31cb`.

The `upstream` Git remote must point to `https://github.com/MetaCubeX/mihomo.git` without embedded credentials. Upstream updates are merged deliberately and must preserve Aster-specific management, security, and interoperability tests.

Protocol behavior may be compared against sing-box and Xray-core, but Aster Core exposes Mihomo YAML and the Clash-compatible controller API only. Code copied from another project must retain its applicable license and source revision.

## Local patches to upstream code

These fixes are carried in Aster Core only and are not submitted upstream. A merge that touches the same code must keep them, or the defect returns.

- `transport/vless/vision`: connection state is synchronized across both relay directions. Upstream lets `FilterTLS` mutate the TLS-sniffing fields from the read and the write path alike, and lets `Upstream`, `FrontHeadroom` and the `Replaceable` predicates read those fields from the opposite direction's goroutine, which the race detector reports as 29 races in the VLESS inbound tests.
- `transport/anytls/session`: `Stream.dieErr` is read atomically, because it is assigned inside `dieOnce` while `Read` and `Write` read it from whichever goroutine owns that direction.
- `listener/shadowsocks`: the package-level fallback listener is an `atomic.Pointer`, because `New` publishes it while `HandleShadowSocks` reads it from connection-handling goroutines.
- `component/updater`: archive member names are reduced to their base name and checked for containment before unpacking, so a release artifact cannot write outside the update directory.

### Why `listener/inbound` is still absent from the race job

After the fixes above, no data race reported by `go test -race ./listener/inbound` is owned by code in this repository. What remains is owned by dependencies, so it cannot be fixed here:

- `github.com/metacubex/restls-client-go` accounts for almost all of it. `Conn.extractRestlsAppData` on the read path and `Conn.writeRestlsApplicationRecord` / `Conn.write0x17AuthHeader` on the write path touch the same connection state without synchronizing, so every Restls test races.
- `github.com/metacubex/tls` and `github.com/metacubex/smux` share a buffer between `writeRecordLocked` and `smux`'s `sendLoop`.

Adding the package to the race job requires those modules to be fixed or replaced first.
