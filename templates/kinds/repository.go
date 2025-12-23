package kinds

// RepositoryTemplate renders kind 30617 repository announcement events (NIP-34)
const RepositoryTemplate = `
{{define "render-repository"}}
<article class="note repo-note" id="note-{{.ID}}" aria-label="Repository by {{displayName .AuthorProfile .NpubShort}}">
  {{template "author-header" .}}
  <div class="repo-card">
    <div class="repo-header">
      <span class="repo-icon">📦</span>
      <h3 class="repo-name">{{if .RepoName}}{{.RepoName}}{{else}}{{.RepoID}}{{end}}</h3>
    </div>
    {{if .RepoDescription}}
    <p class="repo-description">{{.RepoDescription}}</p>
    {{end}}
    <div class="repo-links">
      {{if .RepoWebURLs}}
      <div class="repo-link-group">
        <span class="repo-link-label">{{i18n "label.browse"}}:</span>
        {{range .RepoWebURLs}}
        <a href="{{.}}" class="repo-link" target="_blank" rel="noopener">{{.}}</a>
        {{end}}
      </div>
      {{end}}
      {{if .RepoCloneURLs}}
      <div class="repo-link-group">
        <span class="repo-link-label">{{i18n "label.clone"}}:</span>
        {{range .RepoCloneURLs}}
        <code class="repo-clone-url">{{.}}</code>
        {{end}}
      </div>
      {{end}}
    </div>
    {{if .RepoMaintainers}}
    <div class="repo-maintainers">
      <span class="repo-maintainers-label">{{i18n "label.maintainers"}}:</span>
      {{range .RepoMaintainers}}
      <a href="/profile/{{.Npub}}" class="repo-maintainer" rel="author">
        {{if .Picture}}<img src="{{.Picture}}" alt="" class="repo-maintainer-avatar" loading="lazy">{{end}}
        <span class="repo-maintainer-name">{{.DisplayName}}</span>
      </a>
      {{end}}
    </div>
    {{end}}
    {{if .RepoHashtags}}
    <div class="repo-tags">
      {{range .RepoHashtags}}
      <a href="{{buildURL "/search" "q" (print "#" .)}}" class="repo-hashtag">#{{.}}</a>
      {{end}}
    </div>
    {{end}}
  </div>
  {{template "note-footer" .}}
</article>
{{end}}
`
