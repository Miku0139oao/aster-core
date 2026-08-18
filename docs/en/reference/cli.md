# CLI and environment variables

## Main-program flags

| Flag | Environment variable | Description |
| --- | --- | --- |
| `-d <dir>` | `CLASH_HOME_DIR` | Configuration and data home directory |
| `-f <file>` | `CLASH_CONFIG_FILE` | Configuration file; `-` means stdin |
| `--config <base64>` | `CLASH_CONFIG_STRING` | Base64-encoded YAML |
| `--age-secret-key <key>` | `CLASH_AGE_SECRET_KEY` | Age decryption secret key |
| `--ext-ui <dir>` | `CLASH_OVERRIDE_EXTERNAL_UI_DIR` | Override the external UI directory |
| `--ext-ctl <addr>` | `CLASH_OVERRIDE_EXTERNAL_CONTROLLER` | Override the HTTP Controller address |
| `--ext-ctl-tls <addr>` | `CLASH_OVERRIDE_EXTERNAL_CONTROLLER_TLS` | Override the HTTPS Controller address |
| `--ext-ctl-unix <path>` | `CLASH_OVERRIDE_EXTERNAL_CONTROLLER_UNIX` | Override the Unix socket |
| `--ext-ctl-pipe <path>` | `CLASH_OVERRIDE_EXTERNAL_CONTROLLER_PIPE` | Override the Windows named pipe |
| `--ext-ctl-routing-mark <n>` | `CLASH_OVERRIDE_EXTERNAL_CONTROLLER_ROUTING_MARK` | Linux Controller socket mark |
| `--secret <secret>` | `CLASH_OVERRIDE_SECRET` | Override the ordinary Controller secret |
| `--post-up <command>` | `CLASH_POST_UP` | Run a shell command after the runtime starts |
| `--post-down <command>` | `CLASH_POST_DOWN` | Run a shell command during shutdown |
| `-m` | — | Enable geodata mode |
| `-t` | — | Validate the configuration and exit |
| `-v` | — | Print version, platform, Go, build time, and tags |

::: danger Shell commands
`--post-up` and `--post-down` run through the system shell. Do not let an API, user input, or an untrusted configuration source control these values.
:::

## Configuration source examples

```sh
# Default <home>/config.yaml
aster-core

# Explicit home and file
aster-core -d /etc/mihomo -f /etc/mihomo/config.yaml

# stdin
aster-core -d /etc/mihomo -f -

# Base64
aster-core --config "$(base64 -w0 config.yaml)"
```

## Generate credentials and keys

### UUID

```sh
aster-core generate uuid
```

### REALITY X25519

```sh
aster-core generate reality-keypair
```

Prints `PrivateKey` and `PublicKey`. The server uses the private key; the client uses the public key.

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

You can pass an existing seed/private key. The command also prints suggested server and client settings.

### Sudoku

```sh
aster-core generate sudoku-keypair
```

## Age configuration encryption

```sh
# X25519 identity
aster-core age keygen

# ML-KEM-768 + X25519 hybrid identity
aster-core age keygen-pq

# Convert a secret key to a public recipient
aster-core age convert <secret-key>

# Encrypt and decrypt; source/target may be - for stdio
aster-core age encrypt <public-key> <source> <target>
aster-core age decrypt <secret-key> <source> <target>
```

## Rule-set conversion

```sh
aster-core convert-ruleset <behavior> <format> <source> <target>
```

This tool converts rule-provider data among supported formats such as text/YAML/MRS. `behavior` must match a provider behavior such as `domain`, `ipcidr`, or `classical`.

## Advanced environment variables

| Variable | Purpose |
| --- | --- |
| `XDG_CONFIG_HOME` | Configuration root used when the default Mihomo directory does not exist |
| `SAFE_PATHS` | Extra allowed file roots, using the OS path-list separator |
| `SKIP_SAFE_PATH_CHECK` | Disable safe-path checks entirely |
| `DISABLE_EMBED_CA` | Do not use the embedded CA bundle |
| `DISABLE_SYSTEM_CA` | Do not load system CAs |
| `DISABLE_SYSTEM_HOSTS` | Do not query system hosts |
| `DISABLE_LOOPBACK_DETECTOR` | Disable the loopback connection detector |
| `FORCE_ANET` | Force the anet path |
| `HOST_PROC` | Linux procfs root, default `/proc` |
| `SKIP_SYSTEM_IPV6_CHECK` | Skip system IPv6 capability detection |
| `DISABLE_NFTABLES` | Do not use nftables for TUN auto-redirect |
| `DISABLE_OVERRIDE_ANDROID_VPN` | Disable Android VPN interface override |
| `LISTEN_NAMEDPIPE_SDDL` | Windows named-pipe SDDL |
| `QUIC_GO_DISABLE_GSO` | Disable quic-go GSO |
| `QUIC_GO_DISABLE_ECN` | Disable quic-go ECN |

## Signals and shutdown

| Signal | Behavior |
| --- | --- |
| `SIGHUP` | Reapply configuration; file mode rereads the file |
| `SIGINT` | Graceful shutdown |
| `SIGTERM` | Graceful shutdown |

A graceful shutdown runs runtime shutdown, closes listeners, cleans automatic iptables, saves fake-IP, and flushes Aster traffic.
