# Development Guide

Guide for extending nostr-hypermedia with new actions, event kinds, pages, and navigation.

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
├── .env.example           # Environment template (copy to .env)
├── main.go                # HTTP server, routes, SIGHUP handler
├── logging.go             # Structured logging (slog, JSON, LOG_LEVEL env var)
├── metrics.go             # Prometheus metrics (/metrics endpoint)
├── html_handlers.go       # Page handlers (timeline, thread, profile, search, notifications, mutes)
├── html_auth.go           # Action handlers (post, reply, react, repost, quote, bookmark, mute, follow)
├── html.go                # Template compilation and rendering orchestration
├── handlers.go            # JSON API response handlers (TimelineResponse, EventItem, Siren support)
├── relay.go               # WebSocket relay client, event fetching
├── relay_pool.go          # Connection pooling with health tracking
├── actions.go             # Action system core (GetActionsForEvent)
├── actions_config.go      # JSON config loader with hot-reload
├── actions_registry.go    # Programmatic action registration
├── kinds.go               # Event kind definitions, registry, behavioral flags
├── kinds_appliers.go      # Kind-specific data appliers (zap, live, highlight, video, etc.)
├── navigation.go          # Navigation helpers (FeedMode, KindFilter, NavItem, SettingsItem)
├── navigation_config.go   # Unified navigation config loader
├── relays_config.go       # Relay list config loader
├── i18n_config.go         # Internationalization strings loader
├── flash.go               # Flash message cookies (success/error messages across redirects)
├── nip19.go               # NIP-19 bech32 encoding (npub, naddr, nevent, nprofile)
├── nip44.go               # NIP-44 encryption (ChaCha20 + HMAC-SHA256)
├── nip46.go               # NIP-46 remote signing
├── nostrconnect.go        # Nostr Connect flow (nostrconnect:// URI)
├── nostr_action_tags.go   # Parsing action definitions from Nostr event tags
├── nostr_kind_fetcher.go  # Metadata fetching from Nostr kind 39001 events (experimental)
├── csrf.go                # CSRF token handling
├── cache.go               # Cache wrappers and initialization (supports Redis or in-memory)
├── cache_interface.go     # CacheBackend, SessionStore, PendingConnStore interfaces
├── cache_memory.go        # In-memory cache implementations
├── cache_redis.go         # Redis cache implementations (optional)
├── link_preview.go        # Open Graph metadata fetching
├── lnurl.go               # LNURL-pay handling for Lightning payments
├── nwc.go                 # NWC client (NIP-47 Nostr Wallet Connect)
├── site_config.go         # Site configuration loader (site.json)
├── sse.go                 # Server-sent events for live updates
├── siren.go               # Siren hypermedia format (JSON API)
├── giphy.go               # Giphy API client for GIF picker
│
├── templates/             # Go template files
│   ├── base.go            # Base layout, CSS, navigation, settings dropdown
│   ├── timeline.go        # Timeline/feed content
│   ├── thread.go          # Thread view with replies
│   ├── profile.go         # Profile page with edit mode
│   ├── notifications.go   # Notifications list
│   ├── search.go          # Search page
│   ├── quote.go           # Quote form
│   ├── login.go           # Login page (NIP-46/Nostr Connect)
│   ├── mutes.go           # Muted users list
│   ├── kinds.go           # Kind-specific rendering (note, photo, video, article, etc.)
│   ├── fragment.go        # Reusable fragments (author-header, note-footer, etc.)
│   ├── gifs.go            # GIF picker panel and search results
│   ├── compose.go         # Compose page (no-JS fallback for media attachments)
│   └── wallet.go          # Wallet page (NWC connection, balance display)
│
├── static/                # Static assets (source files, .gz generated at release)
│   ├── style.css          # Stylesheet
│   ├── helm.js            # HelmJS (external) - github.com/Letdown2491/helmjs
│   ├── avatar.jpg         # Default avatar image
│   ├── favicon.ico        # Favicon
│   └── og-image.png       # Open Graph default image
│
├── config/                # JSON configuration (hot-reloadable via SIGHUP)
│   ├── site.json          # Site identity, title format, Open Graph, scripts
│   ├── actions.json       # Action definitions
│   ├── navigation.json    # Feeds, utility nav, kind filters
│   ├── relays.json        # Relay URLs by category (default, search, publish, profile, NIP-46)
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
      "href": "/html/zap",
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
http.HandleFunc("/html/zap", securityHeaders(limitBody(htmlZapHandler, maxBodySize)))
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
            Href:      "/html/zap",
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
        Href:   "/html/zap",
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
| `ShowInTimeline`, `ShowReplyCount`, `ShowReactions` | UI flags |
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

**4. Add rendering template in `templates/kinds.go`:**

```html
{{define "kind-highlight"}}
<article class="note highlight">
  {{template "author-header" .}}
  <blockquote class="highlight-content">{{.Content}}</blockquote>
  {{if .HighlightContext}}<cite>{{.HighlightContext}}</cite>{{end}}
  {{template "note-footer" .}}
</article>
{{end}}
```

**5. Configure actions in `config/actions.json`:**

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
http.HandleFunc("/html/newpage", securityHeaders(htmlNewpageHandler))
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
    { "name": "search", "href": "/html/search", "icon": "🔎︎", "iconOnly": "always" },
    { "name": "notifications", "href": "/html/notifications", "requiresLogin": true, "icon": "🔔", "iconOnly": "always" }
  ],
  "settings": [
    { "name": "relays", "dynamic": "relays", "requiresLogout": true },
    { "name": "edit_profile", "href": "/html/profile/edit", "requiresLogin": true },
    { "name": "bookmarks", "kinds": [10003], "feed": "me", "requiresLogin": true },
    { "name": "highlights", "kinds": [9802], "feed": "me", "requiresLogin": true },
    { "name": "mutes", "href": "/html/mutes", "requiresLogin": true },
    { "name": "logout", "href": "/html/logout", "requiresLogin": true, "dividerBefore": true }
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
| `dynamic` | string | Special rendering: "theme" or "relays" |
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
      "href": "/html/quote/{event_id}",
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

Note: For the "all" filter, use `kinds` and `kindsByFeed` to explicitly define which kinds to query instead of auto-collecting from all filters. `kindsByFeed` overrides `kinds` for specific feeds.

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
    { "name": "mutes", "href": "/html/mutes", "only": ["me"] }
  ]
}
```

The `href` field overrides the automatic `?kinds=X` URL generation.

## CSS Conventions

All styles in `static/style.css` (or `templates/base.go`).

### Theme Variables

```css
--bg-primary, --bg-secondary     /* Backgrounds */
--text-primary, --text-secondary /* Text colors */
--accent, --accent-secondary     /* Accent colors */
--border-color                   /* Borders */
```

### Action Classes

```css
.action-reply, .action-repost, .action-quote
.action-react, .action-bookmark, .action-mute
.action-disabled  /* Applied to disabled actions */
```

### Adding Custom Styles

```css
.action-zap {
    color: var(--accent);
}
.action-zap:hover {
    color: #f7931a; /* Bitcoin orange */
}
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

| Variable | Default | Description |
|----------|---------|-------------|
| `PORT` | 8080 | Server port |
| `LOG_LEVEL` | info | Log verbosity: `debug`, `info`, `warn`, `error` (JSON structured logging via slog) |
| `DEV_MODE` | - | Persistent keypair for development |
| `GZIP_ENABLED` | - | Enable gzip compression (`1`) |
| `ACTIONS_CONFIG` | config/actions.json | Actions config path |
| `NAVIGATION_CONFIG` | config/navigation.json | Navigation config path |
| `RELAYS_CONFIG` | config/relays.json | Relays config path |
| `SITE_CONFIG` | config/site.json | Site configuration path |
| `I18N_CONFIG_DIR` | config/i18n | i18n directory |
| `I18N_DEFAULT_LANG` | en | Default language |
| `ACTIONS_DISABLE` | - | Comma-separated actions to disable |
| `CSRF_SECRET` | - | Override CSRF secret (auto-generated if not set) |
| `NOSTR_ACTIONS_ENABLED` | - | Enable experimental Nostr-based action definitions from kind 39001 |
| `NOSTR_ACTIONS_RELAYS` | - | Relays to fetch action metadata from (comma-separated) |
| `NOSTR_ACTIONS_TTL` | - | Cache TTL for Nostr action metadata |
| `REDIS_URL` | - | Redis URL for distributed caching (format: `redis://[:password@]host:port/db`) |
| `GIPHY_API_KEY` | - | Giphy API key to enable GIF picker in post/reply/quote forms |
| `TRUSTED_PROXY_COUNT` | 0 | Number of trusted reverse proxies for rate limiting |
| `HSTS_ENABLED` | - | Set to `1` to enable HSTS header (HTTPS deployments only) |
| `HSTS_MAX_AGE` | 31536000 | HSTS max-age in seconds (default: 1 year) |
| `SECURE_COOKIES` | - | Set to `1` to force Secure flag on cookies, `0` to disable |

## Quality Checks

Six static analysis tools validate compliance with project standards. All produce HTML reports with consistent structure:

1. **Score Card** - Overall percentage score with pass/fail counts
2. **Categories Grid** - Visual breakdown by category with individual scores
3. **Detailed Findings** - Expandable sections grouped by category, with pass/fail indicators and file locations

Run all checks at once (reports saved to `/reports/`):

```bash
./cmd/run_checks.sh
```

### Accessibility (WCAG 2.1)

```bash
cd cmd/accessibility-check && go build && ./accessibility-check -path ../.. -output ../../reports/accessibility-report.html
```

Categories: Perceivable, Operable, Understandable, Robust, Motion & Animation, Timing, Focus Management.

### HATEOAS Compliance

```bash
cd cmd/hateoas-check && go build && ./hateoas-check -path ../.. -output ../../reports/hateoas-report.html
```

Categories: Navigation, Forms & Actions, Links, Self-Describing, State Transfer, Accessibility, Pagination.

### NATEOAS Compliance

```bash
cd cmd/nateoas-check && go build && ./nateoas-check -path ../.. -output ../../reports/nateoas-report.html
```

Categories: Phase 1 (Centralize), Phase 2 (Dynamic), Phase 3 (Nostr-Native), Phase 4 (Full NATEOAS).

### Markup Validation

```bash
cd cmd/markup-check && go build && ./markup-check -path ../.. -output ../../reports/markup-report.html
```

Categories: HTML, CSS, Semantic, Best Practices, Dead Code, Performance, SEO & Meta, Mobile.

### i18n Coverage

```bash
cd cmd/i18n-check && go build && ./i18n-check -path ../.. -output ../../reports/i18n-report.html
```

Categories: Navigation & Actions, Labels & Titles, Buttons & Controls, Messages & Prompts, and many more.

### Security Analysis

```bash
cd cmd/security-check && go build && ./security-check -path ../.. -output ../../reports/security-report.html
```

Categories: CSRF Protection, HTTP Security Headers, Session Security, Nostr Security, Rate Limiting, SSRF Prevention, Cryptography, Information Disclosure.

## Testing Changes

```bash
# Build and start server
go build && DEV_MODE=1 ./nostr-server

# Test endpoints
curl -s "http://localhost:3000/html/timeline?kinds=1&limit=1&feed=global"
curl -s "http://localhost:3000/html/mutes"
curl -s "http://localhost:3000/html/search?q=test"

# Check action appears
curl -s "http://localhost:3000/html/timeline?kinds=1&limit=1" | grep "action-zap"
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

## Security

### Overview

The application implements multiple layers of security:

| Protection | Implementation | File |
|------------|----------------|------|
| CSRF | HMAC-SHA256 signed tokens with session binding | `csrf.go` |
| XSS | `html.EscapeString()` + bluemonday sanitization | `html.go` |
| SSRF | Connection-time IP validation, blocks private/metadata IPs | `link_preview.go` |
| Rate Limiting | Per-IP and per-session limits with fallback | `nip46.go` |
| Authentication | NIP-46 remote signing (private keys never touch server) | `nip46.go` |
| Session Security | HttpOnly, SameSite, Secure cookies | `html_auth.go` |

### CSRF Protection

All state-changing POST handlers validate CSRF tokens:

```go
// Generate token (bound to session ID)
csrfToken := generateCSRFToken(session.ID)

// Validate in handler
if !validateCSRFToken(session.ID, r.FormValue("csrf_token")) {
    http.Error(w, "Invalid CSRF token", http.StatusForbidden)
    return
}
```

For login forms (pre-auth), unique anonymous session IDs are generated per page load and stored in a `SameSite=Strict` cookie.

### XSS Prevention

User content is escaped before rendering:

```go
// Plain text content
escaped := html.EscapeString(content)

// Markdown content (long-form articles) - sanitized after rendering
sanitized := markdownSanitizer.Sanitize(buf.String())
return template.HTML(sanitized)
```

The `markdownSanitizer` uses bluemonday's `UGCPolicy()` which:
- Allows: links, images, bold, italic, lists, tables, code blocks
- Blocks: scripts, event handlers, `javascript:` URLs, iframes

### SSRF Protection

Link preview fetching validates IPs at connection time to prevent DNS rebinding:

```go
// ssrfSafeDialContext resolves DNS and validates before connecting
func ssrfSafeDialContext(ctx context.Context, network, addr string) (net.Conn, error) {
    // Resolve DNS
    ips, err := net.LookupIP(host)
    // Validate all IPs are public
    for _, ip := range ips {
        if isPublicIP(ip) {
            return ssrfSafeDialer.DialContext(ctx, network, net.JoinHostPort(ip.String(), port))
        }
    }
    return nil, errors.New("all resolved IPs are private/internal")
}
```

Blocked targets:
- Private IPs (10.x, 172.16-31.x, 192.168.x)
- Localhost (127.0.0.1, ::1)
- Link-local (169.254.x.x, fe80::/10)
- Cloud metadata (169.254.169.254)
- `.local` and `.internal` domains

### Proxy Configuration

When running behind a reverse proxy, configure `TRUSTED_PROXY_COUNT` to correctly identify client IPs for rate limiting:

| Value | Deployment | Behavior |
|-------|------------|----------|
| `0` (default) | Direct connection | Only trust `RemoteAddr` |
| `1` | Behind reverse proxy | Trust second-to-last IP in X-Forwarded-For |
| `2` | Cloudflare → reverse proxy | Trust third-to-last IP in X-Forwarded-For |

This prevents attackers from spoofing `X-Forwarded-For` to bypass rate limits.

### HSTS

For HTTPS deployments, enable HTTP Strict Transport Security:

```bash
HSTS_ENABLED=1 HSTS_MAX_AGE=31536000 ./nostr-server
```

Only enable on HTTPS - HSTS on HTTP causes browsers to refuse connecting.

### HTTP Security Headers

All responses include:

```
Content-Security-Policy: default-src 'self'; img-src * data:; media-src *;
                         frame-src youtube; style-src 'self' 'unsafe-inline';
                         script-src 'self'
X-Frame-Options: SAMEORIGIN
X-Content-Type-Options: nosniff
X-XSS-Protection: 1; mode=block
Referrer-Policy: strict-origin-when-cross-origin
Strict-Transport-Security: max-age=31536000; includeSubDomains (when HSTS_ENABLED=1)
```

## SSE (Server-Sent Events)

SSE provides real-time updates without polling. Used for live notifications.

### Endpoints

| Endpoint | Auth | Description |
|----------|------|-------------|
| `/stream/timeline` | No | Live timeline events (JSON) |
| `/stream/notifications?format=html\|json` | Yes | Live notification updates |

**`/stream/notifications` details:**
- Uses authenticated user's pubkey from session
- Subscribes to user's NIP-65 read relays (or default relays)
- Filters: `{kinds: [1,6,7,9735], "#p": [userPubkey], since: now}`
- Excludes self-notifications

### Format Parameter

`/stream/notifications` requires a `format` parameter (EventSource doesn't support custom headers):

```go
format := r.URL.Query().Get("format")
if format != "html" && format != "json" {
    http.Error(w, "Missing or invalid format parameter", http.StatusBadRequest)
    return
}

isHTMLFormat := format == "html"

if isHTMLFormat {
    // Return HTML fragment for HelmJS
    sendSSEHTML(w, flusher, "notification", badgeHTML)
} else {
    // Return JSON for Siren
    sendSSEEvent(w, flusher, "notification", eventData)
}
```

### HelmJS Integration

```html
<!-- SSE listener (hidden, only when logged in) -->
<span h-sse="/stream/notifications?format=html" hidden>
  <template h-sse-on="notification" h-target="#notification-badge" h-swap="outer"></template>
</span>

<!-- Badge element that gets updated -->
<span class="notification-badge notification-badge-hidden" id="notification-badge"></span>
```

When an SSE event with type `notification` arrives, HelmJS swaps the data into `#notification-badge`.

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

### HTTP Server Configuration

The HTTP server is configured with timeouts to prevent resource exhaustion:

```go
server := &http.Server{
    Addr:              ":" + port,
    ReadTimeout:       15 * time.Second,   // Max time to read request
    ReadHeaderTimeout: 5 * time.Second,    // Max time to read headers
    WriteTimeout:      60 * time.Second,   // Max time to write response (higher for SSE)
    IdleTimeout:       120 * time.Second,  // Max time for keep-alive connections
    MaxHeaderBytes:    1 << 20,            // 1MB max header size
}
```

### Relay Pool

WebSocket connections to Nostr relays are managed by a connection pool (`relay_pool.go`):

| Setting | Value | Description |
|---------|-------|-------------|
| `maxTotalConnections` | 50 | Maximum concurrent relay connections |
| Cleanup interval | 60s | How often idle connections are checked |
| Idle timeout | 2 min | Connections closed after inactivity |
| Ping interval | 30s | WebSocket keep-alive ping frequency |

**Monitoring:**
- `DroppedEventCount()` returns total events dropped due to full channels
- Logs include "dropped event" messages with relay URL and running total
- Logs include "connection pool limit reached" when at capacity

### External HTTP Clients

All external HTTP clients have timeout configuration:

| Client | Timeout | Transport Limits |
|--------|---------|------------------|
| Link Preview | 5s | 10 idle conns, 30s idle timeout |
| Giphy | 10s | 10 idle conns, 5 per host, 30s idle timeout |
| Avatar | 3s | Default transport, 3 redirect limit |

## Caching System

The caching system supports both in-memory and Redis backends, allowing multi-instance deployments with shared cache.

### Configuration

Set `REDIS_URL` to use Redis:

```bash
# Redis URL format
REDIS_URL=redis://localhost:6379/0

# With authentication
REDIS_URL=redis://:password@localhost:6379/0
```

If `REDIS_URL` is not set, the system falls back to in-memory caching.

### Cache Interfaces

All caches implement backend-agnostic interfaces defined in `cache_interface.go`:

| Interface | Purpose |
|-----------|---------|
| `CacheBackend` | Generic key-value cache with TTL |
| `SessionStore` | NIP-46 bunker session management |
| `PendingConnStore` | Pending Nostr Connect connections |
| `RateLimitStore` | Sliding window rate limiting |
| `SearchCacheStore` | NIP-50 search result caching |
| `ThreadCacheStore` | Thread page caching (root + replies) |
| `NotificationReadStore` | Notification last-read timestamp per user |

### Rate Limiting

Rate limiting protects against abuse:

| Limit Type | Default | Fallback | Key |
|------------|---------|----------|-----|
| Sign requests | 10/min | 10/min | `sign:{session_id}` |
| Login attempts | 5/min | 3/min (stricter) | `login:{client_ip}` |

**Fallback Rate Limiter:**

When the primary rate limit store (Redis) is unavailable, the system falls back to an in-memory rate limiter instead of failing open. This ensures rate limiting is always enforced:

- Login uses stricter fallback limit (3/min vs 5/min) for security-critical operations
- Fallback limiter is per-instance (not distributed), but still provides protection
- Logs indicate when fallback is used: `"Rate limit check error (using fallback): ..."`

**Usage in code:**

```go
// Check sign rate limit (in BunkerSession methods)
if err := s.checkSignRateLimit(); err != nil {
    return nil, err
}

// Check login rate limit by IP
if err := CheckLoginRateLimit(getClientIP(r)); err != nil {
    redirectWithError(w, r, "/html/login", "Too many login attempts")
    return
}
```

Rate limiting uses a sliding window algorithm for accurate counting.

### Search Caching

Search results are cached to reduce relay load:

| Parameter | Default |
|-----------|---------|
| TTL | 2 minutes |
| Scope | Initial searches (not paginated) |

**Cache key format:** `search:{query}|{kinds}|{limit}`

**Behavior:**
- Only initial searches are cached (pagination hits relays)
- Cache is bypassed when `until` parameter is present
- Mute filtering is applied after cache retrieval

### Cache TTLs

Default TTLs in `CacheConfig`:

| Cache | Found TTL | Not Found TTL |
|-------|-----------|---------------|
| Profile | 10 min | 2 min |
| Contacts | 5 min | - |
| Relay List | 30 min | 5 min |
| Avatar | 10 min | 5 min |
| Link Preview | 24 hr | 1 hr |
| Session | 24 hr | - |
| Pending Connection | 10 min | - |
| Search Results | 2 min | - |
| Thread | 3 min | - |
| Notification Read | 30 days | - |

### Thread Caching

Thread pages (root event + replies) are cached to reduce relay load:

| Parameter | Default |
|-----------|---------|
| TTL | 3 minutes |
| Scope | Event ID lookups (not naddr) |

**Behavior:**
- Cache stores root event and all replies together
- On cache hit, fetches new replies since cache time and merges them
- Profiles and reply counts are fetched fresh (already cached separately)
- Mute filtering applied after cache retrieval (user-specific)
- naddr lookups bypass cache (addressable events may change)

### Notification Read State

Tracks when each user last viewed their notifications:
- Key: `nostr:notif_read:{pubkey}` → Unix timestamp
- TTL: 30 days (cleanup inactive users)
- For logged-in users: Redis/store takes priority, cookie as fallback
- For logged-out users: cookie only (no pubkey to key on)
- On notification page visit (first page only): updates both store and cookie
- Enables accurate unread badge that persists across sessions/restarts

### Redis Key Structure

```
nostr:profile:{pubkey}           - Profile metadata
nostr:contacts:{pubkey}          - Contact list
nostr:relaylist:{pubkey}         - NIP-65 relay list
nostr:avatar:{url_hash}          - Avatar validation
nostr:linkpreview:{url_hash}     - Link preview data
nostr:session:{session_id}       - Bunker session
nostr:pending:{secret}           - Pending NIP-46 connection
nostr:ratelimit:sign:{id}        - Sign request timestamps (sorted set)
nostr:ratelimit:login:{ip}       - Login attempt timestamps (sorted set)
nostr:search:{query}|{kinds}|{limit} - Search results
nostr:thread:{event_id}          - Thread (root event + replies)
nostr:notif_read:{pubkey}        - Notification last read timestamp
```

### Adding a New Cache Type

**1. Add interface to `cache_interface.go`:**

```go
type NewCacheStore interface {
    Get(ctx context.Context, key string) (*DataType, bool, error)
    Set(ctx context.Context, key string, data *DataType, ttl time.Duration) error
}
```

**2. Add memory implementation to `cache_memory.go`:**

```go
type MemoryNewCacheStore struct {
    cache sync.Map
}

func NewMemoryNewCacheStore() *MemoryNewCacheStore {
    store := &MemoryNewCacheStore{}
    go store.cleanup()
    return store
}
```

**3. Add Redis implementation to `cache_redis.go`:**

```go
type RedisNewCacheStore struct {
    client *redis.Client
    prefix string
}

func NewRedisNewCacheStore(client *redis.Client, prefix string) *RedisNewCacheStore {
    return &RedisNewCacheStore{
        client: client,
        prefix: prefix + "newcache:",
    }
}
```

**4. Add global instance to `cache.go`:**

```go
var newCacheStore NewCacheStore

// In InitCaches():
if redisURL != "" {
    newCacheStore = NewRedisNewCacheStore(redisClient, "nostr:")
} else {
    newCacheStore = NewMemoryNewCacheStore()
}
```
