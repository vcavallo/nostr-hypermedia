package templates

// Thread template - root note with replies and reply form.

func GetThreadTemplate() string {
	return threadContent
}

var threadContent = `{{define "content"}}
{{template "flash-messages" .}}

{{if .Root}}
<article class="note root">
  {{template "author-header" .Root}}
  {{if .Root.HasContentWarning}}<details class="content-warning">
    <summary class="content-warning-label">{{if .Root.ContentWarning}}{{.Root.ContentWarning}}{{else}}{{i18n "label.sensitive_content"}}{{end}}</summary>
    <div class="content-warning-body">{{end}}
  {{if eq .Root.Kind 30023}}
  <article class="long-form-article">
    {{if .Root.HeaderImage}}<img src="{{.Root.HeaderImage}}" alt="Article header" class="article-header-image" loading="lazy">{{end}}
    {{if .Root.Title}}<h2 class="article-title">{{.Root.Title}}</h2>{{end}}
    {{if .Root.Summary}}<p class="article-summary">{{.Root.Summary}}</p>{{end}}
    {{if .Root.PublishedAt}}<div class="article-published">Published: <time datetime="{{isoTime .Root.PublishedAt}}">{{formatTime .Root.PublishedAt}}</time></div>{{end}}
    <div class="article-content">{{.Root.ContentHTML}}</div>
  </article>
  {{else if eq .Root.Kind 6}}
  {{if .Root.RepostedEvent}}
  <div class="repost-indicator">{{i18n "label.reposted"}}</div>
  <div class="reposted-note">
    <div class="note-author">
      <span class="text-muted">
      <img class="author-avatar" src="{{if and .Root.RepostedEvent.AuthorProfile .Root.RepostedEvent.AuthorProfile.Picture}}{{avatarURL .Root.RepostedEvent.AuthorProfile.Picture}}{{else}}/static/avatar.jpg{{end}}" alt="{{displayName .Root.RepostedEvent.AuthorProfile "User"}}'s avatar" loading="lazy">
      </span>
      <div class="author-info">
        <span class="text-muted">
        {{if and .Root.RepostedEvent.AuthorProfile (or .Root.RepostedEvent.AuthorProfile.DisplayName .Root.RepostedEvent.AuthorProfile.Name)}}
        <span class="author-name">{{displayName .Root.RepostedEvent.AuthorProfile .Root.RepostedEvent.NpubShort}}</span>
        {{if .Root.RepostedEvent.AuthorProfile.Nip05}}<span class="author-nip05">{{.Root.RepostedEvent.AuthorProfile.Nip05}}</span>{{end}}
        {{else if and .Root.RepostedEvent.AuthorProfile .Root.RepostedEvent.AuthorProfile.Nip05}}
        <span class="author-nip05">{{.Root.RepostedEvent.AuthorProfile.Nip05}}</span>
        {{else}}
        <span class="pubkey" title="{{.Root.RepostedEvent.Pubkey}}">{{.Root.RepostedEvent.NpubShort}}</span>
        {{end}}
        </span>
      </div>
    </div>
    {{if eq .Root.RepostedEvent.TemplateName "picture"}}
    <div class="picture-note">
      {{if .Root.RepostedEvent.Title}}<div class="picture-title">{{.Root.RepostedEvent.Title}}</div>{{end}}
      <div class="picture-gallery">{{.Root.RepostedEvent.ImagesHTML}}</div>
      {{if .Root.RepostedEvent.Content}}<div class="picture-caption">{{.Root.RepostedEvent.ContentHTML}}</div>{{end}}
    </div>
    {{else}}
    <div class="note-content">{{.Root.RepostedEvent.ContentHTML}}</div>
    {{end}}
    <a href="/thread/{{eventLink .Root.RepostedEvent.ID .Root.RepostedEvent.Kind .Root.RepostedEvent.Pubkey .Root.RepostedEvent.DTag}}" h-get h-target="#page-content" h-swap="inner" h-push-url h-scroll="top" h-prefetch class="view-note-link" rel="related">{{i18n "nav.view_quoted_note"}} &rarr;</a>
  </div>
  {{else}}
  <div class="note-content repost-empty">{{i18n "msg.repost_not_available"}}</div>
  {{end}}
  {{else}}
  <div class="note-content">{{.Root.ContentHTML}}</div>
  {{end}}
  {{template "quoted-note" .Root}}
  {{if .Root.HasContentWarning}}</div>
  </details>{{end}}
  <footer class="note-footer" id="footer-{{.Root.ID}}">
    <div class="note-actions">
      {{if .Root.ParentID}}<a href="/thread/{{noteLink .Root.ParentID}}" h-get h-target="#page-content" h-swap="inner" h-push-url h-scroll="top" h-prefetch class="action-pill action-parent" rel="up">↑ {{i18n "nav.view_parent"}}</a>{{end}}
      {{range .Root.ActionGroups}}
        {{if .HasGroup}}
        <details class="action-group {{.Primary.Class}}{{if .Primary.Completed}} completed{{end}}">
          <summary class="action-pill"{{if eq .Primary.IconOnly "always"}} title="{{.Primary.Title}}" aria-label="{{.Primary.Title}}"{{end}}>
            {{template "pill-content" .Primary}}
          </summary>
          <div class="action-dropdown">
            {{template "dropdown-action" dict "Action" .Primary "EventID" $.Root.ID}}
            {{range .Children}}
            {{template "dropdown-action" dict "Action" . "EventID" $.Root.ID}}
            {{end}}
          </div>
        </details>
        {{else}}
        {{template "action-pill" dict "Action" .Primary "EventID" $.Root.ID}}
        {{end}}
      {{end}}
    </div>
  </footer>
</article>

{{if .LoggedIn}}
<div class="reply-form-minimal">
  <div id="reply-error" class="form-error" role="alert" aria-live="polite"></div>
  <form method="POST" action="/reply" class="reply-form" id="reply-form" h-post h-target="#reply-form" h-swap="outer" h-indicator="#reply-spinner" h-error-target="#reply-error">
    <input type="hidden" name="csrf_token" value="{{.CSRFToken}}">
    <input type="hidden" name="reply_to" value="{{.Root.ID}}">
    <input type="hidden" name="reply_to_pubkey" value="{{.Root.Pubkey}}">
    <input type="hidden" name="reply_to_kind" value="{{.Root.Kind}}">
    {{if .Root.DTag}}<input type="hidden" name="reply_to_dtag" value="{{.Root.DTag}}">{{end}}
    <input type="hidden" name="reply_count" value="{{.TotalReplyCount}}">
    <input type="hidden" id="mentions-data-reply" name="mentions" value="{}">
    <label for="reply-content" class="sr-only">Write a reply</label>
    <textarea id="reply-content" name="content" placeholder="Write a reply..."></textarea>
    <a href="{{buildURL "/mentions" "target" "reply"}}" h-get h-target="#mentions-dropdown-reply" h-swap="inner" h-trigger="input debounce:300 from:#reply-content" h-include="#reply-content" hidden aria-hidden="true" aria-label="Mention autocomplete trigger"></a>
    <div id="mentions-dropdown-reply" class="mentions-dropdown"></div>
    <div id="gif-attachment-reply"></div>
    <div class="reply-actions-minimal">
      <button type="submit" class="btn-primary">{{i18n "btn.reply"}} <span id="reply-spinner" class="h-indicator"><span class="h-spinner"></span></span></button>
      {{if .ShowGifButton}}<a href="{{buildURL "/gifs" "target" "reply"}}" h-get h-target="#gif-panel-reply" h-swap="inner" class="btn-primary" title="Add GIF">Add GIF</a>{{end}}
      <details class="cw-dropdown">
        <summary class="cw-toggle" title="{{i18n "label.content_warning"}}">⚠️</summary>
        <div class="cw-options">
          <select name="content_warning" class="cw-select" aria-label="{{i18n "label.content_warning"}}">
            <option value="">{{i18n "option.no_warning"}}</option>
            <option value="nsfw">NSFW</option>
            <option value="spoiler">{{i18n "option.spoiler"}}</option>
            <option value="sensitive">{{i18n "option.sensitive"}}</option>
          </select>
          <input type="text" name="content_warning_custom" class="cw-custom" placeholder="{{i18n "placeholder.custom_warning"}}">
        </div>
      </details>
      <a href="/profile/{{.UserNpub}}" class="reply-avatar-link" title="{{.UserDisplayName}}" rel="author">
        <img src="{{if .UserAvatarURL}}{{.UserAvatarURL}}{{else}}/static/avatar.jpg{{end}}" alt="Your avatar" class="reply-avatar" loading="lazy">
      </a>
    </div>
  </form>
  <div id="gif-panel-reply"></div>
</div>
{{else}}
<div class="login-prompt-box">
  <a href="/login" class="text-link" rel="nofollow">{{i18n "btn.login"}}</a> {{i18n "label.to_reply"}}
</div>
{{end}}

<section class="replies-section" aria-labelledby="replies-heading">
  <h3 id="replies-heading">{{i18n "label.replies"}} (<span id="reply-count">{{.TotalReplyCount}}</span>)</h3>
  {{if and .CachedAt .Identifier}}<div id="thread-new-replies" h-poll="{{buildURL (print "/thread/" .Identifier "/check-new") "since" .CachedAt "url" .CurrentURL}} 30s" h-target="#thread-new-replies" h-swap="outer" h-poll-pause-hidden></div>{{end}}
  <div id="replies-list">
  {{if not .ReplyGroups}}
  <p class="replies-empty">{{i18n "msg.no_replies_yet"}}</p>
  {{end}}
  {{range .ReplyGroups}}
  <div class="reply-group">
    <article class="note reply" id="reply-{{.Parent.ID}}">
      {{template "author-header" .Parent}}
      {{if .Parent.HasContentWarning}}<details class="content-warning">
        <summary class="content-warning-label">{{if .Parent.ContentWarning}}{{.Parent.ContentWarning}}{{else}}{{i18n "label.sensitive_content"}}{{end}}</summary>
        <div class="content-warning-body">{{end}}
      <div class="note-content">{{.Parent.ContentHTML}}</div>
      {{template "quoted-note" .Parent}}
      {{if .Parent.HasContentWarning}}</div>
      </details>{{end}}
      {{template "note-footer" .Parent}}
    </article>
    {{if .Children}}
    <div class="reply-children">
      {{range .Children}}
      <article class="note reply reply-nested" id="reply-{{.ID}}">
        {{template "author-header" .}}
        {{if .ReplyToName}}
        <div class="reply-context">
          <a href="#reply-{{.ParentID}}" class="reply-context-link">↩ {{i18n "label.replying_to"}} {{.ReplyToName}}</a>
        </div>
        {{end}}
        {{if .HasContentWarning}}<details class="content-warning">
          <summary class="content-warning-label">{{if .ContentWarning}}{{.ContentWarning}}{{else}}{{i18n "label.sensitive_content"}}{{end}}</summary>
          <div class="content-warning-body">{{end}}
        <div class="note-content">{{.ContentHTML}}</div>
        {{template "quoted-note" .}}
        {{if .HasContentWarning}}</div>
        </details>{{end}}
        {{template "note-footer" .}}
      </article>
      {{end}}
    </div>
    {{end}}
  </div>
  {{end}}
  </div>
</section>
{{else}}
<div class="empty-state">
  <div class="empty-state-icon">🔍</div>
  <p>Event not found</p>
  <p class="empty-state-hint">This note may have been deleted or may not exist on the relays we checked.</p>
</div>
{{end}}
{{end}}`
