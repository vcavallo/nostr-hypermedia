package main

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"html"
	"html/template"
	"log"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/yuin/goldmark"
)

// Cached compiled templates - initialized at startup via init()
var (
	cachedHTMLTemplate    *template.Template
	cachedThreadTemplate  *template.Template
	cachedProfileTemplate *template.Template
	templateFuncMap       template.FuncMap
)

// formatRelativeTime returns a human-readable relative time string
func formatRelativeTime(ts int64) string {
	t := time.Unix(ts, 0)
	now := time.Now()
	diff := now.Sub(t)

	// Handle future timestamps (shouldn't happen but just in case)
	if diff < 0 {
		return "just now"
	}

	seconds := int(diff.Seconds())
	minutes := int(diff.Minutes())
	hours := int(diff.Hours())
	days := hours / 24
	weeks := days / 7
	months := days / 30
	years := days / 365

	switch {
	case seconds < 60:
		return "just now"
	case minutes == 1:
		return "1 min ago"
	case minutes < 60:
		return fmt.Sprintf("%d mins ago", minutes)
	case hours == 1:
		return "1 hour ago"
	case hours < 24:
		return fmt.Sprintf("%d hours ago", hours)
	case days == 1:
		return "yesterday"
	case days < 7:
		return fmt.Sprintf("%d days ago", days)
	case weeks == 1:
		return "1 week ago"
	case weeks < 4:
		return fmt.Sprintf("%d weeks ago", weeks)
	case months == 1:
		return "1 month ago"
	case months < 12:
		return fmt.Sprintf("%d months ago", months)
	case years == 1:
		return "1 year ago"
	default:
		return fmt.Sprintf("%d years ago", years)
	}
}

// initTemplates compiles all templates once at startup for performance
func initTemplates() {
	templateFuncMap = template.FuncMap{
		"formatTime": func(ts int64) string {
			return formatRelativeTime(ts)
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

	var err error

	// Compile main HTML template
	cachedHTMLTemplate, err = template.New("html").Funcs(templateFuncMap).Parse(htmlTemplate)
	if err != nil {
		log.Fatalf("Failed to compile HTML template: %v", err)
	}

	// Compile thread template
	cachedThreadTemplate, err = template.New("thread").Funcs(templateFuncMap).Parse(htmlThreadTemplate)
	if err != nil {
		log.Fatalf("Failed to compile thread template: %v", err)
	}

	// Compile profile template
	cachedProfileTemplate, err = template.New("profile").Funcs(templateFuncMap).Parse(htmlProfileTemplate)
	if err != nil {
		log.Fatalf("Failed to compile profile template: %v", err)
	}

	log.Printf("All HTML templates compiled successfully")
}

// getThemeFromRequest reads the theme cookie and returns (themeClass, themeLabel)
// themeClass is used on <html> element, themeLabel shows the current theme
func getThemeFromRequest(r *http.Request) (string, string) {
	theme := ""
	if cookie, err := r.Cookie("theme"); err == nil {
		theme = cookie.Value
	}

	switch theme {
	case "dark":
		return "dark", "Dark"
	case "light":
		return "light", "Light"
	default:
		return "", "Auto"
	}
}

var htmlTemplate = `<!DOCTYPE html>
<html lang="en"{{if .ThemeClass}} class="{{.ThemeClass}}"{{end}}>
<head>
  <meta charset="UTF-8">
  <meta name="viewport" content="width=device-width, initial-scale=1.0">
  <title>{{.Title}} - Nostr Hypermedia</title>
  <link rel="icon" href="/static/favicon.ico" />
  <style>
    :root {
      --bg-page: #f5f5f5;
      --bg-container: #ffffff;
      --bg-card: #ffffff;
      --bg-secondary: #f8f9fa;
      --bg-input: #ffffff;
      --bg-badge: #f0f0f0;
      --bg-badge-hover: #e0e0e0;
      --bg-reply-badge: #e7eaff;
      --text-primary: #333333;
      --text-secondary: #666666;
      --text-muted: #999999;
      --text-content: #24292e;
      --border-color: #e1e4e8;
      --border-light: #dee2e6;
      --accent: #667eea;
      --accent-hover: #5568d3;
      --accent-secondary: #764ba2;
      --success: #2e7d32;
      --success-bg: #28a745;
      --success-hover: #218838;
      --link-preview-bg: #fafbfc;
      --quoted-bg: #f8f9fa;
      --error-bg: #fff5f5;
      --error-border: #fecaca;
      --error-accent: #dc2626;
      --ref-event-bg: #e8f4fd;
      --ref-event-border: #b8daff;
      --ref-event-color: #0056b3;
      --ref-event-hover: #d1e9fc;
      --ref-addr-bg: #fff3e0;
      --ref-addr-border: #ffcc80;
      --ref-addr-color: #e65100;
      --ref-addr-hover: #ffe0b2;
      --shadow: rgba(0,0,0,0.1);
      --shadow-accent: rgba(102, 126, 234, 0.15);
    }
    @media (prefers-color-scheme: dark) {
      :root:not(.light) {
        --bg-page: #121212;
        --bg-container: #1e1e1e;
        --bg-card: #1e1e1e;
        --bg-secondary: #252525;
        --bg-input: #2a2a2a;
        --bg-badge: #2a2a2a;
        --bg-badge-hover: #3a3a3a;
        --bg-reply-badge: #2d2d4a;
        --text-primary: #e4e4e7;
        --text-secondary: #a1a1aa;
        --text-muted: #71717a;
        --text-content: #e4e4e7;
        --border-color: #333333;
        --border-light: #333333;
        --accent: #818cf8;
        --accent-hover: #6366f1;
        --accent-secondary: #a78bfa;
        --success: #4ade80;
        --success-bg: #22c55e;
        --success-hover: #16a34a;
        --link-preview-bg: #252525;
        --quoted-bg: #252525;
        --error-bg: #2d1f1f;
        --error-border: #7f1d1d;
        --error-accent: #f87171;
        --ref-event-bg: #1e2a3a;
        --ref-event-border: #3b5998;
        --ref-event-color: #60a5fa;
        --ref-event-hover: #253545;
        --ref-addr-bg: #2a2518;
        --ref-addr-border: #92400e;
        --ref-addr-color: #fbbf24;
        --ref-addr-hover: #352f1e;
        --shadow: rgba(0,0,0,0.3);
        --shadow-accent: rgba(129, 140, 248, 0.2);
      }
    }
    html.dark {
      --bg-page: #121212;
      --bg-container: #1e1e1e;
      --bg-card: #1e1e1e;
      --bg-secondary: #252525;
      --bg-input: #2a2a2a;
      --bg-badge: #2a2a2a;
      --bg-badge-hover: #3a3a3a;
      --bg-reply-badge: #2d2d4a;
      --text-primary: #e4e4e7;
      --text-secondary: #a1a1aa;
      --text-muted: #71717a;
      --text-content: #e4e4e7;
      --border-color: #333333;
      --border-light: #333333;
      --accent: #818cf8;
      --accent-hover: #6366f1;
      --accent-secondary: #a78bfa;
      --success: #4ade80;
      --success-bg: #22c55e;
      --success-hover: #16a34a;
      --link-preview-bg: #252525;
      --quoted-bg: #252525;
      --error-bg: #2d1f1f;
      --error-border: #7f1d1d;
      --error-accent: #f87171;
      --ref-event-bg: #1e2a3a;
      --ref-event-border: #3b5998;
      --ref-event-color: #60a5fa;
      --ref-event-hover: #253545;
      --ref-addr-bg: #2a2518;
      --ref-addr-border: #92400e;
      --ref-addr-color: #fbbf24;
      --ref-addr-hover: #352f1e;
      --shadow: rgba(0,0,0,0.3);
      --shadow-accent: rgba(129, 140, 248, 0.2);
    }
    html.light {
      --bg-page: #f5f5f5;
      --bg-container: #ffffff;
      --bg-card: #ffffff;
      --bg-secondary: #f8f9fa;
      --bg-input: #ffffff;
      --bg-badge: #f0f0f0;
      --bg-badge-hover: #e0e0e0;
      --bg-reply-badge: #e7eaff;
      --text-primary: #333333;
      --text-secondary: #666666;
      --text-muted: #999999;
      --text-content: #24292e;
      --border-color: #e1e4e8;
      --border-light: #dee2e6;
      --accent: #667eea;
      --accent-hover: #5568d3;
      --accent-secondary: #764ba2;
      --success: #2e7d32;
      --success-bg: #28a745;
      --success-hover: #218838;
      --link-preview-bg: #fafbfc;
      --quoted-bg: #f8f9fa;
      --error-bg: #fff5f5;
      --error-border: #fecaca;
      --error-accent: #dc2626;
      --ref-event-bg: #e8f4fd;
      --ref-event-border: #b8daff;
      --ref-event-color: #0056b3;
      --ref-event-hover: #d1e9fc;
      --ref-addr-bg: #fff3e0;
      --ref-addr-border: #ffcc80;
      --ref-addr-color: #e65100;
      --ref-addr-hover: #ffe0b2;
      --shadow: rgba(0,0,0,0.1);
      --shadow-accent: rgba(102, 126, 234, 0.15);
    }
    * { box-sizing: border-box; margin: 0; padding: 0; }
    html { scroll-behavior: smooth; }
    .scroll-top {
      position: fixed;
      bottom: 20px;
      right: max(20px, calc((100vw - 840px) / 2 - 60px));
      width: 44px;
      height: 44px;
      background: var(--accent);
      color: white;
      border-radius: 50%;
      display: flex;
      align-items: center;
      justify-content: center;
      text-decoration: none;
      font-size: 20px;
      font-weight: bold;
      box-shadow: 0 2px 8px var(--shadow);
      opacity: 0.8;
      transition: opacity 0.2s, background 0.2s;
      z-index: 1000;
    }
    .scroll-top:hover {
      opacity: 1;
      background: var(--accent-hover);
    }
    @media (max-width: 600px) {
      .scroll-top {
        width: 40px;
        height: 40px;
        font-size: 18px;
        bottom: 16px;
        right: 16px;
      }
    }
    @keyframes flashFadeOut {
      0%, 60% { opacity: 1; max-height: 100px; padding: 12px; margin-bottom: 16px; }
      100% { opacity: 0; max-height: 0; padding: 0; margin-bottom: 0; overflow: hidden; }
    }
    .flash-message {
      background: var(--success-bg);
      color: white;
      border: 1px solid var(--success);
      border-radius: 4px;
      animation: flashFadeOut 3s ease-out forwards;
    }
    body {
      font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, Oxygen, Ubuntu, Cantarell, sans-serif;
      line-height: 1.6;
      color: var(--text-primary);
      background: var(--bg-page);
      padding: 20px;
    }
    .container {
      max-width: 800px;
      margin: 0 auto;
      background: var(--bg-container);
      border-radius: 8px;
      box-shadow: 0 2px 8px var(--shadow);
    }
    .sticky-section {
      position: sticky;
      top: 0;
      z-index: 100;
      background: var(--bg-container);
    }
    nav {
      padding: 12px 15px;
      background: var(--bg-secondary);
      border-bottom: 1px solid var(--border-light);
      display: flex;
      align-items: center;
      gap: 8px;
      flex-wrap: wrap;
    }
    .post-form {
      padding: 12px 16px;
      background: var(--bg-secondary);
      border-bottom: 1px solid var(--border-light);
    }
    .post-form textarea {
      width: 100%;
      padding: 8px 12px;
      border: 1px solid var(--border-color);
      border-radius: 4px;
      font-size: 14px;
      font-family: inherit;
      height: 36px;
      min-height: 36px;
      resize: none;
      margin-bottom: 0;
      background: var(--bg-input);
      color: var(--text-primary);
      box-sizing: border-box;
      transition: height 0.15s ease, min-height 0.15s ease;
      overflow: hidden;
    }
    .post-form:focus-within textarea {
      height: 80px;
      min-height: 80px;
      resize: vertical;
      margin-bottom: 10px;
      overflow: auto;
    }
    .post-form button[type="submit"] {
      display: none;
      padding: 8px 16px;
      background: linear-gradient(135deg, var(--accent) 0%, var(--accent-secondary) 100%) !important;
      color: white;
      border: none;
      border-radius: 4px;
      font-size: 14px;
      font-weight: 600;
      cursor: pointer;
    }
    .post-form:focus-within button[type="submit"] {
      display: block;
    }
    .nav-tab {
      padding: 8px 16px;
      background: var(--bg-badge);
      color: var(--text-secondary);
      text-decoration: none;
      border-radius: 4px;
      font-size: 14px;
      transition: background 0.2s, color 0.2s;
    }
    .nav-tab:hover { background: var(--bg-badge-hover); }
    .nav-tab.active {
      background: var(--accent);
      color: white;
    }
    .nav-tab.active:hover { background: var(--accent-hover); }
    .kind-filter {
      display: flex;
      gap: 16px;
      padding: 6px 20px;
      font-size: 12px;
      border-bottom: 1px solid var(--border);
    }
    .kind-filter a {
      color: var(--text-muted);
      text-decoration: none;
      padding: 2px 0;
      border-bottom: 2px solid transparent;
    }
    .kind-filter a:hover {
      color: var(--text-primary);
    }
    .kind-filter a.active {
      color: var(--text-primary);
      border-bottom-color: var(--accent);
    }
    .kind-filter-spacer {
      flex-grow: 1;
    }
    .edit-profile-link {
      color: var(--text-muted);
      text-decoration: none;
      padding: 2px 8px;
      border: 1px solid var(--border);
      border-radius: 4px;
      font-size: 11px;
    }
    .edit-profile-link:hover {
      color: var(--text-primary);
      border-color: var(--text-muted);
    }
    main { padding: 12px 20px 20px 20px; min-height: 400px; }
    .meta-info {
      background: var(--bg-secondary);
      padding: 12px;
      border-radius: 4px;
      font-size: 13px;
      color: var(--text-secondary);
      margin: 16px 0;
      display: flex;
      gap: 16px;
      justify-content: center;
      flex-wrap: wrap;
    }
    .meta-item { display: flex; align-items: center; gap: 4px; }
    .meta-label { font-weight: 600; }
    .note {
      background: var(--bg-card);
      border: 1px solid var(--border-color);
      border-radius: 6px;
      padding: 16px;
      margin: 12px 0;
      transition: box-shadow 0.2s;
    }
    .note:hover { box-shadow: 0 2px 8px var(--shadow); }
    .note-content {
      font-size: 15px;
      line-height: 1.6;
      margin: 12px 0;
      color: var(--text-content);
      white-space: pre-wrap;
      word-wrap: break-word;
    }
    .note-content img {
      max-width: 100%;
      border-radius: 8px;
      margin-top: 8px;
      display: block;
    }
    .image-gallery {
      display: flex;
      flex-wrap: wrap;
      gap: 8px;
      margin-top: 8px;
    }
    .image-gallery img {
      max-width: calc(50% - 4px);
      flex: 1 1 calc(50% - 4px);
      margin-top: 0;
      object-fit: cover;
      aspect-ratio: 1;
    }
    /* Kind 20 picture note styles */
    .picture-note {
      margin: 12px 0;
    }
    .picture-title {
      font-size: 18px;
      font-weight: 600;
      color: var(--text-primary);
      margin-bottom: 12px;
    }
    .picture-gallery {
      display: flex;
      flex-direction: column;
      gap: 8px;
    }
    .picture-image {
      max-width: 100%;
      border-radius: 8px;
      display: block;
    }
    .picture-caption {
      font-size: 14px;
      color: var(--text-muted);
      margin-top: 12px;
      line-height: 1.5;
    }
    /* Kind 6 repost styles */
    .repost-indicator {
      font-size: 12px;
      color: var(--text-muted);
      margin-bottom: 8px;
    }
    .reposted-note {
      border: 1px solid var(--border-color);
      border-radius: 8px;
      padding: 12px;
      background: var(--bg-secondary);
    }
    .reposted-note .note-author {
      margin-bottom: 8px;
    }
    .reposted-note .author-avatar {
      width: 32px;
      height: 32px;
    }
    .repost-empty {
      font-style: italic;
      color: var(--text-muted);
    }
    /* View note link style for quoted/reposted notes */
    .view-note-link {
      display: block;
      margin-top: 8px;
      color: var(--accent-color);
      text-decoration: none;
      font-size: 0.9em;
    }
    .view-note-link:hover {
      text-decoration: underline;
    }
    .quoted-note {
      border: 1px solid var(--border-color);
      border-radius: 8px;
      padding: 12px;
      margin-top: 12px;
      background: var(--bg-secondary);
      cursor: pointer;
      overflow: hidden;
    }
    .quoted-note img {
      max-width: 100%;
      height: auto;
    }
    .quoted-note:hover {
      border-color: var(--accent-color);
    }
    .quoted-note .note-author {
      margin-bottom: 8px;
    }
    .quoted-note .author-avatar {
      width: 32px;
      height: 32px;
    }
    .quoted-note-fallback {
      font-style: italic;
      color: var(--text-muted);
    }
    /* Kind 9735 zap receipt styles */
    .zap-content {
      display: flex;
      align-items: flex-start;
      gap: 12px;
    }
    .zap-icon {
      font-size: 20px;
      line-height: 1;
    }
    .zap-info {
      flex: 1;
    }
    .zap-header {
      display: flex;
      flex-wrap: wrap;
      align-items: center;
      gap: 6px;
      font-size: 15px;
    }
    .zap-sender, .zap-recipient {
      font-weight: 600;
      color: var(--accent);
      text-decoration: none;
    }
    .zap-sender:hover, .zap-recipient:hover {
      text-decoration: underline;
    }
    .zap-action {
      color: var(--text-muted);
    }
    .zap-amount {
      font-weight: 600;
      color: var(--text-primary);
    }
    .zap-comment {
      margin-top: 8px;
      font-size: 14px;
      color: var(--text-primary);
    }
    .zap-target {
      margin-top: 8px;
      font-size: 13px;
    }
    /* Kind 30311 live event styles */
    .live-event {
      padding: 0;
      overflow: hidden;
    }
    .live-event-thumbnail {
      position: relative;
      width: 100%;
      aspect-ratio: 16 / 9;
      background: var(--bg-tertiary);
      overflow: hidden;
    }
    .live-event-thumbnail img {
      width: 100%;
      height: 100%;
      object-fit: cover;
    }
    .live-event-thumbnail-placeholder {
      width: 100%;
      height: 100%;
      display: flex;
      align-items: center;
      justify-content: center;
      background: linear-gradient(135deg, var(--bg-tertiary) 0%, var(--bg-secondary) 100%);
    }
    .live-event-thumbnail-placeholder span {
      font-size: 48px;
      opacity: 0.3;
    }
    .live-event-overlay {
      position: absolute;
      top: 12px;
      left: 12px;
      display: flex;
      align-items: center;
      gap: 8px;
    }
    .live-badge {
      display: inline-flex;
      align-items: center;
      gap: 4px;
      padding: 4px 10px;
      border-radius: 4px;
      font-size: 12px;
      font-weight: 700;
      text-transform: uppercase;
      letter-spacing: 0.5px;
      background: rgba(0,0,0,0.7);
      color: white;
      backdrop-filter: blur(4px);
    }
    .live-badge.live {
      background: #dc2626;
      animation: pulse 2s ease-in-out infinite;
    }
    .live-badge.live::before {
      content: "";
      width: 8px;
      height: 8px;
      background: white;
      border-radius: 50%;
    }
    @keyframes pulse {
      0%, 100% { opacity: 1; }
      50% { opacity: 0.8; }
    }
    .live-badge.planned {
      background: #2563eb;
    }
    .live-badge.ended {
      background: rgba(0,0,0,0.6);
    }
    .live-viewers {
      display: inline-flex;
      align-items: center;
      gap: 4px;
      padding: 4px 10px;
      border-radius: 4px;
      font-size: 12px;
      font-weight: 500;
      background: rgba(0,0,0,0.7);
      color: white;
      backdrop-filter: blur(4px);
    }
    .live-event-body {
      padding: 16px;
    }
    .live-event-title {
      font-size: 18px;
      font-weight: 600;
      color: var(--text-primary);
      margin: 0 0 8px 0;
      line-height: 1.3;
    }
    .live-event-summary {
      font-size: 14px;
      color: var(--text-secondary);
      margin: 0 0 12px 0;
      line-height: 1.5;
      display: -webkit-box;
      -webkit-line-clamp: 2;
      -webkit-box-orient: vertical;
      overflow: hidden;
    }
    .live-event-host {
      display: flex;
      align-items: center;
      gap: 8px;
      margin-bottom: 4px;
      font-size: 14px;
    }
    .host-label {
      color: var(--text-muted);
    }
    .host-link {
      display: inline-flex;
      align-items: center;
      gap: 6px;
      text-decoration: none;
      color: var(--text-primary);
    }
    .host-link:hover {
      color: var(--accent);
    }
    .host-avatar {
      width: 20px;
      height: 20px;
      border-radius: 50%;
      object-fit: cover;
    }
    .host-name {
      font-weight: 500;
    }
    .live-event-meta {
      display: flex;
      align-items: center;
      gap: 16px;
      font-size: 13px;
      color: var(--text-muted);
      margin-bottom: 12px;
    }
    .live-event-meta-item {
      display: flex;
      align-items: center;
      gap: 4px;
    }
    .live-event-tags {
      display: flex;
      gap: 6px;
      flex-wrap: wrap;
      margin-bottom: 12px;
    }
    .live-hashtag {
      font-size: 12px;
      color: var(--accent);
      background: var(--bg-secondary);
      padding: 4px 10px;
      border-radius: 14px;
      text-decoration: none;
    }
    .live-hashtag:hover {
      background: var(--bg-tertiary);
    }
    .live-participants {
      display: flex;
      align-items: center;
      gap: 12px;
      padding: 12px 0;
      border-top: 1px solid var(--border);
      margin-top: 4px;
    }
    .participants-list {
      display: flex;
      gap: 8px;
      flex-wrap: wrap;
      align-items: center;
    }
    .participant {
      display: inline-flex;
      align-items: center;
      gap: 6px;
      text-decoration: none;
      color: var(--text-primary);
      font-size: 13px;
      padding: 4px 10px 4px 4px;
      background: var(--bg-secondary);
      border-radius: 20px;
      transition: background 0.15s;
    }
    .participant:hover {
      background: var(--bg-tertiary);
    }
    .participant-avatar {
      width: 24px;
      height: 24px;
      border-radius: 50%;
      object-fit: cover;
      background: var(--bg-tertiary);
    }
    .participant-avatar-placeholder {
      width: 24px;
      height: 24px;
      border-radius: 50%;
      background: var(--bg-tertiary);
      display: flex;
      align-items: center;
      justify-content: center;
      font-size: 11px;
      color: var(--text-muted);
    }
    .participant-name {
      font-weight: 500;
    }
    .participant-role {
      font-size: 10px;
      color: var(--accent);
      font-weight: 600;
      text-transform: uppercase;
      margin-left: 2px;
    }
    .live-event-actions {
      padding: 12px 16px;
      display: flex;
      gap: 10px;
      background: var(--bg-secondary);
    }
    .live-action-btn {
      display: inline-flex;
      align-items: center;
      justify-content: center;
      gap: 8px;
      padding: 10px 20px;
      border-radius: 8px;
      font-size: 14px;
      font-weight: 600;
      text-decoration: none;
      transition: all 0.15s ease;
      flex: 1;
    }
    .stream-btn {
      background: #dc2626;
      color: white;
    }
    .stream-btn:hover {
      background: #b91c1c;
    }
    .recording-btn {
      background: var(--bg-tertiary);
      color: var(--text-primary);
      border: 1px solid var(--border);
    }
    .recording-btn:hover {
      background: var(--bg-secondary);
    }
    /* Highlight (kind 9802) styles */
    .highlight {
      padding: 16px 20px;
    }
    .highlight-blockquote {
      border-left: 4px solid var(--accent);
      padding-left: 16px;
      margin: 0 0 12px 0;
      font-style: italic;
      font-size: 15px;
      line-height: 1.6;
      color: var(--text-content);
    }
    .highlight-context {
      margin-top: 8px;
      padding: 12px;
      background: var(--bg-secondary);
      border-radius: 6px;
      font-size: 13px;
      color: var(--text-secondary);
      font-style: normal;
      line-height: 1.5;
    }
    .highlight-comment {
      margin-top: 12px;
      font-size: 14px;
      color: var(--text-primary);
      font-style: normal;
    }
    .highlight-source {
      margin-top: 12px;
      padding-top: 12px;
      border-top: 1px solid var(--border-color);
      font-size: 13px;
    }
    .highlight-source-link {
      color: var(--accent);
      text-decoration: none;
      word-break: break-all;
    }
    .highlight-source-link:hover {
      text-decoration: underline;
    }
    .highlight-meta {
      display: flex;
      justify-content: space-between;
      align-items: center;
      margin-top: 12px;
      padding-top: 12px;
      border-top: 1px solid var(--border-color);
    }
    .highlight-author {
      display: flex;
      align-items: center;
      gap: 8px;
    }
    .highlight-author-avatar {
      width: 24px;
      height: 24px;
      border-radius: 50%;
      object-fit: cover;
    }
    .highlight-author-name {
      color: var(--text-secondary);
      text-decoration: none;
      font-size: 13px;
    }
    .highlight-author-name:hover {
      color: var(--accent);
    }
    .highlight-time {
      font-size: 12px;
      color: var(--text-muted);
    }
    /* Bookmark list (kind 10003) styles */
    .bookmarks {
      padding: 16px 20px;
    }
    .bookmarks-header {
      display: flex;
      align-items: center;
      gap: 8px;
      margin-bottom: 16px;
    }
    .bookmarks-icon {
      font-size: 18px;
    }
    .bookmarks-title {
      font-size: 16px;
      font-weight: 600;
      color: var(--text-primary);
    }
    .bookmarks-count {
      font-size: 13px;
      color: var(--text-muted);
      margin-left: auto;
    }
    .bookmarks-section {
      margin-bottom: 16px;
    }
    .bookmarks-section:last-child {
      margin-bottom: 0;
    }
    .bookmarks-section-title {
      font-size: 12px;
      font-weight: 600;
      color: var(--text-secondary);
      text-transform: uppercase;
      margin-bottom: 8px;
      letter-spacing: 0.5px;
    }
    .bookmarks-list {
      display: flex;
      flex-direction: column;
      gap: 6px;
    }
    .bookmark-item {
      display: flex;
      align-items: center;
      gap: 8px;
      padding: 8px 12px;
      background: var(--bg-secondary);
      border-radius: 6px;
      font-size: 13px;
      color: var(--text-content);
      text-decoration: none;
      transition: background 0.15s ease;
    }
    .bookmark-item:hover {
      background: var(--bg-hover);
    }
    .bookmark-item-icon {
      font-size: 14px;
      opacity: 0.7;
    }
    .bookmark-item-text {
      overflow: hidden;
      text-overflow: ellipsis;
      white-space: nowrap;
    }
    .bookmark-hashtag {
      color: var(--accent);
    }
    .bookmarks-meta {
      display: flex;
      justify-content: space-between;
      align-items: center;
      margin-top: 16px;
      padding-top: 12px;
      border-top: 1px solid var(--border-color);
    }
    .bookmarks-author {
      display: flex;
      align-items: center;
      gap: 8px;
    }
    .bookmarks-author-avatar {
      width: 24px;
      height: 24px;
      border-radius: 50%;
      object-fit: cover;
    }
    .bookmarks-author-name {
      color: var(--text-secondary);
      text-decoration: none;
      font-size: 13px;
    }
    .bookmarks-author-name:hover {
      color: var(--accent);
    }
    .bookmarks-time {
      font-size: 12px;
      color: var(--text-muted);
    }
    .live-event-player {
      padding: 0;
      border-top: 1px solid var(--border);
    }
    .live-embed-iframe {
      width: 100%;
      height: 360px;
      display: block;
    }
    .live-event .note-meta {
      padding: 12px 16px;
      border-top: 1px solid var(--border);
    }
    /* Article preview styles (kind 30023 in timeline) */
    .article-preview {
      margin: 12px 0;
    }
    .article-preview-image {
      width: 100%;
      max-height: 200px;
      object-fit: cover;
      border-radius: 8px;
      margin-bottom: 12px;
    }
    .article-preview-title {
      font-size: 18px;
      font-weight: 600;
      color: var(--text-primary);
      margin-bottom: 8px;
      line-height: 1.3;
    }
    .article-preview-summary {
      font-size: 14px;
      color: var(--text-muted);
      margin-bottom: 12px;
      line-height: 1.5;
      display: -webkit-box;
      -webkit-line-clamp: 3;
      -webkit-box-orient: vertical;
      overflow: hidden;
    }
    /* Full article styles (kind 30023 in thread view) */
    .long-form-article {
      margin: 12px 0;
    }
    .article-header-image {
      width: 100%;
      max-height: 300px;
      object-fit: cover;
      border-radius: 8px;
      margin-bottom: 16px;
    }
    .article-title {
      font-size: 24px;
      font-weight: 700;
      color: var(--text-primary);
      margin-bottom: 12px;
      line-height: 1.3;
    }
    .article-summary {
      font-size: 16px;
      color: var(--text-muted);
      margin-bottom: 12px;
      font-style: italic;
      line-height: 1.5;
    }
    .article-published {
      font-size: 13px;
      color: var(--text-muted);
      margin-bottom: 16px;
    }
    .article-content {
      font-size: 15px;
      line-height: 1.8;
      color: var(--text-primary);
    }
    .article-content h1, .article-content h2, .article-content h3 {
      margin-top: 24px;
      margin-bottom: 12px;
      color: var(--text-primary);
    }
    .article-content h1 { font-size: 22px; }
    .article-content h2 { font-size: 20px; }
    .article-content h3 { font-size: 18px; }
    .article-content p {
      margin-bottom: 16px;
    }
    .article-content ul, .article-content ol {
      margin-bottom: 16px;
      padding-left: 24px;
    }
    .article-content li {
      margin-bottom: 8px;
    }
    .article-content blockquote {
      border-left: 4px solid var(--border-color);
      padding-left: 16px;
      margin: 16px 0;
      color: var(--text-muted);
      font-style: italic;
    }
    .article-content code {
      background: var(--bg-secondary);
      padding: 2px 6px;
      border-radius: 4px;
      font-family: monospace;
      font-size: 14px;
    }
    .article-content pre {
      background: var(--bg-secondary);
      padding: 16px;
      border-radius: 8px;
      overflow-x: auto;
      margin-bottom: 16px;
    }
    .article-content pre code {
      background: none;
      padding: 0;
    }
    .article-content a {
      color: var(--accent-color);
    }
    .article-content img {
      max-width: 100%;
      border-radius: 8px;
      margin: 16px 0;
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
      color: var(--accent);
      text-decoration: none;
    }
    .note-content a:hover {
      text-decoration: underline;
    }
    .link-preview {
      display: flex;
      border: 1px solid var(--border-color);
      border-radius: 8px;
      margin: 12px 0;
      overflow: hidden;
      text-decoration: none;
      color: inherit;
      background: var(--link-preview-bg);
      transition: border-color 0.2s, box-shadow 0.2s;
    }
    .link-preview:hover {
      border-color: var(--accent);
      box-shadow: 0 2px 8px var(--shadow-accent);
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
      color: var(--text-muted);
      text-transform: uppercase;
      letter-spacing: 0.5px;
      margin-bottom: 2px;
    }
    .link-preview-title {
      font-size: 14px;
      font-weight: 600;
      color: var(--text-content);
      line-height: 1.3;
      margin-bottom: 4px;
      display: -webkit-box;
      -webkit-line-clamp: 2;
      -webkit-box-orient: vertical;
      overflow: hidden;
    }
    .link-preview-desc {
      font-size: 12px;
      color: var(--text-secondary);
      line-height: 1.4;
      display: -webkit-box;
      -webkit-line-clamp: 2;
      -webkit-box-orient: vertical;
      overflow: hidden;
    }
    .quoted-note {
      background: var(--quoted-bg);
      border: 1px solid var(--border-color);
      border-left: 3px solid var(--accent);
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
      color: var(--text-content);
    }
    .quoted-note .quoted-author-npub {
      font-family: monospace;
      font-size: 11px;
      color: var(--accent);
    }
    .quoted-note .quoted-content {
      color: var(--text-content);
      white-space: pre-wrap;
      word-wrap: break-word;
    }
    .quoted-note .quoted-meta {
      font-size: 11px;
      color: var(--text-secondary);
      margin-top: 8px;
    }
    .quoted-note .quoted-meta a {
      color: var(--accent);
    }
    .quoted-note.quoted-article .quoted-article-title {
      font-weight: 600;
      font-size: 15px;
      margin-bottom: 6px;
      color: var(--text-content);
    }
    .quoted-note.quoted-article .quoted-article-summary {
      color: var(--text-secondary);
      font-size: 14px;
      line-height: 1.4;
    }
    .quoted-note-error {
      background: var(--error-bg);
      border: 1px solid var(--error-border);
      border-left: 3px solid var(--error-accent);
      border-radius: 4px;
      padding: 8px 12px;
      margin: 8px 0;
      font-size: 13px;
      color: var(--text-secondary);
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
      background: var(--ref-event-bg);
      border: 1px solid var(--ref-event-border);
      color: var(--ref-event-color);
    }
    .nostr-ref-event:hover {
      background: var(--ref-event-hover);
    }
    .nostr-ref-profile {
      display: inline;
      padding: 0;
      margin: 0;
      border: none;
      border-radius: 0;
      background: none;
      color: var(--success);
      font-weight: 500;
    }
    .nostr-ref-profile:hover {
      background: none;
      text-decoration: underline;
    }
    .nostr-ref-addr {
      background: var(--ref-addr-bg);
      border: 1px solid var(--ref-addr-border);
      color: var(--ref-addr-color);
    }
    .nostr-ref-addr:hover {
      background: var(--ref-addr-hover);
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
      border: 2px solid var(--border-color);
    }
    .author-info {
      display: flex;
      flex-direction: row;
      align-items: baseline;
      gap: 6px;
      flex-wrap: wrap;
    }
    .author-name {
      font-weight: 600;
      font-size: 15px;
      color: var(--text-content);
    }
    .author-nip05 {
      font-size: 12px;
      color: var(--accent);
    }
    .author-nip05::before {
      content: "·";
      margin-right: 6px;
      color: var(--text-secondary);
    }
    .author-time {
      font-size: 12px;
      color: var(--text-secondary);
    }
    .author-time::before {
      content: "·";
      margin-right: 6px;
      color: var(--text-secondary);
    }
    .note-footer {
      display: flex;
      gap: 16px;
      flex-wrap: wrap;
      margin-top: 12px;
      padding-top: 12px;
      border-top: 1px solid var(--border-color);
      align-items: center;
      font-size: 13px;
    }
    .note-footer-actions {
      display: flex;
      gap: 12px;
      align-items: center;
    }
    .note-footer-reactions {
      display: flex;
      gap: 8px;
      align-items: center;
      margin-left: auto;
    }
    .footer-separator {
      color: var(--border-color);
      margin: 0 4px;
    }
    .note-reactions {
      display: flex;
      gap: 8px;
      flex-wrap: wrap;
      margin: 12px 0;
      padding: 8px 0;
      border-top: 1px solid var(--border-color);
    }
    .reaction-badge {
      display: inline-flex;
      align-items: center;
      gap: 4px;
      padding: 4px 10px;
      background: var(--bg-badge);
      border-radius: 16px;
      font-size: 13px;
      color: var(--text-secondary);
    }
    .reply-count-badge {
      background: var(--bg-reply-badge);
      color: var(--accent);
    }
    .note-meta {
      display: flex;
      gap: 16px;
      font-size: 12px;
      color: var(--text-secondary);
      margin-top: 12px;
      padding-top: 12px;
      border-top: 1px solid var(--border-color);
      flex-wrap: wrap;
    }
    .pubkey {
      font-family: monospace;
      font-size: 11px;
      color: var(--accent);
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
      background: var(--bg-card);
      border: 1px solid var(--accent);
      color: var(--accent);
      text-decoration: none;
      border-radius: 4px;
      font-size: 13px;
      transition: all 0.2s;
    }
    .link:hover {
      background: var(--accent);
      color: white;
    }
    .pagination {
      display: flex;
      justify-content: center;
      gap: 12px;
      margin: 12px 0 0 0;
      padding: 12px 0;
      border-top: 1px solid var(--border-color);
    }
    .actions {
      display: flex;
      gap: 12px;
      flex-wrap: wrap;
      margin-top: 12px;
    }
    .action-form {
      background: var(--bg-secondary);
      padding: 12px;
      border-radius: 4px;
      border: 1px solid var(--border-light);
    }
    .action-form h4 {
      font-size: 14px;
      margin-bottom: 8px;
      color: var(--text-secondary);
    }
    .action-field {
      margin: 8px 0;
    }
    .action-field label {
      display: block;
      font-size: 13px;
      font-weight: 600;
      color: var(--text-secondary);
      margin-bottom: 4px;
    }
    .action-field input,
    .action-field textarea {
      width: 100%;
      padding: 8px;
      border: 1px solid var(--border-light);
      border-radius: 4px;
      font-size: 14px;
      font-family: inherit;
      background: var(--bg-input);
      color: var(--text-primary);
    }
    .action-field textarea {
      min-height: 80px;
      resize: vertical;
    }
    button[type="submit"] {
      padding: 8px 16px;
      background: var(--success-bg);
      color: white;
      border: none;
      border-radius: 4px;
      cursor: pointer;
      font-size: 14px;
      margin-top: 8px;
    }
    button[type="submit"]:hover {
      background: var(--success-hover);
    }
    button[type="submit"].reaction-badge {
      display: inline-flex;
      align-items: center;
      gap: 4px;
      padding: 4px 10px;
      background: var(--bg-badge);
      color: var(--text-secondary);
      border: none;
      border-radius: 16px;
      font-size: 13px;
      line-height: 1.4;
      cursor: pointer;
      font-family: inherit;
      margin-top: 0;
    }
    button[type="submit"].reaction-badge:hover {
      background: var(--bg-badge-hover);
    }
    /* Utility classes */
    .ml-auto { margin-left: auto; }
    .mr-md { margin-right: 12px; }
    .flex { display: flex; }
    .flex-center { display: flex; align-items: center; }
    .gap-sm { gap: 8px; }
    .gap-md { gap: 12px; }
    .gap-lg { gap: 16px; }
    .text-link { color: var(--accent); text-decoration: none; }
    .text-link:hover { text-decoration: underline; }
    button.text-link, button.repost-btn { background: none !important; border: none !important; font: inherit; cursor: pointer; padding: 0 !important; margin: 0 !important; border-radius: 0 !important; color: var(--accent) !important; }
    button.text-link:hover { text-decoration: underline; }
    .note-footer {
      display: flex;
      gap: 16px;
      flex-wrap: wrap;
      margin-top: 12px;
      padding-top: 12px;
      border-top: 1px solid var(--border-color);
      align-items: center;
      font-size: 13px;
    }
    .note-footer-actions {
      display: flex;
      gap: 16px;
      flex-wrap: wrap;
    }
    .note-footer-reactions {
      display: flex;
      gap: 8px;
      flex-wrap: wrap;
      margin-left: auto;
    }
    .text-muted { color: var(--text-secondary); text-decoration: none; }
    .text-sm { font-size: 13px; }
    .text-xs { font-size: 12px; }
    .font-medium { font-weight: 500; }
    .inline-form { display: inline; margin: 0; }
    .ghost-btn {
      background: none;
      border: none;
      color: var(--text-secondary);
      cursor: pointer;
      font-family: inherit;
      padding: 0;
    }
    .accent-btn {
      appearance: none;
      -webkit-appearance: none;
      background: none !important;
      border: none !important;
      color: var(--accent) !important;
      cursor: pointer;
      font-family: inherit;
      font-size: inherit;
      padding: 0 !important;
      margin: 0 !important;
      line-height: inherit;
      border-radius: 0 !important;
    }
    .accent-btn:hover {
      text-decoration: underline;
      background: none !important;
    }
    /* Note reactions */
    .note-reactions-bar {
      margin: 12px 0;
      padding: 8px 0;
      border-top: 1px solid var(--border-color);
    }
    .note-reactions {
      margin: 8px 0;
    }
    /* Settings dropdown */
    .settings-dropdown { position: relative; }
    .settings-toggle {
      cursor: pointer;
      list-style: none;
      font-size: 16px;
    }
    .settings-menu {
      position: absolute;
      right: 0;
      top: 100%;
      margin-top: 8px;
      background: var(--bg-card);
      border: 1px solid var(--border-color);
      border-radius: 4px;
      padding: 10px 14px;
      box-shadow: 0 4px 12px var(--shadow);
      z-index: 100;
      white-space: nowrap;
      font-size: 12px;
      color: var(--text-secondary);
    }
    .settings-item { margin-bottom: 8px; }
    .settings-item:last-child { margin-bottom: 0; }
    .settings-divider {
      padding-top: 8px;
      border-top: 1px solid var(--border-light);
    }
    /* Notification bell */
    .notification-bell {
      position: relative;
      text-decoration: none;
      font-size: 16px;
    }
    .notification-badge {
      position: absolute;
      top: -4px;
      right: -6px;
      width: 8px;
      height: 8px;
      background: var(--accent-color);
      border-radius: 50%;
    }
    /* Checkbox toggle */
    .checkbox-link {
      text-decoration: none;
      display: flex;
      align-items: center;
      gap: 6px;
      color: var(--text-secondary);
    }
    .checkbox-box {
      display: inline-block;
      width: 14px;
      height: 14px;
      border: 2px solid var(--border-color);
      border-radius: 3px;
      background: transparent;
      position: relative;
    }
    .checkbox-box.checked {
      border-color: var(--accent);
      background: var(--accent);
    }
    .checkbox-check {
      position: absolute;
      left: 3px;
      top: 0px;
      width: 4px;
      height: 7px;
      border: solid white;
      border-width: 0 2px 2px 0;
      transform: rotate(45deg);
    }
    /* Note actions */
    .note-actions {
      display: flex;
      gap: 16px;
      align-items: center;
      margin-left: auto;
    }
    /* Error/alert box */
    .error-box {
      background: var(--error-bg);
      color: var(--error-accent);
      border: 1px solid var(--error-border);
      padding: 12px;
      border-radius: 4px;
      margin-bottom: 16px;
    }
    /* Reply form */
    .reply-form {
      background: var(--bg-card);
      padding: 16px;
      border-radius: 8px;
      border: 1px solid var(--border-color);
      margin: 16px 0;
    }
    .reply-form textarea {
      width: 100%;
      padding: 12px;
      border: 1px solid var(--border-color);
      border-radius: 4px;
      font-size: 14px;
      font-family: inherit;
      min-height: 60px;
      resize: vertical;
      margin-bottom: 10px;
      background: var(--bg-input);
      color: var(--text-primary);
      box-sizing: border-box;
    }
    .reply-form button[type="submit"] {
      padding: 10px 20px;
      background: linear-gradient(135deg, var(--accent) 0%, var(--accent-secondary) 100%);
      color: white;
      border: none;
      border-radius: 4px;
      font-size: 14px;
      font-weight: 600;
      cursor: pointer;
    }
    .reply-info {
      margin-bottom: 10px;
      font-size: 13px;
      color: var(--text-secondary);
    }
    .reply-info span {
      color: var(--accent);
      font-weight: 500;
    }
    .sr-only {
      position: absolute;
      width: 1px;
      height: 1px;
      padding: 0;
      margin: -1px;
      overflow: hidden;
      clip: rect(0, 0, 0, 0);
      white-space: nowrap;
      border: 0;
    }
    /* Login prompt box */
    .login-prompt-box {
      background: var(--bg-secondary);
      padding: 12px;
      border-radius: 8px;
      border: 1px solid var(--border-light);
      margin: 16px 0;
      text-align: center;
      color: var(--text-secondary);
      font-size: 14px;
    }
    /* Relay list in dropdown */
    .relay-item {
      padding: 1px 0;
      font-family: monospace;
      font-size: 11px;
      color: var(--text-secondary);
    }
    footer {
      text-align: center;
      padding: 20px;
      background: var(--bg-secondary);
      color: var(--text-secondary);
      font-size: 13px;
      border-top: 1px solid var(--border-light);
      border-radius: 0 0 8px 8px;
    }
    /* Accessibility: Screen reader only class */
    .sr-only {
      position: absolute;
      width: 1px;
      height: 1px;
      padding: 0;
      margin: -1px;
      overflow: hidden;
      clip: rect(0, 0, 0, 0);
      white-space: nowrap;
      border: 0;
    }
    /* Accessibility: Skip link for keyboard navigation */
    .skip-link {
      position: absolute;
      top: 0;
      left: 0;
      transform: translateY(-100%);
      background: var(--accent);
      color: white;
      padding: 8px 16px;
      z-index: 1000;
      text-decoration: none;
      border-radius: 0 0 4px 0;
    }
    .skip-link:focus {
      transform: translateY(0);
    }
  </style>
</head>
<body>
  <a href="#main-content" class="skip-link">Skip to main content</a>
  <div id="top" class="container">
    <div class="sticky-section">
      <nav>
        {{if .LoggedIn}}
        <a href="?kinds=1&limit=20&feed=follows{{if not .ShowReactions}}&fast=1{{end}}" class="nav-tab{{if eq .FeedMode "follows"}} active{{end}}">Follows</a>
        {{end}}
        <a href="?kinds=1&limit=20&feed=global{{if not .ShowReactions}}&fast=1{{end}}" class="nav-tab{{if or (eq .FeedMode "global") (not .LoggedIn)}} active{{end}}">Global</a>
        {{if .LoggedIn}}
        <a href="?kinds=1&limit=20&feed=me{{if not .ShowReactions}}&fast=1{{end}}" class="nav-tab{{if eq .FeedMode "me"}} active{{end}}">Me</a>
        {{end}}
        <div class="ml-auto flex-center gap-md">
          {{if .LoggedIn}}
          <a href="/html/notifications" class="notification-bell" title="Notifications">🔔{{if .HasUnreadNotifications}}<span class="notification-badge"></span>{{end}}</a>
          {{end}}
          <details class="settings-dropdown">
            <summary class="settings-toggle" title="Settings">⚙️</summary>
            <div class="settings-menu">
              <div class="settings-item">
                <a href="?kinds=1,6&limit=20&feed={{.FeedMode}}{{if .ShowReactions}}&fast=1{{end}}" class="checkbox-link">
                  <span class="checkbox-box{{if .ShowReactions}} checked{{end}}">{{if .ShowReactions}}<span class="checkbox-check"></span>{{end}}</span>
                  <span>Include reactions</span>
                </a>
              </div>
              <div class="settings-item">
                <form method="POST" action="/html/theme" class="inline-form">
                  <button type="submit" class="ghost-btn text-xs">Theme: {{.ThemeLabel}}</button>
                </form>
              </div>
              {{if .ActiveRelays}}
              <div class="settings-divider">
                <div class="settings-item">{{len .ActiveRelays}} relay{{if gt (len .ActiveRelays) 1}}s{{end}}:</div>
                {{range .ActiveRelays}}<div class="relay-item">{{.}}</div>{{end}}
              </div>
              {{end}}
            </div>
          </details>
          {{if .LoggedIn}}
          <a href="/html/logout" class="text-muted text-sm">Logout</a>
          {{else}}
          <a href="/html/login" class="text-link text-sm font-medium">Login</a>
          {{end}}
        </div>
      </nav>
      <div class="kind-filter">
        <a href="/html/timeline?kinds=1,6,20,30023,9802,30311&limit=20&feed={{.FeedMode}}{{if not .ShowReactions}}&fast=1{{end}}" class="{{if eq .KindFilter "all"}}active{{end}}">All</a>
        <a href="/html/timeline?kinds=1&limit=20&feed={{.FeedMode}}{{if not .ShowReactions}}&fast=1{{end}}" class="{{if eq .KindFilter "notes"}}active{{end}}">Notes</a>
        <a href="/html/timeline?kinds=20&limit=20&feed={{.FeedMode}}{{if not .ShowReactions}}&fast=1{{end}}" class="{{if eq .KindFilter "photos"}}active{{end}}">Photos</a>
        <a href="/html/timeline?kinds=30023&limit=20&feed={{.FeedMode}}{{if not .ShowReactions}}&fast=1{{end}}" class="{{if eq .KindFilter "reads"}}active{{end}}">Longform</a>
        {{if eq .FeedMode "me"}}<a href="/html/timeline?kinds=10003&limit=20&feed={{.FeedMode}}{{if not .ShowReactions}}&fast=1{{end}}" class="{{if eq .KindFilter "bookmarks"}}active{{end}}">Bookmarks</a>{{end}}
        <a href="/html/timeline?kinds=9802&limit=20&feed={{.FeedMode}}{{if not .ShowReactions}}&fast=1{{end}}" class="{{if eq .KindFilter "highlights"}}active{{end}}">Highlights</a>
        <a href="/html/timeline?kinds=30311&limit=20&feed={{.FeedMode}}{{if not .ShowReactions}}&fast=1{{end}}" class="{{if eq .KindFilter "livestreams"}}active{{end}}">Livestreams</a>
        {{if eq .FeedMode "me"}}<span class="kind-filter-spacer"></span><a href="/html/profile/edit" class="edit-profile-link">Edit Profile</a>{{end}}
      </div>
      {{if .LoggedIn}}
      <form method="POST" action="/html/post" class="post-form">
        <input type="hidden" name="csrf_token" value="{{.CSRFToken}}">
        <label for="post-content" class="sr-only">Write a new note</label>
        <textarea id="post-content" name="content" placeholder="What's on your mind?" required></textarea>
        <button type="submit">Post</button>
      </form>
      {{end}}
    </div>

    <main id="main-content">
      {{if .Error}}
      <div class="error-box">{{.Error}}</div>
      {{end}}
      {{if .Success}}
      <div class="flash-message">{{.Success}}</div>
      {{end}}

      {{range .Items}}
      {{$item := .}}
      {{if eq .Kind 9735}}
      <article class="note zap-receipt">
        <div class="zap-content">
          <span class="zap-icon">⚡</span>
          <div class="zap-info">
            <div class="zap-header">
              <a href="/html/profile/{{.ZapSenderNpub}}" class="zap-sender">
                {{if .ZapSenderProfile}}
                  {{if or .ZapSenderProfile.DisplayName .ZapSenderProfile.Name}}
                    {{if .ZapSenderProfile.DisplayName}}{{.ZapSenderProfile.DisplayName}}{{else}}{{.ZapSenderProfile.Name}}{{end}}
                  {{else}}{{.ZapSenderNpubShort}}{{end}}
                {{else}}{{.ZapSenderNpubShort}}{{end}}
              </a>
              <span class="zap-action">zapped</span>
              <a href="/html/profile/{{.ZapRecipientNpub}}" class="zap-recipient">
                {{if .ZapRecipientProfile}}
                  {{if or .ZapRecipientProfile.DisplayName .ZapRecipientProfile.Name}}
                    {{if .ZapRecipientProfile.DisplayName}}{{.ZapRecipientProfile.DisplayName}}{{else}}{{.ZapRecipientProfile.Name}}{{end}}
                  {{else}}{{.ZapRecipientNpubShort}}{{end}}
                {{else}}{{.ZapRecipientNpubShort}}{{end}}
              </a>
            </div>
            <div class="zap-amount">{{.ZapAmountSats}} sats</div>
            {{if .ZapComment}}<div class="zap-comment">{{.ZapComment}}</div>{{end}}
            {{if .ZappedEventID}}<div class="zap-target"><a href="/html/thread/{{.ZappedEventID}}" class="text-link">View zapped note</a></div>{{end}}
          </div>
        </div>
        <div class="note-meta">
          <span>{{formatTime .CreatedAt}}</span>
        </div>
      </article>
      {{else if eq .Kind 30311}}
      <article class="note live-event">
        <div class="live-event-thumbnail">
          {{if .LiveImage}}
          <img src="{{.LiveImage}}" alt="{{.LiveTitle}}">
          {{else}}
          <div class="live-event-thumbnail-placeholder"><span>LIVE</span></div>
          {{end}}
          <div class="live-event-overlay">
            {{if eq .LiveStatus "live"}}
            <span class="live-badge live">LIVE</span>
            {{else if eq .LiveStatus "planned"}}
            <span class="live-badge planned">SCHEDULED</span>
            {{else if eq .LiveStatus "ended"}}
            <span class="live-badge ended">ENDED</span>
            {{else}}
            <span class="live-badge">{{.LiveStatus}}</span>
            {{end}}
            {{if .LiveCurrentCount}}<span class="live-viewers">{{.LiveCurrentCount}} watching</span>{{end}}
          </div>
        </div>

        <div class="live-event-body">
          <h3 class="live-event-title">{{if .LiveTitle}}{{.LiveTitle}}{{else}}Live Event{{end}}</h3>
          {{if .LiveSummary}}<p class="live-event-summary">{{.LiveSummary}}</p>{{end}}

          {{if .LiveParticipants}}
          <div class="live-event-host">
            {{range .LiveParticipants}}{{if eq .Role "host"}}
            <span class="host-label">Host:</span>
            <a href="/html/profile/{{.Npub}}" class="host-link" title="{{.NpubShort}}">
              {{if and .Profile .Profile.Picture}}<img class="host-avatar" src="{{.Profile.Picture}}" alt="{{if .Profile.DisplayName}}{{.Profile.DisplayName}}{{else if .Profile.Name}}{{.Profile.Name}}{{else}}Host{{end}}'s avatar">{{end}}
              <span class="host-name">{{if and .Profile .Profile.DisplayName}}{{.Profile.DisplayName}}{{else if and .Profile .Profile.Name}}{{.Profile.Name}}{{else}}{{.NpubShort}}{{end}}</span>
            </a>
            {{end}}{{end}}
          </div>
          {{end}}

          <div class="live-event-meta">
            {{if .LiveStarts}}
            <span class="live-event-meta-item">{{if eq .LiveStatus "ended"}}Started{{else if eq .LiveStatus "live"}}Started{{else}}Starts{{end}}: {{formatTime .LiveStarts}}</span>
            {{end}}
            {{if and .LiveEnds (eq .LiveStatus "ended")}}
            <span class="live-event-meta-item">Ended: {{formatTime .LiveEnds}}</span>
            {{end}}
          </div>

          {{if .LiveHashtags}}
          <div class="live-event-tags">
            {{range .LiveHashtags}}<span class="live-hashtag">#{{.}}</span>{{end}}
          </div>
          {{end}}
        </div>

        <div class="live-event-actions">
          {{if .LiveEmbedURL}}
          <a href="{{.LiveEmbedURL}}" class="live-action-btn stream-btn" target="_blank" rel="noopener">Watch on zap.stream</a>
          {{else if and .LiveStreamingURL (ne .LiveStatus "ended")}}
          <a href="{{.LiveStreamingURL}}" class="live-action-btn stream-btn" target="_blank" rel="noopener">Watch Stream</a>
          {{end}}
          {{if and .LiveRecordingURL (eq .LiveStatus "ended")}}
          <a href="{{.LiveRecordingURL}}" class="live-action-btn recording-btn" target="_blank" rel="noopener">Watch Recording</a>
          {{end}}
        </div>
      </article>
      {{else if eq .Kind 10003}}
      <article class="note bookmarks">
        <div class="bookmarks-header">
          <span class="bookmarks-icon">🔖</span>
          <span class="bookmarks-title">Bookmarks</span>
          <span class="bookmarks-count">{{.BookmarkCount}} items</span>
        </div>
        {{if .BookmarkEventIDs}}
        <div class="bookmarks-section">
          <div class="bookmarks-section-title">Events</div>
          <div class="bookmarks-list">
            {{range .BookmarkEventIDs}}
            <a href="/html/event/{{.}}" class="bookmark-item">
              <span class="bookmark-item-icon">📝</span>
              <span class="bookmark-item-text">{{slice . 0 12}}...</span>
            </a>
            {{end}}
          </div>
        </div>
        {{end}}
        {{if .BookmarkArticleRefs}}
        <div class="bookmarks-section">
          <div class="bookmarks-section-title">Articles</div>
          <div class="bookmarks-list">
            {{range .BookmarkArticleRefs}}
            <div class="bookmark-item">
              <span class="bookmark-item-icon">📄</span>
              <span class="bookmark-item-text">{{.}}</span>
            </div>
            {{end}}
          </div>
        </div>
        {{end}}
        {{if .BookmarkHashtags}}
        <div class="bookmarks-section">
          <div class="bookmarks-section-title">Hashtags</div>
          <div class="bookmarks-list">
            {{range .BookmarkHashtags}}
            <span class="bookmark-item bookmark-hashtag">
              <span class="bookmark-item-icon">#</span>
              <span class="bookmark-item-text">{{.}}</span>
            </span>
            {{end}}
          </div>
        </div>
        {{end}}
        {{if .BookmarkURLs}}
        <div class="bookmarks-section">
          <div class="bookmarks-section-title">Links</div>
          <div class="bookmarks-list">
            {{range .BookmarkURLs}}
            <a href="{{.}}" class="bookmark-item" target="_blank" rel="noopener">
              <span class="bookmark-item-icon">🔗</span>
              <span class="bookmark-item-text">{{.}}</span>
            </a>
            {{end}}
          </div>
        </div>
        {{end}}
        <div class="bookmarks-meta">
          <div class="bookmarks-author">
            <a href="/html/profile/{{.Npub}}">
              {{if and .AuthorProfile .AuthorProfile.Picture}}
              <img class="bookmarks-author-avatar" src="{{.AuthorProfile.Picture}}" alt="{{if .AuthorProfile.DisplayName}}{{.AuthorProfile.DisplayName}}{{else if .AuthorProfile.Name}}{{.AuthorProfile.Name}}{{else}}User{{end}}'s avatar">
              {{else}}
              <img class="bookmarks-author-avatar" src="/static/avatar.jpg" alt="Default avatar">
              {{end}}
            </a>
            <a href="/html/profile/{{.Npub}}" class="bookmarks-author-name">
              {{if .AuthorProfile}}
                {{if .AuthorProfile.DisplayName}}{{.AuthorProfile.DisplayName}}{{else if .AuthorProfile.Name}}{{.AuthorProfile.Name}}{{else}}{{.NpubShort}}{{end}}
              {{else}}{{.NpubShort}}{{end}}
            </a>
          </div>
          <span class="bookmarks-time">{{formatTime .CreatedAt}}</span>
        </div>
      </article>
      {{else if eq .Kind 9802}}
      <article class="note highlight">
        <blockquote class="highlight-blockquote">
          {{.Content}}
          {{if .HighlightContext}}
          <div class="highlight-context">{{.HighlightContext}}</div>
          {{end}}
        </blockquote>
        {{if .HighlightComment}}
        <div class="highlight-comment">{{.HighlightComment}}</div>
        {{end}}
        {{if .HighlightSourceURL}}
        <div class="highlight-source">
          <a href="{{.HighlightSourceURL}}" class="highlight-source-link" target="_blank" rel="noopener">{{.HighlightSourceURL}}</a>
        </div>
        {{end}}
        <div class="highlight-meta">
          <div class="highlight-author">
            <a href="/html/profile/{{.Npub}}">
              {{if and .AuthorProfile .AuthorProfile.Picture}}
              <img class="highlight-author-avatar" src="{{.AuthorProfile.Picture}}" alt="{{if .AuthorProfile.DisplayName}}{{.AuthorProfile.DisplayName}}{{else if .AuthorProfile.Name}}{{.AuthorProfile.Name}}{{else}}User{{end}}'s avatar">
              {{else}}
              <img class="highlight-author-avatar" src="/static/avatar.jpg" alt="Default avatar">
              {{end}}
            </a>
            <a href="/html/profile/{{.Npub}}" class="highlight-author-name">
              {{if .AuthorProfile}}
                {{if .AuthorProfile.DisplayName}}{{.AuthorProfile.DisplayName}}{{else if .AuthorProfile.Name}}{{.AuthorProfile.Name}}{{else}}{{.NpubShort}}{{end}}
              {{else}}{{.NpubShort}}{{end}}
            </a>
          </div>
          <span class="highlight-time">{{formatTime .CreatedAt}}</span>
        </div>
      </article>
      {{else}}
      <article class="note">
        <div class="note-author">
          <a href="/html/profile/{{.Npub}}" class="text-muted">
          {{if and .AuthorProfile .AuthorProfile.Picture}}
          <img class="author-avatar" src="{{.AuthorProfile.Picture}}" alt="{{if .AuthorProfile.DisplayName}}{{.AuthorProfile.DisplayName}}{{else if .AuthorProfile.Name}}{{.AuthorProfile.Name}}{{else}}User{{end}}'s avatar">
          {{else}}
          <img class="author-avatar" src="/static/avatar.jpg" alt="Default avatar">
          {{end}}
          </a>
          <div class="author-info">
            <a href="/html/profile/{{.Npub}}" class="text-muted">
            {{if .AuthorProfile}}
            {{if or .AuthorProfile.DisplayName .AuthorProfile.Name}}
            <span class="author-name">{{if .AuthorProfile.DisplayName}}{{.AuthorProfile.DisplayName}}{{else}}{{.AuthorProfile.Name}}{{end}}</span>
            {{if .AuthorProfile.Nip05}}<span class="author-nip05">{{.AuthorProfile.Nip05}}</span>{{end}}
            {{else if .AuthorProfile.Nip05}}
            <span class="author-nip05">{{.AuthorProfile.Nip05}}</span>
            {{else}}
            <span class="pubkey" title="{{.Pubkey}}">{{.NpubShort}}</span>
            {{end}}
            {{else}}
            <span class="pubkey" title="{{.Pubkey}}">{{.NpubShort}}</span>
            {{end}}
            </a>
            <span class="author-time">{{formatTime .CreatedAt}}</span>
          </div>
        </div>
        {{if eq .Kind 6}}
        {{if .RepostedEvent}}
        <div class="repost-indicator">reposted</div>
        <div class="reposted-note">
          <div class="note-author">
            <span class="text-muted">
            {{if and .RepostedEvent.AuthorProfile .RepostedEvent.AuthorProfile.Picture}}
            <img class="author-avatar" src="{{.RepostedEvent.AuthorProfile.Picture}}" alt="{{if .RepostedEvent.AuthorProfile.DisplayName}}{{.RepostedEvent.AuthorProfile.DisplayName}}{{else if .RepostedEvent.AuthorProfile.Name}}{{.RepostedEvent.AuthorProfile.Name}}{{else}}User{{end}}'s avatar">
            {{else}}
            <img class="author-avatar" src="/static/avatar.jpg" alt="Default avatar">
            {{end}}
            </span>
            <div class="author-info">
              <span class="text-muted">
              {{if .RepostedEvent.AuthorProfile}}
              {{if or .RepostedEvent.AuthorProfile.DisplayName .RepostedEvent.AuthorProfile.Name}}
              <span class="author-name">{{if .RepostedEvent.AuthorProfile.DisplayName}}{{.RepostedEvent.AuthorProfile.DisplayName}}{{else}}{{.RepostedEvent.AuthorProfile.Name}}{{end}}</span>
              {{if .RepostedEvent.AuthorProfile.Nip05}}<span class="author-nip05">{{.RepostedEvent.AuthorProfile.Nip05}}</span>{{end}}
              {{else if .RepostedEvent.AuthorProfile.Nip05}}
              <span class="author-nip05">{{.RepostedEvent.AuthorProfile.Nip05}}</span>
              {{else}}
              <span class="pubkey" title="{{.RepostedEvent.Pubkey}}">{{.RepostedEvent.NpubShort}}</span>
              {{end}}
              {{else}}
              <span class="pubkey" title="{{.RepostedEvent.Pubkey}}">{{.RepostedEvent.NpubShort}}</span>
              {{end}}
              </span>
            </div>
          </div>
          {{if eq .RepostedEvent.Kind 20}}
          <div class="picture-note">
            {{if .RepostedEvent.Title}}<div class="picture-title">{{.RepostedEvent.Title}}</div>{{end}}
            <div class="picture-gallery">{{.RepostedEvent.ImagesHTML}}</div>
            {{if .RepostedEvent.Content}}<div class="picture-caption">{{.RepostedEvent.ContentHTML}}</div>{{end}}
          </div>
          {{else}}
          <div class="note-content">{{.RepostedEvent.ContentHTML}}</div>
          {{end}}
          <a href="/html/thread/{{.RepostedEvent.ID}}" class="view-note-link">View note &rarr;</a>
        </div>
        {{else}}
        <div class="note-content repost-empty">Reposted note not available</div>
        {{end}}
        {{else if eq .Kind 20}}
        <div class="picture-note">
          {{if .Title}}<div class="picture-title">{{.Title}}</div>{{end}}
          <div class="picture-gallery">{{.ImagesHTML}}</div>
          {{if .Content}}<div class="picture-caption">{{.ContentHTML}}</div>{{end}}
        </div>
        {{else if eq .Kind 30023}}
        <div class="article-preview">
          {{if .HeaderImage}}<img src="{{.HeaderImage}}" alt="" class="article-preview-image">{{end}}
          {{if .Title}}<h3 class="article-preview-title">{{.Title}}</h3>{{end}}
          {{if .Summary}}<p class="article-preview-summary">{{.Summary}}</p>{{end}}
        </div>
        {{else}}
        <div class="note-content">{{.ContentHTML}}</div>
        {{if .QuotedEvent}}
        <div class="quoted-note">
          <div class="quoted-author">
            {{if and .QuotedEvent.AuthorProfile .QuotedEvent.AuthorProfile.Picture}}
            <img src="{{.QuotedEvent.AuthorProfile.Picture}}" alt="{{if .QuotedEvent.AuthorProfile.DisplayName}}{{.QuotedEvent.AuthorProfile.DisplayName}}{{else if .QuotedEvent.AuthorProfile.Name}}{{.QuotedEvent.AuthorProfile.Name}}{{else}}User{{end}}'s avatar">
            {{else}}
            <img src="/static/avatar.jpg" alt="Default avatar">
            {{end}}
            <span class="quoted-author-name">
              {{if .QuotedEvent.AuthorProfile}}
              {{if or .QuotedEvent.AuthorProfile.DisplayName .QuotedEvent.AuthorProfile.Name}}
              {{if .QuotedEvent.AuthorProfile.DisplayName}}{{.QuotedEvent.AuthorProfile.DisplayName}}{{else}}{{.QuotedEvent.AuthorProfile.Name}}{{end}}
              {{else}}
              {{.QuotedEvent.NpubShort}}
              {{end}}
              {{else}}
              {{.QuotedEvent.NpubShort}}
              {{end}}
            </span>
          </div>
          {{if eq .QuotedEvent.Kind 30023}}
          <div class="quoted-article-title">{{if .QuotedEvent.Title}}{{.QuotedEvent.Title}}{{else}}Untitled Article{{end}}</div>
          {{if .QuotedEvent.Summary}}<div class="quoted-article-summary">{{.QuotedEvent.Summary}}</div>{{end}}
          <a href="/html/thread/{{.QuotedEvent.ID}}" class="view-note-link">Read article &rarr;</a>
          {{else}}
          <div class="note-content">{{.QuotedEvent.ContentHTML}}</div>
          <a href="/html/thread/{{.QuotedEvent.ID}}" class="view-note-link">View quoted note &rarr;</a>
          {{end}}
        </div>
        {{else if .QuotedEventID}}
        <div class="quoted-note quoted-note-fallback">
          <a href="/html/thread/{{.QuotedEventID}}" class="view-note-link">View quoted note &rarr;</a>
        </div>
        {{end}}
        {{end}}
        <div class="note-footer">
          <div class="note-footer-actions">
          {{if $.LoggedIn}}
            {{if eq .Kind 6}}
            {{/* For reposts, actions target the reposted note */}}
            {{if .RepostedEvent}}
            <a href="/html/thread/{{.RepostedEvent.ID}}" class="text-link">Reply{{if gt .RepostedEvent.ReplyCount 0}} {{.RepostedEvent.ReplyCount}}{{end}}</a>
            <form method="POST" action="/html/repost" class="inline-form">
              <input type="hidden" name="csrf_token" value="{{$.CSRFToken}}">
              <input type="hidden" name="event_id" value="{{.RepostedEvent.ID}}">
              <input type="hidden" name="event_pubkey" value="{{.RepostedEvent.Pubkey}}">
              <input type="hidden" name="return_url" value="{{$.CurrentURL}}">
              <button type="submit" class="text-link">Repost</button>
            </form>
            <a href="/html/quote/{{.RepostedEvent.ID}}" class="text-link">Quote</a>
            <form method="POST" action="/html/react" class="inline-form">
              <input type="hidden" name="csrf_token" value="{{$.CSRFToken}}">
              <input type="hidden" name="event_id" value="{{.RepostedEvent.ID}}">
              <input type="hidden" name="event_pubkey" value="{{.RepostedEvent.Pubkey}}">
              <input type="hidden" name="return_url" value="{{$.CurrentURL}}">
              <input type="hidden" name="reaction" value="❤️">
              <button type="submit" class="text-link">Like</button>
            </form>
            <form method="POST" action="/html/bookmark" class="inline-form">
              <input type="hidden" name="csrf_token" value="{{$.CSRFToken}}">
              <input type="hidden" name="event_id" value="{{.RepostedEvent.ID}}">
              <input type="hidden" name="return_url" value="{{$.CurrentURL}}">
              {{if .RepostedEvent.IsBookmarked}}
              <input type="hidden" name="action" value="remove">
              <button type="submit" class="text-link" title="Remove bookmark">Unbookmark</button>
              {{else}}
              <input type="hidden" name="action" value="add">
              <button type="submit" class="text-link" title="Add bookmark">Bookmark</button>
              {{end}}
            </form>
            {{end}}
            {{else if ne .Kind 30023}}
            <a href="/html/thread/{{.ID}}" class="text-link">Reply{{if gt .ReplyCount 0}} {{.ReplyCount}}{{end}}</a>
            <form method="POST" action="/html/repost" class="inline-form">
              <input type="hidden" name="csrf_token" value="{{$.CSRFToken}}">
              <input type="hidden" name="event_id" value="{{.ID}}">
              <input type="hidden" name="event_pubkey" value="{{.Pubkey}}">
              <input type="hidden" name="return_url" value="{{$.CurrentURL}}">
              <button type="submit" class="text-link">Repost</button>
            </form>
            <a href="/html/quote/{{.ID}}" class="text-link">Quote</a>
            <form method="POST" action="/html/react" class="inline-form">
              <input type="hidden" name="csrf_token" value="{{$.CSRFToken}}">
              <input type="hidden" name="event_id" value="{{$item.ID}}">
              <input type="hidden" name="event_pubkey" value="{{$item.Pubkey}}">
              <input type="hidden" name="return_url" value="{{$.CurrentURL}}">
              <input type="hidden" name="reaction" value="❤️">
              <button type="submit" class="text-link">Like</button>
            </form>
            <form method="POST" action="/html/bookmark" class="inline-form">
              <input type="hidden" name="csrf_token" value="{{$.CSRFToken}}">
              <input type="hidden" name="event_id" value="{{$item.ID}}">
              <input type="hidden" name="return_url" value="{{$.CurrentURL}}">
              {{if .IsBookmarked}}
              <input type="hidden" name="action" value="remove">
              <button type="submit" class="text-link" title="Remove bookmark">Unbookmark</button>
              {{else}}
              <input type="hidden" name="action" value="add">
              <button type="submit" class="text-link" title="Add bookmark">Bookmark</button>
              {{end}}
            </form>
            {{else}}
            <a href="/html/thread/{{.ID}}" class="text-link">Read article</a>
            {{end}}
          {{else}}
            {{if eq .Kind 30023}}
            <a href="/html/thread/{{.ID}}" class="text-link">Read article</a>
            {{else if eq .Kind 6}}
            {{if and .RepostedEvent (gt .RepostedEvent.ReplyCount 0)}}
            <a href="/html/thread/{{.RepostedEvent.ID}}" class="text-link">{{.RepostedEvent.ReplyCount}} replies</a>
            {{end}}
            {{else if gt .ReplyCount 0}}
            <a href="/html/thread/{{.ID}}" class="text-link">{{.ReplyCount}} replies</a>
            {{end}}
          {{end}}
          </div>
          {{if or (and .Reactions (gt .Reactions.Total 0)) (and (not $.LoggedIn) (gt .ReplyCount 0))}}
          <div class="note-footer-reactions">
            {{if and .Reactions (gt .Reactions.Total 0)}}
            {{range $type, $count := .Reactions.ByType}}
            <span class="reaction-badge">{{$type}} {{$count}}</span>
            {{end}}
            {{end}}
          </div>
          {{end}}
        </div>
        {{if and .Links (ne .Kind 6)}}
        <div class="links">
          {{range .Links}}
          {{if not (or (contains . "self") (contains . "next") (contains . "prev"))}}
          <a href="{{.}}" class="link">{{linkName .}} →</a>
          {{end}}
          {{end}}
        </div>
        {{end}}
      </article>
      {{end}}{{/* end if eq .Kind 9735 else */}}
      {{else}}
      <div class="empty-state">
        <div class="empty-state-icon">📭</div>
        <p>No notes found</p>
        <p class="empty-state-hint">Try adjusting your filters or check back later.</p>
      </div>
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
      <p>{{if .Meta}}Generated: {{.Meta.GeneratedAt.Format "15:04:05"}} · {{end}}Zero-JS Hypermedia Browser</p>
    </footer>
  </div>
  <a href="#top" class="scroll-top" aria-label="Scroll to top">↑</a>
</body>
</html>
`

type HTMLPageData struct {
	Title                  string
	Meta                   *MetaInfo
	Items                  []HTMLEventItem
	Pagination             *HTMLPagination
	Actions                []HTMLAction
	Links                  []string
	LoggedIn               bool
	UserPubKey             string
	UserDisplayName        string   // Display name from profile (falls back to @npubShort)
	Error                  string
	Success                string
	ShowReactions          bool     // Whether reactions are being fetched (slow mode)
	FeedMode               string   // "follows" or "global"
	KindFilter             string   // Current kind filter: "all", "notes", "photos", "reads", "streams"
	ActiveRelays           []string // Relays being used for this request
	CurrentURL             string   // Current page URL for reaction redirects
	ThemeClass             string   // "dark", "light", or "" for system default
	ThemeLabel             string   // Label for theme toggle button
	CSRFToken              string   // CSRF token for form submission
	HasUnreadNotifications bool     // Whether there are notifications newer than last seen
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
	ImagesHTML    template.HTML // Pre-rendered images from imeta tags (kind 20)
	Title         string        // Title from title tag (kind 20, 30023)
	Summary       string        // Summary from summary tag (kind 30023)
	HeaderImage   string        // Header image URL from image tag (kind 30023)
	PublishedAt   int64         // Published timestamp from published_at tag (kind 30023)
	RelaysSeen    []string
	Links         []string
	AuthorProfile *ProfileInfo
	Reactions     *ReactionsSummary
	ReplyCount    int
	ParentID      string         // ID of parent event if this is a reply
	RepostedEvent  *HTMLEventItem // For kind 6 reposts: the embedded original event
	QuotedEvent    *HTMLEventItem // For quote posts: the quoted note (from q tag)
	QuotedEventID  string         // Event ID from q tag (used to fetch quoted event)
	// Kind 9735 zap receipt fields
	ZapSenderPubkey    string       // Pubkey of who sent the zap
	ZapSenderNpub      string       // Npub of sender
	ZapSenderNpubShort string       // Short npub of sender
	ZapSenderProfile   *ProfileInfo // Profile of sender
	ZapRecipientPubkey string       // Pubkey of who received the zap
	ZapRecipientNpub   string       // Npub of recipient
	ZapRecipientNpubShort string    // Short npub of recipient
	ZapRecipientProfile *ProfileInfo // Profile of recipient
	ZapAmountSats      int64        // Amount in sats
	ZapComment         string       // Optional zap comment
	ZappedEventID      string       // Event ID that was zapped (if any)
	// Kind 30311 live event fields
	LiveTitle         string              // Event title
	LiveSummary       string              // Event summary/description
	LiveImage         string              // Preview image URL
	LiveStatus        string              // "planned", "live", or "ended"
	LiveStreamingURL  string              // Streaming URL
	LiveRecordingURL  string              // Recording URL (after event ends)
	LiveStarts        int64               // Start timestamp
	LiveEnds          int64               // End timestamp
	LiveParticipants  []LiveParticipant   // List of participants with roles
	LiveCurrentCount  int                 // Current participant count
	LiveTotalCount    int                 // Total participant count
	LiveHashtags      []string            // Hashtags for the event
	LiveDTag          string              // d-tag identifier for addressable events
	LiveEmbedURL      string              // Embed URL for iframe (e.g., zap.stream)
	// Kind 9802 highlight fields
	HighlightContext    string        // Surrounding context text
	HighlightComment    string        // User's comment on the highlight
	HighlightSourceURL  string        // Source URL (from r tag)
	HighlightSourceRef  string        // Nostr reference (from a tag) - naddr or nevent
	// Kind 10003 bookmark list fields
	BookmarkEventIDs    []string      // Bookmarked event IDs (from e tags)
	BookmarkArticleRefs []string      // Bookmarked article references (from a tags)
	BookmarkHashtags    []string      // Bookmarked hashtags (from t tags)
	BookmarkURLs        []string      // Bookmarked URLs (from r tags)
	BookmarkCount       int           // Total bookmark count
	// Bookmark state for current user
	IsBookmarked        bool          // Whether logged-in user has bookmarked this item
}

// LiveParticipant represents a participant in a live event
type LiveParticipant struct {
	Pubkey    string
	Npub      string
	NpubShort string
	Role      string       // Host, Speaker, Participant, etc.
	Profile   *ProfileInfo
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

// Regex to collapse multiple newlines before media URLs (images, videos, audio, youtube)
var mediaURLRegex = regexp.MustCompile(`(?i)(\n\s*)+\n(https?://[^\s<>"]+\.(jpg|jpeg|png|gif|webp|mp4|webm|mov|m4v|mp3|wav|ogg|flac|m4a|aac)(\?[^\s<>"]*)?|https?://(?:youtube\.com/watch\?v=|youtu\.be/|youtube\.com/shorts/)[a-zA-Z0-9_-]{11})`)

// consecutiveImgRegex matches 2+ consecutive <img> tags (with optional whitespace between)
var consecutiveImgRegex = regexp.MustCompile(`(<img [^>]+>)(\s*<img [^>]+>)+`)

// wrapConsecutiveImages wraps groups of 2+ consecutive images in a gallery div
func wrapConsecutiveImages(html string) string {
	return consecutiveImgRegex.ReplaceAllStringFunc(html, func(match string) string {
		return `<div class="image-gallery">` + match + `</div>`
	})
}

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

// ImetaImage represents a parsed image from an imeta tag (NIP-68)
type ImetaImage struct {
	URL      string
	MimeType string
	Alt      string
	Dim      string // e.g., "1920x1080"
	Blurhash string
}

// parseImetaTag parses an imeta tag into an ImetaImage struct
// imeta format: ["imeta", "url https://...", "m image/jpeg", "dim 1920x1080", "alt description", "blurhash LEHV6n..."]
func parseImetaTag(tag []string) *ImetaImage {
	if len(tag) < 2 || tag[0] != "imeta" {
		return nil
	}

	img := &ImetaImage{}
	for _, field := range tag[1:] {
		// Each field is "key value" format
		parts := strings.SplitN(field, " ", 2)
		if len(parts) != 2 {
			continue
		}
		key, value := parts[0], parts[1]
		switch key {
		case "url":
			img.URL = value
		case "m":
			img.MimeType = value
		case "alt":
			img.Alt = value
		case "dim":
			img.Dim = value
		case "blurhash":
			img.Blurhash = value
		}
	}

	// URL is required
	if img.URL == "" {
		return nil
	}
	return img
}

// extractImetaImages extracts all imeta tags from event tags and renders them as HTML
func extractImetaImages(tags [][]string) template.HTML {
	var images []*ImetaImage
	for _, tag := range tags {
		if img := parseImetaTag(tag); img != nil {
			images = append(images, img)
		}
	}

	if len(images) == 0 {
		return ""
	}

	var sb strings.Builder
	for _, img := range images {
		alt := img.Alt
		if alt == "" {
			alt = "image"
		}
		sb.WriteString(`<img src="`)
		sb.WriteString(html.EscapeString(img.URL))
		sb.WriteString(`" alt="`)
		sb.WriteString(html.EscapeString(alt))
		sb.WriteString(`" loading="lazy" class="picture-image">`)
	}

	return template.HTML(sb.String())
}

// extractTitle extracts the title tag value from event tags
func extractTitle(tags [][]string) string {
	for _, tag := range tags {
		if len(tag) >= 2 && tag[0] == "title" {
			return tag[1]
		}
	}
	return ""
}

// extractSummary extracts the summary tag value from event tags (kind 30023)
func extractSummary(tags [][]string) string {
	for _, tag := range tags {
		if len(tag) >= 2 && tag[0] == "summary" {
			return tag[1]
		}
	}
	return ""
}

// extractHeaderImage extracts the image tag value from event tags (kind 30023)
func extractHeaderImage(tags [][]string) string {
	for _, tag := range tags {
		if len(tag) >= 2 && tag[0] == "image" {
			return tag[1]
		}
	}
	return ""
}

// extractPublishedAt extracts the published_at tag value from event tags (kind 30023)
func extractPublishedAt(tags [][]string) int64 {
	for _, tag := range tags {
		if len(tag) >= 2 && tag[0] == "published_at" {
			if ts, err := strconv.ParseInt(tag[1], 10, 64); err == nil {
				return ts
			}
		}
	}
	return 0
}

// extractDTag extracts the d tag value from event tags (for addressable events)
func extractDTag(tags [][]string) string {
	for _, tag := range tags {
		if len(tag) >= 2 && tag[0] == "d" {
			return tag[1]
		}
	}
	return ""
}

// renderMarkdown converts markdown content to HTML using goldmark
func renderMarkdown(content string) template.HTML {
	var buf bytes.Buffer
	if err := goldmark.Convert([]byte(content), &buf); err != nil {
		// Fallback to escaped plain text if markdown parsing fails
		return template.HTML(html.EscapeString(content))
	}
	return template.HTML(buf.String())
}

// parseRepostedEvent parses the embedded event JSON from a kind 6 repost's content field
func parseRepostedEvent(content string, relays []string, resolvedRefs map[string]string, linkPreviews map[string]*LinkPreview, profiles map[string]*ProfileInfo) *HTMLEventItem {
	// The content of a kind 6 repost is the stringified JSON of the original event
	var embeddedEvent struct {
		ID        string     `json:"id"`
		PubKey    string     `json:"pubkey"`
		CreatedAt int64      `json:"created_at"`
		Kind      int        `json:"kind"`
		Tags      [][]string `json:"tags"`
		Content   string     `json:"content"`
		Sig       string     `json:"sig"`
	}

	if err := json.Unmarshal([]byte(content), &embeddedEvent); err != nil {
		log.Printf("Failed to parse reposted event JSON: %v", err)
		return nil
	}

	// Generate npub from hex pubkey
	npub, _ := encodeBech32Pubkey(embeddedEvent.PubKey)

	reposted := &HTMLEventItem{
		ID:            embeddedEvent.ID,
		Kind:          embeddedEvent.Kind,
		Pubkey:        embeddedEvent.PubKey,
		Npub:          npub,
		NpubShort:     formatNpubShort(npub),
		CreatedAt:     embeddedEvent.CreatedAt,
		Content:       embeddedEvent.Content,
		ContentHTML:   processContentToHTMLFull(embeddedEvent.Content, relays, resolvedRefs, linkPreviews),
		AuthorProfile: profiles[embeddedEvent.PubKey],
	}

	// Handle kind 20 (picture notes) within reposts
	if embeddedEvent.Kind == 20 {
		reposted.ImagesHTML = extractImetaImages(embeddedEvent.Tags)
		reposted.Title = extractTitle(embeddedEvent.Tags)
	}

	return reposted
}

// ZapInfo holds parsed information from a kind 9735 zap receipt
type ZapInfo struct {
	SenderPubkey    string
	RecipientPubkey string
	AmountMsats     int64
	Comment         string
	ZappedEventID   string
}

// parseZapReceipt extracts zap information from a kind 9735 event's tags
func parseZapReceipt(tags [][]string) *ZapInfo {
	info := &ZapInfo{}

	var descriptionJSON string

	for _, tag := range tags {
		if len(tag) < 2 {
			continue
		}
		switch tag[0] {
		case "p":
			info.RecipientPubkey = tag[1]
		case "P":
			info.SenderPubkey = tag[1]
		case "e":
			info.ZappedEventID = tag[1]
		case "description":
			descriptionJSON = tag[1]
		}
	}

	// Parse the description (zap request) to get sender and amount
	if descriptionJSON != "" {
		var zapRequest struct {
			PubKey  string     `json:"pubkey"`
			Content string     `json:"content"`
			Tags    [][]string `json:"tags"`
		}
		if err := json.Unmarshal([]byte(descriptionJSON), &zapRequest); err == nil {
			// Sender is the author of the zap request
			if info.SenderPubkey == "" {
				info.SenderPubkey = zapRequest.PubKey
			}
			// Comment is the content of the zap request
			info.Comment = zapRequest.Content
			// Look for amount tag in zap request
			for _, tag := range zapRequest.Tags {
				if len(tag) >= 2 && tag[0] == "amount" {
					if msats, err := strconv.ParseInt(tag[1], 10, 64); err == nil {
						info.AmountMsats = msats
					}
				}
			}
		}
	}

	return info
}

// LiveEventInfo holds parsed information from a kind 30311 live event
type LiveEventInfo struct {
	DTag             string // d-tag identifier for addressable events
	Title            string
	Summary          string
	Image            string
	Status           string // "planned", "live", "ended"
	StreamingURL     string
	RecordingURL     string
	Starts           int64
	Ends             int64
	CurrentCount     int
	TotalCount       int
	Hashtags         []string
	ParticipantPubkeys []string // Pubkeys of participants
	ParticipantRoles   map[string]string // Pubkey -> Role mapping
}

// parseLiveEvent extracts live event information from a kind 30311 event's tags
func parseLiveEvent(tags [][]string) *LiveEventInfo {
	info := &LiveEventInfo{
		ParticipantRoles: make(map[string]string),
	}

	for _, tag := range tags {
		if len(tag) < 2 {
			continue
		}
		switch tag[0] {
		case "d":
			info.DTag = tag[1]
		case "title":
			info.Title = tag[1]
		case "summary":
			info.Summary = tag[1]
		case "image":
			info.Image = tag[1]
		case "status":
			info.Status = tag[1]
		case "streaming":
			info.StreamingURL = tag[1]
		case "recording":
			info.RecordingURL = tag[1]
		case "starts":
			if ts, err := strconv.ParseInt(tag[1], 10, 64); err == nil {
				info.Starts = ts
			}
		case "ends":
			if ts, err := strconv.ParseInt(tag[1], 10, 64); err == nil {
				info.Ends = ts
			}
		case "current_participants":
			if count, err := strconv.Atoi(tag[1]); err == nil {
				info.CurrentCount = count
			}
		case "total_participants":
			if count, err := strconv.Atoi(tag[1]); err == nil {
				info.TotalCount = count
			}
		case "t":
			info.Hashtags = append(info.Hashtags, tag[1])
		case "p":
			// p tag format: ["p", pubkey, relay, role, proof]
			pubkey := tag[1]
			info.ParticipantPubkeys = append(info.ParticipantPubkeys, pubkey)
			if len(tag) >= 4 && tag[3] != "" {
				info.ParticipantRoles[pubkey] = tag[3]
			}
		}
	}

	return info
}

// HighlightInfo holds parsed data from a kind 9802 highlight event
type HighlightInfo struct {
	Context    string // Surrounding text context
	Comment    string // User's commentary on the highlight
	SourceURL  string // Source URL (from r tag)
	SourceRef  string // Nostr reference (from a tag) - naddr or nevent
}

// parseHighlight extracts highlight information from a kind 9802 event's tags
func parseHighlight(tags [][]string) *HighlightInfo {
	info := &HighlightInfo{}

	for _, tag := range tags {
		if len(tag) < 2 {
			continue
		}
		switch tag[0] {
		case "context":
			info.Context = tag[1]
		case "comment":
			info.Comment = tag[1]
		case "r":
			// Source URL - only take the first one if multiple
			if info.SourceURL == "" {
				info.SourceURL = tag[1]
			}
		case "a":
			// Nostr article/event reference (naddr format)
			if info.SourceRef == "" {
				info.SourceRef = tag[1]
			}
		}
	}

	return info
}

// BookmarkInfo holds parsed data from a kind 10003 bookmark list event
type BookmarkInfo struct {
	EventIDs    []string // Bookmarked event IDs (from e tags)
	ArticleRefs []string // Bookmarked article references (from a tags)
	Hashtags    []string // Bookmarked hashtags (from t tags)
	URLs        []string // Bookmarked URLs (from r tags)
}

// parseBookmarks extracts bookmark information from a kind 10003 event's tags
func parseBookmarks(tags [][]string) *BookmarkInfo {
	info := &BookmarkInfo{
		EventIDs:    []string{},
		ArticleRefs: []string{},
		Hashtags:    []string{},
		URLs:        []string{},
	}

	for _, tag := range tags {
		if len(tag) < 2 {
			continue
		}
		switch tag[0] {
		case "e":
			info.EventIDs = append(info.EventIDs, tag[1])
		case "a":
			info.ArticleRefs = append(info.ArticleRefs, tag[1])
		case "t":
			info.Hashtags = append(info.Hashtags, tag[1])
		case "r":
			info.URLs = append(info.URLs, tag[1])
		}
	}

	return info
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
	// Trim leading/trailing whitespace
	content = strings.TrimSpace(content)

	// Collapse multiple newlines before media URLs to just a single newline
	content = mediaURLRegex.ReplaceAllString(content, "\n$2")

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

	// Wrap consecutive images in a gallery div for better layout
	result = wrapConsecutiveImages(result)

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
	// Last fallback: truncate pubkey (with bounds check)
	if len(pubkey) >= 12 {
		return "@" + pubkey[:12] + "..."
	}
	return "@" + pubkey
}

// stripQuotedNostrRef removes nostr:nevent1... or nostr:note1... references that point to quotedEventID
// This is used to strip the reference from content when we're rendering an inline preview
func stripQuotedNostrRef(content string, quotedEventID string) string {
	// Match nostr:nevent1... or nostr:note1... patterns
	nostrRefPattern := regexp.MustCompile(`nostr:(nevent1[a-z0-9]+|note1[a-z0-9]+)`)
	return nostrRefPattern.ReplaceAllStringFunc(content, func(match string) string {
		identifier := strings.TrimPrefix(match, "nostr:")
		var eventID string
		if strings.HasPrefix(identifier, "nevent1") {
			if ne, err := DecodeNEvent(identifier); err == nil {
				eventID = ne.EventID
			}
		} else if strings.HasPrefix(identifier, "note1") {
			if id, err := DecodeNote(identifier); err == nil {
				eventID = id
			}
		}
		// If this reference points to the quoted event, remove it
		if eventID == quotedEventID {
			return ""
		}
		// Keep other references
		return match
	})
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

	// Format timestamp
	timestamp := time.Unix(event.CreatedAt, 0).Format("2006-01-02 15:04")

	// Handle kind 30023 (longform articles) differently - show card-style preview
	if event.Kind == 30023 {
		title := extractTitle(event.Tags)
		if title == "" {
			title = "Untitled Article"
		}
		summary := extractSummary(event.Tags)
		if summary == "" {
			// Extract first ~200 chars of content as summary
			content := event.Content
			if len(content) > 200 {
				content = content[:200] + "..."
			}
			// Strip markdown syntax for cleaner preview
			summary = content
		}

		// Get d-tag for naddr link
		dTag := extractDTag(event.Tags)
		var linkURL string
		if dTag != "" {
			naddr, err := EncodeNAddr(30023, event.PubKey, dTag)
			if err == nil {
				linkURL = "/html/thread/" + naddr
			}
		}
		if linkURL == "" {
			linkURL = "/html/thread/" + event.ID
		}

		return fmt.Sprintf(`<div class="quoted-note quoted-article">%s<div class="quoted-article-title">%s</div><div class="quoted-article-summary">%s</div><div class="quoted-meta"><span>%s</span> · <a href="%s">Read article →</a></div></div>`,
			authorHTML,
			html.EscapeString(title),
			html.EscapeString(summary),
			html.EscapeString(timestamp),
			html.EscapeString(linkURL))
	}

	// Regular notes - show truncated content
	content := event.Content
	if len(content) > 500 {
		content = content[:500] + "..."
	}

	// Process content but don't recurse into nested nostr: refs (pass nil relays)
	contentHTML := processContentToHTMLWithRelays(content, nil)

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

// computeKindFilter determines the active kind filter from the kinds parameter
// Returns: "all", "notes", "photos", "reads", "bookmarks", "highlights", or "livestreams"
func computeKindFilter(kinds []int) string {
	if len(kinds) == 0 {
		return "all"
	}
	// Check for specific filter patterns
	if len(kinds) == 2 && ((kinds[0] == 1 && kinds[1] == 6) || (kinds[0] == 6 && kinds[1] == 1)) {
		return "notes"
	}
	if len(kinds) == 1 {
		switch kinds[0] {
		case 1:
			return "notes" // Also match single kind=1
		case 20:
			return "photos"
		case 30023:
			return "reads"
		case 10003:
			return "bookmarks"
		case 9802:
			return "highlights"
		case 30311:
			return "livestreams"
		}
	}
	return "all" // Unknown filter pattern, default to all
}

func renderHTML(resp TimelineResponse, relays []string, authors []string, kinds []int, limit int, session *BunkerSession, errorMsg, successMsg string, showReactions bool, feedMode string, currentURL string, themeClass, themeLabel string, csrfToken string, hasUnreadNotifs bool) (string, error) {
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

	// Pre-fetch profiles for live event participants from purplepag.es
	liveParticipantPubkeys := make(map[string]bool)
	for _, item := range resp.Items {
		if item.Kind == 30311 {
			liveInfo := parseLiveEvent(item.Tags)
			if liveInfo != nil {
				for _, pk := range liveInfo.ParticipantPubkeys {
					liveParticipantPubkeys[pk] = true
				}
			}
		}
	}
	var liveParticipantProfiles map[string]*ProfileInfo
	if len(liveParticipantPubkeys) > 0 {
		pubkeys := make([]string, 0, len(liveParticipantPubkeys))
		for pk := range liveParticipantPubkeys {
			pubkeys = append(pubkeys, pk)
		}
		// Fetch from purplepag.es for better profile coverage
		liveParticipantProfiles = fetchProfiles([]string{"wss://purplepag.es"}, pubkeys)
	}

	// Pre-fetch quoted events for quote posts (notes with q tag)
	quotedEventIDs := make(map[string]bool)
	quotedEventIndexes := make(map[string][]int) // Map event ID to indexes of items that quote it
	for i, item := range resp.Items {
		if item.Kind == 1 {
			for _, tag := range item.Tags {
				if len(tag) >= 2 && tag[0] == "q" {
					eventID := tag[1]
					quotedEventIDs[eventID] = true
					quotedEventIndexes[eventID] = append(quotedEventIndexes[eventID], i)
					break // Only one q tag per event
				}
			}
		}
	}

	// Batch fetch quoted events
	quotedEvents := make(map[string]*Event)
	quotedEventProfiles := make(map[string]*ProfileInfo)
	if len(quotedEventIDs) > 0 {
		eventIDs := make([]string, 0, len(quotedEventIDs))
		for id := range quotedEventIDs {
			eventIDs = append(eventIDs, id)
		}
		// Fetch the quoted events using IDs filter
		filter := Filter{IDs: eventIDs, Limit: len(eventIDs)}
		fetchedEvents, _ := fetchEventsFromRelays(relays, filter)
		// Collect pubkeys for profile fetching
		pubkeys := make(map[string]bool)
		for i := range fetchedEvents {
			ev := &fetchedEvents[i]
			quotedEvents[ev.ID] = ev
			pubkeys[ev.PubKey] = true
		}
		// Fetch profiles for quoted event authors
		if len(pubkeys) > 0 {
			pks := make([]string, 0, len(pubkeys))
			for pk := range pubkeys {
				pks = append(pks, pk)
			}
			quotedEventProfiles = fetchProfiles([]string{"wss://purplepag.es"}, pks)
		}
	}

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

		// Extract imeta images and title for kind 20 (picture notes)
		if item.Kind == 20 {
			items[i].ImagesHTML = extractImetaImages(item.Tags)
			items[i].Title = extractTitle(item.Tags)
		}

		// Extract metadata and render markdown for kind 30023 (long-form articles)
		if item.Kind == 30023 {
			items[i].Title = extractTitle(item.Tags)
			items[i].Summary = extractSummary(item.Tags)
			items[i].HeaderImage = extractHeaderImage(item.Tags)
			items[i].PublishedAt = extractPublishedAt(item.Tags)
			// For kind 30023, render markdown instead of processing as plain text
			items[i].ContentHTML = renderMarkdown(item.Content)
		}

		// Parse embedded event for kind 6 (reposts)
		if item.Kind == 6 && item.Content != "" {
			// Build profiles map from response items for reposted author lookup
			profilesMap := make(map[string]*ProfileInfo)
			for _, it := range resp.Items {
				if it.AuthorProfile != nil {
					profilesMap[it.Pubkey] = it.AuthorProfile
				}
			}
			items[i].RepostedEvent = parseRepostedEvent(item.Content, relays, resolvedRefs, linkPreviews, profilesMap)
		}

		// Attach quoted event for quote posts (kind 1 with q tag)
		if item.Kind == 1 {
			for _, tag := range item.Tags {
				if len(tag) >= 2 && tag[0] == "q" {
					quotedEventID := tag[1]
					items[i].QuotedEventID = quotedEventID
					// Always strip the nostr reference from content since we render the fallback box
					strippedContent := stripQuotedNostrRef(item.Content, quotedEventID)
					items[i].ContentHTML = processContentToHTMLFull(strippedContent, relays, resolvedRefs, linkPreviews)
					// Check if we fetched this event
					if qev, ok := quotedEvents[quotedEventID]; ok {
						// Build an HTMLEventItem for the quoted event
						qNpub, _ := encodeBech32Pubkey(qev.PubKey)
						quotedItem := &HTMLEventItem{
							ID:            qev.ID,
							Kind:          qev.Kind,
							Pubkey:        qev.PubKey,
							Npub:          qNpub,
							NpubShort:     formatNpubShort(qNpub),
							CreatedAt:     qev.CreatedAt,
							Content:       qev.Content,
							ContentHTML:   processContentToHTMLFull(qev.Content, relays, resolvedRefs, linkPreviews),
							AuthorProfile: quotedEventProfiles[qev.PubKey],
						}
						// For kind 30023 (longform articles), extract title and summary
						if qev.Kind == 30023 {
							quotedItem.Title = extractTitle(qev.Tags)
							quotedItem.Summary = extractSummary(qev.Tags)
						}
						items[i].QuotedEvent = quotedItem
					}
					break
				}
			}
		}

		// Parse zap receipt for kind 9735
		if item.Kind == 9735 {
			zapInfo := parseZapReceipt(item.Tags)
			if zapInfo != nil {
				items[i].ZapSenderPubkey = zapInfo.SenderPubkey
				items[i].ZapRecipientPubkey = zapInfo.RecipientPubkey
				items[i].ZapAmountSats = zapInfo.AmountMsats / 1000 // Convert msats to sats
				items[i].ZapComment = zapInfo.Comment
				items[i].ZappedEventID = zapInfo.ZappedEventID

				// Generate npubs
				if zapInfo.SenderPubkey != "" {
					senderNpub, _ := encodeBech32Pubkey(zapInfo.SenderPubkey)
					items[i].ZapSenderNpub = senderNpub
					items[i].ZapSenderNpubShort = formatNpubShort(senderNpub)
				}
				if zapInfo.RecipientPubkey != "" {
					recipientNpub, _ := encodeBech32Pubkey(zapInfo.RecipientPubkey)
					items[i].ZapRecipientNpub = recipientNpub
					items[i].ZapRecipientNpubShort = formatNpubShort(recipientNpub)
				}

				// Look up profiles from existing profiles map
				profilesMap := make(map[string]*ProfileInfo)
				for _, it := range resp.Items {
					if it.AuthorProfile != nil {
						profilesMap[it.Pubkey] = it.AuthorProfile
					}
				}
				items[i].ZapSenderProfile = profilesMap[zapInfo.SenderPubkey]
				items[i].ZapRecipientProfile = profilesMap[zapInfo.RecipientPubkey]
			}
		}

		// Parse live event for kind 30311
		if item.Kind == 30311 {
			liveInfo := parseLiveEvent(item.Tags)
			if liveInfo != nil {
				items[i].LiveTitle = liveInfo.Title
				items[i].LiveSummary = liveInfo.Summary
				items[i].LiveImage = liveInfo.Image
				items[i].LiveStatus = liveInfo.Status
				items[i].LiveStreamingURL = liveInfo.StreamingURL
				items[i].LiveRecordingURL = liveInfo.RecordingURL
				items[i].LiveStarts = liveInfo.Starts
				items[i].LiveEnds = liveInfo.Ends
				items[i].LiveCurrentCount = liveInfo.CurrentCount
				items[i].LiveTotalCount = liveInfo.TotalCount
				items[i].LiveHashtags = liveInfo.Hashtags
				items[i].LiveDTag = liveInfo.DTag

				// Generate zap.stream embed URL if the streaming URL is from zap.stream
				if strings.Contains(liveInfo.StreamingURL, "zap.stream") || strings.Contains(liveInfo.RecordingURL, "zap.stream") {
					// Create naddr for the event
					naddr, err := EncodeNAddr(30311, item.Pubkey, liveInfo.DTag)
					if err == nil {
						items[i].LiveEmbedURL = "https://zap.stream/" + naddr
					}
				}

				// Build participant list with profiles from purplepag.es
				participants := make([]LiveParticipant, 0, len(liveInfo.ParticipantPubkeys))
				for _, pk := range liveInfo.ParticipantPubkeys {
					npub, _ := encodeBech32Pubkey(pk)
					participant := LiveParticipant{
						Pubkey:    pk,
						Npub:      npub,
						NpubShort: formatNpubShort(npub),
						Role:      liveInfo.ParticipantRoles[pk],
						Profile:   liveParticipantProfiles[pk],
					}
					participants = append(participants, participant)
				}
				items[i].LiveParticipants = participants
			}
		}

		// Parse highlight for kind 9802
		if item.Kind == 9802 {
			highlightInfo := parseHighlight(item.Tags)
			if highlightInfo != nil {
				items[i].HighlightContext = highlightInfo.Context
				items[i].HighlightComment = highlightInfo.Comment
				items[i].HighlightSourceURL = highlightInfo.SourceURL
				items[i].HighlightSourceRef = highlightInfo.SourceRef
			}
		}

		// Parse bookmarks for kind 10003
		if item.Kind == 10003 {
			bookmarkInfo := parseBookmarks(item.Tags)
			if bookmarkInfo != nil {
				items[i].BookmarkEventIDs = bookmarkInfo.EventIDs
				items[i].BookmarkArticleRefs = bookmarkInfo.ArticleRefs
				items[i].BookmarkHashtags = bookmarkInfo.Hashtags
				items[i].BookmarkURLs = bookmarkInfo.URLs
				items[i].BookmarkCount = len(bookmarkInfo.EventIDs) + len(bookmarkInfo.ArticleRefs) + len(bookmarkInfo.Hashtags) + len(bookmarkInfo.URLs)
			}
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
		KindFilter:    computeKindFilter(kinds),
		ActiveRelays:  relays,
		CurrentURL:    currentURL,
		ThemeClass:    themeClass,
		ThemeLabel:    themeLabel,
		CSRFToken:     csrfToken,
	}

	// Add session info if logged in
	if session != nil && session.Connected {
		data.LoggedIn = true
		pubkeyHex := hex.EncodeToString(session.UserPubKey)
		data.UserPubKey = pubkeyHex
		data.UserDisplayName = getUserDisplayName(pubkeyHex)
		data.HasUnreadNotifications = hasUnreadNotifs
	}

	// Use cached template for better performance
	var buf strings.Builder
	if err := cachedHTMLTemplate.Execute(&buf, data); err != nil {
		return "", err
	}

	return buf.String(), nil
}

var htmlThreadTemplate = `<!DOCTYPE html>
<html lang="en"{{if .ThemeClass}} class="{{.ThemeClass}}"{{end}}>
<head>
  <meta charset="UTF-8">
  <meta name="viewport" content="width=device-width, initial-scale=1.0">
  <title>Thread - Nostr Hypermedia</title>
  <link rel="icon" href="/static/favicon.ico" />
  <style>
    :root {
      --bg-page: #f5f5f5;
      --bg-container: #ffffff;
      --bg-card: #ffffff;
      --bg-secondary: #f8f9fa;
      --bg-input: #ffffff;
      --bg-badge: #f0f0f0;
      --bg-badge-hover: #e0e0e0;
      --bg-reply-badge: #e7eaff;
      --text-primary: #333333;
      --text-secondary: #666666;
      --text-muted: #999999;
      --text-content: #24292e;
      --border-color: #e1e4e8;
      --border-light: #dee2e6;
      --accent: #667eea;
      --accent-hover: #5568d3;
      --accent-secondary: #764ba2;
      --success: #2e7d32;
      --success-bg: #28a745;
      --success-hover: #218838;
      --link-preview-bg: #fafbfc;
      --quoted-bg: #f8f9fa;
      --error-bg: #fff5f5;
      --error-border: #fecaca;
      --error-accent: #dc2626;
      --ref-event-bg: #e8f4fd;
      --ref-event-border: #b8daff;
      --ref-event-color: #0056b3;
      --ref-event-hover: #d1e9fc;
      --ref-addr-bg: #fff3e0;
      --ref-addr-border: #ffcc80;
      --ref-addr-color: #e65100;
      --ref-addr-hover: #ffe0b2;
      --shadow: rgba(0,0,0,0.1);
      --shadow-accent: rgba(102, 126, 234, 0.15);
    }
    @media (prefers-color-scheme: dark) {
      :root:not(.light) {
        --bg-page: #121212;
        --bg-container: #1e1e1e;
        --bg-card: #1e1e1e;
        --bg-secondary: #252525;
        --bg-input: #2a2a2a;
        --bg-badge: #2a2a2a;
        --bg-badge-hover: #3a3a3a;
        --bg-reply-badge: #2d2d4a;
        --text-primary: #e4e4e7;
        --text-secondary: #a1a1aa;
        --text-muted: #71717a;
        --text-content: #e4e4e7;
        --border-color: #333333;
        --border-light: #333333;
        --accent: #818cf8;
        --accent-hover: #6366f1;
        --accent-secondary: #a78bfa;
        --success: #4ade80;
        --success-bg: #22c55e;
        --success-hover: #16a34a;
        --link-preview-bg: #252525;
        --quoted-bg: #252525;
        --error-bg: #2d1f1f;
        --error-border: #7f1d1d;
        --error-accent: #f87171;
        --ref-event-bg: #1e2a3a;
        --ref-event-border: #3b5998;
        --ref-event-color: #60a5fa;
        --ref-event-hover: #253545;
        --ref-addr-bg: #2a2518;
        --ref-addr-border: #92400e;
        --ref-addr-color: #fbbf24;
        --ref-addr-hover: #352f1e;
        --shadow: rgba(0,0,0,0.3);
        --shadow-accent: rgba(129, 140, 248, 0.2);
      }
    }
    html.dark {
      --bg-page: #121212;
      --bg-container: #1e1e1e;
      --bg-card: #1e1e1e;
      --bg-secondary: #252525;
      --bg-input: #2a2a2a;
      --bg-badge: #2a2a2a;
      --bg-badge-hover: #3a3a3a;
      --bg-reply-badge: #2d2d4a;
      --text-primary: #e4e4e7;
      --text-secondary: #a1a1aa;
      --text-muted: #71717a;
      --text-content: #e4e4e7;
      --border-color: #333333;
      --border-light: #333333;
      --accent: #818cf8;
      --accent-hover: #6366f1;
      --accent-secondary: #a78bfa;
      --success: #4ade80;
      --success-bg: #22c55e;
      --success-hover: #16a34a;
      --link-preview-bg: #252525;
      --quoted-bg: #252525;
      --error-bg: #2d1f1f;
      --error-border: #7f1d1d;
      --error-accent: #f87171;
      --ref-event-bg: #1e2a3a;
      --ref-event-border: #3b5998;
      --ref-event-color: #60a5fa;
      --ref-event-hover: #253545;
      --ref-addr-bg: #2a2518;
      --ref-addr-border: #92400e;
      --ref-addr-color: #fbbf24;
      --ref-addr-hover: #352f1e;
      --shadow: rgba(0,0,0,0.3);
      --shadow-accent: rgba(129, 140, 248, 0.2);
    }
    html.light {
      --bg-page: #f5f5f5;
      --bg-container: #ffffff;
      --bg-card: #ffffff;
      --bg-secondary: #f8f9fa;
      --bg-input: #ffffff;
      --bg-badge: #f0f0f0;
      --bg-badge-hover: #e0e0e0;
      --bg-reply-badge: #e7eaff;
      --text-primary: #333333;
      --text-secondary: #666666;
      --text-muted: #999999;
      --text-content: #24292e;
      --border-color: #e1e4e8;
      --border-light: #dee2e6;
      --accent: #667eea;
      --accent-hover: #5568d3;
      --accent-secondary: #764ba2;
      --success: #2e7d32;
      --success-bg: #28a745;
      --success-hover: #218838;
      --link-preview-bg: #fafbfc;
      --quoted-bg: #f8f9fa;
      --error-bg: #fff5f5;
      --error-border: #fecaca;
      --error-accent: #dc2626;
      --ref-event-bg: #e8f4fd;
      --ref-event-border: #b8daff;
      --ref-event-color: #0056b3;
      --ref-event-hover: #d1e9fc;
      --ref-addr-bg: #fff3e0;
      --ref-addr-border: #ffcc80;
      --ref-addr-color: #e65100;
      --ref-addr-hover: #ffe0b2;
      --shadow: rgba(0,0,0,0.1);
      --shadow-accent: rgba(102, 126, 234, 0.15);
    }
    * { box-sizing: border-box; margin: 0; padding: 0; }
    html { scroll-behavior: smooth; }
    .scroll-top {
      position: fixed;
      bottom: 20px;
      right: max(20px, calc((100vw - 840px) / 2 - 60px));
      width: 44px;
      height: 44px;
      background: var(--accent);
      color: white;
      border-radius: 50%;
      display: flex;
      align-items: center;
      justify-content: center;
      text-decoration: none;
      font-size: 20px;
      font-weight: bold;
      box-shadow: 0 2px 8px var(--shadow);
      opacity: 0.8;
      transition: opacity 0.2s, background 0.2s;
      z-index: 1000;
    }
    .scroll-top:hover {
      opacity: 1;
      background: var(--accent-hover);
    }
    @media (max-width: 600px) {
      .scroll-top {
        width: 40px;
        height: 40px;
        font-size: 18px;
        bottom: 16px;
        right: 16px;
      }
    }
    @keyframes flashFadeOut {
      0%, 60% { opacity: 1; max-height: 100px; padding: 12px; margin-bottom: 16px; }
      100% { opacity: 0; max-height: 0; padding: 0; margin-bottom: 0; overflow: hidden; }
    }
    .flash-message {
      background: var(--success-bg);
      color: white;
      border: 1px solid var(--success);
      border-radius: 4px;
      animation: flashFadeOut 3s ease-out forwards;
    }
    body {
      font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, Oxygen, Ubuntu, Cantarell, sans-serif;
      line-height: 1.6;
      color: var(--text-primary);
      background: var(--bg-page);
      padding: 20px;
    }
    .container {
      max-width: 800px;
      margin: 0 auto;
      background: var(--bg-container);
      border-radius: 8px;
      box-shadow: 0 2px 8px var(--shadow);
    }
    .sticky-section {
      position: sticky;
      top: 0;
      z-index: 100;
      background: var(--bg-container);
    }
    nav {
      padding: 12px 15px;
      background: var(--bg-secondary);
      border-bottom: 1px solid var(--border-light);
      display: flex;
      align-items: center;
      gap: 8px;
      flex-wrap: wrap;
    }
    .post-form {
      padding: 12px 16px;
      background: var(--bg-secondary);
      border-bottom: 1px solid var(--border-light);
    }
    .post-form textarea {
      width: 100%;
      padding: 8px 12px;
      border: 1px solid var(--border-color);
      border-radius: 4px;
      font-size: 14px;
      font-family: inherit;
      height: 36px;
      min-height: 36px;
      resize: none;
      margin-bottom: 0;
      background: var(--bg-input);
      color: var(--text-primary);
      box-sizing: border-box;
      transition: height 0.15s ease, min-height 0.15s ease;
      overflow: hidden;
    }
    .post-form:focus-within textarea {
      height: 80px;
      min-height: 80px;
      resize: vertical;
      margin-bottom: 10px;
      overflow: auto;
    }
    .post-form button[type="submit"] {
      display: none;
      padding: 8px 16px;
      background: linear-gradient(135deg, var(--accent) 0%, var(--accent-secondary) 100%) !important;
      color: white;
      border: none;
      border-radius: 4px;
      font-size: 14px;
      font-weight: 600;
      cursor: pointer;
    }
    .post-form:focus-within button[type="submit"] {
      display: block;
    }
    .nav-tab {
      padding: 8px 16px;
      background: var(--bg-badge);
      color: var(--text-secondary);
      text-decoration: none;
      border-radius: 4px;
      font-size: 14px;
      transition: background 0.2s, color 0.2s;
    }
    .nav-tab:hover { background: var(--bg-badge-hover); }
    .nav-tab.active {
      background: var(--accent);
      color: white;
    }
    .nav-tab.active:hover { background: var(--accent-hover); }
    .kind-filter {
      display: flex;
      gap: 16px;
      padding: 6px 20px;
      font-size: 12px;
      border-bottom: 1px solid var(--border);
    }
    .kind-filter a {
      color: var(--text-muted);
      text-decoration: none;
      padding: 2px 0;
      border-bottom: 2px solid transparent;
    }
    .kind-filter a:hover {
      color: var(--text-primary);
    }
    .kind-filter a.active {
      color: var(--text-primary);
      border-bottom-color: var(--accent);
    }
    .kind-filter-spacer {
      flex-grow: 1;
    }
    .edit-profile-link {
      color: var(--text-muted);
      text-decoration: none;
      padding: 2px 8px;
      border: 1px solid var(--border);
      border-radius: 4px;
      font-size: 11px;
    }
    .edit-profile-link:hover {
      color: var(--text-primary);
      border-color: var(--text-muted);
    }
    main { padding: 12px 20px 20px 20px; min-height: 400px; }
    .meta-info {
      background: var(--bg-secondary);
      padding: 12px;
      border-radius: 4px;
      font-size: 13px;
      color: var(--text-secondary);
      margin: 16px 0;
      display: flex;
      gap: 16px;
      justify-content: center;
      flex-wrap: wrap;
    }
    .meta-item { display: flex; align-items: center; gap: 4px; }
    .meta-label { font-weight: 600; }
    .note {
      background: var(--bg-card);
      border: 1px solid var(--border-color);
      border-radius: 6px;
      padding: 16px;
      margin: 12px 0;
      transition: box-shadow 0.2s;
    }
    .note:hover { box-shadow: 0 2px 8px var(--shadow); }
    .note.root {
      border: 1px solid var(--border-color);
      background: var(--bg-card);
    }
    .note-content {
      font-size: 15px;
      line-height: 1.6;
      margin: 12px 0;
      color: var(--text-content);
      white-space: pre-wrap;
      word-wrap: break-word;
    }
    .note-content img {
      max-width: 100%;
      border-radius: 8px;
      margin-top: 8px;
      display: block;
    }
    .image-gallery {
      display: flex;
      flex-wrap: wrap;
      gap: 8px;
      margin-top: 8px;
    }
    .image-gallery img {
      max-width: calc(50% - 4px);
      flex: 1 1 calc(50% - 4px);
      margin-top: 0;
      object-fit: cover;
      aspect-ratio: 1;
    }
    /* Kind 20 picture note styles */
    .picture-note {
      margin: 12px 0;
    }
    .picture-title {
      font-size: 18px;
      font-weight: 600;
      color: var(--text-primary);
      margin-bottom: 12px;
    }
    .picture-gallery {
      display: flex;
      flex-direction: column;
      gap: 8px;
    }
    .picture-image {
      max-width: 100%;
      border-radius: 8px;
      display: block;
    }
    .picture-caption {
      font-size: 14px;
      color: var(--text-muted);
      margin-top: 12px;
      line-height: 1.5;
    }
    /* Kind 6 repost styles */
    .repost-indicator {
      font-size: 12px;
      color: var(--text-muted);
      margin-bottom: 8px;
    }
    .reposted-note {
      border: 1px solid var(--border-color);
      border-radius: 8px;
      padding: 12px;
      background: var(--bg-secondary);
    }
    .reposted-note .note-author {
      margin-bottom: 8px;
    }
    .reposted-note .author-avatar {
      width: 32px;
      height: 32px;
    }
    .repost-empty {
      font-style: italic;
      color: var(--text-muted);
    }
    /* View note link style for quoted/reposted notes */
    .view-note-link {
      display: block;
      margin-top: 8px;
      color: var(--accent-color);
      text-decoration: none;
      font-size: 0.9em;
    }
    .view-note-link:hover {
      text-decoration: underline;
    }
    .quoted-note {
      border: 1px solid var(--border-color);
      border-radius: 8px;
      padding: 12px;
      margin-top: 12px;
      background: var(--bg-secondary);
      cursor: pointer;
      overflow: hidden;
    }
    .quoted-note img {
      max-width: 100%;
      height: auto;
    }
    .quoted-note:hover {
      border-color: var(--accent-color);
    }
    .quoted-note .note-author {
      margin-bottom: 8px;
    }
    .quoted-note .author-avatar {
      width: 32px;
      height: 32px;
    }
    .quoted-note-fallback {
      font-style: italic;
      color: var(--text-muted);
    }
    /* Kind 9735 zap receipt styles */
    .zap-content {
      display: flex;
      align-items: flex-start;
      gap: 12px;
    }
    .zap-icon {
      font-size: 20px;
      line-height: 1;
    }
    .zap-info {
      flex: 1;
    }
    .zap-header {
      display: flex;
      flex-wrap: wrap;
      align-items: center;
      gap: 6px;
      font-size: 15px;
    }
    .zap-sender, .zap-recipient {
      font-weight: 600;
      color: var(--accent);
      text-decoration: none;
    }
    .zap-sender:hover, .zap-recipient:hover {
      text-decoration: underline;
    }
    .zap-action {
      color: var(--text-muted);
    }
    .zap-amount {
      font-weight: 600;
      color: var(--text-primary);
    }
    .zap-comment {
      margin-top: 8px;
      font-size: 14px;
      color: var(--text-primary);
    }
    .zap-target {
      margin-top: 8px;
      font-size: 13px;
    }
    /* Kind 30311 live event styles */
    .live-event {
      padding: 0;
      overflow: hidden;
    }
    .live-event-thumbnail {
      position: relative;
      width: 100%;
      aspect-ratio: 16 / 9;
      background: var(--bg-tertiary);
      overflow: hidden;
    }
    .live-event-thumbnail img {
      width: 100%;
      height: 100%;
      object-fit: cover;
    }
    .live-event-thumbnail-placeholder {
      width: 100%;
      height: 100%;
      display: flex;
      align-items: center;
      justify-content: center;
      background: linear-gradient(135deg, var(--bg-tertiary) 0%, var(--bg-secondary) 100%);
    }
    .live-event-thumbnail-placeholder span {
      font-size: 48px;
      opacity: 0.3;
    }
    .live-event-overlay {
      position: absolute;
      top: 12px;
      left: 12px;
      display: flex;
      align-items: center;
      gap: 8px;
    }
    .live-badge {
      display: inline-flex;
      align-items: center;
      gap: 4px;
      padding: 4px 10px;
      border-radius: 4px;
      font-size: 12px;
      font-weight: 700;
      text-transform: uppercase;
      letter-spacing: 0.5px;
      background: rgba(0,0,0,0.7);
      color: white;
      backdrop-filter: blur(4px);
    }
    .live-badge.live {
      background: #dc2626;
      animation: pulse 2s ease-in-out infinite;
    }
    .live-badge.live::before {
      content: "";
      width: 8px;
      height: 8px;
      background: white;
      border-radius: 50%;
    }
    @keyframes pulse {
      0%, 100% { opacity: 1; }
      50% { opacity: 0.8; }
    }
    .live-badge.planned {
      background: #2563eb;
    }
    .live-badge.ended {
      background: rgba(0,0,0,0.6);
    }
    .live-viewers {
      display: inline-flex;
      align-items: center;
      gap: 4px;
      padding: 4px 10px;
      border-radius: 4px;
      font-size: 12px;
      font-weight: 500;
      background: rgba(0,0,0,0.7);
      color: white;
      backdrop-filter: blur(4px);
    }
    .live-event-body {
      padding: 16px;
    }
    .live-event-title {
      font-size: 18px;
      font-weight: 600;
      color: var(--text-primary);
      margin: 0 0 8px 0;
      line-height: 1.3;
    }
    .live-event-summary {
      font-size: 14px;
      color: var(--text-secondary);
      margin: 0 0 12px 0;
      line-height: 1.5;
      display: -webkit-box;
      -webkit-line-clamp: 2;
      -webkit-box-orient: vertical;
      overflow: hidden;
    }
    .live-event-host {
      display: flex;
      align-items: center;
      gap: 8px;
      margin-bottom: 4px;
      font-size: 14px;
    }
    .host-label {
      color: var(--text-muted);
    }
    .host-link {
      display: inline-flex;
      align-items: center;
      gap: 6px;
      text-decoration: none;
      color: var(--text-primary);
    }
    .host-link:hover {
      color: var(--accent);
    }
    .host-avatar {
      width: 20px;
      height: 20px;
      border-radius: 50%;
      object-fit: cover;
    }
    .host-name {
      font-weight: 500;
    }
    .live-event-meta {
      display: flex;
      align-items: center;
      gap: 16px;
      font-size: 13px;
      color: var(--text-muted);
      margin-bottom: 12px;
    }
    .live-event-meta-item {
      display: flex;
      align-items: center;
      gap: 4px;
    }
    .live-event-tags {
      display: flex;
      gap: 6px;
      flex-wrap: wrap;
      margin-bottom: 12px;
    }
    .live-hashtag {
      font-size: 12px;
      color: var(--accent);
      background: var(--bg-secondary);
      padding: 4px 10px;
      border-radius: 14px;
      text-decoration: none;
    }
    .live-hashtag:hover {
      background: var(--bg-tertiary);
    }
    .live-participants {
      display: flex;
      align-items: center;
      gap: 12px;
      padding: 12px 0;
      border-top: 1px solid var(--border);
      margin-top: 4px;
    }
    .participants-list {
      display: flex;
      gap: 8px;
      flex-wrap: wrap;
      align-items: center;
    }
    .participant {
      display: inline-flex;
      align-items: center;
      gap: 6px;
      text-decoration: none;
      color: var(--text-primary);
      font-size: 13px;
      padding: 4px 10px 4px 4px;
      background: var(--bg-secondary);
      border-radius: 20px;
      transition: background 0.15s;
    }
    .participant:hover {
      background: var(--bg-tertiary);
    }
    .participant-avatar {
      width: 24px;
      height: 24px;
      border-radius: 50%;
      object-fit: cover;
      background: var(--bg-tertiary);
    }
    .participant-avatar-placeholder {
      width: 24px;
      height: 24px;
      border-radius: 50%;
      background: var(--bg-tertiary);
      display: flex;
      align-items: center;
      justify-content: center;
      font-size: 11px;
      color: var(--text-muted);
    }
    .participant-name {
      font-weight: 500;
    }
    .participant-role {
      font-size: 10px;
      color: var(--accent);
      font-weight: 600;
      text-transform: uppercase;
      margin-left: 2px;
    }
    .live-event-actions {
      padding: 12px 16px;
      display: flex;
      gap: 10px;
      background: var(--bg-secondary);
    }
    .live-action-btn {
      display: inline-flex;
      align-items: center;
      justify-content: center;
      gap: 8px;
      padding: 10px 20px;
      border-radius: 8px;
      font-size: 14px;
      font-weight: 600;
      text-decoration: none;
      transition: all 0.15s ease;
      flex: 1;
    }
    .stream-btn {
      background: #dc2626;
      color: white;
    }
    .stream-btn:hover {
      background: #b91c1c;
    }
    .recording-btn {
      background: var(--bg-tertiary);
      color: var(--text-primary);
      border: 1px solid var(--border);
    }
    .recording-btn:hover {
      background: var(--bg-secondary);
    }
    /* Highlight (kind 9802) styles */
    .highlight {
      padding: 16px 20px;
    }
    .highlight-blockquote {
      border-left: 4px solid var(--accent);
      padding-left: 16px;
      margin: 0 0 12px 0;
      font-style: italic;
      font-size: 15px;
      line-height: 1.6;
      color: var(--text-content);
    }
    .highlight-context {
      margin-top: 8px;
      padding: 12px;
      background: var(--bg-secondary);
      border-radius: 6px;
      font-size: 13px;
      color: var(--text-secondary);
      font-style: normal;
      line-height: 1.5;
    }
    .highlight-comment {
      margin-top: 12px;
      font-size: 14px;
      color: var(--text-primary);
      font-style: normal;
    }
    .highlight-source {
      margin-top: 12px;
      padding-top: 12px;
      border-top: 1px solid var(--border-color);
      font-size: 13px;
    }
    .highlight-source-link {
      color: var(--accent);
      text-decoration: none;
      word-break: break-all;
    }
    .highlight-source-link:hover {
      text-decoration: underline;
    }
    .highlight-meta {
      display: flex;
      justify-content: space-between;
      align-items: center;
      margin-top: 12px;
      padding-top: 12px;
      border-top: 1px solid var(--border-color);
    }
    .highlight-author {
      display: flex;
      align-items: center;
      gap: 8px;
    }
    .highlight-author-avatar {
      width: 24px;
      height: 24px;
      border-radius: 50%;
      object-fit: cover;
    }
    .highlight-author-name {
      color: var(--text-secondary);
      text-decoration: none;
      font-size: 13px;
    }
    .highlight-author-name:hover {
      color: var(--accent);
    }
    .highlight-time {
      font-size: 12px;
      color: var(--text-muted);
    }
    /* Bookmark list (kind 10003) styles */
    .bookmarks {
      padding: 16px 20px;
    }
    .bookmarks-header {
      display: flex;
      align-items: center;
      gap: 8px;
      margin-bottom: 16px;
    }
    .bookmarks-icon {
      font-size: 18px;
    }
    .bookmarks-title {
      font-size: 16px;
      font-weight: 600;
      color: var(--text-primary);
    }
    .bookmarks-count {
      font-size: 13px;
      color: var(--text-muted);
      margin-left: auto;
    }
    .bookmarks-section {
      margin-bottom: 16px;
    }
    .bookmarks-section:last-child {
      margin-bottom: 0;
    }
    .bookmarks-section-title {
      font-size: 12px;
      font-weight: 600;
      color: var(--text-secondary);
      text-transform: uppercase;
      margin-bottom: 8px;
      letter-spacing: 0.5px;
    }
    .bookmarks-list {
      display: flex;
      flex-direction: column;
      gap: 6px;
    }
    .bookmark-item {
      display: flex;
      align-items: center;
      gap: 8px;
      padding: 8px 12px;
      background: var(--bg-secondary);
      border-radius: 6px;
      font-size: 13px;
      color: var(--text-content);
      text-decoration: none;
      transition: background 0.15s ease;
    }
    .bookmark-item:hover {
      background: var(--bg-hover);
    }
    .bookmark-item-icon {
      font-size: 14px;
      opacity: 0.7;
    }
    .bookmark-item-text {
      overflow: hidden;
      text-overflow: ellipsis;
      white-space: nowrap;
    }
    .bookmark-hashtag {
      color: var(--accent);
    }
    .bookmarks-meta {
      display: flex;
      justify-content: space-between;
      align-items: center;
      margin-top: 16px;
      padding-top: 12px;
      border-top: 1px solid var(--border-color);
    }
    .bookmarks-author {
      display: flex;
      align-items: center;
      gap: 8px;
    }
    .bookmarks-author-avatar {
      width: 24px;
      height: 24px;
      border-radius: 50%;
      object-fit: cover;
    }
    .bookmarks-author-name {
      color: var(--text-secondary);
      text-decoration: none;
      font-size: 13px;
    }
    .bookmarks-author-name:hover {
      color: var(--accent);
    }
    .bookmarks-time {
      font-size: 12px;
      color: var(--text-muted);
    }
    .live-event-player {
      padding: 0;
      border-top: 1px solid var(--border);
    }
    .live-embed-iframe {
      width: 100%;
      height: 360px;
      display: block;
    }
    .live-event .note-meta {
      padding: 12px 16px;
      border-top: 1px solid var(--border);
    }
    /* Article preview styles (kind 30023 in timeline) */
    .article-preview {
      margin: 12px 0;
    }
    .article-preview-image {
      width: 100%;
      max-height: 200px;
      object-fit: cover;
      border-radius: 8px;
      margin-bottom: 12px;
    }
    .article-preview-title {
      font-size: 18px;
      font-weight: 600;
      color: var(--text-primary);
      margin-bottom: 8px;
      line-height: 1.3;
    }
    .article-preview-summary {
      font-size: 14px;
      color: var(--text-muted);
      margin-bottom: 12px;
      line-height: 1.5;
      display: -webkit-box;
      -webkit-line-clamp: 3;
      -webkit-box-orient: vertical;
      overflow: hidden;
    }
    /* Full article styles (kind 30023 in thread view) */
    .long-form-article {
      margin: 12px 0;
    }
    .article-header-image {
      width: 100%;
      max-height: 300px;
      object-fit: cover;
      border-radius: 8px;
      margin-bottom: 16px;
    }
    .article-title {
      font-size: 24px;
      font-weight: 700;
      color: var(--text-primary);
      margin-bottom: 12px;
      line-height: 1.3;
    }
    .article-summary {
      font-size: 16px;
      color: var(--text-muted);
      margin-bottom: 12px;
      font-style: italic;
      line-height: 1.5;
    }
    .article-published {
      font-size: 13px;
      color: var(--text-muted);
      margin-bottom: 16px;
    }
    .article-content {
      font-size: 15px;
      line-height: 1.8;
      color: var(--text-primary);
    }
    .article-content h1, .article-content h2, .article-content h3 {
      margin-top: 24px;
      margin-bottom: 12px;
      color: var(--text-primary);
    }
    .article-content h1 { font-size: 22px; }
    .article-content h2 { font-size: 20px; }
    .article-content h3 { font-size: 18px; }
    .article-content p {
      margin-bottom: 16px;
    }
    .article-content ul, .article-content ol {
      margin-bottom: 16px;
      padding-left: 24px;
    }
    .article-content li {
      margin-bottom: 8px;
    }
    .article-content blockquote {
      border-left: 4px solid var(--border-color);
      padding-left: 16px;
      margin: 16px 0;
      color: var(--text-muted);
      font-style: italic;
    }
    .article-content code {
      background: var(--bg-secondary);
      padding: 2px 6px;
      border-radius: 4px;
      font-family: monospace;
      font-size: 14px;
    }
    .article-content pre {
      background: var(--bg-secondary);
      padding: 16px;
      border-radius: 8px;
      overflow-x: auto;
      margin-bottom: 16px;
    }
    .article-content pre code {
      background: none;
      padding: 0;
    }
    .article-content a {
      color: var(--accent-color);
    }
    .article-content img {
      max-width: 100%;
      border-radius: 8px;
      margin: 16px 0;
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
      color: var(--accent);
      text-decoration: none;
    }
    .note-content a:hover {
      text-decoration: underline;
    }
    .link-preview {
      display: flex;
      border: 1px solid var(--border-color);
      border-radius: 8px;
      margin: 12px 0;
      overflow: hidden;
      text-decoration: none;
      color: inherit;
      background: var(--link-preview-bg);
      transition: border-color 0.2s, box-shadow 0.2s;
    }
    .link-preview:hover {
      border-color: var(--accent);
      box-shadow: 0 2px 8px var(--shadow-accent);
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
      color: var(--text-muted);
      text-transform: uppercase;
      letter-spacing: 0.5px;
      margin-bottom: 2px;
    }
    .link-preview-title {
      font-size: 14px;
      font-weight: 600;
      color: var(--text-content);
      line-height: 1.3;
      margin-bottom: 4px;
      display: -webkit-box;
      -webkit-line-clamp: 2;
      -webkit-box-orient: vertical;
      overflow: hidden;
    }
    .link-preview-desc {
      font-size: 12px;
      color: var(--text-secondary);
      line-height: 1.4;
      display: -webkit-box;
      -webkit-line-clamp: 2;
      -webkit-box-orient: vertical;
      overflow: hidden;
    }
    .quoted-note {
      background: var(--quoted-bg);
      border: 1px solid var(--border-color);
      border-left: 3px solid var(--accent);
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
      color: var(--text-content);
    }
    .quoted-note .quoted-author-npub {
      font-family: monospace;
      font-size: 11px;
      color: var(--accent);
    }
    .quoted-note .quoted-content {
      color: var(--text-content);
      white-space: pre-wrap;
      word-wrap: break-word;
    }
    .quoted-note .quoted-meta {
      font-size: 11px;
      color: var(--text-secondary);
      margin-top: 8px;
    }
    .quoted-note .quoted-meta a {
      color: var(--accent);
    }
    .quoted-note.quoted-article .quoted-article-title {
      font-weight: 600;
      font-size: 15px;
      margin-bottom: 6px;
      color: var(--text-content);
    }
    .quoted-note.quoted-article .quoted-article-summary {
      color: var(--text-secondary);
      font-size: 14px;
      line-height: 1.4;
    }
    .quoted-note-error {
      background: var(--error-bg);
      border: 1px solid var(--error-border);
      border-left: 3px solid var(--error-accent);
      border-radius: 4px;
      padding: 8px 12px;
      margin: 8px 0;
      font-size: 13px;
      color: var(--text-secondary);
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
      background: var(--ref-event-bg);
      border: 1px solid var(--ref-event-border);
      color: var(--ref-event-color);
    }
    .nostr-ref-event:hover {
      background: var(--ref-event-hover);
    }
    .nostr-ref-profile {
      display: inline;
      padding: 0;
      margin: 0;
      border: none;
      border-radius: 0;
      background: none;
      color: var(--success);
      font-weight: 500;
    }
    .nostr-ref-profile:hover {
      background: none;
      text-decoration: underline;
    }
    .nostr-ref-addr {
      background: var(--ref-addr-bg);
      border: 1px solid var(--ref-addr-border);
      color: var(--ref-addr-color);
    }
    .nostr-ref-addr:hover {
      background: var(--ref-addr-hover);
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
      border: 2px solid var(--border-color);
    }
    .author-info {
      display: flex;
      flex-direction: column;
      gap: 2px;
    }
    .author-name {
      font-weight: 600;
      font-size: 15px;
      color: var(--text-primary);
    }
    .author-nip05 {
      font-size: 12px;
      color: var(--accent);
    }
    .note-meta {
      display: flex;
      gap: 16px;
      font-size: 12px;
      color: var(--text-secondary);
      margin-top: 12px;
      padding-top: 12px;
      border-top: 1px solid var(--border-color);
      flex-wrap: wrap;
    }
    .pubkey {
      font-family: monospace;
      font-size: 11px;
      color: var(--accent);
    }
    .replies-section {
      margin-top: 24px;
      padding-top: 20px;
      border-top: 2px solid var(--border-color);
    }
    .replies-section h3 {
      color: var(--text-secondary);
      font-size: 16px;
      margin-bottom: 16px;
    }
    .reply {
      margin-left: 20px;
      border-left: 3px solid var(--border-color);
      padding-left: 16px;
    }
    .reaction-badge {
      display: inline-flex;
      align-items: center;
      gap: 4px;
      padding: 4px 10px;
      background: var(--bg-badge);
      border: 1px solid var(--border-color);
      border-radius: 16px;
      font-size: 13px;
      color: var(--text-secondary);
      line-height: 1.4;
    }
    button[type="submit"].reaction-badge {
      display: inline-flex;
      align-items: center;
      gap: 4px;
      padding: 4px 10px;
      background: var(--bg-badge);
      border: 1px solid var(--border-color);
      border-radius: 16px;
      font-size: 13px;
      color: var(--text-secondary);
      line-height: 1.4;
      cursor: pointer;
      font-family: inherit;
      margin-top: 0;
    }
    button[type="submit"].reaction-badge:hover {
      background: var(--bg-badge-hover);
    }
    /* Utility classes */
    .ml-auto { margin-left: auto; }
    .mr-md { margin-right: 12px; }
    .flex { display: flex; }
    .flex-center { display: flex; align-items: center; }
    .gap-sm { gap: 8px; }
    .gap-md { gap: 12px; }
    .gap-lg { gap: 16px; }
    .text-link { color: var(--accent); text-decoration: none; }
    .text-link:hover { text-decoration: underline; }
    button.text-link, button.repost-btn { background: none !important; border: none !important; font: inherit; cursor: pointer; padding: 0 !important; margin: 0 !important; border-radius: 0 !important; color: var(--accent) !important; }
    button.text-link:hover { text-decoration: underline; }
    .note-footer {
      display: flex;
      gap: 16px;
      flex-wrap: wrap;
      margin-top: 12px;
      padding-top: 12px;
      border-top: 1px solid var(--border-color);
      align-items: center;
      font-size: 13px;
    }
    .note-footer-actions {
      display: flex;
      gap: 16px;
      flex-wrap: wrap;
    }
    .note-footer-reactions {
      display: flex;
      gap: 8px;
      flex-wrap: wrap;
      margin-left: auto;
    }
    .text-muted { color: var(--text-secondary); text-decoration: none; }
    .text-sm { font-size: 13px; }
    .text-xs { font-size: 12px; }
    .font-medium { font-weight: 500; }
    .inline-form { display: inline; margin: 0; }
    .ghost-btn {
      background: none;
      border: none;
      color: var(--text-secondary);
      cursor: pointer;
      font-family: inherit;
      padding: 0;
    }
    .accent-btn {
      appearance: none;
      -webkit-appearance: none;
      background: none !important;
      border: none !important;
      color: var(--accent) !important;
      cursor: pointer;
      font-family: inherit;
      font-size: inherit;
      padding: 0 !important;
      margin: 0 !important;
      line-height: inherit;
      border-radius: 0 !important;
    }
    .accent-btn:hover {
      text-decoration: underline;
      background: none !important;
    }
    /* Note reactions */
    .note-reactions-bar {
      margin: 12px 0;
      padding: 8px 0;
      border-top: 1px solid var(--border-color);
    }
    .note-reactions {
      margin: 8px 0;
    }
    /* Settings dropdown */
    .settings-dropdown { position: relative; }
    .settings-toggle {
      cursor: pointer;
      list-style: none;
      font-size: 16px;
    }
    .settings-menu {
      position: absolute;
      right: 0;
      top: 100%;
      margin-top: 8px;
      background: var(--bg-card);
      border: 1px solid var(--border-color);
      border-radius: 4px;
      padding: 10px 14px;
      box-shadow: 0 4px 12px var(--shadow);
      z-index: 100;
      white-space: nowrap;
      font-size: 12px;
      color: var(--text-secondary);
    }
    .settings-item { margin-bottom: 8px; }
    .settings-item:last-child { margin-bottom: 0; }
    .settings-divider {
      padding-top: 8px;
      border-top: 1px solid var(--border-light);
    }
    /* Notification bell */
    .notification-bell {
      position: relative;
      text-decoration: none;
      font-size: 16px;
    }
    .notification-badge {
      position: absolute;
      top: -4px;
      right: -6px;
      width: 8px;
      height: 8px;
      background: var(--accent-color);
      border-radius: 50%;
    }
    /* Checkbox toggle */
    .checkbox-link {
      text-decoration: none;
      display: flex;
      align-items: center;
      gap: 6px;
      color: var(--text-secondary);
    }
    .checkbox-box {
      display: inline-block;
      width: 14px;
      height: 14px;
      border: 2px solid var(--border-color);
      border-radius: 3px;
      background: transparent;
      position: relative;
    }
    .checkbox-box.checked {
      border-color: var(--accent);
      background: var(--accent);
    }
    .checkbox-check {
      position: absolute;
      left: 3px;
      top: 0px;
      width: 4px;
      height: 7px;
      border: solid white;
      border-width: 0 2px 2px 0;
      transform: rotate(45deg);
    }
    /* Note actions */
    .note-actions {
      display: flex;
      gap: 16px;
      align-items: center;
      margin-left: auto;
    }
    /* Error/alert box */
    .error-box {
      background: var(--error-bg);
      color: var(--error-accent);
      border: 1px solid var(--error-border);
      padding: 12px;
      border-radius: 4px;
      margin-bottom: 16px;
    }
    /* Reply form */
    .reply-form {
      background: var(--bg-card);
      padding: 16px;
      border-radius: 8px;
      border: 1px solid var(--border-color);
      margin: 16px 0;
    }
    .reply-form textarea {
      width: 100%;
      padding: 12px;
      border: 1px solid var(--border-color);
      border-radius: 4px;
      font-size: 14px;
      font-family: inherit;
      min-height: 60px;
      resize: vertical;
      margin-bottom: 10px;
      background: var(--bg-input);
      color: var(--text-primary);
      box-sizing: border-box;
    }
    .reply-form button[type="submit"] {
      padding: 10px 20px;
      background: linear-gradient(135deg, var(--accent) 0%, var(--accent-secondary) 100%);
      color: white;
      border: none;
      border-radius: 4px;
      font-size: 14px;
      font-weight: 600;
      cursor: pointer;
    }
    .reply-info {
      margin-bottom: 10px;
      font-size: 13px;
      color: var(--text-secondary);
    }
    .reply-info span {
      color: var(--accent);
      font-weight: 500;
    }
    .sr-only {
      position: absolute;
      width: 1px;
      height: 1px;
      padding: 0;
      margin: -1px;
      overflow: hidden;
      clip: rect(0, 0, 0, 0);
      white-space: nowrap;
      border: 0;
    }
    /* Login prompt box */
    .login-prompt-box {
      background: var(--bg-secondary);
      padding: 12px;
      border-radius: 8px;
      border: 1px solid var(--border-light);
      margin: 16px 0;
      text-align: center;
      color: var(--text-secondary);
      font-size: 14px;
    }
    /* Relay list in dropdown */
    .relay-item {
      padding: 1px 0;
      font-family: monospace;
      font-size: 11px;
      color: var(--text-secondary);
    }
    footer {
      text-align: center;
      padding: 20px;
      background: var(--bg-secondary);
      color: var(--text-secondary);
      font-size: 13px;
      border-top: 1px solid var(--border-color);
      border-radius: 0 0 8px 8px;
    }
  </style>
</head>
<body>
  <div id="top" class="container">
    <nav>
      {{if .LoggedIn}}
      <a href="/html/timeline?kinds=1&limit=20&feed=follows" class="nav-tab">Follows</a>
      {{end}}
      <a href="/html/timeline?kinds=1&limit=20&feed=global" class="nav-tab{{if not .LoggedIn}} active{{end}}">Global</a>
      {{if .LoggedIn}}
      <a href="/html/timeline?kinds=1&limit=20&feed=me" class="nav-tab">Me</a>
      {{end}}
      <div class="ml-auto flex-center gap-md">
        <span class="text-xs text-muted">{{len .Replies}} repl{{if eq (len .Replies) 1}}y{{else}}ies{{end}}</span>
        {{if .LoggedIn}}
        <a href="/html/notifications" class="notification-bell" title="Notifications">🔔{{if .HasUnreadNotifications}}<span class="notification-badge"></span>{{end}}</a>
        {{end}}
        <details class="settings-dropdown">
          <summary class="settings-toggle" title="Settings">⚙️</summary>
          <div class="settings-menu">
            <div class="settings-item">
              <form method="POST" action="/html/theme" class="inline-form">
                <button type="submit" class="ghost-btn text-xs">Theme: {{.ThemeLabel}}</button>
              </form>
            </div>
          </div>
        </details>
        {{if .LoggedIn}}
        <a href="/html/logout" class="text-muted text-sm">Logout</a>
        {{else}}
        <a href="/html/login" class="text-link text-sm font-medium">Login</a>
        {{end}}
      </div>
    </nav>

    <main>
      {{if .Success}}
      <div class="flash-message">{{.Success}}</div>
      {{end}}

      {{if .Root}}
      <article class="note root">
        <div class="note-author">
          <a href="/html/profile/{{.Root.Npub}}" class="text-link">
          {{if and .Root.AuthorProfile .Root.AuthorProfile.Picture}}
          <img class="author-avatar" src="{{.Root.AuthorProfile.Picture}}" alt="{{if .Root.AuthorProfile.DisplayName}}{{.Root.AuthorProfile.DisplayName}}{{else if .Root.AuthorProfile.Name}}{{.Root.AuthorProfile.Name}}{{else}}User{{end}}'s avatar">
          {{else}}
          <img class="author-avatar" src="/static/avatar.jpg" alt="Default avatar">
          {{end}}
          </a>
          <div class="author-info">
            <a href="/html/profile/{{.Root.Npub}}" class="text-link">
            {{if .Root.AuthorProfile}}
            {{if or .Root.AuthorProfile.DisplayName .Root.AuthorProfile.Name}}
            <span class="author-name">{{if .Root.AuthorProfile.DisplayName}}{{.Root.AuthorProfile.DisplayName}}{{else}}{{.Root.AuthorProfile.Name}}{{end}}</span>
            {{if .Root.AuthorProfile.Nip05}}<span class="author-nip05">{{.Root.AuthorProfile.Nip05}}</span>{{end}}
            {{else if .Root.AuthorProfile.Nip05}}
            <span class="author-nip05">{{.Root.AuthorProfile.Nip05}}</span>
            {{else}}
            <span class="pubkey" title="{{.Root.Pubkey}}">{{.Root.NpubShort}}</span>
            {{end}}
            {{else}}
            <span class="pubkey" title="{{.Root.Pubkey}}">{{.Root.NpubShort}}</span>
            {{end}}
            </a>
            <span class="author-time">{{formatTime .Root.CreatedAt}}</span>
          </div>
        </div>
        {{if eq .Root.Kind 30023}}
        <article class="long-form-article">
          {{if .Root.HeaderImage}}<img src="{{.Root.HeaderImage}}" alt="Article header" class="article-header-image">{{end}}
          {{if .Root.Title}}<h2 class="article-title">{{.Root.Title}}</h2>{{end}}
          {{if .Root.Summary}}<p class="article-summary">{{.Root.Summary}}</p>{{end}}
          {{if .Root.PublishedAt}}<div class="article-published">Published: {{formatTime .Root.PublishedAt}}</div>{{end}}
          <div class="article-content">{{.Root.ContentHTML}}</div>
        </article>
        {{else}}
        <div class="note-content">{{.Root.ContentHTML}}</div>
        {{end}}
        {{if .Root.QuotedEvent}}
        <div class="quoted-note">
          <div class="quoted-author">
            {{if and .Root.QuotedEvent.AuthorProfile .Root.QuotedEvent.AuthorProfile.Picture}}
            <img src="{{.Root.QuotedEvent.AuthorProfile.Picture}}" alt="{{if .Root.QuotedEvent.AuthorProfile.DisplayName}}{{.Root.QuotedEvent.AuthorProfile.DisplayName}}{{else if .Root.QuotedEvent.AuthorProfile.Name}}{{.Root.QuotedEvent.AuthorProfile.Name}}{{else}}User{{end}}'s avatar">
            {{end}}
            <span class="quoted-author-name">
              {{if .Root.QuotedEvent.AuthorProfile}}
              {{if or .Root.QuotedEvent.AuthorProfile.DisplayName .Root.QuotedEvent.AuthorProfile.Name}}
              {{if .Root.QuotedEvent.AuthorProfile.DisplayName}}{{.Root.QuotedEvent.AuthorProfile.DisplayName}}{{else}}{{.Root.QuotedEvent.AuthorProfile.Name}}{{end}}
              {{else}}
              {{.Root.QuotedEvent.NpubShort}}
              {{end}}
              {{else}}
              {{.Root.QuotedEvent.NpubShort}}
              {{end}}
            </span>
          </div>
          {{if eq .Root.QuotedEvent.Kind 30023}}
          <div class="quoted-article-title">{{if .Root.QuotedEvent.Title}}{{.Root.QuotedEvent.Title}}{{else}}Untitled Article{{end}}</div>
          {{if .Root.QuotedEvent.Summary}}<div class="quoted-article-summary">{{.Root.QuotedEvent.Summary}}</div>{{end}}
          <a href="/html/thread/{{.Root.QuotedEvent.ID}}" class="view-note-link">Read article &rarr;</a>
          {{else}}
          <div class="quoted-content">{{.Root.QuotedEvent.ContentHTML}}</div>
          <a href="/html/thread/{{.Root.QuotedEvent.ID}}" class="view-note-link">View quoted note &rarr;</a>
          {{end}}
        </div>
        {{else if .Root.QuotedEventID}}
        <div class="quoted-note quoted-note-fallback">
          <a href="/html/thread/{{.Root.QuotedEventID}}" class="view-note-link">View quoted note &rarr;</a>
        </div>
        {{end}}
        <div class="note-footer">
          <div class="note-footer-actions">
          {{if $.LoggedIn}}
          <form method="POST" action="/html/repost" class="inline-form">
            <input type="hidden" name="csrf_token" value="{{$.CSRFToken}}">
            <input type="hidden" name="event_id" value="{{.Root.ID}}">
            <input type="hidden" name="event_pubkey" value="{{.Root.Pubkey}}">
            <input type="hidden" name="return_url" value="{{$.CurrentURL}}">
            <button type="submit" class="text-link">Repost</button>
          </form>
          <a href="/html/quote/{{.Root.ID}}" class="text-link">Quote</a>
          <form method="POST" action="/html/react" class="inline-form">
            <input type="hidden" name="csrf_token" value="{{$.CSRFToken}}">
            <input type="hidden" name="event_id" value="{{.Root.ID}}">
            <input type="hidden" name="event_pubkey" value="{{.Root.Pubkey}}">
            <input type="hidden" name="return_url" value="{{$.CurrentURL}}">
            <input type="hidden" name="reaction" value="❤️">
            <button type="submit" class="text-link">Like</button>
          </form>
          <form method="POST" action="/html/bookmark" class="inline-form">
            <input type="hidden" name="csrf_token" value="{{$.CSRFToken}}">
            <input type="hidden" name="event_id" value="{{.Root.ID}}">
            <input type="hidden" name="return_url" value="{{$.CurrentURL}}">
            {{if .Root.IsBookmarked}}
            <input type="hidden" name="action" value="remove">
            <button type="submit" class="text-link">Unbookmark</button>
            {{else}}
            <input type="hidden" name="action" value="add">
            <button type="submit" class="text-link">Bookmark</button>
            {{end}}
          </form>
          {{end}}
          {{if .Root.ParentID}}
          <a href="/html/thread/{{.Root.ParentID}}" class="text-link">↑ Parent</a>
          {{end}}
          </div>
          {{if and .Root.Reactions (gt .Root.Reactions.Total 0)}}
          <div class="note-footer-reactions">
            {{range $type, $count := .Root.Reactions.ByType}}
            <span class="reaction-badge">{{$type}} {{$count}}</span>
            {{end}}
          </div>
          {{end}}
        </div>
      </article>

      {{if .LoggedIn}}
      <form method="POST" action="/html/reply" class="reply-form">
        <input type="hidden" name="csrf_token" value="{{.CSRFToken}}">
        <input type="hidden" name="reply_to" value="{{.Root.ID}}">
        <input type="hidden" name="reply_to_pubkey" value="{{.Root.Pubkey}}">
        <div class="reply-info">
          Replying as: <span class="reply-author">{{.UserDisplayName}}</span>
        </div>
        <label for="reply-content" class="sr-only">Write a reply</label>
        <textarea id="reply-content" name="content" placeholder="Write a reply..." required></textarea>
        <button type="submit">Reply</button>
      </form>
      {{else}}
      <div class="login-prompt-box">
        <a href="/html/login" class="text-link">Login</a> to reply
      </div>
      {{end}}

      {{if .Replies}}
      <div class="replies-section">
        <h3>Replies ({{len .Replies}})</h3>
        {{range .Replies}}
        {{$reply := .}}
        <article class="note reply">
          <div class="note-author">
            <a href="/html/profile/{{.Npub}}" class="text-link">
            {{if and .AuthorProfile .AuthorProfile.Picture}}
            <img class="author-avatar" src="{{.AuthorProfile.Picture}}" alt="{{if .AuthorProfile.DisplayName}}{{.AuthorProfile.DisplayName}}{{else if .AuthorProfile.Name}}{{.AuthorProfile.Name}}{{else}}User{{end}}'s avatar">
            {{else}}
            <img class="author-avatar" src="/static/avatar.jpg" alt="Default avatar">
            {{end}}
            </a>
            <div class="author-info">
              <a href="/html/profile/{{.Npub}}" class="text-link">
              {{if .AuthorProfile}}
              {{if or .AuthorProfile.DisplayName .AuthorProfile.Name}}
              <span class="author-name">{{if .AuthorProfile.DisplayName}}{{.AuthorProfile.DisplayName}}{{else}}{{.AuthorProfile.Name}}{{end}}</span>
              {{if .AuthorProfile.Nip05}}<span class="author-nip05">{{.AuthorProfile.Nip05}}</span>{{end}}
              {{else if .AuthorProfile.Nip05}}
              <span class="author-nip05">{{.AuthorProfile.Nip05}}</span>
              {{else}}
              <span class="pubkey" title="{{.Pubkey}}">{{.NpubShort}}</span>
              {{end}}
              {{else}}
              <span class="pubkey" title="{{.Pubkey}}">{{.NpubShort}}</span>
              {{end}}
              </a>
              <span class="author-time">{{formatTime .CreatedAt}}</span>
            </div>
          </div>
          <div class="note-content">{{.ContentHTML}}</div>
          {{if .QuotedEvent}}
          <div class="quoted-note">
            <div class="quoted-author">
              {{if and .QuotedEvent.AuthorProfile .QuotedEvent.AuthorProfile.Picture}}
              <img src="{{.QuotedEvent.AuthorProfile.Picture}}" alt="{{if .QuotedEvent.AuthorProfile.DisplayName}}{{.QuotedEvent.AuthorProfile.DisplayName}}{{else if .QuotedEvent.AuthorProfile.Name}}{{.QuotedEvent.AuthorProfile.Name}}{{else}}User{{end}}'s avatar">
              {{end}}
              <span class="quoted-author-name">
                {{if .QuotedEvent.AuthorProfile}}
                {{if or .QuotedEvent.AuthorProfile.DisplayName .QuotedEvent.AuthorProfile.Name}}
                {{if .QuotedEvent.AuthorProfile.DisplayName}}{{.QuotedEvent.AuthorProfile.DisplayName}}{{else}}{{.QuotedEvent.AuthorProfile.Name}}{{end}}
                {{else}}
                {{.QuotedEvent.NpubShort}}
                {{end}}
                {{else}}
                {{.QuotedEvent.NpubShort}}
                {{end}}
              </span>
            </div>
            {{if eq .QuotedEvent.Kind 30023}}
            <div class="quoted-article-title">{{if .QuotedEvent.Title}}{{.QuotedEvent.Title}}{{else}}Untitled Article{{end}}</div>
            {{if .QuotedEvent.Summary}}<div class="quoted-article-summary">{{.QuotedEvent.Summary}}</div>{{end}}
            <a href="/html/thread/{{.QuotedEvent.ID}}" class="view-note-link">Read article &rarr;</a>
            {{else}}
            <div class="quoted-content">{{.QuotedEvent.ContentHTML}}</div>
            <a href="/html/thread/{{.QuotedEvent.ID}}" class="view-note-link">View quoted note &rarr;</a>
            {{end}}
          </div>
          {{else if .QuotedEventID}}
          <div class="quoted-note quoted-note-fallback">
            <a href="/html/thread/{{.QuotedEventID}}" class="view-note-link">View quoted note &rarr;</a>
          </div>
          {{end}}
          <div class="note-footer">
            <div class="note-footer-actions">
            {{if $.LoggedIn}}
            <a href="/html/thread/{{.ID}}" class="text-link">Reply</a>
            <form method="POST" action="/html/repost" class="inline-form">
              <input type="hidden" name="csrf_token" value="{{$.CSRFToken}}">
              <input type="hidden" name="event_id" value="{{$reply.ID}}">
              <input type="hidden" name="event_pubkey" value="{{$reply.Pubkey}}">
              <input type="hidden" name="return_url" value="{{$.CurrentURL}}">
              <button type="submit" class="text-link">Repost</button>
            </form>
            <a href="/html/quote/{{.ID}}" class="text-link">Quote</a>
            <form method="POST" action="/html/react" class="inline-form">
              <input type="hidden" name="csrf_token" value="{{$.CSRFToken}}">
              <input type="hidden" name="event_id" value="{{$reply.ID}}">
              <input type="hidden" name="event_pubkey" value="{{$reply.Pubkey}}">
              <input type="hidden" name="return_url" value="{{$.CurrentURL}}">
              <input type="hidden" name="reaction" value="❤️">
              <button type="submit" class="text-link">Like</button>
            </form>
            <form method="POST" action="/html/bookmark" class="inline-form">
              <input type="hidden" name="csrf_token" value="{{$.CSRFToken}}">
              <input type="hidden" name="event_id" value="{{$reply.ID}}">
              <input type="hidden" name="return_url" value="{{$.CurrentURL}}">
              {{if .IsBookmarked}}
              <input type="hidden" name="action" value="remove">
              <button type="submit" class="text-link">Unbookmark</button>
              {{else}}
              <input type="hidden" name="action" value="add">
              <button type="submit" class="text-link">Bookmark</button>
              {{end}}
            </form>
            {{end}}
            {{if gt .ReplyCount 0}}
            <a href="/html/thread/{{.ID}}" class="text-link">{{.ReplyCount}} replies ↓</a>
            {{end}}
            </div>
            {{if and .Reactions (gt .Reactions.Total 0)}}
            <div class="note-footer-reactions">
              {{range $type, $count := .Reactions.ByType}}
              <span class="reaction-badge">{{$type}} {{$count}}</span>
              {{end}}
            </div>
            {{end}}
          </div>
        </article>
        {{end}}
      </div>
      {{end}}
      {{else}}
      <div class="empty-state">
        <div class="empty-state-icon">🔍</div>
        <p>Event not found</p>
        <p class="empty-state-hint">This note may have been deleted or may not exist on the relays we checked.</p>
      </div>
      {{end}}
    </main>

    <footer>
      <p>{{if .Meta}}Generated: {{.Meta.GeneratedAt.Format "15:04:05"}} · {{end}}Zero-JS Hypermedia Browser</p>
    </footer>
  </div>
  <a href="#top" class="scroll-top" aria-label="Scroll to top">↑</a>
</body>
</html>
`

type HTMLThreadData struct {
	Title                  string
	Meta                   *MetaInfo
	Root                   *HTMLEventItem
	Replies                []HTMLEventItem
	LoggedIn               bool
	UserPubKey             string
	UserDisplayName        string
	CurrentURL             string
	ThemeClass             string // "dark", "light", or "" for system default
	ThemeLabel             string // Label for theme toggle button
	Success                string
	CSRFToken              string // CSRF token for form submission
	HasUnreadNotifications bool   // Whether there are notifications newer than last seen
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

func renderThreadHTML(resp ThreadResponse, relays []string, session *BunkerSession, currentURL string, themeClass, themeLabel, successMsg, csrfToken string, hasUnreadNotifs bool) (string, error) {
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

	// Collect q tags for quote post processing
	quotedEventIDs := make(map[string]bool)
	if resp.Root.Kind == 1 {
		for _, tag := range resp.Root.Tags {
			if len(tag) >= 2 && tag[0] == "q" {
				quotedEventIDs[tag[1]] = true
			}
		}
	}
	for _, item := range resp.Replies {
		if item.Kind == 1 {
			for _, tag := range item.Tags {
				if len(tag) >= 2 && tag[0] == "q" {
					quotedEventIDs[tag[1]] = true
				}
			}
		}
	}

	// Fetch quoted events and their profiles
	quotedEvents := make(map[string]*Event)
	quotedEventProfiles := make(map[string]*ProfileInfo)
	if len(quotedEventIDs) > 0 {
		ids := make([]string, 0, len(quotedEventIDs))
		for id := range quotedEventIDs {
			ids = append(ids, id)
		}
		filter := Filter{
			IDs:   ids,
			Limit: len(ids),
		}
		fetchedEvents, _ := fetchEventsFromRelays(relays, filter)
		pubkeys := make(map[string]bool)
		for i := range fetchedEvents {
			ev := &fetchedEvents[i]
			quotedEvents[ev.ID] = ev
			pubkeys[ev.PubKey] = true
		}
		if len(pubkeys) > 0 {
			pks := make([]string, 0, len(pubkeys))
			for pk := range pubkeys {
				pks = append(pks, pk)
			}
			quotedEventProfiles = fetchProfiles(relays, pks)
		}
	}

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

	// Handle kind 30023 (long-form articles) - extract metadata and render markdown
	if resp.Root.Kind == 30023 {
		root.Title = extractTitle(resp.Root.Tags)
		root.Summary = extractSummary(resp.Root.Tags)
		root.HeaderImage = extractHeaderImage(resp.Root.Tags)
		root.PublishedAt = extractPublishedAt(resp.Root.Tags)
		root.ContentHTML = renderMarkdown(resp.Root.Content)
	}

	// Handle quote posts for root event (kind 1 with q tag)
	if resp.Root.Kind == 1 {
		for _, tag := range resp.Root.Tags {
			if len(tag) >= 2 && tag[0] == "q" {
				quotedEventID := tag[1]
				root.QuotedEventID = quotedEventID
				// Strip the nostr reference from content since we render the fallback box
				strippedContent := stripQuotedNostrRef(resp.Root.Content, quotedEventID)
				root.ContentHTML = processContentToHTMLFull(strippedContent, relays, resolvedRefs, linkPreviews)
				// Check if we fetched this event
				if qev, ok := quotedEvents[quotedEventID]; ok {
					qNpub, _ := encodeBech32Pubkey(qev.PubKey)
					quotedItem := &HTMLEventItem{
						ID:            qev.ID,
						Kind:          qev.Kind,
						Pubkey:        qev.PubKey,
						Npub:          qNpub,
						NpubShort:     formatNpubShort(qNpub),
						CreatedAt:     qev.CreatedAt,
						Content:       qev.Content,
						ContentHTML:   processContentToHTMLFull(qev.Content, relays, resolvedRefs, linkPreviews),
						AuthorProfile: quotedEventProfiles[qev.PubKey],
					}
					// For kind 30023 (longform articles), extract title and summary
					if qev.Kind == 30023 {
						quotedItem.Title = extractTitle(qev.Tags)
						quotedItem.Summary = extractSummary(qev.Tags)
					}
					root.QuotedEvent = quotedItem
				}
				break
			}
		}
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

		// Handle quote posts for replies (kind 1 with q tag)
		if item.Kind == 1 {
			for _, tag := range item.Tags {
				if len(tag) >= 2 && tag[0] == "q" {
					quotedEventID := tag[1]
					replies[i].QuotedEventID = quotedEventID
					// Strip the nostr reference from content since we render the fallback box
					strippedContent := stripQuotedNostrRef(item.Content, quotedEventID)
					replies[i].ContentHTML = processContentToHTMLFull(strippedContent, relays, resolvedRefs, linkPreviews)
					// Check if we fetched this event
					if qev, ok := quotedEvents[quotedEventID]; ok {
						qNpub, _ := encodeBech32Pubkey(qev.PubKey)
						quotedItem := &HTMLEventItem{
							ID:            qev.ID,
							Kind:          qev.Kind,
							Pubkey:        qev.PubKey,
							Npub:          qNpub,
							NpubShort:     formatNpubShort(qNpub),
							CreatedAt:     qev.CreatedAt,
							Content:       qev.Content,
							ContentHTML:   processContentToHTMLFull(qev.Content, relays, resolvedRefs, linkPreviews),
							AuthorProfile: quotedEventProfiles[qev.PubKey],
						}
						// For kind 30023 (longform articles), extract title and summary
						if qev.Kind == 30023 {
							quotedItem.Title = extractTitle(qev.Tags)
							quotedItem.Summary = extractSummary(qev.Tags)
						}
						replies[i].QuotedEvent = quotedItem
					}
					break
				}
			}
		}
	}

	data := HTMLThreadData{
		Title:      "Thread",
		Meta:       &resp.Meta,
		Root:       root,
		Replies:    replies,
		CurrentURL: currentURL,
		ThemeClass: themeClass,
		ThemeLabel: themeLabel,
		Success:    successMsg,
		CSRFToken:  csrfToken,
	}

	// Add session info
	if session != nil && session.Connected {
		data.LoggedIn = true
		pubkeyHex := hex.EncodeToString(session.UserPubKey)
		data.UserPubKey = pubkeyHex
		data.UserDisplayName = getUserDisplayName(pubkeyHex)
		data.HasUnreadNotifications = hasUnreadNotifs
	}

	// Use cached template for better performance
	var buf strings.Builder
	if err := cachedThreadTemplate.Execute(&buf, data); err != nil {
		return "", err
	}

	return buf.String(), nil
}

var htmlProfileTemplate = `<!DOCTYPE html>
<html lang="en"{{if .ThemeClass}} class="{{.ThemeClass}}"{{end}}>
<head>
  <meta charset="UTF-8">
  <meta name="viewport" content="width=device-width, initial-scale=1.0">
  <title>{{.Title}} - Nostr Hypermedia</title>
  <link rel="icon" href="/static/favicon.ico" />
  <style>
    :root {
      --bg-page: #f5f5f5;
      --bg-container: #ffffff;
      --bg-card: #ffffff;
      --bg-secondary: #f8f9fa;
      --bg-input: #ffffff;
      --bg-badge: #f0f0f0;
      --bg-badge-hover: #e0e0e0;
      --bg-reply-badge: #e7eaff;
      --text-primary: #333333;
      --text-secondary: #666666;
      --text-muted: #999999;
      --text-content: #24292e;
      --border-color: #e1e4e8;
      --border-light: #dee2e6;
      --accent: #667eea;
      --accent-hover: #5568d3;
      --accent-secondary: #764ba2;
      --success: #2e7d32;
      --success-bg: #28a745;
      --success-hover: #218838;
      --link-preview-bg: #fafbfc;
      --quoted-bg: #f8f9fa;
      --error-bg: #fff5f5;
      --error-border: #fecaca;
      --error-accent: #dc2626;
      --ref-event-bg: #e8f4fd;
      --ref-event-border: #b8daff;
      --ref-event-color: #0056b3;
      --ref-event-hover: #d1e9fc;
      --ref-addr-bg: #fff3e0;
      --ref-addr-border: #ffcc80;
      --ref-addr-color: #e65100;
      --ref-addr-hover: #ffe0b2;
      --shadow: rgba(0,0,0,0.1);
      --shadow-accent: rgba(102, 126, 234, 0.15);
    }
    @media (prefers-color-scheme: dark) {
      :root:not(.light) {
        --bg-page: #121212;
        --bg-container: #1e1e1e;
        --bg-card: #1e1e1e;
        --bg-secondary: #252525;
        --bg-input: #2a2a2a;
        --bg-badge: #2a2a2a;
        --bg-badge-hover: #3a3a3a;
        --bg-reply-badge: #2d2d4a;
        --text-primary: #e4e4e7;
        --text-secondary: #a1a1aa;
        --text-muted: #71717a;
        --text-content: #e4e4e7;
        --border-color: #333333;
        --border-light: #333333;
        --accent: #818cf8;
        --accent-hover: #6366f1;
        --accent-secondary: #a78bfa;
        --success: #4ade80;
        --success-bg: #22c55e;
        --success-hover: #16a34a;
        --link-preview-bg: #252525;
        --quoted-bg: #252525;
        --error-bg: #2d1f1f;
        --error-border: #7f1d1d;
        --error-accent: #f87171;
        --ref-event-bg: #1e2a3a;
        --ref-event-border: #3b5998;
        --ref-event-color: #60a5fa;
        --ref-event-hover: #253545;
        --ref-addr-bg: #2a2518;
        --ref-addr-border: #92400e;
        --ref-addr-color: #fbbf24;
        --ref-addr-hover: #352f1e;
        --shadow: rgba(0,0,0,0.3);
        --shadow-accent: rgba(129, 140, 248, 0.2);
      }
    }
    html.dark {
      --bg-page: #121212;
      --bg-container: #1e1e1e;
      --bg-card: #1e1e1e;
      --bg-secondary: #252525;
      --bg-input: #2a2a2a;
      --bg-badge: #2a2a2a;
      --bg-badge-hover: #3a3a3a;
      --bg-reply-badge: #2d2d4a;
      --text-primary: #e4e4e7;
      --text-secondary: #a1a1aa;
      --text-muted: #71717a;
      --text-content: #e4e4e7;
      --border-color: #333333;
      --border-light: #333333;
      --accent: #818cf8;
      --accent-hover: #6366f1;
      --accent-secondary: #a78bfa;
      --success: #4ade80;
      --success-bg: #22c55e;
      --success-hover: #16a34a;
      --link-preview-bg: #252525;
      --quoted-bg: #252525;
      --error-bg: #2d1f1f;
      --error-border: #7f1d1d;
      --error-accent: #f87171;
      --ref-event-bg: #1e2a3a;
      --ref-event-border: #3b5998;
      --ref-event-color: #60a5fa;
      --ref-event-hover: #253545;
      --ref-addr-bg: #2a2518;
      --ref-addr-border: #92400e;
      --ref-addr-color: #fbbf24;
      --ref-addr-hover: #352f1e;
      --shadow: rgba(0,0,0,0.3);
      --shadow-accent: rgba(129, 140, 248, 0.2);
    }
    html.light {
      --bg-page: #f5f5f5;
      --bg-container: #ffffff;
      --bg-card: #ffffff;
      --bg-secondary: #f8f9fa;
      --bg-input: #ffffff;
      --bg-badge: #f0f0f0;
      --bg-badge-hover: #e0e0e0;
      --bg-reply-badge: #e7eaff;
      --text-primary: #333333;
      --text-secondary: #666666;
      --text-muted: #999999;
      --text-content: #24292e;
      --border-color: #e1e4e8;
      --border-light: #dee2e6;
      --accent: #667eea;
      --accent-hover: #5568d3;
      --accent-secondary: #764ba2;
      --success: #2e7d32;
      --success-bg: #28a745;
      --success-hover: #218838;
      --link-preview-bg: #fafbfc;
      --quoted-bg: #f8f9fa;
      --error-bg: #fff5f5;
      --error-border: #fecaca;
      --error-accent: #dc2626;
      --ref-event-bg: #e8f4fd;
      --ref-event-border: #b8daff;
      --ref-event-color: #0056b3;
      --ref-event-hover: #d1e9fc;
      --ref-addr-bg: #fff3e0;
      --ref-addr-border: #ffcc80;
      --ref-addr-color: #e65100;
      --ref-addr-hover: #ffe0b2;
      --shadow: rgba(0,0,0,0.1);
      --shadow-accent: rgba(102, 126, 234, 0.15);
    }
    * { box-sizing: border-box; margin: 0; padding: 0; }
    html { scroll-behavior: smooth; }
    .scroll-top {
      position: fixed;
      bottom: 20px;
      right: max(20px, calc((100vw - 840px) / 2 - 60px));
      width: 44px;
      height: 44px;
      background: var(--accent);
      color: white;
      border-radius: 50%;
      display: flex;
      align-items: center;
      justify-content: center;
      text-decoration: none;
      font-size: 20px;
      font-weight: bold;
      box-shadow: 0 2px 8px var(--shadow);
      opacity: 0.8;
      transition: opacity 0.2s, background 0.2s;
      z-index: 1000;
    }
    .scroll-top:hover {
      opacity: 1;
      background: var(--accent-hover);
    }
    @media (max-width: 600px) {
      .scroll-top {
        width: 40px;
        height: 40px;
        font-size: 18px;
        bottom: 16px;
        right: 16px;
      }
    }
    @keyframes flashFadeOut {
      0%, 60% { opacity: 1; max-height: 100px; padding: 12px; margin-bottom: 16px; }
      100% { opacity: 0; max-height: 0; padding: 0; margin-bottom: 0; overflow: hidden; }
    }
    .flash-message {
      background: var(--success-bg);
      color: white;
      border: 1px solid var(--success);
      border-radius: 4px;
      animation: flashFadeOut 3s ease-out forwards;
    }
    body {
      font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, Oxygen, Ubuntu, Cantarell, sans-serif;
      line-height: 1.6;
      color: var(--text-primary);
      background: var(--bg-page);
      padding: 20px;
    }
    .container {
      max-width: 800px;
      margin: 0 auto;
      background: var(--bg-container);
      border-radius: 8px;
      box-shadow: 0 2px 8px var(--shadow);
    }
    .sticky-section {
      position: sticky;
      top: 0;
      z-index: 100;
      background: var(--bg-container);
    }
    nav {
      padding: 12px 15px;
      background: var(--bg-secondary);
      border-bottom: 1px solid var(--border-light);
      display: flex;
      align-items: center;
      gap: 8px;
      flex-wrap: wrap;
    }
    .post-form {
      padding: 12px 16px;
      background: var(--bg-secondary);
      border-bottom: 1px solid var(--border-light);
    }
    .post-form textarea {
      width: 100%;
      padding: 8px 12px;
      border: 1px solid var(--border-color);
      border-radius: 4px;
      font-size: 14px;
      font-family: inherit;
      height: 36px;
      min-height: 36px;
      resize: none;
      margin-bottom: 0;
      background: var(--bg-input);
      color: var(--text-primary);
      box-sizing: border-box;
      transition: height 0.15s ease, min-height 0.15s ease;
      overflow: hidden;
    }
    .post-form:focus-within textarea {
      height: 80px;
      min-height: 80px;
      resize: vertical;
      margin-bottom: 10px;
      overflow: auto;
    }
    .post-form button[type="submit"] {
      display: none;
      padding: 8px 16px;
      background: linear-gradient(135deg, var(--accent) 0%, var(--accent-secondary) 100%) !important;
      color: white;
      border: none;
      border-radius: 4px;
      font-size: 14px;
      font-weight: 600;
      cursor: pointer;
    }
    .post-form:focus-within button[type="submit"] {
      display: block;
    }
    .nav-tab {
      padding: 8px 16px;
      background: var(--bg-badge);
      color: var(--text-secondary);
      text-decoration: none;
      border-radius: 4px;
      font-size: 14px;
      transition: background 0.2s, color 0.2s;
    }
    .nav-tab:hover { background: var(--bg-badge-hover); }
    .nav-tab.active {
      background: var(--accent);
      color: white;
    }
    .nav-tab.active:hover { background: var(--accent-hover); }
    main { padding: 12px 20px 20px 20px; min-height: 400px; }
    .profile-header {
      display: flex;
      align-items: flex-start;
      gap: 20px;
      padding: 24px;
      background: var(--bg-secondary);
      border-radius: 8px;
      margin-bottom: 24px;
    }
    .profile-avatar {
      width: 80px;
      height: 80px;
      border-radius: 50%;
      object-fit: cover;
      border: 3px solid var(--accent);
      flex-shrink: 0;
      background: var(--bg-tertiary);
    }
    .profile-info {
      flex: 1;
    }
    .profile-name-row {
      display: flex;
      align-items: center;
      justify-content: space-between;
      gap: 16px;
      margin-bottom: 4px;
    }
    .profile-name {
      font-size: 24px;
      font-weight: 700;
      color: var(--text-content);
    }
    .follow-btn {
      padding: 8px 20px;
      font-size: 14px;
      font-weight: 600;
      border-radius: 20px;
      cursor: pointer;
      transition: all 0.2s;
      border: none;
    }
    .follow-btn.follow {
      background: var(--accent);
      color: white;
    }
    .follow-btn.follow:hover {
      background: var(--accent-hover);
    }
    .follow-btn.unfollow {
      background: var(--bg-tertiary);
      color: var(--text-secondary);
      border: 1px solid var(--border-color);
    }
    .follow-btn.unfollow:hover {
      background: #fee2e2;
      color: #dc2626;
      border-color: #dc2626;
    }
    .edit-profile-btn {
      padding: 8px 20px;
      font-size: 14px;
      font-weight: 600;
      border-radius: 20px;
      cursor: pointer;
      transition: all 0.2s;
      background: var(--bg-tertiary);
      color: var(--text-secondary);
      border: 1px solid var(--border-color);
      text-decoration: none;
      display: inline-block;
    }
    .edit-profile-btn:hover {
      background: var(--bg-secondary);
      color: var(--text-primary);
      border-color: var(--text-secondary);
    }
    /* Profile edit form styles */
    .edit-form-section {
      padding: 20px;
      border-bottom: 1px solid var(--border-color);
    }
    .edit-form-section h3 {
      font-size: 18px;
      margin-bottom: 20px;
      color: var(--text-primary);
    }
    .edit-form-group {
      margin-bottom: 16px;
    }
    .edit-form-group label {
      display: block;
      font-size: 13px;
      font-weight: 600;
      color: var(--text-secondary);
      margin-bottom: 6px;
    }
    .edit-form-group input[type="text"],
    .edit-form-group input[type="url"],
    .edit-form-group textarea {
      width: 100%;
      padding: 10px 12px;
      font-size: 14px;
      border: 1px solid var(--border-color);
      border-radius: 6px;
      background: var(--bg-input);
      color: var(--text-primary);
      transition: border-color 0.2s;
    }
    .edit-form-group input:focus,
    .edit-form-group textarea:focus {
      outline: none;
      border-color: var(--accent);
    }
    .edit-form-group textarea {
      min-height: 80px;
      resize: vertical;
    }
    .edit-form-hint {
      font-size: 11px;
      color: var(--text-muted);
      margin-top: 4px;
    }
    .edit-form-buttons {
      display: flex;
      gap: 12px;
      margin-top: 24px;
    }
    .edit-form-btn {
      padding: 10px 24px;
      font-size: 14px;
      font-weight: 600;
      border-radius: 6px;
      cursor: pointer;
      transition: all 0.2s;
      text-decoration: none;
      display: inline-block;
      text-align: center;
    }
    .edit-form-btn-primary {
      background: var(--accent);
      color: white;
      border: none;
    }
    .edit-form-btn-primary:hover {
      background: var(--accent-hover);
    }
    .edit-form-btn-secondary {
      background: transparent;
      color: var(--text-secondary);
      border: 1px solid var(--border-color);
    }
    .edit-form-btn-secondary:hover {
      background: var(--bg-secondary);
      color: var(--text-primary);
    }
    .edit-form-error {
      background: var(--error-bg);
      border: 1px solid var(--error-border);
      color: var(--error-accent);
      padding: 12px;
      border-radius: 6px;
      margin-bottom: 16px;
      font-size: 14px;
    }
    .profile-nip05 {
      font-size: 14px;
      color: var(--accent);
      margin-bottom: 8px;
    }
    .profile-npub {
      font-family: monospace;
      font-size: 12px;
      color: var(--text-secondary);
      background: var(--bg-badge);
      padding: 4px 8px;
      border-radius: 4px;
      display: inline-block;
      margin-bottom: 8px;
    }
    .profile-about {
      font-size: 14px;
      color: var(--text-secondary);
      line-height: 1.5;
    }
    .note {
      background: var(--bg-card);
      border: 1px solid var(--border-color);
      border-radius: 6px;
      padding: 16px;
      margin: 12px 0;
      transition: box-shadow 0.2s;
    }
    .note:hover { box-shadow: 0 2px 8px var(--shadow); }
    .note-content {
      font-size: 15px;
      line-height: 1.6;
      margin: 12px 0;
      color: var(--text-content);
      white-space: pre-wrap;
      word-wrap: break-word;
    }
    .note-content img {
      max-width: 100%;
      border-radius: 8px;
      margin-top: 8px;
      display: block;
    }
    .image-gallery {
      display: flex;
      flex-wrap: wrap;
      gap: 8px;
      margin-top: 8px;
    }
    .image-gallery img {
      max-width: calc(50% - 4px);
      flex: 1 1 calc(50% - 4px);
      margin-top: 0;
      object-fit: cover;
      aspect-ratio: 1;
    }
    /* Kind 20 picture note styles */
    .picture-note {
      margin: 12px 0;
    }
    .picture-title {
      font-size: 18px;
      font-weight: 600;
      color: var(--text-primary);
      margin-bottom: 12px;
    }
    .picture-gallery {
      display: flex;
      flex-direction: column;
      gap: 8px;
    }
    .picture-image {
      max-width: 100%;
      border-radius: 8px;
      display: block;
    }
    .picture-caption {
      font-size: 14px;
      color: var(--text-muted);
      margin-top: 12px;
      line-height: 1.5;
    }
    /* Kind 6 repost styles */
    .repost-indicator {
      font-size: 12px;
      color: var(--text-muted);
      margin-bottom: 8px;
    }
    .reposted-note {
      border: 1px solid var(--border-color);
      border-radius: 8px;
      padding: 12px;
      background: var(--bg-secondary);
    }
    .reposted-note .note-author {
      margin-bottom: 8px;
    }
    .reposted-note .author-avatar {
      width: 32px;
      height: 32px;
    }
    .repost-empty {
      font-style: italic;
      color: var(--text-muted);
    }
    /* View note link style for quoted/reposted notes */
    .view-note-link {
      display: block;
      margin-top: 8px;
      color: var(--accent-color);
      text-decoration: none;
      font-size: 0.9em;
    }
    .view-note-link:hover {
      text-decoration: underline;
    }
    .quoted-note {
      border: 1px solid var(--border-color);
      border-radius: 8px;
      padding: 12px;
      margin-top: 12px;
      background: var(--bg-secondary);
      cursor: pointer;
      overflow: hidden;
    }
    .quoted-note img {
      max-width: 100%;
      height: auto;
    }
    .quoted-note:hover {
      border-color: var(--accent-color);
    }
    .quoted-note .note-author {
      margin-bottom: 8px;
    }
    .quoted-note .author-avatar {
      width: 32px;
      height: 32px;
    }
    .quoted-note-fallback {
      font-style: italic;
      color: var(--text-muted);
    }
    /* Kind 9735 zap receipt styles */
    .zap-content {
      display: flex;
      align-items: flex-start;
      gap: 12px;
    }
    .zap-icon {
      font-size: 20px;
      line-height: 1;
    }
    .zap-info {
      flex: 1;
    }
    .zap-header {
      display: flex;
      flex-wrap: wrap;
      align-items: center;
      gap: 6px;
      font-size: 15px;
    }
    .zap-sender, .zap-recipient {
      font-weight: 600;
      color: var(--accent);
      text-decoration: none;
    }
    .zap-sender:hover, .zap-recipient:hover {
      text-decoration: underline;
    }
    .zap-action {
      color: var(--text-muted);
    }
    .zap-amount {
      font-weight: 600;
      color: var(--text-primary);
    }
    .zap-comment {
      margin-top: 8px;
      font-size: 14px;
      color: var(--text-primary);
    }
    .zap-target {
      margin-top: 8px;
      font-size: 13px;
    }
    /* Kind 30311 live event styles */
    .live-event {
      padding: 0;
      overflow: hidden;
    }
    .live-event-thumbnail {
      position: relative;
      width: 100%;
      aspect-ratio: 16 / 9;
      background: var(--bg-tertiary);
      overflow: hidden;
    }
    .live-event-thumbnail img {
      width: 100%;
      height: 100%;
      object-fit: cover;
    }
    .live-event-thumbnail-placeholder {
      width: 100%;
      height: 100%;
      display: flex;
      align-items: center;
      justify-content: center;
      background: linear-gradient(135deg, var(--bg-tertiary) 0%, var(--bg-secondary) 100%);
    }
    .live-event-thumbnail-placeholder span {
      font-size: 48px;
      opacity: 0.3;
    }
    .live-event-overlay {
      position: absolute;
      top: 12px;
      left: 12px;
      display: flex;
      align-items: center;
      gap: 8px;
    }
    .live-badge {
      display: inline-flex;
      align-items: center;
      gap: 4px;
      padding: 4px 10px;
      border-radius: 4px;
      font-size: 12px;
      font-weight: 700;
      text-transform: uppercase;
      letter-spacing: 0.5px;
      background: rgba(0,0,0,0.7);
      color: white;
      backdrop-filter: blur(4px);
    }
    .live-badge.live {
      background: #dc2626;
      animation: pulse 2s ease-in-out infinite;
    }
    .live-badge.live::before {
      content: "";
      width: 8px;
      height: 8px;
      background: white;
      border-radius: 50%;
    }
    @keyframes pulse {
      0%, 100% { opacity: 1; }
      50% { opacity: 0.8; }
    }
    .live-badge.planned {
      background: #2563eb;
    }
    .live-badge.ended {
      background: rgba(0,0,0,0.6);
    }
    .live-viewers {
      display: inline-flex;
      align-items: center;
      gap: 4px;
      padding: 4px 10px;
      border-radius: 4px;
      font-size: 12px;
      font-weight: 500;
      background: rgba(0,0,0,0.7);
      color: white;
      backdrop-filter: blur(4px);
    }
    .live-event-body {
      padding: 16px;
    }
    .live-event-title {
      font-size: 18px;
      font-weight: 600;
      color: var(--text-primary);
      margin: 0 0 8px 0;
      line-height: 1.3;
    }
    .live-event-summary {
      font-size: 14px;
      color: var(--text-secondary);
      margin: 0 0 12px 0;
      line-height: 1.5;
      display: -webkit-box;
      -webkit-line-clamp: 2;
      -webkit-box-orient: vertical;
      overflow: hidden;
    }
    .live-event-host {
      display: flex;
      align-items: center;
      gap: 8px;
      margin-bottom: 4px;
      font-size: 14px;
    }
    .host-label {
      color: var(--text-muted);
    }
    .host-link {
      display: inline-flex;
      align-items: center;
      gap: 6px;
      text-decoration: none;
      color: var(--text-primary);
    }
    .host-link:hover {
      color: var(--accent);
    }
    .host-avatar {
      width: 20px;
      height: 20px;
      border-radius: 50%;
      object-fit: cover;
    }
    .host-name {
      font-weight: 500;
    }
    .live-event-meta {
      display: flex;
      align-items: center;
      gap: 16px;
      font-size: 13px;
      color: var(--text-muted);
      margin-bottom: 12px;
    }
    .live-event-meta-item {
      display: flex;
      align-items: center;
      gap: 4px;
    }
    .live-event-tags {
      display: flex;
      gap: 6px;
      flex-wrap: wrap;
      margin-bottom: 12px;
    }
    .live-hashtag {
      font-size: 12px;
      color: var(--accent);
      background: var(--bg-secondary);
      padding: 4px 10px;
      border-radius: 14px;
      text-decoration: none;
    }
    .live-hashtag:hover {
      background: var(--bg-tertiary);
    }
    .live-participants {
      display: flex;
      align-items: center;
      gap: 12px;
      padding: 12px 0;
      border-top: 1px solid var(--border);
      margin-top: 4px;
    }
    .participants-list {
      display: flex;
      gap: 8px;
      flex-wrap: wrap;
      align-items: center;
    }
    .participant {
      display: inline-flex;
      align-items: center;
      gap: 6px;
      text-decoration: none;
      color: var(--text-primary);
      font-size: 13px;
      padding: 4px 10px 4px 4px;
      background: var(--bg-secondary);
      border-radius: 20px;
      transition: background 0.15s;
    }
    .participant:hover {
      background: var(--bg-tertiary);
    }
    .participant-avatar {
      width: 24px;
      height: 24px;
      border-radius: 50%;
      object-fit: cover;
      background: var(--bg-tertiary);
    }
    .participant-avatar-placeholder {
      width: 24px;
      height: 24px;
      border-radius: 50%;
      background: var(--bg-tertiary);
      display: flex;
      align-items: center;
      justify-content: center;
      font-size: 11px;
      color: var(--text-muted);
    }
    .participant-name {
      font-weight: 500;
    }
    .participant-role {
      font-size: 10px;
      color: var(--accent);
      font-weight: 600;
      text-transform: uppercase;
      margin-left: 2px;
    }
    .live-event-actions {
      padding: 12px 16px;
      display: flex;
      gap: 10px;
      background: var(--bg-secondary);
    }
    .live-action-btn {
      display: inline-flex;
      align-items: center;
      justify-content: center;
      gap: 8px;
      padding: 10px 20px;
      border-radius: 8px;
      font-size: 14px;
      font-weight: 600;
      text-decoration: none;
      transition: all 0.15s ease;
      flex: 1;
    }
    .stream-btn {
      background: #dc2626;
      color: white;
    }
    .stream-btn:hover {
      background: #b91c1c;
    }
    .recording-btn {
      background: var(--bg-tertiary);
      color: var(--text-primary);
      border: 1px solid var(--border);
    }
    .recording-btn:hover {
      background: var(--bg-secondary);
    }
    /* Highlight (kind 9802) styles */
    .highlight {
      padding: 16px 20px;
    }
    .highlight-blockquote {
      border-left: 4px solid var(--accent);
      padding-left: 16px;
      margin: 0 0 12px 0;
      font-style: italic;
      font-size: 15px;
      line-height: 1.6;
      color: var(--text-content);
    }
    .highlight-context {
      margin-top: 8px;
      padding: 12px;
      background: var(--bg-secondary);
      border-radius: 6px;
      font-size: 13px;
      color: var(--text-secondary);
      font-style: normal;
      line-height: 1.5;
    }
    .highlight-comment {
      margin-top: 12px;
      font-size: 14px;
      color: var(--text-primary);
      font-style: normal;
    }
    .highlight-source {
      margin-top: 12px;
      padding-top: 12px;
      border-top: 1px solid var(--border-color);
      font-size: 13px;
    }
    .highlight-source-link {
      color: var(--accent);
      text-decoration: none;
      word-break: break-all;
    }
    .highlight-source-link:hover {
      text-decoration: underline;
    }
    .highlight-meta {
      display: flex;
      justify-content: space-between;
      align-items: center;
      margin-top: 12px;
      padding-top: 12px;
      border-top: 1px solid var(--border-color);
    }
    .highlight-author {
      display: flex;
      align-items: center;
      gap: 8px;
    }
    .highlight-author-avatar {
      width: 24px;
      height: 24px;
      border-radius: 50%;
      object-fit: cover;
    }
    .highlight-author-name {
      color: var(--text-secondary);
      text-decoration: none;
      font-size: 13px;
    }
    .highlight-author-name:hover {
      color: var(--accent);
    }
    .highlight-time {
      font-size: 12px;
      color: var(--text-muted);
    }
    /* Bookmark list (kind 10003) styles */
    .bookmarks {
      padding: 16px 20px;
    }
    .bookmarks-header {
      display: flex;
      align-items: center;
      gap: 8px;
      margin-bottom: 16px;
    }
    .bookmarks-icon {
      font-size: 18px;
    }
    .bookmarks-title {
      font-size: 16px;
      font-weight: 600;
      color: var(--text-primary);
    }
    .bookmarks-count {
      font-size: 13px;
      color: var(--text-muted);
      margin-left: auto;
    }
    .bookmarks-section {
      margin-bottom: 16px;
    }
    .bookmarks-section:last-child {
      margin-bottom: 0;
    }
    .bookmarks-section-title {
      font-size: 12px;
      font-weight: 600;
      color: var(--text-secondary);
      text-transform: uppercase;
      margin-bottom: 8px;
      letter-spacing: 0.5px;
    }
    .bookmarks-list {
      display: flex;
      flex-direction: column;
      gap: 6px;
    }
    .bookmark-item {
      display: flex;
      align-items: center;
      gap: 8px;
      padding: 8px 12px;
      background: var(--bg-secondary);
      border-radius: 6px;
      font-size: 13px;
      color: var(--text-content);
      text-decoration: none;
      transition: background 0.15s ease;
    }
    .bookmark-item:hover {
      background: var(--bg-hover);
    }
    .bookmark-item-icon {
      font-size: 14px;
      opacity: 0.7;
    }
    .bookmark-item-text {
      overflow: hidden;
      text-overflow: ellipsis;
      white-space: nowrap;
    }
    .bookmark-hashtag {
      color: var(--accent);
    }
    .bookmarks-meta {
      display: flex;
      justify-content: space-between;
      align-items: center;
      margin-top: 16px;
      padding-top: 12px;
      border-top: 1px solid var(--border-color);
    }
    .bookmarks-author {
      display: flex;
      align-items: center;
      gap: 8px;
    }
    .bookmarks-author-avatar {
      width: 24px;
      height: 24px;
      border-radius: 50%;
      object-fit: cover;
    }
    .bookmarks-author-name {
      color: var(--text-secondary);
      text-decoration: none;
      font-size: 13px;
    }
    .bookmarks-author-name:hover {
      color: var(--accent);
    }
    .bookmarks-time {
      font-size: 12px;
      color: var(--text-muted);
    }
    .live-event-player {
      padding: 0;
      border-top: 1px solid var(--border);
    }
    .live-embed-iframe {
      width: 100%;
      height: 360px;
      display: block;
    }
    .live-event .note-meta {
      padding: 12px 16px;
      border-top: 1px solid var(--border);
    }
    /* Article preview styles (kind 30023 in timeline) */
    .article-preview {
      margin: 12px 0;
    }
    .article-preview-image {
      width: 100%;
      max-height: 200px;
      object-fit: cover;
      border-radius: 8px;
      margin-bottom: 12px;
    }
    .article-preview-title {
      font-size: 18px;
      font-weight: 600;
      color: var(--text-primary);
      margin-bottom: 8px;
      line-height: 1.3;
    }
    .article-preview-summary {
      font-size: 14px;
      color: var(--text-muted);
      margin-bottom: 12px;
      line-height: 1.5;
      display: -webkit-box;
      -webkit-line-clamp: 3;
      -webkit-box-orient: vertical;
      overflow: hidden;
    }
    /* Full article styles (kind 30023 in thread view) */
    .long-form-article {
      margin: 12px 0;
    }
    .article-header-image {
      width: 100%;
      max-height: 300px;
      object-fit: cover;
      border-radius: 8px;
      margin-bottom: 16px;
    }
    .article-title {
      font-size: 24px;
      font-weight: 700;
      color: var(--text-primary);
      margin-bottom: 12px;
      line-height: 1.3;
    }
    .article-summary {
      font-size: 16px;
      color: var(--text-muted);
      margin-bottom: 12px;
      font-style: italic;
      line-height: 1.5;
    }
    .article-published {
      font-size: 13px;
      color: var(--text-muted);
      margin-bottom: 16px;
    }
    .article-content {
      font-size: 15px;
      line-height: 1.8;
      color: var(--text-primary);
    }
    .article-content h1, .article-content h2, .article-content h3 {
      margin-top: 24px;
      margin-bottom: 12px;
      color: var(--text-primary);
    }
    .article-content h1 { font-size: 22px; }
    .article-content h2 { font-size: 20px; }
    .article-content h3 { font-size: 18px; }
    .article-content p {
      margin-bottom: 16px;
    }
    .article-content ul, .article-content ol {
      margin-bottom: 16px;
      padding-left: 24px;
    }
    .article-content li {
      margin-bottom: 8px;
    }
    .article-content blockquote {
      border-left: 4px solid var(--border-color);
      padding-left: 16px;
      margin: 16px 0;
      color: var(--text-muted);
      font-style: italic;
    }
    .article-content code {
      background: var(--bg-secondary);
      padding: 2px 6px;
      border-radius: 4px;
      font-family: monospace;
      font-size: 14px;
    }
    .article-content pre {
      background: var(--bg-secondary);
      padding: 16px;
      border-radius: 8px;
      overflow-x: auto;
      margin-bottom: 16px;
    }
    .article-content pre code {
      background: none;
      padding: 0;
    }
    .article-content a {
      color: var(--accent-color);
    }
    .article-content img {
      max-width: 100%;
      border-radius: 8px;
      margin: 16px 0;
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
      color: var(--accent);
      text-decoration: none;
    }
    .note-content a:hover {
      text-decoration: underline;
    }
    .link-preview {
      display: flex;
      border: 1px solid var(--border-color);
      border-radius: 8px;
      margin: 12px 0;
      overflow: hidden;
      text-decoration: none;
      color: inherit;
      background: var(--link-preview-bg);
      transition: border-color 0.2s, box-shadow 0.2s;
    }
    .link-preview:hover {
      border-color: var(--accent);
      box-shadow: 0 2px 8px var(--shadow-accent);
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
      color: var(--text-muted);
      text-transform: uppercase;
      letter-spacing: 0.5px;
      margin-bottom: 2px;
    }
    .link-preview-title {
      font-size: 14px;
      font-weight: 600;
      color: var(--text-content);
      line-height: 1.3;
      margin-bottom: 4px;
      display: -webkit-box;
      -webkit-line-clamp: 2;
      -webkit-box-orient: vertical;
      overflow: hidden;
    }
    .link-preview-desc {
      font-size: 12px;
      color: var(--text-secondary);
      line-height: 1.4;
      display: -webkit-box;
      -webkit-line-clamp: 2;
      -webkit-box-orient: vertical;
      overflow: hidden;
    }
    .quoted-note {
      background: var(--quoted-bg);
      border: 1px solid var(--border-color);
      border-left: 3px solid var(--accent);
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
      color: var(--text-content);
    }
    .quoted-note .quoted-author-npub {
      font-family: monospace;
      font-size: 11px;
      color: var(--accent);
    }
    .quoted-note .quoted-content {
      color: var(--text-content);
      white-space: pre-wrap;
      word-wrap: break-word;
    }
    .quoted-note .quoted-meta {
      font-size: 11px;
      color: var(--text-secondary);
      margin-top: 8px;
    }
    .quoted-note .quoted-meta a {
      color: var(--accent);
    }
    .quoted-note.quoted-article .quoted-article-title {
      font-weight: 600;
      font-size: 15px;
      margin-bottom: 6px;
      color: var(--text-content);
    }
    .quoted-note.quoted-article .quoted-article-summary {
      color: var(--text-secondary);
      font-size: 14px;
      line-height: 1.4;
    }
    .quoted-note-error {
      background: var(--error-bg);
      border: 1px solid var(--error-border);
      border-left: 3px solid var(--error-accent);
      border-radius: 4px;
      padding: 8px 12px;
      margin: 8px 0;
      font-size: 13px;
      color: var(--text-secondary);
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
      background: var(--ref-event-bg);
      border: 1px solid var(--ref-event-border);
      color: var(--ref-event-color);
    }
    .nostr-ref-event:hover {
      background: var(--ref-event-hover);
    }
    .nostr-ref-profile {
      display: inline;
      padding: 0;
      margin: 0;
      border: none;
      border-radius: 0;
      background: none;
      color: var(--success);
      font-weight: 500;
    }
    .nostr-ref-profile:hover {
      background: none;
      text-decoration: underline;
    }
    .nostr-ref-addr {
      background: var(--ref-addr-bg);
      border: 1px solid var(--ref-addr-border);
      color: var(--ref-addr-color);
    }
    .nostr-ref-addr:hover {
      background: var(--ref-addr-hover);
    }
    .note-meta {
      display: flex;
      gap: 16px;
      font-size: 12px;
      color: var(--text-secondary);
      margin-top: 12px;
      padding-top: 12px;
      border-top: 1px solid var(--border-color);
      flex-wrap: wrap;
    }
    .pagination {
      display: flex;
      justify-content: center;
      gap: 12px;
      margin: 12px 0 0 0;
      padding: 12px 0;
      border-top: 1px solid var(--border-color);
    }
    .link {
      display: inline-flex;
      align-items: center;
      gap: 4px;
      padding: 6px 12px;
      background: var(--bg-card);
      border: 1px solid var(--accent);
      color: var(--accent);
      text-decoration: none;
      border-radius: 4px;
      font-size: 13px;
      transition: all 0.2s;
    }
    .link:hover {
      background: var(--accent);
      color: white;
    }
    /* Utility classes */
    .ml-auto { margin-left: auto; }
    .flex { display: flex; }
    .flex-center { display: flex; align-items: center; }
    .gap-sm { gap: 8px; }
    .gap-md { gap: 12px; }
    .text-muted { color: var(--text-secondary); text-decoration: none; }
    .text-sm { font-size: 13px; }
    .text-link { color: var(--accent); text-decoration: none; }
    .text-link:hover { text-decoration: underline; }
    button.text-link, button.repost-btn { background: none !important; border: none !important; font: inherit; cursor: pointer; padding: 0 !important; margin: 0 !important; border-radius: 0 !important; color: var(--accent) !important; }
    button.text-link:hover { text-decoration: underline; }
    .text-xs { font-size: 12px; }
    .font-medium { font-weight: 500; }
    .inline-form { display: inline; margin: 0; }
    .ghost-btn {
      background: none;
      border: none;
      color: var(--text-secondary);
      cursor: pointer;
      font-family: inherit;
      padding: 0;
    }
    .accent-btn {
      appearance: none;
      -webkit-appearance: none;
      background: none !important;
      border: none !important;
      color: var(--accent) !important;
      cursor: pointer;
      font-family: inherit;
      font-size: inherit;
      padding: 0 !important;
      margin: 0 !important;
      line-height: inherit;
      border-radius: 0 !important;
    }
    .accent-btn:hover {
      text-decoration: underline;
      background: none !important;
    }
    .note-actions {
      display: flex;
      gap: 16px;
      align-items: center;
      margin-left: auto;
    }
    .note-footer {
      display: flex;
      gap: 16px;
      flex-wrap: wrap;
      margin-top: 12px;
      padding-top: 12px;
      border-top: 1px solid var(--border-color);
      align-items: center;
      font-size: 13px;
    }
    .note-footer-actions {
      display: flex;
      gap: 16px;
      flex-wrap: wrap;
    }
    .note-footer-reactions {
      display: flex;
      gap: 8px;
      flex-wrap: wrap;
      margin-left: auto;
    }
    .note-author {
      display: flex;
      align-items: flex-start;
      gap: 10px;
      margin-bottom: 8px;
    }
    .author-avatar {
      width: 40px;
      height: 40px;
      border-radius: 50%;
      object-fit: cover;
    }
    .author-info {
      display: flex;
      flex-direction: column;
      gap: 2px;
    }
    .author-name {
      font-weight: 600;
      color: var(--text-primary);
    }
    .author-npub {
      font-family: monospace;
      font-size: 12px;
      color: var(--text-secondary);
    }
    /* Settings dropdown */
    .settings-dropdown { position: relative; }
    .settings-toggle {
      cursor: pointer;
      list-style: none;
      font-size: 16px;
    }
    .settings-menu {
      position: absolute;
      right: 0;
      top: 100%;
      margin-top: 8px;
      background: var(--bg-card);
      border: 1px solid var(--border-color);
      border-radius: 4px;
      padding: 10px 14px;
      box-shadow: 0 4px 12px var(--shadow);
      z-index: 100;
      white-space: nowrap;
      font-size: 12px;
      color: var(--text-secondary);
    }
    footer {
      text-align: center;
      padding: 20px;
      background: var(--bg-secondary);
      color: var(--text-secondary);
      font-size: 13px;
      border-top: 1px solid var(--border-light);
      border-radius: 0 0 8px 8px;
    }
		input:checked + span {
			background-color: var(--accent);
		}
		input:checked + span + span {
			transform: translateX(20px);
		}
  </style>
</head>
<body>
  <div id="top" class="container">
    <nav>
      {{if .LoggedIn}}
      <a href="/html/timeline?kinds=1&limit=20&feed=follows" class="nav-tab">Follows</a>
      {{end}}
      <a href="/html/timeline?kinds=1&limit=20&feed=global" class="nav-tab{{if not .LoggedIn}} active{{end}}">Global</a>
      {{if .LoggedIn}}
      <a href="/html/timeline?kinds=1&limit=20&feed=me" class="nav-tab">Me</a>
      {{end}}
      <div class="ml-auto flex-center gap-md">
        {{if .LoggedIn}}
        <a href="/html/notifications" class="notification-bell" title="Notifications">🔔{{if .HasUnreadNotifications}}<span class="notification-badge"></span>{{end}}</a>
        {{end}}
        <details class="settings-dropdown">
          <summary class="settings-toggle" title="Settings">⚙️</summary>
          <div class="settings-menu">
            <div class="settings-item">
              <form method="POST" action="/html/theme" class="inline-form">
                <button type="submit" class="ghost-btn text-xs">Theme: {{.ThemeLabel}}</button>
              </form>
            </div>
          </div>
        </details>
        {{if .LoggedIn}}
        <a href="/html/logout" class="text-muted text-sm">Logout</a>
        {{else}}
        <a href="/html/login" class="text-link text-sm font-medium">Login</a>
        {{end}}
      </div>
    </nav>

    <main>
      <div class="profile-header">
        {{if and .Profile .Profile.Picture}}
        <img class="profile-avatar" src="{{.Profile.Picture}}" alt="{{if .Profile.DisplayName}}{{.Profile.DisplayName}}{{else if .Profile.Name}}{{.Profile.Name}}{{else}}User{{end}}'s avatar">
        {{else}}
        <img class="profile-avatar" src="/static/avatar.jpg" alt="Default avatar">
        {{end}}
        <div class="profile-info">
          <div class="profile-name-row">
            {{if .Profile}}
            {{if or .Profile.DisplayName .Profile.Name}}
            <div class="profile-name">{{if .Profile.DisplayName}}{{.Profile.DisplayName}}{{else}}{{.Profile.Name}}{{end}}</div>
            {{else}}
            <div class="profile-name">{{.NpubShort}}</div>
            {{end}}
            {{else}}
            <div class="profile-name">{{.NpubShort}}</div>
            {{end}}
            {{if and .LoggedIn (not .IsSelf)}}
            <form method="POST" action="/html/follow" class="inline-form">
              <input type="hidden" name="csrf_token" value="{{.CSRFToken}}">
              <input type="hidden" name="pubkey" value="{{.Pubkey}}">
              <input type="hidden" name="return_url" value="{{.CurrentURL}}">
              {{if .IsFollowing}}
              <input type="hidden" name="action" value="unfollow">
              <button type="submit" class="follow-btn unfollow">Following</button>
              {{else}}
              <input type="hidden" name="action" value="follow">
              <button type="submit" class="follow-btn follow">Follow</button>
              {{end}}
            </form>
            {{end}}
            {{if and .LoggedIn .IsSelf}}
            <a href="/html/profile/edit" class="edit-profile-btn">Edit Profile</a>
            {{end}}
          </div>
          {{if and .Profile .Profile.Nip05}}
          <div class="profile-nip05">{{.Profile.Nip05}}</div>
          {{end}}
          <div class="profile-npub" title="{{.Pubkey}}">{{.NpubShort}}</div>
          {{if and .Profile .Profile.About}}
          <div class="profile-about">{{.Profile.About}}</div>
          {{end}}
        </div>
      </div>

      {{if .EditMode}}
      <div class="edit-form-section">
        <h3>Edit Profile</h3>
        {{if .Error}}
        <div class="edit-form-error">{{.Error}}</div>
        {{end}}
        {{if .Success}}
        <div class="flash-message">{{.Success}}</div>
        {{end}}
        <form method="POST" action="/html/profile/edit">
          <input type="hidden" name="csrf_token" value="{{.CSRFToken}}">
          <input type="hidden" name="raw_content" value="{{.RawContent}}">
          <div class="edit-form-group">
            <label for="display_name">Display Name</label>
            <input type="text" id="display_name" name="display_name" value="{{if .Profile}}{{.Profile.DisplayName}}{{end}}" placeholder="Your display name">
          </div>
          <div class="edit-form-group">
            <label for="name">Username</label>
            <input type="text" id="name" name="name" value="{{if .Profile}}{{.Profile.Name}}{{end}}" placeholder="username">
            <div class="edit-form-hint">Short identifier (lowercase, no spaces)</div>
          </div>
          <div class="edit-form-group">
            <label for="about">About</label>
            <textarea id="about" name="about" placeholder="Tell us about yourself">{{if .Profile}}{{.Profile.About}}{{end}}</textarea>
          </div>
          <div class="edit-form-group">
            <label for="picture">Profile Picture URL</label>
            <input type="url" id="picture" name="picture" value="{{if .Profile}}{{.Profile.Picture}}{{end}}" placeholder="https://example.com/avatar.jpg">
          </div>
          <div class="edit-form-group">
            <label for="banner">Banner Image URL</label>
            <input type="url" id="banner" name="banner" value="{{if .Profile}}{{.Profile.Banner}}{{end}}" placeholder="https://example.com/banner.jpg">
          </div>
          <div class="edit-form-group">
            <label for="nip05">NIP-05 Identifier</label>
            <input type="text" id="nip05" name="nip05" value="{{if .Profile}}{{.Profile.Nip05}}{{end}}" placeholder="you@example.com">
            <div class="edit-form-hint">Verified identifier (like user@domain.com)</div>
          </div>
          <div class="edit-form-group">
            <label for="lud16">Lightning Address</label>
            <input type="text" id="lud16" name="lud16" value="{{if .Profile}}{{.Profile.Lud16}}{{end}}" placeholder="you@getalby.com">
            <div class="edit-form-hint">For receiving zaps</div>
          </div>
          <div class="edit-form-group">
            <label for="website">Website</label>
            <input type="url" id="website" name="website" value="{{if .Profile}}{{.Profile.Website}}{{end}}" placeholder="https://yourwebsite.com">
          </div>
          <div class="edit-form-buttons">
            <button type="submit" class="edit-form-btn edit-form-btn-primary">Save Profile</button>
            <a href="/html/profile/{{.Npub}}" class="edit-form-btn edit-form-btn-secondary">Cancel</a>
          </div>
        </form>
      </div>
      {{else}}
      <div class="notes-section">
        {{range .Items}}
        <article class="note">
          <div class="note-author">
            <a href="/html/profile/{{$.Npub}}" class="text-muted">
            {{if and $.Profile $.Profile.Picture}}
            <img class="author-avatar" src="{{$.Profile.Picture}}" alt="{{if $.Profile.DisplayName}}{{$.Profile.DisplayName}}{{else if $.Profile.Name}}{{$.Profile.Name}}{{else}}User{{end}}'s avatar">
            {{end}}
            </a>
            <div class="author-info">
              <a href="/html/profile/{{$.Npub}}" class="text-muted">
              {{if and $.Profile $.Profile.Name}}
              <span class="author-name">{{$.Profile.Name}}</span>
              {{else}}
              <span class="author-npub">{{$.NpubShort}}</span>
              {{end}}
              </a>
              <span class="author-time">{{formatTime .CreatedAt}}</span>
            </div>
          </div>
          <div class="note-content">{{.ContentHTML}}</div>
          <div class="note-footer">
            <div class="note-footer-actions">
            {{if $.LoggedIn}}
              {{if ne .Kind 30023}}
              <a href="/html/thread/{{.ID}}" class="text-link">Reply{{if gt .ReplyCount 0}} {{.ReplyCount}}{{end}}</a>
              {{end}}
              <form method="POST" action="/html/repost" class="inline-form">
                <input type="hidden" name="csrf_token" value="{{$.CSRFToken}}">
                <input type="hidden" name="event_id" value="{{.ID}}">
                <input type="hidden" name="event_pubkey" value="{{.Pubkey}}">
                <input type="hidden" name="return_url" value="{{$.CurrentURL}}">
                <button type="submit" class="text-link">Repost</button>
              </form>
              <a href="/html/quote/{{.ID}}" class="text-link">Quote</a>
              <form method="POST" action="/html/react" class="inline-form">
                <input type="hidden" name="csrf_token" value="{{$.CSRFToken}}">
                <input type="hidden" name="event_id" value="{{.ID}}">
                <input type="hidden" name="event_pubkey" value="{{.Pubkey}}">
                <input type="hidden" name="return_url" value="{{$.CurrentURL}}">
                <input type="hidden" name="reaction" value="❤️">
                <button type="submit" class="text-link">Like</button>
              </form>
              <form method="POST" action="/html/bookmark" class="inline-form">
                <input type="hidden" name="csrf_token" value="{{$.CSRFToken}}">
                <input type="hidden" name="event_id" value="{{.ID}}">
                <input type="hidden" name="return_url" value="{{$.CurrentURL}}">
                {{if .IsBookmarked}}
                <input type="hidden" name="action" value="remove">
                <button type="submit" class="text-link" title="Remove bookmark">Unbookmark</button>
                {{else}}
                <input type="hidden" name="action" value="add">
                <button type="submit" class="text-link" title="Add bookmark">Bookmark</button>
                {{end}}
              </form>
            {{else}}
              {{if eq .Kind 30023}}
              <a href="/html/thread/{{.ID}}" class="text-link">Read article</a>
              {{else if gt .ReplyCount 0}}
              <a href="/html/thread/{{.ID}}" class="text-link">{{.ReplyCount}} replies</a>
              {{end}}
            {{end}}
            </div>
          </div>
        </article>
        {{end}}
        {{if not .Items}}
        <div class="empty-state">
          <div class="empty-state-icon">📝</div>
          <p>No notes yet</p>
          <p class="empty-state-hint">This user hasn't posted any notes.</p>
        </div>
        {{end}}
      </div>

      {{if .Pagination}}
      <div class="pagination">
        {{if .Pagination.Next}}
        <a href="{{.Pagination.Next}}" class="link">Load More →</a>
        {{end}}
      </div>
      {{end}}
      {{end}}
    </main>

    <footer>
      <p>{{if .Meta}}Generated: {{.Meta.GeneratedAt.Format "15:04:05"}} · {{end}}Zero-JS Hypermedia Browser</p>
    </footer>
  </div>
  <a href="#top" class="scroll-top" aria-label="Scroll to top">↑</a>
</body>
</html>
`

type HTMLProfileData struct {
	Title                  string
	Pubkey                 string
	Npub                   string
	NpubShort              string
	Profile                *ProfileInfo
	Items                  []HTMLEventItem
	Pagination             *HTMLPagination
	Meta                   *MetaInfo
	ThemeClass             string // "dark", "light", or "" for system default
	ThemeLabel             string // Label for theme toggle button
	LoggedIn               bool
	CurrentURL             string
	CSRFToken              string // CSRF token for form submission
	IsFollowing            bool   // Whether logged-in user follows this profile
	IsSelf                 bool   // Whether this is the logged-in user's own profile
	HasUnreadNotifications bool   // Whether there are notifications newer than last seen
	// Edit mode fields
	EditMode   bool   // Whether showing edit form instead of notes
	RawContent string // JSON of raw profile content (for preserving unknown fields)
	Error      string // Error message for edit form
	Success    string // Success message for edit form
}

func renderProfileHTML(resp ProfileResponse, relays []string, limit int, themeClass, themeLabel string, loggedIn bool, currentURL, csrfToken string, isFollowing, isSelf, hasUnreadNotifs bool) (string, error) {
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
		Title:                  title,
		Pubkey:                 resp.Pubkey,
		Npub:                   npub,
		NpubShort:              formatNpubShort(npub),
		Profile:                resp.Profile,
		Items:                  items,
		Pagination:             pagination,
		Meta:                   &resp.Notes.Meta,
		ThemeClass:             themeClass,
		ThemeLabel:             themeLabel,
		LoggedIn:               loggedIn,
		CurrentURL:             currentURL,
		CSRFToken:              csrfToken,
		IsFollowing:            isFollowing,
		IsSelf:                 isSelf,
		HasUnreadNotifications: hasUnreadNotifs,
	}

	// Use cached template for better performance
	var buf strings.Builder
	if err := cachedProfileTemplate.Execute(&buf, data); err != nil {
		return "", err
	}

	return buf.String(), nil
}

// HTMLNotificationItem represents a notification for HTML rendering
type HTMLNotificationItem struct {
	Event             *Event
	Type              NotificationType
	TypeLabel         string // Human-readable label: "replied", "mentioned", "reacted", "reposted"
	TypeIcon          string // Emoji icon for the notification type
	TargetEventID     string
	TargetContentHTML template.HTML // Content of the target event (for reactions/reposts to show what was reacted to)
	AuthorProfile     *ProfileInfo
	AuthorNpub        string
	AuthorNpubShort   string
	ContentHTML       template.HTML
	TimeAgo           string
}

// HTMLNotificationsData is the data passed to the notifications template
type HTMLNotificationsData struct {
	Title           string
	ThemeClass      string
	ThemeLabel      string
	UserDisplayName string
	UserPubKey      string
	Items           []HTMLNotificationItem
	GeneratedAt     time.Time
	Pagination      *HTMLPagination
}

var htmlNotificationsTemplate = `<!DOCTYPE html>
<html lang="en"{{if .ThemeClass}} class="{{.ThemeClass}}"{{end}}>
<head>
  <meta charset="UTF-8">
  <meta name="viewport" content="width=device-width, initial-scale=1.0">
  <title>{{.Title}} - Nostr Hypermedia</title>
  <link rel="icon" href="/static/favicon.ico" />
  <style>
    :root {
      --bg-page: #f5f5f5;
      --bg-container: #ffffff;
      --bg-card: #ffffff;
      --bg-secondary: #f8f9fa;
      --bg-input: #ffffff;
      --bg-badge: #f0f0f0;
      --bg-badge-hover: #e0e0e0;
      --text-primary: #333333;
      --text-secondary: #666666;
      --text-muted: #999999;
      --text-content: #24292e;
      --border-color: #e1e4e8;
      --border-light: #dee2e6;
      --accent: #667eea;
      --accent-hover: #5568d3;
      --accent-secondary: #764ba2;
      --shadow: rgba(0,0,0,0.1);
    }
    @media (prefers-color-scheme: dark) {
      :root:not(.light) {
        --bg-page: #121212;
        --bg-container: #1e1e1e;
        --bg-card: #1e1e1e;
        --bg-secondary: #252525;
        --bg-input: #2a2a2a;
        --bg-badge: #2a2a2a;
        --bg-badge-hover: #3a3a3a;
        --text-primary: #e4e4e7;
        --text-secondary: #a1a1aa;
        --text-muted: #71717a;
        --text-content: #e4e4e7;
        --border-color: #333333;
        --border-light: #333333;
        --accent: #818cf8;
        --accent-hover: #6366f1;
        --accent-secondary: #a78bfa;
        --shadow: rgba(0,0,0,0.3);
      }
    }
    html.dark {
      --bg-page: #121212;
      --bg-container: #1e1e1e;
      --bg-card: #1e1e1e;
      --bg-secondary: #252525;
      --bg-input: #2a2a2a;
      --bg-badge: #2a2a2a;
      --bg-badge-hover: #3a3a3a;
      --text-primary: #e4e4e7;
      --text-secondary: #a1a1aa;
      --text-muted: #71717a;
      --text-content: #e4e4e7;
      --border-color: #333333;
      --border-light: #333333;
      --accent: #818cf8;
      --accent-hover: #6366f1;
      --accent-secondary: #a78bfa;
      --shadow: rgba(0,0,0,0.3);
    }
    html.light {
      --bg-page: #f5f5f5;
      --bg-container: #ffffff;
      --bg-card: #ffffff;
      --bg-secondary: #f8f9fa;
      --bg-input: #ffffff;
      --bg-badge: #f0f0f0;
      --bg-badge-hover: #e0e0e0;
      --text-primary: #333333;
      --text-secondary: #666666;
      --text-muted: #999999;
      --text-content: #24292e;
      --border-color: #e1e4e8;
      --border-light: #dee2e6;
      --accent: #667eea;
      --accent-hover: #5568d3;
      --accent-secondary: #764ba2;
      --shadow: rgba(0,0,0,0.1);
    }
    * { box-sizing: border-box; margin: 0; padding: 0; }
    html { scroll-behavior: smooth; }
    body {
      font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, Oxygen, Ubuntu, Cantarell, sans-serif;
      line-height: 1.6;
      color: var(--text-primary);
      background: var(--bg-page);
      padding: 20px;
    }
    .container {
      max-width: 800px;
      margin: 0 auto;
      background: var(--bg-container);
      border-radius: 8px;
      box-shadow: 0 2px 8px var(--shadow);
    }
    .sticky-section {
      position: sticky;
      top: 0;
      z-index: 100;
      background: var(--bg-container);
    }
    nav {
      padding: 12px 15px;
      background: var(--bg-secondary);
      border-bottom: 1px solid var(--border-light);
      display: flex;
      align-items: center;
      gap: 8px;
      flex-wrap: wrap;
    }
    .nav-tab {
      padding: 8px 16px;
      background: var(--bg-badge);
      color: var(--text-secondary);
      text-decoration: none;
      border-radius: 4px;
      font-size: 14px;
      transition: background 0.2s, color 0.2s;
    }
    .nav-tab:hover { background: var(--bg-badge-hover); }
    .nav-tab.active {
      background: var(--accent);
      color: white;
    }
    .nav-tab.active:hover { background: var(--accent-hover); }
    main { padding: 12px 20px 20px 20px; min-height: 400px; }
    footer {
      text-align: center;
      padding: 20px;
      background: var(--bg-secondary);
      color: var(--text-secondary);
      font-size: 13px;
      border-top: 1px solid var(--border-color);
      border-radius: 0 0 8px 8px;
    }
    /* Settings dropdown */
    .settings-dropdown { position: relative; }
    .settings-toggle {
      cursor: pointer;
      list-style: none;
      font-size: 16px;
    }
    .settings-menu {
      position: absolute;
      right: 0;
      top: 100%;
      margin-top: 8px;
      background: var(--bg-card);
      border: 1px solid var(--border-color);
      border-radius: 4px;
      padding: 10px 14px;
      box-shadow: 0 4px 12px var(--shadow);
      z-index: 100;
      white-space: nowrap;
      font-size: 12px;
      color: var(--text-secondary);
    }
    .settings-item { margin-bottom: 8px; }
    .settings-item:last-child { margin-bottom: 0; }
    /* Notification bell */
    .notification-bell {
      position: relative;
      text-decoration: none;
      font-size: 16px;
    }
    .notification-badge {
      position: absolute;
      top: -4px;
      right: -6px;
      width: 8px;
      height: 8px;
      background: var(--accent);
      border-radius: 50%;
    }
    /* Utility classes */
    .ml-auto { margin-left: auto; }
    .flex-center { display: flex; align-items: center; }
    .gap-md { gap: 12px; }
    .text-link { color: var(--accent); text-decoration: none; }
    .text-link:hover { text-decoration: underline; }
    .text-muted { color: var(--text-secondary); text-decoration: none; }
    .text-sm { font-size: 13px; }
    .text-xs { font-size: 12px; }
    .font-medium { font-weight: 500; }
    .inline-form { display: inline; margin: 0; }
    .ghost-btn {
      background: none;
      border: none;
      color: var(--text-secondary);
      cursor: pointer;
      font-family: inherit;
      padding: 0;
    }
    /* Scroll to top button */
    .scroll-top {
      position: fixed;
      bottom: 20px;
      right: max(20px, calc((100vw - 840px) / 2 - 60px));
      width: 44px;
      height: 44px;
      background: var(--accent);
      color: white;
      border-radius: 50%;
      display: flex;
      align-items: center;
      justify-content: center;
      text-decoration: none;
      font-size: 20px;
      font-weight: bold;
      box-shadow: 0 2px 8px var(--shadow);
      opacity: 0.8;
      transition: opacity 0.2s, background 0.2s;
      z-index: 1000;
    }
    .scroll-top:hover {
      opacity: 1;
      background: var(--accent-hover);
    }
    @media (max-width: 600px) {
      .scroll-top {
        width: 40px;
        height: 40px;
        font-size: 18px;
        bottom: 16px;
        right: 16px;
      }
    }
    .pagination {
      display: flex;
      justify-content: center;
      gap: 12px;
      margin: 12px 0 0 0;
      padding: 12px 0;
      border-top: 1px solid var(--border-color);
    }
    .link {
      display: inline-flex;
      align-items: center;
      gap: 4px;
      padding: 6px 12px;
      background: var(--bg-card);
      border: 1px solid var(--accent);
      color: var(--accent);
      text-decoration: none;
      border-radius: 6px;
      font-size: 0.9rem;
      transition: background 0.2s, border-color 0.2s;
    }
    .link:hover {
      background: var(--bg-badge-hover);
    }
    /* Kind filter (submenu) */
    .kind-filter {
      display: flex;
      gap: 16px;
      padding: 6px 20px;
      font-size: 12px;
      border-bottom: 1px solid var(--border);
    }
    .kind-filter a {
      color: var(--text-muted);
      text-decoration: none;
      padding: 2px 0;
      border-bottom: 2px solid transparent;
    }
    .kind-filter a:hover {
      color: var(--text-primary);
    }
    .kind-filter a.active {
      color: var(--text-primary);
      border-bottom-color: var(--accent);
    }
    .kind-filter-spacer {
      flex-grow: 1;
    }
    .edit-profile-link {
      color: var(--text-muted);
      text-decoration: none;
      padding: 2px 8px;
      border: 1px solid var(--border);
      border-radius: 4px;
      font-size: 11px;
    }
    .edit-profile-link:hover {
      color: var(--text-primary);
      border-color: var(--text-muted);
    }
    .sr-only {
      position: absolute;
      width: 1px;
      height: 1px;
      padding: 0;
      margin: -1px;
      overflow: hidden;
      clip: rect(0, 0, 0, 0);
      white-space: nowrap;
      border: 0;
    }
    /* Post form */
    .post-form {
      padding: 12px 16px;
      background: var(--bg-secondary);
      border-bottom: 1px solid var(--border-light);
    }
    .post-form textarea {
      width: 100%;
      padding: 8px 12px;
      border: 1px solid var(--border-color);
      border-radius: 4px;
      font-size: 14px;
      font-family: inherit;
      height: 36px;
      min-height: 36px;
      resize: none;
      margin-bottom: 0;
      background: var(--bg-input);
      color: var(--text-primary);
      box-sizing: border-box;
      transition: height 0.15s ease, min-height 0.15s ease;
      overflow: hidden;
    }
    .post-form:focus-within textarea {
      height: 80px;
      min-height: 80px;
      resize: vertical;
      margin-bottom: 10px;
      overflow: auto;
    }
    .post-form button[type="submit"] {
      display: none;
      padding: 8px 16px;
      background: linear-gradient(135deg, var(--accent) 0%, var(--accent-secondary) 100%) !important;
      color: white;
      border: none;
      border-radius: 4px;
      font-size: 14px;
      font-weight: 600;
      cursor: pointer;
    }
    .post-form:focus-within button[type="submit"] {
      display: block;
    }
    /* Notification items */
    .notification-list {
      display: flex;
      flex-direction: column;
      gap: 1px;
    }
    .notification-item {
      background: var(--bg-card);
      padding: 16px;
      border: 1px solid var(--border-color);
      border-radius: 6px;
      margin-bottom: 12px;
      transition: box-shadow 0.2s;
    }
    .notification-item:hover {
      box-shadow: 0 2px 8px var(--shadow);
    }
    .notification-header {
      display: flex;
      align-items: flex-start;
      gap: 12px;
      margin-bottom: 8px;
    }
    .notification-icon {
      font-size: 1.5rem;
      flex-shrink: 0;
    }
    .notification-meta { flex: 1; }
    .notification-author {
      font-weight: 600;
      color: var(--text-primary);
      text-decoration: none;
    }
    .notification-author:hover { text-decoration: underline; }
    .notification-action { color: var(--text-secondary); }
    .notification-time {
      color: var(--text-muted);
      font-size: 0.85rem;
      margin-left: 8px;
    }
    .notification-content {
      margin-left: 44px;
      padding: 12px;
      background: var(--bg-secondary);
      border-radius: 6px;
      color: var(--text-secondary);
      font-size: 0.95rem;
      overflow: hidden;
      text-overflow: ellipsis;
    }
    .notification-content a {
      color: var(--accent);
      text-decoration: none;
    }
    .notification-content a:hover { text-decoration: underline; }
    .notification-target-content {
      margin-left: 44px;
      margin-top: 8px;
      padding: 10px 12px;
      background: var(--bg-card);
      border-left: 3px solid var(--accent);
      border-radius: 4px;
      color: var(--text-muted);
      font-size: 0.9rem;
      font-style: italic;
      word-break: break-word;
      overflow-wrap: break-word;
    }
    .notification-link {
      display: block;
      margin-left: 44px;
      margin-top: 8px;
      color: var(--accent);
      text-decoration: none;
      font-size: 0.9rem;
    }
    .notification-link:hover { text-decoration: underline; }
    .empty-state {
      text-align: center;
      padding: 60px 20px;
      color: var(--text-muted);
    }
    .empty-state-icon {
      font-size: 3rem;
      margin-bottom: 16px;
    }
    .empty-state-hint {
      margin-top: 8px;
      font-size: 0.9rem;
    }
  </style>
</head>
<body>
  <div id="top" class="container">
    <div class="sticky-section">
      <nav>
        <a href="/html/timeline?kinds=1&limit=20&feed=follows" class="nav-tab">Follows</a>
        <a href="/html/timeline?kinds=1&limit=20&feed=global" class="nav-tab">Global</a>
        <a href="/html/timeline?kinds=1&limit=20&feed=me" class="nav-tab active">Me</a>
        <div class="ml-auto flex-center gap-md">
          <a href="/html/notifications" class="notification-bell" title="Notifications">🔔</a>
          <details class="settings-dropdown">
            <summary class="settings-toggle" title="Settings">⚙️</summary>
            <div class="settings-menu">
              <div class="settings-item">
                <form method="POST" action="/html/theme" class="inline-form">
                  <button type="submit" class="ghost-btn text-xs">Theme: {{.ThemeLabel}}</button>
                </form>
              </div>
            </div>
          </details>
          <a href="/html/logout" class="text-muted text-sm">Logout</a>
        </div>
      </nav>
      <div class="kind-filter">
        <a href="/html/timeline?limit=20&feed=me">All</a>
        <a href="/html/timeline?kinds=1,6&limit=20&feed=me">Notes</a>
        <a href="/html/timeline?kinds=20&limit=20&feed=me">Photos</a>
        <a href="/html/timeline?kinds=30023&limit=20&feed=me">Longform</a>
        <a href="/html/timeline?kinds=10003&limit=20&feed=me">Bookmarks</a>
        <a href="/html/timeline?kinds=9802&limit=20&feed=me">Highlights</a>
        <a href="/html/timeline?kinds=30311&limit=20&feed=me">Livestreams</a>
        <span class="kind-filter-spacer"></span>
        <a href="/html/profile/edit" class="edit-profile-link">Edit Profile</a>
      </div>
    </div>

    <form method="POST" action="/html/post" class="post-form">
      <label for="notif-post-content" class="sr-only">Write a new note</label>
      <textarea id="notif-post-content" name="content" placeholder="What's on your mind?" required></textarea>
      <button type="submit" class="post-btn">Post</button>
    </form>

    <main>
      {{if .Items}}
      <div class="notification-list">
        {{range .Items}}
        <div class="notification-item">
          <div class="notification-header">
            <span class="notification-icon">{{.TypeIcon}}</span>
            <div class="notification-meta">
              <a href="/html/profile/{{.AuthorNpub}}" class="notification-author">
                {{if .AuthorProfile}}
                  {{if .AuthorProfile.DisplayName}}{{.AuthorProfile.DisplayName}}{{else if .AuthorProfile.Name}}{{.AuthorProfile.Name}}{{else}}{{.AuthorNpubShort}}{{end}}
                {{else}}
                  {{.AuthorNpubShort}}
                {{end}}
              </a>
              <span class="notification-action">{{.TypeLabel}}</span>
              <span class="notification-time">{{.TimeAgo}}</span>
            </div>
          </div>
          {{if .ContentHTML}}
          <div class="notification-content">{{.ContentHTML}}</div>
          {{end}}
          {{if .TargetContentHTML}}
          <div class="notification-target-content">{{.TargetContentHTML}}</div>
          {{end}}
          {{if .TargetEventID}}
          <a href="/html/thread/{{.TargetEventID}}" class="notification-link">View thread →</a>
          {{else if .Event}}
          <a href="/html/thread/{{.Event.ID}}" class="notification-link">View note →</a>
          {{end}}
        </div>
        {{end}}
      </div>
      {{else}}
      <div class="empty-state">
        <div class="empty-state-icon">🔔</div>
        <p>No notifications yet</p>
        <p class="empty-state-hint">When people mention you, reply to you, or react to your notes, you'll see it here.</p>
      </div>
      {{end}}
      {{if .Pagination}}
      <div class="pagination">
        {{if .Pagination.Next}}
        <a href="{{.Pagination.Next}}" class="link">Next →</a>
        {{end}}
      </div>
      {{end}}
    </main>

    <footer>
      <p>Generated: {{.GeneratedAt.Format "15:04:05"}} · Zero-JS Hypermedia Browser</p>
    </footer>
  </div>
  <a href="#top" class="scroll-top" title="Back to top">↑</a>
</body>
</html>`

var cachedNotificationsTemplate *template.Template

func initNotificationsTemplate() {
	var err error
	cachedNotificationsTemplate, err = template.New("notifications").Parse(htmlNotificationsTemplate)
	if err != nil {
		log.Fatalf("Failed to compile notifications template: %v", err)
	}
}

func renderNotificationsHTML(notifications []Notification, profiles map[string]*ProfileInfo, targetEvents map[string]*Event, themeClass, themeLabel, userDisplayName, userPubKey string, pagination *HTMLPagination) (string, error) {
	// Initialize template if not done
	if cachedNotificationsTemplate == nil {
		initNotificationsTemplate()
	}

	items := make([]HTMLNotificationItem, len(notifications))
	for i, notif := range notifications {
		// Get author profile
		profile := profiles[notif.Event.PubKey]
		npub, _ := encodeBech32Pubkey(notif.Event.PubKey)

		// Determine type label and icon
		var typeLabel, typeIcon string
		switch notif.Type {
		case NotificationMention:
			typeLabel = "mentioned you"
			typeIcon = "📢"
		case NotificationReply:
			typeLabel = "replied to you"
			typeIcon = "💬"
		case NotificationReaction:
			typeLabel = "reacted to your note"
			typeIcon = "❤️"
			// Use the actual reaction content as icon if it's an emoji
			if len(notif.Event.Content) > 0 && len(notif.Event.Content) < 10 {
				typeIcon = notif.Event.Content
				if typeIcon == "+" || typeIcon == "" {
					typeIcon = "❤️"
				}
			}
		case NotificationRepost:
			typeLabel = "reposted your note"
			typeIcon = "🔁"
		}

		// Truncate content for preview (skip for reactions since the emoji is shown as the icon)
		var contentHTML template.HTML
		if notif.Type != NotificationReaction {
			content := notif.Event.Content
			if len(content) > 200 {
				content = content[:200] + "..."
			}
			contentHTML = template.HTML(html.EscapeString(content))
		}

		// For reactions/reposts, show a preview of the target note content
		var targetContentHTML template.HTML
		if (notif.Type == NotificationReaction || notif.Type == NotificationRepost) && notif.TargetEventID != "" {
			if targetEvent, ok := targetEvents[notif.TargetEventID]; ok {
				targetContent := targetEvent.Content
				if len(targetContent) > 150 {
					targetContent = targetContent[:150] + "..."
				}
				targetContentHTML = template.HTML(html.EscapeString(targetContent))
			}
		}

		items[i] = HTMLNotificationItem{
			Event:             &notif.Event,
			Type:              notif.Type,
			TypeLabel:         typeLabel,
			TypeIcon:          typeIcon,
			TargetEventID:     notif.TargetEventID,
			TargetContentHTML: targetContentHTML,
			AuthorProfile:     profile,
			AuthorNpub:        npub,
			AuthorNpubShort:   formatNpubShort(npub),
			ContentHTML:       contentHTML,
			TimeAgo:           formatTimeAgo(notif.Event.CreatedAt),
		}
	}

	data := HTMLNotificationsData{
		Title:           "Notifications",
		ThemeClass:      themeClass,
		ThemeLabel:      themeLabel,
		UserDisplayName: userDisplayName,
		UserPubKey:      userPubKey,
		Items:           items,
		GeneratedAt:     time.Now(),
		Pagination:      pagination,
	}

	var buf strings.Builder
	if err := cachedNotificationsTemplate.Execute(&buf, data); err != nil {
		return "", err
	}

	return buf.String(), nil
}

func formatTimeAgo(timestamp int64) string {
	now := time.Now().Unix()
	diff := now - timestamp

	if diff < 60 {
		return "just now"
	} else if diff < 3600 {
		mins := diff / 60
		if mins == 1 {
			return "1 min ago"
		}
		return fmt.Sprintf("%d mins ago", mins)
	} else if diff < 86400 {
		hours := diff / 3600
		if hours == 1 {
			return "1 hour ago"
		}
		return fmt.Sprintf("%d hours ago", hours)
	} else if diff < 604800 {
		days := diff / 86400
		if days == 1 {
			return "1 day ago"
		}
		return fmt.Sprintf("%d days ago", days)
	} else {
		return time.Unix(timestamp, 0).Format("Jan 2")
	}
}
