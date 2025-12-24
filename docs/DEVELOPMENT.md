# Development Guide

Guide for extending nostr-server with new actions, event kinds, pages, and navigation.

## NATEOAS: Nostr As The Engine Of Application State

This project follows NATEOAS principles, a hypermedia-driven approach where the server provides all available actions and navigation for the current state. The client doesn't construct URLs; it just follows what the server provides.

### Core Principles

1. **Server-driven navigation**: Every response includes all available links/actions
2. **No hardcoded URLs in templates**: Templates iterate over server-provided data
3. **Context-sensitive actions**: Links appear based on login state, page, permissions
4. **Configuration over code**: Navigation in config files, code handles filtering

## Project Structure

```
├── Dockerfile             # Multi-stage build (golang:alpine → alpine runtime)
├── docker-compose.yml     # Container orchestration (app + optional Redis)
├── .dockerignore          # Files excluded from Docker build context
├── build-release.sh       # Build release package (release.tar.gz)
├── build-css.sh           # Dev CSS build (concatenates modules, optional watch mode)
├── .env.example           # Environment template (copy to .env)
├── main.go                # HTTP server, routes, SIGHUP handler
├── logging.go             # Structured logging (slog, JSON, LOG_LEVEL env var)
├── metrics.go             # Prometheus metrics (/metrics endpoint)
├── html_handlers.go       # Page handlers (timeline, thread, profile, search, notifications, mutes)
├── html_auth.go           # Action handlers (post, reply, react, repost, quote, bookmark, mute, follow)
├── html.go                # Template compilation and rendering orchestration
├── handlers.go            # JSON API response handlers (TimelineResponse, EventItem, Siren support)
├── relay.go               # WebSocket relay client, event fetching
├── relay_pool.go          # Connection pooling, health tracking, per-relay rate limiting
├── actions.go             # Action system core (GetActionsForEvent)
├── actions_config.go      # JSON config loader with hot-reload
├── actions_registry.go    # Programmatic action registration
├── kinds.go               # Event kind definitions, registry, behavioral flags
├── kinds_appliers.go      # Kind-specific data appliers (zap, live, highlight, video, etc.)
├── navigation.go          # Navigation helpers (FeedMode, KindFilter, NavItem, SettingsItem)
├── navigation_config.go   # Unified navigation config loader
├── flash.go               # Flash message cookies (success/error messages across redirects)
├── nip46.go               # NIP-46 remote signing
├── nostrconnect.go        # Nostr Connect flow (nostrconnect:// URI)
├── nostr_action_tags.go   # Parsing action definitions from Nostr event tags
├── nostr_kind_fetcher.go  # Metadata fetching from Nostr kind 39001 events (experimental)
├── csrf.go                # CSRF token handling
├── cache.go               # Cache wrappers and initialization (supports Redis or in-memory)
├── cache_interface.go     # CacheBackend, SessionStore, PendingConnStore interfaces
├── cache_memory.go        # In-memory cache implementations
├── cache_redis.go         # Redis cache implementations (optional)
├── singleflight.go        # Request deduplication for identical concurrent requests
├── batcher.go             # Request coalescing for overlapping concurrent requests
├── subscription_aggregator.go  # Background subscriptions for cache warming
├── link_preview.go        # Open Graph metadata fetching
├── lnurl.go               # LNURL-pay handling for Lightning payments
├── nwc.go                 # NWC client (NIP-47 Nostr Wallet Connect)
├── sse.go                 # Server-sent events for live updates
├── siren.go               # Siren hypermedia format (JSON API)
├── giphy.go               # Giphy API client for GIF picker
├── dvm.go                 # NIP-90 DVM client (requests, responses, metadata)
├── nip05.go               # NIP-05 identity verification with caching
├── helpers.go             # Domain-specific helpers (outbox relays, mentions, event processing)
├── cookies.go             # Cookie helpers with security defaults
│
├── internal/              # Internal packages (not for external import)
│   ├── auth/
│   │   └── csrf.go        # CSRF utilities
│   ├── cache/
│   │   ├── backend.go     # Cache backend interface
│   │   ├── config.go      # Cache configuration
│   │   └── memory.go      # In-memory cache implementation
│   ├── config/
│   │   ├── i18n.go        # Internationalization strings loader
│   │   ├── relays.go      # Relay list config loader
│   │   ├── site.go        # Site configuration loader (site.json)
│   │   └── client.go      # NIP-89 client identification config
│   ├── nips/
│   │   ├── bech32.go      # Bech32 encoding utilities
│   │   ├── nip19.go       # NIP-19 bech32 encoding (npub, naddr, nevent, nprofile)
│   │   └── nip44.go       # NIP-44 encryption (ChaCha20 + HMAC-SHA256)
│   ├── nostr/
│   │   ├── event.go       # Nostr event utilities
│   │   └── url.go         # Nostr URL parsing
│   ├── services/
│   │   ├── giphy.go       # Giphy service helpers
│   │   └── lnurl.go       # LNURL service helpers
│   ├── types/
│   │   ├── cache.go       # Cache type definitions
│   │   ├── event.go       # Event type definitions
│   │   ├── notification.go # Notification type definitions
│   │   ├── profile.go     # Profile type definitions
│   │   ├── relay.go       # Relay type definitions
│   │   └── services.go    # Service type definitions
│   └── util/
│       ├── constants.go   # External service URLs and constants
│       ├── helpers.go     # Reusable helper functions (tags, maps, filtering, timeouts, URL building)
│       └── http.go        # HTTP response helpers (errors, headers)
│
├── templates/             # Go template files
│   ├── base.go            # Base layout, CSS, navigation, settings dropdown
│   ├── timeline.go        # Timeline/feed content
│   ├── thread.go          # Thread view with replies
│   ├── profile.go         # Profile page with edit mode
│   ├── notifications.go   # Notifications list
│   ├── search.go          # Search page
│   ├── quote.go           # Quote form
│   ├── report.go          # Report form (NIP-56)
│   ├── login.go           # Login page (NIP-46/Nostr Connect)
│   ├── mutes.go           # Muted users list
│   ├── fragment.go        # Reusable fragments (author-header, note-footer, etc.)
│   ├── gifs.go            # GIF picker panel and search results
│   ├── mentions.go        # @mention autocomplete dropdown (NIP-27)
│   ├── compose.go         # Compose page (no-JS fallback for media attachments)
│   ├── wallet.go          # Wallet page (NWC connection, balance display)
│   └── kinds/             # Kind-specific templates (one per event type)
│       ├── dispatcher.go  # Routes events to correct template
│       ├── partials.go    # Shared partial templates across kinds
│       ├── note.go        # Kind 1 text notes
│       ├── repost.go      # Kind 6, 16 reposts
│       ├── picture.go     # Kind 20 photos
│       ├── video.go       # Kind 30 videos
│       ├── shortvideo.go  # Kind 22 short videos
│       ├── zap.go         # Kind 9735 zap receipts
│       ├── highlight.go   # Kind 9802 highlights
│       ├── comment.go     # Kind 1111 comments
│       ├── bookmarks.go   # Kind 10003 bookmark lists
│       ├── longform.go    # Kind 30023 articles
│       ├── livestream.go  # Kind 30311 live events
│       ├── livechat.go    # Kind 1311 live chat
│       ├── calendar.go    # Kind 31922/31923 calendar events
│       ├── rsvp.go        # Kind 31925 calendar RSVPs
│       ├── classified.go  # Kind 30402 classifieds
│       ├── file.go        # Kind 1063 file metadata
│       ├── marketplace.go # Kind 30017/30018 stalls/products
│       ├── status.go      # Kind 30315 user status
│       ├── community.go   # Kind 34550 communities
│       ├── badge.go       # Kind 30009/8 badges
│       ├── repository.go  # Kind 30617 git repos
│       ├── label.go       # Kind 1985 labels
│       ├── report.go      # Kind 1984 reports
│       ├── recommendation.go # Kind 31989 app recommendations
│       ├── handler.go     # Kind 31990 app handlers
│       └── default.go     # Fallback for unknown kinds
│
├── static/                # Static assets (source files, .gz generated at release)
│   ├── style.css          # Concatenated stylesheet (built from css/ modules)
│   ├── css/               # Modular CSS source files
│   │   ├── base.css       # Reset, CSS variables, typography
│   │   ├── layout.css     # Page layout, header, navigation
│   │   ├── notes.css      # Note card base styles
│   │   ├── kinds/         # Per-kind CSS (matches templates/kinds/)
│   │   │   ├── picture.css, repost.css, zap.css, livestream.css,
│   │   │   ├── highlight.css, comment.css, calendar.css, video.css,
│   │   │   ├── bookmarks.css, longform.css, classified.css,
│   │   │   ├── shortvideo.css, file.css, marketplace.css,
│   │   │   ├── status.css, community.css, badge.css, repository.css,
│   │   │   ├── label.css, report.css, livechat.css, rsvp.css,
│   │   │   ├── handler.css, recommendation.css, default.css
│   │   ├── components.css # Action pills, dropdowns, utilities
│   │   └── pages.css      # Profile, search, notifications, wallet pages
│   ├── helm.js            # HelmJS (external) - github.com/Letdown2491/helmjs
│   ├── avatar.jpg         # Default avatar image
│   ├── favicon.ico        # Favicon
│   └── og-image.png       # Open Graph default image
│
├── config/                # JSON configuration (hot-reloadable via SIGHUP)
│   ├── site.json          # Site identity, title format, Open Graph, scripts
│   ├── actions.json       # Action definitions
│   ├── navigation.json    # Feeds, utility nav, kind filters
│   ├── relays.json        # Relay URLs by category (default, search, publish, profile, NIP-46, NIP-89, write-only)
│   ├── client.json        # NIP-89 client identification (disabled by default)
│   └── i18n/
│       └── en.json        # English strings
│
├── cmd/                   # Quality check tools
│   ├── run_checks.sh          # Run all checks at once
│   ├── accessibility-check/   # WCAG 2.1 validator
│   ├── nateoas-check/         # NATEOAS compliance
│   ├── hateoas-check/         # HATEOAS compliance
│   ├── markup-check/          # HTML validation
│   ├── i18n-check/            # i18n coverage
│   └── security-check/        # Security analysis
│
└── reports/               # Generated HTML reports (gitignored)
```

For API endpoints, request/response formats, and integration details, see [API.md](API.md).

## Adding New Actions

Actions are buttons/links shown on events (reply, repost, react, bookmark, mute).

### Option A: JSON Configuration (Simple)

Best for standard actions. Supports hot-reload.

**1. Add to `config/actions.json`:**

```json
{
  "actions": {
    "zap": {
      "method": "POST",
      "href": "/zap",
      "class": "action-zap",
      "icon": "⚡",
      "rel": "payment",
      "appliesTo": [1, 20, 30023],
      "fields": ["csrf_token", "event_id", "event_pubkey", "amount", "return_url"]
    }
  },
  "displayOrder": ["reply", "repost", "quote", "react", "zap", "bookmark", "mute"]
}
```

**Action fields:**

| Field | Type | Description |
|-------|------|-------------|
| `titleKey` | string | i18n key (defaults to `action.{name}`) |
| `method` | string | "GET" (link) or "POST" (form) |
| `href` | string | URL path with `{event_id}` placeholder |
| `class` | string | CSS class for styling |
| `icon` | string | Optional emoji icon |
| `iconOnly` | string | Icon display mode (see iconOnly Values below) |
| `rel` | string | Link relation for semantic meaning |
| `appliesTo` | int[] | Nostr event kinds this action applies to |
| `fields` | string[] | Form fields for POST actions |
| `hasCount` | bool | Show count next to action (reply count, reaction count) |
| `toggleable` | bool | Can toggle off on re-click (bookmark, mute) |
| `groupWith` | string | Appears in another action's dropdown menu |
| `requiresWallet` | bool | Requires wallet connection (zap action) |
| `amounts` | int[] | Preset amounts for zap dropdown in sats (e.g., `[21, 69, 420]`) |

**2. Create handler in `html_auth.go`:**

```go
func htmlZapHandler(w http.ResponseWriter, r *http.Request) {
    if r.Method != http.MethodPost {
        http.Redirect(w, r, DefaultTimelineURL(), http.StatusSeeOther)
        return
    }

    session := requireAuth(w, r)
    if session == nil {
        return
    }

    csrfToken := r.FormValue("csrf_token")
    if !requireCSRF(w, session.ID, csrfToken) {
        return
    }

    eventID := strings.TrimSpace(r.FormValue("event_id"))
    eventPubkey := strings.TrimSpace(r.FormValue("event_pubkey"))
    amount := strings.TrimSpace(r.FormValue("amount"))
    returnURL := sanitizeReturnURL(r.FormValue("return_url"), true) // logged in (requireAuth passed)

    if eventID == "" || !isValidEventID(eventID) {
        redirectWithError(w, r, returnURL, "Invalid event ID")
        return
    }

    // Sign event via bunker
    ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
    defer cancel()

    signedEvent, err := session.SignEvent(ctx, event)
    if err != nil {
        log.Printf("Failed to sign zap: %v", err)
        if isHelmRequest(r) {
            http.Error(w, sanitizeErrorForUser("Sign event", err), http.StatusInternalServerError)
            return
        }
        redirectWithError(w, r, returnURL, sanitizeErrorForUser("Sign event", err))
        return
    }

    // Publish to relays...

    // For HelmJS requests, return updated fragment
    if isHelmRequest(r) {
        // Return HTML fragment for partial update
        w.Header().Set("Content-Type", "text/html; charset=utf-8")
        w.Write([]byte(html))
        return
    }

    redirectWithSuccess(w, r, returnURL, "Zap sent")
}
```

**3. Register route in `main.go`:**

```go
http.HandleFunc("/zap", securityHeaders(limitBody(htmlZapHandler, maxBodySize)))
```

**4. Reload:**

```bash
kill -HUP $(pgrep nostr-server)
```

### Option B: Programmatic Registration (Complex)

Best for actions with dynamic titles, conditional fields, or custom logic.

**1. Create feature file (e.g., `zap.go`):**

```go
package main

func init() {
    RegisterAction(RegisteredAction{
        Name: "zap",
        Config: ActionConfig{
            Method:    "POST",
            Href:      "/zap",
            Class:     "action-zap",
            Icon:      "⚡",
            AppliesTo: []int{1, 20, 30023},
        },
        Builder:       buildZapAction,
        RequiresLogin: true,
        Priority:      50,
    })
}

func buildZapAction(ctx ActionContext) ActionDefinition {
    return ActionDefinition{
        Name:   "zap",
        Title:  "Zap",
        Method: "POST",
        Href:   "/zap",
        Class:  "action-zap",
        Icon:   "⚡",
        Fields: []FieldDefinition{
            {Name: "csrf_token", Type: "hidden", Value: ctx.CSRFToken},
            {Name: "event_id", Type: "hidden", Value: ctx.EventID},
            {Name: "event_pubkey", Type: "hidden", Value: ctx.EventPubkey},
            {Name: "return_url", Type: "hidden", Value: ctx.ReturnURL},
        },
    }
}
```

### When to Use Each Approach

| JSON Config | Programmatic Registration |
|-------------|---------------------------|
| Simple title, href, fields | Dynamic titles based on context |
| Standard form submission | Conditional fields |
| Hot-reload needed | Custom `ActionBuilder` logic |
| Deployment-time changes | Compile-time features |

## Adding New Event Kinds

To support a new Nostr event kind:

**1. Add kind definition in `kinds.go`:**

```go
var KindRegistry = map[int]*KindDefinition{
    // ... existing kinds
    9802: {
        Kind:           9802,
        Name:           "highlight",
        LabelKey:       "kind.highlight.label",
        TemplateName:   "highlight",
        ShowInTimeline: true,
        ShowReactions:  true,
    },
}
```

**KindDefinition fields:**

| Field | Description |
|-------|-------------|
| `Kind` | Nostr event kind number |
| `Name` | Machine name for CSS classes |
| `LabelKey` | i18n key for human-readable label |
| `TemplateName` | Template block name for rendering |
| `ExtractTitle`, `ExtractSummary`, `ExtractImages` | Content extraction hints |
| `IsRepost`, `IsAddressable`, `IsReplaceable` | Protocol behavior flags |
| `SkipContent`, `RenderMarkdown` | Rendering hints |
| `ShowInTimeline`, `ShowReplyCount` | UI flags |
| `RequiredTags`, `RequireAnyTag` | Tag validation |

**2. Add i18n label in `config/i18n/en.json`:**

```json
{
  "kind.highlight.label": "Highlight"
}
```

**3. (Optional) Add data applier in `kinds_appliers.go`:**

If your kind has structured tag data:

```go
func init() {
    RegisterKindDataApplier(9802, applyHighlightData)
}

func applyHighlightData(item *HTMLEventItem, tags [][]string, ctx *KindProcessingContext) {
    for _, tag := range tags {
        if len(tag) >= 2 && tag[0] == "context" {
            item.HighlightContext = tag[1]
        }
    }
}
```

**4. Create template file in `templates/kinds/{name}.go`:**

```go
package kinds

// HighlightTemplate renders kind 9802 highlights
const HighlightTemplate = `
{{define "render-highlight"}}
<div class="highlight">
  <blockquote class="highlight-blockquote">{{.Content}}</blockquote>
  {{if .HighlightContext}}
  <div class="highlight-context">{{.HighlightContext}}</div>
  {{end}}
</div>
{{end}}
`
```

**5. Add to dispatcher in `templates/kinds/dispatcher.go`:**

```go
// Add to the if/else chain:
{{else if eq .RenderTemplate "render-highlight"}}{{template "render-highlight" .}}

// Add to GetAllTemplates():
HighlightTemplate +
```

**6. Create CSS in `static/css/kinds/{name}.css`:**

```css
/* Highlight styles (kind 9802) */
.highlight { padding: 16px 20px; }
.highlight-blockquote {
  border-left: 4px solid var(--accent);
  padding-left: 16px;
  margin: 0;
  font-style: italic;
}
.highlight-context {
  margin-top: 8px;
  font-size: 13px;
  color: var(--text-muted);
}
```

**7. Rebuild CSS and test:**

```bash
./build-css.sh
go build && DEV_MODE=1 ./nostr-server
```

**8. Configure actions in `config/actions.json`:**

```json
{
  "actions": {
    "react": {
      "appliesTo": [1, 6, 20, 9735, 30311, 9802]
    }
  }
}
```

Or use kind overrides:

```json
{
  "kindOverrides": {
    "9802": {
      "actions": ["react", "bookmark"]
    }
  }
}
```

### Actions Configuration (`config/actions.json`)

Complete structure with all top-level fields:

```json
{
  "actions": {
    "reply": { ... },
    "repost": { ... }
  },
  "displayOrder": ["reply", "repost", "zap", "react", "quote", "bookmark", "mute"],
  "kindOverrides": {
    "30023": {
      "actions": ["read", "repost", "zap", "react", "quote", "bookmark"],
      "comment": "Articles show read action instead of reply"
    }
  },
  "fieldDefaults": {
    "reaction": "❤️"
  }
}
```

| Field | Type | Description |
|-------|------|-------------|
| `actions` | object | Map of action name to ActionConfig |
| `displayOrder` | string[] | Order actions appear in UI (excludes grouped actions) |
| `kindOverrides` | object | Kind-specific action lists (key is kind number as string) |
| `kindOverrides[kind].actions` | string[] | Actions to show for this kind |
| `kindOverrides[kind].comment` | string | Optional documentation comment |
| `fieldDefaults` | object | Default values for form fields (e.g., default reaction emoji) |

### Relays Configuration (`config/relays.json`)

```json
{
  "defaultRelays": [
    "wss://relay.damus.io",
    "wss://relay.nostr.band",
    "wss://nos.lol"
  ],
  "searchRelays": [
    "wss://relay.nostr.band"
  ],
  "publishRelays": [
    "wss://relay.damus.io",
    "wss://relay.nostr.band"
  ],
  "profileRelays": [
    "wss://relay.nostr.band"
  ],
  "nostrConnectRelays": [
    "wss://relay.nsec.app",
    "wss://relay.damus.io"
  ],
  "handlerRelays": [
    "wss://relay.nostr.band"
  ],
  "blossomServers": [
    "https://blossom.primal.net",
    "https://blossom.nostr.build",
    "https://cdn.satellite.earth"
  ],
  "writeOnlyRelays": [
    "wss://sendit.nosflare.com",
    "wss://nostr.mutinywallet.com"
  ]
}
```

| Field | Type | Description |
|-------|------|-------------|
| `defaultRelays` | string[] | Relays for general event fetching (timeline, thread) |
| `searchRelays` | string[] | Relays supporting NIP-50 full-text search |
| `publishRelays` | string[] | Relays to publish user events to |
| `profileRelays` | string[] | Relays for profile metadata lookups (kind 0) |
| `nostrConnectRelays` | string[] | Relays for NIP-46 remote signing communication |
| `handlerRelays` | string[] | Relays for NIP-89 handler discovery (31990 events) |
| `blossomServers` | string[] | Blossom servers for BUD-10 URI resolution (first is default) |
| `writeOnlyRelays` | string[] | Relays that only accept EVENT but not REQ; filtered from outbox read queries. Also detected dynamically from NOTICE messages. |

## Adding New Pages

**1. Create template file `templates/newpage.go`:**

```go
package templates

func GetNewpageTemplate() string {
    return newpageContent
}

var newpageContent = `{{define "content"}}
<div class="newpage">
  <h1>{{.Title}}</h1>
  {{range .Items}}
  <article class="item">{{.Content}}</article>
  {{end}}
</div>
{{if not .Items}}
<div class="empty-state">
  <p>{{i18n "msg.no_items"}}</p>
</div>
{{end}}
{{end}}`
```

**2. Create data struct in `html.go`:**

```go
type HTMLNewpageData struct {
    Title                  string
    ThemeClass             string
    ThemeLabel             string
    LoggedIn               bool
    UserDisplayName        string
    UserPubKey             string
    Items                  []NewpageItem
    CurrentURL             string
    CSRFToken              string
    FeedModes              []FeedMode
    KindFilters            []KindFilter
    NavItems               []NavItem
    SettingsItems          []SettingsItem
    ShowPostForm           bool
    HasUnreadNotifications bool
}
```

**3. Create render function:**

```go
func renderNewpageHTML(w http.ResponseWriter, data HTMLNewpageData) error {
    data.FeedModes = GetFeedModes(FeedModeContext{
        LoggedIn:   data.LoggedIn,
        ActiveFeed: "",
    })
    data.KindFilters = GetKindFilters(KindFilterContext{
        LoggedIn:   data.LoggedIn,
        ActiveFeed: "",
        ActivePage: "newpage",
    })
    data.NavItems = GetNavItems(NavContext{
        LoggedIn:   data.LoggedIn,
        ActivePage: "newpage",
    })
    data.SettingsItems = GetSettingsItems(SettingsContext{
        LoggedIn:   data.LoggedIn,
        ThemeLabel: data.ThemeLabel,
    })
    return newpageTemplate.Execute(w, data)
}
```

**4. Add handler in `html_handlers.go`:**

```go
func htmlNewpageHandler(w http.ResponseWriter, r *http.Request) {
    session := getSessionFromRequest(r)
    loggedIn := session != nil && session.Connected

    themeClass, themeLabel := getThemeFromRequest(r)

    data := HTMLNewpageData{
        Title:      "New Page",
        ThemeClass: themeClass,
        ThemeLabel: themeLabel,
        LoggedIn:   loggedIn,
        CurrentURL: r.URL.String(),
    }

    if loggedIn {
        userPubkey := hex.EncodeToString(session.UserPubKey)
        data.UserDisplayName = getUserDisplayName(userPubkey)
        data.CSRFToken = generateCSRFToken(session.ID)
    }

    // Fetch items...
    data.Items = fetchItems()

    renderNewpageHTML(w, data)
}
```

**5. Register template in `html.go` init:**

```go
newpageTemplate = template.Must(template.New("newpage").Funcs(templateFuncs).Parse(
    templates.GetBaseTemplates() + templates.GetNewpageTemplate(),
))
```

**6. Register route in `main.go`:**

```go
http.HandleFunc("/newpage", securityHeaders(htmlNewpageHandler))
```

## Navigation Configuration

All navigation is in `config/navigation.json`:

```json
{
  "feeds": [
    { "name": "follows", "requiresLogin": true },
    { "name": "global" },
    { "name": "me", "requiresLogin": true }
  ],
  "utility": [
    { "name": "search", "href": "/search", "icon": "🔎︎", "iconOnly": "always" },
    { "name": "notifications", "href": "/notifications", "requiresLogin": true, "icon": "🔔", "iconOnly": "always" }
  ],
  "settings": [
    { "name": "relays", "dynamic": "relays", "requiresLogout": true },
    { "name": "edit_profile", "href": "/profile/edit", "requiresLogin": true },
    { "name": "bookmarks", "kinds": [10003], "feed": "me", "requiresLogin": true },
    { "name": "highlights", "kinds": [9802], "feed": "me", "requiresLogin": true },
    { "name": "mutes", "href": "/mutes", "requiresLogin": true },
    { "name": "logout", "href": "/logout", "requiresLogin": true, "dividerBefore": true }
  ],
  "kindFilters": [
    { "name": "all", "kinds": [1,6,20,22,30023,30311,30402], "kindsByFeed": {"follows": [1,6,20,22,30023]} },
    { "name": "notes", "kinds": [1, 6] },
    { "name": "classifieds", "kinds": [30402], "only": ["global"] },
    { "name": "highlights", "kinds": [9802], "only": ["follows", "me"] },
    { "name": "live", "kinds": [30311], "only": ["global"] },
    { "name": "longform", "kinds": [30023] },
    { "name": "photos", "kinds": [20] },
    { "name": "shorts", "kinds": [22], "only": ["follows", "global"] }
  ],
  "defaults": {
    "feed": "follows",
    "loggedOutFeed": "global",
    "settingsIcon": "avatar",
    "settingsIconFallback": "/static/avatar.jpg"
  }
}
```

Note: Settings items can use `kinds` + `feed` fields to navigate directly to timeline with kind filter, instead of a custom `href`. This is a DRY approach that reuses the timeline handler.

### Feed Configuration

| Field | Type | Description |
|-------|------|-------------|
| `name` | string | Identifier (titleKey: `feed.{name}`) |
| `titleKey` | string | Override i18n key |
| `icon` | string | Emoji or icon |
| `iconOnly` | string | Icon display mode (see iconOnly Values) |
| `requiresLogin` | bool | Hide when logged out |
| `feeds` | array | Nested child feeds (see Nested Feeds below) |
| `dvm` | object | DVM configuration (see DVM Feeds below) |

### DVM Feeds (NIP-90)

Feeds can be powered by Data Vending Machines (DVMs) for algorithmic content like trending notes. Add a `dvm` object to any feed:

```json
{
  "feeds": [
    { "name": "follows", "requiresLogin": true },
    { "name": "global" },
    {
      "name": "trending",
      "icon": "🔥",
      "dvm": {
        "kind": 5300,
        "pubkey": "abc123...",
        "relays": ["wss://relay.damus.io"],
        "cacheTTL": 300,
        "pageSize": 20
      }
    }
  ]
}
```

### Nested Feeds (DVM Groups)

For multiple related DVMs (e.g., different trending algorithms), use nested feeds. The parent feed acts as a container with a dropdown menu:

```json
{
  "feeds": [
    { "name": "follows", "requiresLogin": true },
    { "name": "global" },
    {
      "name": "discover",
      "icon": "🔥",
      "feeds": [
        {
          "name": "trending",
          "dvm": { "kind": 5300, "pubkey": "abc...", "name": "Trending 4h" }
        },
        {
          "name": "mostzapped",
          "dvm": { "kind": 5300, "pubkey": "abc...", "name": "Most Zapped" }
        },
        {
          "name": "foryou",
          "requiresLogin": true,
          "dvm": { "kind": 5300, "pubkey": "def...", "personalized": true }
        }
      ]
    }
  ]
}
```

**Nested Feed Behavior:**

| Children | Display |
|----------|---------|
| 1 visible | Direct tab with parent's name/icon, links to child feed |
| 2+ visible | Dropdown menu with parent as toggle, children as items |
| 0 visible | Hidden (all children require login and user logged out) |

**Active States:**

- Parent dropdown gets `active` class when any child is active
- Active child gets `active` class and `aria-current="page"`

**i18n Keys:**

Add translations for feed names:
```json
{
  "feed.discover": "Discover",
  "feed.trending": "Trending",
  "feed.mostzapped": "Most Zapped",
  "feed.foryou": "For You"
}
```

**DVM Configuration Fields:**

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `kind` | int | required | DVM request kind (e.g., 5300 for content discovery) |
| `pubkey` | string | required | DVM service pubkey (hex) |
| `relays` | string[] | default relays | Relays to communicate with DVM |
| `cacheTTL` | int | 300 | Cache duration in seconds |
| `pageSize` | int | 20 | Events per page |
| `personalized` | bool | false | Include user pubkey in request (requires login) |
| `params` | object | {} | Additional DVM parameters |
| `name` | string | from kind 31990 | Display name override |
| `image` | string | from kind 31990 | Image URL override |
| `description` | string | from kind 31990 | Description override |

**DVM Metadata Display:**

DVM feeds display a header with name, image, and description. Metadata is fetched from the DVM's kind 31990 announcement event and cached for 1 hour. You can override any field in the config.

**How It Works:**

1. Server sends kind 5300 request to DVM with optional params
2. DVM responds with kind 6300 containing event references ("e" and "a" tags)
3. Server fetches referenced events from relay hints or default relays
4. Events are cached and rendered in timeline

**Performance Features:**

- DVM responses cached (configurable TTL)
- Fetched events use global event cache
- Relay hints respected with fallback to defaults
- Addressable events ("a" tags) supported for articles

### Utility Navigation

| Field | Type | Description |
|-------|------|-------------|
| `name` | string | Identifier (titleKey defaults to `nav.{name}`) |
| `titleKey` | string | Override default i18n key |
| `href` | string | Static URL |
| `icon` | string | Emoji or icon |
| `iconOnly` | string | Icon display mode (see iconOnly Values) |
| `requiresLogin` | bool | Hide when logged out |

### Settings Dropdown

| Field | Type | Description |
|-------|------|-------------|
| `name` | string | Identifier (titleKey defaults to `settings.{name}`) |
| `titleKey` | string | Override default i18n key |
| `href` | string | URL for the item |
| `icon` | string | Emoji or icon prefix |
| `method` | string | "GET" (link) or "POST" (form) |
| `requiresLogin` | bool | Hide when logged out |
| `requiresLogout` | bool | Hide when logged in (e.g., relay list for logged-out users) |
| `dividerBefore` | bool | Render visual divider above item |
| `dynamic` | string | Special rendering: `"theme"` or `"relays"` |
| `kinds` | int[] | Navigate to timeline with kind filter (e.g., `[10003]` for bookmarks) |
| `feed` | string | Feed to use with `kinds` (e.g., `"me"`) |

### iconOnly Values

The `iconOnly` field controls responsive icon/text display:

| Value | Desktop | Mobile |
|-------|---------|--------|
| `"always"` | Icon only | Icon only |
| `"never"` | Text only | Text only |
| `"mobile"` | Icon + text | Icon only |
| `"desktop"` | Icon only | Icon + text |
| `""` (empty) | Icon + text | Icon + text |

### Action Grouping

Actions can be grouped into dropdown menus using the `groupWith` field:

```json
{
  "actions": {
    "quote": {
      "method": "GET",
      "href": "/quote/{event_id}",
      "icon": "👉",
      "iconOnly": "always",
      "appliesTo": [1, 6, 20, 22, 30023]
    },
    "bookmark": {
      "groupWith": "quote",
      "toggleable": true,
      "appliesTo": [1, 6, 20, 22, 30023]
    },
    "mute": {
      "groupWith": "quote",
      "toggleable": true,
      "appliesTo": [1, 6, 20, 22, 9735, 30023, 30311]
    }
  }
}
```

When an action has `groupWith` set:
- It appears in the dropdown of the parent action (e.g., bookmark/mute appear under quote)
- It's automatically excluded from the main `displayOrder` iteration (no duplicates)
- Dropdown items always show text only (no icons), regardless of `iconOnly` setting

### Site Configuration (`config/site.json`)

Controls site identity, page titles, Open Graph metadata, and asset loading.

```json
{
  "site": {
    "name": "Nostr Hypermedia",
    "titleFormat": "{title} - {siteName}",
    "description": "A hypermedia Nostr client for the web"
  },
  "meta": {
    "themeColor": {
      "light": "#f5f5f5",
      "dark": "#121212"
    }
  },
  "openGraph": {
    "type": "website",
    "image": "/static/og-image.png"
  },
  "links": {
    "favicon": "/static/favicon.ico",
    "stylesheet": "/static/style.css",
    "preconnect": [
      "https://nostr.build",
      "https://image.nostr.build"
    ]
  },
  "scripts": [
    { "src": "/static/helm.js", "defer": true }
  ]
}
```

| Section | Field | Description |
|---------|-------|-------------|
| `site.name` | string | Site name used in titles and branding |
| `site.titleFormat` | string | Page title format with `{title}` and `{siteName}` placeholders |
| `site.description` | string | Default meta description for SEO |
| `meta.themeColor.light` | string | Theme color meta tag for light mode |
| `meta.themeColor.dark` | string | Theme color meta tag for dark mode |
| `openGraph.type` | string | Default Open Graph type (e.g., "website") |
| `openGraph.image` | string | Default Open Graph image path |
| `links.favicon` | string | Favicon path |
| `links.stylesheet` | string | Main stylesheet path |
| `links.preconnect` | string[] | URLs to preconnect for performance |
| `scripts[].src` | string | Script source URL |
| `scripts[].defer` | bool | Load script with defer attribute |
| `scripts[].async` | bool | Load script with async attribute |

### Kind Filters

| Field | Type | Description |
|-------|------|-------------|
| `name` | string | Identifier (titleKey defaults to `kind.{name}`) |
| `titleKey` | string | Override default i18n key |
| `kinds` | int[] | Nostr event kinds to query |
| `kindsByFeed` | object | Per-feed kind overrides (e.g., `{"follows": [1,6,20]}`) |
| `href` | string | Custom URL (overrides kinds-based URL) |
| `only` | string[] | Feeds where filter appears (e.g., `["me", "follows"]`) |
| `children` | array | Nested filters (renders as dropdown group) |

Note: For the "all" filter, use `kinds` and `kindsByFeed` to explicitly define which kinds to query instead of auto-collecting from all filters. `kindsByFeed` overrides `kinds` for specific feeds.

**Nested Kind Filters (Dropdown Groups):**

Use the `children` field to create grouped dropdowns for less common filters:

```json
{
  "kindFilters": [
    { "name": "notes", "kinds": [1] },
    { "name": "photos", "kinds": [20] },
    {
      "name": "more",
      "children": [
        { "name": "badges", "kinds": [30009, 8] },
        { "name": "classifieds", "kinds": [30402] },
        { "name": "events", "kinds": [31922, 31923] },
        { "name": "live", "kinds": [30311, 1311] }
      ]
    }
  ]
}
```

The parent filter (`more`) displays as a dropdown toggle, with children as menu items. Each child supports the same fields as top-level filters (`kinds`, `kindsByFeed`, `href`, `only`).

### Adding a Kind Filter

**1. Add to `config/navigation.json`:**

```json
{
  "kindFilters": [
    { "name": "zaps", "kinds": [9735], "only": ["me"] }
  ]
}
```

**2. Add i18n key to `config/i18n/en.json`:**

```json
{
  "kind.zaps": "Zaps"
}
```

**3. Reload:**

```bash
kill -HUP $(pgrep nostr-server)
```

### Adding a Custom Page to Submenu

For pages that aren't kind-based (like mutes):

```json
{
  "kindFilters": [
    { "name": "mutes", "href": "/mutes", "only": ["me"] }
  ]
}
```

The `href` field overrides the automatic `?kinds=X` URL generation.

### Custom Page Size

Use `limit` to set a custom page size (defaults to 10):

```json
{
  "kindFilters": [
    { "name": "recommendations", "kinds": [31989], "limit": 25 }
  ]
}
```

## CSS Conventions

CSS is organized into modular files in `static/css/`:

```
static/css/
├── base.css        # Reset, CSS variables, typography
├── layout.css      # Page layout, header, navigation
├── notes.css       # Note card base styles
├── kinds/          # Per-kind styles (one file per event type)
│   ├── picture.css, repost.css, zap.css, ...
├── components.css  # Action pills, dropdowns, utilities
└── pages.css       # Profile, search, notifications, wallet
```

### Development Workflow

```bash
# Build concatenated CSS (one-time)
./build-css.sh

# Watch mode (rebuilds on changes)
./build-css.sh --watch
```

The `build-release.sh` script automatically concatenates, minifies, and gzip-compresses CSS for production.

### Theme Variables

```css
--bg-primary, --bg-secondary     /* Backgrounds */
--text-primary, --text-secondary /* Text colors */
--accent, --accent-secondary     /* Accent colors */
--border-color                   /* Borders */
```

### Adding Kind-Specific Styles

Create `static/css/kinds/{kindname}.css`:

```css
/* Example: Kind 9802 highlight styles */
.highlight { padding: 16px 20px; }
.highlight-blockquote {
    border-left: 4px solid var(--accent);
    padding-left: 16px;
}
```

Then rebuild: `./build-css.sh`

### Action Classes

```css
.action-reply, .action-repost, .action-quote
.action-react, .action-bookmark, .action-mute
.action-disabled  /* Applied to disabled actions */
```

## Internationalization (i18n)

Strings in `config/i18n/en.json`:

```json
{
  "btn.post": "Post",
  "nav.search": "Search",
  "kind.notes": "Notes",
  "feed.follows": "Follows",
  "msg.no_results": "No results found",
  "action.mute": "Mute",
  "confirm.unmute": "Unmute this user?"
}
```

### Using i18n in Templates

```html
<button type="submit">{{i18n "btn.post"}}</button>
<span>{{i18n "kind.notes"}}</span>
```

### Key Naming Convention

| Prefix | Use |
|--------|-----|
| `settings.*` | Settings dropdown items (theme, profile, wallet, logout) |
| `theme.*` | Theme labels (dark, light, auto) |
| `nav.*` | Navigation items (search, notifications, settings) |
| `wallet.*` | Wallet page strings (connected, setup, balance, zap) |
| `time.*` | Relative time labels (now, minute_ago, hours_ago) |
| `feed.*` | Feed tab labels (follows, global, me) |
| `kind.*` | Kind filter labels (notes, photos, longform) |
| `action.*` | Action button labels (reply, repost, react, zap) |
| `confirm.*` | Confirmation dialogs (mute, unmute) |
| `btn.*` | Button labels (post, reply, search, save) |
| `label.*` | General labels (quoting_as, replying_to, reposted) |
| `status.*` | Status indicators (live, scheduled, ended, loading) |
| `msg.*` | Messages and prompts (no_results, login_to) |
| `a11y.*` | Accessibility labels (avatar, skip_to_main)

### Adding a New Language

1. Copy `config/i18n/en.json` to `config/i18n/{lang}.json`
2. Translate all values
3. Set `I18N_DEFAULT_LANG={lang}` environment variable
4. Reload: `kill -HUP $(pgrep nostr-server)`

## Hot Reload

Reload all JSON config without restart:

```bash
kill -HUP $(pgrep nostr-server)
```

Files reloaded:
- `config/site.json`
- `config/actions.json`
- `config/navigation.json`
- `config/relays.json`
- `config/i18n/*.json`

**Auto-refresh**: Connected browsers automatically refresh via SSE when config is reloaded. The server broadcasts a `reload` event to `/stream/config`, and a hidden HelmJS link re-fetches the current page with `cache_only=1` to avoid slow relay fetches. The body is morphed to preserve video playback, form inputs, and scroll position.

Note: Programmatically registered actions require a restart.

## Environment Variables

See [DEPLOYMENT.md](DEPLOYMENT.md) for the complete environment variable reference.

## Quality Checks

```bash
./cmd/run_checks.sh
```

Runs six static analysis tools (accessibility, HATEOAS, NATEOAS, markup, i18n, security). Reports saved to `reports/`. See [cmd/README.md](cmd/README.md) for individual tool usage.

## Testing Changes

```bash
# Build and start server
go build && DEV_MODE=1 ./nostr-server

# Test endpoints
curl -s "http://localhost:3000/timeline?feed=global&kinds=1&limit=1"
curl -s "http://localhost:3000/mutes"
curl -s "http://localhost:3000/search?q=test"

# Check action appears
curl -s "http://localhost:3000/timeline?feed=global&kinds=1&limit=1" | grep "action-zap"
```

## Key Types Reference

### ActionContext

Passed to action builders:

```go
type ActionContext struct {
    EventID       string
    EventPubkey   string
    Kind          int
    IsBookmarked  bool
    IsReacted     bool   // User has already reacted
    IsReposted    bool   // User has already reposted
    IsMuted       bool   // Author is in user's mute list
    ReplyCount    int    // Number of replies
    RepostCount   int    // Number of reposts
    ReactionCount int    // Total reactions (consolidated)
    ZapTotal      int64  // Total zap amount in sats
    LoggedIn      bool
    IsAuthor      bool
    CSRFToken     string
    ReturnURL     string
    LoginURL      string
}
```

### ActionDefinition

Returned by builders, used by templates:

```go
type ActionDefinition struct {
    Name      string
    Title     string
    Method    string  // "GET" or "POST"
    Href      string
    Class     string
    Rel       string  // Link relation (e.g., "reply", "bookmark")
    Icon      string  // Optional icon
    IconOnly  string  // "always", "never", "mobile", "desktop", or ""
    Fields    []FieldDefinition
    Disabled  bool    // Render as non-interactive text
    Completed bool    // Action already performed (filled pill style)
    Count     int     // Count to display (if HasCount is true)
    HasCount  bool    // Whether to show count
    GroupWith string  // Parent action name (appears in dropdown)
}
```

### KindFilterContext

For building kind filters:

```go
type KindFilterContext struct {
    LoggedIn    bool
    ActiveFeed  string
    ActiveKinds string
    ActivePage  string  // For custom href items
    FastMode    bool
}
```

### FeedModeContext

For building feed tabs:

```go
type FeedModeContext struct {
    LoggedIn    bool
    ActiveFeed  string
    CurrentPage string
}
```

## Architecture Notes

### Action Flow

1. `GetActionsForEvent(ctx)` called for each event
2. Checks `actions.json` for applicable actions
3. Checks registered actions via `RegisterKindAction`
4. Returns `[]ActionDefinition` for template
5. Template renders GET as `<a>`, POST as `<form>`

### Data Flow

```
Request → Handler → Relay Query → Event Processing → Template → Response
                         ↓
                   GetActionsForEvent()
                         ↓
              config/actions.json + registered actions
```

### Navigation Flow

1. Handler creates context (LoggedIn, ActiveFeed, ActivePage)
2. `GetFeedModes()`, `GetKindFilters()`, `GetNavItems()` build nav data
3. Template iterates over provided navigation
4. No hardcoded URLs in templates

### Content Processing

Note content goes through `processContentToHTMLFull()` which handles:

1. **Blossom URIs (BUD-10)** - `blossom:<sha256>.<ext>[?xs=server&sz=bytes]`
   - Resolved to HTTPS URLs using configured `blossomServers`
   - Rendered as styled file links with icon, filename, and optional size
   - Example: `blossom:b167...553.pdf?sz=184292` → `📄 Click to download: b1674191.pdf (180 KB)`

2. **Media URLs** - Images, videos, audio auto-embedded
   - Images: `.jpg`, `.png`, `.gif`, `.webp` → `<img>`
   - Videos: `.mp4`, `.webm`, `.mov` → `<video>`
   - Audio: `.mp3`, `.wav`, `.ogg` → `<audio>`

3. **File URLs** - Non-media files get styled download links
   - Documents: `.pdf`, `.doc`, `.txt` → `📄 Click to download: file.pdf`
   - Archives: `.zip`, `.tar`, `.gz` → `📦 Click to download: file.zip`

4. **Nostr references** - `nostr:npub1...`, `nostr:nevent1...` resolved to links/embeds

5. **Link previews** - OpenGraph metadata fetched for regular URLs

### @Mention Autocomplete (NIP-27)

Post, reply, and quote forms support @mention autocomplete:

1. User types `@xxx` (3+ chars) in textarea
2. Hidden `h-trigger` fires debounced request to `/mentions`
3. Server searches user's follows for matching profiles
4. Dropdown shows results with avatar, name, and npub
5. Click selects: OOB replaces `@xxx` → `@displayname`, merges pubkey mapping
6. On submit: `@displayname` → `nostr:nprofile1...` with relay hints, p-tags added

**Files:** `templates/mentions.go`, `html_handlers.go` (handlers), `html_auth.go` (`processMentions`)

## Security

| Protection | Implementation | File |
|------------|----------------|------|
| CSRF | HMAC-SHA256 signed tokens with session binding | `csrf.go` |
| XSS | `html.EscapeString()` + bluemonday sanitization | `html.go` |
| SSRF | Connection-time IP validation, blocks private/metadata IPs | `link_preview.go` |
| Rate Limiting | Per-IP and per-session limits with fallback | `nip46.go` |
| Authentication | NIP-46 remote signing (private keys never touch server) | `nip46.go` |
| Session Security | HttpOnly, SameSite=Strict, Secure cookies | `html_auth.go` |

**Key patterns:**

```go
// CSRF: Generate and validate tokens
csrfToken := generateCSRFToken(session.ID)
if !validateCSRFToken(session.ID, r.FormValue("csrf_token")) { ... }

// XSS: Escape user content
escaped := html.EscapeString(content)
sanitized := markdownSanitizer.Sanitize(rendered)  // For markdown
```

See [API.md](API.md) for authentication flow and rate limits. See [DEPLOYMENT.md](DEPLOYMENT.md) for proxy configuration, HSTS, and security checklist.

## SSE (Server-Sent Events)

See [API.md](API.md) for SSE endpoint documentation. Below is how to add new endpoints.

### Adding a New SSE Endpoint

**1. Create handler in `sse.go`:**

```go
func streamNewFeatureHandler(w http.ResponseWriter, r *http.Request) {
    // Auth check if needed
    session := getSessionFromRequest(r)
    if session == nil || !session.Connected {
        http.Error(w, "Unauthorized", http.StatusUnauthorized)
        return
    }

    // SSE setup
    flusher, ok := w.(http.Flusher)
    if !ok {
        http.Error(w, "SSE not supported", http.StatusInternalServerError)
        return
    }

    w.Header().Set("Content-Type", "text/event-stream")
    w.Header().Set("Cache-Control", "no-cache")
    w.Header().Set("Connection", "keep-alive")

    // Check client type for content negotiation
    isHelmRequest := r.Header.Get("H-Request") == "true"

    // Subscribe to relays, process events...
    for event := range eventChan {
        if isHelmRequest {
            sendSSEHTML(w, flusher, "eventtype", htmlFragment)
        } else {
            sendSSEEvent(w, flusher, "eventtype", jsonData)
        }
    }
}
```

**2. Register route in `main.go`:**

```go
http.HandleFunc("/stream/newfeature", securityHeaders(streamNewFeatureHandler))
```

**3. Add HelmJS listener in template:**

```html
<span h-sse="/stream/newfeature" hidden>
  <template h-sse-on="eventtype" h-target="#target-element" h-swap="outer"></template>
</span>
```

### SSE Helper Functions

```go
// Send JSON data (for Siren clients)
sendSSEEvent(w, flusher, "eventtype", data)

// Send raw HTML (for HelmJS clients)
sendSSEHTML(w, flusher, "eventtype", htmlString)
```

## Networking

- **NIP-65 Outbox Model** - Relay selection for fetching and publishing
- **Request Deduplication** - Singleflight, batching, and subscription aggregation
- **Relay Pool** - Connection pooling, health tracking, per-relay concurrency limits

**Key files:** `relay.go`, `relay_pool.go`, `singleflight.go`, `batcher.go`, `subscription_aggregator.go`

## Caching System

Supports both in-memory and Redis backends. See [DEPLOYMENT.md](DEPLOYMENT.md) for Redis configuration.

**Key files:** `cache.go`, `cache_interface.go`, `cache_memory.go`, `cache_redis.go`

### Adding a New Cache Type

1. Add interface to `cache_interface.go`
2. Add memory implementation to `cache_memory.go`
3. Add Redis implementation to `cache_redis.go`
4. Add global instance and initialization in `cache.go`
