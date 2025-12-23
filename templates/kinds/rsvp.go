package kinds

// RSVPTemplate renders kind 31925 calendar RSVP events (NIP-52)
const RSVPTemplate = `
{{define "render-rsvp"}}
<article class="note rsvp-note" id="note-{{.ID}}" aria-label="RSVP by {{displayName .AuthorProfile .NpubShort}}">
  {{template "author-header" .}}
  <div class="rsvp-card rsvp-{{.RSVPStatus}}">
    <div class="rsvp-header">
      <span class="rsvp-icon">{{if eq .RSVPStatus "accepted"}}✓{{else if eq .RSVPStatus "declined"}}✗{{else}}?{{end}}</span>
      <span class="rsvp-status-text">
        {{if eq .RSVPStatus "accepted"}}{{i18n "rsvp.accepted"}}{{else if eq .RSVPStatus "declined"}}{{i18n "rsvp.declined"}}{{else}}{{i18n "rsvp.tentative"}}{{end}}
      </span>
    </div>
    {{if .CalendarEventRef}}
    <div class="rsvp-event">
      <span class="rsvp-event-icon">📅</span>
      <a href="/thread/{{noteLink .CalendarEventRef}}" h-get h-target="#page-content" h-swap="inner" h-push-url h-prefetch class="rsvp-event-link">
        {{if .CalendarEventTitle}}{{.CalendarEventTitle}}{{else}}{{i18n "kind.event.label"}}{{end}}
      </a>
    </div>
    {{end}}
    {{if .RSVPFreebusy}}
    <div class="rsvp-freebusy">
      {{if eq .RSVPFreebusy "free"}}{{i18n "rsvp.marked_free"}}{{else}}{{i18n "rsvp.marked_busy"}}{{end}}
    </div>
    {{end}}
    {{if .Content}}
    <div class="rsvp-note">{{.Content}}</div>
    {{end}}
  </div>
  {{template "note-footer" .}}
</article>
{{end}}
`
