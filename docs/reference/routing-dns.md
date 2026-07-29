# 規則與 DNS

## Rules 的比對順序

`rules` 由上往下比對，第一個命中的規則決定出站：

```yaml
rules:
  - DOMAIN-SUFFIX,example.com,PROXY
  - GEOIP,PRIVATE,DIRECT,no-resolve
  - MATCH,PROXY
```

常見規則：

| 類型 | 範例 |
| --- | --- |
| `DOMAIN` | `DOMAIN,www.example.com,PROXY` |
| `DOMAIN-SUFFIX` | `DOMAIN-SUFFIX,example.com,PROXY` |
| `DOMAIN-KEYWORD` | `DOMAIN-KEYWORD,video,PROXY` |
| `DOMAIN-REGEX` | `DOMAIN-REGEX,^api\\.,PROXY` |
| `DOMAIN-WILDCARD` | `DOMAIN-WILDCARD,*.example.com,PROXY` |
| `IP-CIDR` | `IP-CIDR,10.0.0.0/8,DIRECT,no-resolve` |
| `IP-CIDR6` | `IP-CIDR6,2001:db8::/32,DIRECT` |
| `IP-SUFFIX` | 依 IP suffix |
| `GEOIP` | `GEOIP,TW,DIRECT` |
| `GEOSITE` | `GEOSITE,category-ads-all,REJECT` |
| `IP-ASN` | `IP-ASN,13335,PROXY` |
| `DST-PORT` | `DST-PORT,443,PROXY` |
| `SRC-PORT` | 依來源 port |
| `PROCESS-NAME` | `PROCESS-NAME,curl,DIRECT` |
| `PROCESS-PATH` | 依 process path |
| `UID` | 依 Unix/Android UID |
| `IN-NAME` | 依 named listener |
| `IN-TYPE` | 依 inbound type |
| `IN-USER` | 依 authenticated user |
| `NETWORK` | TCP/UDP |
| `DSCP` | 依 DSCP |
| `RULE-SET` | 引用 rule provider |
| `AND`、`OR`、`NOT` | 邏輯規則 |
| `MATCH` | 無條件 final |

## Rule providers

```yaml
rule-providers:
  private:
    type: http
    behavior: ipcidr
    format: mrs
    url: https://example.com/private.mrs
    path: ./rules/private.mrs
    interval: 86400

rules:
  - RULE-SET,private,DIRECT
  - MATCH,PROXY
```

Behavior：

- `domain`
- `ipcidr`
- `classical`

Format 依 provider parser 支援 text、YAML 與 MRS 等形式。MRS 是壓縮二進位格式，適合大型規則。

## Sub-rules

Sub-rule 可把複雜規則分支獨立命名，再由主 rules 使用 `SUB-RULE`。適合依 inbound、network 或用途分拆，但應避免循環引用。

## DNS 基本設定

```yaml
dns:
  enable: true
  listen: 0.0.0.0:1053
  ipv6: true
  enhanced-mode: fake-ip
  fake-ip-range: 198.18.0.1/16
  nameserver:
    - https://1.1.1.1/dns-query
    - tls://8.8.8.8
```

支援的上游包含：

- UDP/TCP DNS
- DoH
- DoT
- DoQ
- DHCP
- System resolver

## Fake-IP

Fake-IP 會對 DNS client 回傳保留範圍位址，再在流量進入 tunnel 時還原原始網域，以提升 domain rule 與抗污染能力。

```yaml
dns:
  enhanced-mode: fake-ip
  fake-ip-filter:
    - "*.lan"
    - "*.local"
    - time.*.com
```

不適合 fake-IP 的 LAN、mDNS、時間同步或特殊服務應加入 filter。

## Nameserver policy

```yaml
dns:
  nameserver:
    - https://1.1.1.1/dns-query
  nameserver-policy:
    "+.internal.example.com":
      - 10.0.0.53
    "geosite:cn":
      - https://dns.alidns.com/dns-query
```

Policy 可依 domain matcher 選擇不同 resolver。

## Fallback

Fallback 可在主要 resolver 結果不可信或符合條件時使用另一組上游：

```yaml
dns:
  fallback:
    - https://8.8.8.8/dns-query
  fallback-filter:
    geoip: true
    geoip-code: TW
    ipcidr:
      - 240.0.0.0/4
```

Fallback 策略會增加 DNS latency 與複雜度。先確認實際威脅模型，不要只因範例存在就全部開啟。

## DNS hijack 與 TUN

```yaml
tun:
  enable: true
  dns-hijack:
    - any:53
```

DNS hijack 需要內部 DNS 已啟用。容器、路由器或多 DNS server 環境中，應確認：

- 53/UDP 與 53/TCP 的流向。
- Aster 自己的 upstream query 不會被再次 hijack。
- IPv6 DNS 是否同時處理。
- systemd-resolved、dnsmasq 或 odhcpd 是否造成 port 衝突。

## Cache

Controller 提供 fake-IP 與 DNS cache flush route。變更 hosts、policy 或 provider 後若結果未立即更新，可先清 cache，再確認不是上游 TTL 或系統 resolver cache。
