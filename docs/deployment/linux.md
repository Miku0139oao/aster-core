# Linux 套件與 systemd

::: tip 正式環境逐步部署
要從 release asset、檔案權限、防火牆、systemd、驗證一路做到更新與回退，請使用[Linux VPS 正式部署實戰](/tutorials/linux-production)。
:::

## Release packages

發行流程可產生：

- Debian/Ubuntu `.deb`
- RPM `.rpm`
- Arch/Pacman `.pkg.tar.zst`

套件安裝：

```text
/usr/bin/aster-core
/usr/bin/mihomo -> aster-core compatibility
/etc/mihomo/config.yaml
aster-core.service
aster-core@.service
```

## 安裝前驗證

```sh
sha256sum --check --ignore-missing checksums.txt
```

請在已下載的目標 asset 與 `checksums.txt` 所在目錄執行。Checksum 清單包含該 release 的所有 assets；只下載一個套件時，不加 `--ignore-missing` 會把其他未下載檔案報成失敗。

套件安裝後：

```sh
sudo /usr/bin/aster-core -d /etc/mihomo -t
```

## 目錄與權限

建議：

```sh
sudo install -d -m 700 /etc/mihomo
sudo chmod 600 /etc/mihomo/config.yaml
sudo chmod 600 /etc/mihomo/aster-state.json* 2>/dev/null || true
```

如果 service 使用專用 user，請把 owner 改成該 account。Aster store directory 必須符合 owner-only 規則。

## 主服務

```sh
sudo systemctl enable --now aster-core
sudo systemctl status aster-core
sudo journalctl -u aster-core -f
```

Unit 執行：

```text
/usr/bin/aster-core -d /etc/mihomo
```

重新載入：

```sh
sudo /usr/bin/aster-core -d /etc/mihomo -t
sudo systemctl reload aster-core
```

先 `-t`，再 reload，避免把明顯無效設定交給執行中服務。

## 多實例

```sh
sudo systemctl enable --now aster-core@edge
```

設定目錄：

```text
/etc/mihomo/edge
```

每個 instance 必須使用不同：

- Proxy/listener ports
- Controller address
- TUN device/name
- Aster store
- Unix socket/named resource

不要讓兩個 instance 指向同一份 Aster state；store generation 與 lock 雖可避免部分衝突，但 runtime listener ownership 仍不成立。

## Capabilities

Package unit 可能授予：

- `CAP_NET_ADMIN`
- `CAP_NET_RAW`
- `CAP_NET_BIND_SERVICE`
- `CAP_SYS_TIME`
- `CAP_SYS_PTRACE`
- `CAP_DAC_READ_SEARCH`
- `CAP_DAC_OVERRIDE`

這組能力涵蓋多種 Mihomo 功能，但權限很高。若只跑 HTTP/SOCKS client，可建立更小權限的自訂 unit。

## 手動 binary service

範例：

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

TUN/transparent proxy 需要額外 capabilities。若不需要，不要照抄 package unit 的完整權限。

## Geodata

Docker image 內建 geodata；binary/package 部署時，依設定可能在首次使用時下載，或需要自行放入 home directory：

- `geoip.metadb`
- `GeoIP.dat`
- `GeoSite.dat`
- `ASN.mmdb`

上線環境若無外網，應在部署 artifact 中一併提供並驗證來源。

## Upgrade/rollback

1. 備份 `/etc/mihomo`。
2. 保存舊 binary/package。
3. 用新 binary 執行 `-t`。
4. 停止或 reload service。
5. 驗證 Controller、DNS、TCP、UDP 與 managed users。
6. 發現問題時回復 binary 與相容 state/config。

Aster state version 不支援時會拒絕載入，不應手動修改 `version` 欄位繞過。
