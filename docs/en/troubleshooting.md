# Troubleshooting

::: tip A symptom-driven runbook
If you need a practical order from `-t`, sockets, journal, DNS, REALITY, Controller, Aster state, through to UDP, use the [troubleshooting handbook](/en/tutorials/troubleshooting).
:::

## Configuration not found or the wrong file is read

Specify the paths explicitly:

```sh
aster-core -d /absolute/home -f /absolute/config.yaml -t
```

A relative `-f` is resolved from the current working directory, not from `-d`. Check:

```sh
pwd
ls -l /absolute/config.yaml
```

`<home>/config.yaml` is read only when `-f` is omitted.

## `path is not subpath of home directory or SAFE_PATHS`

A certificate, provider, or store referenced by the configuration sits outside home.

Preferred:

1. Move the file into the home directory.
2. Or add the trusted root to `SAFE_PATHS`.

Do not set this long-term just to solve one path:

```sh
SKIP_SAFE_PATH_CHECK=true
```

## Aster Admin returns 404

Possible causes:

- There is no `aster` block.
- The Controller is plaintext TCP bound off loopback.
- The route/host points at the wrong Controller.
- Managed listener initialization failed.

Confirm:

```yaml
external-controller: 127.0.0.1:9090
aster:
  secret: "at-least-32-bytes..."
```

Then from the same host:

```sh
curl -v \
  -H "Authorization: Bearer $ASTER_TOKEN" \
  http://127.0.0.1:9090/api/admin/status
```

## Aster Admin returns 401

- You used the Controller secret instead of the Aster secret.
- `Bearer` case or spacing is wrong.
- The secret was copied with a newline.

Correct format:

```http
Authorization: Bearer actual-aster-secret
```

## Aster Admin returns 403

Same-origin failed. Check:

- Whether browser `Origin` equals the request scheme/host.
- Whether the reverse proxy overwrites `Host` correctly.
- Whether `X-Forwarded-Proto` is `https`.
- Whether the request has `Sec-Fetch-Site: cross-site`.

Dashboards should go through a same-origin backend/BFF. Do not let the browser call cross-site directly.

## Mutation returns 409

The revision is stale:

1. GET `/api/admin/inbounds` again.
2. GET the user again.
3. Compare another administrator’s changes.
4. Resubmit with the new revision.

Do not just increment the revision by one and retry.

## Subscription returns 404

Check:

- Whether `public-base-url` is set.
- Whether the user is enabled.
- Whether the token was rotated.
- Whether the listener is still in `managed-listeners`.
- Whether the listener has a determinable port.
- Whether VLESS/AnyTLS security can be exported.
- Whether you used ShadowTLS, ResTLS, JLS, or advanced XHTTP.

## Store cannot load

Common causes:

- Parent directory permissions are too open.
- The state file is not `0600`.
- Wrong owner.
- The file is a symlink.
- JSON is damaged.
- Both primary and backup are invalid.
- The state version is unsupported.

Do not delete both copies immediately. Back them up offline first:

```sh
cp -a aster-state.json aster-state.json.forensics
cp -a aster-state.json.bak aster-state.json.bak.forensics
```

Then read the log to decide which copy is valid. State contains credentials, so the forensic files need the same protection.

## Docker published port cannot connect

Bridge mode needs:

```yaml
allow-lan: true
bind-address: "*"
```

And confirm:

```sh
docker port aster-core
docker logs aster-core
```

Host network and Docker Desktop do not behave the same way. Do not assume `--network host` is consistent across platforms.

## TUN cannot be created

Check:

- Whether the binary includes `with_gvisor`.
- Whether `/dev/net/tun` exists.
- Whether the container received the device.
- Whether `CAP_NET_ADMIN` is present.
- Whether the TUN name conflicts.
- Whether auto-route tables/rules conflict.
- Whether another VPN already owns the routes.

```sh
aster-core -v
ip tuntap
ip rule
ip route show table all
```

## Kernel DIRECT takes down every node together with DIRECT

On OpenWrt, `auto-redirect` can send Aster’s own SYN back in. If the core drops locally sourced REDIR / TUN SYNs in `handleTCPConn`, DIRECT and nodes that are not bound to an interface time out together. The correct behavior is to let the packet finish rule matching, then let `DIRECT.CheckConn`, the kernel-direct exclude set, and the 30-second zero-byte TCP reaper handle the loop.

```sh
/usr/bin/mihomo -v
curl -sS -H "Authorization: Bearer $SECRET" \
  http://127.0.0.1:9090/proxies/DIRECT/delay?timeout=8000\&url=http://www.gstatic.com/generate_204
```

`-v` should be a release without the inbound-drop experiment. Delay should return a number, not empty or a timeout.

## Dual-WAN delay tests all fail

`tun.auto-detect-interface: true` on ECMP / macvlan / mwan3 often makes `FindInterfaceName` return `<invalid>`, so socket bind fails. Set it to `false`, and confirm Nikki `mixin.uc` does not write `true` back when `tun_kernel_direct` is on.

```sh
yq -M '.tun["auto-detect-interface"]' /etc/nikki/run/config.yaml
```

## Connection count and memory explode (many zero-byte TCP)

When Aster’s DIRECT SYN is grabbed again by auto-redirect, TCP trackers with no payload are left behind. The 30-second reaper closes those TCP connections. UDP (including Wi‑Fi calling `500` / `4500`) is left alone. Do not use `DELETE /connections` as cleanup.

Look at `GET /connections` for `upload` / `download` of 0, and whether the source is the router’s own WAN IP. Then confirm whether the dest entered `inet4_route_exclude_address_set`, and check `learned_sets` from `GET /api/aster/kernel-direct/status`.

## iptables conflicts with TUN

Automatic iptables management and TUN cannot be enabled together. Decide who owns transparent intercept:

- TUN auto-route/auto-redirect, or
- External iptables/TProxy/Redir.

Do not let both rewrite the same traffic.

## Proxy group `relay` cannot be parsed

`relay` was removed. Move the chain onto the outbound:

```yaml
proxies:
  - name: hop-2
    type: vless
    dialer-proxy: hop-1
```

## SIGHUP did not pick up new content

If you started with:

```sh
aster-core --config '<base64>'
aster-core -f -
```

SIGHUP only reapplies the original bytes. To reread from disk, use ordinary file mode.

## Provider or geodata download failed

Check:

- System time.
- CA bundle.
- DNS.
- Whether the proxy chain is circular.
- Safe path.
- URL/ETag.
- Whether the runtime can reach GitHub/API.

Offline environments should pre-place provider/geodata. Do not depend on a first-start download.

## Windows named pipe

The pipe must start with:

```text
\\.\pipe\
```

Understand SDDL before using `LISTEN_NAMEDPIPE_SDDL` for a custom ACL. An overly open ACL exposes the Controller to other local users.

## Still cannot locate it

Collect:

- `aster-core -v`
- OS and architecture
- A minimal config with secrets removed
- Complete `-t` output
- Runtime log
- Whether the problem is only TCP, UDP, DNS, IPv4, IPv6, or a specific listener

Do not publish:

- UUID/password
- Private keys
- Aster/Controller secrets
- Subscription URLs/tokens
- The complete state file
