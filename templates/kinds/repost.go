package kinds

// Template renders kind 6 and 16 (repost/generic repost).
var Repost = `{{define "render-repost"}}
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
    {{if .RepostedEvent.HasContentWarning}}<details class="content-warning">
      <summary class="content-warning-label">{{if .RepostedEvent.ContentWarning}}{{.RepostedEvent.ContentWarning}}{{else}}{{i18n "label.sensitive_content"}}{{end}}</summary>
      <div class="content-warning-body">{{end}}
    {{if eq .RepostedEvent.TemplateName "picture"}}
    <div class="picture-note">
      {{if .RepostedEvent.Title}}<div class="picture-title">{{.RepostedEvent.Title}}</div>{{end}}
      <div class="picture-gallery">{{.RepostedEvent.ImagesHTML}}</div>
      {{if .RepostedEvent.Content}}<div class="picture-caption">{{.RepostedEvent.ContentHTML}}</div>{{end}}
    </div>
    {{else}}
    <div class="note-content">{{.RepostedEvent.ContentHTML}}</div>
    {{end}}
    {{if or .RepostedEvent.QuotedEvent .RepostedEvent.QuotedEventID}}
    {{template "quoted-note" .RepostedEvent}}
    {{end}}
    {{if .RepostedEvent.HasContentWarning}}</div>
    </details>{{end}}
    <a href="/thread/{{eventLink .RepostedEvent.ID .RepostedEvent.Kind .RepostedEvent.Pubkey .RepostedEvent.DTag}}" h-get h-target="#page-content" h-swap="inner" h-push-url h-scroll="top" h-prefetch class="view-note-link" rel="related">{{i18n "nav.view_note"}} &rarr;</a>
  </div>
  {{else}}
  <div class="note-content repost-empty">{{i18n "msg.repost_not_available"}}</div>
  {{end}}
  {{template "note-footer" .}}
</article>
{{end}}`
