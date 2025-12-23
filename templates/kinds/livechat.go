package kinds

// LiveChatTemplate renders kind 1311 live chat messages (NIP-53)
const LiveChatTemplate = `
{{define "render-live-chat"}}
<article class="note live-chat-note" id="note-{{.ID}}" aria-label="Chat message by {{displayName .AuthorProfile .NpubShort}}">
  {{template "author-header" .}}
  <div class="live-chat-message">
    <div class="live-chat-content">{{.Content}}</div>
    {{if .LiveEventRef}}
    <div class="live-chat-context">
      <span class="live-chat-icon">📺</span>
      <span>{{i18n "label.chat_in"}}</span>
      <a href="/thread/{{noteLink .LiveEventRef}}" h-get h-target="#page-content" h-swap="inner" h-push-url h-prefetch class="live-chat-event-link">{{if .LiveEventTitle}}{{.LiveEventTitle}}{{else}}{{i18n "msg.live_event"}}{{end}}</a>
    </div>
    {{end}}
    {{if .ReplyToID}}
    <div class="live-chat-reply-indicator">
      <span class="live-chat-reply-icon">↩</span>
      <a href="/thread/{{noteLink .ReplyToID}}" h-get h-target="#page-content" h-swap="inner" h-push-url h-prefetch class="live-chat-reply-link">{{i18n "nav.view_thread"}}</a>
    </div>
    {{end}}
  </div>
  {{template "note-footer" .}}
</article>
{{end}}
`
