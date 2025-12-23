package kinds

// Template renders kind 9802 (highlight).
var Highlight = `{{define "render-highlight"}}
<article class="note highlight" id="note-{{.ID}}" aria-label="Highlight by {{displayName .AuthorProfile .NpubShort}}">
  {{template "author-header" .}}
  {{template "content-warning-start" .}}
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
  {{template "content-warning-end" .}}
  {{template "note-footer" .}}
</article>
{{end}}`
