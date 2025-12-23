package kinds

// Template renders kind 22 (short-form vertical video - NIP-71).
var Shortvideo = `{{define "render-shortvideo"}}
<article class="note" id="note-{{.ID}}"{{if .IsScrollTarget}} data-scroll-target{{end}} aria-label="Video by {{displayName .AuthorProfile .NpubShort}}">
  {{template "author-header" .}}
  {{template "content-warning-start" .}}
  <div class="shortvideo-note">
    {{if .VideoTitle}}<div class="shortvideo-title">{{.VideoTitle}}</div>{{end}}
    <div class="shortvideo-player-container">
      {{if .VideoURL}}
      <video class="shortvideo-player" controls playsinline preload="metadata"{{if .VideoThumbnail}} poster="{{.VideoThumbnail}}"{{end}}>
        <source src="{{.VideoURL}}"{{if .VideoMimeType}} type="{{.VideoMimeType}}"{{end}}>
        {{i18n "msg.browser_not_supported"}} video playback.
      </video>
      {{else if .VideoThumbnail}}
      <img src="{{.VideoThumbnail}}" alt="Video thumbnail" class="shortvideo-thumbnail" loading="lazy">
      {{else}}
      <div class="shortvideo-placeholder">
        <span>▶ {{i18n "msg.video_not_available"}}</span>
      </div>
      {{end}}
    </div>
    {{if .Content}}<div class="shortvideo-caption">{{.ContentHTML}}</div>{{end}}
  </div>
  {{template "content-warning-end" .}}
  {{template "note-footer" .}}
</article>
{{end}}`
