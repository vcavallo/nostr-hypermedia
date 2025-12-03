# Malleable UI: Server-Rendered, No JavaScript

A demonstration of **hypermedia as the engine of application state** taken to its logical extreme—where Nostr notes contain not just data, but UI specifications that the server interprets and renders as HTML.

## The Vision

What if vibecoding didn't require source code files or hosting? What if entire UIs and interactions could be composed solely of notes' content?

This system demonstrates that possibility:

1. A note's `content` field contains a JSON UI specification
2. The server parses and renders it as HTML
3. Actions (buttons) publish new Nostr events
4. **Zero client-side JavaScript required**

The "app" becomes just a dumb interpreter + note content. Anyone can publish new UIs, fork existing ones, or compose them—all without deploying anything.

## Try It

```bash
# Start the server
go run .

# Visit the demo
open http://localhost:8080/html/malleable

# View a real note as malleable UI (if it contains a UI spec)
open http://localhost:8080/html/malleable?event=<event-id>
```

## UI Specification Format

Notes can contain JSON that describes how to render them:

```json
{
  "layout": "card",
  "title": "Page Title",
  "elements": [
    {"type": "heading", "value": "Welcome"},
    {"type": "text", "value": "This UI is defined in a Nostr note."},
    {"type": "button", "label": "Click Me", "action": "do-something"}
  ],
  "actions": [
    {
      "id": "do-something",
      "publish": {
        "kind": 7,
        "content": "clicked",
        "tags": [["e", "{{$.id}}"]]
      }
    }
  ]
}
```

### Supported Elements

| Type | Properties | Description |
|------|------------|-------------|
| `heading` | `value` | Renders as `<h2>` |
| `text` | `value` | Renders as `<p>` |
| `image` | `src` or `value` | Renders as `<img>` |
| `link` | `href`, `label` | Renders as `<a>` |
| `button` | `label`, `action`, `href` | Triggers actions or navigates |
| `input` | `name`, `label`, `value` | Text input field |
| `container` | `children`, `style` | Groups elements; `style: "options"` for flex layout |
| `data` | `bind`, `label` | Displays bound event data |
| `hr` | — | Horizontal rule |

### Data Bindings

Elements can bind to the note's own data using `$.path` syntax:

| Binding | Value |
|---------|-------|
| `$.id` | Event ID (hex) |
| `$.pubkey` | Author pubkey (hex) |
| `$.npub` | Author npub (bech32) |
| `$.content` | Raw content string |
| `$.time` | Formatted timestamp |
| `$.kind` | Event kind number |

Example:
```json
{"type": "data", "bind": "$.npub", "label": "Author: "}
```

### Actions

Actions define what happens when a button is clicked. Currently supported:

**Publish Action** — Signs and publishes a new Nostr event:
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

Template variables like `{{$.id}}` are replaced with actual values.

**Link Action** — Navigates to a URL:
```json
{
  "id": "view-profile",
  "link": "/html/profile/{{$.pubkey}}"
}
```

## Example: A Self-Describing Poll

```json
{
  "layout": "card",
  "title": "Community Poll",
  "elements": [
    {"type": "heading", "value": "What should we build next?"},
    {"type": "text", "value": "Vote by clicking an option. Your vote is published as a reaction."},
    {"type": "hr"},
    {"type": "container", "style": "options", "children": [
      {"type": "button", "label": "Mobile App", "action": "vote-mobile"},
      {"type": "button", "label": "Desktop App", "action": "vote-desktop"},
      {"type": "button", "label": "Web App", "action": "vote-web"}
    ]},
    {"type": "hr"},
    {"type": "data", "bind": "$.npub", "label": "Poll by: "}
  ],
  "actions": [
    {"id": "vote-mobile", "publish": {"kind": 7, "content": "mobile", "tags": [["e", "{{$.id}}"]]}},
    {"id": "vote-desktop", "publish": {"kind": 7, "content": "desktop", "tags": [["e", "{{$.id}}"]]}},
    {"id": "vote-web", "publish": {"kind": 7, "content": "web", "tags": [["e", "{{$.id}}"]]}}
  ]
}
```

When someone views this note at `/html/malleable?event=<id>`:
- They see a rendered poll UI
- Clicking a button publishes a kind-7 reaction to the poll note
- No JavaScript executes—it's all HTML forms and server rendering

## Architecture

```
┌─────────────────────────────────────────────────────────────┐
│                     Nostr Relay Network                      │
│  ┌─────────────────────────────────────────────────────┐    │
│  │  Note with UI Spec                                   │    │
│  │  {                                                   │    │
│  │    "content": "{\"layout\":\"card\", ...}",         │    │
│  │    "kind": 1,                                        │    │
│  │    ...                                               │    │
│  │  }                                                   │    │
│  └─────────────────────────────────────────────────────┘    │
└─────────────────────────────────────────────────────────────┘
                              │
                              │ fetch
                              ▼
┌─────────────────────────────────────────────────────────────┐
│                  Hypermedia Server (Go)                      │
│                                                              │
│  1. Fetch note from relays                                   │
│  2. Parse content as JSON UI spec                            │
│  3. Resolve data bindings ($.id, $.npub, etc.)              │
│  4. Render elements to HTML                                  │
│  5. Wire actions as <form> elements                          │
│                                                              │
└─────────────────────────────────────────────────────────────┘
                              │
                              │ HTML response
                              ▼
┌─────────────────────────────────────────────────────────────┐
│                      Browser (No JS)                         │
│                                                              │
│  ┌─────────────────────────────────────────────────────┐    │
│  │  ┌──────────────────────────────────┐               │    │
│  │  │  What should we build next?      │               │    │
│  │  └──────────────────────────────────┘               │    │
│  │  Vote by clicking an option.                        │    │
│  │  ─────────────────────────────────                  │    │
│  │  [Mobile App] [Desktop App] [Web App]               │    │
│  │  ─────────────────────────────────                  │    │
│  │  Poll by: npub1abc...xyz                            │    │
│  └─────────────────────────────────────────────────────┘    │
│                                                              │
│  Clicking a button submits a form → server signs & publishes │
│                                                              │
└─────────────────────────────────────────────────────────────┘
```

## Why This Matters

### Traditional App Development
```
Write code → Build → Deploy → Users visit hosted app
```

### Malleable UI via Nostr
```
Publish a note → Everyone with a hypermedia client sees it
```

**Implications:**

1. **No hosting required** — The "app" is just note content
2. **Instant updates** — Publish a new note version, everyone sees it
3. **Forkable UIs** — Anyone can publish an alternative UI for the same data
4. **Composable** — UI specs can reference other notes (nested apps)
5. **Offline-first** — Notes can be cached, UIs work without live relays
6. **Censorship-resistant** — UIs propagate through relay network like any other note

## Limitations & Future Work

Current limitations:
- Simple element types (no tables, lists, complex layouts yet)
- No conditional rendering (`if`/`else` based on data)
- No loops (`for each` over arrays)
- Actions only support publishing events (no arbitrary HTTP calls)
- No form validation

Possible extensions:
- **Kind-specific rendering** — Auto-detect certain kinds and render appropriate UI
- **Nested specs** — Reference other notes as components
- **State binding** — Show reaction counts, reply threads inline
- **Custom CSS** — Allow notes to include style overrides
- **Signed UI specs** — Trust certain pubkeys for UI rendering

## Files

- `malleable.go` — UI spec parser, HTML renderer, HTTP handlers
- `main.go` — Route registration at `/html/malleable` and `/html/malleable-action`

## Related Concepts

- **HATEOAS** — Hypermedia as the Engine of Application State
- **REST** — Representational State Transfer (the original vision, not "JSON over HTTP")
- **Semantic Web** — Machine-readable content that describes itself
- **Local-first software** — Apps that work offline and sync via CRDTs/event logs
