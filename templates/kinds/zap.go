package kinds

// Template renders kind 9735 (zap receipt).
var Zap = `{{define "render-zap"}}
<article class="note zap-receipt" id="note-{{.ID}}" aria-label="Zap from {{displayName .ZapSenderProfile .ZapSenderNpubShort}} to {{displayName .ZapRecipientProfile .ZapRecipientNpubShort}}">
  <div class="zap-content">
    <span class="zap-icon">⚡</span>
    <div class="zap-info">
      <div class="zap-header">
        <a href="/profile/{{.ZapSenderNpub}}" class="zap-sender" rel="author">{{displayName .ZapSenderProfile .ZapSenderNpubShort}}</a>
        <span class="zap-action">{{i18n "label.zapped"}}</span>
        <a href="/profile/{{.ZapRecipientNpub}}" class="zap-recipient" rel="author">{{displayName .ZapRecipientProfile .ZapRecipientNpubShort}}</a>
      </div>
      <div class="zap-amount">{{.ZapAmountSats}} sats</div>
      {{if .ZapComment}}<div class="zap-comment">{{.ZapComment}}</div>{{end}}
      {{if .ZappedEventID}}<div class="zap-target"><a href="/thread/{{noteLink .ZappedEventID}}" h-get h-target="#page-content" h-swap="inner" h-push-url h-scroll="top" h-prefetch class="text-link" rel="related">{{i18n "nav.view_zapped_note"}}</a></div>{{end}}
    </div>
  </div>
  <div class="note-meta">
    <time datetime="{{isoTime .CreatedAt}}">{{formatTime .CreatedAt}}</time>
  </div>
</article>
{{end}}`
