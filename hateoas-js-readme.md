# Client-Side Malleable UI: HATEOAS with JavaScript

A fully client-side implementation of malleable UI that connects directly to Nostr relays—no custom server required. The "app" is just an HTML file that interprets UI specifications from note content.

## Quick Start

### Option 1: Open directly in browser (no server)

```bash
# Just open the file
open static/malleable.html

# Or on Linux
xdg-open static/malleable.html
```

### Option 2: Serve via the Go server

```bash
# Start the server
go run .

# Visit in browser
open http://localhost:8080/static/malleable.html
```

### Option 3: Any static file server

```bash
# Python
python -m http.server 8000 --directory static

# Node
npx serve static

# Then visit
open http://localhost:8000/malleable.html
```

## Test with a Real Note

A test UI-spec note has been published to `wss://relay.damus.io`:

```
Event ID: 35f6b5747d49a0a232bbc7f130bc232699b31ee1b99f5a1a2e5ef22b9d99acae
```

1. Open `malleable.html` in your browser
2. Paste the event ID into the input field
3. Click "Load Event"
4. See the UI rendered from the note's content

## Publish Your Own UI-Spec Note

Using [nak](https://github.com/fiatjaf/nak):

```bash
# Generate a key (or use your own)
NSEC=$(nak key generate)

# Create a UI spec
UI_SPEC='{
  "layout": "card",
  "elements": [
    {"type": "heading", "value": "My Custom UI"},
    {"type": "text", "value": "This UI lives in a Nostr note!"},
    {"type": "button", "label": "Like", "action": "like"}
  ],
  "actions": [
    {"id": "like", "publish": {"kind": 7, "content": "+", "tags": [["e", "{{$.id}}"]]}}
  ]
}'

# Publish to relay
nak event --sec "$NSEC" -k 1 -c "$UI_SPEC" wss://relay.damus.io

# Note the event ID from the output, then load it in malleable.html
```

## Architecture

```
┌─────────────────────────────────────────────────────────────────┐
│                    malleable.html (~600 lines)                   │
│                                                                  │
│  ┌─────────────────────────────────────────────────────────┐    │
│  │                     Vanilla JS                           │    │
│  │  • RelayPool - WebSocket connection management           │    │
│  │  • Bech32 decoder - parse nevent1.../note1.../naddr1... │    │
│  │  • TLV parser - extract event IDs from encoded refs      │    │
│  │  • UI interpreter - JSON spec → HTML DOM                 │    │
│  └─────────────────────────────────────────────────────────┘    │
│                                                                  │
│  ┌─────────────────────────────────────────────────────────┐    │
│  │                     Alpine.js (~8KB)                     │    │
│  │  • Reactive state management                             │    │
│  │  • Loading/error states                                  │    │
│  │  • Two-way data binding                                  │    │
│  │  • Event handling                                        │    │
│  └─────────────────────────────────────────────────────────┘    │
│                                                                  │
│  ┌─────────────────────────────────────────────────────────┐    │
│  │                  NIP-07 Integration                      │    │
│  │  • window.nostr.getPublicKey() - connect wallet          │    │
│  │  • window.nostr.signEvent() - sign actions               │    │
│  │  • Works with Alby, nos2x, Flamingo, etc.               │    │
│  └─────────────────────────────────────────────────────────┘    │
└─────────────────────────────────────────────────────────────────┘
                              │
                              │ WebSocket (direct to relays)
                              │ No custom server needed!
                              ▼
┌─────────────────────────────────────────────────────────────────┐
│                      Nostr Relay Network                         │
│  • wss://relay.damus.io                                         │
│  • wss://relay.nostr.band                                       │
│  • wss://nos.lol                                                │
│  • wss://relay.primal.net                                       │
└─────────────────────────────────────────────────────────────────┘
```

## How It Works

### 1. Fetch a note

The client connects to multiple relays via WebSocket and sends a REQ message:

```json
["REQ", "sub_1", {"ids": ["35f6b5..."], "limit": 1}]
```

### 2. Parse UI spec from content

When the note arrives, its `content` field is parsed as JSON:

```json
{
  "layout": "card",
  "elements": [
    {"type": "heading", "value": "Hello!"},
    {"type": "button", "label": "Click me", "action": "do-thing"}
  ],
  "actions": [
    {"id": "do-thing", "publish": {"kind": 7, "content": "clicked"}}
  ]
}
```

### 3. Render to DOM

The interpreter converts each element to HTML:

```html
<div class="ui-card">
  <h2 class="ui-heading">Hello!</h2>
  <button class="ui-button" onclick="...">Click me</button>
</div>
```

### 4. Execute actions

When a button is clicked:
1. Build an unsigned event from the action's `publish` spec
2. Call `window.nostr.signEvent()` to sign with NIP-07
3. Publish the signed event to relays

## UI Spec Reference

### Top-level properties

| Property | Type | Description |
|----------|------|-------------|
| `layout` | string | Container class: `card`, `list`, `form` |
| `title` | string | Page title (optional) |
| `elements` | array | UI elements to render |
| `actions` | array | Named actions for buttons |
| `style` | string | Custom CSS (optional) |

### Element types

| Type | Properties | Output |
|------|------------|--------|
| `heading` | `value` | `<h2>` |
| `text` | `value` | `<p>` |
| `image` | `src` or `value` | `<img>` |
| `link` | `href`, `label` | `<a>` |
| `button` | `label`, `action` or `href` | `<button>` or `<a>` |
| `input` | `name`, `label`, `value` | `<input>` |
| `container` | `children`, `style` | `<div>` wrapper |
| `data` | `bind`, `label` | Displays bound data |
| `hr` | — | `<hr>` |

### Data bindings

Use `$.path` syntax to bind to event data:

| Binding | Value |
|---------|-------|
| `$.id` | Event ID (hex) |
| `$.pubkey` | Author pubkey (hex) |
| `$.npub` | Author npub (bech32) |
| `$.content` | Raw content |
| `$.time` | Formatted timestamp |
| `$.kind` | Event kind number |

### Actions

**Publish action** — create and sign a new event:

```json
{
  "id": "vote-yes",
  "publish": {
    "kind": 7,
    "content": "yes",
    "tags": [["e", "{{$.id}}"]]
  }
}
```

**Link action** — navigate to URL:

```json
{
  "id": "view-thread",
  "link": "/html/thread/{{$.id}}"
}
```

### Template variables

In action content and tags, use `{{$.path}}` for substitution:

- `{{$.id}}` → event ID
- `{{$.pubkey}}` → author pubkey
- `{{input:fieldname}}` → value from input element with that name

## Demo Specs

The page includes three built-in demos:

### Poll
```json
{
  "layout": "card",
  "elements": [
    {"type": "heading", "value": "What's the best approach?"},
    {"type": "container", "style": "options", "children": [
      {"type": "button", "label": "Option A", "action": "vote-a"},
      {"type": "button", "label": "Option B", "action": "vote-b"}
    ]}
  ],
  "actions": [
    {"id": "vote-a", "publish": {"kind": 7, "content": "a", "tags": [["e", "{{$.id}}"]]}},
    {"id": "vote-b", "publish": {"kind": 7, "content": "b", "tags": [["e", "{{$.id}}"]]}}
  ]
}
```

### Profile Card
Shows user data with follow/message actions.

### Feedback Form
Text input with submit action that publishes a kind-1 note.

## Comparison: Server vs Client Rendering

| Aspect | Server-side (`/html/malleable`) | Client-side (`malleable.html`) |
|--------|--------------------------------|-------------------------------|
| Server required | Yes (Go) | No |
| Relay connection | Server → Relays | Browser → Relays |
| Signing | NIP-46 (bunker) | NIP-07 (extension) |
| Works offline | No | Yes (with cached notes) |
| JavaScript | None | Alpine.js (~8KB) |
| File size | N/A | ~15KB |

## The Vision

This demonstrates **hypermedia as the engine of application state** in its purest form:

1. **No server** — The HTML file is the entire "app"
2. **No build step** — Just open the file
3. **No hosting** — Share the HTML file, or host it anywhere
4. **No deploy** — Publish a note, everyone sees the new UI
5. **Forkable** — Anyone can publish their own UI for the same data
6. **Composable** — UI specs can reference other notes

### Vibecoding without files

Imagine iterating on an app by:
1. Editing JSON in a text field
2. Publishing it as a note
3. Instantly seeing the result
4. Sharing the event ID for others to try

The "source code" is just notes. The "runtime" is any client that understands the UI spec format.

## Files

| File | Description |
|------|-------------|
| `static/malleable.html` | Client-side malleable UI (this feature) |
| `malleable.go` | Server-side malleable UI (no-JS version) |
| `malleable-nojs-readme.md` | Documentation for server-side version |

## Dependencies

**Runtime (loaded from CDN):**
- Alpine.js 3.x (~8KB gzipped)

**For publishing test notes:**
- [nak](https://github.com/fiatjaf/nak) — `go install github.com/fiatjaf/nak@latest`

**For NIP-07 signing:**
- Browser extension: Alby, nos2x, Flamingo, or similar
