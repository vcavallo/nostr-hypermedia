package kinds

// Template is the fallback for unknown/unsupported kinds.
// Shows kind info, handler links for opening in other apps, and raw content.
var Default = `{{define "render-default"}}
<article class="note unknown-kind" id="note-{{.ID}}"{{if .IsScrollTarget}} data-scroll-target{{end}} aria-label="Event by {{displayName .AuthorProfile .NpubShort}}">
  {{template "author-header" .}}
  <div class="unknown-kind-info">
    <span class="unknown-kind-badge">Kind {{.Kind}}</span>
    <span class="unknown-kind-message">{{i18n "msg.unsupported_kind"}}</span>
  </div>
  {{if .Handlers}}
  <nav class="handler-links">
    {{range .Handlers}}<a href="{{.URL}}" target="_blank" rel="noopener" class="handler-link{{if gt .RecommendedBy 0}} handler-recommended{{end}}">{{if .Picture}}<img src="{{.Picture}}" alt="" class="handler-icon" loading="lazy">{{end}}{{i18n "action.open_in"}} {{.Name}}{{if gt .RecommendedBy 0}}<span class="handler-recommended-badge">{{i18n "label.recommended_by"}} {{.RecommendedBy}}</span>{{end}}</a>{{end}}
  </nav>
  {{end}}
  {{if .Content}}
  <details class="unknown-kind-content">
    <summary>{{i18n "action.view_raw"}}</summary>
    <pre class="raw-content">{{.Content}}</pre>
  </details>
  {{end}}
  {{template "note-footer" .}}
</article>
{{end}}`
