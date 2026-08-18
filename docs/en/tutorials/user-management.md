# Manage VLESS and AnyTLS users and subscriptions live

::: warning Optional advanced server feature
This page applies only when you explicitly choose Aster to provide VLESS / AnyTLS listeners. Aster’s main job is the client. Ordinary servers can use Xray, sing-box, or SideraCore and do not need this management plane.
:::

This walkthrough puts two Aster listeners under the management plane, then uses complete `curl` flows to:

- Enable `aster.managed-listeners`, the state store, and the Admin API.
- Create, list, read, update, disable, re-enable, and delete AnyTLS/VLESS users.
- Use each listener’s revision correctly.
- Reproduce and handle HTTP `409 Conflict`.
- Fetch and rotate a single-user subscription URL.
- Back up, restore, and protect credentials/tokens in state.

User changes apply live to the running VLESS/AnyTLS listener. You do not recreate the listener. REALITY keys, port, SNI, transport, and other listener settings still come from YAML and a configuration reload. They are not part of user CRUD.

## Architecture and security boundary

This page uses:

```text
Administrator
  └─ SSH / localhost ──> 127.0.0.1:9090/api/admin/*
                              │ Aster Bearer token
                              ▼
                         Aster manager
                           ├─ live listeners
                           └─ state/aster-state.json + .bak

Ordinary user
  └─ HTTPS ──> subscription.example.com/sub/aster/{token}
                              │
                              └─ one Base64 proxy link
```

The Admin API stays on loopback. The public reverse proxy forwards only `/sub/aster/*`. Do not expose `/api/admin/*`, the ordinary Clash Controller, or the state file to the Internet.

## Prerequisites

- Aster Core is installed on the VPS and can run with `-d /etc/mihomo`.
- You have two REALITY key pairs and short IDs; see [Deploy AnyTLS + REALITY from scratch](/en/tutorials/anytls-reality).
- `subscription.example.com` points at the VPS and has a working HTTPS reverse proxy.
- The VPS has `curl` and `jq`.
- The AnyTLS/VLESS public ports are open in both the cloud and host firewalls.

Listener ports on this page:

| Listener | Port | Credential |
| --- | --- | --- |
| `edge-anytls` | TCP 8443 | password |
| `edge-vless` | TCP 8444 | UUID, optional `xtls-rprx-vision` flow |
| Loopback Controller | TCP 9090 | `aster.secret` protects the Admin API |
| Public subscription HTTPS | TCP 443 | Each user’s subscription token |

`8443/8444` leave `443` free for Caddy/Nginx to serve subscription HTTPS. If you change ports, update DNS, firewall, YAML, and tests together.

### Management scope

- Only named VLESS and AnyTLS entries under `listeners` can join `managed-listeners`.
- The Admin API live-updates user credentials, flow, enabled, traffic, and subscription tokens. REALITY/TLS/transport/port still come from YAML.
- Quota and expiration capabilities are currently `false`. You cannot implement traffic quotas or expiry-disable with Aster Core alone.
- Revision belongs to the whole listener, not one user. Any user mutation on that listener advances the revision.
- Subscriptions only emit eligible VLESS/AnyTLS settings. ShadowTLS, ResTLS, JLS, and advanced XHTTP do not produce managed share links.
- Other listeners/protocols inherited from Mihomo are still configured the original way. This Admin API cannot manage them.

## 1. Generate independent secrets and listener material

The Controller secret, Aster Admin secret, user credentials, and subscription tokens are four different secrets. Do not reuse them.

On the VPS, generate two management secrets:

```sh
umask 077
openssl rand -hex 32
openssl rand -hex 32
```

In order:

- `<CONTROLLER_SECRET>`: ordinary Controller API such as `/version` and `/configs`.
- `<ASTER_SECRET>`: `/api/admin/*` only, at least 32 bytes.

Generate a REALITY key pair and short ID for each listener:

```sh
/usr/bin/aster-core generate reality-keypair
openssl rand -hex 8

/usr/bin/aster-core generate reality-keypair
openssl rand -hex 8
```

Record them as:

- `<ANYTLS_REALITY_PRIVATE_KEY>` / `<ANYTLS_REALITY_PUBLIC_KEY>` / `<ANYTLS_SHORT_ID>`
- `<VLESS_REALITY_PRIVATE_KEY>` / `<VLESS_REALITY_PUBLIC_KEY>` / `<VLESS_SHORT_ID>`

Private keys appear only in server YAML. The Admin API manages AnyTLS passwords and VLESS UUIDs. It does not return or change REALITY private keys.

## 2. Write the complete Aster server YAML

Create a protected configuration directory:

```sh
sudo install -d -m 700 /etc/mihomo
sudoedit /etc/mihomo/config.yaml
```

Fill in the following and replace every placeholder:

```yaml
mode: rule
log-level: info
ipv6: true

# A plaintext Controller only mounts Aster Admin routes when bound to loopback.
external-controller: 127.0.0.1:9090
secret: "<CONTROLLER_SECRET>"

listeners:
  - name: edge-anytls
    type: anytls
    listen: 0.0.0.0
    port: 8443
    users: {}
    reality-config:
      dest: www.microsoft.com:443
      private-key: "<ANYTLS_REALITY_PRIVATE_KEY>"
      short-id:
        - "<ANYTLS_SHORT_ID>"
      server-names:
        - www.microsoft.com

  - name: edge-vless
    type: vless
    listen: 0.0.0.0
    port: 8444
    users: []
    reality-config:
      dest: www.microsoft.com:443
      private-key: "<VLESS_REALITY_PRIVATE_KEY>"
      short-id:
        - "<VLESS_SHORT_ID>"
      server-names:
        - www.microsoft.com

aster:
  secret: "<ASTER_SECRET>"
  public-base-url: "https://subscription.example.com"
  store: "state/aster-state.json"
  managed-listeners:
    - edge-anytls
    - edge-vless

rules:
  - MATCH,DIRECT
```

The field name is `aster.store`. Relative paths resolve from the Aster home (`/etc/mihomo` on this page), so state lives at:

```text
/etc/mihomo/state/aster-state.json
/etc/mihomo/state/aster-state.json.bak
/etc/mihomo/state/aster-state.json.lock
```

`public-base-url` must be an absolute HTTPS URL with no userinfo, query, or fragment. Aster uses its hostname as the proxy-link server and the listener’s real port.

### How YAML users relate to state

On first start, if state has no matching listener yet, Aster imports YAML `users` into state. After that, state is the durable source for managed users. Do not treat YAML as a live user database while you also manage users through the API.

This page starts with empty `{}`/`[]` and creates every account through the API. If you delete state and restart, YAML users may be imported again, so deleting state is not a safe “wipe all accounts” method.

## 3. Validate, start, and confirm state

```sh
sudo chmod 600 /etc/mihomo/config.yaml
sudo /usr/bin/aster-core -d /etc/mihomo -t
sudo systemctl restart aster-core
sudo systemctl status --no-pager aster-core
sudo journalctl -u aster-core -n 80 --no-pager
```

Confirm listeners and the Controller:

```sh
sudo ss -ltnp | grep -E ':(8443|8444|9090)[[:space:]]'
```

Expected:

- `0.0.0.0:8443`: AnyTLS.
- `0.0.0.0:8444`: VLESS.
- `127.0.0.1:9090`: Controller.

Check state permissions:

```sh
sudo stat \
  -c '%A %a %U:%G %n' \
  /etc/mihomo/state \
  /etc/mihomo/state/aster-state.json \
  /etc/mihomo/state/aster-state.json.bak
```

With the package’s root service, expect directory `700`, state files `600`, and owner `root`. If the service uses a dedicated account, the owner must be that account. Aster rejects an unsafe directory, a symlink, a non-regular file, or a loose state file.

## 4. Expose only the subscription route

Caddy can forward only the public subscription, for example:

```caddy
subscription.example.com {
	route {
		@asterSubscription path /sub/aster/*
		reverse_proxy @asterSubscription 127.0.0.1:9090
		respond 404
	}

	header {
		Referrer-Policy "no-referrer"
	}
}
```

Other paths become 404. `/api/admin/*` is not matched by this rule. Aster already sets `Cache-Control: no-store` on subscription responses; the reverse proxy must not make them cacheable.

Run admin operations from VPS localhost, or through an SSH tunnel:

```sh
ssh -N -L 9090:127.0.0.1:9090 root@<VPS_HOST>
```

The admin client still calls `http://127.0.0.1:9090`. If you bind a plaintext `external-controller` off loopback, Aster deliberately does not mount Admin routes. Do not “fix” remote access with `0.0.0.0:9090`.

## 5. Prepare a safer curl environment

In a trusted admin terminal, set the base URL and type the Aster secret interactively so it is less likely to stay in shell history:

```sh
export ASTER_API='http://127.0.0.1:9090'
read -r -s -p 'Aster secret: ' ASTER_TOKEN
echo
export ASTER_TOKEN

acurl() {
  curl \
    --silent \
    --show-error \
    --fail \
    -H "Authorization: Bearer ${ASTER_TOKEN}" \
    "$@"
}
```

CLI requests usually have no `Origin`, so they pass the same-origin check. The Bearer token is still required. `aster.secret` is not the root-level Controller `secret`; the wrong one returns `401`.

Test overview first:

```sh
acurl "${ASTER_API}/api/admin/overview" |
  jq '{status, api_version, authentication_enabled, users}'
```

Expected:

```json
{
  "status": "running",
  "api_version": 1,
  "authentication_enabled": true,
  "users": {
    "total": 0,
    "enabled": 0,
    "disabled": 0
  }
}
```

The important part is `running` with authentication enabled. Listener summary is checked in the next step.

## 6. Read listeners and revision

```sh
acurl "${ASTER_API}/api/admin/inbounds" |
  jq '.inbounds[] | {
    tag, type, managed, credential, flow, traffic,
    user_count, enabled_user_count,
    revision, applied_revision, pending
  }'
```

Both `edge-anytls` and `edge-vless` should look like:

```json
{
  "managed": true,
  "revision": 1785312000000,
  "applied_revision": 1785312000000
}
```

Revision is a **per-listener** positive integer and is not guaranteed to increment by one. Read the current revision before every mutation. Mutating `edge-anytls` does not change the `edge-vless` revision.

Store the current values in the shell:

```sh
ANYTLS_REV=$(
  acurl "${ASTER_API}/api/admin/inbounds" |
    jq -er '.inbounds[] | select(.tag == "edge-anytls") | .revision'
)

VLESS_REV=$(
  acurl "${ASTER_API}/api/admin/inbounds" |
    jq -er '.inbounds[] | select(.tag == "edge-vless") | .revision'
)
```

## 7. Create an AnyTLS user

You can supply `password` yourself. If you omit it or leave it empty, Aster generates a 32-byte Base64URL password. This page lets the server generate one so you do not pick a weak password:

```sh
ANYTLS_CREATE=$(
  jq -n \
    --arg inbound 'edge-anytls' \
    --arg name 'alice-phone' \
    --argjson revision "${ANYTLS_REV}" \
    '{
      inbound: $inbound,
      name: $name,
      enabled: true,
      revision: $revision
    }' |
  acurl \
    -X POST \
    -H 'Content-Type: application/json' \
    --data-binary @- \
    "${ASTER_API}/api/admin/users"
)
```

Inspect the create result only in a trusted terminal:

```sh
printf '%s\n' "${ANYTLS_CREATE}" |
  jq '{id, inbound, type, name, password, enabled, revision, subscription_url}'
```

Success is HTTP `201 Created`. The response `type` is `anytls`, and it includes the generated `password`, a new revision, and an eligible subscription URL.

Save the fields you need next:

```sh
ANYTLS_ID=$(printf '%s\n' "${ANYTLS_CREATE}" | jq -er '.id')
ANYTLS_REV=$(printf '%s\n' "${ANYTLS_CREATE}" | jq -er '.revision')
ANYTLS_SUB=$(printf '%s\n' "${ANYTLS_CREATE}" | jq -er '.subscription_url')
unset ANYTLS_CREATE
```

## 8. Create a VLESS user

VLESS `uuid` can also be omitted; Aster generates a UUID. `flow` accepts only an empty string or `xtls-rprx-vision`:

```sh
VLESS_CREATE=$(
  jq -n \
    --arg inbound 'edge-vless' \
    --arg name 'bob-laptop' \
    --arg flow 'xtls-rprx-vision' \
    --argjson revision "${VLESS_REV}" \
    '{
      inbound: $inbound,
      name: $name,
      flow: $flow,
      enabled: true,
      revision: $revision
    }' |
  acurl \
    -X POST \
    -H 'Content-Type: application/json' \
    --data-binary @- \
    "${ASTER_API}/api/admin/users"
)
```

```sh
printf '%s\n' "${VLESS_CREATE}" |
  jq '{id, inbound, type, name, uuid, flow, enabled, revision, subscription_url}'

VLESS_ID=$(printf '%s\n' "${VLESS_CREATE}" | jq -er '.id')
VLESS_REV=$(printf '%s\n' "${VLESS_CREATE}" | jq -er '.revision')
VLESS_SUB=$(printf '%s\n' "${VLESS_CREATE}" | jq -er '.subscription_url')
unset VLESS_CREATE
```

A successful response has `type` `vless` and a generated UUID. Do not send `password` for VLESS, and do not send `uuid` or `flow` for AnyTLS. Those mistakes return HTTP `400`.

## 9. List users and read one user

List everyone:

```sh
acurl "${ASTER_API}/api/admin/users" |
  jq '.users[] | {
    id, inbound, type, name, flow, enabled,
    upload_bytes, download_bytes, active_connections,
    revision, applied_revision
  }'
```

AnyTLS only:

```sh
acurl "${ASTER_API}/api/admin/users?inbound=edge-anytls" |
  jq '.users'
```

The list response deliberately omits UUID/password. To see credentials, read one user:

```sh
acurl "${ASTER_API}/api/admin/users/${ANYTLS_ID}" |
  jq '{id, inbound, type, name, password, enabled, revision, subscription_url}'

acurl "${ASTER_API}/api/admin/users/${VLESS_ID}" |
  jq '{id, inbound, type, name, uuid, flow, enabled, revision, subscription_url}'
```

Single-user responses, create/update responses, and subscription URLs are sensitive. Do not write them to ordinary application logs or analytics.

## 10. Update an AnyTLS password and name

Read the latest revision, then generate a new password:

```sh
ANYTLS_REV=$(
  acurl "${ASTER_API}/api/admin/users/${ANYTLS_ID}" |
    jq -er '.revision'
)
NEW_ANYTLS_PASSWORD=$(openssl rand -hex 32)
```

Send a partial update; fields you omit stay unchanged:

```sh
ANYTLS_UPDATE=$(
  jq -n \
    --arg name 'alice-tablet' \
    --arg password "${NEW_ANYTLS_PASSWORD}" \
    --argjson revision "${ANYTLS_REV}" \
    '{
      name: $name,
      password: $password,
      revision: $revision
    }' |
  acurl \
    -X PUT \
    -H 'Content-Type: application/json' \
    --data-binary @- \
    "${ASTER_API}/api/admin/users/${ANYTLS_ID}"
)

printf '%s\n' "${ANYTLS_UPDATE}" |
  jq '{id, name, enabled, revision, applied_revision}'
ANYTLS_REV=$(printf '%s\n' "${ANYTLS_UPDATE}" | jq -er '.revision')
unset ANYTLS_UPDATE NEW_ANYTLS_PASSWORD
```

Expect `revision == applied_revision`. The new password is used for authentication immediately; the old password cannot open a new session. Aster also closes active/pending connections whose credentials are now invalid. Connections for other users that this change does not affect stay up. Still verify both the new and old password with a brand-new client session.

## 11. Update a VLESS UUID, flow, or name

Generate a new UUID with the Aster CLI:

```sh
NEW_VLESS_UUID=$(/usr/bin/aster-core generate uuid)
VLESS_REV=$(
  acurl "${ASTER_API}/api/admin/users/${VLESS_ID}" |
    jq -er '.revision'
)
```

```sh
VLESS_UPDATE=$(
  jq -n \
    --arg name 'bob-desktop' \
    --arg uuid "${NEW_VLESS_UUID}" \
    --arg flow 'xtls-rprx-vision' \
    --argjson revision "${VLESS_REV}" \
    '{
      name: $name,
      uuid: $uuid,
      flow: $flow,
      revision: $revision
    }' |
  acurl \
    -X PUT \
    -H 'Content-Type: application/json' \
    --data-binary @- \
    "${ASTER_API}/api/admin/users/${VLESS_ID}"
)

printf '%s\n' "${VLESS_UPDATE}" |
  jq '{id, name, flow, enabled, revision, applied_revision}'
VLESS_REV=$(printf '%s\n' "${VLESS_UPDATE}" | jq -er '.revision')
unset VLESS_UPDATE NEW_VLESS_UUID
```

To clear flow, send `"flow": ""` explicitly. Any other flow returns `400`.

## 12. Disable and re-enable a user

Disable removes the user from the live credential set but keeps state, traffic, and the user ID. That is a reversible revoke. Disable AnyTLS first:

```sh
ANYTLS_REV=$(
  acurl "${ASTER_API}/api/admin/users/${ANYTLS_ID}" |
    jq -er '.revision'
)

ANYTLS_DISABLE=$(
  jq -n \
    --argjson revision "${ANYTLS_REV}" \
    '{enabled: false, revision: $revision}' |
  acurl \
    -X PUT \
    -H 'Content-Type: application/json' \
    --data-binary @- \
    "${ASTER_API}/api/admin/users/${ANYTLS_ID}"
)
ANYTLS_REV=$(printf '%s\n' "${ANYTLS_DISABLE}" | jq -er '.revision')
printf '%s\n' "${ANYTLS_DISABLE}" |
  jq '{id, enabled, active_connections, revision}'
unset ANYTLS_DISABLE
```

Then disable VLESS:

```sh
VLESS_REV=$(
  acurl "${ASTER_API}/api/admin/users/${VLESS_ID}" |
    jq -er '.revision'
)

VLESS_DISABLE=$(
  jq -n \
    --argjson revision "${VLESS_REV}" \
    '{enabled: false, revision: $revision}' |
  acurl \
    -X PUT \
    -H 'Content-Type: application/json' \
    --data-binary @- \
    "${ASTER_API}/api/admin/users/${VLESS_ID}"
)
VLESS_REV=$(printf '%s\n' "${VLESS_DISABLE}" | jq -er '.revision')
printf '%s\n' "${VLESS_DISABLE}" |
  jq '{id, enabled, active_connections, revision}'
unset VLESS_DISABLE
```

After disable, both subscription URLs should return `404`:

```sh
curl -sS -o /dev/null -w 'AnyTLS subscription: HTTP %{http_code}\n' "${ANYTLS_SUB}"
curl -sS -o /dev/null -w 'VLESS subscription: HTTP %{http_code}\n' "${VLESS_SUB}"
```

Also confirm with a fully restarted new client session that the credentials can no longer log in.

To re-enable, use each user’s latest revision:

```sh
ANYTLS_ENABLE=$(
  jq -n \
    --argjson revision "${ANYTLS_REV}" \
    '{enabled: true, revision: $revision}' |
  acurl \
    -X PUT \
    -H 'Content-Type: application/json' \
    --data-binary @- \
    "${ASTER_API}/api/admin/users/${ANYTLS_ID}"
)
ANYTLS_REV=$(printf '%s\n' "${ANYTLS_ENABLE}" | jq -er '.revision')
unset ANYTLS_ENABLE

VLESS_ENABLE=$(
  jq -n \
    --argjson revision "${VLESS_REV}" \
    '{enabled: true, revision: $revision}' |
  acurl \
    -X PUT \
    -H 'Content-Type: application/json' \
    --data-binary @- \
    "${ASTER_API}/api/admin/users/${VLESS_ID}"
)
VLESS_REV=$(printf '%s\n' "${VLESS_ENABLE}" | jq -er '.revision')
unset VLESS_ENABLE
```

## 13. Fetch, decode, and rotate subscriptions

After a user is enabled, the single-user response includes a subscription URL:

```sh
ANYTLS_SUB=$(
  acurl "${ASTER_API}/api/admin/users/${ANYTLS_ID}" |
    jq -er '.subscription_url'
)
VLESS_SUB=$(
  acurl "${ASTER_API}/api/admin/users/${VLESS_ID}" |
    jq -er '.subscription_url'
)
```

The subscription endpoint does not use a Bearer token. The body is one Base64-encoded proxy URI:

```sh
curl --fail --silent --show-error "${ANYTLS_SUB}" | base64 -d
echo
curl --fail --silent --show-error "${VLESS_SUB}" | base64 -d
echo
```

They should start with `anytls://` and `vless://`. If macOS `base64` does not accept `-d`, use `base64 -D`.

An AnyTLS + REALITY link should include:

```text
security=reality
type=tcp
sni=...
fp=chrome
pbk=...
sid=...
```

Before rotating the AnyTLS subscription, save the current revision and old URL:

```sh
STALE_REV=$(
  acurl "${ASTER_API}/api/admin/users/${ANYTLS_ID}" |
    jq -er '.revision'
)
OLD_ANYTLS_SUB="${ANYTLS_SUB}"
```

```sh
ANYTLS_ROTATE=$(
  jq -n \
    --argjson revision "${STALE_REV}" \
    '{revision: $revision}' |
  acurl \
    -X POST \
    -H 'Content-Type: application/json' \
    --data-binary @- \
    "${ASTER_API}/api/admin/users/${ANYTLS_ID}/rotate-subscription"
)

printf '%s\n' "${ANYTLS_ROTATE}" | jq .
ANYTLS_REV=$(printf '%s\n' "${ANYTLS_ROTATE}" | jq -er '.revision')
ANYTLS_SUB=$(printf '%s\n' "${ANYTLS_ROTATE}" | jq -er '.subscription_url')
unset ANYTLS_ROTATE
```

The old token is invalid immediately; the new token works:

```sh
curl -sS -o /dev/null -w 'old: HTTP %{http_code}\n' "${OLD_ANYTLS_SUB}"
curl -sS -o /dev/null -w 'new: HTTP %{http_code}\n' "${ANYTLS_SUB}"
```

Expect `old: HTTP 404` and `new: HTTP 200`. Rotating VLESS uses the same endpoint; swap the ID, revision, and variables:

```sh
VLESS_REV=$(
  acurl "${ASTER_API}/api/admin/users/${VLESS_ID}" |
    jq -er '.revision'
)
OLD_VLESS_SUB="${VLESS_SUB}"

VLESS_ROTATE=$(
  jq -n \
    --argjson revision "${VLESS_REV}" \
    '{revision: $revision}' |
  acurl \
    -X POST \
    -H 'Content-Type: application/json' \
    --data-binary @- \
    "${ASTER_API}/api/admin/users/${VLESS_ID}/rotate-subscription"
)
VLESS_REV=$(printf '%s\n' "${VLESS_ROTATE}" | jq -er '.revision')
VLESS_SUB=$(printf '%s\n' "${VLESS_ROTATE}" | jq -er '.subscription_url')
unset VLESS_ROTATE
```

Rotating the URL does not change the AnyTLS password or VLESS UUID. It only revokes the old subscription token. If you also suspect a credential leak, update the credential separately.

## 14. Handle revision `409 Conflict` correctly

The `STALE_REV` saved above is the AnyTLS revision from before rotate. Rotate already advanced the listener to `ANYTLS_REV`. Deliberately send an update with the old value:

```sh
CONFLICT_BODY=$(mktemp)
chmod 600 "${CONFLICT_BODY}"

CONFLICT_STATUS=$(
  jq -n \
    --arg name 'must-not-apply' \
    --argjson revision "${STALE_REV}" \
    '{name: $name, revision: $revision}' |
  curl \
    --silent \
    --show-error \
    -o "${CONFLICT_BODY}" \
    -w '%{http_code}' \
    -X PUT \
    -H "Authorization: Bearer ${ASTER_TOKEN}" \
    -H 'Content-Type: application/json' \
    --data-binary @- \
    "${ASTER_API}/api/admin/users/${ANYTLS_ID}"
)

printf 'HTTP %s\n' "${CONFLICT_STATUS}"
jq . "${CONFLICT_BODY}"
rm -f "${CONFLICT_BODY}"
```

Expected:

```text
HTTP 409
```

The error message includes the expected/current revision. The correct recovery is:

1. GET the listener or single user again.
2. Compare the server’s current state with the change you have not sent yet.
3. After merging, build a new request with the reread current revision.
4. If you get 409 again, reread. Do not guess the revision.

```sh
CURRENT_ANYTLS=$(
  acurl "${ASTER_API}/api/admin/users/${ANYTLS_ID}"
)
printf '%s\n' "${CURRENT_ANYTLS}" |
  jq '{id, name, enabled, revision, applied_revision}'

CURRENT_ANYTLS_REV=$(
  printf '%s\n' "${CURRENT_ANYTLS}" |
    jq -er '.revision'
)

jq -n \
  --arg name 'alice-final' \
  --argjson revision "${CURRENT_ANYTLS_REV}" \
  '{name: $name, revision: $revision}' |
acurl \
  -X PUT \
  -H 'Content-Type: application/json' \
  --data-binary @- \
  "${ASTER_API}/api/admin/users/${ANYTLS_ID}" |
jq '{id, name, revision, applied_revision}'

unset CURRENT_ANYTLS CURRENT_ANYTLS_REV STALE_REV
```

Do not increment an old revision by one yourself. Aster revisions may advance using the current Unix milliseconds, and a mutation of another user on the same listener also expires the old revision.

## 15. Delete AnyTLS and VLESS users

Delete is not as reversible as disable. It removes the user and the subscription token. Production usually disables first, observes, then deletes.

Read the latest revision, then delete AnyTLS:

```sh
ANYTLS_REV=$(
  acurl "${ASTER_API}/api/admin/users/${ANYTLS_ID}" |
    jq -er '.revision'
)

curl \
  --silent \
  --show-error \
  --fail \
  -o /dev/null \
  -w 'AnyTLS delete: HTTP %{http_code}\n' \
  -X DELETE \
  -H "Authorization: Bearer ${ASTER_TOKEN}" \
  "${ASTER_API}/api/admin/users/${ANYTLS_ID}?revision=${ANYTLS_REV}"
```

Delete VLESS:

```sh
VLESS_REV=$(
  acurl "${ASTER_API}/api/admin/users/${VLESS_ID}" |
    jq -er '.revision'
)

curl \
  --silent \
  --show-error \
  --fail \
  -o /dev/null \
  -w 'VLESS delete: HTTP %{http_code}\n' \
  -X DELETE \
  -H "Authorization: Bearer ${ASTER_TOKEN}" \
  "${ASTER_API}/api/admin/users/${VLESS_ID}?revision=${VLESS_REV}"
```

Both successful responses are HTTP `204 No Content`. Verify:

```sh
acurl "${ASTER_API}/api/admin/users" |
  jq --arg anytls "${ANYTLS_ID}" --arg vless "${VLESS_ID}" \
    '[.users[] | select(.id == $anytls or .id == $vless)]'

curl -sS -o /dev/null -w 'AnyTLS sub: HTTP %{http_code}\n' "${ANYTLS_SUB}"
curl -sS -o /dev/null -w 'VLESS sub: HTTP %{http_code}\n' "${VLESS_SUB}"
```

Expect an empty user array `[]` and `404` for both subscriptions.

## 16. Back up state and sensitive data

State contains:

- VLESS UUIDs.
- AnyTLS passwords.
- Subscription tokens.
- User IDs/names, traffic, and revisions.

`config.yaml` also contains REALITY private keys, the Controller secret, and the Aster secret. Treat both as a credential database.

The most consistent backup is a short stop of the single Aster instance:

```sh
sudo systemctl stop aster-core

BACKUP_STAMP=$(date -u +%Y%m%dT%H%M%SZ)
sudo install -d -m 700 "/root/aster-backups/${BACKUP_STAMP}"
sudo cp --preserve=mode,ownership,timestamps \
  /etc/mihomo/config.yaml \
  /etc/mihomo/state/aster-state.json \
  /etc/mihomo/state/aster-state.json.bak \
  "/root/aster-backups/${BACKUP_STAMP}/"

sudo systemctl start aster-core
sudo systemctl is-active aster-core
```

You do not need to back up `.lock`. After the service is running again, encrypt the backup with age, offline media, or a controlled backup system. Do not upload a plaintext tar to ordinary object storage.

If you cannot stop the process, both the primary and `.bak` use atomic replace. Still copy both in the same short window and keep the newer generation that you can validate. Offline backups remain easier to reason about.

### Restore state

Restoring old state also restores every user, credential, token, traffic counter, and revision from that moment. It can re-enable revoked data. Do it only after you understand the impact:

1. Stop Aster.
2. Copy the current state into another root-only forensic directory first.
3. Confirm the backup’s listener names match the current YAML exactly.
4. Restore the primary and `.bak`, with mode `0600`.
5. Run `aster-core -t` first, then start and check logs / an Admin snapshot.
6. Verify that credentials/tokens that should stay revoked were not restored by accident.

Do not hand-edit JSON `generation`, `revision`, tokens, or `version` to bypass checks. If the primary is damaged, Aster tries a valid `.bak`. If both fail, preserve the scene and restore from a controlled backup.

## 17. Common problems and recovery

### Admin API returns 404

- There is no `aster` block.
- The plaintext Controller is bound off loopback, so Admin routes were not mounted.
- The listener name is not in `managed-listeners`.
- The reverse proxy / SSH tunnel points at the wrong Controller.

On the VPS first:

```sh
curl -v \
  -H "Authorization: Bearer ${ASTER_TOKEN}" \
  http://127.0.0.1:9090/api/admin/status
```

### Returns 401 or 403

- `401`: you used the root-level Controller secret instead of `aster.secret`.
- `403`: browser `Origin`/`Sec-Fetch-Site` or the reverse-proxy scheme/host failed the same-origin check.

CLI does not need an `Origin` you add yourself. Dashboards should go through a same-origin backend/BFF. Do not put the Aster token in a frontend bundle, URL, or localStorage.

### Mutation returns 400

- `revision` is missing or not a positive integer.
- User name is blank, has leading/trailing space, is longer than 256 characters, or duplicates another name on the same listener (case-insensitive).
- A credential is duplicated on the same listener.
- AnyTLS is missing a password, or it also sent UUID/flow.
- VLESS UUID is invalid, or flow is not empty/`xtls-rprx-vision`.
- JSON over 1 MiB is `413`.

### `revision != applied_revision` or `pending: true`

Do not continue batch mutations. Check Aster logs, listener sockets, and the current user snapshot first. When start/apply of managed credentials fails, Aster prefers fail-closed. Fix the runtime/store problem first.

### Subscription returns 404

- The user is disabled, deleted, or the token was rotated.
- `public-base-url` is not set.
- The listener is no longer managed or does not yet have a usable port.
- The security mode cannot be exported. ShadowTLS, ResTLS, and JLS do not produce managed share links.
- VLESS uses advanced XHTTP placement/padding.

### Accidental update or disable

If the user is not deleted yet, prefer the Admin API and the latest revision to put name/credential/enabled back. That only affects the target listener. Do not restore the whole state for one field mistake.

If the user is already deleted:

- Safer: create a new user and issue a new credential and subscription URL.
- Only consider a full state rollback if you must restore the original ID/token, and weigh the fact that every other user rolls back too.

## 18. End the admin session

Clear shell variables that hold tokens, IDs, and URLs:

```sh
unset \
  ASTER_TOKEN ASTER_API \
  ANYTLS_ID ANYTLS_REV ANYTLS_SUB OLD_ANYTLS_SUB \
  VLESS_ID VLESS_REV VLESS_SUB OLD_VLESS_SUB
unset -f acurl
```

Close the SSH tunnel, and confirm you did not upload terminal scrollback, `jq` responses, or subscription URLs into a ticket system.

## Next steps

- [Aster Admin API fields and status codes](/en/aster/api)
- [Security, state store, and reverse proxy](/en/aster/security)
- [AnyTLS + REALITY detailed deployment](/en/tutorials/anytls-reality)
- [Linux production deployment](/en/tutorials/linux-production)
- [Troubleshooting](/en/troubleshooting)
