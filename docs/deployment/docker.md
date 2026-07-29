# Docker

## 使用官方映像

映像發布到：

```text
docker.io/miku0139oao/aster-core
```

包含：

- Alpine 3.22 runtime
- `ca-certificates`
- `tzdata`
- `iptables`
- GeoIP/GeoSite data
- Release `aster-core` binary

預設 volume：

```text
/root/.config/mihomo
```

Entry point：

```text
/aster-core
```

## 一般 HTTP/SOCKS

`config/config.yaml`：

```yaml
mixed-port: 7890
allow-lan: true
bind-address: "*"
mode: rule
rules:
  - MATCH,DIRECT
```

執行：

```sh
docker run -d \
  --name aster-core \
  --restart unless-stopped \
  -p 127.0.0.1:7890:7890 \
  -v "$PWD/config:/root/.config/mihomo" \
  miku0139oao/aster-core:latest
```

即使 host 只 publish 到 loopback，container 內仍需 `allow-lan: true`，否則 Aster 只綁 container 自己的 `127.0.0.1`，Docker port forwarding 無法到達。

## TUN 或透明代理

Linux：

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

視設定可能還需要 `NET_RAW` 或 host routing/iptables 調整。不要直接使用 `--privileged`，除非已確認最小 capabilities 無法滿足且接受其風險。

## Controller 與 Aster API

Host network 可讓 loopback Controller 保持：

```yaml
external-controller: 127.0.0.1:9090
```

Bridge network 若需要 publish Controller：

```yaml
external-controller: 0.0.0.0:9090
secret: "replace-with-a-strong-secret"
```

```sh
-p 127.0.0.1:9090:9090
```

::: warning Aster Admin
明文 TCP Controller 綁定非 loopback address 時，Aster Admin routes 不會掛載。Bridge network 若需要 Aster Admin，建議使用 HTTPS Controller，或讓 container 使用 host network 並把 Controller 綁 host loopback。
:::

Subscription routes 可經過 reverse proxy 對外發布；admin routes 不應直接 public。

## Persistent files

應持久化整個 config home：

```text
config.yaml
cache.db
aster-state.json
aster-state.json.bak
providers/
rules/
certificates
```

如果只 bind mount 單一 `config.yaml`，container recreation 後 Aster users、traffic、subscriptions 與 provider cache 會遺失。

## Health check

映像本身沒有內建 `HEALTHCHECK`。可依需求使用 Controller：

```sh
curl -fsS \
  -H "Authorization: Bearer $CONTROLLER_SECRET" \
  http://127.0.0.1:9090/version
```

或 Aster：

```sh
curl -fsS \
  -H "Authorization: Bearer $ASTER_SECRET" \
  http://127.0.0.1:9090/api/admin/status
```

不要把 secret 直接寫進會被所有使用者讀取的 container metadata。

## 從 repository 建 image

Repository 的 Dockerfile **不會編譯 Go**。它需要：

```text
bin/version.txt
bin/aster-core-linux-<arch>-<version>.gz
```

因此乾淨 clone 直接 `docker build .` 會失敗。

本機 amd64 範例：

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

CI 發布平台：

- `linux/386`
- `linux/amd64`
- `linux/arm64`
- `linux/arm/v7`

## Docker Desktop

Docker Desktop 的 host networking、TUN device 與 route 能力和原生 Linux 不同。一般 proxy 使用 `-p`；需要透明代理時，優先在 Linux VM、WSL network namespace 或實體 Linux host 驗證。

## 更新

更新前：

1. 備份 config 與 Aster state。
2. 檢查 release notes。
3. 先用新 image 執行 `-t`。
4. 保留舊 image digest 以便 rollback。

不要只追蹤 mutable `latest` 而沒有 rollback 記錄；正式環境建議 pin release tag 或 digest。
