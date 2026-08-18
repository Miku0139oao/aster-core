# AnyTLS + REALITY

Aster is usually an AnyTLS + REALITY client connecting to Xray, sing-box, SideraCore, Aster, or another compatible server. The full deployment walkthrough is in the [tutorial](/en/tutorials/anytls-reality).

```text
Your app → Aster Core → AnyTLS + REALITY node → Internet
```

The server needs to provide the node address, port, password, SNI, public key, and short ID.

## Client configuration

```yaml
proxies:
  - name: anytls-reality
    type: anytls
    server: proxy.example.com
    port: 443
    password: "replace-with-your-password"
    sni: www.microsoft.com
    client-fingerprint: chrome
    reality-opts:
      public-key: <server-public-key>
      short-id: 0123456789abcdef
    udp: true
```

### Fields

| Field | What to put there |
| --- | --- |
| `name` | A node name you can recognize |
| `server` | The node IP or domain you actually connect to, not the camouflage site |
| `port` | The port from the server |
| `password` | The AnyTLS password from the server |
| `sni` | The camouflage hostname from the server |
| `client-fingerprint` | The browser type to imitate; use `chrome` if unsure |
| `public-key` | The REALITY public key from the server |
| `short-id` | A short identifier from the server; omit it only if none was given |
| `udp` | Set to `true` so programs that need UDP can use this node |

::: danger Key direction
The client only needs the public key. The private key must stay on the server.
:::

## Share links

Aster supports this format:

```text
anytls://<password>@proxy.example.com:443?security=reality&sni=www.microsoft.com&fp=chrome&pbk=<public-key>&sid=<short-id>#AnyTLS-REALITY
```

| Share-link content | Meaning |
| --- | --- |
| `<password>` | AnyTLS password |
| `proxy.example.com:443` | Node address and port |
| `sni` | Camouflage site |
| `fp` | Browser fingerprint |
| `pbk` | REALITY public key |
| `sid` | short ID |
| `#AnyTLS-REALITY` | Display name; you can change it |

If the userinfo is written as `username:password@host`, Aster uses the part after the colon as the password.

## Common mistakes

| What you see | Check first |
| --- | --- |
| Connection refused immediately | `server`, `port`, firewall, and whether the server is running |
| Wait, then timeout | Whether DNS is correct and the server is reachable from the outside |
| The port is reachable, but the proxy still fails | `password`, `sni`, `public-key`, `short-id` |
| Web pages work, but games or voice fail | Whether `udp: true` is set and the server supports it |
| Importing a share link does not work | Whether `pbk`, `sid`, `sni`, and `fp` are complete |

Do not use `skip-cert-verify` to hide a wrong public key, SNI, or short ID.

## Aster server

Aster can also accept AnyTLS + REALITY connections directly.

Generate a key first:

```sh
aster-core generate reality-keypair
```

You get:

```text
PrivateKey: <server-private-key>
PublicKey: <server-public-key>
```

- PrivateKey stays on the server only.
- PublicKey is given to clients.

Server configuration:

```yaml
listeners:
  - name: edge-anytls
    type: anytls
    listen: 0.0.0.0
    port: 443
    users:
      alice: "replace-with-a-long-random-password"
    reality-config:
      dest: www.microsoft.com:443
      private-key: <server-private-key>
      short-id:
        - 0123456789abcdef
      server-names:
        - www.microsoft.com
```

| Server field | Meaning |
| --- | --- |
| `listen` | `0.0.0.0` accepts connections on all IPv4 interfaces |
| `port` | Public TCP port |
| `users` | Usernames and passwords |
| `dest` | Ordinary HTTPS site used when verification fails |
| `private-key` | Server private key; do not leak it |
| `short-id` | Short IDs the server allows |
| `server-names` | SNIs the client may use |

The same AnyTLS service cannot use REALITY, ordinary certificate TLS, ShadowTLS, ResTLS, or JLS at the same time. Pick one.

## Manage users live

After you add the AnyTLS service to `managed-listeners`, you can add, change, disable, or delete passwords without restarting the whole service.

```yaml
external-controller: 127.0.0.1:9090

aster:
  secret: "replace-with-at-least-32-random-bytes"
  state-file: ./aster-state.json
  public-base-url: https://proxy.example.com
  managed-listeners:
    - edge-anytls
```

The full operations are in the [user-management tutorial](/en/tutorials/user-management).

## Related pages

- [AnyTLS + REALITY step-by-step tutorial](/en/tutorials/anytls-reality)
- [First proxy profile](/en/tutorials/first-proxy)
- [Remote node configuration](/en/reference/outbounds)
- [Troubleshooting handbook](/en/tutorials/troubleshooting)
