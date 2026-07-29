# 設定概念

## 設定來源優先順序

Aster Core 依下列順序決定設定來源：

1. `--config` 或 `CLASH_CONFIG_STRING`：Base64 編碼 YAML。
2. `-f -` 或 `CLASH_CONFIG_FILE=-`：標準輸入。
3. `-f <file>` 或 `CLASH_CONFIG_FILE`：指定檔案。
4. Home directory 下的 `config.yaml`。

```sh
# 指定檔案
aster-core -d /etc/mihomo -f /etc/mihomo/config.yaml

# stdin
aster-core -d /etc/mihomo -f - < /etc/mihomo/config.yaml

# Base64
aster-core --config '<base64-data>'
```

## Home directory

| 平台 | 預設目錄 |
| --- | --- |
| Unix | `$HOME/.config/mihomo` |
| Windows | `%USERPROFILE%\.config\mihomo` |

若預設目錄不存在且設定了 `XDG_CONFIG_HOME`，程式會使用 `$XDG_CONFIG_HOME/mihomo`。

`-d` 會改變：

- 預設 `config.yaml` 位置。
- 相對資產與 provider cache 的根目錄。
- `cache.db`、GeoIP/GeoSite 等資料位置。
- Aster 預設 state 路徑。

相對 `-d` 與相對 `-f` 都以**目前工作目錄**解析；相對 `-f` 不會以 `-d` 為基準。

## 安全路徑

設定內引用的檔案預設必須位於 home directory。例如 certificate、private key、provider path 與 Aster store 都應放在該目錄。

要允許其他可信目錄，使用作業系統 path-list 格式：

```sh
SAFE_PATHS=/etc/aster:/srv/aster aster-core -d /etc/mihomo
```

Windows：

```powershell
$env:SAFE_PATHS = "D:\certs;D:\providers"
```

`SKIP_SAFE_PATH_CHECK=true` 會停用此防護。除非執行環境已被其他 sandbox 完整隔離，否則不建議使用。

## Age 加密設定

建立 key：

```sh
aster-core age keygen
```

用 public key 加密：

```sh
aster-core age encrypt <public-key> config.yaml config.age
```

啟動：

```sh
aster-core -f config.age --age-secret-key '<secret-key>'
```

也可使用 `CLASH_AGE_SECRET_KEY`，避免把 secret 直接放在命令歷史。

## 頂層結構

常見設定可分為：

| 區塊 | 用途 |
| --- | --- |
| General | ports、mode、logging、LAN、interface、routing mark |
| `dns` | DNS server、fake-IP、nameserver、fallback、policy |
| `tun` | TUN interface、route、DNS hijack |
| `proxies` | 靜態出站節點 |
| `proxy-groups` | 選擇、健康檢查、fallback、負載平衡 |
| `proxy-providers` | 遠端或本機代理清單 |
| `rule-providers` | 遠端或本機規則集 |
| `rules` | 依序比對的流量規則 |
| `listeners` | 額外具名入站 server |
| `external-controller*` | Controller transports |
| `tls` | Controller TLS 與共用憑證設定 |
| `aster` | Aster 使用者管理 |

完整範例可直接開啟 [`config.yaml`](/config.yaml)，分類說明請閱讀[設定總覽](/reference/configuration)。

## 驗證策略

每次變更都先執行：

```sh
aster-core -d /path/to/home -f /path/to/config.yaml -t
```

`-t` 通過仍不保證所有 runtime 條件成立，例如：

- 連接埠已被其他程序使用。
- certificate 檔案權限不足。
- TUN 或 iptables 權限不足。
- provider URL 無法連線。
- REALITY destination 或 SNI 無法使用。

正式切換前應在目標平台進行實際 TCP、UDP、DNS、IPv4、IPv6 與透明代理測試。
