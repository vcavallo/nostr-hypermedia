package templates

// Kind-specific templates for rendering different Nostr event kinds.
// Each template is named "render-{name}" and receives an HTMLEventItem.
// The dispatcher uses {{template .RenderTemplate .}} for dynamic dispatch.

// renderTemplateNote renders kind 1 (note) - the default for most text content
var renderTemplateNote = `{{define "render-note"}}
<article class="note" id="note-{{.ID}}"{{if .IsScrollTarget}} data-scroll-target{{end}} aria-label="Note by {{displayName .AuthorProfile .NpubShort}}">
  {{template "author-header" .}}
  <div class="note-content">{{.ContentHTML}}</div>
  {{template "quoted-note" .}}
  {{template "note-footer" .}}
</article>
{{end}}`

// renderTemplateRepost renders kind 6 (repost)
var renderTemplateRepost = `{{define "render-repost"}}
<article class="note" id="note-{{.ID}}"{{if .IsScrollTarget}} data-scroll-target{{end}} aria-label="Repost by {{displayName .AuthorProfile .NpubShort}}">
  {{template "author-header" .}}
  {{if .RepostedEvent}}
  <div class="repost-indicator">{{i18n "label.reposted"}}</div>
  <div class="reposted-note">
    <div class="note-author">
      <span class="text-muted">
      <img class="author-avatar" src="{{if and .RepostedEvent.AuthorProfile .RepostedEvent.AuthorProfile.Picture}}{{avatarURL .RepostedEvent.AuthorProfile.Picture}}{{else}}/static/avatar.jpg{{end}}" alt="{{displayName .RepostedEvent.AuthorProfile "User"}}'s avatar" loading="lazy">
      </span>
      <div class="author-info">
        <span class="text-muted">
        {{if and .RepostedEvent.AuthorProfile (or .RepostedEvent.AuthorProfile.DisplayName .RepostedEvent.AuthorProfile.Name)}}
        <span class="author-name">{{displayName .RepostedEvent.AuthorProfile .RepostedEvent.NpubShort}}</span>
        {{if .RepostedEvent.AuthorProfile.Nip05}}<span class="author-nip05">{{.RepostedEvent.AuthorProfile.Nip05}}</span>{{end}}
        {{else if and .RepostedEvent.AuthorProfile .RepostedEvent.AuthorProfile.Nip05}}
        <span class="author-nip05">{{.RepostedEvent.AuthorProfile.Nip05}}</span>
        {{else}}
        <span class="pubkey" title="{{.RepostedEvent.Pubkey}}">{{.RepostedEvent.NpubShort}}</span>
        {{end}}
        </span>
      </div>
    </div>
    {{if eq .RepostedEvent.TemplateName "picture"}}
    <div class="picture-note">
      {{if .RepostedEvent.Title}}<div class="picture-title">{{.RepostedEvent.Title}}</div>{{end}}
      <div class="picture-gallery">{{.RepostedEvent.ImagesHTML}}</div>
      {{if .RepostedEvent.Content}}<div class="picture-caption">{{.RepostedEvent.ContentHTML}}</div>{{end}}
    </div>
    {{else}}
    <div class="note-content">{{.RepostedEvent.ContentHTML}}</div>
    {{end}}
    <a href="/html/thread/{{.RepostedEvent.ID}}" class="view-note-link" rel="related">{{i18n "nav.view_note"}} &rarr;</a>
  </div>
  {{else}}
  <div class="note-content repost-empty">{{i18n "msg.repost_not_available"}}</div>
  {{end}}
  {{template "note-footer" .}}
</article>
{{end}}`

// renderTemplatePicture renders kind 20 (picture)
var renderTemplatePicture = `{{define "render-picture"}}
<article class="note" id="note-{{.ID}}"{{if .IsScrollTarget}} data-scroll-target{{end}} aria-label="Photo by {{displayName .AuthorProfile .NpubShort}}">
  {{template "author-header" .}}
  <div class="picture-note">
    {{if .Title}}<div class="picture-title">{{.Title}}</div>{{end}}
    <div class="picture-gallery">{{.ImagesHTML}}</div>
    {{if .Content}}<div class="picture-caption">{{.ContentHTML}}</div>{{end}}
  </div>
  {{template "note-footer" .}}
</article>
{{end}}`

// renderTemplateShortvideo renders kind 22 (short-form vertical video - NIP-71)
var renderTemplateShortvideo = `{{define "render-shortvideo"}}
<article class="note" id="note-{{.ID}}"{{if .IsScrollTarget}} data-scroll-target{{end}} aria-label="Video by {{displayName .AuthorProfile .NpubShort}}">
  {{template "author-header" .}}
  <div class="shortvideo-note">
    {{if .VideoTitle}}<div class="shortvideo-title">{{.VideoTitle}}</div>{{end}}
    <div class="shortvideo-player-container">
      {{if .VideoURL}}
      <video class="shortvideo-player" controls playsinline preload="metadata"{{if .VideoThumbnail}} poster="{{.VideoThumbnail}}"{{end}}>
        <source src="{{.VideoURL}}"{{if .VideoMimeType}} type="{{.VideoMimeType}}"{{end}}>
        {{i18n "msg.browser_not_supported"}} video playback.
      </video>
      {{else if .VideoThumbnail}}
      <img src="{{.VideoThumbnail}}" alt="Video thumbnail" class="shortvideo-thumbnail" loading="lazy">
      {{else}}
      <div class="shortvideo-placeholder">
        <span>▶ {{i18n "msg.video_not_available"}}</span>
      </div>
      {{end}}
    </div>
    {{if .Content}}<div class="shortvideo-caption">{{.ContentHTML}}</div>{{end}}
  </div>
  {{template "note-footer" .}}
</article>
{{end}}`

// renderTemplateLongform renders kind 30023 (article)
var renderTemplateLongform = `{{define "render-longform"}}
<article class="note" id="note-{{.ID}}"{{if .IsScrollTarget}} data-scroll-target{{end}} aria-label="Article by {{displayName .AuthorProfile .NpubShort}}">
  {{template "author-header" .}}
  <div class="article-preview">
    {{if .HeaderImage}}<img src="{{.HeaderImage}}" alt="Article header image" class="article-preview-image" loading="lazy">{{end}}
    {{if .Title}}<h3 class="article-preview-title">{{.Title}}</h3>{{end}}
    {{if .Summary}}<p class="article-preview-summary">{{.Summary}}</p>{{end}}
  </div>
  {{template "note-footer" .}}
</article>
{{end}}`

// renderTemplateZap renders kind 9735 (zap receipt)
var renderTemplateZap = `{{define "render-zap"}}
<article class="note zap-receipt" id="note-{{.ID}}">
  <div class="zap-content">
    <span class="zap-icon">⚡</span>
    <div class="zap-info">
      <div class="zap-header">
        <a href="/html/profile/{{.ZapSenderNpub}}" class="zap-sender" rel="author">{{displayName .ZapSenderProfile .ZapSenderNpubShort}}</a>
        <span class="zap-action">{{i18n "label.zapped"}}</span>
        <a href="/html/profile/{{.ZapRecipientNpub}}" class="zap-recipient" rel="author">{{displayName .ZapRecipientProfile .ZapRecipientNpubShort}}</a>
      </div>
      <div class="zap-amount">{{.ZapAmountSats}} sats</div>
      {{if .ZapComment}}<div class="zap-comment">{{.ZapComment}}</div>{{end}}
      {{if .ZappedEventID}}<div class="zap-target"><a href="/html/thread/{{.ZappedEventID}}" class="text-link" rel="related">{{i18n "nav.view_zapped_note"}}</a></div>{{end}}
    </div>
  </div>
  <div class="note-meta">
    <time datetime="{{isoTime .CreatedAt}}">{{formatTime .CreatedAt}}</time>
  </div>
</article>
{{end}}`

// renderTemplateLivestream renders kind 30311 (live event)
var renderTemplateLivestream = `{{define "render-livestream"}}
<article class="note live-event" id="note-{{.ID}}">
  <div class="live-event-thumbnail">
    {{if .LiveImage}}
    <img src="{{.LiveImage}}" alt="{{.LiveTitle}}" loading="lazy">
    {{else}}
    <div class="live-event-thumbnail-placeholder"><span>{{i18n "status.live"}}</span></div>
    {{end}}
    <div class="live-event-overlay">
      {{if eq .LiveStatus "live"}}
      <span class="live-badge live">{{i18n "status.live"}}</span>
      {{else if eq .LiveStatus "planned"}}
      <span class="live-badge planned">{{i18n "status.scheduled"}}</span>
      {{else if eq .LiveStatus "ended"}}
      <span class="live-badge ended">{{i18n "status.ended"}}</span>
      {{else}}
      <span class="live-badge">{{.LiveStatus}}</span>
      {{end}}
      {{if .LiveCurrentCount}}<span class="live-viewers">{{.LiveCurrentCount}} {{i18n "status.watching"}}</span>{{end}}
    </div>
  </div>

  <div class="live-event-body">
    <h3 class="live-event-title">{{if .LiveTitle}}{{.LiveTitle}}{{else}}{{i18n "msg.live_event"}}{{end}}</h3>
    {{if .LiveSummary}}<p class="live-event-summary">{{.LiveSummary}}</p>{{end}}

    {{if .LiveParticipants}}
    <div class="live-event-host">
      {{range .LiveParticipants}}{{if eq .Role "host"}}
      <span class="host-label">{{i18n "label.host"}}</span>
      <a href="/html/profile/{{.Npub}}" class="host-link" title="{{.NpubShort}}" rel="author">
        {{if and .Profile .Profile.Picture}}<img class="host-avatar" src="{{avatarURL .Profile.Picture}}" alt="{{displayName .Profile "Host"}}'s avatar" loading="lazy">{{end}}
        <span class="host-name">{{displayName .Profile .NpubShort}}</span>
      </a>
      {{end}}{{end}}
    </div>
    {{end}}

    <div class="live-event-meta">
      {{if .LiveStarts}}
      <span class="live-event-meta-item">{{if eq .LiveStatus "ended"}}{{i18n "time.started"}}{{else if eq .LiveStatus "live"}}{{i18n "time.started"}}{{else}}{{i18n "time.starts"}}{{end}} {{formatTime .LiveStarts}}</span>
      {{end}}
      {{if and .LiveEnds (eq .LiveStatus "ended")}}
      <span class="live-event-meta-item">{{i18n "time.ended"}} {{formatTime .LiveEnds}}</span>
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
    <a href="{{.LiveEmbedURL}}" class="live-action-btn stream-btn" target="_blank" rel="external noopener">{{i18n "nav.watch_zap_stream"}}</a>
    {{else if and .LiveStreamingURL (ne .LiveStatus "ended")}}
    <a href="{{.LiveStreamingURL}}" class="live-action-btn stream-btn" target="_blank" rel="external noopener">{{i18n "nav.watch_stream"}}</a>
    {{end}}
    {{if and .LiveRecordingURL (eq .LiveStatus "ended")}}
    <a href="{{.LiveRecordingURL}}" class="live-action-btn recording-btn" target="_blank" rel="external noopener">{{i18n "nav.watch_recording"}}</a>
    {{end}}
  </div>
</article>
{{end}}`

// renderTemplateBookmarks renders kind 10003 (bookmark list)
var renderTemplateBookmarks = `{{define "render-bookmarks"}}
<article class="note bookmarks" id="note-{{.ID}}">
  <div class="bookmarks-header">
    <span class="bookmarks-icon">🔖</span>
    <span class="bookmarks-title">{{i18n "label.bookmarks"}}</span>
    <span class="bookmarks-count">{{.BookmarkCount}} items</span>
  </div>
  {{if .BookmarkEventIDs}}
  <div class="bookmarks-section">
    <div class="bookmarks-section-title">{{i18n "label.events"}}</div>
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
    <div class="bookmarks-section-title">{{i18n "label.articles"}}</div>
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
    <div class="bookmarks-section-title">{{i18n "label.hashtags"}}</div>
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
    <div class="bookmarks-section-title">{{i18n "label.links"}}</div>
    <div class="bookmarks-list">
      {{range .BookmarkURLs}}
      <a href="{{.}}" class="bookmark-item" target="_blank" rel="external noopener">
        <span class="bookmark-item-icon">🔗</span>
        <span class="bookmark-item-text">{{.}}</span>
      </a>
      {{end}}
    </div>
  </div>
  {{end}}
  <div class="bookmarks-meta">
    <div class="bookmarks-author">
      <a href="/html/profile/{{.Npub}}" rel="author">
        <img class="bookmarks-author-avatar" src="{{if and .AuthorProfile .AuthorProfile.Picture}}{{avatarURL .AuthorProfile.Picture}}{{else}}/static/avatar.jpg{{end}}" alt="{{displayName .AuthorProfile "User"}}'s avatar" loading="lazy">
      </a>
      <a href="/html/profile/{{.Npub}}" class="bookmarks-author-name" rel="author">{{displayName .AuthorProfile .NpubShort}}</a>
    </div>
    <time class="bookmarks-time" datetime="{{isoTime .CreatedAt}}">{{formatTime .CreatedAt}}</time>
  </div>
</article>
{{end}}`

// renderTemplateHighlight renders kind 9802 (highlight)
var renderTemplateHighlight = `{{define "render-highlight"}}
<article class="note highlight" id="note-{{.ID}}">
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
    <a href="{{.HighlightSourceURL}}" class="highlight-source-link" target="_blank" rel="external noopener">{{.HighlightSourceURL}}</a>
  </div>
  {{end}}
  <div class="highlight-meta">
    <div class="highlight-author">
      <a href="/html/profile/{{.Npub}}" rel="author">
        <img class="highlight-author-avatar" src="{{if and .AuthorProfile .AuthorProfile.Picture}}{{avatarURL .AuthorProfile.Picture}}{{else}}/static/avatar.jpg{{end}}" alt="{{displayName .AuthorProfile "User"}}'s avatar" loading="lazy">
      </a>
      <a href="/html/profile/{{.Npub}}" class="highlight-author-name" rel="author">{{displayName .AuthorProfile .NpubShort}}</a>
    </div>
    <time class="highlight-time" datetime="{{isoTime .CreatedAt}}">{{formatTime .CreatedAt}}</time>
  </div>
</article>
{{end}}`

// renderTemplateClassified renders kind 30402 (classified listing - NIP-99)
var renderTemplateClassified = `{{define "render-classified"}}
<article class="note classified-listing{{if eq .ClassifiedStatus "sold"}} classified-sold{{end}}" id="note-{{.ID}}">
  {{if .ClassifiedImages}}
  <div class="classified-images">
    {{range .ClassifiedImages}}
    <img src="{{.}}" alt="Listing image" class="classified-image" loading="lazy">
    {{end}}
  </div>
  {{end}}
  <div class="classified-body">
    <div class="classified-header">
      <h3 class="classified-title">{{if .Title}}{{.Title}}{{else}}{{i18n "msg.untitled"}} Listing{{end}}</h3>
      {{if .ClassifiedPrice}}
      <div class="classified-price">{{.ClassifiedPrice}}</div>
      {{end}}
    </div>
    {{if .ClassifiedStatus}}
    <div class="classified-status classified-status-{{.ClassifiedStatus}}">{{.ClassifiedStatus}}</div>
    {{end}}
    {{if .Summary}}
    <p class="classified-summary">{{.Summary}}</p>
    {{end}}
    {{if .Content}}
    <div class="classified-description">{{.ContentHTML}}</div>
    {{end}}
    {{if .ClassifiedLocation}}
    <div class="classified-location">
      <span class="classified-location-icon">📍</span>
      <span>{{.ClassifiedLocation}}</span>
    </div>
    {{end}}
    <div class="classified-meta">
      <div class="classified-author">
        <a href="/html/profile/{{.Npub}}" rel="author">
          <img class="classified-author-avatar" src="{{if and .AuthorProfile .AuthorProfile.Picture}}{{avatarURL .AuthorProfile.Picture}}{{else}}/static/avatar.jpg{{end}}" alt="{{displayName .AuthorProfile "Seller"}}'s avatar" loading="lazy">
        </a>
        <a href="/html/profile/{{.Npub}}" class="classified-author-name" rel="author">{{displayName .AuthorProfile .NpubShort}}</a>
      </div>
      <time class="classified-time" datetime="{{isoTime .CreatedAt}}">{{formatTime .CreatedAt}}</time>
    </div>
  </div>
  {{template "note-footer" .}}
</article>
{{end}}`

// renderTemplateDefault is the fallback for unknown kinds
var renderTemplateDefault = `{{define "render-default"}}
<article class="note" id="note-{{.ID}}"{{if .IsScrollTarget}} data-scroll-target{{end}} aria-label="Event by {{displayName .AuthorProfile .NpubShort}}">
  {{template "author-header" .}}
  <div class="note-content">{{.ContentHTML}}</div>
  {{template "note-footer" .}}
</article>
{{end}}`

// Shared partial templates

// partialAuthorHeader renders the author header used by most templates
var partialAuthorHeader = `{{define "author-header"}}
<div class="note-author">
  <a href="/html/profile/{{.Npub}}" class="text-muted" rel="author">
  <img class="author-avatar" src="{{if and .AuthorProfile .AuthorProfile.Picture}}{{avatarURL .AuthorProfile.Picture}}{{else}}/static/avatar.jpg{{end}}" alt="{{displayName .AuthorProfile "User"}}'s avatar" loading="lazy">
  </a>
  <div class="author-info">
    <a href="/html/profile/{{.Npub}}" class="text-muted" rel="author">
    {{if and .AuthorProfile (or .AuthorProfile.DisplayName .AuthorProfile.Name)}}
    <span class="author-name">{{displayName .AuthorProfile .NpubShort}}</span>
    {{if .AuthorProfile.Nip05}}<span class="author-nip05">{{.AuthorProfile.Nip05}}</span>{{end}}
    {{else if and .AuthorProfile .AuthorProfile.Nip05}}
    <span class="author-nip05">{{.AuthorProfile.Nip05}}</span>
    {{else}}
    <span class="pubkey" title="{{.Pubkey}}">{{.NpubShort}}</span>
    {{end}}
    </a>
    <time class="author-time" datetime="{{isoTime .CreatedAt}}">{{formatTime .CreatedAt}}</time>
  </div>
</div>
{{end}}`

// partialNoteFooter renders the footer (actions as pills with optional counts) for notes
var partialNoteFooter = `{{define "note-footer"}}
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
{{if $a.Disabled}}
<span class="action-pill {{$a.Class}} disabled"{{if eq $a.IconOnly "always"}} title="{{$a.Title}}" aria-label="{{$a.Title}}"{{end}}>{{template "pill-content" $a}}</span>
{{else if eq $a.Method "GET"}}
<a href="{{$a.Href}}" class="action-pill {{$a.Class}}{{if $a.Completed}} completed{{end}}"{{if $a.Rel}} rel="{{$a.Rel}}"{{end}}{{if eq $a.IconOnly "always"}} title="{{$a.Title}}" aria-label="{{$a.Title}}"{{end}}>{{template "pill-content" $a}}</a>
{{else if eq $a.Name "mute"}}
<form method="POST" action="{{$a.Href}}" class="action-pill-form" h-post h-target="#note-{{$eid}}" h-swap="delete" h-confirm="{{i18n "confirm.mute"}}">
  <input type="hidden" name="csrf_token" value="{{$a.CSRFToken}}">
  {{range $a.Fields}}<input type="hidden" name="{{.Name}}" value="{{.Value}}">{{end}}
  <button type="submit" class="action-pill {{$a.Class}}{{if $a.Completed}} completed{{end}}"{{if eq $a.IconOnly "always"}} title="{{$a.Title}}" aria-label="{{$a.Title}}"{{end}}>{{template "pill-content" $a}}</button>
</form>
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
<a href="{{$a.Href}}" class="dropdown-action {{$a.Class}}"{{if $a.Rel}} rel="{{$a.Rel}}"{{end}}>{{$a.Title}}</a>
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

// partialQuotedNote renders a quoted note within a note
var partialQuotedNote = `{{define "quoted-note"}}
{{if .QuotedEvent}}
<div class="quoted-note">
  <div class="quoted-author">
    <img src="{{if and .QuotedEvent.AuthorProfile .QuotedEvent.AuthorProfile.Picture}}{{avatarURL .QuotedEvent.AuthorProfile.Picture}}{{else}}/static/avatar.jpg{{end}}" alt="{{displayName .QuotedEvent.AuthorProfile "User"}}'s avatar" loading="lazy">
    <span class="quoted-author-name">{{displayName .QuotedEvent.AuthorProfile .QuotedEvent.NpubShort}}</span>
  </div>
  {{if eq .QuotedEvent.TemplateName "longform"}}
  <div class="quoted-article-title">{{if .QuotedEvent.Title}}{{.QuotedEvent.Title}}{{else}}{{i18n "msg.untitled"}} Article{{end}}</div>
  {{if .QuotedEvent.Summary}}<div class="quoted-article-summary">{{.QuotedEvent.Summary}}</div>{{end}}
  <a href="/html/thread/{{.QuotedEvent.ID}}" class="view-note-link" rel="related">{{i18n "nav.read_article"}} &rarr;</a>
  {{else}}
  <div class="note-content">{{.QuotedEvent.ContentHTML}}</div>
  <a href="/html/thread/{{.QuotedEvent.ID}}" class="view-note-link" rel="related">{{i18n "nav.view_quoted_note"}} &rarr;</a>
  {{end}}
</div>
{{else if .QuotedEventID}}
<div class="quoted-note quoted-note-fallback">
  <a href="/html/thread/{{.QuotedEventID}}" class="view-note-link" rel="related">{{i18n "nav.view_quoted_note"}} &rarr;</a>
</div>
{{end}}
{{end}}`

// partialPagination renders the load more pagination block
var partialPagination = `{{define "pagination"}}
<div class="pagination" id="pagination">
{{if and .Pagination .Pagination.Next}}
  <a href="{{.Pagination.Next}}&append=1" h-get h-target="#pagination" h-swap="outer" h-push-url h-trigger="intersect once" h-disabled class="link load-more-btn" rel="next"><span class="load-more-text">{{i18n "nav.load_more"}} →</span><span class="load-more-loading"><span class="h-spinner"></span> {{i18n "status.loading"}}...</span></a>
{{end}}
</div>
{{end}}`

// partialFlashMessages renders error and success flash messages
var partialFlashMessages = `{{define "flash-messages"}}
{{if .Error}}<div class="error-box" role="alert">{{.Error}}</div>{{end}}
{{if .Success}}<div class="flash-message" role="status" aria-live="polite">{{.Success}}</div>{{end}}
{{end}}`

// eventDispatcher routes to the appropriate render template based on .RenderTemplate
// This is the universal entry point - routing is purely mechanical (no kind logic here)
// The RenderTemplate value is computed server-side from event metadata (render-hint tags or kind mapping)
var eventDispatcher = `{{define "event-dispatcher"}}
{{if eq .RenderTemplate "render-note"}}{{template "render-note" .}}
{{else if eq .RenderTemplate "render-repost"}}{{template "render-repost" .}}
{{else if eq .RenderTemplate "render-picture"}}{{template "render-picture" .}}
{{else if eq .RenderTemplate "render-shortvideo"}}{{template "render-shortvideo" .}}
{{else if eq .RenderTemplate "render-longform"}}{{template "render-longform" .}}
{{else if eq .RenderTemplate "render-zap"}}{{template "render-zap" .}}
{{else if eq .RenderTemplate "render-livestream"}}{{template "render-livestream" .}}
{{else if eq .RenderTemplate "render-bookmarks"}}{{template "render-bookmarks" .}}
{{else if eq .RenderTemplate "render-highlight"}}{{template "render-highlight" .}}
{{else if eq .RenderTemplate "render-classified"}}{{template "render-classified" .}}
{{else}}{{template "render-default" .}}
{{end}}
{{end}}`

// GetKindTemplates returns all kind templates concatenated
func GetKindTemplates() string {
	return partialFlashMessages +
		partialPagination +
		partialAuthorHeader +
		partialNoteFooter +
		partialQuotedNote +
		renderTemplateNote +
		renderTemplateRepost +
		renderTemplatePicture +
		renderTemplateShortvideo +
		renderTemplateLongform +
		renderTemplateZap +
		renderTemplateLivestream +
		renderTemplateBookmarks +
		renderTemplateHighlight +
		renderTemplateClassified +
		renderTemplateDefault +
		eventDispatcher
}
