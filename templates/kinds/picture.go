package kinds

// Template renders kind 20 (picture).
var Picture = `{{define "render-picture"}}
<article class="note" id="note-{{.ID}}"{{if .IsScrollTarget}} data-scroll-target{{end}} aria-label="Photo by {{displayName .AuthorProfile .NpubShort}}">
  {{template "author-header" .}}
  {{template "content-warning-start" .}}
  <div class="picture-note">
    {{if .Title}}<div class="picture-title">{{.Title}}</div>{{end}}
    <div class="picture-gallery{{if eq .ImageCount 1}} single-image{{end}}">{{.ImagesHTML}}</div>
    {{if .Content}}<div class="picture-caption">{{.ContentHTML}}</div>{{end}}
  </div>
  {{template "content-warning-end" .}}
  {{template "note-footer" .}}
</article>
{{end}}`
