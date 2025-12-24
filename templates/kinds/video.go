package kinds

// Template renders kind 30 (long-form horizontal video - NIP-71).
var Video = `{{define "render-video"}}
<article class="note" id="note-{{.ID}}"{{if .IsScrollTarget}} data-scroll-target{{end}} aria-label="Video by {{displayName .AuthorProfile .NpubShort}}">
  {{template "author-header" .}}
  {{template "content-warning-start" .}}
  <div class="video-note">
    {{if .VideoTitle}}<h3 class="video-title">{{.VideoTitle}}</h3>{{end}}
    <div class="video-player-container">
      {{if .VideoURL}}
      <video class="video-player" controls preload="metadata"{{if .VideoThumbnail}} poster="{{.VideoThumbnail}}"{{end}}>
        <source src="{{.VideoURL}}"{{if .VideoMimeType}} type="{{.VideoMimeType}}"{{end}}>
        {{i18n "msg.browser_not_supported"}} video playback.
      </video>
      {{else if .VideoThumbnail}}
      <img src="{{.VideoThumbnail}}" alt="Video thumbnail" class="video-thumbnail" loading="lazy">
      {{else}}
      <div class="video-placeholder">
        <span>▶ {{i18n "msg.video_not_available"}}</span>
      </div>
      {{end}}
    </div>
    {{if .VideoDuration}}<div class="video-duration">{{.VideoDuration}}</div>{{end}}
    {{if .Content}}<div class="video-description">{{.ContentHTML}}</div>{{end}}
    {{if .VideoHashtags}}
    <div class="video-tags">
      {{range .VideoHashtags}}<a href="{{buildURL "/search" "q" (print "#" .)}}" class="video-hashtag">#{{.}}</a>{{end}}
    </div>
    {{end}}
  </div>
  {{template "content-warning-end" .}}
  {{template "note-footer" .}}
</article>
{{end}}`
