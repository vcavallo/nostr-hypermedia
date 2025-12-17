package templates

// Timeline template - main feed with post form, events, and pagination.

func GetTimelineTemplate() string {
	return timelineContent
}

var timelineContent = `{{define "content"}}
{{template "flash-messages" .}}

{{if and .LoggedIn .NewestTimestamp (eq .FeedMode "follows")}}
<div id="new-notes-indicator" h-poll="/html/timeline/check-new?since={{.NewestTimestamp}}&amp;kinds={{.KindsParam}}&amp;filter={{.KindFilter}}&amp;url={{urlquery .CurrentURL}} 30s" h-target="#new-notes-indicator" h-swap="outer" h-poll-pause-hidden></div>
{{end}}

<div id="notes-list">
{{range .Items}}
{{template "event-dispatcher" .}}
{{end}}
</div>
{{if not .Items}}
<div class="empty-state">
  <div class="empty-state-icon">📭</div>
  <p>No notes found</p>
  <p class="empty-state-hint">Try adjusting your filters or check back later.</p>
  <a href="{{.CurrentURL}}" h-get h-target="#page-content" h-swap="inner" h-indicator="#nav-loading" class="btn btn-secondary">{{i18n "btn.try_again"}}</a>
</div>
{{end}}

{{if .Pagination}}
<div class="pagination" id="pagination">
  {{if .Pagination.Prev}}
  <a href="{{.Pagination.Prev}}" h-get h-target="#page-content" h-swap="inner" h-push-url h-scroll="top" h-indicator="#nav-loading" class="link" rel="prev">← Newer</a>
  {{end}}
  {{if .Pagination.Next}}
  <a href="{{.Pagination.Next}}&append=1" h-get h-target="#pagination" h-swap="outer" h-push-url h-trigger="intersect once" h-disabled class="link load-more-btn" rel="next"><span class="load-more-text">{{i18n "nav.load_more"}} →</span><span class="load-more-loading"><span class="h-spinner"></span> {{i18n "status.loading"}}...</span></a>
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
{{end}}`
