package kinds

// Template renders kind 30023 (article).
var Longform = `{{define "render-longform"}}
<article class="note" id="note-{{.ID}}"{{if .IsScrollTarget}} data-scroll-target{{end}} aria-label="Article by {{displayName .AuthorProfile .NpubShort}}">
  {{template "author-header" .}}
  {{template "content-warning-start" .}}
  <div class="article-preview">
    {{if .HeaderImage}}<img src="{{.HeaderImage}}" alt="Article header image" class="article-preview-image" loading="lazy">{{end}}
    {{if .Title}}<h3 class="article-preview-title">{{.Title}}</h3>{{end}}
    {{if .Summary}}<p class="article-preview-summary">{{.Summary}}</p>{{end}}
  </div>
  {{template "content-warning-end" .}}
  {{template "note-footer" .}}
</article>
{{end}}`
