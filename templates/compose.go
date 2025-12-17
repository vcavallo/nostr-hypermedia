package templates

// Compose template - compose page for posting with media attachments.
// Used as no-JS fallback when selecting media (GIFs, images, etc.).

// GetComposeTemplate returns the compose page template.
func GetComposeTemplate() string {
	return composeContent
}

// composeContent is the compose page for posting with media attachments.
var composeContent = `{{define "content"}}
<div class="compose-page">
  <h2>Compose Note</h2>

  {{if .MediaURL}}
  <div class="compose-media-preview">
    <img src="{{if .MediaThumb}}{{.MediaThumb}}{{else}}{{.MediaURL}}{{end}}" alt="Attached media" loading="lazy">
    <span class="compose-media-url">{{truncateURL .MediaURL 50}}</span>
  </div>
  {{end}}

  <form method="POST" action="/html/post" class="compose-form">
    <input type="hidden" name="csrf_token" value="{{.CSRFToken}}">
    {{if .MediaURL}}
    <input type="hidden" name="gif_url" value="{{.MediaURL}}">
    {{end}}
    <label for="compose-content" class="sr-only">Write your note</label>
    <textarea id="compose-content" name="content" placeholder="What's on your mind?" autofocus></textarea>
    <div class="compose-actions">
      <a href="/html/gifs" class="compose-change-media">Change GIF</a>
      <button type="submit">{{i18n "btn.post"}}</button>
    </div>
  </form>
</div>
{{end}}`
