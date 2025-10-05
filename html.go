package main

import (
	"fmt"
	"html/template"
	"strings"
	"time"
)

var htmlTemplate = `<!DOCTYPE html>
<html lang="en">
<head>
  <meta charset="UTF-8">
  <meta name="viewport" content="width=device-width, initial-scale=1.0">
  <title>{{.Title}} - Nostr Hypermedia</title>
  <style>
    * { box-sizing: border-box; margin: 0; padding: 0; }
    body {
      font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, Oxygen, Ubuntu, Cantarell, sans-serif;
      line-height: 1.6;
      color: #333;
      background: #f5f5f5;
      padding: 20px;
    }
    .container {
      max-width: 800px;
      margin: 0 auto;
      background: white;
      border-radius: 8px;
      box-shadow: 0 2px 8px rgba(0,0,0,0.1);
      overflow: hidden;
    }
    header {
      background: linear-gradient(135deg, #667eea 0%, #764ba2 100%);
      color: white;
      padding: 30px;
      text-align: center;
    }
    header h1 { font-size: 28px; margin-bottom: 8px; }
    .subtitle { opacity: 0.9; font-size: 14px; }
    nav {
      padding: 15px;
      background: #f8f9fa;
      border-bottom: 1px solid #dee2e6;
      display: flex;
      gap: 10px;
      flex-wrap: wrap;
    }
    nav a {
      padding: 8px 16px;
      background: #667eea;
      color: white;
      text-decoration: none;
      border-radius: 4px;
      font-size: 14px;
      transition: background 0.2s;
    }
    nav a:hover { background: #5568d3; }
    main { padding: 20px; min-height: 400px; }
    .meta-info {
      background: #f8f9fa;
      padding: 12px;
      border-radius: 4px;
      font-size: 13px;
      color: #666;
      margin: 16px 0;
      display: flex;
      gap: 16px;
      justify-content: center;
      flex-wrap: wrap;
    }
    .meta-item { display: flex; align-items: center; gap: 4px; }
    .meta-label { font-weight: 600; }
    .note {
      background: white;
      border: 1px solid #e1e4e8;
      border-radius: 6px;
      padding: 16px;
      margin: 12px 0;
      transition: box-shadow 0.2s;
    }
    .note:hover { box-shadow: 0 2px 8px rgba(0,0,0,0.1); }
    .note-content {
      font-size: 15px;
      line-height: 1.6;
      margin: 12px 0;
      color: #24292e;
      white-space: pre-wrap;
      word-wrap: break-word;
    }
    .note-meta {
      display: flex;
      gap: 16px;
      font-size: 12px;
      color: #666;
      margin-top: 12px;
      padding-top: 12px;
      border-top: 1px solid #e1e4e8;
      flex-wrap: wrap;
    }
    .pubkey {
      font-family: monospace;
      font-size: 11px;
      color: #667eea;
    }
    .links {
      display: flex;
      gap: 8px;
      flex-wrap: wrap;
      margin: 12px 0;
    }
    .link {
      display: inline-flex;
      align-items: center;
      gap: 4px;
      padding: 6px 12px;
      background: white;
      border: 1px solid #667eea;
      color: #667eea;
      text-decoration: none;
      border-radius: 4px;
      font-size: 13px;
      transition: all 0.2s;
    }
    .link:hover {
      background: #667eea;
      color: white;
    }
    .pagination {
      display: flex;
      justify-content: center;
      gap: 12px;
      margin: 24px 0;
      padding: 20px 0;
      border-top: 1px solid #e1e4e8;
    }
    .actions {
      display: flex;
      gap: 12px;
      flex-wrap: wrap;
      margin-top: 12px;
    }
    .action-form {
      background: #f8f9fa;
      padding: 12px;
      border-radius: 4px;
      border: 1px solid #dee2e6;
    }
    .action-form h4 {
      font-size: 14px;
      margin-bottom: 8px;
      color: #555;
    }
    .action-field {
      margin: 8px 0;
    }
    .action-field label {
      display: block;
      font-size: 13px;
      font-weight: 600;
      color: #555;
      margin-bottom: 4px;
    }
    .action-field input,
    .action-field textarea {
      width: 100%;
      padding: 8px;
      border: 1px solid #ced4da;
      border-radius: 4px;
      font-size: 14px;
      font-family: inherit;
    }
    .action-field textarea {
      min-height: 80px;
      resize: vertical;
    }
    button[type="submit"] {
      padding: 8px 16px;
      background: #28a745;
      color: white;
      border: none;
      border-radius: 4px;
      cursor: pointer;
      font-size: 14px;
      margin-top: 8px;
    }
    button[type="submit"]:hover {
      background: #218838;
    }
    footer {
      text-align: center;
      padding: 20px;
      background: #f8f9fa;
      color: #666;
      font-size: 13px;
      border-top: 1px solid #dee2e6;
    }
  </style>
</head>
<body>
  <div class="container">
    <header>
      <h1>{{.Title}}</h1>
      <p class="subtitle">Zero-JS Hypermedia Browser</p>
    </header>

    <nav>
      <a href="/html/timeline?kinds=1&limit=20">Timeline</a>
      <a href="/html/timeline?kinds=1,7&limit=20">Timeline + Reactions</a>
      <a href="/">JS Client</a>
    </nav>

    <main>
      {{if .Meta}}
      <div class="meta-info">
        {{if .Meta.QueriedRelays}}
        <div class="meta-item">
          <span class="meta-label">Relays:</span> {{.Meta.QueriedRelays}}
        </div>
        {{end}}
        {{if .Meta.EOSE}}
        <div class="meta-item">
          <span class="meta-label">EOSE:</span> ✓
        </div>
        {{end}}
        <div class="meta-item">
          <span class="meta-label">Generated:</span> {{.Meta.GeneratedAt.Format "15:04:05"}}
        </div>
      </div>
      {{end}}

      {{range .Items}}
      <article class="note">
        <div class="note-content">{{.Content}}</div>
        <div class="note-meta">
          <span class="pubkey" title="{{.Pubkey}}">{{slice .Pubkey 0 16}}...</span>
          <span>{{formatTime .CreatedAt}}</span>
          <span>kind: {{.Kind}}</span>
          {{if .RelaysSeen}}
          <span title="{{join .RelaysSeen ", "}}">from {{len .RelaysSeen}} relay(s)</span>
          {{end}}
        </div>
        {{if .Links}}
        <div class="links">
          {{range .Links}}
          {{if not (or (contains . "self") (contains . "next") (contains . "prev"))}}
          <a href="{{.}}" class="link">{{linkName .}} →</a>
          {{end}}
          {{end}}
        </div>
        {{end}}
      </article>
      {{end}}

      {{if .Pagination}}
      <div class="pagination">
        {{if .Pagination.Prev}}
        <a href="{{.Pagination.Prev}}" class="link">← Previous</a>
        {{end}}
        {{if .Pagination.Next}}
        <a href="{{.Pagination.Next}}" class="link">Next →</a>
        {{end}}
      </div>
      {{end}}

      {{if .Actions}}
      {{range .Actions}}
      <form class="action-form" method="POST" action="{{.Href}}">
        <h4>{{.Title}}</h4>
        {{range .Fields}}
        <div class="action-field">
          <label for="{{.Name}}">{{title .Name}}</label>
          {{if eq .Name "content"}}
          <textarea name="{{.Name}}" id="{{.Name}}">{{.Value}}</textarea>
          {{else}}
          <input type="text" name="{{.Name}}" id="{{.Name}}" value="{{.Value}}">
          {{end}}
        </div>
        {{end}}
        <button type="submit">Submit</button>
      </form>
      {{end}}
      {{end}}
    </main>

    <footer>
      <p>Pure HTML hypermedia - no JavaScript required</p>
    </footer>
  </div>
</body>
</html>
`

type HTMLPageData struct {
	Title      string
	Meta       *MetaInfo
	Items      []HTMLEventItem
	Pagination *HTMLPagination
	Actions    []HTMLAction
	Links      []string
}

type HTMLEventItem struct {
	ID         string
	Kind       int
	Pubkey     string
	CreatedAt  int64
	Content    string
	RelaysSeen []string
	Links      []string
}

type HTMLPagination struct {
	Prev string
	Next string
}

type HTMLAction struct {
	Title  string
	Href   string
	Method string
	Fields []HTMLField
}

type HTMLField struct {
	Name  string
	Value string
}

func renderHTML(resp TimelineResponse, relays []string, authors []string, kinds []int, limit int) (string, error) {
	// Convert to HTML page data
	items := make([]HTMLEventItem, len(resp.Items))
	for i, item := range resp.Items {
		items[i] = HTMLEventItem{
			ID:         item.ID,
			Kind:       item.Kind,
			Pubkey:     item.Pubkey,
			CreatedAt:  item.CreatedAt,
			Content:    item.Content,
			RelaysSeen: item.RelaysSeen,
			Links:      []string{},
		}

		// Add profile link
		items[i].Links = append(items[i].Links, fmt.Sprintf("/html/profiles/%s", item.Pubkey))

		// Add thread link if reply
		for _, tag := range item.Tags {
			if len(tag) >= 2 && tag[0] == "e" {
				items[i].Links = append(items[i].Links, fmt.Sprintf("/html/threads/%s", tag[1]))
				break
			}
		}
	}

	// Build pagination
	var pagination *HTMLPagination
	if resp.Page.Next != nil {
		pagination = &HTMLPagination{
			Next: strings.Replace(*resp.Page.Next, "/timeline", "/html/timeline", 1),
		}
	}

	data := HTMLPageData{
		Title:      "Nostr Timeline",
		Meta:       &resp.Meta,
		Items:      items,
		Pagination: pagination,
		Actions:    []HTMLAction{},
	}

	// Template functions
	funcMap := template.FuncMap{
		"formatTime": func(ts int64) string {
			return time.Unix(ts, 0).Format("2006-01-02 15:04:05")
		},
		"slice": func(s string, start, end int) string {
			if end > len(s) {
				end = len(s)
			}
			return s[start:end]
		},
		"join": func(arr []string, sep string) string {
			return strings.Join(arr, sep)
		},
		"contains": func(s, substr string) bool {
			return strings.Contains(s, substr)
		},
		"linkName": func(s string) string {
			if strings.Contains(s, "/profiles/") {
				return "profile"
			}
			if strings.Contains(s, "/threads/") {
				return "thread"
			}
			return "link"
		},
		"title": func(s string) string {
			return strings.Title(strings.ReplaceAll(s, "_", " "))
		},
	}

	tmpl, err := template.New("html").Funcs(funcMap).Parse(htmlTemplate)
	if err != nil {
		return "", err
	}

	var buf strings.Builder
	if err := tmpl.Execute(&buf, data); err != nil {
		return "", err
	}

	return buf.String(), nil
}
