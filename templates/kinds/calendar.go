package kinds

// Template renders kind 31922 (date-based calendar event) and 31923 (time-based calendar event) - NIP-52.
var Calendar = `{{define "render-calendar"}}
<article class="note calendar-event" id="note-{{.ID}}"{{if .IsScrollTarget}} data-scroll-target{{end}} aria-label="Event by {{displayName .AuthorProfile .NpubShort}}">
  <div class="calendar-event-header">
    <div class="calendar-date-badge">
      {{if .CalendarStartDate}}
      <span class="calendar-month">{{.CalendarStartMonth}}</span>
      <span class="calendar-day">{{.CalendarStartDay}}</span>
      {{else}}
      <span class="calendar-icon">📅</span>
      {{end}}
    </div>
    <div class="calendar-event-info">
      <h3 class="calendar-event-title">{{if .Title}}{{.Title}}{{else}}{{i18n "msg.untitled"}} Event{{end}}</h3>
      <div class="calendar-event-time">
        {{if .CalendarIsAllDay}}
        <span class="calendar-all-day">{{i18n "label.all_day"}}</span>
        {{else if .CalendarStartTime}}
        <span class="calendar-time">{{.CalendarStartTime}}{{if .CalendarEndTime}} - {{.CalendarEndTime}}{{end}}</span>
        {{end}}
        {{if and .CalendarEndDate (ne .CalendarEndDate .CalendarStartDate)}}
        <span class="calendar-end-date">→ {{.CalendarEndMonth}} {{.CalendarEndDay}}</span>
        {{end}}
      </div>
    </div>
  </div>
  {{if .CalendarImage}}
  <div class="calendar-event-image">
    <img src="{{.CalendarImage}}" alt="Event image" loading="lazy">
  </div>
  {{end}}
  <div class="calendar-event-body">
    {{if .Summary}}<p class="calendar-event-summary">{{.Summary}}</p>{{end}}
    {{if .Content}}<div class="calendar-event-description">{{.ContentHTML}}</div>{{end}}
    {{if .CalendarLocation}}
    <div class="calendar-event-location">
      <span class="calendar-location-icon">📍</span>
      <span>{{.CalendarLocation}}</span>
    </div>
    {{end}}
    {{if .CalendarParticipants}}
    <div class="calendar-event-participants">
      <span class="calendar-participants-label">{{i18n "label.participants"}}:</span>
      {{range .CalendarParticipants}}
      <a href="/profile/{{.Npub}}" class="calendar-participant" rel="author">
        {{if and .Profile .Profile.Picture}}<img src="{{avatarURL .Profile.Picture}}" alt="{{displayName .Profile .NpubShort}}'s avatar" class="calendar-participant-avatar" loading="lazy">{{end}}
        <span>{{displayName .Profile .NpubShort}}</span>
      </a>
      {{end}}
    </div>
    {{end}}
    {{if .CalendarHashtags}}
    <div class="calendar-event-tags">
      {{range .CalendarHashtags}}<a href="{{buildURL "/search" "q" (print "#" .)}}" class="calendar-hashtag">#{{.}}</a>{{end}}
    </div>
    {{end}}
  </div>
  <div class="calendar-event-meta">
    <div class="calendar-event-author">
      <a href="/profile/{{.Npub}}" rel="author">
        <img class="calendar-author-avatar" src="{{if and .AuthorProfile .AuthorProfile.Picture}}{{avatarURL .AuthorProfile.Picture}}{{else}}/static/avatar.jpg{{end}}" alt="{{displayName .AuthorProfile "User"}}'s avatar" loading="lazy">
      </a>
      <a href="/profile/{{.Npub}}" class="calendar-author-name" rel="author">{{displayName .AuthorProfile .NpubShort}}</a>
    </div>
    <time class="calendar-posted-time" datetime="{{isoTime .CreatedAt}}">{{i18n "label.posted"}} {{formatTime .CreatedAt}}</time>
  </div>
  {{template "note-footer" .}}
</article>
{{end}}`
