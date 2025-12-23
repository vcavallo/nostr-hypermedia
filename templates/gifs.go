package templates

// GIF picker templates for searching and selecting GIFs.
// Supports both full-page (no-JS) and inline panel (HelmJS) modes.

// GetGifsPageTemplate returns the full GIF search page template.
func GetGifsPageTemplate() string {
	return gifsPageContent
}

// GetGifPanelTemplate returns the inline GIF picker panel template.
func GetGifPanelTemplate() string {
	return gifPanelTemplate
}

// GetGifResultsTemplate returns the GIF results grid template.
func GetGifResultsTemplate() string {
	return gifResultsTemplate
}

// GetGifAttachmentTemplate returns the selected GIF preview template.
func GetGifAttachmentTemplate() string {
	return gifAttachmentTemplate
}

// gifsPageContent is the full-page GIF search (no-JS fallback).
var gifsPageContent = `{{define "content"}}
<div class="gifs-page">
  <h2>Search GIFs</h2>
  <form action="/gifs" method="GET" class="gif-search-form">
    <label for="gif-search-input" class="sr-only">Search GIFs</label>
    <input type="search" id="gif-search-input" name="q" value="{{.Query}}" placeholder="Search for GIFs..." class="gif-search-input" autocomplete="off" autofocus>
    <button type="submit">{{i18n "btn.search"}}</button>
  </form>

  <div class="gif-results">
  {{if .Query}}
    {{if .Results}}
      {{range .Results}}
      <a href="{{buildURL "/compose" "media_url" .URL "media_thumb" .ThumbURL}}" class="gif-item">
        <img src="{{.ThumbURL}}" alt="{{.Title}}" loading="lazy">
      </a>
      {{end}}
    {{else}}
      <div class="gif-empty">
        <p>No GIFs found for "{{.Query}}"</p>
        <p class="gif-empty-hint">Try different keywords.</p>
      </div>
    {{end}}
  {{else}}
    <div class="gif-empty">
      <p>Search for GIFs</p>
      <p class="gif-empty-hint">Enter a search term above to find GIFs.</p>
    </div>
  {{end}}
  </div>
</div>
{{end}}`

// gifPanelTemplate is the inline GIF picker panel for HelmJS.
var gifPanelTemplate = `{{define "gif-panel"}}
<aside class="gif-panel" aria-label="GIF picker">
  <header class="gif-panel-header">
    <form action="/gifs/search" method="GET" class="gif-search-form" h-get h-target="#gif-results-{{.TargetID}}" h-swap="inner" h-indicator="#gif-results-{{.TargetID}}">
      <input type="hidden" name="target" value="{{.TargetID}}">
      <label for="gif-search-{{.TargetID}}" class="sr-only">Search GIFs</label>
      <input type="search" id="gif-search-{{.TargetID}}" name="q" placeholder="Search GIFs..." class="gif-search-input" autocomplete="off" autofocus>
      <button type="submit" class="sr-only">{{i18n "btn.search"}}</button>
    </form>
    <a href="{{buildURL "/gifs/close" "target" .TargetID}}" h-get h-target="#gif-panel-{{.TargetID}}" h-swap="inner" class="gif-close-btn" aria-label="Close GIF picker">&#10005;</a>
  </header>
  <section class="gif-results" id="gif-results-{{.TargetID}}" aria-label="GIF search results"></section>
</aside>
{{end}}`

// gifResultsTemplate renders the GIF search results grid.
// href points to select endpoint; handler redirects to compose for no-JS, returns fragment for HelmJS
var gifResultsTemplate = `{{define "gif-results"}}
{{if .Results}}
  {{range .Results}}
  <a href="{{buildURL "/gifs/select" "target" $.TargetID "url" .URL "thumb" .ThumbURL}}"
     h-get
     h-target="#gif-attachment-{{$.TargetID}}"
     h-swap="inner"
     class="gif-item">
    <img src="{{.ThumbURL}}" alt="{{.Title}}" loading="lazy">
  </a>
  {{end}}
{{else if .Query}}
  <div class="gif-empty">
    <p>No GIFs found for "{{.Query}}"</p>
  </div>
{{end}}
{{end}}`

// gifAttachmentTemplate renders the selected GIF preview with remove option.
var gifAttachmentTemplate = `{{define "gif-attachment"}}
<input type="hidden" name="gif_url" value="{{.URL}}">
<div class="gif-preview">
  <img src="{{.ThumbURL}}" alt="Selected GIF" loading="lazy">
  <span class="gif-url">{{truncateURL .URL 40}}</span>
  <a href="{{buildURL "/gifs/clear" "target" .TargetID}}" h-get h-target="#gif-attachment-{{.TargetID}}" h-swap="inner" class="gif-remove-btn" aria-label="Remove GIF">&#10005;</a>
</div>
<div id="gif-panel-{{.TargetID}}" h-oob="inner"></div>
{{end}}`
