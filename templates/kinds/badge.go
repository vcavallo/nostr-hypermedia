package kinds

// BadgeDefinition renders kind 30009 badge definition events (NIP-58)
var BadgeDefinition = `{{define "render-badge-definition"}}
<article class="note badge-definition-note" id="note-{{.ID}}" aria-label="Badge definition by {{displayName .AuthorProfile .NpubShort}}">
  {{template "author-header" .}}
  <div class="badge-card">
    {{if .BadgeImage}}
    <img src="{{if .BadgeThumbnail}}{{.BadgeThumbnail}}{{else}}{{.BadgeImage}}{{end}}" alt="{{.BadgeName}}" class="badge-image" loading="lazy">
    {{else}}
    <div class="badge-placeholder">🏅</div>
    {{end}}
    <div class="badge-info">
      <h3 class="badge-name">{{.BadgeName}}</h3>
      {{if .BadgeDescription}}<p class="badge-description">{{.BadgeDescription}}</p>{{end}}
    </div>
  </div>
  {{template "note-footer" .}}
</article>
{{end}}`

// BadgeAward renders kind 8 badge award events (NIP-58)
var BadgeAward = `{{define "render-badge-award"}}
<article class="note badge-award-note" id="note-{{.ID}}" aria-label="Badge award by {{displayName .AuthorProfile .NpubShort}}">
  {{template "author-header" .}}
  <div class="badge-award-card">
    <div class="badge-award-header">
      <span class="badge-award-icon">🎖️</span>
      <span class="badge-award-text">{{i18n "label.badge_awarded"}}</span>
    </div>
    {{if .BadgeAwardees}}
    <div class="badge-awardees">
      <span class="badge-awardees-label">{{i18n "label.recipients"}}:</span>
      <span class="badge-awardees-count">{{len .BadgeAwardees}}</span>
    </div>
    {{end}}
  </div>
  {{template "note-footer" .}}
</article>
{{end}}`
