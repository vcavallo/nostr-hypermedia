package kinds

// LabelTemplate renders kind 1985 label events (NIP-32)
const LabelTemplate = `
{{define "render-label"}}
<article class="note label-note" id="note-{{.ID}}" aria-label="Label by {{displayName .AuthorProfile .NpubShort}}">
  {{template "author-header" .}}
  <div class="label-card">
    <div class="label-header">
      <span class="label-icon">🏷️</span>
      <span class="label-title">{{i18n "kind.label.label"}}</span>
    </div>
    <div class="label-tags">
      {{range .Labels}}
      <span class="label-tag">
        {{if .Namespace}}<span class="label-namespace">{{.Namespace}}:</span>{{end}}
        <span class="label-value">{{.Value}}</span>
      </span>
      {{end}}
    </div>
    {{if .LabelTargets}}
    <div class="label-targets">
      <span class="label-targets-title">{{i18n "label.applied_to"}}:</span>
      {{range .LabelTargets}}
      <a href="{{.URL}}" class="label-target-link">{{.Type}}</a>
      {{end}}
    </div>
    {{end}}
    {{if .Content}}
    <div class="label-description">{{.Content}}</div>
    {{end}}
  </div>
  {{template "note-footer" .}}
</article>
{{end}}
`
