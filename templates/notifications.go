package templates

// Notifications template - list of notifications (mentions, replies, reactions).

func GetNotificationsTemplate() string {
	return notificationsContent
}

var notificationsContent = `{{define "content"}}
<ul id="notes-list" class="notification-list">
{{range .Items}}
<li class="notification-item">
  <header class="notification-header">
    <span class="notification-icon">{{.TypeIcon}}</span>
    <div class="notification-meta">
      <a href="/html/profile/{{.AuthorNpub}}" class="notification-author" rel="author">{{displayName .AuthorProfile .AuthorNpubShort}}</a>
      <span class="notification-action">{{.TypeLabel}}</span>
      <time class="notification-time" datetime="{{isoTime .Event.CreatedAt}}">{{.TimeAgo}}</time>
    </div>
  </header>
  {{if .ContentHTML}}
  <div class="notification-content">{{.ContentHTML}}</div>
  {{end}}
  {{template "quoted-note" .}}
  {{if .TargetContentHTML}}
  <div class="notification-target-content">{{.TargetContentHTML}}</div>
  {{end}}
  {{if .TargetEventID}}
  <a href="/html/thread/{{.TargetEventID}}" class="notification-link" rel="related">{{i18n "nav.view_note"}} →</a>
  {{else if .Event}}
  <a href="/html/thread/{{.Event.ID}}" class="notification-link" rel="related">{{i18n "nav.view_note"}} →</a>
  {{end}}
</li>
{{end}}
</ul>
{{if not .Items}}
<div class="empty-state">
  <div class="empty-state-icon">🔔</div>
  <p>{{i18n "msg.no_notifications"}}</p>
  <p class="empty-state-hint">When people mention you, reply to you, or react to your notes, you'll see it here.</p>
</div>
{{end}}
{{template "pagination" .}}
{{end}}`
