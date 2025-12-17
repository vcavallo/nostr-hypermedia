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
    <a href="/html/thread/{{.Root.RepostedEvent.ID}}" class="view-note-link" rel="related">{{i18n "nav.view_note"}} &rarr;</a>
  </div>
  {{else}}
  <div class="note-content repost-empty">{{i18n "msg.repost_not_available"}}</div>
  {{end}}
  {{else}}
  <div class="note-content">{{.Root.ContentHTML}}</div>
  {{end}}
  {{template "quoted-note" .Root}}
  {{template "note-footer" .Root}}
  {{if .Root.ParentID}}
  <div class="thread-nav">
    <a href="/html/thread/{{.Root.ParentID}}" class="text-link" rel="up">↑ View parent note</a>
  </div>
  {{end}}
</article>

{{if .LoggedIn}}
<div class="reply-form-minimal">
  <div id="reply-error" class="form-error" role="alert" aria-live="polite"></div>
  <form method="POST" action="/html/reply" class="reply-form" id="reply-form" h-post h-target="#reply-form" h-swap="outer" h-indicator="#reply-spinner" h-error-target="#reply-error">
    <input type="hidden" name="csrf_token" value="{{.CSRFToken}}">
    <input type="hidden" name="reply_to" value="{{.Root.ID}}">
    <input type="hidden" name="reply_to_pubkey" value="{{.Root.Pubkey}}">
    <input type="hidden" name="reply_count" value="{{.TotalReplyCount}}">
    <label for="reply-content" class="sr-only">Write a reply</label>
    <textarea id="reply-content" name="content" placeholder="Write a reply..."></textarea>
    <div id="gif-attachment-reply"></div>
    <div class="reply-actions-minimal">
      <button type="submit" class="btn-primary">{{i18n "btn.reply"}} <span id="reply-spinner" class="h-indicator"><span class="h-spinner"></span></span></button>
      {{if .ShowGifButton}}<a href="/html/gifs?target=reply" h-get h-target="#gif-panel-reply" h-swap="inner" class="btn-primary" title="Add GIF">Add GIF</a>{{end}}
      <a href="/html/profile/{{.UserNpub}}" class="reply-avatar-link" title="{{.UserDisplayName}}" rel="author">
        <img src="{{if .UserAvatarURL}}{{.UserAvatarURL}}{{else}}/static/avatar.jpg{{end}}" alt="Your avatar" class="reply-avatar" loading="lazy">
      </a>
    </div>
  </form>
  <div id="gif-panel-reply"></div>
</div>
{{else}}
<div class="login-prompt-box">
  <a href="/html/login" class="text-link" rel="nofollow">{{i18n "btn.login"}}</a> {{i18n "label.to_reply"}}
</div>
{{end}}

<section class="replies-section" aria-labelledby="replies-heading">
  <h3 id="replies-heading">{{i18n "label.replies"}} (<span id="reply-count">{{.TotalReplyCount}}</span>)</h3>
  <div id="replies-list">
  {{if not .ReplyGroups}}
  <p class="replies-empty">{{i18n "msg.no_replies_yet"}}</p>
  {{end}}
  {{range .ReplyGroups}}
  <div class="reply-group">
    <article class="note reply" id="reply-{{.Parent.ID}}">
      {{template "author-header" .Parent}}
      <div class="note-content">{{.Parent.ContentHTML}}</div>
      {{template "quoted-note" .Parent}}
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
        <div class="note-content">{{.ContentHTML}}</div>
        {{template "quoted-note" .}}
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
