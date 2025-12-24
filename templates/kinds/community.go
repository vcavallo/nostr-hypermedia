package kinds

// Community renders kind 34550 community definition events (NIP-72)
var Community = `{{define "render-community"}}
<article class="note community-note" id="note-{{.ID}}" aria-label="Community by {{displayName .AuthorProfile .NpubShort}}">
  {{template "author-header" .}}
  <div class="community-card">
    {{if .CommunityImage}}
    <img src="{{.CommunityImage}}" alt="{{.CommunityName}}" class="community-image" loading="lazy">
    {{end}}
    <div class="community-info">
      <h3 class="community-name">{{.CommunityName}}</h3>
      {{if .CommunityDescription}}<p class="community-description">{{.CommunityDescription}}</p>{{end}}
      <div class="community-meta">
        {{if .CommunityModerators}}
        <span class="community-moderators">{{i18n "label.moderators"}}: {{len .CommunityModerators}}</span>
        {{end}}
      </div>
    </div>
  </div>
  {{template "note-footer" .}}
</article>
{{end}}`
