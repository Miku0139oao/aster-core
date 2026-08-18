# Introduction

Aster Core is a Mihomo-based proxy core that mainly runs on the client. It accepts traffic from the computer or LAN, picks a node according to the rules, and sends the traffic to a remote server.

```text
Browser / app → Aster Core → remote node → Internet
```

The remote server does not have to run Aster. Common pairings include Xray, sing-box, SideraCore, and other implementations that speak the same protocol. As long as the protocol and parameters match, each side can use different software.

## Main capabilities

- Proxy protocols such as VLESS, VMess, Trojan, AnyTLS, and Hysteria
- HTTP, SOCKS5, transparent proxy, and TUN
- Rule-based routing, proxy groups, and node health checks
- DNS, fake-IP, DoH, DoT, and DoQ
- Clash-compatible API and dashboards
- Mihomo proxy providers and rule providers

Configuration is still Mihomo YAML, so an existing profile usually only needs a backup and an `aster-core -t` check. Aster does not read Xray or sing-box JSON directly. Take the node address, port, password, SNI, keys, and transport from the server and put them in the matching `proxies` entry.

## What Aster adds

On top of the Mihomo baseline, Aster adds AnyTLS + REALITY client support and share-link import. It also fixes configuration reloads, Hysteria UDP, VLESS packets, XHTTP, DNS responses, and core updates. High connection counts and large user-management paths get extra work as well.

The full comparison is in [How Aster differs from Mihomo](/en/reference/mihomo-differences).

## Server features

Aster includes VLESS and AnyTLS listeners and can manage users and subscriptions live. Those features fit testing, all-Aster environments, or deployments that want the Aster management API. Ordinary node servers can still use Xray, sing-box, or SideraCore.

Next, create a profile from [Quick start](/en/guide/getting-started) or [Your first proxy profile](/en/tutorials/first-proxy).
