package templates

// Quote template - note being quoted and commentary form.

func GetQuoteTemplate() string {
	return quoteContent
}

var quoteContent = `{{define "content"}}
{{template "flash-messages" .}}

<div class="quoted-note">
  <div class="quoted-author">
    <img src="{{if and .QuotedEvent.AuthorProfile .QuotedEvent.AuthorProfile.Picture}}{{avatarURL .QuotedEvent.AuthorProfile.Picture}}{{else}}/static/avatar.jpg{{end}}" alt="{{displayName .QuotedEvent.AuthorProfile "User"}}'s avatar" loading="lazy">
    <span class="quoted-author-name">{{displayName .QuotedEvent.AuthorProfile .QuotedEvent.NpubShort}}</span>
  </div>
  {{if eq .QuotedEvent.TemplateName "longform"}}
  <div class="quoted-article-title">{{if .QuotedEvent.Title}}{{.QuotedEvent.Title}}{{else}}{{i18n "msg.untitled"}} Article{{end}}</div>
  {{if .QuotedEvent.Summary}}<div class="quoted-article-summary">{{.QuotedEvent.Summary}}</div>{{end}}
  <a href="/thread/{{eventLink .QuotedEvent.ID .QuotedEvent.Kind .QuotedEvent.Pubkey .QuotedEvent.DTag}}" h-get h-target="#page-content" h-swap="inner" h-push-url h-prefetch class="view-note-link" rel="related">{{i18n "nav.read_article"}} &rarr;</a>
  {{else}}
  <div class="note-content">{{.QuotedEvent.ContentHTML}}</div>
  <a href="/thread/{{eventLink .QuotedEvent.ID .QuotedEvent.Kind .QuotedEvent.Pubkey .QuotedEvent.DTag}}" h-get h-target="#page-content" h-swap="inner" h-push-url h-prefetch class="view-note-link" rel="related">{{i18n "nav.view_original_note"}} &rarr;</a>
  {{end}}
</div>

{{if .LoggedIn}}
<div class="reply-form-minimal">
  <form method="POST" class="reply-form">
    <input type="hidden" name="csrf_token" value="{{.CSRFToken}}">
    <input type="hidden" name="quoted_pubkey" value="{{.QuotedEvent.Pubkey}}">
    <input type="hidden" id="mentions-data-quote" name="mentions" value="{}">
    <label for="quote-content" class="sr-only">Add your commentary</label>
    <textarea id="quote-content" name="content" placeholder="Add your commentary..." autofocus></textarea>
    <a href="{{buildURL "/mentions" "target" "quote"}}" h-get h-target="#mentions-dropdown-quote" h-swap="inner" h-trigger="input debounce:300 from:#quote-content" h-include="#quote-content" hidden aria-hidden="true" aria-label="Mention autocomplete trigger"></a>
    <div id="mentions-dropdown-quote" class="mentions-dropdown"></div>
    <div id="gif-attachment-quote"></div>
    <div class="reply-actions-minimal">
      <button type="submit" class="btn-primary">{{i18n "btn.post_commentary"}}</button>
      {{if .ShowGifButton}}<a href="{{buildURL "/gifs" "target" "quote"}}" h-get h-target="#gif-panel-quote" h-swap="inner" class="btn-primary" title="Add GIF">Add GIF</a>{{end}}
      <a href="/profile/{{.UserNpub}}" class="reply-avatar-link" title="{{.UserDisplayName}}" rel="author">
        <img src="{{if .UserAvatarURL}}{{.UserAvatarURL}}{{else}}/static/avatar.jpg{{end}}" alt="Your avatar" class="reply-avatar" loading="lazy">
      </a>
    </div>
  </form>
  <div id="gif-panel-quote"></div>
</div>
{{else}}
<div class="login-prompt-box">
  <a href="/login" class="text-link" rel="nofollow">{{i18n "btn.login"}}</a> to quote this note
</div>
{{end}}
{{end}}`
