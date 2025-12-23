package templates

// Profile template - user profile header, edit form, and notes list.

func GetProfileTemplate() string {
	return profileContent
}

var profileContent = `{{define "content"}}
<div class="profile-header">
  <img class="profile-avatar" src="{{if and .Profile .Profile.Picture}}{{avatarURL .Profile.Picture}}{{else}}/static/avatar.jpg{{end}}" alt="{{displayName .Profile "User"}}'s avatar" loading="lazy">
  <div class="profile-info">
    <div class="profile-name-row">
      <div class="profile-name">{{displayName .Profile .NpubShort}}</div>
    </div>
    {{if and .Profile .Profile.NIP05Verified}}
    <div class="profile-nip05"><a href="{{nip05URL .Profile.Nip05}}" target="_blank" rel="noopener" class="nip05-verified">{{nip05Badge}} {{.Profile.NIP05Domain}}</a></div>
    {{else if and .Profile .Profile.Nip05}}
    <div class="profile-nip05">{{.Profile.Nip05}}</div>
    {{end}}
    <div class="profile-npub" title="{{.Pubkey}}">{{.NpubShort}}</div>
    {{if and .Profile .Profile.About}}
    <div class="profile-about">{{.Profile.About}}</div>
    {{end}}
    {{if or (and .LoggedIn (not .IsSelf)) (and .LoggedIn .IsSelf (not .EditMode))}}
    <div class="profile-actions">
      {{if and .LoggedIn (not .IsSelf)}}
      <span id="follow-btn-{{.Pubkey}}">
      <form method="POST" action="/follow" class="inline-form" h-post h-target="#follow-btn-{{.Pubkey}}" h-swap="inner" h-indicator="#follow-spinner-{{.Pubkey}}"{{if .IsFollowing}} h-confirm="Unfollow this user?"{{end}}>
        <input type="hidden" name="csrf_token" value="{{.CSRFToken}}">
        <input type="hidden" name="pubkey" value="{{.Pubkey}}">
        <input type="hidden" name="return_url" value="{{.CurrentURL}}">
        {{if .IsFollowing}}
        <input type="hidden" name="action" value="unfollow">
        <button type="submit" class="follow-btn unfollow">{{i18n "btn.unfollow"}} <span id="follow-spinner-{{.Pubkey}}" class="h-indicator"><span class="h-spinner"></span></span></button>
        {{else}}
        <input type="hidden" name="action" value="follow">
        <button type="submit" class="follow-btn follow">{{i18n "btn.follow"}} <span id="follow-spinner-{{.Pubkey}}" class="h-indicator"><span class="h-spinner"></span></span></button>
        {{end}}
      </form>
      </span>
      <span id="mute-btn-{{.Pubkey}}">
      <form method="POST" action="/mute" class="inline-form" h-post h-target="#mute-btn-{{.Pubkey}}" h-swap="inner" h-indicator="#mute-spinner-{{.Pubkey}}"{{if .IsMuted}} h-confirm="Unmute this user?"{{else}} h-confirm="Mute this user? Their content will be hidden from your timeline."{{end}}>
        <input type="hidden" name="csrf_token" value="{{.CSRFToken}}">
        <input type="hidden" name="pubkey" value="{{.Pubkey}}">
        <input type="hidden" name="return_url" value="{{.CurrentURL}}">
        {{if .IsMuted}}
        <input type="hidden" name="action" value="unmute">
        <button type="submit" class="mute-btn unmute">{{i18n "action.unmute"}} <span id="mute-spinner-{{.Pubkey}}" class="h-indicator"><span class="h-spinner"></span></span></button>
        {{else}}
        <input type="hidden" name="action" value="mute">
        <button type="submit" class="mute-btn mute">{{i18n "action.mute"}} <span id="mute-spinner-{{.Pubkey}}" class="h-indicator"><span class="h-spinner"></span></span></button>
        {{end}}
      </form>
      </span>
      {{end}}
      {{if and .LoggedIn .IsSelf (not .EditMode)}}
      <a href="/profile/edit" class="edit-profile-btn">{{i18n "label.edit_profile"}}</a>
      {{end}}
    </div>
    {{end}}
  </div>
</div>

{{if .EditMode}}
<div class="edit-form-section">
  <h3>{{i18n "label.edit_profile"}}</h3>
  {{if .Error}}
  <div class="edit-form-error" role="alert">{{.Error}}</div>
  {{end}}
  {{if .Success}}
  <div class="flash-message" role="status" aria-live="polite">{{.Success}}</div>
  {{end}}
  <form method="POST" action="/profile/edit">
    <input type="hidden" name="csrf_token" value="{{.CSRFToken}}">
    <input type="hidden" name="raw_content" value="{{.RawContent}}">
    <div class="edit-form-group">
      <label for="display_name">{{i18n "label.display_name"}}</label>
      <input type="text" id="display_name" name="display_name" value="{{if .Profile}}{{.Profile.DisplayName}}{{end}}" placeholder="Your display name">
    </div>
    <div class="edit-form-group">
      <label for="name">{{i18n "label.username"}}</label>
      <input type="text" id="name" name="name" value="{{if .Profile}}{{.Profile.Name}}{{end}}" placeholder="username">
      <div class="edit-form-hint">Short identifier (lowercase, no spaces)</div>
    </div>
    <div class="edit-form-group">
      <label for="about">{{i18n "label.about"}}</label>
      <textarea id="about" name="about" placeholder="Tell us about yourself">{{if .Profile}}{{.Profile.About}}{{end}}</textarea>
    </div>
    <div class="edit-form-group">
      <label for="picture">{{i18n "label.picture_url"}}</label>
      <input type="url" id="picture" name="picture" value="{{if .Profile}}{{.Profile.Picture}}{{end}}" placeholder="https://example.com/avatar.jpg">
      {{if and .Profile .Profile.Picture}}<div class="edit-form-preview"><img src="{{avatarURL .Profile.Picture}}" alt="Current profile picture" class="edit-form-preview-img" loading="lazy"></div>{{end}}
      <div class="edit-form-hint">Current picture shown above. Changes appear after saving.</div>
    </div>
    <div class="edit-form-group">
      <label for="banner">{{i18n "label.banner_url"}}</label>
      <input type="url" id="banner" name="banner" value="{{if .Profile}}{{.Profile.Banner}}{{end}}" placeholder="https://example.com/banner.jpg">
    </div>
    <div class="edit-form-group">
      <label for="nip05">{{i18n "label.nip05"}}</label>
      <input type="text" id="nip05" name="nip05" value="{{if .Profile}}{{.Profile.Nip05}}{{end}}" placeholder="you@example.com">
      <div class="edit-form-hint">Verified identifier (like user@domain.com)</div>
    </div>
    <div class="edit-form-group">
      <label for="lud16">{{i18n "label.lightning_address"}}</label>
      <input type="text" id="lud16" name="lud16" value="{{if .Profile}}{{.Profile.Lud16}}{{end}}" placeholder="you@getalby.com">
      <div class="edit-form-hint">For receiving zaps</div>
    </div>
    <div class="edit-form-group">
      <label for="website">{{i18n "label.website"}}</label>
      <input type="url" id="website" name="website" value="{{if .Profile}}{{.Profile.Website}}{{end}}" placeholder="https://yourwebsite.com">
    </div>
    <div class="edit-form-buttons">
      <button type="submit" class="submit-btn">{{i18n "btn.save_profile"}}</button>
      <a href="/profile/{{.Npub}}" class="edit-form-btn edit-form-btn-secondary">{{i18n "btn.cancel"}}</a>
    </div>
  </form>
</div>
{{else}}
<section class="notes-section" id="notes-list">
  {{range .Items}}
  <article class="note" aria-label="Note by {{displayName $.Profile $.NpubShort}}">
    <div class="note-author">
      <a href="/profile/{{$.Npub}}" class="text-muted" rel="author">
      <img class="author-avatar" src="{{if and $.Profile $.Profile.Picture}}{{avatarURL $.Profile.Picture}}{{else}}/static/avatar.jpg{{end}}" alt="{{displayName $.Profile "User"}}'s avatar" loading="lazy">
      </a>
      <div class="author-info">
        <a href="/profile/{{$.Npub}}" class="text-muted" rel="author">
        <span class="author-name">{{displayName $.Profile $.NpubShort}}</span>
        </a>
        <time class="author-time" datetime="{{isoTime .CreatedAt}}">{{formatTime .CreatedAt}}</time>
      </div>
    </div>
    {{if .HasContentWarning}}<details class="content-warning">
      <summary class="content-warning-label">{{if .ContentWarning}}{{.ContentWarning}}{{else}}{{i18n "label.sensitive_content"}}{{end}}</summary>
      <div class="content-warning-body">{{end}}
    <div class="note-content">{{.ContentHTML}}</div>
    {{if .HasContentWarning}}</div>
    </details>{{end}}
    {{template "note-footer" .}}
  </article>
  {{end}}
</section>
{{if not .Items}}
<div class="empty-state">
  <div class="empty-state-icon">📝</div>
  <p>No notes yet</p>
  <p class="empty-state-hint">This user hasn't posted any notes.</p>
</div>
{{end}}
{{template "pagination" .}}
{{end}}
{{end}}`
