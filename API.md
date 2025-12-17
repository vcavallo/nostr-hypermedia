# API Reference

Complete API documentation for integrating with nostr-hypermedia.

## HTML Pages

| Endpoint | Description |
|----------|-------------|
| `GET /` | Redirects to timeline |
| `GET /html/timeline` | Main timeline with feed/kind filters |
| `GET /html/timeline/check-new` | Check for new posts since timestamp (returns indicator) |
| `GET /html/thread/{id}` | Thread view with replies |
| `GET /html/profile/{npub}` | User profile page |
| `GET /html/profile/edit` | Edit profile (requires login) |
| `GET /html/search` | Full-text search (NIP-50) |
| `GET /html/notifications` | Notifications (requires login) |
| `GET /html/mutes` | Muted users (requires login) |
| `GET /html/quote/{id}` | Quote post form |
| `GET /html/compose` | Compose page (no-JS media fallback) |
| `GET /html/login` | Login page |
| `GET /html/logout` | Logout (requires login) |
| `GET /html/check-connection` | Check NIP-46 connection status |
| `GET /html/reconnect` | Reconnect NIP-46 session |
| `GET /html/wallet` | Wallet connection (requires login) |
| `GET /html/wallet/info` | Wallet balance fragment |
| `GET /html/gifs` | GIF picker panel |
| `GET /html/gifs/search` | Search GIFs |

## Query Parameters

### Timeline (`/html/timeline`)

| Parameter | Type | Description |
|-----------|------|-------------|
| `feed` | string | Feed mode: `follows`, `global`, `me` |
| `kinds` | string | Comma-separated event kinds (e.g., `1,6` or `30023`) |
| `limit` | int | Number of events (default: 20) |
| `until` | int | Unix timestamp for pagination (fetch older) |
| `cache_only` | bool | Return empty on cache miss (no relay fetch) |
| `refresh` | bool | Force full page render (for config reload) |

### Thread (`/html/thread/{id}`)

| Parameter | Type | Description |
|-----------|------|-------------|
| `{id}` | string | Event ID (hex), `note1...`, `nevent1...`, or `naddr1...` |

### Profile (`/html/profile/{npub}`)

| Parameter | Type | Description |
|-----------|------|-------------|
| `{npub}` | string | `npub1...` or hex pubkey |

### Search (`/html/search`)

| Parameter | Type | Description |
|-----------|------|-------------|
| `q` | string | Search query |
| `kinds` | string | Filter by kinds |
| `limit` | int | Results per page (default: 20) |
| `until` | int | Pagination cursor |

### Notifications (`/html/notifications`)

| Parameter | Type | Description |
|-----------|------|-------------|
| `until` | int | Pagination cursor |
| `limit` | int | Results per page (default: 20) |

## POST Actions

All POST endpoints require login and CSRF token.

| Endpoint | Description |
|----------|-------------|
| `POST /html/post` | Create note |
| `POST /html/reply` | Reply to note |
| `POST /html/react` | React to note |
| `POST /html/repost` | Repost note |
| `POST /html/zap` | Zap note (requires wallet) |
| `POST /html/bookmark` | Toggle bookmark |
| `POST /html/mute` | Toggle mute |
| `POST /html/follow` | Toggle follow |
| `POST /html/profile/edit` | Update profile |
| `POST /html/theme` | Toggle theme |
| `POST /html/wallet/connect` | Connect NWC wallet |
| `POST /html/wallet/disconnect` | Disconnect wallet |
| `POST /html/gifs/select` | Select GIF attachment |
| `POST /html/gifs/clear` | Clear GIF selection |
| `POST /html/gifs/close` | Close GIF picker |

## Request Body Formats

All bodies are `application/x-www-form-urlencoded`.

### Post

```
content=Hello%20world&csrf_token=xxx&return_url=/html/timeline
```

| Field | Required | Description |
|-------|----------|-------------|
| `content` | Yes | Note content |
| `csrf_token` | Yes | CSRF token |
| `return_url` | No | Redirect after success |

### Reply

```
content=Reply%20text&csrf_token=xxx&event_id=abc123&return_url=/html/thread/abc123
```

| Field | Required | Description |
|-------|----------|-------------|
| `content` | Yes | Reply content |
| `event_id` | Yes | Event being replied to |
| `csrf_token` | Yes | CSRF token |
| `return_url` | No | Redirect after success |

### React

```
csrf_token=xxx&event_id=abc123&reaction=%E2%9D%A4%EF%B8%8F&return_url=/html/timeline
```

| Field | Required | Description |
|-------|----------|-------------|
| `event_id` | Yes | Event being reacted to |
| `reaction` | No | Reaction emoji (default: from config) |
| `csrf_token` | Yes | CSRF token |
| `return_url` | No | Redirect after success |

### Repost

```
csrf_token=xxx&event_id=abc123&return_url=/html/timeline
```

| Field | Required | Description |
|-------|----------|-------------|
| `event_id` | Yes | Event being reposted |
| `csrf_token` | Yes | CSRF token |
| `return_url` | No | Redirect after success |

### Zap

```
csrf_token=xxx&event_id=abc123&event_pubkey=def456&amount=1000&return_url=/html/timeline
```

| Field | Required | Description |
|-------|----------|-------------|
| `event_id` | Yes | Event being zapped |
| `event_pubkey` | Yes | Event author's pubkey |
| `amount` | Yes | Amount in sats |
| `csrf_token` | Yes | CSRF token |
| `return_url` | No | Redirect after success |

### Bookmark

```
csrf_token=xxx&event_id=abc123&return_url=/html/timeline
```

| Field | Required | Description |
|-------|----------|-------------|
| `event_id` | Yes | Event to bookmark/unbookmark |
| `csrf_token` | Yes | CSRF token |
| `return_url` | No | Redirect after success |

### Mute

```
csrf_token=xxx&pubkey=def456&return_url=/html/profile/npub1...
```

| Field | Required | Description |
|-------|----------|-------------|
| `pubkey` | Yes | User pubkey to mute/unmute |
| `csrf_token` | Yes | CSRF token |
| `return_url` | No | Redirect after success |

### Follow

```
csrf_token=xxx&pubkey=def456&return_url=/html/profile/npub1...
```

| Field | Required | Description |
|-------|----------|-------------|
| `pubkey` | Yes | User pubkey to follow/unfollow |
| `csrf_token` | Yes | CSRF token |
| `return_url` | No | Redirect after success |

### Wallet Connect

```
csrf_token=xxx&nwc_uri=nostr%2Bwalletconnect://...
```

| Field | Required | Description |
|-------|----------|-------------|
| `nwc_uri` | Yes | NWC connection URI |
| `csrf_token` | Yes | CSRF token |

### Profile Edit

```
csrf_token=xxx&display_name=Alice&about=Hello&picture=https://...&banner=https://...
```

| Field | Required | Description |
|-------|----------|-------------|
| `display_name` | No | Display name |
| `about` | No | Bio/description |
| `picture` | No | Avatar URL |
| `banner` | No | Banner image URL |
| `csrf_token` | Yes | CSRF token |

## JSON API (Siren Format)

The `/timeline` and `/thread/{id}` endpoints return [Siren](https://github.com/kevinswiber/siren) hypermedia format when `Accept: application/json` header is sent.

### Example Request

```bash
curl -H "Accept: application/json" "http://localhost:3000/timeline?feed=global&limit=5"
```

### Example Response

```json
{
  "class": ["timeline"],
  "properties": {
    "feed": "global",
    "kinds": [1],
    "count": 20
  },
  "entities": [
    {
      "class": ["event", "note"],
      "rel": ["item"],
      "properties": {
        "id": "abc123...",
        "pubkey": "def456...",
        "kind": 1,
        "content": "Hello world",
        "created_at": 1699900000,
        "author": {
          "name": "Alice",
          "picture": "https://..."
        }
      },
      "actions": [
        {
          "name": "reply",
          "method": "POST",
          "href": "/html/reply",
          "fields": [
            {"name": "event_id", "type": "hidden", "value": "abc123..."},
            {"name": "csrf_token", "type": "hidden"}
          ]
        }
      ],
      "links": [
        {"rel": ["self"], "href": "/html/thread/abc123..."},
        {"rel": ["author"], "href": "/html/profile/npub1..."}
      ]
    }
  ],
  "links": [
    {"rel": ["self"], "href": "/timeline?feed=global"},
    {"rel": ["next"], "href": "/timeline?feed=global&until=1699899000"}
  ]
}
```

## SSE (Server-Sent Events)

| Endpoint | Auth | Description |
|----------|------|-------------|
| `/stream/timeline` | No | Live timeline events (JSON) |
| `/stream/notifications?format=html\|json` | Yes | Live notifications |
| `/stream/config` | No | Config reload trigger |

### Notification Stream

Requires `format` parameter (EventSource doesn't support custom headers):
- `?format=html` - Returns HTML fragment for HelmJS
- `?format=json` - Returns JSON event data

```javascript
const es = new EventSource('/stream/notifications?format=json');
es.addEventListener('notification', (e) => {
  const data = JSON.parse(e.data);
  console.log('New notification:', data);
});
```

### Event Format

```
event: notification
data: {"id":"abc123","kind":7,"pubkey":"def456","content":"+"}

event: reload
data: {}
```

## Authentication

### Session Cookie

Sessions are managed via HTTP-only cookies:
- **Name:** `nostr_session`
- **Flags:** HTTP-only, SameSite=Lax
- **TTL:** 24 hours

### CSRF Protection

All POST requests require a valid `csrf_token` form field. Tokens are HMAC-SHA256 signed and bound to the session ID.

**Pre-auth CSRF:** Login forms use anonymous session IDs stored in `SameSite=Strict` cookies.

### Authentication Flow

1. User visits `/html/login`
2. Server generates `nostrconnect://` URI with disposable pubkey
3. User scans QR or pastes `bunker://` URL
4. Signer approves connection via NIP-46 relay
5. Session created with user's pubkey

## Rate Limits

| Operation | Limit | Fallback |
|-----------|-------|----------|
| Sign requests | 10/min | 10/min |
| Login attempts | 5/min | 3/min (stricter) |

Rate limiting uses sliding window algorithm. Fallback applies when Redis is unavailable.

## Headers

### Request Headers

| Header | Description |
|--------|-------------|
| `H-Request: true` | HelmJS request (return fragment, not full page) |
| `Accept: application/json` | Request Siren JSON format |
| `Cookie: nostr_session=...` | Session authentication |

### Response Headers

| Header | Description |
|--------|-------------|
| `Content-Type` | `text/html; charset=utf-8` or `application/json` |
| `X-Frame-Options` | `SAMEORIGIN` |
| `X-Content-Type-Options` | `nosniff` |
| `Content-Security-Policy` | Restrictive CSP |
| `Strict-Transport-Security` | HSTS (when enabled) |

## HTTP Status Codes

| Code | Meaning |
|------|---------|
| 200 | Success |
| 303 | Redirect after POST (See Other) |
| 400 | Bad request (invalid parameters) |
| 401 | Unauthorized (login required) |
| 403 | Forbidden (invalid CSRF token) |
| 404 | Not found |
| 429 | Rate limited |
| 500 | Server error |
| 503 | Service unavailable (health check failed) |

## Health & Monitoring

| Endpoint | Description |
|----------|-------------|
| `GET /health` | Health check (503 if degraded) |
| `GET /health?verbose=1` | Health check with per-relay breakdown |
| `GET /health/live` | Liveness probe (always 200 if running) |
| `GET /health/ready` | Readiness probe (503 if no healthy relays) |
| `GET /metrics` | Prometheus metrics |

### Health Response

```json
{
  "status": "healthy",
  "server": {
    "uptime": "2h30m",
    "goroutines": 42
  },
  "relays": {
    "healthy": 5,
    "unhealthy": 1,
    "avg_response_ms": 120
  },
  "cache": {
    "backend": "redis",
    "hit_ratio": 0.85
  }
}
```

### Metrics (For Prometheus and Docker)

Key metrics exposed at `/metrics`:

| Metric | Type | Description |
|--------|------|-------------|
| `http_requests_total` | counter | Total HTTP requests |
| `http_errors_total` | counter | Total HTTP 5xx errors |
| `nostr_relay_connections_active` | gauge | Active relay connections |
| `nostr_relays_healthy` | gauge | Healthy relay count |
| `cache_hit_ratio` | gauge | Cache hit ratio (0-1) |
| `sse_connections_active` | gauge | Active SSE connections |
