# Routing and DNS: fake-IP, policy, rule providers, and leak prevention

This tutorial combines, in one complete profile:

- fake-IP DNS.
- `nameserver-policy`.
- `domain`, `ipcidr`, and `classical` rule providers.
- Rules from precise exceptions down to a final `MATCH`.
- Controller DNS queries, rule hit counters, and provider status.
- Common DNS, IPv6, UDP, and TUN leak checks.

Finish [Your first proxy profile](/en/tutorials/first-proxy) first and confirm a single proxy works before adding complex routing. Otherwise “the node is broken” and “the rules / DNS are wrong” are hard to tell apart.

## Goal

Traffic should end up like this:

| Kind | Result |
| --- | --- |
| Ad / tracker test domains | `REJECT` |
| LAN / internal domains and RFC 1918 addresses | `DIRECT` |
| `example.com` | `DIRECT`, as a reproducible rule test |
| `ipify.org` | `PROXY`, as a reproducible proxy test |
| Everything else | `PROXY` |
| Ordinary DNS names | Return a fake-IP, then Aster restores the domain |
| Private, mDNS, and time-sync names | Do not use fake-IP |

## Prerequisites

You need:

- Aster Core that can start normally.
- One already-working proxy. The complete YAML on this page uses AnyTLS + REALITY; every `<...>` must be replaced with your values.
- `127.0.0.1:7890`, `:1053`, and `:9090` not taken by another program.
- Optional: `dig`, `jq`, and `tcpdump` to inspect results.

::: warning Test on loopback first
This page keeps DNS, the mixed proxy, and the Controller listening on `127.0.0.1` only. Do not change them to `0.0.0.0` just so the LAN can connect. DNS recursion, an unauthenticated proxy, and the Controller each need their own firewall, ACL, and authentication design.
:::

## 1. Understand the fake-IP data path first

Ordinary DNS mode hands the real IP from upstream back to the application. Fake-IP mode is:

1. The application queries Aster DNS for `www.example.org`.
2. Aster returns a reserved address in `198.18.0.0/16`.
3. Traffic enters Aster through the mixed / TUN / transparent-proxy inbound.
4. Aster restores `www.example.org` from the fake-IP mapping.
5. Domain rules match the original domain, then choose `DIRECT`, `PROXY`, or `REJECT`.

The benefit is that domain rules do not depend on a possibly poisoned local lookup. The important requirement is that **DNS and the later traffic both go through the same Aster runtime**. If an app gets `198.18.x.x` and then connects out the physical NIC, that host does not exist on the network and the connection fails.

`fake-ip-filter` is for names that should not use fake-IP, for example:

- `.lan`, `.local`, and mDNS.
- Printers, NAS boxes, game consoles, or captive portals that need a real IP.
- NTP, connectivity checks, or some P2P / VoIP services.

## 2. Write the complete cookbook profile

Create or review `config.yaml` under the Aster home. The profile below is a complete structure; replace every `<...>` first:

```yaml
mixed-port: 7890
allow-lan: false
mode: rule

# Use debug during the tutorial, then change it back to info.
log-level: debug
ipv6: false

external-controller: 127.0.0.1:9090
secret: "<CONTROLLER_SECRET>"

profile:
  store-selected: true
  store-fake-ip: true

dns:
  enable: true
  listen: 127.0.0.1:1053
  ipv6: false
  cache-algorithm: arc
  enhanced-mode: fake-ip
  fake-ip-range: 198.18.0.1/16

  # Bootstrap resolvers may only be raw IPs or system.
  default-nameserver:
    - 1.1.1.1
    - 8.8.8.8

  # Ordinary queries use encrypted DNS. IP literals avoid an extra hostname bootstrap.
  nameserver:
    - https://1.1.1.1/dns-query
    - https://8.8.8.8/dns-query

  # Resolve proxy node hostnames so you do not create a “need a proxy to resolve the proxy” loop.
  proxy-server-nameserver:
    - https://1.1.1.1/dns-query
    - https://8.8.8.8/dns-query

  # Default false: nameserver / fallback connections do not go through rules again.
  respect-rules: false

  # In blacklist mode, hits use a real IP; non-hits use fake-IP.
  fake-ip-filter-mode: blacklist
  fake-ip-filter:
    - "*.lan"
    - "*.local"
    - "time.*.com"
    - "rule-set:private-domains"

  # Ordinary domain matchers, GEOSITE, or domain/classical rule sets are all valid.
  nameserver-policy:
    "rule-set:private-domains":
      - system
    "+.example.com":
      - https://1.1.1.1/dns-query

proxies:
  - name: Edge-AnyTLS-REALITY
    type: anytls
    server: <ASTER_SERVER_HOST_OR_IP>
    port: 443
    password: "<ANYTLS_PASSWORD>"
    sni: <REALITY_SNI_FROM_SERVER>
    client-fingerprint: chrome
    reality-opts:
      public-key: <REALITY_PUBLIC_KEY>
      short-id: <REALITY_SHORT_ID>
    udp: true

proxy-groups:
  - name: PROXY
    type: select
    proxies:
      - Edge-AnyTLS-REALITY
      - DIRECT

rule-providers:
  # domain behavior payloads contain only domain matchers.
  private-domains:
    type: inline
    behavior: domain
    payload:
      - "+.lan"
      - "+.local"
      - "+.internal.example"

  # ipcidr behavior payloads contain only IP prefixes.
  private-cidrs:
    type: inline
    behavior: ipcidr
    payload:
      - 10.0.0.0/8
      - 172.16.0.0/12
      - 192.168.0.0/16
      - 127.0.0.0/8
      - 169.254.0.0/16

  # classical behavior holds full rules, but without a final target.
  tutorial-ads:
    type: inline
    behavior: classical
    payload:
      - DOMAIN-SUFFIX,ads.example
      - DOMAIN-KEYWORD,tracker

rules:
  # Start with the most specific traffic that must not leave the machine.
  - RULE-SET,tutorial-ads,REJECT
  - RULE-SET,private-domains,DIRECT
  - RULE-SET,private-cidrs,DIRECT,no-resolve

  # Reproducible direct and proxy tests.
  - DOMAIN-SUFFIX,example.com,DIRECT
  - DOMAIN-SUFFIX,ipify.org,PROXY

  # The final rule always comes last.
  - MATCH,PROXY
```

If you are not using AnyTLS, replace the `proxies` block using [Outbounds and groups](/en/reference/outbounds). Keep the name `Edge-AnyTLS-REALITY`, or update the `PROXY.proxies` references to match.

## 3. Understand `nameserver-policy`

`nameserver-policy` picks a different resolver from the queried name. The example has two entries:

```yaml
nameserver-policy:
  "rule-set:private-domains":
    - system
  "+.example.com":
    - https://1.1.1.1/dns-query
```

- Names that hit `private-domains` go to the system resolver, which is a good fit for internal DNS from DHCP / VPN.
- `example.com` and its subdomains always go to Cloudflare DoH.
- Names that miss the policy use `nameserver`.

Valid key forms include:

```yaml
nameserver-policy:
  "www.example.com": 1.1.1.1
  "+.corp.example": 10.0.0.53
  "geosite:private": system
  "rule-set:internal-domains":
    - 10.0.0.53
    - 10.0.0.54
```

`default-nameserver` is not an ordinary domain policy. It bootstraps DoH / DoT upstreams that contain a hostname, so the program requires a raw-IP resolver or `system`. Do not put a value such as `https://dns.example/dns-query` there; that still needs a hostname lookup first.

A `rule-set:` policy may only reference:

- `behavior: domain`.
- `behavior: classical`, but only the domain-class rules take part in DNS policy.

`behavior: ipcidr` has no domain to match, so it cannot be used in `nameserver-policy` or `fake-ip-filter`.

### How to see which upstream was chosen

The tutorial YAML uses `log-level: debug`. During a query you can find:

```text
[DNS] resolve <domain> A from <upstream>
```

`<upstream>` shows the selected DNS address. After you have confirmed it, change `log-level` back to `info` so production does not generate a flood of logs.

## 4. Design rule order

`rules` match from top to bottom; the first hit decides the target. A practical order is usually:

1. Safety blocks and very precise exceptions.
2. LAN / internal domains.
3. Private IPs and IP rules that should not resolve.
4. Explicit direct or proxied services.
5. Larger GEOIP / GEOSITE / rule sets.
6. The final `MATCH`.

The cookbook below can be inserted before `MATCH` in the complete YAML as needed:

```yaml
rules:
  # Single host and suffix.
  - DOMAIN,api.example.com,PROXY
  - DOMAIN-SUFFIX,example.net,DIRECT

  # Keywords and regular expressions should stay specific to avoid collateral hits.
  - DOMAIN-KEYWORD,tracker,REJECT
  - DOMAIN-REGEX,^ads[0-9]*\.,REJECT

  # When an IP rule already has a destination IP, add no-resolve so matching does not trigger a lookup.
  - IP-CIDR,203.0.113.0/24,REJECT,no-resolve
  - IP-CIDR6,2001:db8::/32,REJECT,no-resolve

  # Port / network conditions.
  - DST-PORT,22,DIRECT
  - NETWORK,UDP,PROXY

  - MATCH,PROXY
```

`203.0.113.0/24` and `2001:db8::/32` are documentation reserved ranges. They are not something you should block in a production profile just because they appear here.

::: tip Put domain rules ahead of IP rules that trigger resolution
When metadata still has a hostname, domain rules can match it directly. Putting a large set of resolve-required IP / GEOIP rules first can add DNS latency. Rules that can use `no-resolve` should say so explicitly.
:::

## 5. Choose a rule-provider behavior

### `domain`

Contains only domain matchers. Good for domain blocks, direct lists, and DNS policy:

```yaml
rule-providers:
  streaming-domains:
    type: inline
    behavior: domain
    payload:
      - "+.video.example"
      - "api.media.example"
```

Reference:

```yaml
rules:
  - RULE-SET,streaming-domains,PROXY
```

### `ipcidr`

Contains only IPv4 / IPv6 CIDRs. Good for private networks or an already-curated IP list:

```yaml
rule-providers:
  office-networks:
    type: inline
    behavior: ipcidr
    payload:
      - 10.20.0.0/16
      - 2001:db8:20::/48

rules:
  - RULE-SET,office-networks,DIRECT,no-resolve
```

### `classical`

Each item is full rule syntax, but the provider itself has no target. The outer `RULE-SET` supplies the target:

```yaml
rule-providers:
  mixed-blocklist:
    type: inline
    behavior: classical
    payload:
      - DOMAIN-SUFFIX,ads.example
      - DOMAIN-KEYWORD,telemetry
      - IP-CIDR,203.0.113.0/24,no-resolve

rules:
  - RULE-SET,mixed-blocklist,REJECT
```

Classical is the most flexible, but large lists usually cost more CPU and memory than dedicated domain / ipcidr sets.

## 6. Switch to a local or remote provider

`type: inline` is best for a self-contained tutorial. Production can use `file` or `http`.

### Local YAML provider

Configuration:

```yaml
rule-providers:
  local-direct:
    type: file
    behavior: domain
    format: yaml
    path: ./rules/local-direct.yaml
```

`./rules/local-direct.yaml`:

```yaml
payload:
  - "+.example.com"
  - "updates.example.net"
```

A relative `path` is resolved from the Aster home. By default only safe paths inside the home directory are allowed. If you need another root, set `SAFE_PATHS` explicitly instead of disabling the safe-path check.

### Remote MRS provider

```yaml
rule-providers:
  remote-ads:
    type: http
    behavior: domain
    format: mrs
    url: https://<TRUSTED_PROVIDER_HOST>/rules/ads.mrs
    path: ./rules/remote-ads.mrs
    interval: 86400
    proxy: DIRECT
    size-limit: 5242880
```

Notes:

- `<TRUSTED_PROVIDER_HOST>` must be an HTTPS origin you trust.
- `path` should be a dedicated file under home so providers do not overwrite each other.
- `interval` is in seconds.
- `proxy` controls which outbound / group downloads the provider. If you set `PROXY`, the proxy must already be usable at startup.
- `size-limit` caps an untrusted or abnormal response.
- MRS only supports `domain` and `ipcidr`, not `classical`.

A YAML provider needs a top-level `payload:` (or `rules:`). A text provider is one item per line. MRS is a binary format; convert with Aster:

```sh
aster-core convert-ruleset domain yaml source.yaml target.mrs
aster-core convert-ruleset ipcidr text source.txt target.mrs
```

Do not treat arbitrary subscription content as trusted configuration. A provider can redirect a large amount of traffic. Before you go live, pin the source, use HTTPS, limit size, and keep the previous version for rollback.

## 7. Validate and start

Linux / macOS:

```sh
./aster-core -d ./config -f ./config/config.yaml -t
./aster-core -d ./config -f ./config/config.yaml
```

PowerShell:

```powershell
.\aster-core.exe -d .\config -f .\config\config.yaml -t
.\aster-core.exe -d .\config -f .\config\config.yaml
```

Validation checks:

- Rule targets that exist as a proxy / group.
- `RULE-SET` names that exist.
- Nameserver policy that references a usable domain/classical provider.
- Fake-IP range, filter, and DNS upstream format.
- Provider behavior / format / path.

`-t` does not prove a remote provider URL, internal DNS, or the proxy node can be reached. You still need runtime tests.

## 8. Verify DNS and fake-IP

### Query the DNS listener directly

```sh
dig @127.0.0.1 -p 1053 example.org A +short
```

Ordinary domains should return a fake-IP in `198.18.0.0/16`.

Query a time-sync name from the filter:

```sh
dig @127.0.0.1 -p 1053 time.apple.com A +short
```

If the upstream can resolve it, this should return a real IP, not `198.18.x.x`.

Without `dig`, use the Controller:

```sh
curl -fsS \
  -H 'Authorization: Bearer <CONTROLLER_SECRET>' \
  'http://127.0.0.1:9090/dns/query?name=example.org&type=A'
```

### Flush DNS and fake-IP cache

After you change policy, filters, or providers, old cache can make results look unchanged:

```sh
curl -fsS -X POST \
  -H 'Authorization: Bearer <CONTROLLER_SECRET>' \
  http://127.0.0.1:9090/cache/dns/flush

curl -fsS -X POST \
  -H 'Authorization: Bearer <CONTROLLER_SECRET>' \
  http://127.0.0.1:9090/cache/fakeip/flush
```

Both endpoints return HTTP `204 No Content` on success. The OS, browser, and application may still have their own DNS caches; flush or restart those separately.

## 9. Verify rule hits

Generate three kinds of traffic first:

```sh
# Explicit DIRECT.
curl -I --proxy http://127.0.0.1:7890 https://example.com/

# Explicit PROXY.
curl -fsS --proxy http://127.0.0.1:7890 https://api.ipify.org

# Hitting tutorial-ads should be REJECT quickly.
curl --max-time 3 --proxy http://127.0.0.1:7890 http://ads.example/
```

The terminal log should show `match ... using DIRECT`, `using PROXY[...]`, or `using REJECT`.

Aster keeps hit / miss counters for top-level rules. Read them from the Controller:

```sh
curl -fsS \
  -H 'Authorization: Bearer <CONTROLLER_SECRET>' \
  http://127.0.0.1:9090/rules
```

With `jq`:

```sh
curl -fsS \
  -H 'Authorization: Bearer <CONTROLLER_SECRET>' \
  http://127.0.0.1:9090/rules |
  jq '.rules[] | {
    index,
    type,
    payload,
    proxy,
    hitCount: .extra.hitCount,
    missCount: .extra.missCount
  }'
```

If traffic succeeds but the expected rule’s `hitCount` does not increase:

- An earlier rule already matched.
- The application is not using the Aster proxy / TUN.
- The SOCKS client resolved locally, so metadata is only an IP and domain rules cannot match.
- The browser reused an existing connection and did not open a new one.

### Check provider load status

```sh
curl -fsS \
  -H 'Authorization: Bearer <CONTROLLER_SECRET>' \
  http://127.0.0.1:9090/providers/rules
```

Ask an HTTP / file provider to update:

```sh
curl -fsS -X PUT \
  -H 'Authorization: Bearer <CONTROLLER_SECRET>' \
  http://127.0.0.1:9090/providers/rules/remote-ads
```

Inline providers do not download; an update only refreshes their timestamps.

## 10. DNS leak-prevention strategy

“No DNS leak” only makes sense after you define the threat model. Two common layers:

1. Do not send plaintext UDP/TCP 53: DoH / DoT / DoQ can do that.
2. The DNS upstream itself must also go through the proxy: using DoH alone does not mean it is proxied. The example’s `respect-rules: false` is deliberate so DNS connections go direct and bootstrap stays simple.

### Send ordinary DNS upstreams through the proxy

First confirm the proxy hostname can be resolved directly by `proxy-server-nameserver`, then consider:

```yaml
dns:
  respect-rules: true
  proxy-server-nameserver:
    - https://1.1.1.1/dns-query
  nameserver:
    - https://1.1.1.1/dns-query
```

When `respect-rules: true`, `proxy-server-nameserver` cannot be empty. Ordinary `nameserver` / `fallback` / policy upstream connections follow `rules`; the proxy node itself is bootstrapped by the dedicated resolver so you do not recurse.

You can also pin a single DNS upstream to a group:

```yaml
dns:
  nameserver:
    - "https://1.1.1.1/dns-query#PROXY"
```

Validate the simple `respect-rules: false` mode first, then switch step by step. If “resolving the proxy requires the proxy,” you have a deadlock.

### Applications that bypass system DNS

Browsers, Android Private DNS, VPN clients, and some apps use their own DoH / DoT. Options:

- Turn off the app’s custom DNS and use system / Aster DNS.
- Or manage that DoH host with explicit rules.
- On a network you control, block unauthorized UDP/TCP 53 and 853. DoH uses 443, so port matching alone cannot identify it completely.
- Do not assume setting `dns.listen` intercepts every program.

### SOCKS5 local resolution

This form may resolve the hostname on the host that runs `curl` first:

```sh
curl --proxy socks5://127.0.0.1:7890 https://example.org/
```

Use remote hostname resolution instead:

```sh
curl --proxy socks5h://127.0.0.1:7890 https://example.org/
```

SOCKS clients name this option differently. Confirm whether they send a domain or an IP.

## 11. Whole-machine takeover and TUN DNS hijack

Setting only an HTTP/SOCKS proxy does not take over programs that ignore the system proxy. For whole-machine takeover, add at the top level:

```yaml
tun:
  enable: true
  stack: mixed
  auto-route: true
  auto-detect-interface: true
  strict-route: true
  dns-hijack:
    - any:53
```

Before enabling it, understand:

- Linux usually needs `CAP_NET_ADMIN` / root and `/dev/net/tun`.
- Windows needs matching privileges and a usable TUN driver.
- `strict-route` reduces bypass routes, but can also block LAN or another VPN. Keep an out-of-band management path first.
- DNS hijack only handles matching DNS traffic. An app’s own DoH is still ordinary HTTPS.
- Aster’s own upstreams must not be hijacked back into itself, or you get a loop.
- With fake-IP, TUN must stay up so later connections to `198.18.0.0/16` are intercepted.
- Docker Desktop, WSL, routers, and native Linux do not route the same way.

The first time you enable TUN, keep `allow-lan: false`, do not add a complex firewall rewrite at the same time, and verify DNS, TCP, UDP, and LAN step by step.

## 12. Hunt leaks with packet capture

On Linux you can watch classic DNS / DoT during a test:

```sh
sudo tcpdump -ni any '(udp port 53 or tcp port 53 or tcp port 853)'
```

Generate test traffic in another terminal, then check:

- Whether an application is querying the router, ISP, or an unknown resolver directly.
- Whether the packet’s source process / network namespace is actually not Aster.
- Whether `proxy-server-nameserver` bootstrap queries look as expected.
- Whether IPv6 shows DNS or direct traffic that was never designed in.

DoH uses TCP/UDP 443, so port alone cannot tell you. Cross-check destination IPs, Aster debug logs, browser policy, and firewall logs. On Windows use Wireshark / pktmon; on macOS use `tcpdump`.

## Troubleshooting

### DNS listener failed to start

- `127.0.0.1:1053` is already taken by another Aster instance or DNS program.
- You changed it to 53 and lack permission to bind a low port.
- Docker did not publish both UDP and TCP.
- `dns.enable` is not on in the YAML.

### Queries only time out

- The DoH / DoT upstream is blocked on the network.
- `default-nameserver` cannot bootstrap the upstream hostname.
- `respect-rules` sent DNS through a proxy that is not ready yet.
- The firewall allows only UDP but missed TCP DNS, or only TCP 443 while you are using DoQ.
- The `system` resolver points back at `127.0.0.1:1053` and loops.

### `nameserver-policy` has no effect

- The key’s domain matcher is misspelled.
- `rule-set:` points at a provider that does not exist.
- The provider is `behavior: ipcidr` and cannot match a DNS domain.
- Old DNS cache has not been flushed.
- The real query left through the browser’s built-in DoH and never entered Aster DNS.

### Domain rules miss; only IP / MATCH hits

- The SOCKS client resolved locally; switch to remote DNS mode.
- The application connected by IP, so Aster has no hostname metadata.
- fake-IP DNS and the traffic inbound are not the same Aster instance / profile.
- Rule order is wrong; something earlier already matched.
- A long-lived connection was not rebuilt.

### fake-IP queries look fine, but every connection fails

This usually means DNS already uses Aster, but later traffic does not enter Aster. Confirm:

- The application is set to an HTTP/SOCKS proxy; or
- TUN is enabled and the routes include `198.18.0.0/16`; and
- Another VPN / route table is not stealing the fake-IP traffic.

### HTTP works, but QUIC / games / voice still go direct

HTTP CONNECT mainly handles TCP. UDP apps need:

- SOCKS5 UDP / a client that supports UDP, or
- TUN / TProxy takeover; and
- A proxy outbound that itself supports UDP. This page’s AnyTLS profile uses `udp: true` and carries UDP over UoT, but the client-to-Aster inbound still has to take over that traffic correctly.

### IPv6 leaks or IPv6 sites behave differently

This page sets both top-level `ipv6: false` and `dns.ipv6: false`, which is a controllable IPv4 baseline. To enable IPv6:

1. Confirm the proxy and DNS upstreams support IPv6.
2. Add IPv6 rules and routes.
3. Handle `::/0` and AAAA in TUN / the firewall as well.
4. Test direct and proxied IPv6 egress separately.

Turning off Aster’s AAAA answers cannot stop an app that bypasses Aster entirely and connects over IPv6 itself. Real whole-machine leak prevention still depends on routes / firewall.

### Rules did not change after a provider update

- Check `/providers/rules` for `updatedAt`, `ruleCount`, and format.
- The HTTP response may not be the expected YAML/text/MRS.
- The YAML is missing `payload:` / `rules:`.
- `behavior` does not match the data.
- `path` is outside the allowed safe path.
- Flush DNS cache and make a new request for existing connections.

## Pre-production checklist

- Change `log-level: debug` back to `info`.
- Keep Controller, DNS, and the mixed proxy on loopback, or configure the required ACL / authentication.
- `PROXY` is not stuck on `DIRECT` because of `store-selected`.
- The fake-IP filter covers real LAN, mDNS, NTP, games, and captive-portal needs.
- Providers use HTTPS, a trusted source, a reasonable `size-limit`, and a recoverable cache.
- DNS bootstrap does not depend on a proxy that is not up yet.
- Packet capture shows no unexpected UDP/TCP 53, 853, IPv6, or bypass traffic.
- TUN environments have been tested separately for TCP, UDP, DNS, LAN, sleep/wake, and network switches.

## Next steps

- [First proxy profile](/en/tutorials/first-proxy)
- [Rules and DNS reference](/en/reference/routing-dns)
- [Full configuration fields](/en/reference/configuration)
- [Outbounds and groups](/en/reference/outbounds)
- [AnyTLS + REALITY](/en/reference/anytls-reality)
- [TUN and platform deployment notes](/en/reference/inbounds)
- [Troubleshooting](/en/troubleshooting)
