package kinds

// Template renders kind 30402 (classified listing - NIP-99).
var Classified = `{{define "render-classified"}}
<article class="note classified-listing{{if eq .ClassifiedStatus "sold"}} classified-sold{{end}}" id="note-{{.ID}}" aria-label="Listing by {{displayName .AuthorProfile .NpubShort}}">
  {{template "author-header" .}}
  {{template "content-warning-start" .}}
  {{if .ClassifiedImages}}
  <div class="classified-images">
    {{range .ClassifiedImages}}
    <img src="{{.}}" alt="Listing image" class="classified-image" loading="lazy">
    {{end}}
  </div>
  {{end}}
  <div class="classified-body">
    <div class="classified-header">
      <h3 class="classified-title">{{if .Title}}{{.Title}}{{else}}{{i18n "msg.untitled"}} Listing{{end}}</h3>
      {{if .ClassifiedPrice}}
      <div class="classified-price">{{.ClassifiedPrice}}</div>
      {{end}}
    </div>
    {{if .ClassifiedStatus}}
    <div class="classified-status classified-status-{{.ClassifiedStatus}}">{{.ClassifiedStatus}}</div>
    {{end}}
    {{if .Summary}}
    <p class="classified-summary">{{.Summary}}</p>
    {{end}}
    {{if .Content}}
    <div class="classified-description">{{.ContentHTML}}</div>
    {{end}}
    {{if .ClassifiedLocation}}
    <div class="classified-location">
      <span class="classified-location-icon">📍</span>
      <span>{{.ClassifiedLocation}}</span>
    </div>
    {{end}}
  </div>
  {{template "content-warning-end" .}}
  {{template "note-footer" .}}
</article>
{{end}}`
