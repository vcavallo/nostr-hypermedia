# Deployment Guide

Production deployment, configuration, and infrastructure setup.

## Quick Deploy

```bash
# Extract release package
tar -xzf release.tar.gz
cd release

# Copy environment template and configure
cp .env.example .env

# Start server
./start.sh

# With options
./start.sh --dev              # Persistent keypair for NIP-46
./start.sh --redis            # Enable Redis caching
./start.sh --dev --redis      # Both
./start.sh --debug            # Verbose logging
```

## Build Release Package

```bash
# Build release.tar.gz (includes binary, config, static assets)
./build-release.sh

# Or keep release/ folder for inspection
./build-release.sh --no-gz
```

### Cross-compile for Linux

```bash
# AMD64
GOOS=linux GOARCH=amd64 go build -o release/nostr-server .

# ARM64
GOOS=linux GOARCH=arm64 go build -o release/nostr-server .
```

## Environment Variables

| Variable | Default | Description |
|----------|---------|-------------|
| `PORT` | 8080 | Server port |
| `LOG_LEVEL` | info | Log verbosity: `debug`, `info`, `warn`, `error` |
| `DEV_MODE` | - | Persistent keypair for development |
| `REDIS_URL` | - | Redis URL for distributed caching |
| `GIPHY_API_KEY` | - | Giphy API key to enable GIF picker |
| `CSRF_SECRET` | - | CSRF secret (auto-generated if not set, set explicitly in production) |
| `TRUSTED_PROXY_COUNT` | 0 | Number of trusted reverse proxies |
| `HSTS_ENABLED` | - | Enable HSTS header (HTTPS deployments only) |
| `GZIP_ENABLED` | 1 | Enable gzip compression (disable with `GZIP_ENABLED=0`) |

### Config Path Overrides

| Variable | Default | Description |
|----------|---------|-------------|
| `SITE_CONFIG` | config/site.json | Site identity and metadata |
| `NAVIGATION_CONFIG` | config/navigation.json | Feed tabs, nav, settings |
| `ACTIONS_CONFIG` | config/actions.json | Event actions |
| `RELAYS_CONFIG` | config/relays.json | Relay lists |
| `CLIENT_CONFIG` | config/client.json | NIP-89 client identification |
| `I18N_CONFIG_DIR` | config/i18n | Internationalization files |

## Configuration Files

All config files support hot-reload via `kill -HUP $(pgrep nostr-server)`. Connected browsers auto-refresh via SSE.

| File | Purpose |
|------|---------|
| `site.json` | Site identity, title format, Open Graph defaults |
| `navigation.json` | Feed tabs, utility nav, settings dropdown, kind filters |
| `actions.json` | Actions on events (reply, repost, react, zap, bookmark, mute) |
| `relays.json` | Relay URLs by purpose (default, search, publish, profile, NIP-46, NIP-89, write-only) |
| `client.json` | NIP-89 client identification (disabled by default) |
| `i18n/en.json` | Internationalization strings |

See [DEVELOPMENT.md](DEVELOPMENT.md) for complete configuration schemas.

## NIP-89 Client Identification

Add client identification tags to events created through your instance.

**1. Configure `config/client.json`:**

```json
{
  "enabled": true,
  "name": "My Nostr Client",
  "pubkey": "your-hex-pubkey",
  "dtag": "my-client",
  "relayHint": "wss://relay.example.com",
  "tagKinds": [1, 6, 7]
}
```

**2. Publish a kind 31990 handler event** (tells other clients about your app):

```bash
nak event --sec <your-private-key> -k 31990 \
  -t d=my-client -t k=1 -t k=6 -t k=7 \
  -t name="My Nostr Client" \
  -t web="https://my-instance.com/<bech32>=nevent" \
  -c "A hypermedia Nostr client" wss://relay.example.com
```

**3. Reload:** `kill -HUP $(pgrep nostr-server)`

Events will include: `["client", "31990:<pubkey>:<dtag>", "<relay-hint>"]`

## Reverse Proxy Setup

### Caddy (Recommended)

```caddyfile
yourdomain.com {
    reverse_proxy localhost:8080
}
```

With Caddy, set:
```bash
TRUSTED_PROXY_COUNT=1 HSTS_ENABLED=1 PORT=8080 ./nostr-server
```

### Behind Cloudflare

```bash
# Cloudflare → Caddy → App
TRUSTED_PROXY_COUNT=2 HSTS_ENABLED=1 PORT=8080 ./nostr-server
```

### nginx

```nginx
server {
    listen 443 ssl http2;
    server_name yourdomain.com;

    location / {
        proxy_pass http://127.0.0.1:8080;
        proxy_http_version 1.1;
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;

        # SSE support
        proxy_set_header Connection '';
        proxy_buffering off;
        proxy_cache off;
        chunked_transfer_encoding off;
    }
}
```

## Redis Setup

Enable Redis for distributed caching, session sharing, persistence, and reduced memory usage.

```bash
REDIS_URL=redis://localhost:6379 ./nostr-server
# Or: ./start.sh --redis
```

**Benefits:**
- Multi-instance session sharing
- Cache survives restarts
- Shared relay health data across instances
- Reduced server memory (~5MB vs ~200MB for caches)

**Requirements:** Redis 6.0+, ~500MB memory, low-latency connection

Without Redis, caching is in-memory and sessions are per-instance.

## Systemd Service

```ini
[Unit]
Description=Nostr Hypermedia Client
After=network.target

[Service]
Type=simple
User=www-data
WorkingDirectory=/path/to/nostr-server
EnvironmentFile=/path/to/nostr-server/.env
ExecStart=/path/to/nostr-server/nostr-server
ExecReload=/bin/kill -HUP $MAINPID
Restart=always
RestartSec=5

[Install]
WantedBy=multi-user.target
```

```bash
# Install and enable
sudo cp nostr-server.service /etc/systemd/system/
sudo systemctl daemon-reload
sudo systemctl enable nostr-server
sudo systemctl start nostr-server

# Hot-reload config
sudo systemctl reload nostr-server

# View logs
journalctl -u nostr-server -f
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

The `config/` directory is mounted as a volume—edit files locally and send SIGHUP to reload without rebuilding.

## Monitoring

| Endpoint | Description |
|----------|-------------|
| `/health` | Health check (503 if degraded) |
| `/health?verbose=1` | Health check with per-relay breakdown |
| `/health/live` | Liveness probe (always 200 if running) |
| `/health/ready` | Readiness probe (503 if no healthy relays) |
| `/metrics` | Prometheus metrics (process, HTTP, relays, cache) |
| `/debug/memstats` | Memory statistics (DEV_MODE only) |

### Health Check Examples

```bash
# Basic health
curl -s http://localhost:8080/health

# Verbose with relay status
curl -s http://localhost:8080/health?verbose=1

# Kubernetes-style probes
curl -s http://localhost:8080/health/live   # Liveness
curl -s http://localhost:8080/health/ready  # Readiness
```

## Verify Installation

```bash
# Test timeline
curl -s "http://localhost:8080/timeline?feed=global&limit=1"

# Test search
curl -s "http://localhost:8080/search?q=nostr"

# Health check
curl -s "http://localhost:8080/health"
```

## Security Checklist

- [ ] Set `CSRF_SECRET` explicitly (don't rely on auto-generation)
- [ ] Set `TRUSTED_PROXY_COUNT` correctly for your proxy chain
- [ ] Enable `HSTS_ENABLED=1` for HTTPS deployments
- [ ] Use Redis for multi-instance session sharing
- [ ] Run as non-root user (e.g., www-data)
- [ ] Keep config files readable only by the service user

## Capacity Planning

**Memory formula:** ~36 MB base + 0.32 MB per concurrent user

| Users | Without Redis | With Redis (server + redis) |
|-------|---------------|----------------------------|
| 100 | ~70 MB | ~60 MB + 20 MB |
| 500 | ~200 MB | ~160 MB + 80 MB |
| 1,000 | ~360 MB | ~290 MB + 150 MB |
| 2,000 | ~680 MB | ~540 MB + 275 MB |

**Bottlenecks:** Relay pool (150 max connections), Event cache (50 MB in-memory)

**Memory profiling (DEV_MODE):** `curl -s http://localhost:3000/debug/memstats | jq .`
