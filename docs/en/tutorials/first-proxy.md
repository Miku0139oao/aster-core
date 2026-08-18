# First proxy profile

After Aster starts, it serves an HTTP / SOCKS5 proxy on `127.0.0.1:7890`. The walkthrough below covers install, an AnyTLS + REALITY node, a proxy group, DNS, rules, and connection checks.

::: warning Example node
The host, password, REALITY public key, SNI, and short ID are placeholders. Replace them with values from your server or node provider.
:::

## Prerequisites

- Linux, macOS, or Windows
- An Aster Core release for your OS and CPU architecture
- One working AnyTLS + REALITY node

| Placeholder in this page | What to put there |
| --- | --- |
| `<ASTER_SERVER_HOST_OR_IP>` | The AnyTLS node / server IP or domain; not the camouflage site |
| `443` | The node port; change it if the server is not on 443 |
| `<ANYTLS_PASSWORD>` | The AnyTLS password from the server |
| `<REALITY_SNI_FROM_SERVER>` | The camouflage hostname from the server |
| `<REALITY_PUBLIC_KEY>` | The REALITY public key from the server |
| `<REALITY_SHORT_ID>` | The short ID from the server; omit it only if none was given |
| `<CONTROLLER_SECRET>` | A local Controller password you choose |

If the provider gave you an `anytls://` URI, map it like this:

```text
anytls://<password>@<server>:<port>?security=reality&sni=<sni>&fp=chrome&pbk=<public-key>&sid=<short-id>
```

Do not paste production passwords, private keys, or the local Controller secret into public issues, chat logs, or a Git repository.

## 1. Download and install Aster Core

Go to [GitHub Releases](https://github.com/Miku0139oao/aster-core/releases) and pick the file that matches your OS and CPU architecture. On older x86-64 CPUs, prefer names that include `amd64-v1` or `amd64-compatible`.

Compare the downloaded file’s SHA-256 with the checksum published on the same release:

Linux / macOS:

```sh
sha256sum ./<downloaded-release-file>
```

If macOS does not have `sha256sum`:

```sh
shasum -a 256 ./<downloaded-release-file>
```

Windows PowerShell:

```powershell
Get-FileHash .\<downloaded-release-file> -Algorithm SHA256
```

After extracting, you can name the binary `aster-core`; on Windows use `aster-core.exe`. On Unix, add execute permission and check the version:

```sh
chmod +x ./aster-core
./aster-core -v
```

PowerShell:

```powershell
.\aster-core.exe -v
```

If you installed a `.deb`, `.rpm`, Arch package, or OpenWrt package, the binary is usually already at `/usr/bin/aster-core`. Full Linux package steps are in [Linux packages and systemd](/en/deployment/linux).

### Build from source

A release is the easier path. If you really need to build:

```sh
git clone https://github.com/Miku0139oao/aster-core.git
cd aster-core
go mod download
CGO_ENABLED=0 go build -tags with_gvisor -trimpath -o aster-core .
./aster-core -v
```

Official releases use the `with_gvisor` build tag. A local build that omits the tag may not match the release TUN and related features.

## 2. Create a configuration directory

The examples assume the binary is in the current directory and the profile lives at `./config/config.yaml`:

```sh
mkdir -p ./config
```

PowerShell:

```powershell
New-Item -ItemType Directory -Force .\config
```

`-d ./config` makes this directory the Aster home. `cache.db`, provider cache, and other relative paths resolve from it, so after real use do not back up only the YAML.

## 3. Prepare the secret and node values

You can generate a Controller secret with a password manager, or on a system with OpenSSL:

```sh
openssl rand -base64 32
```

This secret only protects the local Controller API. It is not the AnyTLS password.

Check the server values one by one:

1. `server` is the Aster host you actually connect to.
2. `sni` is the REALITY camouflage name and must appear on the server allow list.
3. `public-key` is the server public key; never put the private key here.
4. `short-id` must match the server.
5. `password` must be that AnyTLS user’s password.

Any mismatch can show up as a TLS / REALITY handshake failure.

## 4. Write the complete profile

Create `config/config.yaml`, paste the following, then replace every `<...>` value:

```yaml
# Shared local HTTP and SOCKS5 entry.
mixed-port: 7890
allow-lan: false
mode: rule
log-level: info
ipv6: false

# Bind the Controller to loopback; do not expose it to the LAN or Internet.
external-controller: 127.0.0.1:9090
secret: "<CONTROLLER_SECRET>"

profile:
  store-selected: true
  store-fake-ip: true

dns:
  enable: true
  listen: 127.0.0.1:1053
  ipv6: false
  enhanced-mode: fake-ip
  fake-ip-range: 198.18.0.1/16

  # Bootstrap resolver used when resolving DoH hostnames or other DNS upstreams.
  # This field should only use raw-IP resolvers or system.
  default-nameserver:
    - 1.1.1.1
    - 8.8.8.8

  nameserver:
    - https://1.1.1.1/dns-query
    - https://8.8.8.8/dns-query

  # Resolve proxy server domains so node lookup does not depend on a proxy that is not up yet.
  proxy-server-nameserver:
    - https://1.1.1.1/dns-query
    - https://8.8.8.8/dns-query

  # LAN, mDNS, and time sync are often a poor fit for fake-IP.
  fake-ip-filter:
    - "*.lan"
    - "*.local"
    - "time.*.com"

proxies:
  - name: Edge-AnyTLS-REALITY
    type: anytls
    server: <ASTER_SERVER_HOST_OR_IP>
    port: 443 # If the server is not 443, change this to the real port.
    password: "<ANYTLS_PASSWORD>"
    sni: <REALITY_SNI_FROM_SERVER>
    client-fingerprint: chrome
    reality-opts:
      public-key: <REALITY_PUBLIC_KEY>
      short-id: <REALITY_SHORT_ID>
    udp: true

proxy-groups:
  - name: PROXY
    type: select
    proxies:
      - Edge-AnyTLS-REALITY
      - DIRECT

rules:
  # Keep private addresses off the remote proxy, and do not trigger extra DNS for IP rules.
  - IP-CIDR,10.0.0.0/8,DIRECT,no-resolve
  - IP-CIDR,172.16.0.0/12,DIRECT,no-resolve
  - IP-CIDR,192.168.0.0/16,DIRECT,no-resolve
  - IP-CIDR,127.0.0.0/8,DIRECT,no-resolve

  # Tutorial checks: example.com is DIRECT, ipify.org goes through PROXY.
  - DOMAIN-SUFFIX,example.com,DIRECT
  - DOMAIN-SUFFIX,ipify.org,PROXY

  # Everything else goes through the switchable PROXY group.
  - MATCH,PROXY
```

A few points about this YAML:

- `allow-lan: false` keeps the mixed proxy local. Sharing it with the LAN needs a separate firewall and authentication design; do not just flip it to `true` and expose it.
- `mode: rule` matches `rules` from top to bottom and stops at the first hit.
- `PROXY` is a `select` group. The first option is AnyTLS; `DIRECT` is only for debugging or an explicit switch.
- `profile.store-selected` remembers the group choice. If you once selected `DIRECT`, a restart may stay on direct.
- fake-IP lets Aster keep the original domain for domain rules. `198.18.0.0/16` is the reserved range returned to the DNS client, not the remote site’s real IP.
- `proxy-server-nameserver` solves the bootstrap problem of “you need a proxy before you can resolve the proxy hostname.”

::: danger Do not use `skip-cert-verify` to hide REALITY errors
If the public key, SNI, or short ID is wrong, fix the values. Adding `skip-cert-verify: true` does not turn a wrong REALITY identity into a correct one, and it weakens other TLS checks.
:::

### If your AnyTLS uses ordinary certificate TLS

Only when the server is not REALITY and has a trusted TLS certificate should you change the outbound to:

```yaml
proxies:
  - name: Edge-AnyTLS-TLS
    type: anytls
    server: <ASTER_SERVER_HOST_OR_IP>
    port: 443
    password: "<ANYTLS_PASSWORD>"
    sni: <CERTIFICATE_HOSTNAME>
    udp: true
```

Remove `reality-opts` and make `sni` match the certificate name. Do not disable certificate verification to save time.

If you use VLESS, Trojan, Hysteria 2, or another protocol, replace only the `proxies` entry using [Outbounds and groups](/en/reference/outbounds). You can keep the `PROXY`, DNS, and rules structure.

## 5. Validate the configuration first

Linux / macOS:

```sh
./aster-core -d ./config -f ./config/config.yaml -t
```

PowerShell:

```powershell
.\aster-core.exe -d .\config -f .\config\config.yaml -t
```

On success you will see something like:

```text
configuration file ... test is successful
```

`-t` parses the whole YAML, builds the proxy/group model, and checks rule references. It does not prove the remote password or REALITY values can complete a handshake. Remote reachability is checked in the next connection tests.

Common validation errors:

- `proxy [PROXY] not found`: a rule points at a group that does not exist or is spelled differently.
- REALITY public-key parse failure: you pasted a private key, a truncated value, or a leftover placeholder.
- Invalid `short-id`: it should be the hex string from the server.
- YAML parse error: bad indentation, mixed tabs, or an unquoted password with special characters.

## 6. Start and watch the log

Foreground start is best for the first debug session:

```sh
./aster-core -d ./config -f ./config/config.yaml
```

PowerShell:

```powershell
.\aster-core.exe -d .\config -f .\config\config.yaml
```

After startup, confirm:

- The mixed listener is bound to `127.0.0.1:7890`.
- The DNS UDP/TCP listener is bound to `127.0.0.1:1053`.
- The Controller is bound to `127.0.0.1:9090`.
- There is no “port already in use”, provider-load, or configuration error.

Keep that terminal open and run the checks below in another one.

## 7. Check the Controller and group selection

Confirm the Controller first:

```sh
curl -fsS \
  -H 'Authorization: Bearer <CONTROLLER_SECRET>' \
  http://127.0.0.1:9090/version
```

On Windows PowerShell use `curl.exe`, so older PowerShell does not treat `curl` as another command:

```powershell
curl.exe -fsS -H "Authorization: Bearer <CONTROLLER_SECRET>" http://127.0.0.1:9090/version
```

If `DIRECT` was selected earlier, switch `PROXY` back to AnyTLS:

```sh
curl -fsS -X PUT \
  -H 'Authorization: Bearer <CONTROLLER_SECRET>' \
  -H 'Content-Type: application/json' \
  --data '{"name":"Edge-AnyTLS-REALITY"}' \
  http://127.0.0.1:9090/proxies/PROXY
```

Success returns HTTP `204 No Content`. Read the group state back:

```sh
curl -fsS \
  -H 'Authorization: Bearer <CONTROLLER_SECRET>' \
  http://127.0.0.1:9090/proxies/PROXY
```

## 8. Verify the first proxied connection with `curl`

Get the current egress IP without Aster, as a baseline:

```sh
curl -fsS https://api.ipify.org
```

Then go through Aster’s HTTP proxy:

```sh
curl -fsS \
  --proxy http://127.0.0.1:7890 \
  https://api.ipify.org
```

You can also test SOCKS5 on the same mixed port:

```sh
curl -fsS \
  --proxy socks5h://127.0.0.1:7890 \
  https://api.ipify.org
```

The `h` in `socks5h://` means the hostname is handed to the proxy instead of being resolved by the OS first. The proxied IP should usually be the proxy server’s egress, not the local egress.

Then check the tutorial’s direct rule:

```sh
curl -I \
  --proxy http://127.0.0.1:7890 \
  https://example.com/
```

The Aster terminal should show rule hits similar to:

```text
[TCP] ... --> api.ipify.org:443 match DomainSuffix(ipify.org) using PROXY[Edge-AnyTLS-REALITY]
[TCP] ... --> example.com:443 match DomainSuffix(example.com) using DIRECT
```

The source address, chain display, and case may differ. What matters is that `ipify.org` uses the `PROXY` chain and `example.com` uses `DIRECT`.

## 9. Check the built-in DNS

If the system has `dig`:

```sh
dig @127.0.0.1 -p 1053 example.org A +short
```

In fake-IP mode, ordinary domains should return an address in `198.18.0.0/16`. You can also query through the Controller:

```sh
curl -fsS \
  -H 'Authorization: Bearer <CONTROLLER_SECRET>' \
  'http://127.0.0.1:9090/dns/query?name=example.org&type=A'
```

This only proves Aster DNS can answer. A normal HTTP proxy request already hands the hostname to Aster; you do not need to point the whole computer’s system DNS at `127.0.0.1` first. To make every app use fake-IP, you still have to set system DNS, TUN, and DNS hijack. If a client gets a fake-IP and then sends traffic around Aster, the connection will fail. The full path is in [Routing and DNS](/en/tutorials/routing-dns).

## Troubleshooting

### `connection refused` to `127.0.0.1:7890`

- Aster has not started, or it already exited because of a configuration error.
- `mixed-port` was changed, but the test command still uses 7890.
- Another program already owns 7890; check the startup log.
- In Docker, the container port was not published to the host. See [Docker](/en/deployment/docker).

### Controller returns `401 Unauthorized`

- `<CONTROLLER_SECRET>` was not replaced.
- The request Bearer token does not match the YAML `secret`.
- The YAML was changed but not reloaded or restarted.

### `PROXY` actually selected `DIRECT`

`profile.store-selected: true` persists the choice. Use the `PUT /proxies/PROXY` call above to switch back to AnyTLS, or check the selection in a compatible dashboard.

### REALITY handshake / EOF / TLS errors

Check in this order:

1. `server` and `port` are the AnyTLS node / server, not the camouflage site.
2. `sni` matches the server `server-names` exactly.
3. `public-key` is the public half of the matching private key.
4. `short-id` is a value the server allows.
5. `client-fingerprint` is a value the peer supports; `chrome` is a reasonable first try.
6. Client and server clocks are NTP-synced.
7. The server firewall and any fronting Caddy / Nginx are not occupying or mis-forwarding that port.

Field details are in [AnyTLS + REALITY](/en/reference/anytls-reality).

### AnyTLS authentication failed

- The password differs in case, whitespace, or quoted content.
- The server user was disabled, deleted, or had its password rotated.
- A URI-percent-encoded string was pasted into YAML without decoding.

### Web pages work, but DNS or some apps bypass the proxy

- An HTTP proxy only affects programs that are explicitly configured to use it.
- SOCKS5 should use remote DNS; with `curl` that means `socks5h://`, not `socks5://`.
- The browser may have its own DoH enabled.
- QUIC / UDP is not taken over automatically by ordinary HTTP CONNECT.
- Taking over the whole machine needs a separate TUN, route, and DNS-hijack deployment.

### Behavior did not change after editing the profile

Validate again, then reload:

```sh
./aster-core -d ./config -f ./config/config.yaml -t
```

A Unix foreground process can re-read the file on `SIGHUP`:

```sh
kill -HUP <aster-core-pid>
```

On Windows, stop it normally and start it again. If you changed a DNS policy or fake-IP filter, you may also need to flush DNS / fake-IP cache; see [Routing and DNS](/en/tutorials/routing-dns).

## Next steps

- [Routing and DNS: fake-IP, policy, rule providers, and leak prevention](/en/tutorials/routing-dns)
- [Rules and DNS reference](/en/reference/routing-dns)
- [Outbounds and groups](/en/reference/outbounds)
- [AnyTLS + REALITY field reference](/en/reference/anytls-reality)
- [Configuration overview](/en/reference/configuration)
- [Troubleshooting](/en/troubleshooting)
