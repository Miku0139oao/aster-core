# Linux packages and systemd

::: tip Production walkthrough
To go from a release asset, file permissions, firewall, and systemd through verification, upgrade, and rollback, use [Linux VPS production deployment](/en/tutorials/linux-production).
:::

## Release packages

The release pipeline can produce:

- Debian/Ubuntu `.deb`
- RPM `.rpm`
- Arch/Pacman `.pkg.tar.zst`

A package install provides:

```text
/usr/bin/aster-core
/usr/bin/mihomo -> aster-core compatibility
/etc/mihomo/config.yaml
aster-core.service
aster-core@.service
```

## Verify before install

```sh
sha256sum -c checksums.txt
```

After the package is installed:

```sh
sudo /usr/bin/aster-core -d /etc/mihomo -t
```

## Directories and permissions

Recommended:

```sh
sudo install -d -m 700 /etc/mihomo
sudo chmod 600 /etc/mihomo/config.yaml
sudo chmod 600 /etc/mihomo/aster-state.json* 2>/dev/null || true
```

If the service uses a dedicated user, change the owner to that account. The Aster store directory must follow the owner-only rule.

## Main service

```sh
sudo systemctl enable --now aster-core
sudo systemctl status aster-core
sudo journalctl -u aster-core -f
```

The unit runs:

```text
/usr/bin/aster-core -d /etc/mihomo
```

Reload:

```sh
sudo /usr/bin/aster-core -d /etc/mihomo -t
sudo systemctl reload aster-core
```

Run `-t` first, then reload, so an obviously invalid profile is not handed to the running service.

## Multiple instances

```sh
sudo systemctl enable --now aster-core@edge
```

Configuration directory:

```text
/etc/mihomo/edge
```

Each instance must use different:

- Proxy/listener ports
- Controller address
- TUN device/name
- Aster store
- Unix socket/named resource

Do not point two instances at the same Aster state. Store generation and locking prevent some conflicts, but runtime listener ownership still does not work.

## Capabilities

The package unit may grant:

- `CAP_NET_ADMIN`
- `CAP_NET_RAW`
- `CAP_NET_BIND_SERVICE`
- `CAP_SYS_TIME`
- `CAP_SYS_PTRACE`
- `CAP_DAC_READ_SEARCH`
- `CAP_DAC_OVERRIDE`

That set covers many Mihomo features and is high privilege. If you only run an HTTP/SOCKS client, create a custom unit with fewer capabilities.

## Manual binary service

Example:

```ini
[Unit]
Description=Aster Core
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
ExecStart=/usr/local/bin/aster-core -d /etc/mihomo
ExecReload=/bin/kill -HUP $MAINPID
Restart=on-failure
RestartSec=5
LimitNOFILE=infinity

[Install]
WantedBy=multi-user.target
```

TUN/transparent proxy needs extra capabilities. If you do not need them, do not copy the full package-unit privilege set.

## Geodata

The Docker image ships geodata. Binary/package deployments may download it on first use, or you may need to place it in the home directory yourself:

- `geoip.metadb`
- `GeoIP.dat`
- `GeoSite.dat`
- `ASN.mmdb`

If production has no outbound Internet, include and verify these in the deploy artifact.

## Upgrade/rollback

1. Back up `/etc/mihomo`.
2. Keep the old binary/package.
3. Run `-t` with the new binary.
4. Stop or reload the service.
5. Verify Controller, DNS, TCP, UDP, and managed users.
6. On a problem, restore the binary and a compatible state/config.

An unsupported Aster state version refuses to load. Do not hand-edit the `version` field to bypass that.
