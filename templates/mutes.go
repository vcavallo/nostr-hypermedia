package templates

// Mutes template - muted users list with unmute buttons.

func GetMutesTemplate() string {
	return mutesContent
}

var mutesContent = `{{define "content"}}
<div class="mutes-list" id="mutes-list">
{{range .Items}}
<article class="note muted-user" id="muted-{{.Pubkey}}">
  <div class="note-author">
    <a href="/html/profile/{{.Npub}}" class="text-muted" rel="author">
    <img class="author-avatar" src="{{if and .Profile .Profile.Picture}}{{avatarURL .Profile.Picture}}{{else}}/static/avatar.jpg{{end}}" alt="{{displayName .Profile "User"}}'s avatar" loading="lazy">
    </a>
    <div class="author-info">
      <a href="/html/profile/{{.Npub}}" class="text-muted" rel="author">
      {{if and .Profile (or .Profile.DisplayName .Profile.Name)}}
      <span class="author-name">{{displayName .Profile .NpubShort}}</span>
      {{if .Profile.Nip05}}<span class="author-nip05">{{.Profile.Nip05}}</span>{{end}}
      {{else if and .Profile .Profile.Nip05}}
      <span class="author-nip05">{{.Profile.Nip05}}</span>
      {{else}}
      <span class="pubkey" title="{{.Pubkey}}">{{.NpubShort}}</span>
      {{end}}
      </a>
    </div>
    <form method="POST" action="/html/mute" class="inline-form mute-action" h-post h-target="#muted-{{.Pubkey}}" h-swap="outer" h-confirm="{{i18n "confirm.unmute"}}">
      <input type="hidden" name="csrf_token" value="{{$.CSRFToken}}">
      <input type="hidden" name="pubkey" value="{{.Pubkey}}">
      <input type="hidden" name="action" value="unmute">
      <input type="hidden" name="return_url" value="{{$.CurrentURL}}">
      <button type="submit" class="btn-unmute">{{i18n "action.unmute"}}</button>
    </form>
  </div>
</article>
{{end}}
</div>
{{if not .Items}}
<div class="empty-state">
  <div class="empty-state-icon">🔇</div>
  <p>{{i18n "msg.no_mutes"}}</p>
  <p class="empty-state-hint">When you mute someone, they'll appear here. Their content will be hidden from your timeline.</p>
</div>
{{end}}
{{end}}`
