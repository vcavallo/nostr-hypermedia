package kinds

// Report renders kind 1984 report events (NIP-56)
var Report = `{{define "render-report"}}
<article class="note report-note" id="note-{{.ID}}" aria-label="Report by {{displayName .AuthorProfile .NpubShort}}">
  {{template "author-header" .}}
  <div class="report-card">
    <div class="report-header">
      <span class="report-icon">⚠️</span>
      <span class="report-type">{{i18n (printf "report.%s" .ReportType)}}</span>
    </div>
    {{if .Content}}<p class="report-reason">{{.Content}}</p>{{end}}
  </div>
  {{template "note-footer" .}}
</article>
{{end}}`
