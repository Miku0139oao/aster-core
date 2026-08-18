# Rules and DNS

## Rule match order

`rules` match from top to bottom. The first hit decides the outbound:

```yaml
rules:
  - DOMAIN-SUFFIX,example.com,PROXY
  - GEOIP,PRIVATE,DIRECT,no-resolve
  - MATCH,PROXY
```

Common rules:

| Type | Example |
| --- | --- |
| `DOMAIN` | `DOMAIN,www.example.com,PROXY` |
| `DOMAIN-SUFFIX` | `DOMAIN-SUFFIX,example.com,PROXY` |
| `DOMAIN-KEYWORD` | `DOMAIN-KEYWORD,video,PROXY` |
| `DOMAIN-REGEX` | `DOMAIN-REGEX,^api\\.,PROXY` |
| `DOMAIN-WILDCARD` | `DOMAIN-WILDCARD,*.example.com,PROXY` |
| `IP-CIDR` | `IP-CIDR,10.0.0.0/8,DIRECT,no-resolve` |
| `IP-CIDR6` | `IP-CIDR6,2001:db8::/32,DIRECT` |
| `IP-SUFFIX` | By IP suffix |
| `GEOIP` | `GEOIP,TW,DIRECT` |
| `GEOSITE` | `GEOSITE,category-ads-all,REJECT` |
| `IP-ASN` | `IP-ASN,13335,PROXY` |
| `DST-PORT` | `DST-PORT,443,PROXY` |
| `SRC-PORT` | By source port |
| `PROCESS-NAME` | `PROCESS-NAME,curl,DIRECT` |
| `PROCESS-PATH` | By process path |
| `UID` | By Unix/Android UID |
| `IN-NAME` | By named listener |
| `IN-TYPE` | By inbound type |
| `IN-USER` | By authenticated user |
| `NETWORK` | TCP/UDP |
| `DSCP` | By DSCP |
| `RULE-SET` | Reference a rule provider |
| `AND`, `OR`, `NOT` | Logical rules |
| `MATCH` | Unconditional final |

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

Behaviors:

- `domain`
- `ipcidr`
- `classical`

Format depends on the provider parser and includes text, YAML, and MRS. MRS is a compressed binary format that fits large rule sets.

## Sub-rules

A sub-rule can name a complex branch, then the main rules use `SUB-RULE`. That is useful when you split by inbound, network, or purpose. Avoid cyclic references.

## Basic DNS configuration

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

Supported upstreams include:

- UDP/TCP DNS
- DoH
- DoT
- DoQ
- DHCP
- System resolver

## Fake-IP

Fake-IP returns a reserved-range address to the DNS client, then restores the original domain when traffic enters the tunnel. That improves domain-rule matching and resistance to poisoned answers.

```yaml
dns:
  enhanced-mode: fake-ip
  fake-ip-filter:
    - "*.lan"
    - "*.local"
    - time.*.com
```

LAN, mDNS, time sync, or special services that should not use fake-IP belong in the filter.

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

Policy can pick a different resolver from a domain matcher.

## Fallback

Fallback can use another upstream set when the primary result is untrusted or matches a condition:

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

Fallback adds DNS latency and complexity. Confirm the real threat model first. Do not enable everything just because the example includes it.

## DNS hijack and TUN

```yaml
tun:
  enable: true
  dns-hijack:
    - any:53
```

DNS hijack requires internal DNS to be enabled. In containers, routers, or multi-DNS environments, confirm:

- Where 53/UDP and 53/TCP go.
- That Aster’s own upstream queries are not hijacked again.
- Whether IPv6 DNS is handled too.
- Whether systemd-resolved, dnsmasq, or odhcpd creates a port conflict.

## Cache

The Controller provides fake-IP and DNS cache flush routes. If results do not update immediately after you change hosts, policy, or a provider, flush cache first, then confirm it is not upstream TTL or the system resolver cache.
