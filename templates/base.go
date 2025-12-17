package templates

// Base template - shared structure for all HTML pages.
// Page templates define "content" block. Some override "header" block.

func GetBaseTemplates() string {
	return baseTemplate + headerTemplate + footerTemplate
}

var baseTemplate = `{{define "base"}}{{$site := siteConfig}}<!DOCTYPE html>
<html lang="en"{{if .ThemeClass}} class="{{.ThemeClass}}"{{end}}>
<head>
  <meta charset="UTF-8">
  <meta name="viewport" content="width=device-width, initial-scale=1.0">
  <meta name="theme-color" content="{{$site.Meta.ThemeColor.Light}}" media="(prefers-color-scheme: light)">
  <meta name="theme-color" content="{{$site.Meta.ThemeColor.Dark}}" media="(prefers-color-scheme: dark)">
  <meta name="description" content="{{if .PageDescription}}{{.PageDescription}}{{else}}{{$site.Site.Description}}{{end}}">
  <meta property="og:title" content="{{.Title}} - {{$site.Site.Name}}">
  <meta property="og:description" content="{{if .PageDescription}}{{.PageDescription}}{{else}}{{$site.Site.Description}}{{end}}">
  <meta property="og:type" content="{{$site.OpenGraph.Type}}">
  <meta property="og:image" content="{{if .PageImage}}{{.PageImage}}{{else}}{{$site.OpenGraph.Image}}{{end}}">
  {{if .CanonicalURL}}<meta property="og:url" content="{{.CanonicalURL}}">
  <link rel="canonical" href="{{.CanonicalURL}}">{{end}}
  <title>{{.Title}} - {{$site.Site.Name}}</title>
  <link rel="icon" href="{{$site.Links.Favicon}}">
  {{range $site.Links.Preconnect}}<link rel="preconnect" href="{{.}}">
  {{end}}<link rel="stylesheet" href="{{$site.Links.Stylesheet}}">
  {{range $site.Scripts}}<script src="{{.Src}}"{{if .Defer}} defer{{end}}{{if .Async}} async{{end}}></script>
  {{end}}
</head>
<body id="top">
  <a href="#main-content" class="skip-link">{{i18n "a11y.skip_to_main"}}</a>
  <div class="container">
    {{template "header" .}}
    <div id="page-content">
      {{if .KindFilters}}<div class="kind-filter" id="kind-filter">
        {{range .KindFilters}}
        <a href="{{.Href}}" h-get h-target="#page-content" h-swap="inner" h-replace-url h-scroll="top" h-indicator="#nav-loading" class="{{if .Active}}active{{end}}">{{.Title}}</a>
        {{end}}
      </div>{{end}}
      {{if and .LoggedIn .ShowPostForm}}
      <div class="post-form-container">
        <div id="post-error" class="form-error" role="alert" aria-live="polite"></div>
        <form method="POST" action="/html/post" class="post-form" id="post-form" h-post h-target="#post-form" h-swap="outer" h-indicator="#post-spinner" h-error-target="#post-error">
          <input type="hidden" name="csrf_token" value="{{.CSRFToken}}">
          <label for="post-content" class="sr-only">Write a new note</label>
          <textarea id="post-content" name="content" placeholder="What's on your mind?"></textarea>
          <div id="gif-attachment-post"></div>
          <div class="post-actions">
            <button type="submit" class="btn-primary">{{i18n "btn.post"}} <span id="post-spinner" class="h-indicator"><span class="h-spinner"></span></span></button>
            {{if .ShowGifButton}}<a href="/html/gifs?target=post" h-get h-target="#gif-panel-post" h-swap="inner" class="btn-primary post-gif" title="Add GIF">Add GIF</a>{{end}}
          </div>
        </form>
        <div id="gif-panel-post"></div>
      </div>
      {{end}}
      <main id="main-content">
        <h1 class="sr-only">{{.Title}}</h1>
        {{template "content" .}}
      </main>
    </div>
    {{template "footer" .}}
  </div>
</body>
</html>{{end}}
`

var headerTemplate = `{{define "header"}}
<header class="sticky-section">
  <nav>
    <span id="feed-tabs">{{range .FeedModes}}{{if eq .IconOnly "always"}}<a href="{{.Href}}" h-get h-target="#page-content" h-swap="inner" h-push-url h-scroll="top" h-indicator="#nav-loading" class="nav-icon{{if .Active}} active{{end}}" title="{{.Title}}"{{if .Active}} aria-current="page"{{end}}>{{.Icon}}</a>{{else if eq .IconOnly "mobile"}}<a href="{{.Href}}" h-get h-target="#page-content" h-swap="inner" h-push-url h-scroll="top" h-indicator="#nav-loading" class="nav-tab{{if .Active}} active{{end}}"{{if .Active}} aria-current="page"{{end}}><span class="icon-mobile-only" title="{{.Title}}">{{.Icon}}</span><span class="icon-desktop-only">{{if .Icon}}{{.Icon}} {{end}}{{.Title}}</span></a>{{else if eq .IconOnly "desktop"}}<a href="{{.Href}}" h-get h-target="#page-content" h-swap="inner" h-push-url h-scroll="top" h-indicator="#nav-loading" class="nav-tab{{if .Active}} active{{end}}"{{if .Active}} aria-current="page"{{end}}><span class="icon-desktop-only" title="{{.Title}}">{{.Icon}}</span><span class="icon-mobile-only">{{if .Icon}}{{.Icon}} {{end}}{{.Title}}</span></a>{{else}}<a href="{{.Href}}" h-get h-target="#page-content" h-swap="inner" h-push-url h-scroll="top" h-indicator="#nav-loading" class="nav-tab{{if .Active}} active{{end}}"{{if .Active}} aria-current="page"{{end}}>{{if .Icon}}{{.Icon}} {{end}}{{.Title}}</a>{{end}}{{end}}</span>
    <span id="nav-loading" class="h-indicator"><span class="h-spinner"></span></span>
    <div class="ml-auto flex-center gap-sm">
      {{range .NavItems}}
      {{if eq .IconOnly "always"}}<a href="{{.Href}}" h-get h-target="#page-content" h-swap="inner" h-push-url h-scroll="top" h-indicator="#nav-loading" class="nav-icon{{if .Active}} active{{end}}" title="{{.Title}}"{{if .Active}} aria-current="page"{{end}}>{{.Icon}}{{if .HasBadge}}<span class="notification-badge{{if not $.HasUnreadNotifications}} notification-badge-hidden{{end}}" id="notification-badge" role="status"{{if $.HasUnreadNotifications}} aria-label="New notifications"{{end}}></span>{{end}}</a>{{else if eq .IconOnly "mobile"}}<a href="{{.Href}}" h-get h-target="#page-content" h-swap="inner" h-push-url h-scroll="top" h-indicator="#nav-loading" class="nav-tab{{if .Active}} active{{end}}"{{if .Active}} aria-current="page"{{end}}><span class="icon-mobile-only" title="{{.Title}}">{{.Icon}}{{if .HasBadge}}<span class="notification-badge{{if not $.HasUnreadNotifications}} notification-badge-hidden{{end}}" id="notification-badge" role="status"{{if $.HasUnreadNotifications}} aria-label="New notifications"{{end}}></span>{{end}}</span><span class="icon-desktop-only">{{if .Icon}}{{.Icon}} {{end}}{{.Title}}{{if .HasBadge}} <span class="badge">•</span>{{end}}</span></a>{{else if eq .IconOnly "desktop"}}<a href="{{.Href}}" h-get h-target="#page-content" h-swap="inner" h-push-url h-scroll="top" h-indicator="#nav-loading" class="nav-tab{{if .Active}} active{{end}}"{{if .Active}} aria-current="page"{{end}}><span class="icon-desktop-only" title="{{.Title}}">{{.Icon}}{{if .HasBadge}}<span class="notification-badge{{if not $.HasUnreadNotifications}} notification-badge-hidden{{end}}" id="notification-badge" role="status"{{if $.HasUnreadNotifications}} aria-label="New notifications"{{end}}></span>{{end}}</span><span class="icon-mobile-only">{{if .Icon}}{{.Icon}} {{end}}{{.Title}}{{if .HasBadge}} <span class="badge">•</span>{{end}}</span></a>{{else}}<a href="{{.Href}}" h-get h-target="#page-content" h-swap="inner" h-push-url h-scroll="top" h-indicator="#nav-loading" class="nav-tab{{if .Active}} active{{end}}"{{if .Active}} aria-current="page"{{end}}>{{if .Icon}}{{.Icon}} {{end}}{{.Title}}{{if .HasBadge}} <span class="badge">•</span>{{end}}</a>{{end}}
      {{end}}
      {{if .LoggedIn}}<details class="settings-dropdown">
        <summary id="settings-toggle" class="settings-toggle" title="{{.SettingsToggle.Title}}">{{if .SettingsToggle.IsImage}}<img src="{{.SettingsToggle.Icon}}" alt="{{.SettingsToggle.Title}}" class="settings-toggle-avatar" loading="lazy" />{{else}}{{.SettingsToggle.Icon}}{{end}}</summary>
        <div class="settings-menu">
          {{range .SettingsItems}}
          {{if .DividerBefore}}<div class="settings-divider"></div>{{end}}
          {{if and .IsDynamic (eq .DynamicType "relays")}}
          {{if $.ActiveRelays}}
          <div class="settings-item">{{len $.ActiveRelays}} relay{{if gt (len $.ActiveRelays) 1}}s{{end}}:</div>
          {{range $.ActiveRelays}}<div class="relay-item">{{.}}</div>{{end}}
          {{end}}
          {{else}}
          <div class="settings-item">
            {{if .IsForm}}
            <form method="POST" action="{{.Href}}" class="inline-form">
              <input type="hidden" name="csrf_token" value="{{$.CSRFToken}}">
              <label for="settings-{{.Name}}" class="sr-only">{{.Title}}</label>
              <button type="submit" id="settings-{{.Name}}" class="ghost-btn text-xs">{{if .Icon}}{{.Icon}} {{end}}{{.Title}}</button>
            </form>
            {{else}}
            <a href="{{.Href}}" class="ghost-btn text-xs{{if .Active}} active{{end}}">{{if .Icon}}{{.Icon}} {{end}}{{.Title}}</a>
            {{end}}
          </div>
          {{end}}
          {{end}}
        </div>
      </details>{{end}}
      <span id="login-btn">{{if not .LoggedIn}}<a href="/html/login" class="btn-primary">{{i18n "btn.login"}}</a>{{end}}</span>
    </div>
  </nav>
  {{if .LoggedIn}}<span h-sse="/stream/notifications?format=html" hidden><template h-sse-on="notification" h-target="#notification-badge" h-swap="outer"></template></span>{{end}}
  <span id="config-sse" h-sse="/stream/config" hidden><template h-sse-on="reload" h-target="#config-sse" h-swap="inner"></template></span>
  <a id="config-reload" href="{{.CurrentURL}}{{if contains .CurrentURL "?"}}&amp;{{else}}?{{end}}refresh=1" h-get h-target="body" h-swap="morph" h-select="body" h-trigger="h:sse-message from:#config-sse" hidden aria-hidden="true" aria-label="Reload page configuration"></a>
</header>
{{end}}`

var footerTemplate = `{{define "footer"}}
<footer>
<a href="#top" class="scroll-top" aria-label="Scroll to top">↑</a>
</footer>
{{end}}`
