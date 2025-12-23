package kinds

// Handler template for NIP-89 Application Handler events (kind 31990).
// Shows app info, supported kinds, and website link.
// Skips rendering if handler has no meaningful content (no name/about/picture/website).
var Handler = `{{define "render-handler"}}
{{if or .HandlerName .HandlerAbout .HandlerPicture .HandlerWebsite}}
<article class="note handler-card" id="note-{{.ID}}"{{if .IsScrollTarget}} data-scroll-target{{end}} aria-label="App handler by {{displayName .AuthorProfile .NpubShort}}">
  {{template "author-header" .}}
  <div class="handler-info">
    {{if .HandlerPicture}}<img src="{{.HandlerPicture}}" alt="" class="handler-app-icon" loading="lazy">{{end}}
    <div class="handler-details">
      {{if .HandlerName}}<h3 class="handler-name">{{.HandlerName}}</h3>{{end}}
      {{if .HandlerAbout}}<p class="handler-about">{{.HandlerAbout}}</p>{{end}}
    </div>
  </div>
  {{if .HandlerKinds}}
  <div class="handler-kinds">
    <span class="handler-kinds-label">Handles:</span>
    {{range .HandlerKinds}}<span class="handler-kind-badge">Kind {{.}}</span>{{end}}
  </div>
  {{end}}
  {{if .HandlerWebsite}}
  <a href="{{.HandlerWebsite}}" target="_blank" rel="noopener" class="handler-website-link">{{.HandlerWebsite}}</a>
  {{end}}
  {{template "note-footer" .}}
</article>
{{end}}
{{end}}`
