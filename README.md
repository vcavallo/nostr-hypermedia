# Nostr Hypermedia Server

HTTP-only Nostr client aggregator with REST and hypermedia support.

Maintains long-lived WebSocket connections to Nostr relays and exposes events over plain HTTP—no WebSockets needed on the client.

## Features

- **HTTP-only client interface** - No WebSocket handling required
- **Multi-relay aggregation** - Fan-out queries to multiple relays with deduplication
- **Two hypermedia UIs**:
  - **JavaScript Siren browser** - Generic client that discovers features from API responses
  - **Zero-JS HTML client** - Pure server-rendered HTML, works without JavaScript
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

Both clients follow the same hypermedia principles: links and actions are discovered from server responses, not hardcoded.

## API Endpoints

### `GET /timeline`

Fetch aggregated events from Nostr relays (JSON/Siren formats).

### `GET /html/timeline`

Fetch aggregated events as server-rendered HTML (zero-JS client).

**Query Parameters:**
- `relays` - Comma-separated relay URLs (default: relay.damus.io, relay.nostr.band)
- `authors` - Comma-separated pubkeys to filter by
- `kinds` - Comma-separated event kinds (e.g., `1` for notes, `7` for reactions)
- `limit` - Max events to return (default: 50, max: 200)
- `since` - Unix timestamp for oldest event
- `until` - Unix timestamp for newest event (used for pagination)

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
- `html_handlers.go` - Server-side HTML rendering
- `relay.go` - WebSocket client, fan-out, dedup, EOSE handling
- `siren.go` - Hypermedia (Siren) format conversion
- `html.go` - HTML template rendering

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

### Phase 2
- [ ] SSE endpoint for live updates (`/stream/timeline`)
- [ ] Thread expansion (`/threads/{id}`)
- [ ] Profile lookup with caching (`/profiles/{pubkey}`)
- [ ] Write support (POST `/events` with NIP-46 signing)
- [ ] Search endpoint (NIP-50)
- [ ] Relay health tracking and scoring
- [ ] Persistent storage (Redis/Postgres)

## Dependencies

- `github.com/gorilla/websocket` - WebSocket client for Nostr relays

## Environment Variables

- `PORT` - HTTP server port (default: 8080)

## License

MIT
