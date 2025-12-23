# API Reference

Complete API documentation for integrating with nostr-server.

## Supported Event Kinds

| Kind | Type | Description |
|------|------|-------------|
| 0 | Profile | User metadata |
| 1 | Note | Short text note |
| 3 | Contacts | Follow list |
| 6 | Repost | Shared note |
| 7 | Reaction | Like/reaction |
| 8 | Badge Award | Badge awarded to users (NIP-58) |
| 16 | Generic Repost | Repost of any event kind |
| 20 | Photo | Image post (NIP-68) |
| 22 | Short Video | Short-form vertical video (NIP-71) |
| 30 | Video | Long-form video (NIP-71) |
| 1063 | File | File metadata (NIP-94) |
| 1111 | Comment | Comments on any content (NIP-22) |
| 1311 | Live Chat | Live stream chat messages (NIP-53) |
| 1984 | Report | Content/user reports (NIP-56) |
| 1985 | Label | Content labels/tags (NIP-32) |
| 9735 | Zap | Lightning payment receipt |
| 9802 | Highlight | Text highlight |
| 10000 | Mutes | Muted users/content |
| 10002 | Relays | Relay list (NIP-65) |
| 10003 | Bookmarks | Saved notes |
| 30009 | Badge | Badge definition (NIP-58) |
| 30017 | Stall | Marketplace shop (NIP-15) |
| 30018 | Product | Marketplace product (NIP-15) |
| 30023 | Article | Long-form content |
| 30311 | Live | Live streaming event (NIP-53) |
| 30315 | Status | User status updates (NIP-38) |
| 30402 | Classified | Marketplace listing (NIP-99) |
| 30617 | Repository | Git repository (NIP-34) |
| 31922 | Calendar Date | Date-based calendar event (NIP-52) |
| 31923 | Calendar Time | Time-based calendar event (NIP-52) |
| 31925 | RSVP | Calendar event RSVP (NIP-52) |
| 31989 | Recommendation | App handler recommendation (NIP-89) |
| 31990 | Handler | Application handler definition (NIP-89) |
| 32123 | Audio | Audio track (NOM - Nostr Open Media) |
| 34550 | Community | Community definition (NIP-72) |

## HTML Pages

| Endpoint | Description |
|----------|-------------|
| `GET /` | Redirects to timeline |
| `GET /timeline` | Main timeline with feed/kind filters |
| `GET /timeline/check-new` | Check for new posts since timestamp (returns indicator) |
| `GET /thread/{id}` | Thread view with replies |
| `GET /thread/{id}/check-new` | Check for new replies since timestamp (returns indicator) |
| `GET /profile/{npub}` | User profile page |
| `GET /profile/edit` | Edit profile (requires login) |
| `GET /search` | Full-text search (NIP-50) |
| `GET /notifications` | Notifications (requires login) |
| `GET /mutes` | Muted users (requires login) |
| `GET /quote/{id}` | Quote post form |
| `GET /report/{id}` | Report form |
| `GET /compose` | Compose page (no-JS media fallback) |
| `GET /login` | Login page |
| `GET /logout` | Logout (requires login) |
| `GET /check-connection` | Check NIP-46 connection status |
| `GET /reconnect` | Reconnect NIP-46 session |
| `GET /wallet` | Wallet connection (requires login) |
| `GET /wallet/info` | Wallet balance fragment |
| `GET /gifs` | GIF picker panel |
| `GET /gifs/search` | Search GIFs |
| `GET /mentions` | @mention autocomplete dropdown (NIP-27) |
| `GET /mentions/select` | Select mention (OOB response) |
| `GET /fragment/author/{pubkey}` | Author info fragment (for async loading) |

**Legacy URLs:** Routes previously under `/html/*` are redirected to the new paths for backwards compatibility.

## Query Parameters

### Timeline (`/timeline`)

| Parameter | Type | Description |
|-----------|------|-------------|
| `feed` | string | Feed mode: `follows`, `global`, `me`, or DVM feed name |
| `kinds` | string | Comma-separated event kinds (e.g., `1,6` or `30023`) |
| `limit` | int | Number of events (default: 20) |
| `until` | int | Unix timestamp for pagination (fetch older) |
| `page` | int | Page number for DVM feeds (0-indexed) |
| `cache_only` | bool | Return empty on cache miss (no relay fetch) |
| `refresh` | bool | Force full page render (for config reload) |

**DVM Feeds:** When `feed` is a DVM-powered feed (configured in `navigation.json`), the timeline displays content from the DVM. DVM feeds show a header with the DVM's name, image, and description. Pagination uses `page` parameter instead of `until`.

### Thread (`/thread/{id}`)

| Parameter | Type | Description |
|-----------|------|-------------|
| `{id}` | string | Event ID (hex), `note1...`, `nevent1...`, or `naddr1...` |

Thread pages use stale-while-revalidate caching: cached threads render immediately while a background refresh updates the cache. New replies trigger a polling indicator.

### Thread Check New (`/thread/{id}/check-new`)

| Parameter | Type | Description |
|-----------|------|-------------|
| `{id}` | string | Event ID (hex), `note1...`, or `nevent1...` |
| `since` | int | Unix timestamp to check for replies after |
| `url` | string | URL to link to for refresh (default: thread URL) |

Returns a polling indicator div. If new replies exist since the given timestamp, includes a "N new replies" button that triggers a full thread refresh with proper threading.

### Profile (`/profile/{npub}`)

| Parameter | Type | Description |
|-----------|------|-------------|
| `{npub}` | string | `npub1...` or hex pubkey |

### Search (`/search`)

| Parameter | Type | Description |
|-----------|------|-------------|
| `q` | string | Search query |
| `kinds` | string | Filter by kinds |
| `limit` | int | Results per page (default: 20) |
| `until` | int | Pagination cursor |

### Notifications (`/notifications`)

| Parameter | Type | Description |
|-----------|------|-------------|
| `until` | int | Pagination cursor |
| `limit` | int | Results per page (default: 20) |

### Mentions (`/mentions`)

| Parameter | Type | Description |
|-----------|------|-------------|
| `target` | string | Textarea target ID: `post`, `reply`, or `quote` |
| `content` | string | Full textarea content (via `h-include`) |

Returns dropdown with matching profiles from user's follows list. Triggered when content ends with `@xxx` (3+ chars).

### Mentions Select (`/mentions/select`)

| Parameter | Type | Description |
|-----------|------|-------------|
| `target` | string | Textarea target ID |
| `query` | string | The `@query` to replace |
| `name` | string | Display name to insert |
| `pubkey` | string | Hex pubkey for mapping |

Returns OOB response that replaces `@query` with `@name` in textarea and merges pubkey into hidden mentions input.

## POST Actions

All POST endpoints require login and CSRF token.

| Endpoint | Description |
|----------|-------------|
| `POST /post` | Create note |
| `POST /reply` | Reply to note |
| `POST /react` | React to note |
| `POST /repost` | Repost note |
| `POST /zap` | Zap note (requires wallet) |
| `POST /bookmark` | Toggle bookmark |
| `POST /mute` | Toggle mute |
| `POST /report` | Report content (NIP-56) |
| `POST /follow` | Toggle follow |
| `POST /profile/edit` | Update profile |
| `POST /theme` | Toggle theme |
| `POST /wallet/connect` | Connect NWC wallet |
| `POST /wallet/disconnect` | Disconnect wallet |
| `POST /gifs/select` | Select GIF attachment |
| `POST /gifs/clear` | Clear GIF selection |
| `POST /gifs/close` | Close GIF picker |

## Request Body Formats

All bodies are `application/x-www-form-urlencoded`.

### Common Fields

All POST actions require these fields:

| Field | Required | Description |
|-------|----------|-------------|
| `csrf_token` | Yes | CSRF token (bound to session) |
| `return_url` | No | Redirect URL after success |

Action-specific fields are listed below.

### Post / Reply

| Field | Required | Description |
|-------|----------|-------------|
| `content` | Yes | Note content |
| `event_id` | Reply only | Event being replied to |
| `mentions` | No | JSON map of display name → pubkey for @mentions (NIP-27) |

On submit, `@displayname` in content is converted to `nostr:nprofile1...` and p-tags are added.

### Event Actions (by event_id)

These actions operate on an event identified by `event_id`:

| Action | Additional Fields |
|--------|-------------------|
| `/react` | `reaction` (optional) - emoji, defaults to config |
| `/repost` | None |
| `/bookmark` | None (toggles on/off) |

### Zap

| Field | Required | Description |
|-------|----------|-------------|
| `event_id` | Yes | Event being zapped |
| `event_pubkey` | Yes | Event author's pubkey |
| `amount` | Yes | Amount in sats |

### User Actions (by pubkey)

These actions operate on a user identified by `pubkey`:

| Action | Description |
|--------|-------------|
| `/follow` | Toggle follow/unfollow |
| `/mute` | Toggle mute/unmute |

### Report

Form page: `GET /report/{event_id}`

| Field | Required | Description |
|-------|----------|-------------|
| `event_id` | Yes | Event ID to report |
| `event_pubkey` | Yes | Author pubkey |
| `category` | Yes | `spam`, `impersonation`, `illegal`, `nudity`, `malware`, `profanity`, `other` |
| `content` | No | Additional details |
| `mute_user` | No | If `1`, also mute the author |

Publishes kind 1984 report event (NIP-56).

### Wallet Connect

| Field | Required | Description |
|-------|----------|-------------|
| `nwc_uri` | Yes | NWC connection URI (`nostr+walletconnect://...`) |

### Profile Edit

| Field | Required | Description |
|-------|----------|-------------|
| `display_name` | No | Display name |
| `about` | No | Bio/description |
| `picture` | No | Avatar URL |
| `banner` | No | Banner image URL |

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
          "href": "/reply",
          "fields": [
            {"name": "event_id", "type": "hidden", "value": "abc123..."},
            {"name": "csrf_token", "type": "hidden"}
          ]
        }
      ],
      "links": [
        {"rel": ["self"], "href": "/thread/abc123..."},
        {"rel": ["author"], "href": "/profile/npub1..."}
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
| `/stream/corrections` | No | Action correction notifications (e.g., updated reaction counts) |

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

1. User visits `/login`
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
