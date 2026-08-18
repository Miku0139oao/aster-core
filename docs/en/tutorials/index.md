# Tutorials

Each tutorial is split by use case and includes a complete configuration, verification steps, and common mistakes.

::: warning
Replace values such as `<SERVER_IP>`, `<PASSWORD>`, and `<PUBLIC_KEY>` yourself. Do not commit real passwords, private keys, or subscription URLs to Git.
:::

## Client

### [First proxy profile](/en/tutorials/first-proxy)

Start from downloading Aster Core, create a local HTTP / SOCKS5 proxy, add one AnyTLS + REALITY node, and confirm the connection with `curl`.

### [Routing and DNS](/en/tutorials/routing-dns)

Decide which traffic goes through the proxy and which goes direct, then set up fake-IP, DNS policy, and rule providers.

### [AnyTLS + REALITY](/en/tutorials/anytls-reality)

Write the node address, password, SNI, public key, and short ID from the server into an Aster profile. The server can be Xray, sing-box, SideraCore, or another compatible implementation.

### [Troubleshooting](/en/tutorials/troubleshooting)

Work through configuration errors, DNS, REALITY, ports, UDP, and system services by symptom.

## Aster server

The following pages apply only when you use Aster’s own listeners and management features:

- [Build an AnyTLS + REALITY server with Aster](/en/tutorials/anytls-reality#aster-server)
- [Manage VLESS / AnyTLS users and subscriptions](/en/tutorials/user-management)
- [Debian / Ubuntu VPS deployment](/en/tutorials/linux-production)
