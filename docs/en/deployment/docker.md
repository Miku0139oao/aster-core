# Docker

Binaries, packages, and image sources are listed on [Downloads](/en/downloads). Public builds currently come from [`Prerelease-main`](https://github.com/Miku0139oao/aster-core/releases/tag/Prerelease-main); no official numbered `v*` release exists yet.

## Image publish status

CI is intended to publish to:

```text
docker.io/miku0139oao/aster-core
```

The Build workflow currently has no Docker Hub credentials, so the latest `main` run skipped the push. `main` and `latest` on that repository cannot be pulled anonymously. Until that is published, build `aster-core:local` from this repository as described below. The examples use that local tag.

It includes:

- Alpine 3.22 runtime
- `ca-certificates`
- `tzdata`
- `iptables`
- GeoIP/GeoSite data
- Release `aster-core` binary

Default volume:

```text
/root/.config/mihomo
```

Entry point:

```text
/aster-core
```

## Ordinary HTTP/SOCKS

`config/config.yaml`:

```yaml
mixed-port: 7890
allow-lan: true
bind-address: "*"
mode: rule
rules:
  - MATCH,DIRECT
```

Run:

```sh
docker run -d \
  --name aster-core \
  --restart unless-stopped \
  -p 127.0.0.1:7890:7890 \
  -v "$PWD/config:/root/.config/mihomo" \
  miku0139oao/aster-core:latest
```

Even if the host only publishes to loopback, the container still needs `allow-lan: true`. Otherwise Aster binds only the container’s own `127.0.0.1` and Docker port forwarding cannot reach it.

## TUN or transparent proxy

Linux:

```sh
docker run -d \
  --name aster-core \
  --restart unless-stopped \
  --network host \
  --cap-add NET_ADMIN \
  --device /dev/net/tun \
  -v "$PWD/config:/root/.config/mihomo" \
  miku0139oao/aster-core:latest
```

Depending on the profile you may also need `NET_RAW` or host routing/iptables changes. Do not use `--privileged` unless you have confirmed the minimum capabilities are not enough and you accept the risk.

## Controller and Aster API

Host networking can keep the Controller on loopback:

```yaml
external-controller: 127.0.0.1:9090
```

If a bridge network needs the Controller published:

```yaml
external-controller: 0.0.0.0:9090
secret: "replace-with-a-strong-secret"
```

```sh
-p 127.0.0.1:9090:9090
```

::: warning Aster Admin
When a plaintext TCP Controller is bound off loopback, Aster Admin routes are not mounted. If a bridge network needs Aster Admin, prefer an HTTPS Controller, or use host networking and bind the Controller to the host loopback.
:::

Subscription routes can be published through a reverse proxy. Admin routes should not be public.

## Persistent files

Persist the entire config home:

```text
config.yaml
cache.db
aster-state.json
aster-state.json.bak
providers/
rules/
certificates
```

If you only bind-mount a single `config.yaml`, Aster users, traffic, subscriptions, and provider cache are lost when the container is recreated.

## Health check

The image itself has no built-in `HEALTHCHECK`. You can use the Controller if you need one:

```sh
curl -fsS \
  -H "Authorization: Bearer $CONTROLLER_SECRET" \
  http://127.0.0.1:9090/version
```

or Aster:

```sh
curl -fsS \
  -H "Authorization: Bearer $ASTER_SECRET" \
  http://127.0.0.1:9090/api/admin/status
```

Do not put secrets in container metadata that every user can read.

## Build an image from the repository

The repository Dockerfile **does not compile Go**. It needs:

```text
bin/version.txt
bin/aster-core-linux-<arch>-<version>.gz
```

So a clean clone and `docker build .` will fail.

Local amd64 example:

```sh
VERSION=local
printf '%s\n' "$VERSION" > bin/version.txt
make VERSION="$VERSION" linux-amd64-v1.gz
docker buildx build \
  --load \
  --platform linux/amd64 \
  -t aster-core:local \
  .
```

CI publish platforms:

- `linux/386`
- `linux/amd64`
- `linux/arm64`
- `linux/arm/v7`

## Docker Desktop

Docker Desktop host networking, TUN devices, and route capabilities differ from native Linux. Use `-p` for an ordinary proxy. For transparent proxy, validate first on a Linux VM, a WSL network namespace, or a physical Linux host.

## Updates

Before updating:

1. Back up the config and Aster state.
2. Read the release notes.
3. Run `-t` with the new image first.
4. Keep the old image digest so you can roll back.

Do not track a mutable `latest` without a rollback record. Production should pin a release tag or digest.
