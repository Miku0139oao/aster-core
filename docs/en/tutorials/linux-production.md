# Debian / Ubuntu VPS production deployment

::: warning Optional Aster server deployment
Aster Core is mainly a client. Ordinary node servers use Xray, sing-box, or SideraCore. Use this VPS walkthrough only for testing, an all-Aster environment, or when you truly need Aster listeners / the management API. For a normal desktop, router, or gateway client, start with [Your first proxy profile](/en/tutorials/first-proxy).
:::

This page starts from a Linux binary asset on GitHub Releases and builds an optional Aster Core server that you can verify, run as a dedicated account, manage with systemd, and update or roll back safely. The example is an AnyTLS + REALITY listener.

There are two install modes. Pick one. Do not mix paths or units across them:

| Mode | Binary | Home / state | Configuration | Runs as |
| --- | --- | --- | --- | --- |
| Primary flow on this page: raw `.gz` | `/opt/aster-core/current/aster-core` | `/var/lib/aster-core` | `/etc/aster-core/config.yaml` | Dedicated `aster-core` user |
| Official `.deb` package | `/usr/bin/aster-core` | `/etc/mihomo` | `/etc/mihomo/config.yaml` | The shipped unit currently runs as root |

::: warning Actual package unit behavior
The repository’s current `.github/release/aster-core.service` has no `User=` or `Group=` and grants several capabilities. If you use the `.deb`, follow that unit’s real behavior. Do not assume it runs as the dedicated account created on this page. A package install path is listed later.
:::

## 1. Prepare before going live

Collect:

- The VPS Debian or Ubuntu version and CPU architecture.
- The release tag to install, for example `v1.2.3`.
- The public AnyTLS port; the example uses TCP `443`.
- A DNS A / AAAA record that points at the VPS.
- REALITY `dest`, allowed SNI, server key pair, and short ID.
- One SSH session you can still use. Do not close it before you adjust the firewall.

Update CAs and download tools first:

```sh
sudo apt update
sudo apt install --yes ca-certificates curl gzip openssl
```

Confirm the platform:

```sh
uname -m
dpkg --print-architecture
lscpu
```

Common release flavors:

| Host | Suggested flavor | Notes |
| --- | --- | --- |
| Typical x86-64 | `amd64-v1` | Broadest compatibility when you are unsure about CPU ISA |
| Confirmed newer x86-64 | `amd64-v2` / `amd64-v3` | Choose only after you confirm CPU support |
| ARM64 | `arm64` | `uname -m` is usually `aarch64` |
| 32-bit ARMv7 | `armv7` | `dpkg --print-architecture` is usually `armhf` |

In the release workflow, an `amd64` asset with no version suffix is currently GOAMD64 v3, not generic x86-64. The older name `amd64-compatible` is expected to go away. New deployments should prefer an explicit `amd64-v1`.

## 2. Download and verify the release

Replace all three values with a tag and asset that actually exist on the Release page. The filename format comes from the repository release workflow:

```sh
ASTER_RELEASE_TAG='Prerelease-main'
ASTER_BUILD_ID='alpha-main-SHA7'
ASTER_ASSET_FLAVOR='amd64-v1'
ASTER_ASSET_NAME="aster-core-linux-${ASTER_ASSET_FLAVOR}-${ASTER_BUILD_ID}.gz"
ASTER_DOWNLOAD_DIR="$(mktemp -d)"
```

Download the binary and the `checksums.txt` from the same Release:

```sh
curl --proto '=https' --tlsv1.2 --fail --location \
  --output "${ASTER_DOWNLOAD_DIR}/${ASTER_ASSET_NAME}" \
  "https://github.com/Miku0139oao/aster-core/releases/download/${ASTER_RELEASE_TAG}/${ASTER_ASSET_NAME}"

curl --proto '=https' --tlsv1.2 --fail --location \
  --output "${ASTER_DOWNLOAD_DIR}/checksums.txt" \
  "https://github.com/Miku0139oao/aster-core/releases/download/${ASTER_RELEASE_TAG}/checksums.txt"
```

Confirm the checksum list has exactly one matching asset, then verify the contents and gzip:

```sh
test "$(grep -Fc "  ./${ASTER_ASSET_NAME}" "${ASTER_DOWNLOAD_DIR}/checksums.txt")" -eq 1
(
  cd "${ASTER_DOWNLOAD_DIR}"
  sha256sum --check --ignore-missing checksums.txt
)
gzip --test "${ASTER_DOWNLOAD_DIR}/${ASTER_ASSET_NAME}"
```

Stop if any step fails. Do not add `--insecure`, skip verification, or continue installing when the checksum does not match.

Decompress into a temp directory only the current user can read, then check the version:

```sh
umask 077
gzip --decompress --stdout "${ASTER_DOWNLOAD_DIR}/${ASTER_ASSET_NAME}" \
  > "${ASTER_DOWNLOAD_DIR}/aster-core"
chmod 0755 "${ASTER_DOWNLOAD_DIR}/aster-core"
"${ASTER_DOWNLOAD_DIR}/aster-core" -v
```

The printed version should match the chosen release. If the tag, version, or architecture does not match, go back to the Release page and pick the asset again.

## 3. Install the binary into a versioned directory

Version directories let rollback switch one symlink instead of overwriting the only binary:

```sh
sudo install -d -o root -g root -m 0755 /opt/aster-core
sudo install -d -o root -g root -m 0755 /opt/aster-core/releases
sudo install -d -o root -g root -m 0755 \
  "/opt/aster-core/releases/${ASTER_RELEASE_TAG}"
sudo install -o root -g root -m 0755 \
  "${ASTER_DOWNLOAD_DIR}/aster-core" \
  "/opt/aster-core/releases/${ASTER_RELEASE_TAG}/aster-core"
sudo ln -sfn \
  "/opt/aster-core/releases/${ASTER_RELEASE_TAG}" \
  /opt/aster-core/current
```

Confirm again from the production path:

```sh
readlink -f /opt/aster-core/current
/opt/aster-core/current/aster-core -v
```

Keep the temp directory until the service passes end-to-end tests. It should not become the systemd ExecStart path.

## 4. Create a dedicated account and directories

Check whether the account already exists. If it does, confirm it really is the expected system account. Do not overwrite an existing account:

```sh
getent passwd aster-core || true
```

On a fresh host, create a non-login service account:

```sh
sudo useradd \
  --system \
  --user-group \
  --home-dir /var/lib/aster-core \
  --create-home \
  --shell /usr/sbin/nologin \
  aster-core
```

Create the configuration and state directories:

```sh
sudo install -d -o root -g aster-core -m 0750 /etc/aster-core
sudo install -d -o aster-core -g aster-core -m 0700 /var/lib/aster-core
```

This split is deliberate:

- root can change `/etc/aster-core/config.yaml`.
- `aster-core` can read the configuration but cannot overwrite it.
- `aster-core` can atomically write `/var/lib/aster-core/aster-state.json*`.
- Other local users cannot read credentials.

The Aster state parent must be owned by the account that runs the service and must not be writable by group/other. State files must be owned by that account and must have no group/other permissions. The program also rejects a symlink state.

## 5. Generate secrets and a REALITY key

Generate values in a trusted terminal and store them in a password manager immediately. Do not paste them into issues, chat, or a shell script:

```sh
openssl rand -base64 48
openssl rand -base64 48
openssl rand -base64 32
openssl rand -hex 8
/opt/aster-core/current/aster-core generate reality-keypair
```

In order they can be used as:

1. Controller `secret`.
2. `aster.secret`, at least 32 bytes, and not the same as the Controller secret.
3. The first AnyTLS user password.
4. REALITY short ID; 8 bytes print as 16 hex characters.
5. REALITY key pair: the private key stays on the server, the public key goes to the client.

Terminal output is still sensitive. If the session is recorded, the terminal is shared, or scrollback is saved, generate the values on an offline trusted device instead.

## 6. Create the AnyTLS + REALITY configuration

Use `sudoedit` so you do not write a world-readable temp file:

```sh
sudoedit /etc/aster-core/config.yaml
```

Start from this minimal server profile and replace every `<...>`:

```yaml
log-level: info
mode: rule

external-controller: 127.0.0.1:9090
secret: "<independent-controller-secret>"

listeners:
  - name: edge-anytls
    type: anytls
    listen: 0.0.0.0
    port: 443
    users:
      first-user: "<long-random-anytls-password>"
    reality-config:
      dest: www.microsoft.com:443
      private-key: "<server-private-key>"
      short-id:
        - "<16-hex-short-id>"
      server-names:
        - www.microsoft.com

aster:
  secret: "<independent-aster-secret-at-least-32-bytes>"
  store: /var/lib/aster-core/aster-state.json
  managed-listeners:
    - edge-anytls

rules:
  - MATCH,DIRECT
```

Configuration permissions:

```sh
sudo chown root:aster-core /etc/aster-core/config.yaml
sudo chmod 0640 /etc/aster-core/config.yaml
sudo stat -c '%U:%G %a %n' /etc/aster-core/config.yaml
```

REALITY notes:

- `dest` is the fallback TLS destination the server connects to when verification fails.
- The client `server` is your VPS IP / domain; `sni` must match `server-names`.
- The client uses the server **public key**. Never treat the private key as `pbk`.
- The client `sid` must match one of the short IDs exactly.
- Certificate TLS `certificate` / `private-key`, `shadow-tls`, `res-tls`, `jls-config`, and `reality-config` are mutually exclusive modes.
- To emit managed subscriptions, also add `public-base-url: https://your-public-domain`. It must be an absolute HTTPS URL with no query, fragment, or user information.

Full fields and a client example are in [AnyTLS + REALITY](/en/reference/anytls-reality).

## 7. Validate the configuration as the service account

`-t` must use the same user, home, configuration path, and safe paths as the production service. A root test succeeding does not prove the service account can read the configuration or the files it references.

```sh
sudo -u aster-core \
  env SAFE_PATHS=/etc/aster-core \
  /opt/aster-core/current/aster-core \
  -d /var/lib/aster-core \
  -f /etc/aster-core/config.yaml \
  -t
```

Expect `configuration ... test is successful`. `-t` only proves parse and static validation. It does not prove:

- TCP 443 is still free.
- The firewall and cloud security group allow the port.
- DNS points at the correct host.
- REALITY `dest` is reachable from the VPS.
- The client SNI, public key, short ID, and clock are correct.
- Server UDP egress works.

If certificates, providers, or other referenced files live outside `/etc/aster-core`, prefer moving them into a controlled directory. If you truly need another trusted root, add that root to `SAFE_PATHS`. Do not use `SKIP_SAFE_PATH_CHECK=true` as a long-term fix.

## 8. Create the systemd service

Create `/etc/systemd/system/aster-core.service`:

```sh
sudoedit /etc/systemd/system/aster-core.service
```

Contents:

```ini
[Unit]
Description=Aster Core
Documentation=https://astercore.fubukishop.app/
Wants=network-online.target
After=network-online.target nss-lookup.target

[Service]
Type=simple
User=aster-core
Group=aster-core
UMask=0077
Environment=SAFE_PATHS=/etc/aster-core
ExecStart=/opt/aster-core/current/aster-core -d /var/lib/aster-core -f /etc/aster-core/config.yaml
ExecReload=/bin/kill -HUP $MAINPID
Restart=on-failure
RestartSec=5s
LimitNOFILE=infinity

StateDirectory=aster-core
StateDirectoryMode=0700
NoNewPrivileges=true
PrivateTmp=true
ProtectSystem=strict
ProtectHome=true
ProtectControlGroups=true
ProtectKernelModules=true
ProtectKernelTunables=true
ReadWritePaths=/var/lib/aster-core

# The example listener uses TCP 443; remove these two lines for ports above 1023.
AmbientCapabilities=CAP_NET_BIND_SERVICE
CapabilityBoundingSet=CAP_NET_BIND_SERVICE

[Install]
WantedBy=multi-user.target
```

This unit is enough for an ordinary server listener, Controller, and outbound. TUN, TProxy, transparent routing, or automatic firewall management need extra privileges and device access. Read [Linux packages and systemd](/en/deployment/linux) and [Rules, DNS, TUN](/en/reference/routing-dns) first, and add only the capabilities you actually need. Do not grant unrelated privileges to save time.

Check the unit and start it:

```sh
sudo systemd-analyze verify /etc/systemd/system/aster-core.service
sudo systemctl daemon-reload
sudo systemctl enable --now aster-core.service
sudo systemctl status aster-core.service --no-pager
```

Read this boot’s log:

```sh
sudo journalctl -u aster-core.service -b --no-pager
```

Do not stop at `active (running)`. Next confirm the socket and end-to-end traffic.

## 9. Port, DNS, and firewall

See who owns 443 first:

```sh
sudo ss -lntp 'sport = :443'
```

Expect `aster-core`. If it is Caddy, Nginx, HAProxy, or another proxy, design a TCP / SNI split or use a different port. Two processes cannot listen on the same address:port.

Confirm DNS:

```sh
getent ahosts proxy.example.com
```

Replace `proxy.example.com` with the real server domain. If the DNS provider has an ordinary HTTP reverse-proxy / CDN switch, AnyTLS + REALITY raw TCP usually needs DNS-only. An ordinary CDN proxy terminates or rejects this non-HTTP protocol.

Check which firewall is actually in use. Do not let two tools write contradictory rules:

```sh
sudo ufw status verbose
sudo nft list ruleset
```

If the host is genuinely managed by UFW, keep the existing SSH rule, then add:

```sh
sudo ufw allow 443/tcp comment 'Aster AnyTLS'
sudo ufw status numbered
```

On a remote VPS, do not run `ufw enable` before you have confirmed an SSH allow rule. If the host is managed by nftables, a cloud security group, or a provider firewall, add the equivalent TCP 443 allow at that layer. Do not use a command that wipes the whole ruleset.

AnyTLS transport itself uses TCP. Client `udp: true` puts UDP payload on AnyTLS/UoT. It does not mean the server must also listen on `443/udp`. Do not open UDP 443 just because of UoT unless you also deployed another protocol that uses a UDP listener.

## 10. Go-live acceptance

On the server:

```sh
sudo systemctl is-active aster-core.service
sudo systemctl show aster-core.service \
  -p User -p Group -p ExecStart -p AmbientCapabilities
sudo ss -lntp 'sport = :443'
sudo ss -lntp 'sport = :9090'
sudo journalctl -u aster-core.service --since '-10 minutes' --no-pager
timedatectl status
```

The Controller should only be on `127.0.0.1:9090`. Aster Admin uses `aster.secret`, not the Controller `secret`. Do not add `curl -v` during tests; it can print the Authorization header:

```sh
read -r -s ASTER_ADMIN_TOKEN
printf '\n'
printf 'header = "Authorization: Bearer %s"\n' "${ASTER_ADMIN_TOKEN}" |
  curl --config - \
    --silent --show-error --fail-with-body \
    http://127.0.0.1:9090/api/admin/status
unset ASTER_ADMIN_TOKEN
```

Then verify from an Aster-compatible client on another network:

1. `server` points at the VPS domain or IP, not the REALITY camouflage site.
2. `sni` matches `server-names`.
3. `pbk` uses the server public key.
4. `sid` matches the server short ID.
5. `client-fingerprint: chrome`.
6. Test TCP web traffic first, then DNS / other UDP with `udp: true`.
7. After changing the AnyTLS password, confirm the new credential works and the old credential cannot open a new connection.

## 11. Upgrade flow

Read the release notes first, download the new asset, and redo checksum verification. Do not overwrite `/opt/aster-core/current/aster-core` in place.

Install the new binary into a new version directory, but do not switch `current` yet:

```sh
ASTER_NEW_RELEASE='alpha-main-SHA7'
sudo install -d -o root -g root -m 0755 \
  "/opt/aster-core/releases/${ASTER_NEW_RELEASE}"
sudo install -o root -g root -m 0755 \
  "${ASTER_DOWNLOAD_DIR}/aster-core" \
  "/opt/aster-core/releases/${ASTER_NEW_RELEASE}/aster-core"
```

Run static validation with the new binary, the production service account, and the production configuration:

```sh
sudo -u aster-core \
  env SAFE_PATHS=/etc/aster-core \
  "/opt/aster-core/releases/${ASTER_NEW_RELEASE}/aster-core" \
  -d /var/lib/aster-core \
  -f /etc/aster-core/config.yaml \
  -t
```

Record the current version and create a root-only consistent backup. This flow includes a short outage so state does not keep changing while you copy it:

```sh
ASTER_PREVIOUS_TARGET="$(readlink -f /opt/aster-core/current)"
ASTER_BACKUP_STAMP="$(date -u +%Y%m%dT%H%M%SZ)"
sudo systemctl stop aster-core.service
sudo install -d -o root -g root -m 0700 \
  "/var/backups/aster-core/${ASTER_BACKUP_STAMP}"
sudo cp -a /etc/aster-core \
  "/var/backups/aster-core/${ASTER_BACKUP_STAMP}/etc-aster-core"
sudo cp -a /var/lib/aster-core \
  "/var/backups/aster-core/${ASTER_BACKUP_STAMP}/var-lib-aster-core"
```

Switch and start:

```sh
sudo ln -sfn \
  "/opt/aster-core/releases/${ASTER_NEW_RELEASE}" \
  /opt/aster-core/current
sudo systemctl start aster-core.service
sudo systemctl status aster-core.service --no-pager
```

Redo the full “go-live acceptance,” especially:

- AnyTLS TCP and UDP/UoT.
- REALITY handshake.
- Controller and Aster Admin.
- Managed users, revision, and subscriptions.
- DNS, IPv4, IPv6.
- Whether the journal shows state recovery, permission, or runtime apply errors.

The backup contains private keys, user credentials, and subscription tokens. Keep `0700` access control and include it in an encrypted backup policy.

## 12. Rollback flow

If the new version cannot start or end-to-end acceptance fails, keep the journal, then return to the exact previous target you recorded:

```sh
sudo journalctl -u aster-core.service --since '-30 minutes' --no-pager
sudo systemctl stop aster-core.service
sudo ln -sfn "${ASTER_PREVIOUS_TARGET}" /opt/aster-core/current
sudo systemctl start aster-core.service
sudo systemctl status aster-core.service --no-pager
```

Try rolling back only the binary first. Restore config and state from the same upgrade backup only while the service is stopped, and only if the old binary explicitly rejects the new config / state schema, or the release notes require a paired restore. Restoring old state loses accounts, traffic, and token changes made after the backup. That is not a free operation.

Never hand-edit `aster-state.json` `version` to bypass compatibility checks, and never delete both the primary and `.bak` at once. For deeper diagnosis, collect redacted data using [Production troubleshooting](/en/tutorials/troubleshooting).

## 13. Differences when using the official `.deb`

If you use the Debian package from Releases, download and verify the same way. Only change the asset name to a `.deb` that actually exists on the Release page:

```sh
ASTER_DEB_NAME="aster-core-linux-amd64-v1-${ASTER_BUILD_ID}.deb"
sudo apt install "./${ASTER_DEB_NAME}"
```

The current package installs:

```text
/usr/bin/aster-core
/usr/bin/mihomo -> aster-core
/etc/mihomo/config.yaml
/usr/lib/systemd/system/aster-core.service
/usr/lib/systemd/system/aster-core@.service
```

The shipped main unit actually starts:

```text
/usr/bin/aster-core -d /etc/mihomo
```

It currently runs as root and keeps:

```text
CAP_NET_ADMIN CAP_NET_RAW CAP_NET_BIND_SERVICE CAP_SYS_TIME
CAP_SYS_PTRACE CAP_DAC_READ_SEARCH CAP_DAC_OVERRIDE
```

The correct validate-and-start commands in package mode are:

```sh
sudo /usr/bin/aster-core -d /etc/mihomo -t
sudo systemctl enable --now aster-core.service
sudo systemctl status aster-core.service --no-pager
```

If you want a dedicated account and minimal capabilities, the raw `.gz` / custom-unit flow on this page is clearer. Do not `chown aster-core` the package directories while the shipped unit still runs as root. That is a half-finished layout that does not match the real threat model.
