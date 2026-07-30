# 快速開始

::: tip 想要完整可照做的流程？
本頁是精簡啟動參考。如果要從 release、第一個遠端節點、完整 DNS/rules 一路做到 `curl` 驗證，請直接閱讀[第一個代理設定實戰](/tutorials/first-proxy)。
:::

## 系統需求

- Go 1.20 或更新版本（從原始碼建置時）。
- 一般 HTTP/SOCKS 使用不需要 root。
- TUN、TProxy、Redir、iptables 或低連接埠可能需要 root、capabilities 或平台權限。
- `with_gvisor` 是正式發行的標準 build tag。

## 取得 Aster Core

### GitHub Releases

從 [Releases](https://github.com/Miku0139oao/aster-core/releases) 選擇作業系統與 CPU 架構。舊款 x86-64 CPU 優先使用 `amd64-v1` 或 `amd64-compatible`。

下載後先檢查 release 中的 checksum，再賦予執行權限：

```sh
chmod +x aster-core
./aster-core -v
```

### 從原始碼建置

```sh
git clone https://github.com/Miku0139oao/aster-core.git
cd aster-core
go mod download
CGO_ENABLED=0 go build -tags with_gvisor -trimpath -o aster-core .
./aster-core -v
```

PowerShell 可先設定：

```powershell
$env:CGO_ENABLED = "0"
go build -tags with_gvisor -trimpath -o aster-core.exe .
```

## 建立最小設定

建立 `config/config.yaml`：

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

這份設定的用途是確認核心可以啟動：

- `127.0.0.1:7890`：HTTP 與 SOCKS mixed proxy。
- `127.0.0.1:9090`：Controller API。
- `MATCH,DIRECT`：所有流量直連，不會使用遠端代理。

::: warning
這不是正式代理 profile。加入遠端 proxy、proxy group 與分流規則後，才會真正透過代理送出流量。
:::

## 驗證設定

```sh
./aster-core -d ./config -f ./config/config.yaml -t
```

成功時會顯示：

```text
configuration file ... test is successful
```

`-t` 只負責解析與驗證，不會持續啟動 listeners。

## 啟動

```sh
./aster-core -d ./config -f ./config/config.yaml
```

測試代理：

```sh
curl -x http://127.0.0.1:7890 https://example.com/
```

測試 Controller：

```sh
curl \
  -H 'Authorization: Bearer replace-this-controller-secret' \
  http://127.0.0.1:9090/version
```

## 重新載入與關閉

Unix：

```sh
kill -HUP <pid>   # 重新讀取檔案型設定
kill -TERM <pid>  # 正常關閉
```

檔案模式的 `SIGHUP` 會重新讀取磁碟；stdin 或 Base64 模式只會重新套用啟動時保留在記憶體的內容。

## 下一步

- [建立第一個完整代理設定](/tutorials/first-proxy)
- [路由與 DNS 分流](/tutorials/routing-dns)
- [理解設定來源與安全路徑](/guide/configuration)
- [加入 proxies 與 proxy groups](/reference/outbounds)
- [設定 rules 與 DNS](/reference/routing-dns)
- [啟用 Aster 使用者管理](/aster/overview)
- [部署到 Docker](/deployment/docker)
