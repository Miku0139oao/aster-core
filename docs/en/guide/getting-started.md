# Quick start

::: tip Want a complete, follow-along walkthrough?
This page is a short launch reference. To go from a release, the first remote node, and a full DNS/rules profile through to `curl` verification, read [Your first proxy profile](/en/tutorials/first-proxy).
:::

## Requirements

- Go 1.20 or newer when building from source.
- Ordinary HTTP/SOCKS use does not need root.
- TUN, TProxy, Redir, iptables, or low ports may need root, capabilities, or platform privileges.
- `with_gvisor` is the standard tag for official releases.

## Get Aster Core

### GitHub Releases

From [Releases](https://github.com/Miku0139oao/aster-core/releases), pick the operating system and CPU architecture. On older x86-64 CPUs, prefer `amd64-v1` or `amd64-compatible`.

Check the checksum published with the release, then make the binary executable:

```sh
chmod +x aster-core
./aster-core -v
```

### Build from source

```sh
git clone https://github.com/Miku0139oao/aster-core.git
cd aster-core
go mod download
CGO_ENABLED=0 go build -tags with_gvisor -trimpath -o aster-core .
./aster-core -v
```

In PowerShell you can set:

```powershell
$env:CGO_ENABLED = "0"
go build -tags with_gvisor -trimpath -o aster-core.exe .
```

## Create a minimal configuration

Create `config/config.yaml`:

```yaml
mixed-port: 7890
allow-lan: false
mode: rule
log-level: info

external-controller: 127.0.0.1:9090
secret: "replace-this-controller-secret"

dns:
  enable: true
  enhanced-mode: fake-ip
  nameserver:
    - system

rules:
  - MATCH,DIRECT
```

This profile only proves the core can start:

- `127.0.0.1:7890`: HTTP and SOCKS mixed proxy.
- `127.0.0.1:9090`: Controller API.
- `MATCH,DIRECT`: every flow goes direct; no remote proxy is used.

::: warning
This is not a production proxy profile. Traffic only starts going through a remote node after you add proxies, a proxy group, and routing rules.
:::

## Validate the configuration

```sh
./aster-core -d ./config -f ./config/config.yaml -t
```

On success you will see:

```text
configuration file ... test is successful
```

`-t` only parses and validates. It does not keep listeners running.

## Start

```sh
./aster-core -d ./config -f ./config/config.yaml
```

Test the proxy:

```sh
curl -x http://127.0.0.1:7890 https://example.com/
```

Test the Controller:

```sh
curl \
  -H 'Authorization: Bearer replace-this-controller-secret' \
  http://127.0.0.1:9090/version
```

## Reload and stop

Unix:

```sh
kill -HUP <pid>   # re-read a file-backed configuration
kill -TERM <pid>  # graceful shutdown
```

In file mode, `SIGHUP` re-reads the disk. Stdin or Base64 mode only reapplies the bytes kept in memory at startup.

## Next steps

- [Build your first complete proxy profile](/en/tutorials/first-proxy)
- [Routing and DNS](/en/tutorials/routing-dns)
- [Configuration sources and safe paths](/en/guide/configuration)
- [Add proxies and proxy groups](/en/reference/outbounds)
- [Configure rules and DNS](/en/reference/routing-dns)
- [Enable Aster user management](/en/aster/overview)
- [Deploy with Docker](/en/deployment/docker)
