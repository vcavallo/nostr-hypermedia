package main

import (
	"encoding/hex"
	"fmt"
	"html"
	"html/template"
	"regexp"
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
    .note-content img {
      max-width: 100%;
      border-radius: 8px;
      margin: 8px 0;
      display: block;
    }
    .note-content a {
      color: #667eea;
      text-decoration: none;
    }
    .note-content a:hover {
      text-decoration: underline;
    }
    .note-author {
      display: flex;
      align-items: center;
      gap: 10px;
      margin-bottom: 12px;
    }
    .author-avatar {
      width: 40px;
      height: 40px;
      border-radius: 50%;
      object-fit: cover;
      border: 2px solid #e1e4e8;
    }
    .author-info {
      display: flex;
      flex-direction: column;
      gap: 2px;
    }
    .author-name {
      font-weight: 600;
      font-size: 15px;
      color: #24292e;
    }
    .author-nip05 {
      font-size: 12px;
      color: #667eea;
    }
    .note-reactions {
      display: flex;
      gap: 8px;
      flex-wrap: wrap;
      margin: 12px 0;
      padding: 8px 0;
      border-top: 1px solid #e1e4e8;
    }
    .reaction-badge {
      display: inline-flex;
      align-items: center;
      gap: 4px;
      padding: 4px 10px;
      background: #f0f0f0;
      border-radius: 16px;
      font-size: 13px;
      color: #555;
    }
    .reaction-badge:first-child {
      background: #e7eaff;
      color: #667eea;
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
      {{if .LoggedIn}}
      <a href="/html/logout">Logout</a>
      {{else}}
      <a href="/html/login">Login</a>
      {{end}}
    </nav>
		<div style="padding:12px 15px;background:#f8f9fa;border-bottom:1px solid #dee2e6;display:flex;align-items:center;gap:16px;flex-wrap:wrap;">
		{{if .LoggedIn}}
		<div style="display:flex;gap:4px;">
		<a href="?kinds=1&limit=20&feed=follows{{if not .ShowReactions}}&fast=1{{end}}" style="padding:6px 12px;border-radius:4px;text-decoration:none;font-size:13px;{{if eq .FeedMode "follows"}}background:#667eea;color:white;{{else}}background:#e9ecef;color:#495057;{{end}}">Follows</a>
		<a href="?kinds=1&limit=20&feed=global{{if not .ShowReactions}}&fast=1{{end}}" style="padding:6px 12px;border-radius:4px;text-decoration:none;font-size:13px;{{if eq .FeedMode "global"}}background:#667eea;color:white;{{else}}background:#e9ecef;color:#495057;{{end}}">Global</a>
		</div>
		{{end}}
		<div style="display:flex;align-items:center;gap:8px;font-size:13px;color:#666;">
		<span>Reactions:</span>
		<a href="?kinds=1&limit=20&feed={{.FeedMode}}{{if .ShowReactions}}&fast=1{{end}}" style="text-decoration:none;display:inline-flex;align-items:center;">
		<span style="display:inline-block;width:36px;height:20px;background:{{if .ShowReactions}}#667eea{{else}}#ccc{{end}};border-radius:10px;position:relative;">
		<span style="position:absolute;top:2px;{{if .ShowReactions}}right:2px{{else}}left:2px{{end}};width:16px;height:16px;background:white;border-radius:50%;"></span>
		</span>
		</a>
		<span style="color:#999;font-size:12px;">{{if .ShowReactions}}(slower){{else}}(faster){{end}}</span>
		</div>
		</div>
		{{if .ActiveRelays}}
		<details style="margin-bottom:16px;font-size:12px;color:#666;">
		<summary style="cursor:pointer;user-select:none;">Using {{len .ActiveRelays}} relay{{if gt (len .ActiveRelays) 1}}s{{end}}</summary>
		<ul style="margin:8px 0 0 20px;padding:0;list-style:disc;">
		{{range .ActiveRelays}}
		<li style="margin:2px 0;font-family:monospace;font-size:11px;color:#667eea;">{{.}}</li>
		{{end}}
		</ul>
		</details>
		{{end}}

    <main>
      {{if .Error}}
      <div style="background:#fee2e2;color:#dc2626;border:1px solid #fecaca;padding:12px;border-radius:4px;margin-bottom:16px;">{{.Error}}</div>
      {{end}}
      {{if .Success}}
      <div style="background:#dcfce7;color:#16a34a;border:1px solid #bbf7d0;padding:12px;border-radius:4px;margin-bottom:16px;">{{.Success}}</div>
      {{end}}

      {{if .LoggedIn}}
      <form method="POST" action="/html/post" style="background:#f8f9fa;padding:16px;border-radius:8px;border:1px solid #dee2e6;margin-bottom:20px;">
        <div style="margin-bottom:10px;font-size:13px;color:#666;">
          Posting as: <span style="font-family:monospace;color:#667eea;">{{slice .UserPubKey 0 12}}...</span>
        </div>
        <textarea name="content" placeholder="What's on your mind?" required
                  style="width:100%;padding:12px;border:1px solid #ced4da;border-radius:4px;font-size:14px;font-family:inherit;min-height:80px;resize:vertical;margin-bottom:10px;"></textarea>
        <button type="submit" style="padding:10px 20px;background:linear-gradient(135deg,#667eea 0%,#764ba2 100%);color:white;border:none;border-radius:4px;font-size:14px;font-weight:600;cursor:pointer;">Post Note</button>
      </form>
      {{end}}

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
        <div class="note-author">
          <a href="/html/profile/{{.Pubkey}}" style="text-decoration:none;">
          {{if and .AuthorProfile .AuthorProfile.Picture}}
          <img class="author-avatar" src="{{.AuthorProfile.Picture}}" alt="avatar" onerror="this.style.display='none'">
          {{end}}
          </a>
          <div class="author-info">
            <a href="/html/profile/{{.Pubkey}}" style="text-decoration:none;">
            {{if .AuthorProfile}}
            {{if or .AuthorProfile.DisplayName .AuthorProfile.Name}}
            <span class="author-name">{{if .AuthorProfile.DisplayName}}{{.AuthorProfile.DisplayName}}{{else}}{{.AuthorProfile.Name}}{{end}}</span>
            {{end}}
            {{if .AuthorProfile.Nip05}}
            <span class="author-nip05">{{.AuthorProfile.Nip05}}</span>
            {{end}}
            {{end}}
            <span class="pubkey" title="{{.Pubkey}}">{{.NpubShort}}</span>
            </a>
          </div>
        </div>
        <div class="note-content">{{.ContentHTML}}</div>
        <div class="note-reactions">
          {{if gt .ReplyCount 0}}
          <a href="/html/thread/{{.ID}}" class="reaction-badge" style="text-decoration:none;">replies {{.ReplyCount}}</a>
          {{end}}
          {{if and .Reactions (gt .Reactions.Total 0)}}
          {{range $type, $count := .Reactions.ByType}}
          <span class="reaction-badge">{{$type}} {{$count}}</span>
          {{end}}
          {{end}}
          {{if $.LoggedIn}}
          <form method="POST" action="/html/react" style="display:inline;margin:0;">
            <input type="hidden" name="event_id" value="{{.ID}}">
            <input type="hidden" name="event_pubkey" value="{{.Pubkey}}">
            <input type="hidden" name="return_url" value="{{$.CurrentURL}}">
            <input type="hidden" name="reaction" value="+">
            <button type="submit" style="background:#f0f0f0;border:none;border-radius:16px;padding:4px 10px;font-size:13px;cursor:pointer;color:#555;">+</button>
          </form>
          {{end}}
        </div>
        <div class="note-meta">
          <span>{{formatTime .CreatedAt}}</span>
          {{if .RelaysSeen}}
          <span title="{{join .RelaysSeen ", "}}">from {{len .RelaysSeen}} relay(s)</span>
          {{end}}
          <a href="/html/thread/{{.ID}}" style="color:#667eea;text-decoration:none;margin-left:auto;">Reply →</a>
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
	Title         string
	Meta          *MetaInfo
	Items         []HTMLEventItem
	Pagination    *HTMLPagination
	Actions       []HTMLAction
	Links         []string
	LoggedIn      bool
	UserPubKey    string
	Error         string
	Success       string
	ShowReactions bool     // Whether reactions are being fetched (slow mode)
	FeedMode      string   // "follows" or "global"
	ActiveRelays  []string // Relays being used for this request
	CurrentURL    string   // Current page URL for reaction redirects
}

type HTMLEventItem struct {
	ID            string
	Kind          int
	Pubkey        string
	Npub          string // Bech32-encoded npub format
	NpubShort     string // Short display format (npub1abc...xyz)
	CreatedAt     int64
	Content       string
	ContentHTML   template.HTML
	RelaysSeen    []string
	Links         []string
	AuthorProfile *ProfileInfo
	Reactions     *ReactionsSummary
	ReplyCount    int
	ParentID      string // ID of parent event if this is a reply
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

// Image extension regex
var imageExtRegex = regexp.MustCompile(`(?i)\.(jpg|jpeg|png|gif|webp)(\?.*)?$`)
var urlRegex = regexp.MustCompile(`https?://[^\s<>"]+`)

// formatNpubShort creates a shortened npub display like "npub1abc...xyz"
func formatNpubShort(npub string) string {
	if len(npub) <= 16 {
		return npub
	}
	return npub[:9] + "..." + npub[len(npub)-4:]
}

// processContentToHTML converts plain text content to HTML with images and links
func processContentToHTML(content string) template.HTML {
	// First escape the content
	escaped := html.EscapeString(content)

	// Find all URLs and replace them
	result := urlRegex.ReplaceAllStringFunc(escaped, func(url string) string {
		// Unescape the URL (it was escaped above)
		url = html.UnescapeString(url)
		if imageExtRegex.MatchString(url) {
			return fmt.Sprintf(`<img src="%s" alt="image" loading="lazy">`, html.EscapeString(url))
		}
		return fmt.Sprintf(`<a href="%s" target="_blank" rel="noopener">%s</a>`, html.EscapeString(url), html.EscapeString(url))
	})

	return template.HTML(result)
}

func renderHTML(resp TimelineResponse, relays []string, authors []string, kinds []int, limit int, session *BunkerSession, errorMsg, successMsg string, showReactions bool, feedMode string, currentURL string) (string, error) {
	// Convert to HTML page data
	items := make([]HTMLEventItem, len(resp.Items))
	for i, item := range resp.Items {
		// Generate npub from hex pubkey
		npub, _ := encodeBech32Pubkey(item.Pubkey)

		items[i] = HTMLEventItem{
			ID:            item.ID,
			Kind:          item.Kind,
			Pubkey:        item.Pubkey,
			Npub:          npub,
			NpubShort:     formatNpubShort(npub),
			CreatedAt:     item.CreatedAt,
			Content:       item.Content,
			ContentHTML:   processContentToHTML(item.Content),
			RelaysSeen:    item.RelaysSeen,
			Links:         []string{},
			AuthorProfile: item.AuthorProfile,
			Reactions:     item.Reactions,
			ReplyCount:    item.ReplyCount,
		}

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
		// Page.Next is already the HTML path from html_handlers.go
		pagination = &HTMLPagination{
			Next: *resp.Page.Next,
		}
	}

	data := HTMLPageData{
		Title:         "Nostr Timeline",
		Meta:          &resp.Meta,
		Items:         items,
		Pagination:    pagination,
		Actions:       []HTMLAction{},
		Error:         errorMsg,
		Success:       successMsg,
		ShowReactions: showReactions,
		FeedMode:      feedMode,
		ActiveRelays:  relays,
		CurrentURL:    currentURL,
	}

	// Add session info if logged in
	if session != nil && session.Connected {
		data.LoggedIn = true
		data.UserPubKey = hex.EncodeToString(session.UserPubKey)
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
		"gt": func(a, b int) bool {
			return a > b
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

var htmlThreadTemplate = `<!DOCTYPE html>
<html lang="en">
<head>
  <meta charset="UTF-8">
  <meta name="viewport" content="width=device-width, initial-scale=1.0">
  <title>Thread - Nostr Hypermedia</title>
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
    .note.root {
      border: 2px solid #667eea;
      background: #f8f9ff;
    }
    .note-content {
      font-size: 15px;
      line-height: 1.6;
      margin: 12px 0;
      color: #24292e;
      white-space: pre-wrap;
      word-wrap: break-word;
    }
    .note-content img {
      max-width: 100%;
      border-radius: 8px;
      margin: 8px 0;
      display: block;
    }
    .note-content a {
      color: #667eea;
      text-decoration: none;
    }
    .note-content a:hover {
      text-decoration: underline;
    }
    .note-author {
      display: flex;
      align-items: center;
      gap: 10px;
      margin-bottom: 12px;
    }
    .author-avatar {
      width: 40px;
      height: 40px;
      border-radius: 50%;
      object-fit: cover;
      border: 2px solid #e1e4e8;
    }
    .author-info {
      display: flex;
      flex-direction: column;
      gap: 2px;
    }
    .author-name {
      font-weight: 600;
      font-size: 15px;
      color: #24292e;
    }
    .author-nip05 {
      font-size: 12px;
      color: #667eea;
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
    .replies-section {
      margin-top: 24px;
      padding-top: 20px;
      border-top: 2px solid #e1e4e8;
    }
    .replies-section h3 {
      color: #555;
      font-size: 16px;
      margin-bottom: 16px;
    }
    .reply {
      margin-left: 20px;
      border-left: 3px solid #e1e4e8;
      padding-left: 16px;
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
      <h1>Thread</h1>
      <p class="subtitle">Zero-JS Hypermedia Browser</p>
    </header>

    <nav>
      <a href="/html/timeline?kinds=1&limit=20">← Timeline</a>
    </nav>

    <main>
      {{if .Meta}}
      <div class="meta-info">
        {{if .Meta.QueriedRelays}}
        <div class="meta-item">
          <span class="meta-label">Relays:</span> {{.Meta.QueriedRelays}}
        </div>
        {{end}}
        <div class="meta-item">
          <span class="meta-label">Replies:</span> {{len .Replies}}
        </div>
        <div class="meta-item">
          <span class="meta-label">Generated:</span> {{.Meta.GeneratedAt.Format "15:04:05"}}
        </div>
      </div>
      {{end}}

      {{if .Root}}
      <article class="note root">
        <div class="note-author">
          <a href="/html/profile/{{.Root.Pubkey}}" style="text-decoration:none;">
          {{if and .Root.AuthorProfile .Root.AuthorProfile.Picture}}
          <img class="author-avatar" src="{{.Root.AuthorProfile.Picture}}" alt="avatar" onerror="this.style.display='none'">
          {{end}}
          </a>
          <div class="author-info">
            <a href="/html/profile/{{.Root.Pubkey}}" style="text-decoration:none;">
            {{if .Root.AuthorProfile}}
            {{if or .Root.AuthorProfile.DisplayName .Root.AuthorProfile.Name}}
            <span class="author-name">{{if .Root.AuthorProfile.DisplayName}}{{.Root.AuthorProfile.DisplayName}}{{else}}{{.Root.AuthorProfile.Name}}{{end}}</span>
            {{end}}
            {{if .Root.AuthorProfile.Nip05}}
            <span class="author-nip05">{{.Root.AuthorProfile.Nip05}}</span>
            {{end}}
            {{end}}
            <span class="pubkey" title="{{.Root.Pubkey}}">{{.Root.NpubShort}}</span>
            </a>
          </div>
        </div>
        <div class="note-content">{{.Root.ContentHTML}}</div>
        {{if .LoggedIn}}
        <div style="margin:12px 0;padding:8px 0;border-top:1px solid #e1e4e8;">
          <form method="POST" action="/html/react" style="display:inline;margin:0;">
            <input type="hidden" name="event_id" value="{{.Root.ID}}">
            <input type="hidden" name="event_pubkey" value="{{.Root.Pubkey}}">
            <input type="hidden" name="return_url" value="{{$.CurrentURL}}">
            <input type="hidden" name="reaction" value="+">
            <button type="submit" style="background:#f0f0f0;border:none;border-radius:16px;padding:4px 10px;font-size:13px;cursor:pointer;color:#555;">+</button>
          </form>
        </div>
        {{end}}
        <div class="note-meta">
          <span>{{formatTime .Root.CreatedAt}}</span>
          {{if .Root.RelaysSeen}}
          <span title="{{join .Root.RelaysSeen ", "}}">from {{len .Root.RelaysSeen}} relay(s)</span>
          {{end}}
          {{if .Root.ParentID}}
          <a href="/html/thread/{{.Root.ParentID}}" style="color:#667eea;text-decoration:none;">↑ Parent</a>
          {{end}}
          {{if gt .Root.ReplyCount 0}}
          <a href="/html/thread/{{.Root.ID}}" style="color:#667eea;text-decoration:none;">{{.Root.ReplyCount}} replies ↓</a>
          {{end}}
        </div>
      </article>

      {{if .LoggedIn}}
      <form method="POST" action="/html/reply" style="background:#f0f4ff;padding:16px;border-radius:8px;border:1px solid #667eea;margin:16px 0;">
        <input type="hidden" name="reply_to" value="{{.Root.ID}}">
        <input type="hidden" name="reply_to_pubkey" value="{{.Root.Pubkey}}">
        <div style="margin-bottom:10px;font-size:13px;color:#666;">
          Replying as: <span style="font-family:monospace;color:#667eea;">{{slice .UserPubKey 0 12}}...</span>
        </div>
        <textarea name="content" placeholder="Write a reply..." required
                  style="width:100%;padding:12px;border:1px solid #ced4da;border-radius:4px;font-size:14px;font-family:inherit;min-height:60px;resize:vertical;margin-bottom:10px;"></textarea>
        <button type="submit" style="padding:10px 20px;background:linear-gradient(135deg,#667eea 0%,#764ba2 100%);color:white;border:none;border-radius:4px;font-size:14px;font-weight:600;cursor:pointer;">Reply</button>
      </form>
      {{else}}
      <div style="background:#f8f9fa;padding:12px;border-radius:8px;border:1px solid #dee2e6;margin:16px 0;text-align:center;color:#666;font-size:14px;">
        <a href="/html/login" style="color:#667eea;">Login</a> to reply
      </div>
      {{end}}
      {{end}}

      {{if .Replies}}
      <div class="replies-section">
        <h3>Replies ({{len .Replies}})</h3>
        {{range .Replies}}
        <article class="note reply">
          <div class="note-author">
            <a href="/html/profile/{{.Pubkey}}" style="text-decoration:none;">
            {{if and .AuthorProfile .AuthorProfile.Picture}}
            <img class="author-avatar" src="{{.AuthorProfile.Picture}}" alt="avatar" onerror="this.style.display='none'">
            {{end}}
            </a>
            <div class="author-info">
              <a href="/html/profile/{{.Pubkey}}" style="text-decoration:none;">
              {{if .AuthorProfile}}
              {{if or .AuthorProfile.DisplayName .AuthorProfile.Name}}
              <span class="author-name">{{if .AuthorProfile.DisplayName}}{{.AuthorProfile.DisplayName}}{{else}}{{.AuthorProfile.Name}}{{end}}</span>
              {{end}}
              {{if .AuthorProfile.Nip05}}
              <span class="author-nip05">{{.AuthorProfile.Nip05}}</span>
              {{end}}
              {{end}}
              <span class="pubkey" title="{{.Pubkey}}">{{.NpubShort}}</span>
              </a>
            </div>
          </div>
          <div class="note-content">{{.ContentHTML}}</div>
          {{if $.LoggedIn}}
          <div style="margin:8px 0;">
            <form method="POST" action="/html/react" style="display:inline;margin:0;">
              <input type="hidden" name="event_id" value="{{.ID}}">
              <input type="hidden" name="event_pubkey" value="{{.Pubkey}}">
              <input type="hidden" name="return_url" value="{{$.CurrentURL}}">
              <input type="hidden" name="reaction" value="+">
              <button type="submit" style="background:#f0f0f0;border:none;border-radius:16px;padding:4px 10px;font-size:13px;cursor:pointer;color:#555;">+</button>
            </form>
          </div>
          {{end}}
          <div class="note-meta">
            <span>{{formatTime .CreatedAt}}</span>
            {{if .RelaysSeen}}
            <span title="{{join .RelaysSeen ", "}}">from {{len .RelaysSeen}} relay(s)</span>
            {{end}}
            {{if .ParentID}}
            <a href="/html/thread/{{.ParentID}}" style="color:#667eea;text-decoration:none;">↑ Parent</a>
            {{end}}
            {{if gt .ReplyCount 0}}
            <a href="/html/thread/{{.ID}}" style="color:#667eea;text-decoration:none;">{{.ReplyCount}} replies ↓</a>
            {{end}}
            <a href="/html/thread/{{.ID}}" style="color:#667eea;text-decoration:none;">Reply</a>
          </div>
        </article>
        {{end}}
      </div>
      {{end}}
    </main>

    <footer>
      <p>Pure HTML hypermedia - no JavaScript required</p>
    </footer>
  </div>
</body>
</html>
`

type HTMLThreadData struct {
	Title      string
	Meta       *MetaInfo
	Root       *HTMLEventItem
	Replies    []HTMLEventItem
	LoggedIn   bool
	UserPubKey string
	CurrentURL string
}

// extractParentID extracts the parent event ID from the "e" tags
// The parent is typically the last "e" tag, or the one marked as "reply"
func extractParentID(tags [][]string) string {
	var parentID string
	for _, tag := range tags {
		if len(tag) >= 2 && tag[0] == "e" {
			// Check if this tag has a marker
			if len(tag) >= 4 && tag[3] == "reply" {
				return tag[1] // This is explicitly marked as the reply target
			}
			// Otherwise, use the last "e" tag as the parent
			parentID = tag[1]
		}
	}
	return parentID
}

func renderThreadHTML(resp ThreadResponse, session *BunkerSession, currentURL string) (string, error) {
	// Generate npub for root author
	rootNpub, _ := encodeBech32Pubkey(resp.Root.Pubkey)

	// Convert root to HTML item
	root := &HTMLEventItem{
		ID:            resp.Root.ID,
		Kind:          resp.Root.Kind,
		Pubkey:        resp.Root.Pubkey,
		Npub:          rootNpub,
		NpubShort:     formatNpubShort(rootNpub),
		CreatedAt:     resp.Root.CreatedAt,
		Content:       resp.Root.Content,
		ContentHTML:   processContentToHTML(resp.Root.Content),
		RelaysSeen:    resp.Root.RelaysSeen,
		AuthorProfile: resp.Root.AuthorProfile,
		ReplyCount:    resp.Root.ReplyCount,
		ParentID:      extractParentID(resp.Root.Tags),
	}

	// Convert replies to HTML items
	replies := make([]HTMLEventItem, len(resp.Replies))
	for i, item := range resp.Replies {
		npub, _ := encodeBech32Pubkey(item.Pubkey)
		replies[i] = HTMLEventItem{
			ID:            item.ID,
			Kind:          item.Kind,
			Pubkey:        item.Pubkey,
			Npub:          npub,
			NpubShort:     formatNpubShort(npub),
			CreatedAt:     item.CreatedAt,
			Content:       item.Content,
			ContentHTML:   processContentToHTML(item.Content),
			RelaysSeen:    item.RelaysSeen,
			AuthorProfile: item.AuthorProfile,
			ReplyCount:    item.ReplyCount,
			ParentID:      extractParentID(item.Tags),
		}
	}

	data := HTMLThreadData{
		Title:      "Thread",
		Meta:       &resp.Meta,
		Root:       root,
		Replies:    replies,
		CurrentURL: currentURL,
	}

	// Add session info
	if session != nil && session.Connected {
		data.LoggedIn = true
		data.UserPubKey = hex.EncodeToString(session.UserPubKey)
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
	}

	tmpl, err := template.New("thread").Funcs(funcMap).Parse(htmlThreadTemplate)
	if err != nil {
		return "", err
	}

	var buf strings.Builder
	if err := tmpl.Execute(&buf, data); err != nil {
		return "", err
	}

	return buf.String(), nil
}

var htmlProfileTemplate = `<!DOCTYPE html>
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
    .profile-header {
      display: flex;
      align-items: flex-start;
      gap: 20px;
      padding: 24px;
      background: #f8f9fa;
      border-radius: 8px;
      margin-bottom: 24px;
    }
    .profile-avatar {
      width: 80px;
      height: 80px;
      border-radius: 50%;
      object-fit: cover;
      border: 3px solid #667eea;
      flex-shrink: 0;
    }
    .profile-info {
      flex: 1;
    }
    .profile-name {
      font-size: 24px;
      font-weight: 700;
      color: #24292e;
      margin-bottom: 4px;
    }
    .profile-nip05 {
      font-size: 14px;
      color: #667eea;
      margin-bottom: 8px;
    }
    .profile-npub {
      font-family: monospace;
      font-size: 12px;
      color: #666;
      background: #e9ecef;
      padding: 4px 8px;
      border-radius: 4px;
      display: inline-block;
      margin-bottom: 8px;
    }
    .profile-about {
      font-size: 14px;
      color: #555;
      line-height: 1.5;
    }
    .notes-section h3 {
      color: #555;
      font-size: 18px;
      margin-bottom: 16px;
      padding-bottom: 8px;
      border-bottom: 2px solid #e1e4e8;
    }
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
    .note-content img {
      max-width: 100%;
      border-radius: 8px;
      margin: 8px 0;
      display: block;
    }
    .note-content a {
      color: #667eea;
      text-decoration: none;
    }
    .note-content a:hover {
      text-decoration: underline;
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
    .pagination {
      display: flex;
      justify-content: center;
      gap: 12px;
      margin: 24px 0;
      padding: 20px 0;
      border-top: 1px solid #e1e4e8;
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
    footer {
      text-align: center;
      padding: 20px;
      background: #f8f9fa;
      color: #666;
      font-size: 13px;
      border-top: 1px solid #dee2e6;
    }
		input:checked + span {
			background-color: #2196F3;
		}
		input:checked + span + span {
			transform: translateX(20px);
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
      <a href="/html/timeline?kinds=1&limit=20">← Timeline</a>
    </nav>

    <main>
      <div class="profile-header">
        {{if and .Profile .Profile.Picture}}
        <img class="profile-avatar" src="{{.Profile.Picture}}" alt="avatar" onerror="this.style.display='none'">
        {{else}}
        <div class="profile-avatar" style="background:#667eea;display:flex;align-items:center;justify-content:center;color:white;font-size:32px;">?</div>
        {{end}}
        <div class="profile-info">
          {{if .Profile}}
          {{if or .Profile.DisplayName .Profile.Name}}
          <div class="profile-name">{{if .Profile.DisplayName}}{{.Profile.DisplayName}}{{else}}{{.Profile.Name}}{{end}}</div>
          {{end}}
          {{if .Profile.Nip05}}
          <div class="profile-nip05">{{.Profile.Nip05}}</div>
          {{end}}
          {{end}}
          <div class="profile-npub" title="{{.Pubkey}}">{{.NpubShort}}</div>
          {{if and .Profile .Profile.About}}
          <div class="profile-about">{{.Profile.About}}</div>
          {{end}}
        </div>
      </div>

      <div class="notes-section">
        <h3>Notes ({{len .Items}})</h3>
        {{range .Items}}
        <article class="note">
          <div class="note-content">{{.ContentHTML}}</div>
          <div class="note-meta">
            <span>{{formatTime .CreatedAt}}</span>
            {{if .RelaysSeen}}
            <span title="{{join .RelaysSeen ", "}}">from {{len .RelaysSeen}} relay(s)</span>
            {{end}}
            <a href="/html/thread/{{.ID}}" style="color:#667eea;text-decoration:none;margin-left:auto;">View Thread →</a>
          </div>
        </article>
        {{end}}
      </div>

      {{if .Pagination}}
      <div class="pagination">
        {{if .Pagination.Next}}
        <a href="{{.Pagination.Next}}" class="link">Load More →</a>
        {{end}}
      </div>
      {{end}}
    </main>

    <footer>
      <p>Pure HTML hypermedia - no JavaScript required</p>
    </footer>
  </div>
</body>
</html>
`

type HTMLProfileData struct {
	Title      string
	Pubkey     string
	Npub       string
	NpubShort  string
	Profile    *ProfileInfo
	Items      []HTMLEventItem
	Pagination *HTMLPagination
	Meta       *MetaInfo
}

func renderProfileHTML(resp ProfileResponse, relays []string, limit int) (string, error) {
	// Generate npub from hex pubkey
	npub, _ := encodeBech32Pubkey(resp.Pubkey)

	// Convert notes to HTML items
	items := make([]HTMLEventItem, len(resp.Notes.Items))
	for i, item := range resp.Notes.Items {
		items[i] = HTMLEventItem{
			ID:            item.ID,
			Kind:          item.Kind,
			Pubkey:        item.Pubkey,
			CreatedAt:     item.CreatedAt,
			Content:       item.Content,
			ContentHTML:   processContentToHTML(item.Content),
			RelaysSeen:    item.RelaysSeen,
			AuthorProfile: item.AuthorProfile,
		}
	}

	// Build pagination
	var pagination *HTMLPagination
	if resp.Notes.Page.Next != nil {
		pagination = &HTMLPagination{
			Next: *resp.Notes.Page.Next,
		}
	}

	// Get display name for title
	title := "Profile"
	if resp.Profile != nil {
		if resp.Profile.DisplayName != "" {
			title = resp.Profile.DisplayName
		} else if resp.Profile.Name != "" {
			title = resp.Profile.Name
		}
	}

	data := HTMLProfileData{
		Title:      title,
		Pubkey:     resp.Pubkey,
		Npub:       npub,
		NpubShort:  formatNpubShort(npub),
		Profile:    resp.Profile,
		Items:      items,
		Pagination: pagination,
		Meta:       &resp.Notes.Meta,
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
	}

	tmpl, err := template.New("profile").Funcs(funcMap).Parse(htmlProfileTemplate)
	if err != nil {
		return "", err
	}

	var buf strings.Builder
	if err := tmpl.Execute(&buf, data); err != nil {
		return "", err
	}

	return buf.String(), nil
}
