package main

import (
	"encoding/hex"
	"fmt"
	"html"
	"html/template"
	"log"
	"regexp"
	"strings"
	"sync"
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
    .note-video {
      max-width: 100%;
      border-radius: 8px;
      margin: 8px 0;
      display: block;
    }
    .note-audio {
      width: 100%;
      margin: 8px 0;
      display: block;
    }
    .youtube-embed {
      width: 100%;
      aspect-ratio: 16/9;
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
    .link-preview {
      display: flex;
      border: 1px solid #e1e4e8;
      border-radius: 8px;
      margin: 12px 0;
      overflow: hidden;
      text-decoration: none;
      color: inherit;
      background: #fafbfc;
      transition: border-color 0.2s, box-shadow 0.2s;
    }
    .link-preview:hover {
      border-color: #667eea;
      box-shadow: 0 2px 8px rgba(102, 126, 234, 0.15);
      text-decoration: none;
    }
    .link-preview-image {
      width: 120px;
      min-width: 120px;
      height: 90px;
      object-fit: cover;
      margin: 0;
      border-radius: 0;
    }
    .link-preview-content {
      padding: 10px 14px;
      overflow: hidden;
      display: flex;
      flex-direction: column;
      justify-content: center;
    }
    .link-preview-site {
      font-size: 11px;
      color: #6a737d;
      text-transform: uppercase;
      letter-spacing: 0.5px;
      margin-bottom: 2px;
    }
    .link-preview-title {
      font-size: 14px;
      font-weight: 600;
      color: #24292e;
      line-height: 1.3;
      margin-bottom: 4px;
      display: -webkit-box;
      -webkit-line-clamp: 2;
      -webkit-box-orient: vertical;
      overflow: hidden;
    }
    .link-preview-desc {
      font-size: 12px;
      color: #586069;
      line-height: 1.4;
      display: -webkit-box;
      -webkit-line-clamp: 2;
      -webkit-box-orient: vertical;
      overflow: hidden;
    }
    .quoted-note {
      background: #f8f9fa;
      border: 1px solid #e1e4e8;
      border-left: 3px solid #667eea;
      border-radius: 4px;
      padding: 12px;
      margin: 12px 0;
      font-size: 14px;
    }
    .quoted-note .quoted-author {
      display: flex;
      align-items: center;
      gap: 8px;
      margin-bottom: 8px;
      font-size: 13px;
    }
    .quoted-note .quoted-author img {
      width: 24px;
      height: 24px;
      border-radius: 50%;
      margin: 0;
    }
    .quoted-note .quoted-author-name {
      font-weight: 600;
      color: #24292e;
    }
    .quoted-note .quoted-author-npub {
      font-family: monospace;
      font-size: 11px;
      color: #667eea;
    }
    .quoted-note .quoted-content {
      color: #24292e;
      white-space: pre-wrap;
      word-wrap: break-word;
    }
    .quoted-note .quoted-meta {
      font-size: 11px;
      color: #666;
      margin-top: 8px;
    }
    .quoted-note .quoted-meta a {
      color: #667eea;
    }
    .quoted-note-error {
      background: #fff5f5;
      border: 1px solid #fecaca;
      border-left: 3px solid #dc2626;
      border-radius: 4px;
      padding: 8px 12px;
      margin: 8px 0;
      font-size: 13px;
      color: #666;
    }
    .nostr-ref {
      display: inline-block;
      padding: 4px 10px;
      margin: 4px 0;
      border-radius: 4px;
      font-size: 13px;
      text-decoration: none;
      transition: background 0.2s;
    }
    .nostr-ref-event {
      background: #e8f4fd;
      border: 1px solid #b8daff;
      color: #0056b3;
    }
    .nostr-ref-event:hover {
      background: #d1e9fc;
    }
    .nostr-ref-profile {
      display: inline;
      padding: 0;
      margin: 0;
      border: none;
      border-radius: 0;
      background: none;
      color: #2e7d32;
      font-weight: 500;
    }
    .nostr-ref-profile:hover {
      background: none;
      text-decoration: underline;
    }
    .nostr-ref-addr {
      background: #fff3e0;
      border: 1px solid #ffcc80;
      color: #e65100;
    }
    .nostr-ref-addr:hover {
      background: #ffe0b2;
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
    .reply-count-badge {
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
    button[type="submit"].reaction-badge {
      display: inline-flex;
      align-items: center;
      gap: 4px;
      padding: 4px 10px;
      background: #f0f0f0;
      color: #555;
      border: none;
      border-radius: 16px;
      font-size: 13px;
      line-height: 1.4;
      cursor: pointer;
      font-family: inherit;
      margin-top: 0;
    }
    button[type="submit"].reaction-badge:hover {
      background: #e0e0e0;
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
          Posting as: <span style="color:#2e7d32;font-weight:500;">{{.UserDisplayName}}</span>
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
      {{$item := .}}
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
          <a href="/html/thread/{{.ID}}" class="reaction-badge reply-count-badge" style="text-decoration:none;">replies {{.ReplyCount}}</a>
          {{end}}
          {{if and .Reactions (index .Reactions.ByType "❤️")}}
          {{if $.LoggedIn}}
          <form method="POST" action="/html/react" style="display:inline;margin:0;">
            <input type="hidden" name="event_id" value="{{$item.ID}}">
            <input type="hidden" name="event_pubkey" value="{{$item.Pubkey}}">
            <input type="hidden" name="return_url" value="{{$.CurrentURL}}">
            <input type="hidden" name="reaction" value="❤️">
            <button type="submit" class="reaction-badge">❤️ {{index .Reactions.ByType "❤️"}} +</button>
          </form>
          {{else}}
          <span class="reaction-badge">❤️ {{index .Reactions.ByType "❤️"}}</span>
          {{end}}
          {{else if $.LoggedIn}}
          <form method="POST" action="/html/react" style="display:inline;margin:0;">
            <input type="hidden" name="event_id" value="{{$item.ID}}">
            <input type="hidden" name="event_pubkey" value="{{$item.Pubkey}}">
            <input type="hidden" name="return_url" value="{{$.CurrentURL}}">
            <input type="hidden" name="reaction" value="❤️">
            <button type="submit" class="reaction-badge">❤️ +</button>
          </form>
          {{end}}
          {{if and .Reactions (gt .Reactions.Total 0)}}
          {{range $type, $count := .Reactions.ByType}}
          {{if ne $type "❤️"}}
          <span class="reaction-badge">{{$type}} {{$count}}</span>
          {{end}}
          {{end}}
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
	Title           string
	Meta            *MetaInfo
	Items           []HTMLEventItem
	Pagination      *HTMLPagination
	Actions         []HTMLAction
	Links           []string
	LoggedIn        bool
	UserPubKey      string
	UserDisplayName string // Display name from profile (falls back to @npubShort)
	Error           string
	Success         string
	ShowReactions   bool     // Whether reactions are being fetched (slow mode)
	FeedMode        string   // "follows" or "global"
	ActiveRelays    []string // Relays being used for this request
	CurrentURL      string   // Current page URL for reaction redirects
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
// Video extension regex
var videoExtRegex = regexp.MustCompile(`(?i)\.(mp4|webm|mov|m4v)(\?.*)?$`)
// Audio extension regex
var audioExtRegex = regexp.MustCompile(`(?i)\.(mp3|wav|ogg|flac|m4a|aac)(\?.*)?$`)
// YouTube URL regex - matches youtube.com/watch?v=ID, youtu.be/ID, youtube.com/shorts/ID
var youtubeRegex = regexp.MustCompile(`(?i)(?:youtube\.com/watch\?v=|youtu\.be/|youtube\.com/shorts/)([a-zA-Z0-9_-]{11})`)
var urlRegex = regexp.MustCompile(`https?://[^\s<>"]+`)

// Nostr reference regex - matches nostr:nevent1..., nostr:note1..., nostr:nprofile1..., nostr:naddr1..., nostr:npub1...
var nostrRefRegex = regexp.MustCompile(`nostr:(nevent1[a-z0-9]+|note1[a-z0-9]+|nprofile1[a-z0-9]+|naddr1[a-z0-9]+|npub1[a-z0-9]+)`)

// ResolvedRef holds a pre-resolved nostr reference
type ResolvedRef struct {
	HTML string
}

// extractNostrRefs extracts all nostr: identifiers from content strings
func extractNostrRefs(contents []string) []string {
	seen := make(map[string]bool)
	var refs []string
	for _, content := range contents {
		matches := nostrRefRegex.FindAllStringSubmatch(content, -1)
		for _, match := range matches {
			if len(match) >= 2 {
				identifier := match[1]
				if !seen[identifier] {
					seen[identifier] = true
					refs = append(refs, identifier)
				}
			}
		}
	}
	return refs
}

// batchResolveNostrRefs pre-fetches all nostr references in parallel
// Returns a map of identifier -> rendered HTML
func batchResolveNostrRefs(identifiers []string, relays []string) map[string]string {
	if len(identifiers) == 0 || len(relays) == 0 {
		return nil
	}

	resolved := make(map[string]string)
	var mu sync.Mutex
	var wg sync.WaitGroup

	log.Printf("Batch resolving %d nostr references...", len(identifiers))

	for _, id := range identifiers {
		wg.Add(1)
		go func(identifier string) {
			defer wg.Done()
			html := resolveNostrReference(identifier, relays)
			mu.Lock()
			resolved[identifier] = html
			mu.Unlock()
		}(id)
	}

	wg.Wait()
	log.Printf("Batch resolved %d nostr references", len(resolved))
	return resolved
}

// formatNpubShort creates a shortened npub display like "npub1abc...xyz"
func formatNpubShort(npub string) string {
	if len(npub) <= 16 {
		return npub
	}
	return npub[:9] + "..." + npub[len(npub)-4:]
}

// processContentToHTML converts plain text content to HTML with images and links
// This version does not resolve nostr: references (for backward compatibility)
func processContentToHTML(content string) template.HTML {
	return processContentToHTMLFull(content, nil, nil, nil)
}

// processContentToHTMLWithRelays converts plain text content to HTML with images, links,
// and resolved nostr: references (quoted notes, profiles)
// NOTE: This function resolves references synchronously - use processContentToHTMLFull
// with pre-resolved refs for better performance when processing multiple items
func processContentToHTMLWithRelays(content string, relays []string) template.HTML {
	return processContentToHTMLFull(content, relays, nil, nil)
}

// processContentToHTMLWithResolved converts plain text content to HTML with images, links,
// and pre-resolved nostr: references. If resolvedRefs is provided, it uses those instead
// of fetching from relays (much faster for batch processing).
func processContentToHTMLWithResolved(content string, relays []string, resolvedRefs map[string]string) template.HTML {
	return processContentToHTMLFull(content, relays, resolvedRefs, nil)
}

// processContentToHTMLFull converts plain text content to HTML with images, links,
// pre-resolved nostr: references, and link previews.
func processContentToHTMLFull(content string, relays []string, resolvedRefs map[string]string, linkPreviews map[string]*LinkPreview) template.HTML {
	// Use placeholders for nostr: references to avoid URL regex matching their HTML
	type placeholder struct {
		key   string
		value string
	}
	var placeholders []placeholder
	placeholderIndex := 0

	// First, extract nostr: references and replace with placeholders (before escaping)
	processedContent := nostrRefRegex.ReplaceAllStringFunc(content, func(match string) string {
		identifier := strings.TrimPrefix(match, "nostr:")

		var resolved string
		if resolvedRefs != nil {
			// Use pre-resolved HTML if available
			if html, ok := resolvedRefs[identifier]; ok {
				resolved = html
			} else {
				// Fallback to simple link if not pre-resolved
				resolved = nostrRefToLink(identifier)
			}
		} else if relays != nil && len(relays) > 0 {
			// Fetch synchronously (slow path - avoid in loops)
			resolved = resolveNostrReference(identifier, relays)
		} else {
			// No relays, just render as link
			resolved = nostrRefToLink(identifier)
		}

		key := fmt.Sprintf("\x00NOSTR_%d\x00", placeholderIndex)
		placeholderIndex++
		placeholders = append(placeholders, placeholder{key: key, value: resolved})
		return key
	})

	// Now escape the content (placeholders will be escaped but that's fine - they're unique)
	escaped := html.EscapeString(processedContent)

	// Find all URLs and replace them
	result := urlRegex.ReplaceAllStringFunc(escaped, func(url string) string {
		// Unescape the URL (it was escaped above)
		url = html.UnescapeString(url)
		if imageExtRegex.MatchString(url) {
			return fmt.Sprintf(`<img src="%s" alt="image" loading="lazy">`, html.EscapeString(url))
		}
		if videoExtRegex.MatchString(url) {
			return fmt.Sprintf(`<video src="%s" controls preload="metadata" class="note-video"></video>`, html.EscapeString(url))
		}
		if audioExtRegex.MatchString(url) {
			return fmt.Sprintf(`<audio src="%s" controls preload="metadata" class="note-audio"></audio>`, html.EscapeString(url))
		}
		if match := youtubeRegex.FindStringSubmatch(url); len(match) > 1 {
			videoID := match[1]
			return fmt.Sprintf(`<iframe class="youtube-embed" src="https://www.youtube-nocookie.com/embed/%s" frameborder="0" allow="accelerometer; clipboard-write; encrypted-media; gyroscope; picture-in-picture" allowfullscreen></iframe>`, html.EscapeString(videoID))
		}
		// Check for link preview
		if linkPreviews != nil {
			if preview, ok := linkPreviews[url]; ok && !preview.Failed && preview.Title != "" {
				return renderLinkPreview(url, preview)
			}
		}
		return fmt.Sprintf(`<a href="%s" target="_blank" rel="noopener">%s</a>`, html.EscapeString(url), html.EscapeString(url))
	})

	// Now replace placeholders with actual HTML (placeholders got escaped, so unescape them first)
	for _, p := range placeholders {
		escapedKey := html.EscapeString(p.key)
		result = strings.Replace(result, escapedKey, p.value, 1)
	}

	return template.HTML(result)
}

// renderLinkPreview creates an HTML preview card for a URL
func renderLinkPreview(url string, preview *LinkPreview) string {
	var sb strings.Builder

	sb.WriteString(`<a href="`)
	sb.WriteString(html.EscapeString(url))
	sb.WriteString(`" target="_blank" rel="noopener" class="link-preview">`)

	// Image on the left (if available)
	if preview.Image != "" {
		sb.WriteString(`<img src="`)
		sb.WriteString(html.EscapeString(preview.Image))
		sb.WriteString(`" alt="" class="link-preview-image" loading="lazy">`)
	}

	sb.WriteString(`<div class="link-preview-content">`)

	// Site name
	if preview.SiteName != "" {
		sb.WriteString(`<div class="link-preview-site">`)
		sb.WriteString(html.EscapeString(preview.SiteName))
		sb.WriteString(`</div>`)
	}

	// Title
	sb.WriteString(`<div class="link-preview-title">`)
	sb.WriteString(html.EscapeString(preview.Title))
	sb.WriteString(`</div>`)

	// Description (truncate if too long)
	if preview.Description != "" {
		desc := preview.Description
		if len(desc) > 150 {
			desc = desc[:147] + "..."
		}
		sb.WriteString(`<div class="link-preview-desc">`)
		sb.WriteString(html.EscapeString(desc))
		sb.WriteString(`</div>`)
	}

	sb.WriteString(`</div></a>`)

	return sb.String()
}

// ExtractMentionedPubkeys extracts all pubkeys from npub/nprofile references in content
func ExtractMentionedPubkeys(contents []string) []string {
	seen := make(map[string]bool)
	var pubkeys []string

	for _, content := range contents {
		matches := nostrRefRegex.FindAllStringSubmatch(content, -1)
		for _, match := range matches {
			if len(match) < 2 {
				continue
			}
			identifier := match[1]

			var pubkey string
			if strings.HasPrefix(identifier, "npub1") {
				pk, err := decodeBech32Pubkey(identifier)
				if err == nil {
					pubkey = pk
				}
			} else if strings.HasPrefix(identifier, "nprofile1") {
				np, err := DecodeNProfile(identifier)
				if err == nil {
					pubkey = np.Pubkey
				}
			}

			if pubkey != "" && !seen[pubkey] {
				seen[pubkey] = true
				pubkeys = append(pubkeys, pubkey)
			}
		}
	}
	return pubkeys
}

// getCachedUsername returns @username if profile is cached, otherwise @npubShort
func getCachedUsername(pubkey string) string {
	// Check profile cache first (no network fetch)
	if profile, ok := profileCache.Get(pubkey); ok && profile != nil {
		// Prefer display_name, then name
		if profile.DisplayName != "" {
			return "@" + profile.DisplayName
		}
		if profile.Name != "" {
			return "@" + profile.Name
		}
	}
	// Fall back to short npub
	if npub, err := encodeBech32Pubkey(pubkey); err == nil {
		return "@" + formatNpubShort(npub)
	}
	return "@" + pubkey[:12] + "..."
}

// nostrRefToLink converts a nostr identifier to a descriptive link
func nostrRefToLink(identifier string) string {
	switch {
	case strings.HasPrefix(identifier, "nevent1"):
		if ne, err := DecodeNEvent(identifier); err == nil {
			return fmt.Sprintf(`<a href="/html/thread/%s" class="nostr-ref nostr-ref-event">View quoted note →</a>`,
				html.EscapeString(ne.EventID))
		}
	case strings.HasPrefix(identifier, "note1"):
		if eventID, err := DecodeNote(identifier); err == nil {
			return fmt.Sprintf(`<a href="/html/thread/%s" class="nostr-ref nostr-ref-event">View quoted note →</a>`,
				html.EscapeString(eventID))
		}
	case strings.HasPrefix(identifier, "nprofile1"):
		if np, err := DecodeNProfile(identifier); err == nil {
			username := getCachedUsername(np.Pubkey)
			return fmt.Sprintf(`<a href="/html/profile/%s" class="nostr-ref nostr-ref-profile">%s</a>`,
				html.EscapeString(np.Pubkey), html.EscapeString(username))
		}
	case strings.HasPrefix(identifier, "npub1"):
		if pubkey, err := decodeBech32Pubkey(identifier); err == nil {
			username := getCachedUsername(pubkey)
			return fmt.Sprintf(`<a href="/html/profile/%s" class="nostr-ref nostr-ref-profile">%s</a>`,
				html.EscapeString(pubkey), html.EscapeString(username))
		}
	case strings.HasPrefix(identifier, "naddr1"):
		// naddr references replaceable events (often long-form articles)
		if na, err := DecodeNAddr(identifier); err == nil {
			// Determine content type based on kind
			label := "View article →"
			if na.Kind == 1 {
				label = "View note →"
			} else if na.Kind == 30023 {
				label = "View article →"
			} else if na.Kind == 30311 {
				label = "View live event →"
			}
			// TODO: naddr needs special handling to fetch by kind:pubkey:d-tag
			return fmt.Sprintf(`<a href="#" class="nostr-ref nostr-ref-addr" title="kind:%d">%s</a>`,
				na.Kind, label)
		}
	}
	// Fallback - return as-is
	return "nostr:" + html.EscapeString(identifier)
}

// resolveNostrReference renders a nostr reference as a styled link
// NOTE: Does NOT fetch events/profiles to keep rendering fast - just creates navigable links
func resolveNostrReference(identifier string, relays []string) string {
	// Use the fast link-only approach for all reference types
	return nostrRefToLink(identifier)
}

// resolveNevent fetches and renders a nevent as a quoted note
func resolveNevent(identifier string, relays []string) string {
	ne, err := DecodeNEvent(identifier)
	if err != nil {
		return renderQuotedError(identifier, "Invalid nevent")
	}

	// Use relay hints if provided, otherwise use passed relays
	fetchRelays := relays
	if len(ne.RelayHints) > 0 {
		fetchRelays = append(ne.RelayHints, relays...)
	}

	events := fetchEventByID(fetchRelays, ne.EventID)
	if len(events) == 0 {
		return renderQuotedError(identifier, "Event not found")
	}

	return renderQuotedNote(&events[0], relays)
}

// resolveNote fetches and renders a note1 as a quoted note
func resolveNote(identifier string, relays []string) string {
	eventID, err := DecodeNote(identifier)
	if err != nil {
		return renderQuotedError(identifier, "Invalid note")
	}

	events := fetchEventByID(relays, eventID)
	if len(events) == 0 {
		return renderQuotedError(identifier, "Event not found")
	}

	return renderQuotedNote(&events[0], relays)
}

// resolveNProfile renders an nprofile as a profile link
func resolveNProfile(identifier string, relays []string) string {
	np, err := DecodeNProfile(identifier)
	if err != nil {
		return nostrRefToLink(identifier)
	}

	// Fetch profile info
	profiles := fetchProfiles(relays, []string{np.Pubkey})
	profile := profiles[np.Pubkey]

	npub, _ := encodeBech32Pubkey(np.Pubkey)
	displayName := formatNpubShort(npub)
	if profile != nil {
		if profile.DisplayName != "" {
			displayName = profile.DisplayName
		} else if profile.Name != "" {
			displayName = profile.Name
		}
	}

	return fmt.Sprintf(`<a href="/html/profile/%s" class="nostr-ref">@%s</a>`,
		html.EscapeString(np.Pubkey), html.EscapeString(displayName))
}

// resolveNpub renders an npub as a profile link
func resolveNpub(identifier string, relays []string) string {
	pubkey, err := decodeBech32Pubkey(identifier)
	if err != nil {
		return nostrRefToLink(identifier)
	}

	// Fetch profile info
	profiles := fetchProfiles(relays, []string{pubkey})
	profile := profiles[pubkey]

	displayName := formatNpubShort(identifier)
	if profile != nil {
		if profile.DisplayName != "" {
			displayName = profile.DisplayName
		} else if profile.Name != "" {
			displayName = profile.Name
		}
	}

	return fmt.Sprintf(`<a href="/html/profile/%s" class="nostr-ref">@%s</a>`,
		html.EscapeString(pubkey), html.EscapeString(displayName))
}

// resolveNAddr fetches and renders an naddr (replaceable event) as a quoted note
func resolveNAddr(identifier string, relays []string) string {
	na, err := DecodeNAddr(identifier)
	if err != nil {
		return renderQuotedError(identifier, "Invalid naddr")
	}

	// Use relay hints if provided
	fetchRelays := relays
	if len(na.RelayHints) > 0 {
		fetchRelays = append(na.RelayHints, relays...)
	}

	// Fetch the replaceable event by kind:pubkey:d-tag
	event := fetchReplaceableEvent(fetchRelays, int(na.Kind), na.Author, na.DTag)
	if event == nil {
		return renderQuotedError(identifier, "Event not found")
	}

	return renderQuotedNote(event, relays)
}

// fetchReplaceableEvent fetches a replaceable event by kind, author, and d-tag
func fetchReplaceableEvent(relays []string, kind int, author string, dTag string) *Event {
	filter := Filter{
		Authors: []string{author},
		Kinds:   []int{kind},
		Limit:   1,
	}

	events, _ := fetchEventsFromRelays(relays, filter)

	// Find the event with matching d-tag
	for _, evt := range events {
		for _, tag := range evt.Tags {
			if len(tag) >= 2 && tag[0] == "d" && tag[1] == dTag {
				return &evt
			}
		}
		// For kind 0/3 (non-parameterized), d-tag may be empty
		if dTag == "" {
			return &evt
		}
	}

	return nil
}

// renderQuotedNote renders an event as an embedded quoted note
// NOTE: Does NOT fetch author profile to keep rendering fast - just shows npub
func renderQuotedNote(event *Event, relays []string) string {
	npub, _ := encodeBech32Pubkey(event.PubKey)
	npubShort := formatNpubShort(npub)

	// Build author section (no profile fetch - too slow for embedded quotes)
	authorHTML := fmt.Sprintf(`<div class="quoted-author"><a href="/html/profile/%s"><span class="quoted-author-npub">%s</span></a></div>`,
		html.EscapeString(event.PubKey),
		html.EscapeString(npubShort))

	// Truncate content if too long
	content := event.Content
	if len(content) > 500 {
		content = content[:500] + "..."
	}

	// Process content but don't recurse into nested nostr: refs (pass nil relays)
	contentHTML := processContentToHTMLWithRelays(content, nil)

	// Format timestamp
	timestamp := time.Unix(event.CreatedAt, 0).Format("2006-01-02 15:04")

	return fmt.Sprintf(`<div class="quoted-note">%s<div class="quoted-content">%s</div><div class="quoted-meta"><span>%s</span> · <a href="/html/thread/%s">View thread →</a></div></div>`,
		authorHTML,
		contentHTML,
		html.EscapeString(timestamp),
		html.EscapeString(event.ID))
}

// renderQuotedError renders an error state for a failed nostr reference
func renderQuotedError(identifier string, message string) string {
	short := identifier
	if len(short) > 20 {
		short = short[:20] + "..."
	}
	return fmt.Sprintf(`<div class="quoted-note-error">%s: <code>%s</code></div>`,
		html.EscapeString(message),
		html.EscapeString(short))
}

func renderHTML(resp TimelineResponse, relays []string, authors []string, kinds []int, limit int, session *BunkerSession, errorMsg, successMsg string, showReactions bool, feedMode string, currentURL string) (string, error) {
	// Pre-fetch all nostr: references in parallel for much faster rendering
	contents := make([]string, len(resp.Items))
	for i, item := range resp.Items {
		contents[i] = item.Content
	}
	nostrRefs := extractNostrRefs(contents)
	resolvedRefs := batchResolveNostrRefs(nostrRefs, relays)

	// Pre-fetch link previews for all URLs
	var allURLs []string
	for _, content := range contents {
		allURLs = append(allURLs, ExtractPreviewableURLs(content)...)
	}
	linkPreviews := FetchLinkPreviews(allURLs)

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
			ContentHTML:   processContentToHTMLFull(item.Content, relays, resolvedRefs, linkPreviews),
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
		pubkeyHex := hex.EncodeToString(session.UserPubKey)
		data.UserPubKey = pubkeyHex
		data.UserDisplayName = getUserDisplayName(pubkeyHex)
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
    .note-video {
      max-width: 100%;
      border-radius: 8px;
      margin: 8px 0;
      display: block;
    }
    .note-audio {
      width: 100%;
      margin: 8px 0;
      display: block;
    }
    .youtube-embed {
      width: 100%;
      aspect-ratio: 16/9;
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
    .link-preview {
      display: flex;
      border: 1px solid #e1e4e8;
      border-radius: 8px;
      margin: 12px 0;
      overflow: hidden;
      text-decoration: none;
      color: inherit;
      background: #fafbfc;
      transition: border-color 0.2s, box-shadow 0.2s;
    }
    .link-preview:hover {
      border-color: #667eea;
      box-shadow: 0 2px 8px rgba(102, 126, 234, 0.15);
      text-decoration: none;
    }
    .link-preview-image {
      width: 120px;
      min-width: 120px;
      height: 90px;
      object-fit: cover;
      margin: 0;
      border-radius: 0;
    }
    .link-preview-content {
      padding: 10px 14px;
      overflow: hidden;
      display: flex;
      flex-direction: column;
      justify-content: center;
    }
    .link-preview-site {
      font-size: 11px;
      color: #6a737d;
      text-transform: uppercase;
      letter-spacing: 0.5px;
      margin-bottom: 2px;
    }
    .link-preview-title {
      font-size: 14px;
      font-weight: 600;
      color: #24292e;
      line-height: 1.3;
      margin-bottom: 4px;
      display: -webkit-box;
      -webkit-line-clamp: 2;
      -webkit-box-orient: vertical;
      overflow: hidden;
    }
    .link-preview-desc {
      font-size: 12px;
      color: #586069;
      line-height: 1.4;
      display: -webkit-box;
      -webkit-line-clamp: 2;
      -webkit-box-orient: vertical;
      overflow: hidden;
    }
    .quoted-note {
      background: #f8f9fa;
      border: 1px solid #e1e4e8;
      border-left: 3px solid #667eea;
      border-radius: 4px;
      padding: 12px;
      margin: 12px 0;
      font-size: 14px;
    }
    .quoted-note .quoted-author {
      display: flex;
      align-items: center;
      gap: 8px;
      margin-bottom: 8px;
      font-size: 13px;
    }
    .quoted-note .quoted-author img {
      width: 24px;
      height: 24px;
      border-radius: 50%;
      margin: 0;
    }
    .quoted-note .quoted-author-name {
      font-weight: 600;
      color: #24292e;
    }
    .quoted-note .quoted-author-npub {
      font-family: monospace;
      font-size: 11px;
      color: #667eea;
    }
    .quoted-note .quoted-content {
      color: #24292e;
      white-space: pre-wrap;
      word-wrap: break-word;
    }
    .quoted-note .quoted-meta {
      font-size: 11px;
      color: #666;
      margin-top: 8px;
    }
    .quoted-note .quoted-meta a {
      color: #667eea;
    }
    .quoted-note-error {
      background: #fff5f5;
      border: 1px solid #fecaca;
      border-left: 3px solid #dc2626;
      border-radius: 4px;
      padding: 8px 12px;
      margin: 8px 0;
      font-size: 13px;
      color: #666;
    }
    .nostr-ref {
      display: inline-block;
      padding: 4px 10px;
      margin: 4px 0;
      border-radius: 4px;
      font-size: 13px;
      text-decoration: none;
      transition: background 0.2s;
    }
    .nostr-ref-event {
      background: #e8f4fd;
      border: 1px solid #b8daff;
      color: #0056b3;
    }
    .nostr-ref-event:hover {
      background: #d1e9fc;
    }
    .nostr-ref-profile {
      display: inline;
      padding: 0;
      margin: 0;
      border: none;
      border-radius: 0;
      background: none;
      color: #2e7d32;
      font-weight: 500;
    }
    .nostr-ref-profile:hover {
      background: none;
      text-decoration: underline;
    }
    .nostr-ref-addr {
      background: #fff3e0;
      border: 1px solid #ffcc80;
      color: #e65100;
    }
    .nostr-ref-addr:hover {
      background: #ffe0b2;
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
        <div class="note-reactions" style="margin:12px 0;padding:8px 0;border-top:1px solid #e1e4e8;">
          {{if and .Root.Reactions (index .Root.Reactions.ByType "❤️")}}
          {{if $.LoggedIn}}
          <form method="POST" action="/html/react" style="display:inline;margin:0;">
            <input type="hidden" name="event_id" value="{{.Root.ID}}">
            <input type="hidden" name="event_pubkey" value="{{.Root.Pubkey}}">
            <input type="hidden" name="return_url" value="{{$.CurrentURL}}">
            <input type="hidden" name="reaction" value="❤️">
            <button type="submit" class="reaction-badge">❤️ {{index .Root.Reactions.ByType "❤️"}} +</button>
          </form>
          {{else}}
          <span class="reaction-badge">❤️ {{index .Root.Reactions.ByType "❤️"}}</span>
          {{end}}
          {{else if $.LoggedIn}}
          <form method="POST" action="/html/react" style="display:inline;margin:0;">
            <input type="hidden" name="event_id" value="{{.Root.ID}}">
            <input type="hidden" name="event_pubkey" value="{{.Root.Pubkey}}">
            <input type="hidden" name="return_url" value="{{$.CurrentURL}}">
            <input type="hidden" name="reaction" value="❤️">
            <button type="submit" class="reaction-badge">❤️ +</button>
          </form>
          {{end}}
          {{if and .Root.Reactions (gt .Root.Reactions.Total 0)}}
          {{range $type, $count := .Root.Reactions.ByType}}
          {{if ne $type "❤️"}}
          <span class="reaction-badge">{{$type}} {{$count}}</span>
          {{end}}
          {{end}}
          {{end}}
        </div>
        {{if false}}{{end}}
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
          Replying as: <span style="color:#2e7d32;font-weight:500;">{{.UserDisplayName}}</span>
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
        {{$reply := .}}
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
          <div class="note-reactions" style="margin:8px 0;">
            {{if and .Reactions (index .Reactions.ByType "❤️")}}
            {{if $.LoggedIn}}
            <form method="POST" action="/html/react" style="display:inline;margin:0;">
              <input type="hidden" name="event_id" value="{{$reply.ID}}">
              <input type="hidden" name="event_pubkey" value="{{$reply.Pubkey}}">
              <input type="hidden" name="return_url" value="{{$.CurrentURL}}">
              <input type="hidden" name="reaction" value="❤️">
              <button type="submit" class="reaction-badge">❤️ {{index .Reactions.ByType "❤️"}} +</button>
            </form>
            {{else}}
            <span class="reaction-badge">❤️ {{index .Reactions.ByType "❤️"}}</span>
            {{end}}
            {{else if $.LoggedIn}}
            <form method="POST" action="/html/react" style="display:inline;margin:0;">
              <input type="hidden" name="event_id" value="{{$reply.ID}}">
              <input type="hidden" name="event_pubkey" value="{{$reply.Pubkey}}">
              <input type="hidden" name="return_url" value="{{$.CurrentURL}}">
              <input type="hidden" name="reaction" value="❤️">
              <button type="submit" class="reaction-badge">❤️ +</button>
            </form>
            {{end}}
            {{if and .Reactions (gt .Reactions.Total 0)}}
            {{range $type, $count := .Reactions.ByType}}
            {{if ne $type "❤️"}}
            <span class="reaction-badge">{{$type}} {{$count}}</span>
            {{end}}
            {{end}}
            {{end}}
          </div>
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
	Root            *HTMLEventItem
	Replies         []HTMLEventItem
	LoggedIn        bool
	UserPubKey      string
	UserDisplayName string
	CurrentURL      string
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

func renderThreadHTML(resp ThreadResponse, relays []string, session *BunkerSession, currentURL string) (string, error) {
	// Pre-fetch all nostr: references in parallel for much faster rendering
	contents := make([]string, 1+len(resp.Replies))
	contents[0] = resp.Root.Content
	for i, item := range resp.Replies {
		contents[i+1] = item.Content
	}
	nostrRefs := extractNostrRefs(contents)
	resolvedRefs := batchResolveNostrRefs(nostrRefs, relays)

	// Pre-fetch link previews for all URLs
	var allURLs []string
	for _, content := range contents {
		allURLs = append(allURLs, ExtractPreviewableURLs(content)...)
	}
	linkPreviews := FetchLinkPreviews(allURLs)

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
		ContentHTML:   processContentToHTMLFull(resp.Root.Content, relays, resolvedRefs, linkPreviews),
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
			ContentHTML:   processContentToHTMLFull(item.Content, relays, resolvedRefs, linkPreviews),
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
		pubkeyHex := hex.EncodeToString(session.UserPubKey)
		data.UserPubKey = pubkeyHex
		data.UserDisplayName = getUserDisplayName(pubkeyHex)
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
    .note-video {
      max-width: 100%;
      border-radius: 8px;
      margin: 8px 0;
      display: block;
    }
    .note-audio {
      width: 100%;
      margin: 8px 0;
      display: block;
    }
    .youtube-embed {
      width: 100%;
      aspect-ratio: 16/9;
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
    .link-preview {
      display: flex;
      border: 1px solid #e1e4e8;
      border-radius: 8px;
      margin: 12px 0;
      overflow: hidden;
      text-decoration: none;
      color: inherit;
      background: #fafbfc;
      transition: border-color 0.2s, box-shadow 0.2s;
    }
    .link-preview:hover {
      border-color: #667eea;
      box-shadow: 0 2px 8px rgba(102, 126, 234, 0.15);
      text-decoration: none;
    }
    .link-preview-image {
      width: 120px;
      min-width: 120px;
      height: 90px;
      object-fit: cover;
      margin: 0;
      border-radius: 0;
    }
    .link-preview-content {
      padding: 10px 14px;
      overflow: hidden;
      display: flex;
      flex-direction: column;
      justify-content: center;
    }
    .link-preview-site {
      font-size: 11px;
      color: #6a737d;
      text-transform: uppercase;
      letter-spacing: 0.5px;
      margin-bottom: 2px;
    }
    .link-preview-title {
      font-size: 14px;
      font-weight: 600;
      color: #24292e;
      line-height: 1.3;
      margin-bottom: 4px;
      display: -webkit-box;
      -webkit-line-clamp: 2;
      -webkit-box-orient: vertical;
      overflow: hidden;
    }
    .link-preview-desc {
      font-size: 12px;
      color: #586069;
      line-height: 1.4;
      display: -webkit-box;
      -webkit-line-clamp: 2;
      -webkit-box-orient: vertical;
      overflow: hidden;
    }
    .quoted-note {
      background: #f8f9fa;
      border: 1px solid #e1e4e8;
      border-left: 3px solid #667eea;
      border-radius: 4px;
      padding: 12px;
      margin: 12px 0;
      font-size: 14px;
    }
    .quoted-note .quoted-author {
      display: flex;
      align-items: center;
      gap: 8px;
      margin-bottom: 8px;
      font-size: 13px;
    }
    .quoted-note .quoted-author img {
      width: 24px;
      height: 24px;
      border-radius: 50%;
      margin: 0;
    }
    .quoted-note .quoted-author-name {
      font-weight: 600;
      color: #24292e;
    }
    .quoted-note .quoted-author-npub {
      font-family: monospace;
      font-size: 11px;
      color: #667eea;
    }
    .quoted-note .quoted-content {
      color: #24292e;
      white-space: pre-wrap;
      word-wrap: break-word;
    }
    .quoted-note .quoted-meta {
      font-size: 11px;
      color: #666;
      margin-top: 8px;
    }
    .quoted-note .quoted-meta a {
      color: #667eea;
    }
    .quoted-note-error {
      background: #fff5f5;
      border: 1px solid #fecaca;
      border-left: 3px solid #dc2626;
      border-radius: 4px;
      padding: 8px 12px;
      margin: 8px 0;
      font-size: 13px;
      color: #666;
    }
    .nostr-ref {
      display: inline-block;
      padding: 4px 10px;
      margin: 4px 0;
      border-radius: 4px;
      font-size: 13px;
      text-decoration: none;
      transition: background 0.2s;
    }
    .nostr-ref-event {
      background: #e8f4fd;
      border: 1px solid #b8daff;
      color: #0056b3;
    }
    .nostr-ref-event:hover {
      background: #d1e9fc;
    }
    .nostr-ref-profile {
      display: inline;
      padding: 0;
      margin: 0;
      border: none;
      border-radius: 0;
      background: none;
      color: #2e7d32;
      font-weight: 500;
    }
    .nostr-ref-profile:hover {
      background: none;
      text-decoration: underline;
    }
    .nostr-ref-addr {
      background: #fff3e0;
      border: 1px solid #ffcc80;
      color: #e65100;
    }
    .nostr-ref-addr:hover {
      background: #ffe0b2;
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
	// Pre-fetch all nostr: references in parallel for much faster rendering
	contents := make([]string, len(resp.Notes.Items))
	for i, item := range resp.Notes.Items {
		contents[i] = item.Content
	}
	nostrRefs := extractNostrRefs(contents)
	resolvedRefs := batchResolveNostrRefs(nostrRefs, relays)

	// Pre-fetch link previews for all URLs
	var allURLs []string
	for _, content := range contents {
		allURLs = append(allURLs, ExtractPreviewableURLs(content)...)
	}
	linkPreviews := FetchLinkPreviews(allURLs)

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
			ContentHTML:   processContentToHTMLFull(item.Content, relays, resolvedRefs, linkPreviews),
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
