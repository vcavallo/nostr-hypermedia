package kinds

// Shared partial templates used by multiple kind templates.

// PartialAuthorHeader renders the author header used by most templates.
var PartialAuthorHeader = `{{define "author-header"}}
{{if .ProfileMissing}}
{{/* Profile not available - lazy load via HelmJS */}}
<div class="note-author" id="author-{{.Pubkey}}" h-get="/fragment/author/{{.Npub}}" h-trigger="load" h-swap="outer">
  <a href="/profile/{{.Npub}}" class="text-muted" rel="author">
  <img class="author-avatar" src="/static/avatar.jpg" alt="User's avatar" width="40" height="40" loading="lazy">
  </a>
  <div class="author-info">
    <a href="/profile/{{.Npub}}" class="text-muted" rel="author">
    <span class="pubkey" title="{{.Pubkey}}">{{.NpubShort}}</span>
    </a>
    <time class="author-time" datetime="{{isoTime .CreatedAt}}">{{formatTime .CreatedAt}}</time>
  </div>
</div>
{{else}}
<div class="note-author" id="author-{{.Pubkey}}">
  <a href="/profile/{{.Npub}}" class="text-muted" rel="author">
  <img class="author-avatar" src="{{if and .AuthorProfile .AuthorProfile.Picture}}{{avatarURL .AuthorProfile.Picture}}{{else}}/static/avatar.jpg{{end}}" alt="{{displayName .AuthorProfile "User"}}'s avatar" width="40" height="40" loading="lazy">
  </a>
  <div class="author-info">
    <a href="/profile/{{.Npub}}" class="text-muted" rel="author">
    {{if and .AuthorProfile (or .AuthorProfile.DisplayName .AuthorProfile.Name)}}
    <span class="author-name">{{displayName .AuthorProfile .NpubShort}}{{if .AuthorProfile.NIP05Verified}} <a href="{{nip05URL .AuthorProfile.Nip05}}" target="_blank" rel="noopener" title="{{.AuthorProfile.NIP05Domain}}" class="nip05-verified">{{nip05Badge}}</a>{{end}}</span>
    {{else if and .AuthorProfile .AuthorProfile.Nip05}}
    <span class="author-nip05">{{.AuthorProfile.Nip05}}</span>
    {{else}}
    <span class="pubkey" title="{{.Pubkey}}">{{.NpubShort}}</span>
    {{end}}
    </a>
    <time class="author-time" datetime="{{isoTime .CreatedAt}}">{{formatTime .CreatedAt}}</time>
  </div>
</div>
{{end}}
{{end}}`

// PartialNoteFooter renders the footer (actions as pills with optional counts) for notes.
var PartialNoteFooter = `{{define "note-footer"}}
<footer class="note-footer" id="footer-{{.ID}}">
  <div class="note-actions">
  {{range .ActionGroups}}
    {{if .HasGroup}}
    {{/* Action with grouped children - render as dropdown */}}
    <details class="action-group {{.Primary.Class}}{{if .Primary.Completed}} completed{{end}}">
      <summary class="action-pill"{{if eq .Primary.IconOnly "always"}} title="{{.Primary.Title}}" aria-label="{{.Primary.Title}}"{{end}}>
        {{template "pill-content" .Primary}}
      </summary>
      <div class="action-dropdown">
        {{/* Primary action in dropdown */}}
        {{template "dropdown-action" dict "Action" .Primary "EventID" $.ID}}
        {{/* Child actions */}}
        {{range .Children}}
        {{template "dropdown-action" dict "Action" . "EventID" $.ID}}
        {{end}}
      </div>
    </details>
    {{else}}
    {{/* Single action - render as pill */}}
    {{template "action-pill" dict "Action" .Primary "EventID" $.ID}}
    {{end}}
  {{end}}
  </div>
</footer>
{{end}}

{{/* pill-content renders the inner content of a pill (icon, text, count, spinner)
     iconOnly values: "always" (icon only), "never" (text only), "mobile", "desktop", "" (icon+text)
     Spinner shows during h-loading state in place of count */}}
{{define "pill-content"}}
{{if eq .IconOnly "always"}}{{.Icon}}{{if and .HasCount (gt .Count 0)}} <span class="pill-count">{{.Count}}</span>{{end}}<span class="pill-spinner h-spinner"></span>{{else if eq .IconOnly "never"}}{{.Title}}{{if and .HasCount (gt .Count 0)}} <span class="pill-count">{{.Count}}</span>{{end}}<span class="pill-spinner h-spinner"></span>{{else if eq .IconOnly "mobile"}}<span class="icon-mobile-only">{{.Icon}}{{if and .HasCount (gt .Count 0)}} <span class="pill-count">{{.Count}}</span>{{end}}<span class="pill-spinner h-spinner"></span></span><span class="icon-desktop-only">{{.Title}}{{if and .HasCount (gt .Count 0)}} <span class="pill-count">{{.Count}}</span>{{end}}<span class="pill-spinner h-spinner"></span></span>{{else if eq .IconOnly "desktop"}}<span class="icon-desktop-only">{{.Icon}}{{if and .HasCount (gt .Count 0)}} <span class="pill-count">{{.Count}}</span>{{end}}<span class="pill-spinner h-spinner"></span></span><span class="icon-mobile-only">{{.Title}}{{if and .HasCount (gt .Count 0)}} <span class="pill-count">{{.Count}}</span>{{end}}<span class="pill-spinner h-spinner"></span></span>{{else}}{{if .Icon}}{{.Icon}} {{end}}{{.Title}}{{if and .HasCount (gt .Count 0)}} <span class="pill-count">{{.Count}}</span>{{end}}<span class="pill-spinner h-spinner"></span>{{end}}
{{end}}

{{/* action-pill renders a standalone action pill (GET link or POST form) */}}
{{define "action-pill"}}
{{$a := .Action}}{{$eid := .EventID}}
{{if eq $a.Method "GET"}}
<a href="{{$a.Href}}" h-get h-target="#page-content" h-swap="inner" h-push-url h-scroll="top" h-prefetch class="action-pill {{$a.Class}}{{if $a.Completed}} completed{{end}}"{{if $a.Rel}} rel="{{$a.Rel}}"{{end}}{{if eq $a.IconOnly "always"}} title="{{$a.Title}}" aria-label="{{$a.Title}}"{{end}}>{{template "pill-content" $a}}</a>
{{else if eq $a.Name "mute"}}
<form method="POST" action="{{$a.Href}}" class="action-pill-form" h-post h-target="#note-{{$eid}}" h-swap="delete" h-confirm="{{i18n "confirm.mute"}}">
  <input type="hidden" name="csrf_token" value="{{$a.CSRFToken}}">
  {{range $a.Fields}}<input type="hidden" name="{{.Name}}" value="{{.Value}}">{{end}}
  <button type="submit" class="action-pill {{$a.Class}}{{if $a.Completed}} completed{{end}}"{{if eq $a.IconOnly "always"}} title="{{$a.Title}}" aria-label="{{$a.Title}}"{{end}}>{{template "pill-content" $a}}</button>
</form>
{{else if gt (len $a.Amounts) 0}}
{{/* Zap action with amount dropdown */}}
<details class="action-group {{$a.Class}}{{if $a.Completed}} completed{{end}}">
  <summary class="action-pill"{{if eq $a.IconOnly "always"}} title="{{$a.Title}}" aria-label="{{$a.Title}}"{{end}}>
    {{template "pill-content" $a}}
  </summary>
  <div class="action-dropdown zap-amounts">
    {{range $i, $amt := $a.Amounts}}
    <form method="POST" action="{{$a.Href}}" class="dropdown-form" h-post h-target="#footer-{{$eid}}" h-swap="outer" h-indicator="#zap-spinner-{{$eid}}-{{$i}}">
      <input type="hidden" name="csrf_token" value="{{$a.CSRFToken}}">
      {{range $a.Fields}}{{if ne .Name "amount"}}<input type="hidden" name="{{.Name}}" value="{{.Value}}">{{end}}{{end}}
      <input type="hidden" name="amount" value="{{$amt}}">
      <button type="submit" class="dropdown-action {{$a.Class}}">{{$amt}} sats <span id="zap-spinner-{{$eid}}-{{$i}}" class="h-indicator"><span class="h-spinner"></span></span></button>
    </form>
    {{end}}
  </div>
</details>
{{else}}
<form method="POST" action="{{$a.Href}}" class="action-pill-form" h-post h-target="#footer-{{$eid}}" h-swap="outer">
  <input type="hidden" name="csrf_token" value="{{$a.CSRFToken}}">
  {{range $a.Fields}}<input type="hidden" name="{{.Name}}" value="{{.Value}}">{{end}}
  <button type="submit" class="action-pill {{$a.Class}}{{if $a.Completed}} completed{{end}}"{{if eq $a.IconOnly "always"}} title="{{$a.Title}}" aria-label="{{$a.Title}}"{{end}}>{{template "pill-content" $a}}</button>
</form>
{{end}}
{{end}}

{{/* dropdown-action renders an action inside a dropdown menu (text only, no icons) */}}
{{define "dropdown-action"}}
{{$a := .Action}}{{$eid := .EventID}}
{{if eq $a.Method "GET"}}
<a href="{{$a.Href}}" h-get h-target="#page-content" h-swap="inner" h-push-url h-scroll="top" h-prefetch class="dropdown-action {{$a.Class}}"{{if $a.Rel}} rel="{{$a.Rel}}"{{end}}>{{$a.Title}}</a>
{{else if eq $a.Name "mute"}}
<form method="POST" action="{{$a.Href}}" class="dropdown-form" h-post h-target="#note-{{$eid}}" h-swap="delete" h-confirm="{{i18n "confirm.mute"}}">
  <input type="hidden" name="csrf_token" value="{{$a.CSRFToken}}">
  {{range $a.Fields}}<input type="hidden" name="{{.Name}}" value="{{.Value}}">{{end}}
  <button type="submit" class="dropdown-action {{$a.Class}}">{{$a.Title}}</button>
</form>
{{else}}
<form method="POST" action="{{$a.Href}}" class="dropdown-form" h-post h-target="#footer-{{$eid}}" h-swap="outer">
  <input type="hidden" name="csrf_token" value="{{$a.CSRFToken}}">
  {{range $a.Fields}}<input type="hidden" name="{{.Name}}" value="{{.Value}}">{{end}}
  <button type="submit" class="dropdown-action {{$a.Class}}">{{$a.Title}}</button>
</form>
{{end}}
{{end}}`

// PartialQuotedNote renders a quoted note within a note.
var PartialQuotedNote = `{{define "quoted-note"}}
{{if .QuotedEvent}}
<div class="quoted-note">
  <div class="quoted-author">
    <img src="{{if and .QuotedEvent.AuthorProfile .QuotedEvent.AuthorProfile.Picture}}{{avatarURL .QuotedEvent.AuthorProfile.Picture}}{{else}}/static/avatar.jpg{{end}}" alt="{{displayName .QuotedEvent.AuthorProfile "User"}}'s avatar" width="24" height="24" loading="lazy">
    <span class="quoted-author-name">{{displayName .QuotedEvent.AuthorProfile .QuotedEvent.NpubShort}}</span>
  </div>
  {{if .QuotedEvent.HasContentWarning}}<details class="content-warning">
    <summary class="content-warning-label">{{if .QuotedEvent.ContentWarning}}{{.QuotedEvent.ContentWarning}}{{else}}{{i18n "label.sensitive_content"}}{{end}}</summary>
    <div class="content-warning-body">{{end}}
  {{if eq .QuotedEvent.TemplateName "longform"}}
  <div class="quoted-article-title">{{if .QuotedEvent.Title}}{{.QuotedEvent.Title}}{{else}}{{i18n "msg.untitled"}} Article{{end}}</div>
  {{if .QuotedEvent.Summary}}<div class="quoted-article-summary">{{.QuotedEvent.Summary}}</div>{{end}}
  <a href="/thread/{{eventLink .QuotedEvent.ID .QuotedEvent.Kind .QuotedEvent.Pubkey .QuotedEvent.DTag}}" h-get h-target="#page-content" h-swap="inner" h-push-url h-scroll="top" h-prefetch class="view-note-link" rel="related">{{i18n "nav.read_article"}} &rarr;</a>
  {{else}}
  <div class="note-content">{{.QuotedEvent.ContentHTML}}</div>
  <a href="/thread/{{eventLink .QuotedEvent.ID .QuotedEvent.Kind .QuotedEvent.Pubkey .QuotedEvent.DTag}}" h-get h-target="#page-content" h-swap="inner" h-push-url h-scroll="top" h-prefetch class="view-note-link" rel="related">{{i18n "nav.view_quoted_note"}} &rarr;</a>
  {{end}}
  {{if .QuotedEvent.HasContentWarning}}</div>
  </details>{{end}}
</div>
{{else if .QuotedEventID}}
<div class="quoted-note quoted-note-fallback">
  <a href="/thread/{{noteLink .QuotedEventID}}" h-get h-target="#page-content" h-swap="inner" h-push-url h-scroll="top" h-prefetch class="view-note-link" rel="related">{{i18n "nav.view_quoted_note"}} &rarr;</a>
</div>
{{end}}
{{end}}`

// PartialPagination renders the load more pagination block.
var PartialPagination = `{{define "pagination"}}
<div class="pagination" id="pagination">
{{if and .Pagination .Pagination.Next}}
  <a href="{{.Pagination.Next}}&append=1" h-get h-target="#pagination" h-swap="outer" h-push-url h-trigger="intersect once" h-prefetch="intersect 30s" h-disabled class="link load-more-btn" rel="next"><span class="load-more-text">{{i18n "nav.load_more"}} →</span><span class="load-more-loading"><span class="h-spinner"></span> {{i18n "status.loading"}}...</span></a>
{{end}}
</div>
{{end}}`

// PartialFlashMessages renders error and success flash messages.
// Wrapper div with id="flash-messages" enables OOB updates from HelmJS responses.
var PartialFlashMessages = `{{define "flash-messages"}}<div id="flash-messages">
{{if .Error}}<div class="error-box" role="alert">{{.Error}}</div>{{end}}
{{if .Success}}<div class="flash-message" role="status" aria-live="polite">{{.Success}}</div>{{end}}
</div>{{end}}`

// PartialContentWarning wraps content in a collapsible warning for NIP-36 sensitive content.
// Uses skeleton placeholder approach - shows fake "redacted" lines until clicked.
// Usage: {{template "content-warning-start" .}} ...content... {{template "content-warning-end" .}}
var PartialContentWarning = `{{define "content-warning-start"}}
{{if .HasContentWarning}}<details class="content-warning">
  <summary class="content-warning-summary">
    <div class="content-warning-placeholder" aria-hidden="true">
      <div class="cw-placeholder-line"></div>
      <div class="cw-placeholder-line"></div>
      <div class="cw-placeholder-line"></div>
    </div>
    <span class="content-warning-label">{{if .ContentWarning}}{{.ContentWarning}}{{else}}{{i18n "label.sensitive_content"}}{{end}}</span>
  </summary>
  <div class="content-warning-body">{{end}}
{{end}}

{{define "content-warning-end"}}
{{if .HasContentWarning}}</div>
</details>{{end}}
{{end}}`

// GetPartials returns all partial templates concatenated.
func GetPartials() string {
	return PartialContentWarning +
		PartialFlashMessages +
		PartialPagination +
		PartialAuthorHeader +
		PartialNoteFooter +
		PartialQuotedNote
}
