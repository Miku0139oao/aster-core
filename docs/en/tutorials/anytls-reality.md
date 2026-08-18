# AnyTLS + REALITY

Aster can connect to AnyTLS + REALITY nodes. The server can be Xray, sing-box, SideraCore, Aster, or another implementation that accepts the same parameters.

```text
Browser or app
   │
   ▼
Aster Core
   │ AnyTLS + REALITY
   ▼
Remote proxy node
   │
   ▼
Internet
```

Client and server do not have to use the same configuration format. Aster uses Mihomo YAML; fill it from the connection parameters the server gives you.

## Client setup

Node values you need:

| Value you need | How to recognize it |
| --- | --- |
| Node address (`server`) | The IP or domain you actually connect to, not the camouflage site |
| Port (`port`) | Often `443`; use the number from the server |
| Password (`password`) | The AnyTLS password the server assigned to you |
| Camouflage site (`sni`) | A domain that looks like an ordinary website |
| Browser fingerprint (`client-fingerprint`) | Start with `chrome` if you are unsure |
| Public key (`public-key`) | The public key from the server; never put a private key here |
| short ID | A short alphanumeric value from the server; omit it only if none was given |

If the server gave you an `anytls://` share link, you can import it in an Aster-compatible app. For a manual profile, save the following as `config.yaml` and replace every `<...>`:

```yaml
mixed-port: 7890
allow-lan: false
mode: rule
log-level: info

external-controller: 127.0.0.1:9090
secret: "<CONTROLLER_SECRET>"

proxies:
  - name: anytls-reality
    type: anytls
    server: "<SERVER_HOST_OR_IP>"
    port: 443
    password: "<ANYTLS_PASSWORD>"
    sni: "<REALITY_SNI>"
    client-fingerprint: chrome
    reality-opts:
      public-key: "<REALITY_PUBLIC_KEY>"
      short-id: "<REALITY_SHORT_ID>"
    udp: true

proxy-groups:
  - name: PROXY
    type: select
    proxies:
      - anytls-reality
      - DIRECT

rules:
  - MATCH,PROXY
```

Validate and start:

```sh
aster-core -d ./aster-client -f ./aster-client/config.yaml -t
aster-core -d ./aster-client -f ./aster-client/config.yaml
```

In another terminal, test the connection:

```sh
curl --proxy http://127.0.0.1:7890 https://www.cloudflare.com/cdn-cgi/trace
```

A normal response means the basic connection works.

Share-link format:

```text
anytls://<password>@proxy.example.com:443?security=reality&sni=www.microsoft.com&fp=chrome&pbk=<public-key>&sid=<short-id>#AnyTLS-REALITY
```

`pbk` is the public key, `sid` is the short ID, and `fp` is the browser fingerprint. The full install and local-proxy walkthrough is in [Your first proxy profile](/en/tutorials/first-proxy).

## Aster server

The rest of this page shows Aster’s built-in AnyTLS listener on a VPS. If the server already uses Xray, sing-box, SideraCore, or another implementation, jump to that project’s documentation instead.

```text
Aster client
   │ AnyTLS over TCP + REALITY
   ▼
Aster listener on the VPS
   ├─ TCP destinations
   └─ UDP over TCP (UoT) destinations
```

### What you will deploy

- One DNS-only node domain, for example `edge.example.com`.
- An AnyTLS listener on the VPS that listens on TCP `443`.
- One REALITY private key that stays only on the VPS.
- A REALITY public key, short ID, and AnyTLS password for the client.
- An Aster client profile that can test TCP and UDP/UoT.
- One `anytls://` REALITY share link that Aster can import.

### What to prepare

- A Linux VPS with a public IPv4, plus root or sudo.
- A domain you can edit DNS for.
- Compatible Aster Core versions on both the VPS and the client; record them with `aster-core -v` first.
- VPS system time already synced by NTP.
- A TLS fallback site the VPS can reach. This page uses `www.microsoft.com:443` as an example; confirm availability and suitability before production use.

These placeholders must be replaced:

| Placeholder | Content |
| --- | --- |
| `<NODE_DOMAIN>` | The domain that points at the VPS, for example `edge.example.com` |
| `<SERVER_PRIVATE_KEY>` | The REALITY private key used on the VPS |
| `<SERVER_PUBLIC_KEY>` | The matching REALITY public key used by the client |
| `<SHORT_ID>` | A hex short ID of at most 8 bytes |
| `<ANYTLS_PASSWORD>` | The AnyTLS user password |
| `<CONTROLLER_SECRET>` | The ordinary Controller secret; it is not the AnyTLS password |

::: danger Do not publish these values
`<SERVER_PRIVATE_KEY>`, `<ANYTLS_PASSWORD>`, and `<CONTROLLER_SECRET>` are sensitive. Do not paste them into issues, chat, monitoring labels, or public Git. The public key can go to clients, but you still should not publish it together with a complete node profile.
:::

## 1. Create DNS

Create only an A record first:

```text
Type: A
Name: edge
Value: <VPS_PUBLIC_IPV4>
TTL: 300
Proxy/CDN: DNS only
```

If you use Cloudflare, the cloud must be grey **DNS only**. Ordinary Cloudflare HTTP proxy does not forward AnyTLS. Unless you have a product that supports arbitrary TCP, an orange cloud sends the client to Cloudflare instead of Aster.

After DNS has propagated, look it up from both the client and the VPS:

```sh
dig +short A <NODE_DOMAIN>
```

You should see only the VPS public IPv4. Do not rush to add AAAA at the start. This page’s server listen address is `0.0.0.0`. If DNS publishes IPv6 first, IPv6-preferring clients may fail. To enable IPv6, bind the listener to IPv6 for real, open the firewall, verify from the outside, and only then add AAAA.

## 2. Check the port, firewall, and clock

### Confirm 443 is free

```sh
sudo ss -ltnp | grep -E '(^|[[:space:]])[^[:space:]]*:443[[:space:]]' || true
```

No output means there is currently no TCP listener. If Caddy, Nginx, HAProxy, or another proxy already owns `443`, design a TCP/SNI split first, or change every `443` on this page to another open port such as `8443`. Do not stop an unknown production service.

### Open the firewall

Both the cloud security group and the VPS host firewall must allow inbound TCP `443`. Keep existing SSH rules, and only use the firewall tool that is actually in charge.

UFW:

```sh
sudo ufw allow 443/tcp
sudo ufw status verbose
```

firewalld:

```sh
sudo firewall-cmd --permanent --add-service=https
sudo firewall-cmd --reload
sudo firewall-cmd --list-all
```

The AnyTLS listener itself listens on **TCP** only. Client UDP is encapsulated as UoT on that TCP connection, so you do not need inbound UDP `443` for AnyTLS. The VPS still needs whatever outbound UDP the destination requires.

### Confirm NTP

```sh
timedatectl status
```

You should at least see:

```text
System clock synchronized: yes
```

If it is not synced, fix chrony, systemd-timesyncd, or the provider time service first. REALITY verification depends on a correct clock.

## 3. Verify the fallback target

From the VPS, test the target’s DNS, TCP, and TLS:

```sh
getent ahosts www.microsoft.com
timeout 10 openssl s_client \
  -connect www.microsoft.com:443 \
  -servername www.microsoft.com \
  -brief </dev/null
```

You should be able to establish TLS, and the certificate name should match `www.microsoft.com`. If it times out, is blocked by the VPS network, or TLS is unstable, pick a TLS site you know works and change all of the following together:

- Server `reality-config.dest`
- Server `reality-config.server-names`
- Client `sni`
- Share-link `sni`

`dest` is the fallback destination when REALITY verification fails. It is not the VPS address the Aster client connects to.

## 4. Generate keys, a short ID, and passwords

In a root shell on the VPS:

```sh
umask 077
/usr/bin/aster-core generate reality-keypair
openssl rand -hex 8
openssl rand -hex 32
openssl rand -hex 32
```

The first command prints:

```text
PrivateKey: <SERVER_PRIVATE_KEY>
PublicKey: <SERVER_PUBLIC_KEY>
```

The next three lines are, in order:

1. `<SHORT_ID>`: `openssl rand -hex 8` produces 16 hex characters, which is 8 bytes.
2. `<ANYTLS_PASSWORD>`: a 32-byte random password.
3. `<CONTROLLER_SECRET>`: a separate 32-byte Controller secret.

Aster’s server short-ID field is a list named `short-id`; each value is at most 8 bytes after hex decode. The client fills a single `reality-opts.short-id`. A short ID must be valid even-length hex. Do not put a UUID or arbitrary text there.

::: warning Key direction
The private key goes only in the VPS `reality-config.private-key`. The client only gets the public key, in `reality-opts.public-key`. Swapping them makes validation or the handshake fail.
:::

## 5. Write the complete server YAML

Create a root-only configuration directory:

```sh
sudo install -d -m 700 /etc/mihomo
sudoedit /etc/mihomo/config.yaml
```

Fill in the complete profile and replace the placeholders:

```yaml
mode: rule
log-level: info
ipv6: true

# The Controller is only for local checks. This is the ordinary Controller secret,
# not the AnyTLS password and not the Aster Admin secret.
external-controller: 127.0.0.1:9090
secret: "<CONTROLLER_SECRET>"

listeners:
  - name: edge-anytls
    type: anytls
    listen: 0.0.0.0
    port: 443
    users:
      alice: "<ANYTLS_PASSWORD>"
    reality-config:
      dest: www.microsoft.com:443
      private-key: "<SERVER_PRIVATE_KEY>"
      short-id:
        - "<SHORT_ID>"
      server-names:
        - www.microsoft.com

rules:
  - MATCH,DIRECT
```

Then tighten permissions:

```sh
sudo chmod 600 /etc/mihomo/config.yaml
sudo chown root:root /etc/mihomo/config.yaml
```

The important mapping is:

| Server field | Client counterpart |
| --- | --- |
| DNS `<NODE_DOMAIN>` + `port` | `server` + `port` |
| Value of `users.alice` | `password` |
| Public half of `reality-config.private-key` | `reality-opts.public-key` |
| One value from `reality-config.short-id` | `reality-opts.short-id` |
| One value from `reality-config.server-names` | `sni` |

The same AnyTLS listener can use only one security mode. When you use `reality-config`, do not also add certificate `certificate`/`private-key`, `shadow-tls`, `res-tls`, or `jls-config`.

## 6. Validate and start

Parse the configuration first:

```sh
sudo /usr/bin/aster-core \
  -d /etc/mihomo \
  -f /etc/mihomo/config.yaml \
  -t
```

On success you should see something like:

```text
configuration file ... test is successful
```

If you installed the project systemd package:

```sh
sudo systemctl enable --now aster-core
sudo systemctl status --no-pager aster-core
sudo journalctl -u aster-core -n 50 --no-pager
```

The log should include `AnyTLS[edge-anytls] proxy listening at`. Then check the actual socket:

```sh
sudo ss -ltnp | grep ':443'
```

From another external host, test TCP reachability only:

```sh
nc -vz <NODE_DOMAIN> 443
```

`succeeded` only means DNS, the firewall, and the TCP listener are fine. It does not prove the REALITY key, SNI, short ID, or password. Full verification has to come from an Aster client.

If this is not a systemd install, start in the foreground first:

```sh
sudo /usr/local/bin/aster-core \
  -d /etc/mihomo \
  -f /etc/mihomo/config.yaml
```

## 7. Write the complete client YAML

On the client, create a dedicated directory such as `./aster-client` and save the following as `config.yaml`:

```yaml
mixed-port: 7890
allow-lan: false
mode: rule
log-level: info
ipv6: false

external-controller: 127.0.0.1:9091
secret: "<LOCAL_CONTROLLER_SECRET>"

dns:
  enable: true
  listen: 127.0.0.1:1053
  ipv6: false
  enhanced-mode: redir-host
  default-nameserver:
    - 1.1.1.1
  proxy-server-nameserver:
    - 1.1.1.1
  nameserver:
    # Send UDP DNS explicitly through the AnyTLS proxy so the next section can verify UoT.
    - "udp://1.1.1.1#edge-anytls-reality"

proxies:
  - name: edge-anytls-reality
    type: anytls
    server: <NODE_DOMAIN>
    port: 443
    password: "<ANYTLS_PASSWORD>"
    sni: www.microsoft.com
    client-fingerprint: chrome
    reality-opts:
      public-key: "<SERVER_PUBLIC_KEY>"
      short-id: "<SHORT_ID>"
    udp: true

rules:
  - MATCH,edge-anytls-reality
```

Generate a separate `<LOCAL_CONTROLLER_SECRET>`; it only protects the client’s local Controller:

```sh
openssl rand -hex 32
```

The client `server` is the VPS node domain. `sni` is the fallback / camouflage name. Do not swap them. A REALITY client must set `client-fingerprint`; start with `chrome`.

These fields do not belong in this Aster outbound YAML:

```text
security: reality
realitySettings:
serverName:
shortIds:
privateKey:
```

`security=reality` appears only in the share-URI query. Whether YAML enables REALITY is decided by `reality-opts.public-key`.

## 8. Start the client

Validate first:

```sh
./aster-core \
  -d ./aster-client \
  -f ./aster-client/config.yaml \
  -t
```

Then start in the foreground. For the first test you can temporarily set `log-level` to `debug`:

```sh
./aster-core \
  -d ./aster-client \
  -f ./aster-client/config.yaml
```

You should have three local sockets:

- `127.0.0.1:7890`: HTTP/SOCKS mixed proxy.
- `127.0.0.1:1053`: DNS listener for the test.
- `127.0.0.1:9091`: client Controller.

Another terminal can confirm:

```sh
curl \
  -H "Authorization: Bearer <LOCAL_CONTROLLER_SECRET>" \
  http://127.0.0.1:9091/version
```

## 9. Verify TCP

Look up egress through the HTTP proxy first:

```sh
curl --fail --show-error \
  --proxy http://127.0.0.1:7890 \
  https://api.ipify.org
echo
```

Then try SOCKS:

```sh
curl --fail --show-error \
  --socks5-hostname 127.0.0.1:7890 \
  https://example.com/ \
  -o /dev/null \
  -w 'HTTP %{http_code}\n'
```

Expected:

- The first command shows the VPS egress IP, not the client’s original IP.
- The second command gets a normal HTTP status.
- The client log shows the rule selected `edge-anytls-reality`.
- The VPS log has no continuous REALITY handshake or password errors.

After you change a password or key, fully restart the client, or at least close the related connections, so an old session does not hide the problem.

## 10. Verify UDP over TCP (UoT)

This page’s client DNS already pins `1.1.1.1:53/udp` to the AnyTLS proxy. Open a short observation window on the VPS:

```sh
sudo tcpdump -ni any 'udp and host 1.1.1.1 and port 53'
```

Then on the client:

```sh
dig @127.0.0.1 -p 1053 example.com A +short
```

Expected:

1. `dig` returns one or more A records.
2. The client DNS log uses `edge-anytls-reality`.
3. The VPS `tcpdump` sees UDP from the VPS to `1.1.1.1:53`.
4. The VPS does not need inbound UDP `443`; client-to-VPS is still TCP `443`.

That result also verifies:

- Client `udp: true`.
- AnyTLS outbound UoT.
- The server can send UDP outbound.
- Return packets can come back to the client through AnyTLS.

If TCP works but this test fails, check `udp: true`, the `#edge-anytls-reality` suffix on `nameserver`, VPS outbound firewall, and the client log. Do not infer UDP from a successful ordinary `curl`.

## 11. Build and check the share link

With the hex password generated on this page, you do not need extra URL-encoding. The complete Aster AnyTLS + REALITY URI is:

```text
anytls://<ANYTLS_PASSWORD>@<NODE_DOMAIN>:443?security=reality&type=tcp&sni=www.microsoft.com&fp=chrome&pbk=<SERVER_PUBLIC_KEY>&sid=<SHORT_ID>#Aster-AnyTLS-REALITY
```

Query-to-YAML mapping:

| URI query | Aster YAML |
| --- | --- |
| `security=reality` | Import `reality-opts` |
| `type=tcp` | AnyTLS transport |
| `sni` | `sni` |
| `fp` | `client-fingerprint` |
| `pbk` | `reality-opts.public-key` |
| `sid` | `reality-opts.short-id` |

If the password is not the pure hex from this page and contains `@`, `:`, `/`, `?`, `#`, or `%`, URL-encode the userinfo first. Do not concatenate the raw string.

After import, check that the generated outbound at least has:

```yaml
type: anytls
server: <NODE_DOMAIN>
port: 443
password: "<ANYTLS_PASSWORD>"
sni: www.microsoft.com
client-fingerprint: chrome
reality-opts:
  public-key: "<SERVER_PUBLIC_KEY>"
  short-id: "<SHORT_ID>"
udp: true
```

## 12. Production safety checks

- The REALITY private key stays on the server forever. Clients, subscriptions, and share links use only the public key.
- Give each user a different AnyTLS password. For live add / disable / rotate, use the [managed-user tutorial](/en/tutorials/user-management).
- Keep the Controller on `127.0.0.1`. Do not change a plaintext Controller to `0.0.0.0:9090` just to reach a remote dashboard.
- Do not add `skip-cert-verify: true` to hide a wrong public key, SNI, or short ID.
- Do not stack REALITY with certificate TLS, ShadowTLS, ResTLS, or JLS on the same listener/outbound.
- Use mode `0600` for `config.yaml`. Encrypt backups and restrict access.
- Recheck NTP, that DNS still points at the correct VPS, and that the fallback is still reachable from the VPS.
- When you change the private key, SNI, or short ID, every client and share link must be updated together.

## 13. Troubleshooting order

| Symptom | Check first |
| --- | --- |
| DNS does not resolve | A record, TTL, nameserver delegation |
| TCP timeout | Cloud security group, host firewall, routing, wrong IP |
| TCP refused | Aster is not running, wrong port, listener not bound |
| You reach Cloudflare/CDN | The DNS record must be DNS only |
| `-t` says invalid private key | Private key swapped, Base64URL truncated, or extra whitespace |
| `invalid short ID` | Not even-length hex, or more than 8 bytes after decode |
| REALITY handshake fails | Public/private key pair, SNI not in `server-names`, short ID mismatch, clock skew |
| AnyTLS authentication fails | `password` does not match the server `users` value |
| TCP works, UDP fails | Client `udp: true`, UoT test path, VPS outbound UDP |
| 443 cannot bind after restart | Caddy/Nginx/another process owns it; check the owner with `ss -ltnp` |
| Fallback looks wrong | VPS DNS/TCP/TLS to `dest`, and whether `dest` and SNI are a reasonable pair |

Test one layer at a time. Do not change key, password, SNI, short ID, DNS, and port in one step. A useful order is:

1. DNS resolves to the correct VPS.
2. External TCP can reach the listener.
3. Server `-t` and the socket look normal.
4. Public/private keys are a pair.
5. `sni`, `server-names`, and `short-id` match character for character.
6. The AnyTLS password matches.
7. TCP proxy.
8. UDP/UoT.

When you share diagnostics, give only the version, a redacted configuration, the error text, and the test layer. Remove private keys, passwords, and complete URIs.

## 14. Roll the deployment back

If you need to undo the change after going live:

1. Keep the current log and a redacted copy of the configuration.
2. Stop Aster, or restore a previous `config.yaml` that already passed `-t`.
3. Restart the original service and confirm the original socket / site is back.
4. Remove the new TCP firewall rule only after no client still uses it.
5. Finally delete or revert the `<NODE_DOMAIN>` DNS record.

With systemd:

```sh
sudo systemctl stop aster-core
sudo cp /etc/mihomo/config.yaml.before-anytls /etc/mihomo/config.yaml
sudo chmod 600 /etc/mihomo/config.yaml
sudo /usr/bin/aster-core -d /etc/mihomo -t
sudo systemctl start aster-core
sudo systemctl status --no-pager aster-core
```

Run that `cp` only if you actually created `config.yaml.before-anytls` beforehand. Do not overwrite a production profile with an empty file or an uncertain source.

## Next steps

- [Manage VLESS/AnyTLS users and subscriptions live](/en/tutorials/user-management)
- [AnyTLS + REALITY field reference](/en/reference/anytls-reality)
- [How Aster differs from Mihomo](/en/reference/mihomo-differences)
- [Linux and systemd](/en/deployment/linux)
- [Troubleshooting](/en/troubleshooting)
