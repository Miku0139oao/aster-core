# Production troubleshooting

This page is a symptom-oriented runbook. Keep evidence first, then make the smallest change. Change one thing at a time and record the result. Do not start by deleting state, flushing the firewall, disabling safe paths, or reinstalling the whole VPS.

## Confirm which deployment you are using

Later commands cover both common layouts:

| Item | Dedicated-account tutorial | Official `.deb` package |
| --- | --- | --- |
| Unit | `aster-core.service` | `aster-core.service` |
| Binary | `/opt/aster-core/current/aster-core` | `/usr/bin/aster-core` |
| Home | `/var/lib/aster-core` | `/etc/mihomo` |
| Config | `/etc/aster-core/config.yaml` | `/etc/mihomo/config.yaml` |
| Expected user | `aster-core` | `root` |

Let systemd tell you the real values. Do not rely on memory:

```sh
sudo systemctl cat aster-core.service
sudo systemctl show aster-core.service \
  -p FragmentPath -p DropInPaths -p User -p Group -p ExecStart -p Environment
readlink -f /opt/aster-core/current 2>/dev/null || true
```

If `ExecStart`, the user, or the paths are not what you expected, sort out package unit, manual unit, and drop-in priority first.

## First ten minutes: a fixed order

### 1. Validate the configuration first

Dedicated-account deployment:

```sh
sudo -u aster-core \
  env SAFE_PATHS=/etc/aster-core \
  /opt/aster-core/current/aster-core \
  -d /var/lib/aster-core \
  -f /etc/aster-core/config.yaml \
  -t
```

Official package:

```sh
sudo /usr/bin/aster-core -d /etc/mihomo -t
```

Do not test a dedicated-account deployment only as root. Root being able to read a file does not mean the `aster-core` user can read the certificate, provider, config, or state parent.

A passing `-t` only means static parse succeeded. Port conflicts, the network, DNS, the REALITY peer, TUN capability, and the firewall can still fail at runtime.

### 2. Check the service and recent logs

```sh
sudo systemctl status aster-core.service --no-pager
sudo systemctl show aster-core.service \
  -p ActiveState -p SubState -p Result -p ExecMainStatus -p NRestarts
sudo journalctl -u aster-core.service -b --no-pager
```

If the log is huge, narrow it to the incident window:

```sh
sudo journalctl -u aster-core.service \
  --since '2026-01-01 12:00:00' \
  --until '2026-01-01 12:15:00' \
  --no-pager
```

Replace the times with the real incident range. Do not start with an unbounded `-f`; it is easy to miss the first error before the service started.

### 3. Check the process and sockets

```sh
sudo systemctl show aster-core.service -p MainPID
sudo ss -lntup
```

For common ports:

```sh
sudo ss -lntp 'sport = :443'
sudo ss -lntp 'sport = :9090'
sudo ss -lnup
```

### 4. Check DNS, time, and disk

```sh
getent ahosts proxy.example.com
timedatectl status
df -h
df -i
```

Replace the domain with the real node / server hostname. Running out of disk or inodes makes state, cache, provider, and log writes fail.

## Symptom: `-t` fails

### Configuration not found or the wrong file is read

Look at `ExecStart`, then reproduce with absolute paths:

```sh
pwd
sudo systemctl show aster-core.service -p ExecStart
sudo ls -l /etc/aster-core/config.yaml
```

A relative `-f` is resolved from the process working directory, not from `-d`. `<home>/config.yaml` is used only when `-f` is omitted. Production units should use an absolute `-d` and an absolute `-f`.

### `path is not subpath of home directory or SAFE_PATHS`

The configuration references a certificate, private key, provider, or store outside home. Handle it in this order:

1. Put sensitive files in the controlled home or `/etc/aster-core`.
2. Confirm the service account can read them.
3. If you truly need another trusted directory, add that exact root to `SAFE_PATHS`.

Check the unit environment:

```sh
sudo systemctl show aster-core.service -p Environment
```

Do not use `SKIP_SAFE_PATH_CHECK=true` to hide a path-design problem.

### YAML or field errors

Common sources:

- Tabs, indentation, or quoting mistakes.
- Duplicate listener names.
- `aster.managed-listeners` pointing at a missing or unmanaged listener type.
- `aster.secret` shorter than 32 bytes, or with leading/trailing space.
- `public-base-url` not an absolute HTTPS URL, or it contains a query / fragment.
- `store` pointing outside the safe path.
- The same listener setting REALITY and another mutually exclusive security mode.
- REALITY private key not a 32-byte X25519 base64url key.
- Short ID not hex, or more than 8 bytes after decode.

Fix only the smallest range the error names, then rerun the exact same `-t` the production service uses.

## Symptom: the service keeps restarting or `status=1/FAILURE`

Get the first failure and restart count:

```sh
sudo systemctl show aster-core.service \
  -p Result -p ExecMainCode -p ExecMainStatus -p NRestarts
sudo journalctl -u aster-core.service -b -n 200 --no-pager
```

Common reads:

| Log / state | Check first |
| --- | --- |
| `permission denied` | Unit user, and config / certificate / state owner and mode |
| `address already in use` | Another process owns the port |
| `not subpath` / `SAFE_PATHS` | Home, referenced paths, and unit environment |
| `load Aster state` | State owner, mode, symlink, JSON, version |
| `operation not permitted` | Capabilities needed for low ports, TUN, or routing |
| `no such file or directory` | Symlink target, ExecStart, certificate/provider path |

If a restart loop makes the log noisy, stop the service and inspect offline:

```sh
sudo systemctl stop aster-core.service
```

Start again only after `-t` and permission checks. Do not let constant restarts replace diagnosis.

## Symptom: `address already in use` or the outside cannot connect

Find the exact owner:

```sh
sudo ss -lntp 'sport = :443'
sudo ss -lntp 'sport = :9090'
sudo systemctl list-units --type=service --state=running
```

If 443 is already used by Caddy, Nginx, HAProxy, or another proxy:

- Use another listener port; or
- Design a TCP / SNI split that supports this protocol.

An ordinary HTTP reverse proxy cannot forward AnyTLS + REALITY as a normal website path. Do not stop an unknown service or kill a PID. Confirm purpose and owner with `systemctl status <unit>` first.

If the server is listening but the outside still times out, check in this order:

1. VPS provider security group / cloud firewall.
2. Host firewall.
3. Whether DNS points at this VPS.
4. Whether AAAA points at an IPv6 that is not actually configured.
5. Whether the client network blocks the port.

Inspect the current rules. Do not flush the ruleset:

```sh
sudo ufw status verbose
sudo nft list ruleset
```

Test TCP reachability from another network:

```sh
nc -vz proxy.example.com 443
```

A successful `nc` only proves TCP can be established, not that REALITY authentication succeeded.

## Symptom: DNS is wrong, flaky, or fails only on some networks

Look up from both client and server:

```sh
getent ahosts proxy.example.com
dig +short A proxy.example.com
dig +short AAAA proxy.example.com
dig @1.1.1.1 +short A proxy.example.com
dig @1.1.1.1 +short AAAA proxy.example.com
```

If `dig` is not installed:

```sh
sudo apt install --yes dnsutils
```

How to read it:

- Public resolver and local results differ: cache, split DNS, or a hosts override.
- A is correct, AAAA is wrong: IPv6-capable clients may prefer the bad AAAA.
- You get a CDN address instead of the VPS: the DNS record may have HTTP proxy on.
- NXDOMAIN: record name, zone, or nameserver delegation is wrong.

AnyTLS + REALITY is raw TCP. Ordinary orange-cloud / HTTP CDN proxy from Cloudflare and similar providers usually will not forward it. Without a matching L4 product, set the proxy server record to DNS-only.

Then check server-side DNS:

```sh
cat /etc/resolv.conf
resolvectl status 2>/dev/null || true
resolvectl query www.microsoft.com 2>/dev/null || true
```

If Aster uses `nameserver: system`, the system resolver itself must work. In a TUN DNS-hijack environment, also confirm Aster’s upstream queries are not intercepted by Aster again.

## Symptom: REALITY handshake fails

First confirm the four values people mix up most:

| Client field | Correct source |
| --- | --- |
| `server` | Aster VPS IP / domain |
| `sni` | One value from server `reality-config.server-names` |
| `reality-opts.public-key` / `pbk` | The **PublicKey** of the server key pair |
| `reality-opts.short-id` / `sid` | One value from server `short-id` |

The client should also set:

```yaml
client-fingerprint: chrome
```

### Clock

REALITY is time-sensitive. On the server:

```sh
date -u
timedatectl status
timedatectl show -p NTPSynchronized -p TimeUSec
systemctl status systemd-timesyncd.service --no-pager 2>/dev/null || true
chronyc tracking 2>/dev/null || true
```

The client clock must be synced too. Do not hide a broken NTP setup long-term by setting `max-time-difference` to a huge value. That field is in microseconds.

### SNI and fallback destination

Confirm the VPS can resolve and reach `dest`:

```sh
getent ahosts www.microsoft.com
timeout 10 openssl s_client \
  -connect www.microsoft.com:443 \
  -servername www.microsoft.com \
  </dev/null
```

Replace the host with the real `dest` / SNI. `dest` should be a stable TLS site that reasonably matches the SNI.

Connecting to Aster’s REALITY port with ordinary `openssl s_client` only exercises unverified fallback behavior. It does not prove REALITY client authentication. The final test must use an Aster client that supports the same REALITY fields.

### Public key and short ID

- X25519 public / private keys are raw URL-safe base64. They are not interchangeable.
- `sid` is a hex string of even length, at most 8 bytes after decode.
- Client `sid` must match the server list exactly. If the server configures an empty short ID, the client must follow that actual setting too.
- After you change the server private key, every client public key and existing subscription must be updated.
- After you change SNI / short ID, clients and subscriptions must be updated too.

Do not try to repair a wrong `pbk`, SNI, or `sid` with `skip-cert-verify`. That does not make REALITY succeed.

### You only see timeout

Check these together:

```sh
sudo ss -lntp 'sport = :443'
sudo journalctl -u aster-core.service --since '-10 minutes' --no-pager
sudo ufw status verbose
```

If TCP never reached the server, fix DNS / firewall / port first. Concentrate on REALITY field comparison only after TCP arrives and the log shows a handshake problem.

## Symptom: Controller or Aster Admin returns 401 / 403 / 404

### Separate the two tokens first

```yaml
secret: "<controller-secret>"

aster:
  secret: "<another-aster-secret-of-at-least-32-bytes>"
```

- Clash-compatible APIs such as `/version`, `/configs`, and `/proxies` use the Controller `secret`.
- `/api/admin/*` uses `aster.secret`.
- `/sub/aster/{token}` uses each user’s subscription token, not a Bearer token.

Do not test token-bearing requests with `curl -v`. It can print the Authorization header. Feed a hidden input into curl’s stdin config:

```sh
read -r -s ASTER_ADMIN_TOKEN
printf '\n'
printf 'header = "Authorization: Bearer %s"\n' "${ASTER_ADMIN_TOKEN}" |
  curl --config - \
    --silent --show-error \
    --output /dev/null \
    --write-out '%{http_code}\n' \
    http://127.0.0.1:9090/api/admin/status
unset ASTER_ADMIN_TOKEN
```

You can verify the ordinary Controller the same way; this time the input is the top-level `secret`:

```sh
read -r -s ASTER_CONTROLLER_TOKEN
printf '\n'
printf 'header = "Authorization: Bearer %s"\n' "${ASTER_CONTROLLER_TOKEN}" |
  curl --config - \
    --silent --show-error \
    --output /dev/null \
    --write-out '%{http_code}\n' \
    http://127.0.0.1:9090/version
unset ASTER_CONTROLLER_TOKEN
```

If `/version` succeeds and `/api/admin/status` is 401, the transport and Controller secret are fine. Focus on `aster.secret`. The reverse is also true: do not send the Aster token to ordinary Controller APIs.

### 401 Unauthorized

Check first:

- You used the Controller secret as the Aster secret, or the reverse.
- `Bearer` spelling, case, or spacing is wrong.
- The secret was copied with an extra newline or surrounding space.
- The reverse proxy / BFF did not forward the Authorization header.

Do not put the token in a URL query, shell history, frontend bundle, or issue.

### 403 Forbidden

Aster Admin enforces same-origin protection. Check:

- Whether browser `Origin` matches the request scheme / host.
- Whether `Sec-Fetch-Site` is `cross-site`.
- Whether the reverse proxy overwrites `Host` correctly.
- Whether an HTTPS proxy forwards `X-Forwarded-Proto: https`.
- Whether the proxy upstream is the loopback Controller.

Aster trusts the first `X-Forwarded-Proto` only when the request comes from loopback. Dashboards should call through a same-origin backend / BFF. Do not let the browser send the Aster token cross-site.

### 404 Not Found

Check:

- Whether the configuration really has an `aster` block.
- Whether `managed-listeners` initialized successfully.
- Whether the request hit the correct Controller port / socket.
- Whether a plaintext `external-controller` is bound off loopback.

By design, Aster Admin is mounted only on:

- A loopback plaintext Controller.
- An HTTPS Controller.
- A Unix socket.
- A Windows named pipe.

If you bind a plaintext Controller to `0.0.0.0`, Admin routes are not mounted. Do not expose the whole Controller to the Internet just to get the route.

## Symptom: Aster mutation returns 409

409 means a listener revision or store generation conflict, not an ordinary network retry.

Handle it like this:

1. GET `/api/admin/inbounds` again.
2. If you are changing an existing user, GET `/api/admin/users/{id}` again.
3. Compare changes another administrator or process already submitted.
4. Merge the intended change.
5. Resubmit the mutation with the latest listener revision.

Do not increment the old revision by one and resend, and do not retry forever automatically. That can overwrite another administrator’s update.

If a single administrator keeps getting 409, check whether two Aster processes share state:

```sh
sudo systemctl list-units 'aster-core*' --all
sudo systemctl list-unit-files 'aster-core*'
sudo fuser -v /var/lib/aster-core/aster-state.json.lock 2>/dev/null || true
sudo fuser -v /etc/mihomo/aster-state.json.lock 2>/dev/null || true
```

`fuser` comes from Debian / Ubuntu `psmisc`. If it is missing, `sudo apt install --yes psmisc` first. Use it only to identify the owner. Do not add `-k` to the result.

Multiple instances must each have different:

- Store paths.
- Listener / proxy ports.
- Controller address / Unix socket.
- TUN device and routing resources.

Do not let two instances take turns writing the same state, and do not delete `.lock` to bypass an owner that is still running.

## Symptom: store permission, owner, or JSON errors

### Metadata-only checks

Dedicated-account layout:

```sh
sudo namei -l /var/lib/aster-core/aster-state.json
sudo stat -c '%U:%G %a %F %n' /var/lib/aster-core
sudo find /var/lib/aster-core \
  -maxdepth 1 \
  -name 'aster-state.json*' \
  -printf '%u:%g %m %y %p\n'
```

Package layout:

```sh
sudo namei -l /etc/mihomo/aster-state.json
sudo stat -c '%U:%G %a %F %n' /etc/mihomo
sudo find /etc/mihomo \
  -maxdepth 1 \
  -name 'aster-state.json*' \
  -printf '%u:%g %m %y %p\n'
```

Expected:

- The parent is a real directory, not a symlink.
- The parent owner equals the service user.
- The parent is not writable by group/other; the dedicated-account tutorial uses `0700`.
- State is a regular file, not a symlink.
- The state owner equals the service user.
- State has no group/other permission; usually `0600`.

### Fix permissions while offline

Stop the service and make a protected forensic copy first. Do not delete files first:

```sh
ASTER_FORENSIC_STAMP="$(date -u +%Y%m%dT%H%M%SZ)"
sudo systemctl stop aster-core.service
sudo install -d -o root -g root -m 0700 \
  "/var/backups/aster-core-forensics/${ASTER_FORENSIC_STAMP}"
sudo cp -a /var/lib/aster-core \
  "/var/backups/aster-core-forensics/${ASTER_FORENSIC_STAMP}/var-lib-aster-core"
```

On the dedicated-account layout you can fix:

```sh
sudo chown aster-core:aster-core /var/lib/aster-core
sudo chmod 0700 /var/lib/aster-core
sudo find /var/lib/aster-core \
  -maxdepth 1 \
  -type f \
  -name 'aster-state.json*' \
  -exec chown aster-core:aster-core {} +
sudo find /var/lib/aster-core \
  -maxdepth 1 \
  -type f \
  -name 'aster-state.json*' \
  -exec chmod 0600 {} +
```

Then run `-t` as the service account, start, and read the journal. On the package layout the expected owner is root. Do not copy `aster-core:aster-core` there.

### Primary / backup damage

Aster validates:

```text
aster-state.json
aster-state.json.bak
```

If both are valid, it uses the newer generation. If only one is valid, it uses that one, and a later commit can restore redundancy.

Do not:

- Delete both the primary and `.bak`.
- Blindly copy `.bak` over the primary without keeping evidence.
- Edit JSON `version` to bypass compatibility checks.
- Upload state to an issue; it contains UUIDs, passwords, tokens, and traffic data.

If the error is `unsupported Aster state version`, use a binary that can read that state, or restore a paired binary, config, and state from the backup taken before the same upgrade.

### Mutation returns 500

Read the journal around the incident, then check:

```sh
df -h /var/lib/aster-core
df -i /var/lib/aster-core
findmnt -no TARGET,OPTIONS /var/lib/aster-core
```

Common causes are a read-only filesystem, exhausted space / inodes, owner / mode changed by a deploy tool, or a runtime apply failure. Do not treat 500 as something you can retry forever.

## Symptom: TCP works, but UDP, DNS, or game traffic fails

Understand the AnyTLS UDP model first:

- The AnyTLS listener is TCP.
- Client `udp: true` enables UoT and puts UDP payload on the AnyTLS transport.
- The server does not necessarily show a `443/udp` socket. Only `443/tcp` is normal.

So “`ss -lnup` does not show 443” is not evidence that AnyTLS UDP failed.

Peel the layers:

1. Whether the client node has `udp: true`.
2. Whether TCP through the same proxy works.
3. Whether the server can do a UDP DNS query directly.
4. Whether server egress firewall / provider blocks UDP.
5. Whether Aster routes / rules send UDP to an outbound that does not support UDP.
6. Whether TUN / MTU / IPv6 only affects larger datagrams.

Compare UDP and TCP DNS on the server:

```sh
dig @1.1.1.1 example.com
dig @1.1.1.1 +tcp example.com
```

If TCP DNS works and UDP DNS times out, check server egress firewall and provider limits first. If both work but client UoT fails, check the client log, node `udp`, rule hits, and outbound capability.

If only TUN mode fails:

```sh
ls -l /dev/net/tun
sudo systemctl show aster-core.service \
  -p User -p AmbientCapabilities -p CapabilityBoundingSet
ip tuntap
ip rule
ip route show table all
```

TUN / transparent routing usually needs `CAP_NET_ADMIN` and the right device access. Do not let TUN auto-route / auto-redirect and external iptables rules take over the same traffic at the same time.

For OpenWrt + Nikki + `kernel-direct`, also see [OpenWrt and Nikki](/en/deployment/openwrt) and [Troubleshooting](/en/troubleshooting):

- Every delay test fails: `auto-detect-interface` must be `false` on dual WAN.
- DIRECT and unbound nodes die together: do not drop inbound REDIR / TUN SYNs.
- Connection count / memory explodes: look at zero-byte TCP and `inet4_route_exclude_address_set`. Do not `DELETE /connections`.

Packet capture can expose destination IPs, DNS names, and usage patterns. Capture only with authorization and after you understand the privacy impact. Do not upload a raw pcap to a public issue.

## Symptom: a configuration reload did not take effect

Validate, then reload:

```sh
sudo -u aster-core \
  env SAFE_PATHS=/etc/aster-core \
  /opt/aster-core/current/aster-core \
  -d /var/lib/aster-core \
  -f /etc/aster-core/config.yaml \
  -t
sudo systemctl reload aster-core.service
sudo journalctl -u aster-core.service --since '-5 minutes' --no-pager
```

In package mode, replace the first block with `/usr/bin/aster-core -d /etc/mihomo -t`.

If startup used `--config '<base64>'` or `-f -`, SIGHUP can only reapply the original bytes. It cannot pick up new content from disk. Production deployments that need file reload should use a normal `-f /absolute/config.yaml`.

Only managed credentials such as AnyTLS passwords can be updated live through the Aster API. Transport settings such as REALITY private key, SNI, short ID, and listener port are still managed by a YAML reload.

## Symptom: the problem appeared only after an upgrade

Answer four questions first:

1. What version and architecture does `aster-core -v` show now?
2. Which binary is systemd actually running?
3. Are the pre-upgrade backup and old binary still there?
4. Is the problem a failed start, or only a specific protocol / UDP / API?

```sh
/opt/aster-core/current/aster-core -v 2>/dev/null || /usr/bin/aster-core -v
sudo systemctl show aster-core.service -p ExecStart
readlink -f /opt/aster-core/current 2>/dev/null || true
sudo journalctl -u aster-core.service --since '-30 minutes' --no-pager
```

A version-directory deployment can roll back only the binary first:

```sh
sudo systemctl stop aster-core.service
sudo ln -sfn /opt/aster-core/releases/<previous-verified-version> /opt/aster-core/current
sudo systemctl start aster-core.service
sudo systemctl status aster-core.service --no-pager
```

`<previous-verified-version>` must be the exact directory confirmed by `readlink` / your deploy notes. Do not guess the version.

If the old binary explicitly rejects the current state version, stop the service and use the paired backup from before that same upgrade. Restoring state rolls back accounts, tokens, traffic, and revisions. Keep a forensic copy of the current state first.

A package downgrade should use a verified old `.deb` that still has its checksum, and you should read the release notes first. Do not treat `apt remove --purge` as rollback. It can make configuration and package state harder to restore.

## Symptom: subscription 404 or unusable content

Check:

- Whether `aster.public-base-url` exists and is HTTPS.
- Whether the user is enabled.
- Whether the subscription token was just rotated; the old token is invalid immediately.
- Whether the listener is still in `managed-listeners`.
- Whether the listener has an exportable port and security mode.
- Whether VLESS / AnyTLS REALITY fields are complete.
- Whether you used a ShadowTLS, ResTLS, JLS, or advanced XHTTP combination that cannot be exported.

The subscription URL itself is an access capability. Do not put the full URL in access logs, analytics, Referer, screenshots, or issues. The reverse proxy should not overwrite Aster’s `Cache-Control: no-store`.

## Collect diagnostics you can share

Create an owner-only directory first:

```sh
umask 077
ASTER_DIAG_DIR="$(mktemp -d)"
printf '%s\n' "${ASTER_DIAG_DIR}"
```

Collect basic data that does not directly include config / state:

```sh
uname -a > "${ASTER_DIAG_DIR}/uname.txt"
(
  /opt/aster-core/current/aster-core -v 2>/dev/null ||
  /usr/bin/aster-core -v
) > "${ASTER_DIAG_DIR}/version.txt"
sudo systemctl show aster-core.service \
  -p FragmentPath -p DropInPaths -p User -p Group \
  -p ExecStart -p ActiveState -p SubState -p Result \
  > "${ASTER_DIAG_DIR}/systemd-show.txt"
sudo systemctl cat aster-core.service \
  > "${ASTER_DIAG_DIR}/systemd-unit.txt"
sudo journalctl -u aster-core.service \
  --since '-30 minutes' \
  --no-pager \
  > "${ASTER_DIAG_DIR}/journal.txt"
sudo ss -lntup > "${ASTER_DIAG_DIR}/sockets.txt"
ip address show > "${ASTER_DIAG_DIR}/ip-address.txt"
ip route show table all > "${ASTER_DIAG_DIR}/ip-route.txt"
```

Even these files need a line-by-line review before you share them. Journal, unit environment, IPs, and routes can contain internal topology or tokens.

Also provide a **hand-built minimal config**. Do not copy the production config and assume automatic redaction is complete. At least remove or replace:

- Controller `secret`.
- `aster.secret`.
- UUIDs and AnyTLS passwords.
- REALITY / WireGuard / certificate private keys.
- Age secret key.
- Subscription tokens and full `/sub/aster/...` URLs.
- Userinfo, query tokens, or signed URLs inside provider URLs.
- Server hostnames / IPs you do not want public.

Do not collect or publish:

- `aster-state.json`, `.bak`, or `.lock`.
- An unredacted config.
- Authorization headers.
- Browser local storage / password-manager exports.
- Raw pcaps.
- Reverse-proxy access logs that contain subscription URLs.

An issue should clearly state:

1. OS, architecture, and the full Aster `-v`.
2. Deployment style and the actual systemd `ExecStart`.
3. A secret-free minimal config.
4. The complete `-t` result run as the same user / paths.
5. Whether the problem is only TCP, UDP, DNS, IPv4, IPv6, REALITY, Controller, or a specific listener.
6. Reproduction steps, expected result, actual result, and the exact incident time.
