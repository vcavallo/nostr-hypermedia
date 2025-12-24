package kinds

// Recommendation template for NIP-89 Handler Recommendation events (kind 31989).
// Compact inline format: [avatar] Name recommended AppName for Kind X · time ago
var Recommendation = `{{define "render-recommendation"}}
<article class="note recommendation-compact" id="note-{{.ID}}"{{if .IsScrollTarget}} data-scroll-target{{end}} aria-label="Recommendation by {{displayName .AuthorProfile .NpubShort}}">
  <div class="recommendation-line">
    <a href="/profile/{{.Npub}}" class="recommendation-author" rel="author">
      <img src="{{if and .AuthorProfile .AuthorProfile.Picture}}{{.AuthorProfile.Picture}}{{else}}/static/avatar.jpg{{end}}" alt="" class="recommendation-author-avatar" loading="lazy">
      <span class="recommendation-author-name">{{displayName .AuthorProfile .NpubShort}}</span>
    </a>
    {{if and .RecommendedHandler (eq .Pubkey .RecommendedHandler.Pubkey)}}
    <span class="recommendation-verb">published a handler</span>
    {{else}}
    <span class="recommendation-verb">recommended</span>
    {{if .RecommendedHandler}}
    <a href="{{buildURL "/timeline" "kinds" "31990" "authors" .RecommendedHandler.Pubkey}}" class="recommendation-handler-link">
      {{if .RecommendedHandler.Picture}}<img src="{{.RecommendedHandler.Picture}}" alt="" class="recommendation-handler-icon" loading="lazy">{{end}}
      <span class="recommendation-handler-name">{{if .RecommendedHandler.Name}}{{.RecommendedHandler.Name}}{{else if .RecommendedHandler.DTag}}{{.RecommendedHandler.DTag}}{{else}}{{i18n "msg.unknown_handler"}}{{end}}</span>
    </a>
    {{else}}
    <span class="recommendation-handler-unknown">{{i18n "msg.unknown_handler"}}</span>
    {{end}}
    {{end}}
    {{if .RecommendedForKind}}
    <span class="recommendation-for-label">for</span>
    <span class="recommendation-kind-badge">Kind {{.RecommendedForKind}}</span>
    {{end}}
    <span class="recommendation-time">· {{timeAgo .CreatedAt}}</span>
  </div>
  {{if .Content}}
  <p class="recommendation-reason">{{.Content}}</p>
  {{end}}
</article>
{{end}}`
