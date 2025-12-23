package kinds

// Status renders kind 30315 user status events (NIP-38)
var Status = `{{define "render-status"}}
<article class="note status-note" id="note-{{.ID}}" aria-label="Status by {{displayName .AuthorProfile .NpubShort}}">
  {{template "author-header" .}}
  <div class="status-card">
    <div class="status-content">
      {{if eq .StatusType "music"}}
      <span class="status-icon">🎵</span>
      {{else}}
      <span class="status-icon">💭</span>
      {{end}}
      <span class="status-text">{{.Content}}</span>
    </div>
    {{if .StatusLink}}
    <a href="{{.StatusLink}}" target="_blank" rel="noopener" class="status-link">{{.StatusLink}}</a>
    {{end}}
  </div>
  {{template "note-footer" .}}
</article>
{{end}}`
