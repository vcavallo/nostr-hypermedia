package templates

// Fragment template - renders just the content block without the base wrapper.
// Used for HelmJS partial page updates where only the main content needs to swap.
//
// This template defines a "fragment" entry point that renders the "content" block
// directly, without the full HTML document structure (head, nav, footer, etc).

// GetFragmentTemplate returns the fragment wrapper template.
// Parse together with content templates to enable fragment rendering.
func GetFragmentTemplate() string {
	return fragmentTemplate
}

// fragmentTemplate renders the page-content wrapper for HelmJS requests.
// Includes kind-filter submenu (if present), post form (if shown), and main content.
// The h-target="#page-content" in navigation links will swap this into the page.
// Also includes OOB update for main nav to sync active states.
var fragmentTemplate = `{{define "fragment"}}{{$site := siteConfig}}<title>{{.Title}} - {{$site.Site.Name}}</title>
{{if .KindFilters}}<div class="kind-filter" id="kind-filter">
  {{range .KindFilters}}{{if .IsDropdown}}<details class="kind-filter-dropdown{{if .Active}} active{{end}}">
    <summary>{{.Title}}</summary>
    <div class="kind-filter-dropdown-menu">
      {{range .Children}}<a href="{{.Href}}" h-get h-target="#page-content" h-swap="inner" h-replace-url h-scroll="top" h-indicator="#nav-loading" h-prefetch class="{{if .Active}}active{{end}}">{{.Title}}</a>{{end}}
    </div>
  </details>{{else}}<a href="{{.Href}}" h-get h-target="#page-content" h-swap="inner" h-replace-url h-scroll="top" h-indicator="#nav-loading" h-prefetch class="{{if .Active}}active{{end}}">{{.Title}}</a>{{end}}{{end}}
</div>{{end}}
{{if and .LoggedIn .ShowPostForm}}
<div class="post-form-container">
  <div id="post-error" class="form-error" role="alert" aria-live="polite"></div>
  <form method="POST" action="/post" class="post-form" id="post-form" h-post h-target="#post-form" h-swap="outer" h-indicator="#post-spinner" h-error-target="#post-error">
    <input type="hidden" name="csrf_token" value="{{.CSRFToken}}">
    <input type="hidden" id="mentions-data-post" name="mentions" value="{}">
    <label for="post-content" class="sr-only">Write a new note</label>
    <textarea id="post-content" name="content" placeholder="What's on your mind?"></textarea>
    <a href="{{buildURL "/mentions" "target" "post"}}" h-get h-target="#mentions-dropdown-post" h-swap="inner" h-trigger="input debounce:300 from:#post-content" h-include="#post-content" hidden aria-hidden="true" aria-label="Mention autocomplete trigger"></a>
    <div id="mentions-dropdown-post" class="mentions-dropdown"></div>
    <div id="gif-attachment-post"></div>
    <div class="post-actions">
      <button type="submit" class="btn-primary">{{i18n "btn.post"}} <span id="post-spinner" class="h-indicator"><span class="h-spinner"></span></span></button>
      {{if .ShowGifButton}}<a href="{{buildURL "/gifs" "target" "post"}}" h-get h-target="#gif-panel-post" h-swap="inner" class="btn-primary post-gif" title="Add GIF">Add GIF</a>{{end}}
      <details class="cw-dropdown">
        <summary class="cw-toggle" title="{{i18n "label.content_warning"}}">⚠️</summary>
        <div class="cw-options">
          <select name="content_warning" class="cw-select" aria-label="{{i18n "label.content_warning"}}">
            <option value="">{{i18n "option.no_warning"}}</option>
            <option value="nsfw">NSFW</option>
            <option value="spoiler">{{i18n "option.spoiler"}}</option>
            <option value="sensitive">{{i18n "option.sensitive"}}</option>
          </select>
          <input type="text" name="content_warning_custom" class="cw-custom" placeholder="{{i18n "placeholder.custom_warning"}}">
        </div>
      </details>
    </div>
  </form>
  <div id="gif-panel-post"></div>
</div>
{{end}}
<main id="main-content">
  <h1 class="sr-only">{{.Title}}</h1>
  {{template "content" .}}
</main>
{{template "nav-oob" .}}{{end}}

{{define "nav-oob"}}
<span id="feed-tabs" h-oob="morph">{{range .FeedModes}}{{if .IsDropdown}}<details class="feed-dropdown{{if .Active}} active{{end}}"><summary class="nav-tab{{if .Active}} active{{end}}">{{if .Icon}}{{.Icon}} {{end}}{{.Title}}</summary><div class="feed-dropdown-menu">{{range .Children}}<a href="{{.Href}}" h-get h-target="#page-content" h-swap="inner" h-push-url h-scroll="top" h-indicator="#nav-loading" class="feed-dropdown-item{{if .Active}} active{{end}}"{{if .Active}} aria-current="page"{{end}}>{{.Title}}</a>{{end}}</div></details>{{else if eq .IconOnly "always"}}<a href="{{.Href}}" h-get h-target="#page-content" h-swap="inner" h-push-url h-scroll="top" h-indicator="#nav-loading" class="nav-icon{{if .Active}} active{{end}}" title="{{.Title}}"{{if .Active}} aria-current="page"{{end}}>{{.Icon}}</a>{{else if eq .IconOnly "mobile"}}<a href="{{.Href}}" h-get h-target="#page-content" h-swap="inner" h-push-url h-scroll="top" h-indicator="#nav-loading" class="nav-tab{{if .Active}} active{{end}}"{{if .Active}} aria-current="page"{{end}}><span class="icon-mobile-only" title="{{.Title}}">{{.Icon}}</span><span class="icon-desktop-only">{{if .Icon}}{{.Icon}} {{end}}{{.Title}}</span></a>{{else if eq .IconOnly "desktop"}}<a href="{{.Href}}" h-get h-target="#page-content" h-swap="inner" h-push-url h-scroll="top" h-indicator="#nav-loading" class="nav-tab{{if .Active}} active{{end}}"{{if .Active}} aria-current="page"{{end}}><span class="icon-desktop-only" title="{{.Title}}">{{.Icon}}</span><span class="icon-mobile-only">{{if .Icon}}{{.Icon}} {{end}}{{.Title}}</span></a>{{else}}<a href="{{.Href}}" h-get h-target="#page-content" h-swap="inner" h-push-url h-scroll="top" h-indicator="#nav-loading" class="nav-tab{{if .Active}} active{{end}}"{{if .Active}} aria-current="page"{{end}}>{{if .Icon}}{{.Icon}} {{end}}{{.Title}}</a>{{end}}{{end}}</span>
<span id="login-btn" h-oob="morph">{{if not .LoggedIn}}<a href="/login" class="btn-primary">{{i18n "btn.login"}}</a>{{end}}</span>
{{if .LoggedIn}}<span class="notification-badge{{if not .HasUnreadNotifications}} notification-badge-hidden{{end}}" id="notification-badge" role="status"{{if .HasUnreadNotifications}} aria-label="New notifications"{{end}} h-oob="outer"></span>{{end}}
<a id="config-reload" href="{{.CurrentURL}}{{if contains .CurrentURL "?"}}&amp;{{else}}?{{end}}refresh=1" h-get h-target="body" h-swap="morph" h-select="body" h-trigger="h:sse-message from:#config-sse" hidden aria-hidden="true" aria-label="Reload page configuration" h-oob="morph"></a>
{{end}}`

// GetFooterFragmentTemplate returns a template for rendering just the note footer.
// Used for HelmJS partial updates after react/bookmark actions.
func GetFooterFragmentTemplate() string {
	return footerFragmentTemplate
}

// footerFragmentTemplate renders just the note-footer for HelmJS action responses.
var footerFragmentTemplate = `{{define "footer-fragment"}}{{template "note-footer" .}}{{end}}`

// GetFollowButtonTemplate returns a template for rendering just the follow button.
// Used for HelmJS partial updates after follow/unfollow actions.
func GetFollowButtonTemplate() string {
	return followButtonTemplate
}

// followButtonTemplate renders just the follow button form for HelmJS action responses.
var followButtonTemplate = `{{define "follow-button"}}
<form method="POST" action="/follow" class="inline-form" h-post h-target="#follow-btn-{{.Pubkey}}" h-swap="inner" h-indicator="#follow-spinner-{{.Pubkey}}"{{if .IsFollowing}} h-confirm="Unfollow this user?"{{end}}>
  <input type="hidden" name="csrf_token" value="{{.CSRFToken}}">
  <input type="hidden" name="pubkey" value="{{.Pubkey}}">
  <input type="hidden" name="return_url" value="{{.ReturnURL}}">
  {{if .IsFollowing}}
  <input type="hidden" name="action" value="unfollow">
  <button type="submit" class="follow-btn unfollow">{{i18n "btn.unfollow"}} <span id="follow-spinner-{{.Pubkey}}" class="h-indicator"><span class="h-spinner"></span></span></button>
  {{else}}
  <input type="hidden" name="action" value="follow">
  <button type="submit" class="follow-btn follow">{{i18n "btn.follow"}} <span id="follow-spinner-{{.Pubkey}}" class="h-indicator"><span class="h-spinner"></span></span></button>
  {{end}}
</form>
{{end}}`

// GetMuteButtonTemplate returns a template for rendering just the mute button.
// Used for HelmJS partial updates after mute/unmute actions.
func GetMuteButtonTemplate() string {
	return muteButtonTemplate
}

// muteButtonTemplate renders just the mute button form for HelmJS action responses.
var muteButtonTemplate = `{{define "mute-button"}}
<form method="POST" action="/mute" class="inline-form" h-post h-target="#mute-btn-{{.Pubkey}}" h-swap="inner" h-indicator="#mute-spinner-{{.Pubkey}}"{{if .IsMuted}} h-confirm="Unmute this user?"{{else}} h-confirm="Mute this user? Their content will be hidden from your timeline."{{end}}>
  <input type="hidden" name="csrf_token" value="{{.CSRFToken}}">
  <input type="hidden" name="pubkey" value="{{.Pubkey}}">
  <input type="hidden" name="return_url" value="{{.ReturnURL}}">
  {{if .IsMuted}}
  <input type="hidden" name="action" value="unmute">
  <button type="submit" class="mute-btn unmute">{{i18n "action.unmute"}} <span id="mute-spinner-{{.Pubkey}}" class="h-indicator"><span class="h-spinner"></span></span></button>
  {{else}}
  <input type="hidden" name="action" value="mute">
  <button type="submit" class="mute-btn mute">{{i18n "action.mute"}} <span id="mute-spinner-{{.Pubkey}}" class="h-indicator"><span class="h-spinner"></span></span></button>
  {{end}}
</form>
{{end}}`

// GetAppendFragmentTemplate returns a template for "Load More" pagination.
// Returns items to append plus updated pagination element.
func GetAppendFragmentTemplate() string {
	return appendFragmentTemplate
}

// appendFragmentTemplate renders items and pagination for append mode.
// The pagination div replaces itself (h-swap="outer" on #pagination).
var appendFragmentTemplate = `{{define "append-fragment"}}
{{range .Items}}
{{template "event-dispatcher" .}}
{{end}}
{{template "pagination" .}}
{{end}}`

// GetNotificationsAppendTemplate returns a template for notifications "Load More".
func GetNotificationsAppendTemplate() string {
	return notificationsAppendTemplate
}

// notificationsAppendTemplate renders notification items for append mode.
var notificationsAppendTemplate = `{{define "notifications-append"}}
{{range .Items}}
<li class="notification-item">
  <header class="notification-header">
    <span class="notification-icon">{{.TypeIcon}}</span>
    <div class="notification-meta">
      <a href="/profile/{{.AuthorNpub}}" class="notification-author" rel="author">{{displayName .AuthorProfile .AuthorNpubShort}}</a>
      <span class="notification-action">{{.TypeLabel}}</span>
      <time class="notification-time" datetime="{{isoTime .Event.CreatedAt}}">{{.TimeAgo}}</time>
    </div>
  </header>
  {{if .ContentHTML}}
  <div class="notification-content">{{.ContentHTML}}</div>
  {{end}}
  {{if .TargetContentHTML}}
  <div class="notification-target-content">{{.TargetContentHTML}}</div>
  {{end}}
  {{if .TargetEventID}}
  <a href="/thread/{{noteLink .TargetEventID}}" h-get h-target="#page-content" h-swap="inner" h-push-url h-prefetch class="notification-link" rel="related">{{i18n "nav.view_note"}} →</a>
  {{else if .Event}}
  <a href="/thread/{{noteLink .Event.ID}}" h-get h-target="#page-content" h-swap="inner" h-push-url h-prefetch class="notification-link" rel="related">{{i18n "nav.view_note"}} →</a>
  {{end}}
</li>
{{end}}
{{template "pagination" .}}
{{end}}`

// GetSearchAppendTemplate returns a template for search "Load More".
func GetSearchAppendTemplate() string {
	return searchAppendTemplate
}

// searchAppendTemplate renders search result items for append mode.
var searchAppendTemplate = `{{define "search-append"}}
{{range .Items}}
<article class="note" aria-label="Note by {{displayName .AuthorProfile .NpubShort}}">
  {{template "author-header" .}}
  <div class="note-content">{{.ContentHTML}}</div>
  <div class="note-footer">
    <a href="/thread/{{eventLink .ID .Kind .Pubkey .DTag}}" h-get h-target="#page-content" h-swap="inner" h-push-url h-prefetch class="text-link" rel="related">{{i18n "nav.view_thread"}}</a>
    {{if or (gt .ReplyCount 0) (gt .ReactionCount 0)}}
    <span class="note-stats">
      {{if gt .ReplyCount 0}}<span class="stat-badge">💬 {{.ReplyCount}}</span>{{end}}
      {{if gt .ReactionCount 0}}<span class="stat-badge">❤️ {{.ReactionCount}}</span>{{end}}
    </span>
    {{end}}
  </div>
</article>
{{end}}
{{template "pagination" .}}
{{end}}`

// GetProfileAppendTemplate returns a template for profile "Load More".
func GetProfileAppendTemplate() string {
	return profileAppendTemplate
}

// GetPostResponseTemplate returns a template for post form response with OOB note.
func GetPostResponseTemplate() string {
	return postResponseTemplate
}

// GetReplyResponseTemplate returns a template for reply form response with OOB reply.
func GetReplyResponseTemplate() string {
	return replyResponseTemplate
}

// replyResponseTemplate returns the cleared reply form plus the new reply as OOB prepend.
var replyResponseTemplate = `{{define "reply-response"}}
<form method="POST" action="/reply" class="reply-form" id="reply-form" h-post h-target="#reply-form" h-swap="outer" h-indicator="#reply-spinner" h-error-target="#reply-error">
  <input type="hidden" name="csrf_token" value="{{.CSRFToken}}">
  <input type="hidden" name="reply_to" value="{{.ReplyTo}}">
  <input type="hidden" name="reply_to_pubkey" value="{{.ReplyToPubkey}}">
  <input type="hidden" name="reply_to_kind" value="{{.ReplyToKind}}">
  {{if .ReplyToDTag}}<input type="hidden" name="reply_to_dtag" value="{{.ReplyToDTag}}">{{end}}
  <input type="hidden" name="reply_count" value="{{.ReplyCount}}">
  <input type="hidden" id="mentions-data-reply" name="mentions" value="{}">
  <label for="reply-content" class="sr-only">Write a reply</label>
  <textarea id="reply-content" name="content" placeholder="Write a reply..."></textarea>
  <a href="{{buildURL "/mentions" "target" "reply"}}" h-get h-target="#mentions-dropdown-reply" h-swap="inner" h-trigger="input debounce:300 from:#reply-content" h-include="#reply-content" hidden aria-hidden="true" aria-label="Mention autocomplete trigger"></a>
  <div id="mentions-dropdown-reply" class="mentions-dropdown"></div>
  <div id="gif-attachment-reply"></div>
  <div class="reply-actions-minimal">
    <button type="submit" class="btn-primary">{{i18n "btn.reply"}} <span id="reply-spinner" class="h-indicator"><span class="h-spinner"></span></span></button>
    {{if .ShowGifButton}}<a href="{{buildURL "/gifs" "target" "reply"}}" h-get h-target="#gif-panel-reply" h-swap="inner" class="btn-primary" title="Add GIF">Add GIF</a>{{end}}
    <details class="cw-dropdown">
      <summary class="cw-toggle" title="{{i18n "label.content_warning"}}">⚠️</summary>
      <div class="cw-options">
        <select name="content_warning" class="cw-select" aria-label="{{i18n "label.content_warning"}}">
          <option value="">{{i18n "option.no_warning"}}</option>
          <option value="nsfw">NSFW</option>
          <option value="spoiler">{{i18n "option.spoiler"}}</option>
          <option value="sensitive">{{i18n "option.sensitive"}}</option>
        </select>
        <input type="text" name="content_warning_custom" class="cw-custom" placeholder="{{i18n "placeholder.custom_warning"}}">
      </div>
    </details>
    <a href="/profile/{{.UserNpub}}" class="reply-avatar-link" title="{{.UserDisplayName}}" rel="author">
      <img src="{{if .UserAvatarURL}}{{.UserAvatarURL}}{{else}}/static/avatar.jpg{{end}}" alt="Your avatar" class="reply-avatar" loading="lazy">
    </a>
  </div>
</form>
<div id="gif-panel-reply" h-oob="inner"></div>
<div id="reply-error" class="form-error" role="alert" aria-live="polite" h-oob="outer"></div>
{{if .NewReply}}<div id="replies-list" h-oob="prepend">
<article class="note reply">
  {{template "author-header" .NewReply}}
  <div class="note-content">{{.NewReply.ContentHTML}}</div>
  {{template "note-footer" .NewReply}}
</article>
</div>
<span id="reply-count" h-oob="outer">{{.ReplyCount}}</span>{{end}}
{{end}}`

// postResponseTemplate returns the cleared post form plus the new note as OOB prepend.
// The new note has h-oob="prepend" targeting #notes-list so it appears at the top.
var postResponseTemplate = `{{define "post-response"}}
<form method="POST" action="/post" class="post-form" id="post-form" h-post h-target="#post-form" h-swap="outer" h-indicator="#post-spinner" h-error-target="#post-error">
  <input type="hidden" name="csrf_token" value="{{.CSRFToken}}">
  <input type="hidden" id="mentions-data-post" name="mentions" value="{}">
  <label for="post-content" class="sr-only">Write a new note</label>
  <textarea id="post-content" name="content" placeholder="What's on your mind?"></textarea>
  <a href="{{buildURL "/mentions" "target" "post"}}" h-get h-target="#mentions-dropdown-post" h-swap="inner" h-trigger="input debounce:300 from:#post-content" h-include="#post-content" hidden aria-hidden="true" aria-label="Mention autocomplete trigger"></a>
  <div id="mentions-dropdown-post" class="mentions-dropdown"></div>
  <div id="gif-attachment-post"></div>
  <div class="post-actions">
    <button type="submit" class="btn-primary">{{i18n "btn.post"}} <span id="post-spinner" class="h-indicator"><span class="h-spinner"></span></span></button>
    {{if .ShowGifButton}}<a href="{{buildURL "/gifs" "target" "post"}}" h-get h-target="#gif-panel-post" h-swap="inner" class="btn-primary post-gif" title="Add GIF">Add GIF</a>{{end}}
    <details class="cw-dropdown">
      <summary class="cw-toggle" title="{{i18n "label.content_warning"}}">⚠️</summary>
      <div class="cw-options">
        <select name="content_warning" class="cw-select" aria-label="{{i18n "label.content_warning"}}">
          <option value="">{{i18n "option.no_warning"}}</option>
          <option value="nsfw">NSFW</option>
          <option value="spoiler">{{i18n "option.spoiler"}}</option>
          <option value="sensitive">{{i18n "option.sensitive"}}</option>
        </select>
        <input type="text" name="content_warning_custom" class="cw-custom" placeholder="{{i18n "placeholder.custom_warning"}}">
      </div>
    </details>
  </div>
</form>
<div id="gif-panel-post" h-oob="inner"></div>
<div id="post-error" class="form-error" role="alert" aria-live="polite" h-oob="outer"></div>
{{if .NewNote}}<div id="notes-list" h-oob="prepend">{{template "event-dispatcher" .NewNote}}</div>{{end}}
{{end}}`

// profileAppendTemplate renders profile note items for append mode.
var profileAppendTemplate = `{{define "profile-append"}}
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
  <div class="note-content">{{.ContentHTML}}</div>
  {{template "note-footer" .}}
</article>
{{end}}
{{template "pagination" .}}
{{end}}`

// GetNewNotesIndicatorTemplate returns the new notes indicator template.
// Used by timeline polling to show "X new posts" button.
func GetNewNotesIndicatorTemplate() string {
	return newNotesIndicatorTemplate
}

// newNotesIndicatorTemplate renders the new notes indicator with optional button.
// Data fields:
//   - Since: timestamp for polling continuation
//   - Kinds: kinds param for polling (e.g., "1,6")
//   - Filter: filter name for label (e.g., "notes", "all")
//   - RefreshURL: URL to navigate to when clicked
//   - Count: number of new posts (0 = empty indicator)
//   - Label: display label (e.g., "5 new notes")
var newNotesIndicatorTemplate = `{{define "new-notes-indicator"}}<div id="new-notes-indicator" h-poll="{{buildURL "/timeline/check-new" "kinds" .Kinds "filter" .Filter "since" .Since "url" .RefreshURL}} 30s" h-target="#new-notes-indicator" h-swap="outer" h-poll-pause-hidden>{{if gt .Count 0}}
	<a href="{{.RefreshURL}}" class="new-notes-btn">{{.Label}}</a>{{end}}
</div>{{end}}`

// GetLinkPreviewTemplate returns the link preview card template.
func GetLinkPreviewTemplate() string {
	return linkPreviewTemplate
}

// linkPreviewTemplate renders an Open Graph link preview card.
// Data fields:
//   - URL: the link URL
//   - Image: preview image URL (optional)
//   - SiteName: site name (optional)
//   - Title: link title
//   - Description: link description (optional, truncated to 150 chars)
var linkPreviewTemplate = `{{define "link-preview"}}<a href="{{.URL}}" target="_blank" rel="noopener" class="link-preview">{{if .Image}}<img src="{{.Image}}" alt="" class="link-preview-image" loading="lazy">{{end}}<div class="link-preview-content">{{if .SiteName}}<div class="link-preview-site">{{.SiteName}}</div>{{end}}<div class="link-preview-title">{{.Title}}</div>{{if .Description}}<div class="link-preview-desc">{{.Description}}</div>{{end}}</div></a>{{end}}`

// GetWavlakePlayerTemplate returns the Wavlake audio player template.
func GetWavlakePlayerTemplate() string {
	return wavlakePlayerTemplate
}

// wavlakePlayerTemplate renders a native audio player for Wavlake tracks.
// Data fields:
//   - Icon: 🎵 for music, 🎙️ for podcasts
//   - Title: track title
//   - Creator: artist/creator name
//   - Duration: formatted duration (e.g., "3:45")
//   - AudioURL: URL to the audio stream
//   - PageURL: URL to the Wavlake page
var wavlakePlayerTemplate = `{{define "wavlake-player"}}<div class="wavlake-player"><div class="wavlake-info"><span class="wavlake-icon">{{.Icon}}</span><div class="wavlake-meta"><a href="{{.PageURL}}" target="_blank" rel="noopener" class="wavlake-title">{{.Title}}</a><span class="wavlake-creator">{{.Creator}}{{if .Duration}} · {{.Duration}}{{end}}</span></div></div><audio src="{{.AudioURL}}" controls preload="metadata" class="wavlake-audio"></audio></div>{{end}}`

// GetOOBFlashTemplate returns the OOB flash message template for HelmJS updates.
func GetOOBFlashTemplate() string {
	return oobFlashTemplate
}

// oobFlashTemplate renders an OOB flash message for HelmJS partial updates.
// Use h-oob="true" to replace #flash-messages element.
// Data fields:
//   - Message: the message text
//   - Type: "error" or "success"
var oobFlashTemplate = `{{define "oob-flash"}}<div id="flash-messages" h-oob="true">{{if .Message}}<div class="{{if eq .Type "error"}}error-box{{else}}flash-message{{end}}" role="{{if eq .Type "error"}}alert{{else}}status{{end}}" aria-live="polite">{{.Message}}</div>{{end}}</div>{{end}}`
