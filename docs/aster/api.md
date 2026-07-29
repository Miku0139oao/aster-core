# Aster Admin API

## Base URL 與認證

以下範例假設：

```text
http://127.0.0.1:9090
```

每個 admin request 都必須帶：

```http
Authorization: Bearer <aster-secret>
```

這個 token 來自 `aster.secret`，不是一般 Controller `secret`。

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

回應包含：

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

目前回傳 VLESS 與 AnyTLS，`update_policy` 為 `live`。

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

`listeners` 是相同資料的 compatibility alias。

## List users

```http
GET /api/admin/users
GET /api/admin/users?inbound=edge-vless
```

List response 不包含 UUID/password：

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

單一 user response 會包含 credential 及 eligible subscription URL。此 endpoint 的輸出應視為敏感資料。

## Create VLESS user

先從 `/inbounds` 取得 revision：

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

成功回傳 `201 Created` 及新的 revision。

## Create AnyTLS user

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

## Update user

```http
PUT /api/admin/users/{id}
```

只有要變更的欄位需要出現；`revision` 必填：

```json
{
  "name": "alice-renamed",
  "enabled": false,
  "revision": 5
}
```

VLESS 可更新 `uuid`、`flow`；AnyTLS 可更新 `password`。

## Delete user

```sh
curl \
  -X DELETE \
  -H "Authorization: Bearer $ASTER_TOKEN" \
  "$ASTER_API/api/admin/users/user-id?revision=6"
```

成功回傳 `204 No Content`。

## Reset traffic

```sh
curl \
  -X POST \
  -H "Authorization: Bearer $ASTER_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"revision": 6}' \
  "$ASTER_API/api/admin/users/user-id/reset-traffic"
```

回傳更新後 user view。

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

舊 token 立即失效。

## Subscription endpoint

```http
GET /sub/aster/{token}
HEAD /sub/aster/{token}
```

- 不使用 Bearer token。
- Token 必須不可預測。
- 回應 `text/plain; charset=utf-8`。
- Body 是 Base64 編碼的單一 proxy share link。
- 帶 `Cache-Control: no-store`。
- 無效、停用或不 eligible user 回傳 `404`。

## Status codes

| Code | 意義 |
| --- | --- |
| `200` | 成功 |
| `201` | User 建立成功 |
| `204` | 刪除成功 |
| `400` | JSON、欄位、credential 或 revision 無效 |
| `401` | Aster Bearer token 無效 |
| `403` | Same-origin 檢查失敗 |
| `404` | Aster 未啟用、資源不存在或 subscription 不 eligible |
| `409` | Revision/store generation conflict |
| `413` | Request body 超過 1 MiB |
| `500` | Store、runtime apply 或內部錯誤 |

## 面板實作建議

1. 先讀 `/inbounds` 與 `/users`。
2. 每次 mutation 使用畫面最後讀到的 listener revision。
3. 遇到 409 重新讀取並提示使用者合併變更。
4. List view 不依賴 credentials；只有編輯單一 user 時才呼叫 GET one。
5. 不把 Aster token 寫入 URL、localStorage 或前端 bundle。
6. 管理端透過同 origin backend/BFF 呼叫 API。
