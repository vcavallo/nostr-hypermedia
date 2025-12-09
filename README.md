# Nostr Hypermedia Server

HTTP-only Nostr client aggregator with REST and hypermedia support.

Maintains long-lived WebSocket connections to Nostr relays and exposes events over plain HTTP—no WebSockets needed on the client.

## Features

- **HTTP-only client interface** - No WebSocket handling required
- **Multi-relay aggregation** - Fan-out queries to multiple relays with deduplication
- **Two hypermedia UIs**:
  - **JavaScript Siren browser** - Generic client that discovers features from API responses
  - **Zero-JS HTML client** - Pure server-rendered HTML, works without JavaScript
- **Zero-trust authentication** - NIP-46 remote signing (your keys never touch the server)
- **Thread views** - View notes with their replies
- **Profile pages** - View user profiles with follow/unfollow
- **Profile editing** - Update your display name, about, avatar, and banner
- **Notifications** - View mentions, replies, reactions, reposts, and zaps
- **Social actions** - React, reply, repost, quote, bookmark, and follow
- **Multiple content types** - Notes, photos, longform articles, highlights, and livestreams
- **Link previews** - Rich Open Graph previews for shared URLs
- **Theme switching** - Light and dark mode support
- **Profile enrichment** - Author names/pictures fetched and cached
- **Reactions & reply counts** - See engagement on notes
- **Multiple response formats** - JSON, Siren (HATEOAS), or HTML based on Accept header
- **Smart caching** - ETag/Last-Modified support for efficient refreshes
- **Signature verification** - Validates Nostr event signatures
- **Pagination** - Cursor-based pagination with `until` parameter

## Quick Start

```bash
go build -o nostr-server .
PORT=3000 ./nostr-server
```

**Open in browser:**
- JS client: http://localhost:3000/
- Zero-JS client: http://localhost:3000/html/timeline?kinds=1&limit=20

## User Interfaces

### JavaScript Siren Browser (`/`)

A **generic hypermedia client** that:
- Fetches Siren JSON and dynamically renders entities, links, and actions
- Has no hardcoded knowledge of Nostr—discovers everything from API responses
- Renders notes, pagination, profile/thread links all from hypermedia
- Would work with any Siren API (blog, todo app, etc.)

When you add new endpoints server-side (threads, profiles, search), the UI automatically exposes them by following links.

### Zero-JS HTML Client (`/html/timeline`)

A **pure HTML hypermedia client** that:
- Requires no JavaScript—works in Lynx, curl, ancient browsers
- Uses plain `<a>` tags for navigation and `<form>` tags for actions
- Server renders complete HTML pages
- True REST/HATEOAS over HTML—the original web architecture
- **NIP-46 authentication** - Login with remote signers (nsec.app, Amber)
- **Post notes** - Create and publish notes without JavaScript
- **Reply to threads** - Participate in conversations
- **Reactions** - React to notes with '+' button
- **Reposts & quotes** - Share notes with optional commentary
- **Bookmarks** - Save notes for later (kind 10003)
- **Follow/unfollow** - Manage your social graph
- **Profile editing** - Update display name, about, avatar, banner
- **Notifications** - View mentions, replies, reactions, reposts, zaps
- **Content filtering** - Filter by notes, photos, longform, highlights, livestreams
- **Theme switching** - Toggle between light and dark modes
- **Link previews** - Rich previews for shared URLs

Both clients follow the same hypermedia principles: links and actions are discovered from server responses, not hardcoded.

## Authentication (NIP-46)

The HTML client supports **zero-trust authentication** via NIP-46 (Nostr Connect). Your private key never leaves your signer app.

### How it works

**Option 1: Bunker URL**
1. Go to `/html/login`
2. Paste your `bunker://` URL from a remote signer:
   - [nsec.app](https://nsec.app) - Web-based remote signer
   - [Amber](https://github.com/greenart7c3/Amber) - Android signer
3. The server connects to your signer via relay
4. When you post, the server requests a signature from your signer
5. You approve/reject in your signer app

**Option 2: Nostr Connect (QR code flow)**
1. Go to `/html/login`
2. Copy the `nostrconnect://` URI or scan the QR code with your signer app
3. Approve the connection in your signer
4. The page auto-refreshes when connected

### Security model

- Server only sees your **public key**
- All signing happens in your signer app
- Communication is **NIP-44 encrypted** (ChaCha20 + HMAC-SHA256)
- Server uses a **disposable keypair** for each session
- Sessions stored server-side with HTTP-only cookies

## API Endpoints

### `GET /timeline`

Fetch aggregated events from Nostr relays (JSON/Siren formats).

### `GET /html/timeline`

Fetch aggregated events as server-rendered HTML (zero-JS client).

### `GET /html/thread/{eventId}`

View a note with its replies as server-rendered HTML.

### `GET /html/profile/{pubkey}`

View a user's profile and their notes. Accepts hex pubkey or `npub1...` format.

### `GET /html/login`

Login page for NIP-46 authentication. POST with `bunker_url` to connect.

### `GET /html/logout`

Logout and clear session.

### `POST /html/post`

Post a new note (requires login). Form field: `content`.

### `POST /html/reply`

Reply to a note (requires login). Form fields: `content`, `event_id`, `event_pubkey`.

### `POST /html/react`

React to a note (requires login). Form fields: `event_id`, `event_pubkey`, `return_url`.

### `POST /html/bookmark`

Bookmark a note (requires login). Form fields: `event_id`, `return_url`.

### `POST /html/repost`

Repost a note (requires login). Form fields: `event_id`, `event_pubkey`, `return_url`.

### `GET /html/quote/{eventId}`

Quote form for composing a quote post. Shows original note with compose area.

### `POST /html/follow`

Follow or unfollow a user (requires login). Form fields: `pubkey`, `action` (follow/unfollow), `return_url`.

### `GET /html/profile/edit`

Edit your profile (requires login). Form to update display name, about, avatar URL, and banner URL.

### `GET /html/notifications`

View your notifications (requires login). Shows mentions, replies, reactions, reposts, and zaps.

### `GET /html/theme`

Toggle between light and dark themes. Stores preference in cookie.

### `GET /html/check-connection`

Check NIP-46 connection status. Returns connection health info.

### `GET /html/reconnect`

Attempt to reconnect NIP-46 session if disconnected.

**Query Parameters (timeline):**
- `relays` - Comma-separated relay URLs (default uses user's NIP-65 relays if logged in, otherwise defaults)
- `authors` - Comma-separated pubkeys to filter by
- `kinds` - Comma-separated event kinds (e.g., `1` for notes, `7` for reactions)
- `limit` - Max events to return (default: 50, max: 200)
- `since` - Unix timestamp for oldest event
- `until` - Unix timestamp for newest event (used for pagination)
- `feed` - Feed mode: `follows` (notes from people you follow) or `global` (all notes). Defaults to `follows` when logged in.
- `fast` - Set to `1` to skip fetching reactions (faster loading)

**Examples:**

```bash
# Get latest 50 notes
curl "http://localhost:3000/timeline?kinds=1&limit=50"

# Filter by specific authors
curl "http://localhost:3000/timeline?authors=pub1,pub2&kinds=1"

# Pagination - use `until` from previous response
curl "http://localhost:3000/timeline?kinds=1&until=1759635730"

# Custom relays
curl "http://localhost:3000/timeline?relays=wss://relay.damus.io,wss://nos.lol&kinds=1"
```

### Response Formats

#### JSON (default)

```json
{
  "items": [
    {
      "id": "...",
      "kind": 1,
      "pubkey": "...",
      "created_at": 1759635732,
      "content": "hello nostr",
      "tags": [],
      "sig": "...",
      "relays_seen": ["wss://relay.damus.io"]
    }
  ],
  "page": {
    "until": 1759635730,
    "next": "/timeline?...&until=1759635730"
  },
  "meta": {
    "queried_relays": 2,
    "eose": true,
    "generated_at": "2025-10-04T22:00:00Z"
  }
}
```

#### Siren (Hypermedia)

Request with `Accept: application/vnd.siren+json`:

```bash
curl -H "Accept: application/vnd.siren+json" "http://localhost:3000/timeline?kinds=1&limit=2"
```

Returns Siren entities with:
- **Links** - Navigate to profiles, threads, pagination
- **Actions** - Discoverable operations (publish, react)
- **Properties** - Event data and metadata

Example structure:
```json
{
  "class": ["timeline"],
  "properties": { "title": "Nostr Timeline", ... },
  "entities": [
    {
      "class": ["event", "note"],
      "properties": { "id": "...", "content": "...", ... },
      "links": [
        { "rel": ["author"], "href": "/profiles/..." },
        { "rel": ["thread"], "href": "/threads/..." }
      ],
      "actions": [
        {
          "name": "react",
          "method": "POST",
          "href": "/actions/react",
          "fields": [...]
        }
      ]
    }
  ],
  "links": [
    { "rel": ["self"], "href": "/timeline?..." },
    { "rel": ["next"], "href": "/timeline?...&until=..." }
  ],
  "actions": [
    { "name": "publish", "method": "POST", "href": "/events", ... }
  ]
}
```

## Caching & Performance

The server sets HTTP cache headers:
- **ETag** - Hash of first/last event ID + count
- **Last-Modified** - Timestamp of most recent event
- **Cache-Control: max-age=5** - 5-second CDN/browser cache

Use `If-None-Match` with the ETag to get `304 Not Modified` when content hasn't changed:

```bash
curl -H 'If-None-Match: "4bff5e5ea3f03f38"' http://localhost:3000/timeline?kinds=1
```

## Architecture

```
┌─────────┐                 ┌──────────────┐
│ Browser │────HTTP only────▶│ Aggregator   │
│ Client  │◀────────────────│ Server       │
└─────────┘                 └──────┬───────┘
                                   │
                        ┌──────────┼──────────┐
                        │          │          │
                        ▼          ▼          ▼
                   ┌────────┐ ┌────────┐ ┌────────┐
                   │ Relay  │ │ Relay  │ │ Relay  │
                   │  WS    │ │  WS    │ │  WS    │
                   └────────┘ └────────┘ └────────┘
```

- **Client** makes simple HTTP GET requests
- **Server** maintains persistent WebSocket connections to relays
- **Fan-out** queries to multiple relays in parallel
- **Dedupe** by event ID, verify signatures
- **Order** by `(created_at DESC, id DESC)`
- **Cache** results with ETag for fast refreshes

## Project Structure

**Server:**
- `main.go` - HTTP server and routes
- `handlers.go` - Timeline endpoint and response building
- `html_handlers.go` - Server-side HTML rendering for timeline/threads/profiles/notifications
- `html_auth.go` - NIP-46 login/logout/post/reply/react/bookmark/repost/follow handlers
- `relay.go` - WebSocket client, fan-out, dedup, EOSE handling
- `siren.go` - Hypermedia (Siren) format conversion
- `html.go` - HTML template rendering with embedded CSS
- `nip46.go` - NIP-46 bunker client (remote signing)
- `nip44.go` - NIP-44 encryption (ChaCha20 + HMAC-SHA256)
- `nostrconnect.go` - Nostr Connect flow (`nostrconnect://` URI handling)
- `cache.go` - In-memory caching for events, contacts, profiles, relay lists, link previews
- `link_preview.go` - Open Graph metadata fetching for link previews
- `bech32.go` - Bech32 encoding/decoding (npub, naddr, etc.)

**Clients:**
- `static/index.html` - JS Siren browser entry point
- `static/app.js` - Generic Siren client (entity/link/action renderer)
- `static/style.css` - UI styling

## Next Steps

### Phase 1 (✅ Complete)
- [x] REST timeline endpoint
- [x] Multi-relay fan-out with WebSocket
- [x] Dedup and signature verification
- [x] ETag/Last-Modified caching
- [x] Siren hypermedia format
- [x] JavaScript Siren browser (generic client)
- [x] Zero-JS HTML client (server-rendered)

### Phase 2 (✅ Complete)
- [x] Thread views (`/html/thread/{id}`)
- [x] Profile enrichment with caching
- [x] Reactions and reply counts
- [x] NIP-46 remote signing (zero-trust auth)
- [x] NIP-44 encryption
- [x] Note posting via HTML forms

### Phase 3 (✅ Complete)
- [x] Profile pages (`/html/profile/{pubkey}`)
- [x] Reply to notes (thread participation)
- [x] Reactions via HTML forms
- [x] NIP-65 relay list support (use logged-in user's relays)
- [x] Follows/Global feed toggle
- [x] Contact list caching
- [x] Nostr Connect flow (QR code / `nostrconnect://` URI)
- [x] In-memory event caching for performance

### Phase 4 (In Progress)
- [x] Follow/unfollow users
- [x] Bookmarks (kind 10003)
- [x] Reposts (kind 6)
- [x] Quote posts (kind 1 with q tag)
- [x] Profile editing (kind 0)
- [x] Notifications page (mentions, replies, reactions, reposts, zaps)
- [x] Content type filtering (notes, photos, longform, highlights, livestreams)
- [x] Livestream support (kind 30311)
- [x] Theme switching (light/dark mode)
- [x] Link previews (Open Graph metadata)
- [x] Connection health monitoring
- [ ] SSE endpoint for live updates (`/stream/timeline`)
- [ ] Search endpoint (NIP-50)
- [ ] Relay health tracking and scoring
- [ ] Persistent storage (Redis/Postgres)

## Dependencies

- `github.com/gorilla/websocket` - WebSocket client for Nostr relays
- `github.com/btcsuite/btcd/btcec/v2` - secp256k1 elliptic curve (for NIP-46/NIP-44)
- `golang.org/x/crypto` - ChaCha20 and HKDF (for NIP-44 encryption)

## Environment Variables

- `PORT` - HTTP server port (default: 8080)
- `DEV_MODE` - Set to `1` to use a persistent server keypair for NIP-46 reconnection

## Deployment

### Build for Linux

```bash
# AMD64
GOOS=linux GOARCH=amd64 go build -o nostr-server .

# ARM64 (AWS Graviton, Raspberry Pi)
GOOS=linux GOARCH=arm64 go build -o nostr-server .
```

Copy the `nostr-server` binary and `static/` directory to your server.

### Run with Caddy

```caddyfile
yourdomain.com {
    reverse_proxy localhost:8080
}
```

```bash
DEV_MODE=1 PORT=8080 ./nostr-server
```

### Systemd Service

Create `/etc/systemd/system/nostr-server.service`:

```ini
[Unit]
Description=Nostr Hypermedia Server
After=network.target

[Service]
Type=simple
User=www-data
WorkingDirectory=/path/to/nostr-hypermedia
Environment=DEV_MODE=1
Environment=PORT=8080
ExecStart=/path/to/nostr-hypermedia/nostr-server
Restart=always
RestartSec=5

[Install]
WantedBy=multi-user.target
```

```bash
sudo systemctl daemon-reload
sudo systemctl enable nostr-server
sudo systemctl start nostr-server
```

## License

MIT
