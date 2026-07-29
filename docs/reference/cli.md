# 命令列與環境變數

## 主程式 flags

| Flag | 環境變數 | 說明 |
| --- | --- | --- |
| `-d <dir>` | `CLASH_HOME_DIR` | 設定與資料 home directory |
| `-f <file>` | `CLASH_CONFIG_FILE` | 設定檔；`-` 表示 stdin |
| `--config <base64>` | `CLASH_CONFIG_STRING` | Base64 編碼 YAML |
| `--age-secret-key <key>` | `CLASH_AGE_SECRET_KEY` | Age 解密 secret key |
| `--ext-ui <dir>` | `CLASH_OVERRIDE_EXTERNAL_UI_DIR` | 覆寫 external UI directory |
| `--ext-ctl <addr>` | `CLASH_OVERRIDE_EXTERNAL_CONTROLLER` | 覆寫 HTTP Controller address |
| `--ext-ctl-tls <addr>` | `CLASH_OVERRIDE_EXTERNAL_CONTROLLER_TLS` | 覆寫 HTTPS Controller address |
| `--ext-ctl-unix <path>` | `CLASH_OVERRIDE_EXTERNAL_CONTROLLER_UNIX` | 覆寫 Unix socket |
| `--ext-ctl-pipe <path>` | `CLASH_OVERRIDE_EXTERNAL_CONTROLLER_PIPE` | 覆寫 Windows named pipe |
| `--ext-ctl-routing-mark <n>` | `CLASH_OVERRIDE_EXTERNAL_CONTROLLER_ROUTING_MARK` | Linux Controller socket mark |
| `--secret <secret>` | `CLASH_OVERRIDE_SECRET` | 覆寫一般 Controller secret |
| `--post-up <command>` | `CLASH_POST_UP` | Runtime 啟動後執行 shell |
| `--post-down <command>` | `CLASH_POST_DOWN` | Runtime 關閉時執行 shell |
| `-m` | — | 啟用 geodata mode |
| `-t` | — | 驗證設定並離開 |
| `-v` | — | 顯示版本、平台、Go、build time 與 tags |

::: danger Shell 指令
`--post-up` 與 `--post-down` 直接透過系統 shell 執行。不要讓 API、使用者輸入或不可信設定來源控制這兩個值。
:::

## 設定來源範例

```sh
# 預設 <home>/config.yaml
aster-core

# 指定 home 與 file
aster-core -d /etc/mihomo -f /etc/mihomo/config.yaml

# stdin
aster-core -d /etc/mihomo -f -

# Base64
aster-core --config "$(base64 -w0 config.yaml)"
```

## 產生 credentials 與 keys

### UUID

```sh
aster-core generate uuid
```

### REALITY X25519

```sh
aster-core generate reality-keypair
```

輸出 `PrivateKey` 與 `PublicKey`。Server 使用 private key，client 使用 public key。

### WireGuard

```sh
aster-core generate wg-keypair
```

### ECH

```sh
aster-core generate ech-keypair example.com
```

### VLESS Encryption

```sh
aster-core generate vless-mlkem768
aster-core generate vless-x25519
```

可選擇傳入既有 seed/private key，命令會同時輸出 server 與 client 建議設定。

### Sudoku

```sh
aster-core generate sudoku-keypair
```

## Age 設定加密

```sh
# X25519 identity
aster-core age keygen

# ML-KEM-768 + X25519 hybrid identity
aster-core age keygen-pq

# Secret key 轉 public recipient
aster-core age convert <secret-key>

# 加密與解密；source/target 可用 - 表示 stdio
aster-core age encrypt <public-key> <source> <target>
aster-core age decrypt <secret-key> <source> <target>
```

## Rule set 轉換

```sh
aster-core convert-ruleset <behavior> <format> <source> <target>
```

此工具可在 text/YAML/MRS 等支援格式間轉換 rule provider 資料。`behavior` 必須符合 `domain`、`ipcidr` 或 `classical` 等 provider 行為。

## 進階環境變數

| 變數 | 用途 |
| --- | --- |
| `XDG_CONFIG_HOME` | 預設 Mihomo 目錄不存在時使用的設定根目錄 |
| `SAFE_PATHS` | 額外允許的檔案根目錄，使用 OS path-list 分隔 |
| `SKIP_SAFE_PATH_CHECK` | 完全停用安全路徑檢查 |
| `DISABLE_EMBED_CA` | 不使用內嵌 CA bundle |
| `DISABLE_SYSTEM_CA` | 不載入系統 CA |
| `DISABLE_SYSTEM_HOSTS` | 不查詢系統 hosts |
| `DISABLE_LOOPBACK_DETECTOR` | 停用 loopback connection detector |
| `FORCE_ANET` | 強制使用 anet path |
| `HOST_PROC` | Linux procfs root，預設 `/proc` |
| `SKIP_SYSTEM_IPV6_CHECK` | 跳過系統 IPv6 能力偵測 |
| `DISABLE_NFTABLES` | TUN auto-redirect 不使用 nftables |
| `DISABLE_OVERRIDE_ANDROID_VPN` | 停用 Android VPN interface override |
| `LISTEN_NAMEDPIPE_SDDL` | Windows named pipe SDDL |
| `QUIC_GO_DISABLE_GSO` | 停用 quic-go GSO |
| `QUIC_GO_DISABLE_ECN` | 停用 quic-go ECN |

## Signals 與結束行為

| Signal | 行為 |
| --- | --- |
| `SIGHUP` | 重新套用設定；檔案模式會重新讀檔 |
| `SIGINT` | 正常關閉 |
| `SIGTERM` | 正常關閉 |

正常關閉會執行 runtime shutdown、關閉 listeners、清理自動 iptables、保存 fake-IP，並 flush Aster traffic。
