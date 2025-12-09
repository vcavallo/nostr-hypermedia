# HTML Client to NATEOAS: Implementation Guide

## Executive Summary

This document outlines a path to make the HTML client (`/html/*` routes) more HATEOAS-compliant by moving from hardcoded actions and navigation to a server-driven, mapping-based approach. The ultimate goal is **"Nostr as the Engine of Application State"** (NATEOAS) where the protocol data itself dictates available actions and UI capabilities.

## Current State Analysis

### Architecture Overview

The application has two distinct clients with different architectural approaches:

#### Siren Client (`/static/app.js`) - HATEOAS-Compliant ✓
- **Discovery-based navigation**: All links discovered from `links` in server responses
- **Discovery-based actions**: All forms generated from `actions` metadata
- **Generic**: Would work with any Siren API (blog, todo app, etc.)
- **Zero hardcoding**: No routes or endpoints in client code
- **State driven by**: URL + current hypermedia entity

#### HTML Client (`/html/*` routes) - Hardcoded ✗
- **Hardcoded navigation**: Routes like `/html/timeline`, `/html/profile`, `/html/thread` are fixed
- **Hardcoded actions**: Form endpoints like `/html/post`, `/html/reply`, `/html/react` are in templates
- **Nostr-specific**: Tightly coupled to Nostr protocol
- **Kind-specific rendering**: Each event kind has custom template code (see `html.go:1960-2388`)
- **State driven by**: URL query params + cookies + session

### What's Currently Driven by Nostr

**YES - Discovered from protocol:**
- ✓ Content to display (events by kind)
- ✓ Who to follow (kind 3 contact lists)
- ✓ Social graph structure (p-tags, e-tags)
- ✓ User bookmarks (kind 10003)
- ✓ Profile metadata (kind 0)
- ✓ User relay preferences (NIP-65)

**NO - Hardcoded in server:**
- ✗ Available actions (reply/react/post/bookmark/repost)
- ✗ Navigation structure (timeline/thread/profile routes)
- ✗ UI rendering per kind (mapping from kind → UI is hardcoded)
- ✗ Form shapes and fields
- ✗ Feed modes (follows/global/me)

### Code Locations

**HTML Templates & Hardcoded Actions:**
- `html.go:1891-1937` - Navigation links (hardcoded routes)
- `html.go:1941-1946` - Post form (hardcoded `/html/post`)
- `html.go:1960-2388` - Kind-specific rendering (massive if/else on `.Kind`)

**Siren Actions (Already Dynamic):**
- `siren.go:29-36` - Action structure definition
- Server already generates actions dynamically for Siren responses

**Current Kind Detection:**
```go
{{if eq .Kind 9735}}
  {{/* Zap event rendering */}}
{{else if eq .Kind 30311}}
  {{/* Live event rendering */}}
{{else if eq .Kind 10003}}
  {{/* Bookmark event rendering */}}
{{else if eq .Kind 9802}}
  {{/* Highlight rendering */}}
{{else if eq .Kind 6}}
  {{/* Repost rendering */}}
{{else if eq .Kind 20}}
  {{/* Photo rendering */}}
{{else if eq .Kind 30023}}
  {{/* Long-form article rendering */}}
{{else}}
  {{/* Default note rendering */}}
{{end}}
```

## The Problem

The HTML client's tight coupling to Nostr kinds means:
1. **Every new kind requires code changes** - Can't extend without modifying templates
2. **Actions duplicated** - Same action definitions exist in both Siren and HTML rendering
3. **No runtime flexibility** - Can't conditionally show actions based on context
4. **Not truly hypermedia-driven** - Client must know what's possible ahead of time

**This violates HATEOAS principles** where the server should tell the client what's possible.

## The Solution: Server-Driven Action Mapping

### Core Concept

Instead of hardcoding forms in templates, create a **Kind-to-Actions mapping** that:
1. Lives in a single place (DRY principle)
2. Defines what actions are available for each kind
3. Gets rendered dynamically based on context
4. Can be extended without template changes
5. Eventually could be sourced from Nostr events themselves

### Current vs. Proposed

**CURRENT (Hardcoded):**
```html
<!-- html.go template -->
<form method="POST" action="/html/reply" class="inline-form">
  <input type="hidden" name="csrf_token" value="{{.CSRFToken}}">
  <input type="hidden" name="event_id" value="{{.ID}}">
  <textarea name="content" placeholder="Write a reply..." required></textarea>
  <button type="submit">Reply</button>
</form>
```

**PROPOSED (Mapping-Based):**
```go
// actions.go - New file
type ActionTemplate struct {
    Name   string
    Title  string
    Href   string
    Method string
    Fields []FieldTemplate
}

type FieldTemplate struct {
    Name        string
    Type        string
    Placeholder string
    Required    bool
    Value       string
}

// Central mapping: Kind → Available Actions
var KindActionsMap = map[int][]ActionTemplate{
    1: { // Text note
        {
            Name:   "reply",
            Title:  "Reply",
            Href:   "/html/reply",
            Method: "POST",
            Fields: []FieldTemplate{
                {Name: "csrf_token", Type: "hidden", Value: "{{.CSRFToken}}"},
                {Name: "event_id", Type: "hidden", Value: "{{.ID}}"},
                {Name: "content", Type: "textarea", Placeholder: "Write a reply...", Required: true},
            },
        },
        {
            Name:   "react",
            Title:  "React",
            Href:   "/html/react",
            Method: "POST",
            Fields: []FieldTemplate{
                {Name: "csrf_token", Type: "hidden", Value: "{{.CSRFToken}}"},
                {Name: "event_id", Type: "hidden", Value: "{{.ID}}"},
                {Name: "content", Type: "text", Placeholder: "+", Required: true},
            },
        },
        {
            Name:   "repost",
            Title:  "Repost",
            Href:   "/html/repost",
            Method: "POST",
            Fields: []FieldTemplate{
                {Name: "csrf_token", Type: "hidden", Value: "{{.CSRFToken}}"},
                {Name: "event_id", Type: "hidden", Value: "{{.ID}}"},
            },
        },
        {
            Name:   "bookmark",
            Title:  "Bookmark",
            Href:   "/html/bookmark",
            Method: "POST",
            Fields: []FieldTemplate{
                {Name: "csrf_token", Type: "hidden", Value: "{{.CSRFToken}}"},
                {Name: "event_id", Type: "hidden", Value: "{{.ID}}"},
            },
        },
    },
    30023: { // Long-form article
        {
            Name:   "reply",
            Title:  "Comment",
            Href:   "/html/reply",
            Method: "POST",
            Fields: []FieldTemplate{
                {Name: "csrf_token", Type: "hidden", Value: "{{.CSRFToken}}"},
                {Name: "event_id", Type: "hidden", Value: "{{.ID}}"},
                {Name: "content", Type: "textarea", Placeholder: "Write a comment...", Required: true},
            },
        },
        {
            Name:   "bookmark",
            Title:  "Bookmark",
            Href:   "/html/bookmark",
            Method: "POST",
            Fields: []FieldTemplate{
                {Name: "csrf_token", Type: "hidden", Value: "{{.CSRFToken}}"},
                {Name: "event_id", Type: "hidden", Value: "{{.ID}}"},
            },
        },
        // Note: No repost for articles
    },
    // Add more kinds...
}

// Helper function to get actions with context
func GetActionsForEvent(event Event, session *Session) []ActionTemplate {
    actions := KindActionsMap[event.Kind]

    // Add contextual actions
    if session != nil {
        if session.PubkeyHex == event.Pubkey {
            // User is the author - add delete action
            actions = append(actions, ActionTemplate{
                Name:   "delete",
                Title:  "Delete",
                Href:   "/html/delete",
                Method: "POST",
                Fields: []FieldTemplate{
                    {Name: "csrf_token", Type: "hidden"},
                    {Name: "event_id", Type: "hidden", Value: event.ID},
                },
            })
        }
    }

    return actions
}
```

**TEMPLATE (Generic):**
```html
<!-- Generic template that works for any kind -->
<article class="note" data-kind="{{.Kind}}">
  <!-- Event content rendering -->
  <div class="note-content">{{.Content}}</div>

  <!-- Actions section - dynamically generated -->
  <div class="note-actions">
    {{range .AvailableActions}}
    <form method="{{.Method}}" action="{{.Href}}" class="action-form action-{{.Name}}">
      {{range .Fields}}
        {{if eq .Type "hidden"}}
          <input type="hidden" name="{{.Name}}" value="{{.Value}}">
        {{else if eq .Type "textarea"}}
          <textarea name="{{.Name}}" placeholder="{{.Placeholder}}" {{if .Required}}required{{end}}></textarea>
        {{else}}
          <input type="{{.Type}}" name="{{.Name}}" placeholder="{{.Placeholder}}" {{if .Value}}value="{{.Value}}"{{end}} {{if .Required}}required{{end}}>
        {{end}}
      {{end}}
      <button type="submit">{{.Title}}</button>
    </form>
    {{end}}
  </div>
</article>
```

## Implementation Phases

### Phase 1: Centralize the Mapping (Server-Side)
**Goal:** Single source of truth for actions, still server-controlled

**Steps:**
1. Create `actions.go` file with:
   - `ActionTemplate` struct
   - `FieldTemplate` struct
   - `KindActionsMap` variable
   - `GetActionsForEvent()` helper

2. Populate mapping for existing kinds:
   - Kind 1 (text notes)
   - Kind 6 (reposts)
   - Kind 7 (reactions)
   - Kind 20 (photos)
   - Kind 30023 (long-form)
   - Kind 30311 (live events)
   - Kind 10003 (bookmarks)

3. Update HTML handlers to inject actions:
   ```go
   // In htmlTimelineHandler or wherever events are prepared
   for i, event := range events {
       events[i].AvailableActions = GetActionsForEvent(event, session)
   }
   ```

4. Create generic action rendering in templates:
   - Replace hardcoded forms with action iteration
   - Keep existing CSS classes for compatibility

5. Test that all existing functionality still works

**Benefits:**
- Actions defined once, used everywhere
- Easier to keep Siren and HTML in sync
- Clear inventory of what's possible per kind

### Phase 2: Make It Dynamic (Runtime)
**Goal:** Actions can vary based on context without code changes

**Steps:**
1. Add contextual logic to `GetActionsForEvent()`:
   ```go
   // Author-only actions
   if session != nil && session.PubkeyHex == event.Pubkey {
       actions = append(actions, deleteAction)
       actions = append(actions, editAction)
   }

   // Logged-in actions
   if session != nil {
       actions = append(actions, dmAuthorAction)
   } else {
       // Maybe show "login to reply" instead
   }

   // Feature flags
   if appConfig.EnableZaps {
       actions = append(actions, zapAction)
   }
   ```

2. Load mapping from config file (JSON/YAML):
   ```json
   {
     "kinds": {
       "1": {
         "actions": [
           {
             "name": "reply",
             "title": "Reply",
             "href": "/html/reply",
             "method": "POST",
             "fields": [...]
           }
         ]
       }
     }
   }
   ```

3. Allow runtime updates:
   - Reload config on SIGHUP
   - Hot-reload mapping without restart

4. Add plugin/extension system:
   ```go
   // Allow external code to register actions
   func RegisterKindAction(kind int, action ActionTemplate) {
       KindActionsMap[kind] = append(KindActionsMap[kind], action)
   }
   ```

**Benefits:**
- Deploy new actions without rebuilding
- A/B test different action sets
- Per-user customization possible
- Feature flags control availability

### Phase 3: Decentralize (Nostr-Native)
**Goal:** Action definitions live on Nostr relays

**Design Options:**

#### Option A: Kind Definition Events (New NIP)
Define a new event kind (e.g., 39001) for "kind metadata":

```json
{
  "kind": 39001,
  "content": "",
  "tags": [
    ["k", "30023"],  // Defines actions for kind 30023
    ["name", "Long-form Article"],
    ["icon", "📄"],
    ["action", "reply", "POST", "/reply", "textarea:content:Write a comment..."],
    ["action", "bookmark", "POST", "/bookmark", "hidden:event_id"],
    ["action", "react", "POST", "/react", "text:content:+"],
    ["render", "article"],  // Rendering hint
    ["color", "#4A90E2"]    // UI hint
  ],
  "created_at": 1234567890,
  "pubkey": "...",
  "sig": "..."
}
```

**Tag format:**
```
["action", name, method, href, field_spec, field_spec, ...]
```
Where `field_spec` is: `type:name:placeholder:required`

#### Option B: Addressable Kind Registry
Use kind 30000 (categorized lists) or similar:

```json
{
  "kind": 30000,
  "tags": [
    ["d", "kind-definitions"],  // Unique identifier
    ["e", "<event-id-of-kind-1-definition>"],
    ["e", "<event-id-of-kind-30023-definition>"],
    ["e", "<event-id-of-kind-30311-definition>"]
  ],
  "content": "My curated kind definitions"
}
```

Each referenced event contains a full kind definition.

#### Option C: NIP-05 Verified Registries
Well-known URLs that serve kind definitions:

```
https://kind-registry.nostr.org/.well-known/nostr-kinds.json
```

```json
{
  "kinds": {
    "1": {
      "name": "Text Note",
      "actions": [...],
      "rendering": {...}
    },
    "30023": {
      "name": "Long-form Article",
      "actions": [...],
      "rendering": {...}
    }
  }
}
```

**Implementation Steps:**
1. Define NIP specification for kind definitions
2. Implement fetcher: `FetchKindDefinitionsFromNostr(relays []string)`
3. Build `KindActionsMap` from fetched events
4. Cache with TTL, refresh periodically
5. Allow local overrides for testing

**Example fetcher:**
```go
func FetchKindDefinitionsFromNostr(relays []string) (map[int][]ActionTemplate, error) {
    // Subscribe to kind 39001 events
    filter := Filter{
        Kinds: []int{39001},
        Limit: 100,
    }

    events := fetchEvents(relays, filter)

    mapping := make(map[int][]ActionTemplate)

    for _, event := range events {
        kind := extractKindFromTags(event.Tags)
        actions := parseActionTags(event.Tags)
        mapping[kind] = actions
    }

    return mapping, nil
}

func parseActionTags(tags [][]string) []ActionTemplate {
    var actions []ActionTemplate

    for _, tag := range tags {
        if tag[0] == "action" && len(tag) >= 4 {
            action := ActionTemplate{
                Name:   tag[1],
                Method: tag[2],
                Href:   tag[3],
                Fields: parseFieldSpecs(tag[4:]),
            }
            actions = append(actions, action)
        }
    }

    return actions
}
```

**Benefits:**
- Truly decentralized - no central authority
- Anyone can publish kind definitions
- Community can curate "best" definitions
- Clients can follow trusted definition publishers
- New kinds self-documenting

### Phase 4: Full NATEOAS
**Goal:** Complete hypermedia-driven architecture

**Vision:**
1. **Actions are addressable**: Each action type has a Nostr event defining it
2. **Forms reference actions**: HTML includes `naddr1...` references to action definitions
3. **Everything discoverable**: Client starts at root, follows links, discovers capabilities
4. **Protocol extensions transparent**: New NIPs automatically supported if definition exists

**Example Flow:**
1. Client fetches kind 1 event from relay
2. Event includes tag: `["action-registry", "naddr1..."]`
3. Client fetches action registry from that address
4. Registry lists available actions with full metadata
5. Client renders forms based on action metadata
6. User submits form → creates new Nostr event
7. Cycle repeats

**Pseudo-code:**
```go
// Event includes action registry reference
event := fetchEvent(relay, eventID)

// Find action registry tag
registryAddr := event.Tags.Find("action-registry")

// Fetch action definitions
actionDefs := fetchNostrAddress(registryAddr)

// Render actions
for _, actionDef := range actionDefs {
    renderActionForm(actionDef)
}
```

**Ultimate Goal:**
The server becomes a **generic Nostr hypermedia renderer** that:
- Knows nothing about specific kinds (except for basic rendering)
- Discovers everything from Nostr events
- Works with any NIP extensions automatically
- Is truly protocol-driven, not application-driven

## Technical Considerations

### Backward Compatibility
- Keep existing HTML working during migration
- Feature flag new behavior: `USE_DYNAMIC_ACTIONS=true`
- Allow fallback to hardcoded templates if mapping missing

### Performance
- Cache kind definitions in-memory
- Set reasonable TTL (1 hour? 1 day?)
- Background refresh, don't block requests
- Local override for development

### Security
- Validate action definitions before use
- Whitelist allowed action hrefs
- Sanitize all field values
- CSRF protection still applies
- Only trust signatures from verified sources

### User Experience
- Action definitions should include:
  - Human-readable titles
  - Accessibility hints (ARIA labels)
  - Icon suggestions
  - Color/styling hints
- Graceful degradation if definition missing

### Testing
- Unit tests for mapping logic
- Integration tests for action rendering
- E2E tests with various kinds
- Test with malformed definitions

## Benefits of This Approach

### For Developers
- **DRY**: Actions defined once, not duplicated
- **Maintainable**: Clear separation of concerns
- **Extensible**: Add kinds without touching templates
- **Testable**: Logic separate from rendering

### For Users
- **Consistent**: Same actions work everywhere
- **Flexible**: Can customize their experience
- **Future-proof**: New features appear automatically
- **Trustworthy**: Can verify action definitions

### For the Nostr Ecosystem
- **Interoperable**: Clients can share definitions
- **Democratic**: Anyone can propose actions
- **Evolutionary**: Protocol can grow organically
- **Discoverable**: New capabilities self-documenting

## Next Steps

### Immediate (Phase 1)
1. [ ] Create `actions.go` file with structs
2. [ ] Define `KindActionsMap` for existing kinds (1, 6, 7, 20, 30023, 30311, 10003)
3. [ ] Implement `GetActionsForEvent()` function
4. [ ] Update HTML handlers to inject `AvailableActions` into template data
5. [ ] Create generic action rendering template partial
6. [ ] Replace hardcoded forms in one template (start with kind 1)
7. [ ] Test thoroughly
8. [ ] Migrate remaining kinds
9. [ ] Remove old hardcoded forms

### Near-term (Phase 2)
1. [ ] Add contextual action logic (author-only, logged-in-only)
2. [ ] Create JSON config file for action definitions
3. [ ] Implement config loader
4. [ ] Add hot-reload capability
5. [ ] Document action definition format

### Long-term (Phase 3)
1. [ ] Draft NIP for kind definition events
2. [ ] Get community feedback
3. [ ] Implement Nostr fetcher for definitions
4. [ ] Add caching layer
5. [ ] Test with community-published definitions
6. [ ] Publish your own kind definitions to Nostr

### Future (Phase 4)
1. [ ] Make server fully generic
2. [ ] Remove kind-specific rendering where possible
3. [ ] Implement full hypermedia browsing
4. [ ] Document "NATEOAS pattern" for other developers

## Example Migration: Kind 1 (Text Note)

### Before (Hardcoded)
```html
<!-- html.go:1941-1946 -->
<form method="POST" action="/html/post" class="post-form">
  <input type="hidden" name="csrf_token" value="{{.CSRFToken}}">
  <textarea name="content" placeholder="What's on your mind?" required></textarea>
  <button type="submit">Post</button>
</form>

<!-- Later in template, per-note actions -->
<form method="POST" action="/html/reply" class="inline-form">
  <input type="hidden" name="csrf_token" value="{{.CSRFToken}}">
  <input type="hidden" name="event_id" value="{{.ID}}">
  <textarea name="content" placeholder="Write a reply..." required></textarea>
  <button type="submit">Reply</button>
</form>

<form method="POST" action="/html/react" class="inline-form">
  <input type="hidden" name="csrf_token" value="{{.CSRFToken}}">
  <input type="hidden" name="event_id" value="{{.ID}}">
  <input type="text" name="content" value="+" style="width: 50px;">
  <button type="submit">React</button>
</form>
```

### After (Mapping-Based)
```go
// actions.go
var KindActionsMap = map[int][]ActionTemplate{
    1: {
        {
            Name: "reply",
            Title: "Reply",
            Href: "/html/reply",
            Method: "POST",
            Fields: []FieldTemplate{
                {Name: "csrf_token", Type: "hidden"},
                {Name: "event_id", Type: "hidden"},
                {Name: "content", Type: "textarea", Placeholder: "Write a reply...", Required: true},
            },
        },
        {
            Name: "react",
            Title: "React",
            Href: "/html/react",
            Method: "POST",
            Fields: []FieldTemplate{
                {Name: "csrf_token", Type: "hidden"},
                {Name: "event_id", Type: "hidden"},
                {Name: "content", Type: "text", Value: "+"},
            },
        },
        {
            Name: "repost",
            Title: "Repost",
            Href: "/html/repost",
            Method: "POST",
            Fields: []FieldTemplate{
                {Name: "csrf_token", Type: "hidden"},
                {Name: "event_id", Type: "hidden"},
            },
        },
    },
}
```

```html
<!-- Template - generic.html -->
{{range .AvailableActions}}
<form method="{{.Method}}" action="{{.Href}}" class="action-form action-{{.Name}}">
  {{range .Fields}}
    {{if eq .Type "hidden"}}
      <input type="hidden" name="{{.Name}}" value="{{renderValue .}}">
    {{else if eq .Type "textarea"}}
      <textarea name="{{.Name}}" placeholder="{{.Placeholder}}" {{if .Required}}required{{end}}></textarea>
    {{else}}
      <input type="{{.Type}}" name="{{.Name}}" {{if .Placeholder}}placeholder="{{.Placeholder}}"{{end}} {{if .Value}}value="{{.Value}}"{{end}} {{if .Required}}required{{end}}>
    {{end}}
  {{end}}
  <button type="submit">{{.Title}}</button>
</form>
{{end}}
```

```go
// html_handlers.go
func htmlTimelineHandler(w http.ResponseWriter, r *http.Request) {
    // ... existing code ...

    // Prepare events with actions
    for i := range events {
        events[i].AvailableActions = GetActionsForEvent(events[i], session)
    }

    // ... render template ...
}
```

## Files to Create/Modify

### New Files
- `actions.go` - Action mapping and logic
- `actions.json` - (Phase 2) Config-driven mapping
- `actions_test.go` - Unit tests
- `nostr_kind_fetcher.go` - (Phase 3) Fetch definitions from Nostr

### Modified Files
- `html.go` - Update templates to use generic action rendering
- `html_handlers.go` - Inject `AvailableActions` into template data
- `siren.go` - Could potentially share action definitions
- `main.go` - Add config loading for action mappings

### Template Changes
- Extract hardcoded forms into generic action renderer
- Keep CSS classes for styling compatibility
- Add data attributes for JavaScript hooks

## Questions to Resolve

1. **Should actions be global or per-kind?**
   - Per-kind allows different actions for different content types
   - Global with context filtering is more flexible
   - Hybrid: global actions with kind-specific overrides?

2. **How to handle action ordering/grouping?**
   - Some actions are primary (reply, react)
   - Others are secondary (bookmark, delete)
   - Use `priority` or `group` field in ActionTemplate?

3. **Client-side vs server-side rendering?**
   - Full SSR: Server renders all forms
   - Hybrid: Server provides data attributes, JS enhances
   - Progressive enhancement preferred for accessibility

4. **Caching strategy for Nostr-sourced definitions?**
   - Cache per-relay or aggregate?
   - TTL duration?
   - Invalidation strategy?

5. **Versioning of action definitions?**
   - What if definition format changes?
   - Backward compatibility strategy?
   - Use `version` tag in events?

6. **Trust model for external definitions?**
   - Only from followed users?
   - NIP-05 verified sources only?
   - Community reputation system?
   - Local whitelist?

## References

### Current Codebase
- `html.go:1891-2388` - HTML templates with hardcoded actions
- `siren.go:29-36` - Siren action structure (already dynamic)
- `static/app.js:730-798` - Client-side action rendering (good example)

### Nostr Protocol
- NIP-01: Basic protocol flow
- NIP-05: Mapping Nostr keys to DNS
- NIP-65: Relay list metadata
- (Future) NIP-XX: Kind definition events

### HATEOAS Resources
- Roy Fielding's dissertation on REST
- Siren specification: https://github.com/kevinswiber/siren
- Hypermedia APIs vs REST APIs

## Success Criteria

You'll know this is working when:
1. [ ] Can add new kind without modifying templates
2. [ ] Actions automatically appear for new kinds
3. [ ] Same action definitions used in both Siren and HTML
4. [ ] Can test different action sets via config
5. [ ] (Phase 3) Action definitions fetched from Nostr relays
6. [ ] HTML client as flexible as Siren client
7. [ ] Zero hardcoded action endpoints in templates

## Conclusion

This migration from hardcoded actions to a mapping-based, eventually Nostr-native approach will make your HTML client truly hypermedia-driven. It aligns with HATEOAS principles and achieves "Nostr as the Engine of Application State."

The phased approach allows incremental progress:
- Phase 1: Quick wins, better maintainability
- Phase 2: Runtime flexibility, easier deployment
- Phase 3: Decentralization, community-driven
- Phase 4: Full NATEOAS, protocol-driven application

Start with Phase 1, validate the approach, then decide whether to continue to decentralization. Even stopping at Phase 2 provides significant benefits while keeping the implementation practical.
