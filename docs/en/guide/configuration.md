# Configuration concepts

## Configuration source priority

Aster Core picks a configuration source in this order:

1. `--config` or `CLASH_CONFIG_STRING`: Base64-encoded YAML.
2. `-f -` or `CLASH_CONFIG_FILE=-`: standard input.
3. `-f <file>` or `CLASH_CONFIG_FILE`: an explicit file.
4. `config.yaml` under the home directory.

```sh
# Explicit file
aster-core -d /etc/mihomo -f /etc/mihomo/config.yaml

# stdin
aster-core -d /etc/mihomo -f - < /etc/mihomo/config.yaml

# Base64
aster-core --config '<base64-data>'
```

## Home directory

| Platform | Default directory |
| --- | --- |
| Unix | `$HOME/.config/mihomo` |
| Windows | `%USERPROFILE%\.config\mihomo` |

If the default directory does not exist and `XDG_CONFIG_HOME` is set, the process uses `$XDG_CONFIG_HOME/mihomo`.

`-d` changes:

- The default `config.yaml` location.
- The root for relative assets and provider cache.
- Locations for `cache.db`, GeoIP/GeoSite, and similar data.
- The default Aster state path.

Relative `-d` and relative `-f` both resolve from the **current working directory**. A relative `-f` is not resolved from `-d`.

## Safe paths

Files referenced by the configuration must live under the home directory by default. Certificates, private keys, provider paths, and the Aster store should all sit in that directory.

To allow other trusted directories, use the operating-system path-list format:

```sh
SAFE_PATHS=/etc/aster:/srv/aster aster-core -d /etc/mihomo
```

Windows:

```powershell
$env:SAFE_PATHS = "D:\certs;D:\providers"
```

`SKIP_SAFE_PATH_CHECK=true` disables this protection. Do not use it unless the runtime is already fully isolated by another sandbox.

## Age-encrypted configuration

Create a key:

```sh
aster-core age keygen
```

Encrypt with the public key:

```sh
aster-core age encrypt <public-key> config.yaml config.age
```

Start:

```sh
aster-core -f config.age --age-secret-key '<secret-key>'
```

You can also set `CLASH_AGE_SECRET_KEY` so the secret does not land in shell history.

## Top-level structure

Common settings fall into:

| Block | Purpose |
| --- | --- |
| General | ports, mode, logging, LAN, interface, routing mark |
| `dns` | DNS server, fake-IP, nameserver, fallback, policy |
| `tun` | TUN interface, routes, DNS hijack |
| `proxies` | Static outbound nodes |
| `proxy-groups` | Select, health check, fallback, load balance |
| `proxy-providers` | Remote or local proxy lists |
| `rule-providers` | Remote or local rule sets |
| `rules` | Traffic rules matched in order |
| `listeners` | Extra named inbound servers |
| `external-controller*` | Controller transports |
| `tls` | Controller TLS and shared certificate settings |
| `aster` | Aster user management |

Open [`config.yaml`](/config.yaml) for a full annotated example. Categorized field notes are in [Configuration overview](/en/reference/configuration).

## Validation strategy

After every change, run:

```sh
aster-core -d /path/to/home -f /path/to/config.yaml -t
```

A passing `-t` still does not prove every runtime condition, for example:

- A port is already in use.
- A certificate file is not readable.
- TUN or iptables privileges are missing.
- A provider URL cannot be reached.
- The REALITY destination or SNI cannot be used.

Before a production cutover, test real TCP, UDP, DNS, IPv4, IPv6, and transparent-proxy paths on the target platform.
