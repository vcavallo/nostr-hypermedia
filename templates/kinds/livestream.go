package kinds

// Template renders kind 30311 (live event).
var Livestream = `{{define "render-livestream"}}
<article class="note live-event" id="note-{{.ID}}" aria-label="Live event">
  <div class="live-event-thumbnail">
    {{if .LiveImage}}
    <img src="{{.LiveImage}}" alt="{{.LiveTitle}}" loading="lazy">
    {{else}}
    <div class="live-event-thumbnail-placeholder"><span>{{i18n "status.live"}}</span></div>
    {{end}}
    <div class="live-event-overlay">
      {{if eq .LiveStatus "live"}}
      <span class="live-badge live">{{i18n "status.live"}}</span>
      {{else if eq .LiveStatus "planned"}}
      <span class="live-badge planned">{{i18n "status.scheduled"}}</span>
      {{else if eq .LiveStatus "ended"}}
      <span class="live-badge ended">{{i18n "status.ended"}}</span>
      {{else}}
      <span class="live-badge">{{.LiveStatus}}</span>
      {{end}}
      {{if .LiveCurrentCount}}<span class="live-viewers">{{.LiveCurrentCount}} {{i18n "status.watching"}}</span>{{end}}
    </div>
  </div>

  <div class="live-event-body">
    <h3 class="live-event-title">{{if .LiveTitle}}{{.LiveTitle}}{{else}}{{i18n "msg.live_event"}}{{end}}</h3>
    {{if .LiveSummary}}<p class="live-event-summary">{{.LiveSummary}}</p>{{end}}

    {{if .LiveParticipants}}
    <div class="live-event-host">
      {{range .LiveParticipants}}{{if eq .Role "host"}}
      <span class="host-label">{{i18n "label.host"}}</span>
      <a href="/profile/{{.Npub}}" class="host-link" title="{{.NpubShort}}" rel="author">
        {{if and .Profile .Profile.Picture}}<img class="host-avatar" src="{{avatarURL .Profile.Picture}}" alt="{{displayName .Profile "Host"}}'s avatar" loading="lazy">{{end}}
        <span class="host-name">{{displayName .Profile .NpubShort}}</span>
      </a>
      {{end}}{{end}}
    </div>
    {{end}}

    <div class="live-event-meta">
      {{if .LiveStarts}}
      <span class="live-event-meta-item">{{if eq .LiveStatus "ended"}}{{i18n "time.started"}}{{else if eq .LiveStatus "live"}}{{i18n "time.started"}}{{else}}{{i18n "time.starts"}}{{end}} {{formatTime .LiveStarts}}</span>
      {{end}}
      {{if and .LiveEnds (eq .LiveStatus "ended")}}
      <span class="live-event-meta-item">{{i18n "time.ended"}} {{formatTime .LiveEnds}}</span>
      {{end}}
    </div>

    {{if .LiveHashtags}}
    <div class="live-event-tags">
      {{range .LiveHashtags}}<span class="live-hashtag">#{{.}}</span>{{end}}
    </div>
    {{end}}
  </div>

  <div class="live-event-actions">
    {{if .LiveEmbedURL}}
    <a href="{{.LiveEmbedURL}}" class="live-action-btn stream-btn" target="_blank" rel="external noopener">{{i18n "nav.watch_zap_stream"}}</a>
    {{else if and .LiveStreamingURL (ne .LiveStatus "ended")}}
    <a href="{{.LiveStreamingURL}}" class="live-action-btn stream-btn" target="_blank" rel="external noopener">{{i18n "nav.watch_stream"}}</a>
    {{end}}
    {{if and .LiveRecordingURL (eq .LiveStatus "ended")}}
    <a href="{{.LiveRecordingURL}}" class="live-action-btn recording-btn" target="_blank" rel="external noopener">{{i18n "nav.watch_recording"}}</a>
    {{end}}
  </div>
</article>
{{end}}`
