# Nostr Hypermedia Client

A hypermedia-first Nostr web client implementing NATEOAS (Nostr As The Engine Of Application State). Server-rendered HTML with progressive enhancement via [HelmJS](https://github.com/Letdown2491/helmjs).

## Philosophy

**Hypermedia-Driven**: The server returns complete, self-describing HTML documents. The browser is a generic hypermedia renderer that follows links and submits forms—no client-side routing or state management.

**Zero-Trust Authentication**: NIP-46 remote signing means your private key never touches the server. Authentication happens entirely in your signer app (nsec.app, Amber).

**Progressive Enhancement**: Everything works without JavaScript. With JS enabled, HelmJS enhances interactions with partial page updates and smoother UX.

## Features

### Content & Discovery
- **Timeline feeds**: Follows, Global, and personal (Me) feeds
- **Content types**: Notes, photos, videos, articles, highlights, live streams, classifieds
- **Full-text search**: NIP-50 search across Nostr
- **Link previews**: Open Graph metadata for shared URLs
- **Thread views**: Notes with nested replies
- **Profile pages**: User profiles with follow/unfollow

### Social Actions
- **Post & reply**: Create notes and participate in threads
- **Reactions**: Like/react to notes
- **Reposts & quotes**: Share notes with optional commentary
- **Zaps**: Send Lightning payments via NIP-47 wallets (Alby, Primal, any NWC-compatible)
- **Bookmarks**: Save notes for later (kind 10003)
- **Follow/unfollow**: Manage your social graph
- **Mute users**: Hide content from specific users (kind 10000)

### User Experience
- **Notifications**: Mentions, replies, reactions, reposts, zaps
- **Theme switching**: Light and dark mode
- **Profile editing**: Update display name, about, avatar, banner
- **Relay management**: Uses your NIP-65 relay list when logged in

### Technical
- **Multi-relay aggregation**: Fan-out queries with deduplication
- **Relay health scoring**: Prioritizes faster, more reliable relays
- **Connection pooling**: Managed relay connections with limits (50 max) and health tracking
- **Event caching**: In-memory or Redis caching for profiles, contacts, events
- **Redis support**: Optional distributed caching for multi-instance deployments
- **Signature verification**: Validates all Nostr event signatures
- **Hot-reload config**: SIGHUP reloads all JSON config without restart, auto-refreshes connected browsers

## Quick Start

### Deploy

```bash
# Extract release package
tar -xzf release.tar.gz
cd release

# Copy environment template and add your secrets
cp .env.example .env

# Start server
./start.sh

# With options
./start.sh --dev              # Persistent keypair for NIP-46
./start.sh --redis            # Enable Redis caching
./start.sh --dev --redis      # Both
./start.sh --debug            # Verbose logging
./start.sh --help             # Show all options
```

### Development

```bash
# Build and run
go build && ./nostr-server

# With options
go build && DEV_MODE=1 ./nostr-server
go build && DEV_MODE=1 LOG_LEVEL=debug ./nostr-server
```

Open http://localhost:3000 in your browser.

**Hot reload configuration** (auto-refreshes connected browsers via SSE):
```bash
kill -HUP $(pgrep nostr-server)
```

## Docker

```bash
# Build and run (in-memory cache)
docker compose up --build

# With Redis caching
docker compose --profile redis up --build

# Run in background
docker compose up -d

# View logs
docker compose logs -f app

# Hot-reload config
docker compose kill -s HUP app

# Stop
docker compose down
```

Copy `.env.example` to `.env` for `GIPHY_API_KEY`, `CSRF_SECRET`, etc. The `config/` directory is mounted as a volume—edit files locally and send SIGHUP to reload without rebuilding.

## Authentication (NIP-46)

Login with a remote signer—your private key never leaves your device.

**Supported Signers:**
- [nsec.app](https://nsec.app) - Web-based remote signer
- [Amber](https://github.com/greenart7c3/Amber) - Android signer

**Login Options:**
1. Paste your `bunker://` URL from your signer
2. Scan the QR code or copy the `nostrconnect://` URI

Your private key never touches the server. Communication is NIP-44 encrypted via relay.

## Verify Installation

```bash
# Test timeline
curl -s "http://localhost:3000/html/timeline?feed=global&limit=1"

# Test search
curl -s "http://localhost:3000/html/search?q=nostr"

# Health check
curl -s "http://localhost:3000/health"
```

## Configuration

All config files in `config/` support hot-reload via `kill -HUP $(pgrep nostr-server)`. Connected browsers auto-refresh via SSE.

| File | Purpose |
|------|---------|
| `site.json` | Site identity, title format, Open Graph defaults |
| `navigation.json` | Feed tabs, utility nav, settings dropdown, kind filters |
| `actions.json` | Actions on events (reply, repost, react, zap, bookmark, mute) |
| `relays.json` | Relay URLs by purpose (default, search, publish, profile, NIP-46) |
| `i18n/en.json` | Internationalization strings |

See [DEVELOPMENT.md](DEVELOPMENT.md) for complete configuration schema and examples.

## Environment Variables

| Variable | Default | Description |
|----------|---------|-------------|
| `PORT` | 8080 | Server port |
| `LOG_LEVEL` | info | Log verbosity: `debug`, `info`, `warn`, `error` |
| `DEV_MODE` | - | Persistent keypair for development |
| `REDIS_URL` | - | Redis URL for distributed caching |
| `GIPHY_API_KEY` | - | Giphy API key to enable GIF picker |
| `CSRF_SECRET` | - | CSRF secret (auto-generated if not set, set explicitly in production) |
| `TRUSTED_PROXY_COUNT` | 0 | Number of trusted reverse proxies for rate limiting |
| `HSTS_ENABLED` | - | Enable HSTS header (HTTPS deployments only) |

See [DEVELOPMENT.md](DEVELOPMENT.md) for all environment variables including config paths and experimental features.

## Monitoring

| Endpoint | Description |
|----------|-------------|
| `/health` | Health check (503 if degraded) |
| `/health?verbose=1` | Health check with per-relay breakdown |
| `/health/live` | Liveness probe (always 200 if running) |
| `/health/ready` | Readiness probe (503 if no healthy relays) |
| `/metrics` | Prometheus metrics (process, HTTP, relays, cache) |

## Supported Nostr Event Kinds

| Kind | Type | Description |
|------|------|-------------|
| 0 | Profile | User metadata |
| 1 | Note | Short text note |
| 3 | Contacts | Follow list |
| 6 | Repost | Shared note |
| 7 | Reaction | Like/reaction |
| 20 | Photo | Image post (NIP-94) |
| 22 | Video | Short-form video (NIP-71) |
| 9735 | Zap | Lightning payment receipt |
| 9802 | Highlight | Text highlight |
| 10000 | Mutes | Muted users/content |
| 10002 | Relays | Relay list (NIP-65) |
| 10003 | Bookmarks | Saved notes |
| 30023 | Article | Long-form content |
| 30311 | Live | Live streaming event |
| 30402 | Classified | Marketplace listing (NIP-99) |

## Deployment

### Build Release Package

```bash
# Build release.tar.gz (includes binary, config, static assets)
./build-release.sh

# Or keep release/ folder for inspection
./build-release.sh --no-gz
```

The release package is self-contained and ready to deploy.

### Cross-compile for Linux

```bash
# AMD64
GOOS=linux GOARCH=amd64 go build -o release/nostr-server .

# ARM64
GOOS=linux GOARCH=arm64 go build -o release/nostr-server .
```

### Production Security Settings

```bash
# Behind Caddy with HTTPS
TRUSTED_PROXY_COUNT=1 HSTS_ENABLED=1 PORT=8080 ./nostr-server

# Behind Cloudflare → Caddy
TRUSTED_PROXY_COUNT=2 HSTS_ENABLED=1 PORT=8080 ./nostr-server
```

### Systemd Service

```ini
[Unit]
Description=Nostr-hypermedia Client
After=network.target

[Service]
Type=simple
User=www-data
WorkingDirectory=/path/to/nostr-hypermedia
Environment=DEV_MODE=1
Environment=PORT=8080
ExecStart=/path/to/nostr-hypermedia/nostr-server
ExecReload=/bin/kill -HUP $MAINPID
Restart=always

[Install]
WantedBy=multi-user.target
```

### Caddy Reverse Proxy

```caddyfile
yourdomain.com {
    reverse_proxy localhost:8080
}
```

## Development

See [API.md](API.md) for:
- All endpoints and query parameters
- Request/response formats
- Authentication and CSRF
- Rate limits and status codes

See [DEVELOPMENT.md](DEVELOPMENT.md) for:
- Configuration schema (all JSON config options)
- Adding actions, event kinds, and pages
- Security implementation details
- Caching system architecture

## Quality Checks

Six static analysis tools in `cmd/` validate accessibility, HATEOAS/NATEOAS compliance, markup, i18n, and security. Run all at once:

```bash
./cmd/run_checks.sh
```

Reports are saved to `reports/`. See [DEVELOPMENT.md](DEVELOPMENT.md) for details on each tool.

## Architecture

```
┌─────────┐                 ┌────────────────────────┐
│ Browser │────HTTP only────▶│   nostr-hypermedia    │
│         │◀────────────────│        Server          │
└─────────┘                 └──────────┬─────────────┘
                                       │
                            ┌──────────┼──────────┐
                            │          │          │
                            ▼          ▼          ▼
                       ┌────────┐ ┌────────┐ ┌────────┐
                       │ Relay  │ │ Relay  │ │ Relay  │
                       │  WS    │ │  WS    │ │  WS    │
                       └────────┘ └────────┘ └────────┘
```

- **Client** makes simple HTTP requests
- **Server** maintains WebSocket connections to relays
- **Fan-out** queries to multiple relays in parallel
- **Dedupe** by event ID, verify signatures
- **Render** as complete HTML with embedded controls

## License

MIT
