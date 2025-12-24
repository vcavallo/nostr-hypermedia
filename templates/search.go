package templates

// Search template - search form and results.

func GetSearchTemplate() string {
	return searchContent
}

var searchContent = `{{define "content"}}
<form action="/search" method="GET" class="search-form" h-get h-target="#search-results" h-swap="inner" h-trigger="input debounce:300 from:#search-input, submit" h-sync="abort" h-replace-url h-indicator="#search-results">
  <label for="search-input" class="sr-only">Search notes</label>
  <input type="search" id="search-input" name="q" value="{{.Query}}" placeholder="Search notes, hashtags, topics..." class="search-input" autocomplete="off" autofocus>
  <button type="submit" class="sr-only">{{i18n "btn.search"}}</button>
</form>

<div id="search-results">
{{template "search-results" .}}
</div>
{{end}}

{{define "skeleton-cards"}}
<div class="skeleton-cards" aria-hidden="true">
  <div class="skeleton-card">
    <div class="skeleton-header">
      <div class="skeleton-avatar"></div>
      <div class="skeleton-name"></div>
      <div class="skeleton-time"></div>
    </div>
    <div class="skeleton-content">
      <div class="skeleton-line"></div>
      <div class="skeleton-line"></div>
      <div class="skeleton-line"></div>
    </div>
    <div class="skeleton-footer"></div>
  </div>
  <div class="skeleton-card">
    <div class="skeleton-header">
      <div class="skeleton-avatar"></div>
      <div class="skeleton-name"></div>
      <div class="skeleton-time"></div>
    </div>
    <div class="skeleton-content">
      <div class="skeleton-line"></div>
      <div class="skeleton-line"></div>
    </div>
    <div class="skeleton-footer"></div>
  </div>
  <div class="skeleton-card">
    <div class="skeleton-header">
      <div class="skeleton-avatar"></div>
      <div class="skeleton-name"></div>
      <div class="skeleton-time"></div>
    </div>
    <div class="skeleton-content">
      <div class="skeleton-line"></div>
      <div class="skeleton-line"></div>
      <div class="skeleton-line"></div>
    </div>
    <div class="skeleton-footer"></div>
  </div>
</div>
{{end}}

{{define "search-results"}}
{{template "skeleton-cards" .}}
<div class="skeleton-cards-hide">
{{if .Query}}
  {{if .Items}}
  <div class="results-header">Results for "{{.Query}}"</div>
  <div id="notes-list">
  {{range .Items}}
  <article class="note" aria-label="Note by {{if and .AuthorProfile .AuthorProfile.DisplayName}}{{.AuthorProfile.DisplayName}}{{else if and .AuthorProfile .AuthorProfile.Name}}{{.AuthorProfile.Name}}{{else}}{{.NpubShort}}{{end}}">
    {{template "author-header" .}}
    <div class="note-content">{{.ContentHTML}}</div>
    <div class="note-footer">
      <a href="/thread/{{eventLink .ID .Kind .Pubkey .DTag}}" h-get h-target="#page-content" h-swap="inner" h-push-url h-scroll="top" h-prefetch class="text-link" rel="related">{{i18n "nav.view_thread"}}</a>
      {{if or (gt .ReplyCount 0) (and .Reactions (gt .Reactions.Total 0))}}
      <span class="note-stats">
        {{if gt .ReplyCount 0}}<span class="stat-badge">💬 {{.ReplyCount}}</span>{{end}}
        {{if and .Reactions (gt .Reactions.Total 0)}}<span class="stat-badge">❤️ {{.Reactions.Total}}</span>{{end}}
      </span>
      {{end}}
    </div>
  </article>
  {{end}}
  </div>

  {{template "pagination" .}}
  {{else}}
  <div class="empty-state">
    <div class="empty-state-icon">🔍</div>
    <p>No results for "{{.Query}}"</p>
    <p class="empty-state-hint">Try different keywords or check your spelling.</p>
  </div>
  {{end}}
{{else}}
<div class="empty-state">
  <div class="empty-state-icon">🔍</div>
  <p>Search notes</p>
  <p class="empty-state-hint">Enter a search term above to find notes.</p>
</div>
{{end}}
</div>
{{end}}`
