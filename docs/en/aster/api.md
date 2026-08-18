# Aster Admin API

## Base URL and authentication

The examples assume:

```text
http://127.0.0.1:9090
```

Every admin request must include:

```http
Authorization: Bearer <aster-secret>
```

This token comes from `aster.secret`, not the ordinary Controller `secret`.

```sh
export ASTER_API=http://127.0.0.1:9090
export ASTER_TOKEN='replace-with-a-random-secret-at-least-32-bytes'

curl \
  -H "Authorization: Bearer $ASTER_TOKEN" \
  "$ASTER_API/api/admin/overview"
```

## Overview

```http
GET /api/admin/overview
GET /api/admin/status
```

The response includes:

```json
{
  "version": "v1.0.0",
  "api_version": 1,
  "status": "running",
  "started_at": 1785312000000,
  "uptime_seconds": 3600,
  "platform": {
    "os": "linux",
    "arch": "amd64",
    "cpu_cores": 4,
    "memory_bytes": 12345678,
    "goroutines": 42
  },
  "traffic": {
    "uplink_total": 1024,
    "downlink_total": 4096,
    "active_connections": 3
  },
  "users": {
    "total": 10,
    "enabled": 9,
    "disabled": 1
  },
  "capabilities": {
    "quota": false,
    "expiration": false
  },
  "authentication_enabled": true,
  "inbounds": []
}
```

## Protocols

```http
GET /api/admin/protocols
```

Currently returns VLESS and AnyTLS, with `update_policy` `live`.

## Inbounds

```http
GET /api/admin/inbounds
GET /api/admin/listeners
```

```json
{
  "inbounds": [
    {
      "tag": "edge-vless",
      "type": "vless",
      "managed": true,
      "credential": "uuid",
      "flow": true,
      "traffic": true,
      "user_count": 2,
      "enabled_user_count": 2,
      "revision": 4,
      "applied_revision": 4
    }
  ],
  "listeners": [
    {
      "tag": "edge-vless",
      "type": "vless",
      "managed": true,
      "credential": "uuid",
      "flow": true,
      "traffic": true,
      "user_count": 2,
      "enabled_user_count": 2,
      "revision": 4,
      "applied_revision": 4
    }
  ]
}
```

`listeners` is a compatibility alias for the same data.

## List users

```http
GET /api/admin/users
GET /api/admin/users?inbound=edge-vless
```

The list response does not include UUID/password:

```json
{
  "users": [
    {
      "id": "user-id",
      "inbound": "edge-vless",
      "type": "vless",
      "name": "alice",
      "flow": "xtls-rprx-vision",
      "enabled": true,
      "upload_bytes": 1024,
      "download_bytes": 4096,
      "traffic_generation": 0,
      "created_at": 1785312000000,
      "updated_at": 1785312000000,
      "active_connections": 1,
      "revision": 4,
      "applied_revision": 4
    }
  ],
  "inbounds": []
}
```

## Get one user

```http
GET /api/admin/users/{id}
```

A single-user response includes the credential and an eligible subscription URL. Treat this endpoint’s output as sensitive.

## Create a VLESS user

Read the revision from `/inbounds` first:

```sh
curl \
  -X POST \
  -H "Authorization: Bearer $ASTER_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "inbound": "edge-vless",
    "name": "alice",
    "uuid": "00000000-0000-0000-0000-000000000000",
    "flow": "xtls-rprx-vision",
    "enabled": true,
    "revision": 4
  }' \
  "$ASTER_API/api/admin/users"
```

Success returns `201 Created` and the new revision.

## Create an AnyTLS user

```sh
curl \
  -X POST \
  -H "Authorization: Bearer $ASTER_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "inbound": "edge-anytls",
    "name": "bob",
    "password": "replace-with-a-random-password",
    "enabled": true,
    "revision": 2
  }' \
  "$ASTER_API/api/admin/users"
```

## Update a user

```http
PUT /api/admin/users/{id}
```

Only changed fields need to appear. `revision` is required:

```json
{
  "name": "alice-renamed",
  "enabled": false,
  "revision": 5
}
```

VLESS can update `uuid` and `flow`. AnyTLS can update `password`.

## Delete a user

```sh
curl \
  -X DELETE \
  -H "Authorization: Bearer $ASTER_TOKEN" \
  "$ASTER_API/api/admin/users/user-id?revision=6"
```

Success returns `204 No Content`.

## Reset traffic

```sh
curl \
  -X POST \
  -H "Authorization: Bearer $ASTER_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"revision": 6}' \
  "$ASTER_API/api/admin/users/user-id/reset-traffic"
```

Returns the updated user view.

## Rotate subscription

```sh
curl \
  -X POST \
  -H "Authorization: Bearer $ASTER_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"revision": 7}' \
  "$ASTER_API/api/admin/users/user-id/rotate-subscription"
```

```json
{
  "revision": 8,
  "subscription_url": "https://proxy.example.com/sub/aster/new-token"
}
```

The old token is invalid immediately.

## Subscription endpoint

```http
GET /sub/aster/{token}
HEAD /sub/aster/{token}
```

- Does not use a Bearer token.
- The token must be unpredictable.
- Response is `text/plain; charset=utf-8`.
- Body is a Base64-encoded single proxy share link.
- Sent with `Cache-Control: no-store`.
- Invalid, disabled, or ineligible users return `404`.

## Status codes

| Code | Meaning |
| --- | --- |
| `200` | Success |
| `201` | User created |
| `204` | Delete succeeded |
| `400` | Invalid JSON, field, credential, or revision |
| `401` | Invalid Aster Bearer token |
| `403` | Same-origin check failed |
| `404` | Aster is not enabled, the resource does not exist, or the subscription is ineligible |
| `409` | Revision/store generation conflict |
| `413` | Request body exceeds 1 MiB |
| `500` | Store, runtime apply, or internal error |

## Dashboard implementation notes

1. Read `/inbounds` and `/users` first.
2. Every mutation should use the listener revision last read on the screen.
3. On 409, reread and prompt the user to merge changes.
4. List views should not depend on credentials. Call GET one only when editing a single user.
5. Do not put the Aster token in a URL, localStorage, or frontend bundle.
6. The admin UI should call the API through a same-origin backend/BFF.
