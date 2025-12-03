package main

import (
	"context"
	"encoding/json"
	"fmt"
	"html"
	"html/template"
	"log"
	"net/http"
	"regexp"
	"strings"
	"time"
)

// UISpec represents a malleable UI specification embedded in note content
type UISpec struct {
	Layout   string      `json:"layout"`   // card, list, form, raw
	Title    string      `json:"title"`    // optional title
	Elements []UIElement `json:"elements"` // UI elements to render
	Actions  []UIAction  `json:"actions"`  // actions that can be triggered
	Style    string      `json:"style"`    // optional custom CSS
}

// UIElement represents a single UI component
type UIElement struct {
	Type     string      `json:"type"`               // text, heading, image, link, button, input, container, data, hr
	ID       string      `json:"id,omitempty"`       // element ID for action targeting
	Bind     string      `json:"bind,omitempty"`     // JSONPath-like binding to event data (e.g., "$.pubkey", "$.content")
	Value    string      `json:"value,omitempty"`    // static value
	Href     string      `json:"href,omitempty"`     // for links
	Src      string      `json:"src,omitempty"`      // for images
	Style    string      `json:"style,omitempty"`    // CSS classes or inline styles
	Action   string      `json:"action,omitempty"`   // action ID to trigger on click
	Children []UIElement `json:"children,omitempty"` // nested elements
	Name     string      `json:"name,omitempty"`     // for form inputs
	Label    string      `json:"label,omitempty"`    // label for buttons/inputs
}

// UIAction defines what happens when an action is triggered
type UIAction struct {
	ID      string          `json:"id"`                // action identifier
	Trigger string          `json:"trigger,omitempty"` // click, submit (default: click)
	Publish *PublishAction  `json:"publish,omitempty"` // publish a new event
	Link    string          `json:"link,omitempty"`    // navigate to URL
}

// PublishAction describes an event to publish
type PublishAction struct {
	Kind    int        `json:"kind"`
	Content string     `json:"content"`
	Tags    [][]string `json:"tags,omitempty"`
}

// MalleablePageData is passed to the malleable template
type MalleablePageData struct {
	Title       string
	EventID     string
	EventPubkey string
	EventNpub   string
	EventTime   string
	RawContent  string
	RenderedUI  template.HTML
	IsUISpec    bool
	ParseError  string
	LoggedIn    bool
	UserPubKey  string
	Relays      []string
}

// Binding context for resolving $.path references
type BindingContext struct {
	ID        string
	Pubkey    string
	Npub      string
	Content   string
	CreatedAt int64
	Kind      int
	Tags      [][]string
	// Add custom data from the UI spec itself
	Custom map[string]interface{}
}

var malleableTemplate = `<!DOCTYPE html>
<html lang="en">
<head>
  <meta charset="UTF-8">
  <meta name="viewport" content="width=device-width, initial-scale=1.0">
  <title>{{.Title}} - Malleable UI</title>
  <style>
    * { box-sizing: border-box; margin: 0; padding: 0; }
    body {
      font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, sans-serif;
      line-height: 1.6;
      color: #333;
      background: #1a1a2e;
      padding: 20px;
      min-height: 100vh;
    }
    .container {
      max-width: 800px;
      margin: 0 auto;
    }
    header {
      background: linear-gradient(135deg, #e94560 0%, #0f3460 100%);
      color: white;
      padding: 20px;
      border-radius: 8px 8px 0 0;
      text-align: center;
    }
    header h1 { font-size: 24px; margin-bottom: 4px; }
    .subtitle { opacity: 0.8; font-size: 13px; }
    nav {
      padding: 12px;
      background: #16213e;
      display: flex;
      gap: 10px;
      flex-wrap: wrap;
    }
    nav a {
      padding: 6px 14px;
      background: #e94560;
      color: white;
      text-decoration: none;
      border-radius: 4px;
      font-size: 13px;
    }
    nav a:hover { background: #ff6b6b; }
    main {
      background: #16213e;
      padding: 20px;
      border-radius: 0 0 8px 8px;
      color: #eee;
    }
    .meta {
      font-size: 12px;
      color: #888;
      margin-bottom: 16px;
      padding-bottom: 12px;
      border-bottom: 1px solid #0f3460;
    }
    .error {
      background: #ff6b6b22;
      border: 1px solid #e94560;
      color: #ff6b6b;
      padding: 12px;
      border-radius: 4px;
      margin: 12px 0;
    }
    .raw-content {
      background: #0f3460;
      padding: 12px;
      border-radius: 4px;
      font-family: monospace;
      font-size: 12px;
      white-space: pre-wrap;
      overflow-x: auto;
      color: #a0a0a0;
    }

    /* Malleable UI element styles */
    .ui-card {
      background: #1a1a2e;
      border: 1px solid #0f3460;
      border-radius: 8px;
      padding: 20px;
      margin: 12px 0;
    }
    .ui-heading {
      font-size: 22px;
      font-weight: 600;
      margin: 16px 0 8px 0;
      color: #fff;
    }
    .ui-heading:first-child { margin-top: 0; }
    .ui-text {
      margin: 8px 0;
      color: #ccc;
    }
    .ui-image {
      max-width: 100%;
      border-radius: 8px;
      margin: 12px 0;
    }
    .ui-link {
      color: #e94560;
      text-decoration: none;
    }
    .ui-link:hover { text-decoration: underline; }
    .ui-container {
      margin: 12px 0;
    }
    .ui-container.options {
      display: flex;
      gap: 10px;
      flex-wrap: wrap;
    }
    .ui-button {
      background: #e94560;
      color: white;
      border: none;
      padding: 10px 20px;
      border-radius: 4px;
      cursor: pointer;
      font-size: 14px;
      text-decoration: none;
      display: inline-block;
    }
    .ui-button:hover { background: #ff6b6b; }
    .ui-input {
      background: #0f3460;
      border: 1px solid #16213e;
      color: #eee;
      padding: 10px;
      border-radius: 4px;
      width: 100%;
      margin: 8px 0;
    }
    .ui-label {
      display: block;
      margin: 8px 0 4px 0;
      color: #888;
      font-size: 13px;
    }
    .ui-hr {
      border: none;
      border-top: 1px solid #0f3460;
      margin: 16px 0;
    }
    .ui-data {
      background: #0f3460;
      padding: 8px 12px;
      border-radius: 4px;
      font-family: monospace;
      font-size: 13px;
      color: #e94560;
      display: inline-block;
      margin: 4px 0;
    }
    .ui-data-label {
      color: #888;
      font-size: 11px;
      margin-right: 8px;
    }

    /* Action forms */
    .action-form {
      display: inline;
    }
  </style>
</head>
<body>
  <div class="container">
    <header>
      <h1>Malleable UI</h1>
      <div class="subtitle">UI driven by note content</div>
    </header>
    <nav>
      <a href="/html/timeline">Timeline</a>
      <a href="/html/malleable">Malleable Demo</a>
      {{if .EventID}}<a href="/html/thread/{{.EventID}}">View Thread</a>{{end}}
    </nav>
    <main>
      <div class="meta">
        {{if .EventID}}
        <strong>Event:</strong> {{.EventID}}<br>
        <strong>Author:</strong> {{.EventNpub}}<br>
        <strong>Time:</strong> {{.EventTime}}
        {{else}}
        <strong>Demo Mode</strong> - showing inline UI spec
        {{end}}
      </div>

      {{if .ParseError}}
      <div class="error">
        <strong>Parse Error:</strong> {{.ParseError}}
      </div>
      <div class="raw-content">{{.RawContent}}</div>
      {{else if .IsUISpec}}
      <div class="rendered-ui">
        {{.RenderedUI}}
      </div>
      {{else}}
      <div class="raw-content">{{.RawContent}}</div>
      {{end}}
    </main>
  </div>
</body>
</html>`

// htmlMalleableHandler serves malleable UI pages
func htmlMalleableHandler(w http.ResponseWriter, r *http.Request) {
	// Check if viewing a specific event or demo mode
	eventID := r.URL.Query().Get("event")

	var uiSpec *UISpec
	var rawContent string
	var parseError string
	var event *Event
	var ctx *BindingContext

	if eventID != "" {
		// Fetch the event from relays
		relays := []string{
			"wss://relay.damus.io",
			"wss://relay.nostr.band",
			"wss://relay.primal.net",
		}

		events := fetchEventByID(relays, eventID)
		if len(events) == 0 {
			http.Error(w, "Event not found", http.StatusNotFound)
			return
		}
		event = &events[0]

		rawContent = event.Content
		ctx = &BindingContext{
			ID:        event.ID,
			Pubkey:    event.PubKey,
			Content:   event.Content,
			CreatedAt: event.CreatedAt,
			Kind:      event.Kind,
			Tags:      event.Tags,
		}
		ctx.Npub, _ = encodeBech32Pubkey(event.PubKey)

		// Try to parse content as UI spec
		uiSpec, parseError = parseUISpec(event.Content)
	} else {
		// Demo mode - show a built-in example
		rawContent = getDemoUISpec()
		uiSpec, parseError = parseUISpec(rawContent)
		ctx = &BindingContext{
			ID:        "demo-event-id",
			Pubkey:    "demo-pubkey",
			Npub:      "npub1demo...",
			Content:   rawContent,
			CreatedAt: time.Now().Unix(),
			Kind:      1,
		}
	}

	// Build page data
	data := MalleablePageData{
		Title:      "Malleable UI",
		RawContent: rawContent,
	}

	if event != nil {
		data.EventID = event.ID
		data.EventPubkey = event.PubKey
		data.EventNpub, _ = encodeBech32Pubkey(event.PubKey)
		data.EventTime = time.Unix(event.CreatedAt, 0).Format("2006-01-02 15:04:05")
	}

	if parseError != "" {
		data.ParseError = parseError
		data.IsUISpec = false
	} else if uiSpec != nil {
		data.IsUISpec = true
		data.RenderedUI = renderUISpec(uiSpec, ctx)
		if uiSpec.Title != "" {
			data.Title = uiSpec.Title
		}
	}

	// Check session
	session := getSessionFromRequest(r)
	if session != nil && session.Connected {
		data.LoggedIn = true
	}

	// Render template
	tmpl, err := template.New("malleable").Parse(malleableTemplate)
	if err != nil {
		http.Error(w, "Template error: "+err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := tmpl.Execute(w, data); err != nil {
		log.Printf("Template execution error: %v", err)
	}
}

// parseUISpec attempts to parse note content as a UI specification
func parseUISpec(content string) (*UISpec, string) {
	content = strings.TrimSpace(content)

	// Content must be JSON object starting with { and containing "ui" or our UI fields
	if !strings.HasPrefix(content, "{") {
		return nil, ""
	}

	// First try parsing as wrapped {"ui": ...} format
	var wrapper struct {
		UI UISpec `json:"ui"`
	}
	if err := json.Unmarshal([]byte(content), &wrapper); err == nil && len(wrapper.UI.Elements) > 0 {
		return &wrapper.UI, ""
	}

	// Try parsing as direct UISpec (layout, elements at top level)
	var direct UISpec
	if err := json.Unmarshal([]byte(content), &direct); err == nil && len(direct.Elements) > 0 {
		return &direct, ""
	}

	// If it looks like JSON but failed to parse as UI spec
	if strings.HasPrefix(content, "{") {
		var raw map[string]interface{}
		if err := json.Unmarshal([]byte(content), &raw); err != nil {
			return nil, "Invalid JSON: " + err.Error()
		}
		// Valid JSON but not a UI spec - that's fine, just render as raw
		return nil, ""
	}

	return nil, ""
}

// renderUISpec converts a UISpec to HTML
func renderUISpec(spec *UISpec, ctx *BindingContext) template.HTML {
	var buf strings.Builder

	// Add custom styles if provided
	if spec.Style != "" {
		buf.WriteString(fmt.Sprintf("<style>%s</style>", html.EscapeString(spec.Style)))
	}

	// Wrap in layout container
	layoutClass := "ui-card"
	if spec.Layout != "" {
		layoutClass = "ui-" + spec.Layout
	}
	buf.WriteString(fmt.Sprintf(`<div class="%s">`, layoutClass))

	// Render each element
	for _, elem := range spec.Elements {
		buf.WriteString(renderUIElement(elem, ctx, spec.Actions))
	}

	buf.WriteString("</div>")

	return template.HTML(buf.String())
}

// renderUIElement renders a single UI element to HTML
func renderUIElement(elem UIElement, ctx *BindingContext, actions []UIAction) string {
	var buf strings.Builder

	// Resolve value from binding or static value
	value := elem.Value
	if elem.Bind != "" {
		value = resolveBind(elem.Bind, ctx)
	}

	// Build style/class attribute
	styleAttr := ""
	if elem.Style != "" {
		if strings.Contains(elem.Style, ":") {
			styleAttr = fmt.Sprintf(` style="%s"`, html.EscapeString(elem.Style))
		} else {
			styleAttr = fmt.Sprintf(` class="ui-%s %s"`, elem.Type, html.EscapeString(elem.Style))
		}
	}

	idAttr := ""
	if elem.ID != "" {
		idAttr = fmt.Sprintf(` id="%s"`, html.EscapeString(elem.ID))
	}

	switch elem.Type {
	case "heading", "h1", "h2", "h3":
		buf.WriteString(fmt.Sprintf(`<h2 class="ui-heading"%s%s>%s</h2>`, idAttr, styleAttr, html.EscapeString(value)))

	case "text", "p":
		buf.WriteString(fmt.Sprintf(`<p class="ui-text"%s%s>%s</p>`, idAttr, styleAttr, html.EscapeString(value)))

	case "image", "img":
		src := elem.Src
		if src == "" {
			src = value
		}
		buf.WriteString(fmt.Sprintf(`<img class="ui-image"%s%s src="%s" alt="">`, idAttr, styleAttr, html.EscapeString(src)))

	case "link", "a":
		href := elem.Href
		if href == "" {
			href = value
		}
		label := elem.Label
		if label == "" {
			label = value
		}
		buf.WriteString(fmt.Sprintf(`<a class="ui-link"%s%s href="%s">%s</a>`, idAttr, styleAttr, html.EscapeString(href), html.EscapeString(label)))

	case "button":
		label := elem.Label
		if label == "" {
			label = value
		}
		if elem.Action != "" {
			// Find the action and render as form
			for _, action := range actions {
				if action.ID == elem.Action {
					buf.WriteString(renderActionButton(label, action, ctx, idAttr, styleAttr))
					break
				}
			}
		} else if elem.Href != "" {
			buf.WriteString(fmt.Sprintf(`<a class="ui-button"%s%s href="%s">%s</a>`, idAttr, styleAttr, html.EscapeString(elem.Href), html.EscapeString(label)))
		} else {
			buf.WriteString(fmt.Sprintf(`<button class="ui-button"%s%s type="button">%s</button>`, idAttr, styleAttr, html.EscapeString(label)))
		}

	case "input":
		label := elem.Label
		name := elem.Name
		if name == "" {
			name = elem.ID
		}
		if label != "" {
			buf.WriteString(fmt.Sprintf(`<label class="ui-label" for="%s">%s</label>`, html.EscapeString(name), html.EscapeString(label)))
		}
		buf.WriteString(fmt.Sprintf(`<input class="ui-input"%s%s name="%s" type="text" value="%s">`, idAttr, styleAttr, html.EscapeString(name), html.EscapeString(value)))

	case "container", "div":
		classes := "ui-container"
		if elem.Style != "" && !strings.Contains(elem.Style, ":") {
			classes += " " + elem.Style
		}
		buf.WriteString(fmt.Sprintf(`<div class="%s"%s>`, classes, idAttr))
		for _, child := range elem.Children {
			buf.WriteString(renderUIElement(child, ctx, actions))
		}
		buf.WriteString("</div>")

	case "hr":
		buf.WriteString(`<hr class="ui-hr">`)

	case "data":
		// Display a piece of bound data with optional label
		label := elem.Label
		if label != "" {
			buf.WriteString(fmt.Sprintf(`<span class="ui-data"%s%s><span class="ui-data-label">%s</span>%s</span>`, idAttr, styleAttr, html.EscapeString(label), html.EscapeString(value)))
		} else {
			buf.WriteString(fmt.Sprintf(`<span class="ui-data"%s%s>%s</span>`, idAttr, styleAttr, html.EscapeString(value)))
		}

	case "raw":
		// Raw HTML (escaped for safety - would need explicit trust for actual raw HTML)
		buf.WriteString(fmt.Sprintf(`<div class="ui-raw"%s%s>%s</div>`, idAttr, styleAttr, html.EscapeString(value)))
	}

	return buf.String()
}

// renderActionButton renders a button that triggers a publish action
func renderActionButton(label string, action UIAction, ctx *BindingContext, idAttr, styleAttr string) string {
	if action.Publish != nil {
		// Render as form that posts to /html/react or a new malleable action endpoint
		content := resolveTemplate(action.Publish.Content, ctx)

		return fmt.Sprintf(`<form class="action-form" method="POST" action="/html/malleable-action">
			<input type="hidden" name="kind" value="%d">
			<input type="hidden" name="content" value="%s">
			<input type="hidden" name="event_id" value="%s">
			<input type="hidden" name="return_to" value="/html/malleable?event=%s">
			<button class="ui-button"%s%s type="submit">%s</button>
		</form>`, action.Publish.Kind, html.EscapeString(content), html.EscapeString(ctx.ID), html.EscapeString(ctx.ID), idAttr, styleAttr, html.EscapeString(label))
	}

	if action.Link != "" {
		href := resolveTemplate(action.Link, ctx)
		return fmt.Sprintf(`<a class="ui-button"%s%s href="%s">%s</a>`, idAttr, styleAttr, html.EscapeString(href), html.EscapeString(label))
	}

	return fmt.Sprintf(`<button class="ui-button"%s%s type="button">%s</button>`, idAttr, styleAttr, html.EscapeString(label))
}

// resolveBind resolves a JSONPath-like binding to a value
func resolveBind(bind string, ctx *BindingContext) string {
	if ctx == nil {
		return ""
	}

	// Simple path resolution (not full JSONPath, just common patterns)
	bind = strings.TrimPrefix(bind, "$.")
	bind = strings.TrimPrefix(bind, "$")

	switch strings.ToLower(bind) {
	case "id":
		return ctx.ID
	case "pubkey":
		return ctx.Pubkey
	case "npub":
		return ctx.Npub
	case "content":
		return ctx.Content
	case "createdat", "created_at", "time":
		return time.Unix(ctx.CreatedAt, 0).Format("2006-01-02 15:04:05")
	case "kind":
		return fmt.Sprintf("%d", ctx.Kind)
	}

	// Check custom data
	if ctx.Custom != nil {
		if val, ok := ctx.Custom[bind]; ok {
			switch v := val.(type) {
			case string:
				return v
			case float64:
				return fmt.Sprintf("%v", v)
			case bool:
				return fmt.Sprintf("%v", v)
			default:
				b, _ := json.Marshal(v)
				return string(b)
			}
		}
	}

	return ""
}

// resolveTemplate replaces {{$.path}} patterns with bound values
func resolveTemplate(tmpl string, ctx *BindingContext) string {
	re := regexp.MustCompile(`\{\{\s*\$\.?([a-zA-Z_][a-zA-Z0-9_]*)\s*\}\}`)
	return re.ReplaceAllStringFunc(tmpl, func(match string) string {
		// Extract the path from {{$.path}} or {{$path}}
		inner := strings.TrimPrefix(match, "{{")
		inner = strings.TrimSuffix(inner, "}}")
		inner = strings.TrimSpace(inner)
		inner = strings.TrimPrefix(inner, "$.")
		inner = strings.TrimPrefix(inner, "$")
		return resolveBind(inner, ctx)
	})
}

// getDemoUISpec returns a demo UI spec for testing
func getDemoUISpec() string {
	return `{
  "layout": "card",
  "title": "Self-Describing Poll",
  "elements": [
    {"type": "heading", "value": "What's the best approach to malleable UI?"},
    {"type": "text", "value": "This entire UI is defined in a Nostr note's content field. The server interprets the JSON and renders it as HTML - no JavaScript required!"},
    {"type": "hr"},
    {"type": "text", "value": "Vote for your preferred approach:"},
    {"type": "container", "style": "options", "children": [
      {"type": "button", "value": "Server-rendered", "action": "vote-server", "label": "Server-rendered"},
      {"type": "button", "value": "Client JS", "action": "vote-client", "label": "Client JS"},
      {"type": "button", "value": "Hybrid", "action": "vote-hybrid", "label": "Hybrid"}
    ]},
    {"type": "hr"},
    {"type": "heading", "value": "Event Data Bindings"},
    {"type": "text", "value": "UI specs can bind to the event's own data:"},
    {"type": "data", "bind": "$.id", "label": "Event ID: "},
    {"type": "data", "bind": "$.npub", "label": "Author: "},
    {"type": "data", "bind": "$.time", "label": "Created: "}
  ],
  "actions": [
    {"id": "vote-server", "publish": {"kind": 7, "content": "server-rendered", "tags": [["e", "{{$.id}}"]]}},
    {"id": "vote-client", "publish": {"kind": 7, "content": "client-js", "tags": [["e", "{{$.id}}"]]}},
    {"id": "vote-hybrid", "publish": {"kind": 7, "content": "hybrid", "tags": [["e", "{{$.id}}"]]}}
  ]
}`
}

// htmlMalleableActionHandler handles action submissions from malleable UI
func htmlMalleableActionHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	session := getSessionFromRequest(r)
	if session == nil || !session.Connected {
		http.Error(w, "Not logged in. Actions require authentication.", http.StatusUnauthorized)
		return
	}

	if err := r.ParseForm(); err != nil {
		http.Error(w, "Invalid form data", http.StatusBadRequest)
		return
	}

	kindStr := r.FormValue("kind")
	content := r.FormValue("content")
	eventID := r.FormValue("event_id")
	returnTo := r.FormValue("return_to")

	if returnTo == "" {
		returnTo = "/html/timeline"
	}

	var kind int
	fmt.Sscanf(kindStr, "%d", &kind)

	// Build tags
	var tags [][]string
	if eventID != "" {
		tags = append(tags, []string{"e", eventID})
	}

	// Request signature and publish
	log.Printf("Malleable action: kind=%d, content=%s, eventID=%s", kind, content, eventID)

	// Create unsigned event (same pattern as htmlPostNoteHandler)
	event := UnsignedEvent{
		Kind:      kind,
		Content:   content,
		Tags:      tags,
		CreatedAt: time.Now().Unix(),
	}

	// Sign via bunker
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	signedEvent, err := session.SignEvent(ctx, event)
	if err != nil {
		log.Printf("Failed to sign malleable action: %v", err)
		http.Redirect(w, r, returnTo+"&error=Failed+to+sign", http.StatusSeeOther)
		return
	}

	// Publish to relays
	relays := []string{
		"wss://relay.damus.io",
		"wss://relay.nostr.band",
		"wss://relay.primal.net",
	}

	if session.UserRelayList != nil && len(session.UserRelayList.Write) > 0 {
		relays = session.UserRelayList.Write
	}

	publishEvent(ctx, relays, signedEvent)

	log.Printf("Published malleable action: %s", signedEvent.ID)
	http.Redirect(w, r, returnTo+"&success=Action+published", http.StatusSeeOther)
}
