package kinds

// Audio renders kind 32123 (NOM - Nostr Open Media audio tracks).
var Audio = `{{define "render-audio"}}
<article class="note audio-note" id="note-{{.ID}}"{{if .IsScrollTarget}} data-scroll-target{{end}} aria-label="Audio track by {{displayName .AuthorProfile .NpubShort}}">
  {{template "author-header" .}}
  {{template "content-warning-start" .}}
  <div class="audio-track">
    {{if .AudioThumbnail}}
    <div class="audio-artwork">
      <img src="{{.AudioThumbnail}}" alt="{{.AudioTitle}}" class="audio-artwork-img" loading="lazy">
    </div>
    {{end}}
    <div class="audio-info">
      {{if .AudioTitle}}<h3 class="audio-title">{{if .AudioPageURL}}<a href="{{.AudioPageURL}}" target="_blank" rel="noopener">{{.AudioTitle}}</a>{{else}}{{.AudioTitle}}{{end}}</h3>{{end}}
      {{if .AudioCreator}}<div class="audio-creator">{{.AudioCreator}}{{if .AudioDuration}} · {{.AudioDuration}}{{end}}</div>{{end}}
    </div>
    {{if .AudioURL}}
    <audio class="audio-player" controls preload="metadata">
      <source src="{{.AudioURL}}"{{if .AudioMimeType}} type="{{.AudioMimeType}}"{{end}}>
      {{i18n "msg.browser_not_supported"}} audio playback.
    </audio>
    {{else}}
    <div class="audio-placeholder">
      <span>🎵 {{i18n "msg.audio_not_available"}}</span>
    </div>
    {{end}}
  </div>
  {{template "content-warning-end" .}}
  {{template "note-footer" .}}
</article>
{{end}}`
