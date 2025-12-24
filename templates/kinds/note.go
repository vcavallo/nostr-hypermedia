package kinds

// Template renders kind 1 (note) - the default for most text content.
var Note = `{{define "render-note"}}
<article class="note" id="note-{{.ID}}"{{if .IsScrollTarget}} data-scroll-target{{end}} aria-label="Note by {{displayName .AuthorProfile .NpubShort}}">
  {{template "author-header" .}}
  {{template "content-warning-start" .}}
  <div class="note-content">{{.ContentHTML}}</div>
  {{template "quoted-note" .}}
  {{template "content-warning-end" .}}
  {{template "note-footer" .}}
</article>
{{end}}`
