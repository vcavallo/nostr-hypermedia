# Nostr Hypermedia Client

A hypermedia-first Nostr web client implementing NATEOAS (Nostr As The Engine Of Application State). Server-rendered HTML with progressive enhancement via [HelmJS](https://github.com/Letdown2491/helmjs).

## Philosophy

**Hypermedia-Driven**: The server returns complete, self-describing HTML documents. The browser is a generic hypermedia renderer that follows links and submits forms—no client-side routing or state management.

**Zero-Trust Authentication**: NIP-46 remote signing means your private key never touches the server. Authentication happens entirely in your signer app (nsec.app, Amber).

**Progressive Enhancement**: Everything works without JavaScript. With JS enabled, HelmJS enhances interactions with partial page updates and smoother UX.

## Features

### Content & Discovery
- **Timeline feeds**: Follows and Global feeds with kind filters
- **DVM feeds**: Algorithmic/trending content via NIP-90 Data Vending Machines
- **Content types**: Notes, reposts, photos, videos, articles, highlights, comments, live streams, live chat, classifieds, calendar events, files, marketplace (stalls/products), communities, badges, repositories, labels, reports, app recommendations
- **Handler discovery**: NIP-89 "Open in app" links for unsupported event kinds, with web of trust filtering
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
- **Report content**: Flag abusive content (NIP-56 kind 1984)

### User Experience
- **Notifications**: Mentions, replies, reactions, reposts, zaps
- **Theme switching**: Light and dark mode
- **Profile editing**: Update display name, about, avatar, banner
- **Relay management**: Uses your NIP-65 relay list when logged in
- **NIP-05 verification**: Display verified identities with configurable badge

### Technical
- **Multi-relay aggregation**: Fan-out queries with deduplication
- **Relay health scoring**: Prioritizes faster, more reliable relays
- **Connection pooling**: Managed relay connections with limits and health tracking
- **Flexible caching**: In-memory or Redis backends for profiles, events, sessions, relay health, and more
- **Signature verification**: Validates all Nostr event signatures
- **Hot-reload config**: SIGHUP reloads all JSON config without restart

## Quick Start

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

### Production

See [DEPLOYMENT.md](docs/DEPLOYMENT.md) for production deployment, Docker, reverse proxy setup, and configuration reference.

## Authentication (NIP-46)

Login with a remote signer—your private key never leaves your device.

**Supported Signers:**
- [nsec.app](https://nsec.app) - Web-based remote signer
- [Amber](https://github.com/greenart7c3/Amber) - Android signer

**Login Options:**
1. Paste your `bunker://` URL from your signer
2. Scan the QR code or copy the `nostrconnect://` URI

Your private key never touches the server. Communication is NIP-44 encrypted via relay.

## Supported Event Kinds

Renders 30+ Nostr event kinds including notes, articles, photos, videos, live streams, highlights, classifieds, calendar events, marketplace listings, and more. See [API.md](docs/API.md#supported-event-kinds) for the complete list.

## Architecture

```
┌─────────┐                 ┌────────────────────────┐
│ Browser │────HTTP only────▶│     nostr-server      │
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

## Documentation

| Document | Contents |
|----------|----------|
| [DEPLOYMENT.md](docs/DEPLOYMENT.md) | Production deployment, Docker, reverse proxy, Redis, NIP-89 client setup |
| [API.md](docs/API.md) | All endpoints, query parameters, request/response formats |
| [DEVELOPMENT.md](docs/DEVELOPMENT.md) | Configuration schemas, adding features, extending the codebase |

## Quality Checks

Six static analysis tools validate accessibility, HATEOAS/NATEOAS compliance, markup, i18n, and security:

```bash
./cmd/run_checks.sh
```

Reports are saved to `reports/`. See [cmd/README.md](cmd/README.md) for details.

## License

MIT
